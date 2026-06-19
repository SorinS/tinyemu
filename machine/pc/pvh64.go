package pc

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"

	"github.com/sorins/tinyemu-go/cpu/x86_64"
)

// PVH boot protocol — Xen's clean alternative to multiboot. Used by OSv,
// FreeBSD-as-Xen-DomU, and any other ELF that exports an
// XEN_ELFNOTE_PHYS32_ENTRY (note type 0x12 in the "Xen" namespace).
//
// CPU state at entry:
//   - 32-bit protected mode (CR0.PE=1, CR0.PG=0, EFER.LME=0)
//   - flat 4 GiB CS/DS/ES/SS/FS/GS, DPL=0
//   - EBX = phys-addr of a hvm_start_info struct
//   - EIP = the address in the PHYS32_ENTRY note
//
// The kernel's own bootstrap is responsible for enabling paging and
// switching to long mode.
//
// Reference: xen-project/xen include/public/arch-x86/hvm/start_info.h
const xenElfNotePhys32Entry = 0x12 // XEN_ELFNOTE_PHYS32_ENTRY

// findPVHEntry scans the ELF's PT_NOTE program headers for the Xen
// XEN_ELFNOTE_PHYS32_ENTRY note and returns its 32-bit entry address.
func findPVHEntry(f *elf.File) (uint32, bool) {
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_NOTE {
			continue
		}
		data := make([]byte, prog.Filesz)
		if _, err := prog.ReadAt(data, 0); err != nil {
			continue
		}
		off := 0
		for off+12 <= len(data) {
			namesz := binary.LittleEndian.Uint32(data[off:])
			descsz := binary.LittleEndian.Uint32(data[off+4:])
			typ := binary.LittleEndian.Uint32(data[off+8:])
			namePadded := (int(namesz) + 3) &^ 3
			descPadded := (int(descsz) + 3) &^ 3
			name := ""
			if namesz > 0 && off+12+int(namesz) <= len(data) {
				raw := data[off+12 : off+12+int(namesz)]
				if i := bytes.IndexByte(raw, 0); i >= 0 {
					name = string(raw[:i])
				} else {
					name = string(raw)
				}
			}
			if name == "Xen" && typ == xenElfNotePhys32Entry && descsz >= 4 {
				descOff := off + 12 + namePadded
				if descOff+4 <= len(data) {
					return binary.LittleEndian.Uint32(data[descOff:]), true
				}
			}
			advance := 12 + namePadded + descPadded
			if advance <= 0 || off+advance > len(data) {
				break
			}
			off += advance
		}
	}
	return 0, false
}

