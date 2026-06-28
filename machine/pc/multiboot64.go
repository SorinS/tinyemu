package pc

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"

	"github.com/sorins/tinyemu-go/cpu/x86_64"
)

// Multiboot2 boot protocol (Multiboot2 Specification 2.0). Used by bare-metal
// kernels built for "qemu-system-x86_64 -kernel" — notably NuttX's qemu-intel64
// board. The loader:
//   - finds the multiboot2 header (magic 0xE85250D6) in the first 32 KiB,
//   - loads the ELF PT_LOAD segments to their physical addresses,
//   - hands control in 32-bit protected mode (CR0.PE=1, paging off) with
//     EAX = 0x36D76289 and EBX = phys-addr of a multiboot2 information struct.
// This mirrors loadPVH64 (same GDT + segment + 32-bit entry setup); only the
// information structure and the EAX magic differ.
const (
	multiboot2HeaderMagic = 0xE85250D6 // in the kernel image
	multiboot2BootMagic   = 0x36D76289 // passed to the kernel in EAX
)

// findMultiboot2Header scans the first 32 KiB for a valid multiboot2 header
// (8-byte aligned; magic + architecture + header_length + checksum == 0).
func findMultiboot2Header(data []byte) bool {
	limit := min(len(data), 32768)
	for off := 0; off+16 <= limit; off += 8 {
		if binary.LittleEndian.Uint32(data[off:]) != multiboot2HeaderMagic {
			continue
		}
		arch := binary.LittleEndian.Uint32(data[off+4:])
		hlen := binary.LittleEndian.Uint32(data[off+8:])
		cksum := binary.LittleEndian.Uint32(data[off+12:])
		if multiboot2HeaderMagic+arch+hlen+cksum == 0 {
			return true
		}
	}
	return false
}

func (p *PC) loadMultiboot64(kernelData, initrdData []byte, cmdLine string) error {
	cpu64, ok := p.cpu.(*x86_64.CPU)
	if !ok {
		return fmt.Errorf("loadMultiboot64 requires *x86_64.CPU; have %T", p.cpu)
	}
	if !findMultiboot2Header(kernelData) {
		return fmt.Errorf("no multiboot2 header in the first 32 KiB")
	}
	f, err := elf.NewFile(bytes.NewReader(kernelData))
	if err != nil {
		return fmt.Errorf("multiboot2 kernel is not ELF: %w", err)
	}
	defer f.Close()

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
		for i := prog.Filesz; i < prog.Memsz; i++ { // .bss
			p.writePhys8(uint32(prog.Paddr+i), 0)
		}
	}

	// Install ACPI tables (RSDP in the F-segment + an RSDT/FADT/MADT). The MADT
	// carries a Processor Local-APIC entry, which the guest's ACPI scan needs to
	// enumerate its CPU — NuttX's x86_64_cpu_init PANICs if acpi_lapic_get fails.
	installACPIDirect(p)

	// Build the multiboot2 information structure at 0x9000: a u32 total_size +
	// u32 reserved, then 8-byte-aligned tags terminated by an end tag.
	const infoAddr uint32 = 0x9000
	off := infoAddr + 8
	align8 := func() { off = (off + 7) &^ 7 }

	// Basic memory info (type 4): mem_lower (KiB <1MiB), mem_upper (KiB).
	p.patchBootParam32(off+0, 4)
	p.patchBootParam32(off+4, 16)
	p.patchBootParam32(off+8, 640)
	p.patchBootParam32(off+12, uint32((uint64(p.ramSize)-0x100000)/1024))
	off += 16
	align8()

	// Memory map (type 6): entry_size=24, entry_version=0, then entries
	// [base_addr:u64][length:u64][type:u32][reserved:u32]. type 1 = available.
	mmapTag := off
	p.patchBootParam32(off+0, 6)
	p.patchBootParam32(off+8, 24)
	p.patchBootParam32(off+12, 0)
	e := off + 16
	addEntry := func(base, length uint64, typ uint32) {
		p.patchBootParam64(e+0, base)
		p.patchBootParam64(e+8, length)
		p.patchBootParam32(e+16, typ)
		p.patchBootParam32(e+20, 0)
		e += 24
	}
	addEntry(0, 0x9FC00, 1)
	addEntry(0x100000, uint64(p.ramSize)-0x100000, 1)
	p.patchBootParam32(mmapTag+4, e-mmapTag) // tag size
	off = e
	align8()

	// End tag (type 0, size 8).
	p.patchBootParam32(off+0, 0)
	p.patchBootParam32(off+4, 8)
	off += 8

	p.patchBootParam32(infoAddr+0, off-infoAddr) // total_size
	p.patchBootParam32(infoAddr+4, 0)            // reserved

	_ = cmdLine    // NuttX qemu-intel64 nsh takes no boot args
	_ = initrdData // no multiboot modules wired yet

	// GDT at 0x80000 — flat 32-bit code/data, DPL=0 (identical to PVH).
	const gdtAddr uint64 = 0x80000
	p.memMap.Write64(gdtAddr+0x00, 0x0)
	p.memMap.Write64(gdtAddr+0x08, 0x00CF9A000000FFFF) // CS: G=1 D=1 type=A
	p.memMap.Write64(gdtAddr+0x10, 0x00CF92000000FFFF) // DS: G=1 D=1 type=2

	cpu64.SetCR(0, x86_64.CR0_ET|x86_64.CR0_NE|x86_64.CR0_PE)
	cpu64.SetCR(4, 0)
	cpu64.SetCR64(3, 0)
	cpu64.SetEFER(0)

	cpu64.SetSegBase64(x86_64.GDTR, gdtAddr)
	cpu64.SetSegLimit(x86_64.GDTR, 0x17)

	cpu64.SetSeg(x86_64.CS, 0x08)
	cpu64.SetSegBase64(x86_64.CS, 0)
	cpu64.SetSegLimit(x86_64.CS, 0xFFFFFFFF)
	cpu64.SetSegAccess(x86_64.CS, 0xC9A) // G=1 D=1 L=0; code RX

	for _, s := range []int{x86_64.DS, x86_64.ES, x86_64.SS, x86_64.FS, x86_64.GS} {
		cpu64.SetSeg(s, 0x10)
		cpu64.SetSegBase64(s, 0)
		cpu64.SetSegLimit(s, 0xFFFFFFFF)
		cpu64.SetSegAccess(s, 0xC92)
	}

	// Multiboot2: EAX = 0x36D76289, EBX = info; enter at the ELF entry.
	cpu64.SetReg64(x86_64.RAX, multiboot2BootMagic)
	cpu64.SetReg64(x86_64.RBX, uint64(infoAddr))
	cpu64.SetReg64(x86_64.RSP, 0x90000)
	cpu64.SetRIP(f.Entry)
	return nil
}
