package pc

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
)

// loadBZImage64 loads a 64-bit Linux bzImage and sets the CPU up to
// jump directly to the kernel's long-mode entry point. This bypasses
// the real-mode setup stub at the front of the bzImage (our
// cpu/x86_64 decoder does not model real-mode opcodes) and instead
// follows the "64-bit boot protocol" laid out in
// Documentation/x86/boot.rst:
//
//   - Bootloader builds an identity-mapped paging hierarchy covering
//     at least the kernel image, boot_params, and cmdline.
//   - CPU enters long mode (CR0.PG | CR0.PE, CR4.PAE, EFER.LME|LMA).
//   - CS = 64-bit code segment (L=1, base 0, limit 4 GiB).
//   - DS/ES/SS = data segment (base 0, limit 4 GiB).
//   - GDT loaded; IDT can be null.
//   - RSI = boot_params, RIP = kernel start + 0x200, RFLAGS = 0x2.
//
// The startup_64 entry at +0x200 takes over from there: it sets up
// its own paging, switches to a kernel stack, and dives into C.
func (p *PC) loadBZImage64(kernelData, initrdData []byte, cmdLine string) error {
	cpu64, ok := p.cpu.(*x86_64.CPU)
	if !ok {
		return fmt.Errorf("loadBZImage64 requires *x86_64.CPU; have %T", p.cpu)
	}

	h, err := parseBZImageHeader(kernelData)
	if err != nil {
		return fmt.Errorf("parse bzImage header: %w", err)
	}

	setupBytes := setupSectsToBytes(h.SetupSects)
	if len(kernelData) < setupBytes {
		return fmt.Errorf("kernel data shorter than setup area (%d < %d)", len(kernelData), setupBytes)
	}

	// boot_params zero page at 0x90000 — same layout as the i386 path.
	const setupAddr uint32 = 0x90000
	for i := uint32(0); i < 0x1000; i++ {
		p.writePhys8(setupAddr+i, 0)
	}
	const hdrStart = 0x1F1
	const hdrEnd = 0x268
	for i := hdrStart; i < hdrEnd && i < len(kernelData); i++ {
		p.writePhys8(setupAddr+uint32(i), kernelData[i])
	}

	// Load the protected-mode (and thus the embedded 64-bit) kernel
	// to 0x100000.
	const kernelAddr uint32 = 0x100000
	for i := setupBytes; i < len(kernelData); i++ {
		p.writePhys8(kernelAddr+uint32(i-setupBytes), kernelData[i])
	}

	// Command line.
	const cmdLineAddr uint32 = 0x99000
	cmdLineBuf := append([]byte(cmdLine), 0)
	for i, b := range cmdLineBuf {
		p.writePhys8(cmdLineAddr+uint32(i), b)
	}

	// Patch boot_params header.
	p.patchBootParam(setupAddr+0x210, 0xFF)             // type_of_loader = 0xFF (generic)
	p.patchBootParam(setupAddr+0x211, h.LoadFlags|0x01) // LOADED_HIGH
	p.patchBootParam32(setupAddr+0x228, cmdLineAddr)    // cmd_line_ptr

	// E820 memory map (RAM available to the kernel).
	p.writeE820Map(setupAddr)

	// initrd: place just below top of RAM (page-aligned). The 64-bit
	// boot protocol allows initrd above 4 GiB if XLF_CAN_BE_LOADED_ABOVE_4G
	// is set; we stay below to keep the addr32 fields valid.
	if len(initrdData) > 0 {
		initrdAddr := uint32(p.ramSize) - uint32(len(initrdData))
		initrdAddr &^= 0xFFF
		if initrdAddr < 0x100000 {
			return fmt.Errorf("initrd too large for available RAM")
		}
		for i, b := range initrdData {
			p.writePhys8(initrdAddr+uint32(i), b)
		}
		p.patchBootParam32(setupAddr+0x218, initrdAddr)
		p.patchBootParam32(setupAddr+0x21C, uint32(len(initrdData)))
	}

	// Build identity-mapped page tables covering the first 4 GiB.
	// Use 1 GiB pages at the PDPT level — four entries, one PML4
	// entry, total cost 8 KiB.
	const pml4Addr uint64 = 0x70000
	const pdptAddr uint64 = 0x71000
	// PML4[0] -> PDPT, P|RW
	p.memMap.Write64(pml4Addr, pdptAddr|0x3)
	for gb := uint64(0); gb < 4; gb++ {
		// 1 GiB huge leaf: addr = gb * 1 GiB; flags P|RW|PS
		entry := (gb << 30) | 0x83
		p.memMap.Write64(pdptAddr+gb*8, entry)
	}

	// Build a minimal GDT so any CS reload the kernel performs sees
	// valid descriptors. Layout: null, kernel CS (64-bit code), kernel
	// SS (data), user CS (64-bit code), user SS. Selectors 0x08, 0x10,
	// 0x18, 0x20.
	const gdtAddr uint64 = 0x80000
	p.memMap.Write64(gdtAddr+0x00, 0x0)                // null
	p.memMap.Write64(gdtAddr+0x08, 0x00AF9A000000FFFF) // kernel CS: P=1, S=1, code, exec/read, L=1, G=1, lim=0xFFFFF
	p.memMap.Write64(gdtAddr+0x10, 0x00CF92000000FFFF) // kernel SS: P=1, S=1, data, R/W, D=1, G=1
	p.memMap.Write64(gdtAddr+0x18, 0x00AFFA000000FFFF) // user CS: same as kernel CS but DPL=3
	p.memMap.Write64(gdtAddr+0x20, 0x00CFF2000000FFFF) // user SS: same as kernel SS but DPL=3

	// Program the CPU into long mode.
	cpu64.SetCR(0, 0)                              // start fresh (clears default CR0_ET; we re-OR below)
	cpu64.SetCR(0, x86_64.CR0_ET|x86_64.CR0_NE)    // bare protected-mode prerequisites
	cpu64.SetCR(4, x86_64.CR4_PAE)                 // long mode requires PAE
	cpu64.SetCR64(3, pml4Addr)                     // page-table root
	cpu64.SetEFER(x86_64.EFER_LME | x86_64.EFER_NXE)
	cpu64.SetCR(0, x86_64.CR0_ET|x86_64.CR0_NE|x86_64.CR0_PE|x86_64.CR0_PG)
	// SetCR with PE+PG should have latched LMA via writeCR; verify.
	if cpu64.GetEFER()&x86_64.EFER_LMA == 0 {
		return fmt.Errorf("LMA failed to latch after enabling paging")
	}

	// Load GDTR, leave IDTR null (kernel installs its own).
	cpu64.SetSegBase64(x86_64.GDTR, gdtAddr)
	cpu64.SetSegLimit(x86_64.GDTR, 0x2F) // 6 descriptors × 8 bytes - 1

	// Install CS/SS/DS/ES/FS/GS with the synthesized 64-bit-flat
	// descriptor cache (we skip the GDT walk since we just built it).
	const kernelCS uint16 = 0x08
	const kernelDS uint16 = 0x10
	cpu64.SetSeg(x86_64.CS, kernelCS)
	cpu64.SetSegBase64(x86_64.CS, 0)
	cpu64.SetSegLimit(x86_64.CS, 0xFFFFFFFF)
	cpu64.SetSegAccess(x86_64.CS, 0xA9A) // P=1, code, executable/readable, L=1, G=1
	for _, s := range []int{x86_64.DS, x86_64.ES, x86_64.SS, x86_64.FS, x86_64.GS} {
		cpu64.SetSeg(s, kernelDS)
		cpu64.SetSegBase64(s, 0)
		cpu64.SetSegLimit(s, 0xFFFFFFFF)
		cpu64.SetSegAccess(s, 0xC92) // P=1, data, writable, G=1, D=1
	}
	// recomputeMode is not exported across package boundaries; the
	// SetSegAccess on CS combined with EFER.LMA already in place means
	// the next instruction fetch goes through the long-mode lip()
	// path because we set CR0.PG via SetCR which goes through writeCR
	// — which DID call recomputeMode internally.

	// Per the 64-bit boot protocol: RSI = boot_params, RIP = entry+0x200.
	entry := uint64(kernelAddr) + 0x200
	cpu64.SetReg64(x86_64.RSI, uint64(setupAddr))
	cpu64.SetReg64(x86_64.RSP, uint64(setupAddr-8))
	cpu64.SetRIP(entry)
	cpu64.SetRFLAGS(2)
	return nil
}