// loadPVH64 loads an ELF kernel via the PVH boot protocol. The ELF must
// carry an XEN_ELFNOTE_PHYS32_ENTRY note; without it this returns an
// error so the caller can fall back to bzImage / vmlinux64 paths.
//
// Layout in low memory:
//
//	0x09000  hvm_start_info (40 bytes)
//	0x09100  hvm_memmap_table_entry array (24 bytes each)
//	0x09400  command line (null-terminated)
//	0x80000  GDT (3 entries: null, 32-bit code, 32-bit data)
func (p *PC) loadPVH64(kernelData, initrdData []byte, cmdLine string) error {
	cpu64, ok := p.cpu.(*x86_64.CPU)
	if !ok {
		return fmt.Errorf("loadPVH64 requires *x86_64.CPU; have %T", p.cpu)
	}

	f, err := elf.NewFile(bytes.NewReader(kernelData))
	if err != nil {
		return fmt.Errorf("not a valid ELF: %w", err)
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("expected ELFCLASS64; got %v", f.Class)
	}

	entry, ok := findPVHEntry(f)
	if !ok {
		return fmt.Errorf("no XEN_ELFNOTE_PHYS32_ENTRY note in ELF")
	}

	// Load PT_LOAD segments at their physical addresses.
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		data := make([]byte, prog.Filesz)
		if _, err := prog.ReadAt(data, 0); err != nil {
			return fmt.Errorf("read PT_LOAD @ paddr %#x: %w", prog.Paddr, err)
		}
		p.writePhysBlock(uint32(prog.Paddr), data)
		if prog.Memsz > prog.Filesz {
			zeroStart := prog.Paddr + prog.Filesz
			zeroLen := prog.Memsz - prog.Filesz
			rng := p.memMap.GetRange(zeroStart)
			if rng != nil && rng.IsRAM {
				off := zeroStart - rng.Addr
				if off+zeroLen <= uint64(len(rng.PhysMem)) {
					clear(rng.PhysMem[off : off+zeroLen])
					continue
				}
			}
			for i := uint64(0); i < zeroLen; i++ {
				p.writePhys8(uint32(zeroStart+i), 0)
			}
		}
	}

	const startInfoAddr uint32 = 0x9000 // hvm_start_info struct (56 bytes)
	const memmapAddr uint32 = 0x9100   // E820-style entries (24 bytes each)
	const modlistAddr uint32 = 0x9300  // hvm_modlist_entry array (32 bytes each)
	const cmdLineAddr uint32 = 0x9400  // null-terminated boot cmdline

	// Build the memmap. A single RAM entry covering all of physical RAM
	// is enough for most kernels; OSv and FreeBSD only care that RAM is
	// reported and contiguous.
	memmapEntries := uint32(0)
	addMemmap := func(addr, size uint64, typ uint32) {
		off := memmapAddr + memmapEntries*24
		p.patchBootParam64(off, addr)
		p.patchBootParam64(off+8, size)
		p.patchBootParam32(off+16, typ)
		p.patchBootParam32(off+20, 0)
		memmapEntries++
	}
	// Type 1 = E820 RAM. The "reserved below 1 MB" carve-out isn't
	// required — OSv parses the table and uses what it sees.
	addMemmap(0, uint64(p.ramSize), 1)

	// Command line (immediately follows the memmap + modlist areas).
	cmdLineBuf := append([]byte(cmdLine), 0)
	for i, b := range cmdLineBuf {
		p.writePhys8(cmdLineAddr+uint32(i), b)
	}

	// Initrd → PVH module 0. The PVH spec lets the loader pass
	// arbitrary modules via modlist_paddr; Linux's pvh_start_xen
	// treats the first one as the initramfs and points
	// boot_params.hdr.ramdisk_{image,size} at it. Place the bytes
	// just below the top of RAM so the kernel's later high-memory
	// initialisation doesn't trample them, then build a single
	// hvm_modlist_entry describing the region.
	nrModules := uint32(0)
	if len(initrdData) > 0 {
		initrdAddr := uint32(p.ramSize) - uint32(len(initrdData))
		initrdAddr &^= 0xFFF
		if initrdAddr < 0x100000 {
			return fmt.Errorf("initrd doesn't fit below the kernel load region (%d bytes, %d MiB RAM)",
				len(initrdData), p.ramSize>>20)
		}
		p.writePhysBlock(initrdAddr, initrdData)
		// hvm_modlist_entry:
		//   0x00  uint64 paddr          — module bytes in guest RAM
		//   0x08  uint64 size           — bytes
		//   0x10  uint64 cmdline_paddr  — optional per-module cmdline (0 = none)
		//   0x18  uint64 reserved       — must be zero
		p.patchBootParam64(modlistAddr+0x00, uint64(initrdAddr))
		p.patchBootParam64(modlistAddr+0x08, uint64(len(initrdData)))
		p.patchBootParam64(modlistAddr+0x10, 0)
		p.patchBootParam64(modlistAddr+0x18, 0)
		nrModules = 1
	}

	// hvm_start_info: magic, version, flags, nr_modules, modlist_paddr,
	// cmdline_paddr, rsdp_paddr, memmap_paddr, memmap_entries, reserved.
	p.patchBootParam32(startInfoAddr+0x00, 0x336ec578) // magic "xEn3"
	p.patchBootParam32(startInfoAddr+0x04, 1)          // version
	p.patchBootParam32(startInfoAddr+0x08, 0)          // flags
	p.patchBootParam32(startInfoAddr+0x0C, nrModules)  // nr_modules
	if nrModules > 0 {
		p.patchBootParam64(startInfoAddr+0x10, uint64(modlistAddr))
	} else {
		p.patchBootParam64(startInfoAddr+0x10, 0)
	}
	p.patchBootParam64(startInfoAddr+0x18, uint64(cmdLineAddr))
	// rsdp_paddr — installACPIDirect lays out RSDP + tables at the
	// fixed direct-boot addresses (0xE0000 / 0xE0080) and returns the
	// RSDP physical address. Linux's PVH entry path prefers this
	// field over an F-segment scan when it's non-zero.
	rsdpAddr := installACPIDirect(p)
	p.patchBootParam64(startInfoAddr+0x20, uint64(rsdpAddr))
	p.patchBootParam64(startInfoAddr+0x28, uint64(memmapAddr))
	p.patchBootParam32(startInfoAddr+0x30, memmapEntries)
	p.patchBootParam32(startInfoAddr+0x34, 0) // reserved

	// GDT at 0x80000 — flat 32-bit code/data, DPL=0.
	const gdtAddr uint64 = 0x80000
	p.memMap.Write64(gdtAddr+0x00, 0x0)
	p.memMap.Write64(gdtAddr+0x08, 0x00CF9A000000FFFF) // CS: G=1 D=1 type=A
	p.memMap.Write64(gdtAddr+0x10, 0x00CF92000000FFFF) // DS: G=1 D=1 type=2

	// Protected mode, paging off, no long mode.
	cpu64.SetCR(0, x86_64.CR0_ET|x86_64.CR0_NE|x86_64.CR0_PE)
	cpu64.SetCR(4, 0)
	cpu64.SetCR64(3, 0)
	cpu64.SetEFER(0)

	cpu64.SetSegBase64(x86_64.GDTR, gdtAddr)
	cpu64.SetSegLimit(x86_64.GDTR, 0x17)

	// CS: 32-bit code (D=1, L=0). The G/D/L/AVL flags pack into the
	// upper-nibble of the access word for recomputeMode's benefit.
	cpu64.SetSeg(x86_64.CS, 0x08)
	cpu64.SetSegBase64(x86_64.CS, 0)
	cpu64.SetSegLimit(x86_64.CS, 0xFFFFFFFF)
	cpu64.SetSegAccess(x86_64.CS, 0xC9A) // G=1 D=1 L=0; type=A code RX

	// Data segments flat.
	for _, s := range []int{x86_64.DS, x86_64.ES, x86_64.SS, x86_64.FS, x86_64.GS} {
		cpu64.SetSeg(s, 0x10)
		cpu64.SetSegBase64(s, 0)
		cpu64.SetSegLimit(s, 0xFFFFFFFF)
		cpu64.SetSegAccess(s, 0xC92)
	}

	// PVH spec: EBX = start_info ptr; ESI/EAX undefined (we zero them).
	// Stack at 0x90000 grown down — kernels that need a usable stack
	// before they set their own up.
	cpu64.SetReg64(x86_64.RBX, uint64(startInfoAddr))
	cpu64.SetReg64(x86_64.RSP, 0x90000)
	cpu64.SetRIP(uint64(entry))
	cpu64.SetRFLAGS(2)

	return nil
}
