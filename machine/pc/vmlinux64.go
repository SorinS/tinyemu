package pc

import (
	"bytes"
	"debug/elf"
	"fmt"
	"os"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
)

// loadVMLinux64 loads a decompressed x86_64 vmlinux ELF directly,
// bypassing the bzImage decompressor stub. This is the long-mode
// counterpart to loadVMLinux for i386: parse the ELF, copy each
// PT_LOAD segment to its physical address (with the BSS zeroed),
// build boot_params and identity-mapped 4-level paging, and jump
// to the ELF's entry point.
//
// The decompressor's "out of pgt_buf" panic on TinyCorePure64's
// bzImage is sidestepped entirely here — we never run the
// decompressor since the input is already decompressed.
func (p *PC) loadVMLinux64(kernelData, initrdData []byte, cmdLine string) error {
	cpu64, ok := p.cpu.(*x86_64.CPU)
	if !ok {
		return fmt.Errorf("loadVMLinux64 requires *x86_64.CPU; have %T", p.cpu)
	}

	f, err := elf.NewFile(bytes.NewReader(kernelData))
	if err != nil {
		return fmt.Errorf("not a valid ELF: %w", err)
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("expected ELFCLASS64; got %v", f.Class)
	}
	if f.Machine != elf.EM_X86_64 {
		return fmt.Errorf("expected EM_X86_64; got %v", f.Machine)
	}

	// Load each PT_LOAD segment at its physical address. The vmlinux
	// is linked at high-half virtual addresses (paddr != vaddr), but
	// the entry point is given as a physical address that lands
	// inside one of the segments — we can enter through the identity
	// map and the kernel's position-independent startup will sort
	// out the high-half jump on its own.
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		// Copy filesz bytes from the ELF segment to physical memory.
		data := make([]byte, prog.Filesz)
		if _, err := prog.ReadAt(data, 0); err != nil {
			return fmt.Errorf("read PT_LOAD @ paddr %#x: %w", prog.Paddr, err)
		}
		p.writePhysBlock(uint32(prog.Paddr), data)
		// Zero the BSS slice (memsz - filesz) at the end of the
		// segment. Use the direct PhysMem slice when the destination
		// is within a RAM range; falls back to writePhys8 otherwise.
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

	// boot_params at 0x90000 — same layout as bzimage64.
	const setupAddr uint32 = 0x90000
	for i := uint32(0); i < 0x1000; i++ {
		p.writePhys8(setupAddr+i, 0)
	}
	// boot_flag (offset 0x1FE) and header magic (offset 0x202) so
	// the kernel recognises the boot_params as a valid Linux setup.
	p.patchBootParam(setupAddr+0x1FE, 0x55)
	p.patchBootParam(setupAddr+0x1FF, 0xAA)
	p.patchBootParam32(setupAddr+0x202, 0x53726448) // "HdrS"
	p.patchBootParam(setupAddr+0x206, 0x0F)         // version 2.15 low
	p.patchBootParam(setupAddr+0x207, 0x02)         // version high
	p.patchBootParam(setupAddr+0x210, 0xFF)         // type_of_loader = generic
	p.patchBootParam(setupAddr+0x211, 0x01)         // LOADED_HIGH

	// Command line just past boot_params.
	const cmdLineAddr uint32 = 0x99000
	cmdLineBuf := append([]byte(cmdLine), 0)
	for i, b := range cmdLineBuf {
		p.writePhys8(cmdLineAddr+uint32(i), b)
	}
	p.patchBootParam32(setupAddr+0x228, cmdLineAddr)

	// E820 map.
	p.writeE820Map(setupAddr)

	// ACPI: install tables + publish RSDP via boot_params.acpi_rsdp_addr
	// (offset 0x070, u64). Same plumbing as bzimage64.
	rsdpAddr := installACPIDirect(p)
	p.patchBootParam32(setupAddr+0x070, rsdpAddr)
	p.patchBootParam32(setupAddr+0x074, 0)

	// initrd at top of RAM.
	if len(initrdData) > 0 {
		initrdAddr := uint32(p.ramSize) - uint32(len(initrdData))
		initrdAddr &^= 0xFFF
		if initrdAddr < 0x100000 {
			return fmt.Errorf("initrd too large")
		}
		p.writePhysBlock(initrdAddr, initrdData)
		p.patchBootParam32(setupAddr+0x218, initrdAddr)
		p.patchBootParam32(setupAddr+0x21C, uint32(len(initrdData)))
	}

	// Identity-mapped 4-level paging at 0x70000 — same shape as
	// bzimage64. 1 GiB pages cover the first 4 GiB.
	const pml4Addr uint64 = 0x70000
	const pdptAddr uint64 = 0x71000
	p.memMap.Write64(pml4Addr, pdptAddr|0x3)
	for gb := uint64(0); gb < 4; gb++ {
		p.memMap.Write64(pdptAddr+gb*8, (gb<<30)|0x83)
	}
	// Also map the kernel-high-half virtual range
	// (0xffffffff80000000..0xffffffffffffffff = PML4[511] + PDPT[510..511])
	// to physical 0..2 GiB. Without this, any jump to a high-half
	// kernel symbol faults.
	const pml4HighIdx = 511
	const pdpt2Addr uint64 = 0x72000
	p.memMap.Write64(pml4Addr+pml4HighIdx*8, pdpt2Addr|0x3)
	p.memMap.Write64(pdpt2Addr+510*8, (uint64(0)<<30)|0x83)
	p.memMap.Write64(pdpt2Addr+511*8, (uint64(1)<<30)|0x83)

	// Map the kernel's "direct map" region — Linux's standard
	// 0xffff888000000000+ window onto all of physical RAM. PML4
	// index = (0xffff888000000000 >> 39) & 0x1FF = 0x111. We give
	// it its own PDPT that 1-GiB-maps the first 4 GiB.
	const pml4DirectIdx = 0x111
	const pdpt3Addr uint64 = 0x73000
	p.memMap.Write64(pml4Addr+pml4DirectIdx*8, pdpt3Addr|0x3)
	for gb := uint64(0); gb < 4; gb++ {
		p.memMap.Write64(pdpt3Addr+gb*8, (gb<<30)|0x83)
	}

	// GDT (small, just so far jumps have something valid to load).
	const gdtAddr uint64 = 0x80000
	p.memMap.Write64(gdtAddr+0x00, 0x0)
	p.memMap.Write64(gdtAddr+0x08, 0x00AF9A000000FFFF) // kernel CS 64-bit
	p.memMap.Write64(gdtAddr+0x10, 0x00CF92000000FFFF) // kernel DS

	cpu64.SetCR(0, x86_64.CR0_ET|x86_64.CR0_NE)
	cpu64.SetCR(4, x86_64.CR4_PAE)
	cpu64.SetCR64(3, pml4Addr)
	cpu64.SetEFER(x86_64.EFER_LME | x86_64.EFER_NXE)
	cpu64.SetCR(0, x86_64.CR0_ET|x86_64.CR0_NE|x86_64.CR0_PE|x86_64.CR0_PG)
	if cpu64.GetEFER()&x86_64.EFER_LMA == 0 {
		return fmt.Errorf("LMA failed to latch after enabling paging")
	}
	cpu64.SetSegBase64(x86_64.GDTR, gdtAddr)
	cpu64.SetSegLimit(x86_64.GDTR, 0x17)
	cpu64.SetSeg(x86_64.CS, 0x08)
	cpu64.SetSegBase64(x86_64.CS, 0)
	cpu64.SetSegLimit(x86_64.CS, 0xFFFFFFFF)
	cpu64.SetSegAccess(x86_64.CS, 0xA9A)
	for _, s := range []int{x86_64.DS, x86_64.ES, x86_64.SS, x86_64.FS, x86_64.GS} {
		cpu64.SetSeg(s, 0x10)
		cpu64.SetSegBase64(s, 0)
		cpu64.SetSegLimit(s, 0xFFFFFFFF)
		cpu64.SetSegAccess(s, 0xC92)
	}

	// Linux x86_64 boot protocol: RSI = boot_params, RIP = entry.
	// Use f.Entry — Linux's vmlinux ELF sets ENTRY(phys_startup_64),
	// which is the cold-boot entry point. Empirically this is far
	// into the kernel image (the startup_64 symbol lives in a later
	// section, not at offset 0 of .text); we just trust the ELF.
	cpu64.SetReg64(x86_64.RSI, uint64(setupAddr))
	cpu64.SetReg64(x86_64.RSP, uint64(setupAddr-8))
	// Allow override via TINYEMU_X64_ENTRY=<hex> for boot debugging.
	entry := f.Entry
	if s := os.Getenv("TINYEMU_X64_ENTRY"); s != "" {
		if len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X") {
			s = s[2:]
		}
		var v uint64
		for _, ch := range s {
			var d uint64
			switch {
			case ch >= '0' && ch <= '9':
				d = uint64(ch - '0')
			case ch >= 'a' && ch <= 'f':
				d = uint64(ch-'a') + 10
			case ch >= 'A' && ch <= 'F':
				d = uint64(ch-'A') + 10
			default:
				continue
			}
			v = v*16 + d
		}
		if v != 0 {
			entry = v
		}
	}
	cpu64.SetRIP(entry)
	cpu64.SetRFLAGS(2)
	return nil
}
