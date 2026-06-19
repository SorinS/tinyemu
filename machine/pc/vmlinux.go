package pc

import (
	"debug/elf"
	"fmt"
	"io"
	"strings"

	"github.com/sorins/tinyemu-go/cpu/x86"
)

// loadVMLinux loads a decompressed 32-bit x86 vmlinux ELF directly into memory.
// It sets up page tables, boot_params, and CPU state for direct kernel entry.
// The kernel is entered at its virtual entry point with paging enabled.
func (p *PC) loadVMLinux(kernelData, initrdData []byte, cmdLine string) (uint32, error) {
	f, err := elf.NewFile(strings.NewReader(string(kernelData)))
	if err != nil {
		return 0, fmt.Errorf("not a valid ELF: %w", err)
	}
	defer f.Close()

	if f.Class != elf.ELFCLASS32 {
		return 0, fmt.Errorf("unsupported ELF class: %v (need ELFCLASS32)", f.Class)
	}
	if f.Machine != elf.EM_386 {
		return 0, fmt.Errorf("unsupported machine: %v (need EM_386)", f.Machine)
	}
	if f.Type != elf.ET_EXEC {
		return 0, fmt.Errorf("unsupported ELF type: %v (need ET_EXEC)", f.Type)
	}

	entryPhys := uint32(f.Entry)
	entryVirt := uint32(f.Entry)

	// Load PT_LOAD segments into physical memory
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_LOAD {
			continue
		}
		paddr := uint32(prog.Paddr)
		filesz := uint32(prog.Filesz)
		memsz := uint32(prog.Memsz)

		// Determine virtual entry point: if Vaddr != Paddr, the kernel is
		// linked high. Use Vaddr as the entry point.
		if uint32(prog.Vaddr) != paddr {
			entryVirt = uint32(f.Entry) // ELF entry is physical; virtual is the same offset in high map
		}

		// Load file content
		data := make([]byte, filesz)
		n, err := prog.ReadAt(data, 0)
		if err != nil && err != io.EOF {
			return 0, fmt.Errorf("failed to read segment at paddr 0x%08X: %w", paddr, err)
		}
		if uint32(n) != filesz {
			return 0, fmt.Errorf("short read for segment at paddr 0x%08X: got %d, want %d", paddr, n, filesz)
		}

		p.writePhysBlock(paddr, data[:filesz])

		// Zero BSS (Memsz > Filesz)
		for i := filesz; i < memsz; i++ {
			p.writePhys8(paddr+i, 0)
		}
	}

	// If Vaddr != Paddr for any PT_LOAD segment, the kernel is high-mapped.
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_LOAD && uint32(prog.Vaddr) != uint32(prog.Paddr) {
			// Compute virtual entry: ELF entry is physical address in the file.
			// Virtual = Physical + (Vaddr - Paddr) for the first segment.
			entryVirt = uint32(f.Entry) + uint32(prog.Vaddr) - uint32(prog.Paddr)
			break
		}
	}

	// Set up boot_params at 0x90000
	bootParamsAddr := uint32(0x90000)
	p.setupBootParams(bootParamsAddr, cmdLine, initrdData)

	// Set up CPU state for direct vmlinux boot.
	// The kernel's startup_32 expects protected mode with paging OFF.
	// It will set up its own page tables and enable paging.
	p.cpu.SetCR(0, x86.CR0_PE)

	// Flat segments
	p.cpu.SetSeg(x86.CS, 0x0008)
	p.cpu.SetSegBase(x86.CS, 0)
	p.cpu.SetSegLimit(x86.CS, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.CS, 0xC9A)

	for _, seg := range []int{x86.DS, x86.ES, x86.SS, x86.FS, x86.GS} {
		p.cpu.SetSeg(seg, 0x0010)
		p.cpu.SetSegBase(seg, 0)
		p.cpu.SetSegLimit(seg, 0xFFFFFFFF)
		p.cpu.SetSegAccess(seg, 0xC92)
	}

	// Boot protocol registers
	p.cpu.SetReg32(x86.EAX, 0x53726448) // "HdrS"
	p.cpu.SetReg32(x86.EBX, bootParamsAddr)
	p.cpu.SetReg32(x86.ESP, bootParamsAddr-4)
	p.cpu.SetReg32(x86.ESI, bootParamsAddr)
	p.cpu.SetReg32(x86.EDI, bootParamsAddr)

	// Jump to the physical entry point. The kernel will enable paging itself.
	p.cpu.SetEIP(entryPhys)

	return entryVirt, nil
}

// setupPSEPageTables creates a page directory using 4MB pages (PSE).
// It identity-maps all of RAM and, if highMap is true,
// maps 0xC0000000 -> 0x00000000 (high kernel map).
func (p *PC) setupPSEPageTables(pageDirAddr uint32, highMap bool) error {
	const pdePresentRW = uint32(0x87) // Present, R/W, Supervisor, PS=1 (4MB page)
	const pageSize4MB = uint64(4 * 1024 * 1024)

	numPages := uint32((p.ramSize + pageSize4MB - 1) / pageSize4MB)
	if numPages > 1024 {
		numPages = 1024 // Max 4GB with 4MB pages
	}

	// Zero the page directory
	for i := uint32(0); i < 4096; i++ {
		p.writePhys8(pageDirAddr+i, 0)
	}

	// Identity map all RAM
	for i := uint32(0); i < numPages; i++ {
		addr := pageDirAddr + i*4
		val := (i << 22) | pdePresentRW
		p.writePhys32(addr, val)
	}

	if highMap {
		// Map 0xC0000000 -> 0x00000000 (PDE index 768 = 0xC0000000 >> 22)
		for i := uint32(0); i < numPages; i++ {
			addr := pageDirAddr + (768+i)*4
			val := (i << 22) | pdePresentRW
			p.writePhys32(addr, val)
		}
	}

	return nil
}

// writePhys32 writes a 32-bit little-endian value to physical memory.
func (p *PC) writePhys32(addr uint32, val uint32) {
	p.writePhys8(addr, uint8(val))
	p.writePhys8(addr+1, uint8(val>>8))
	p.writePhys8(addr+2, uint8(val>>16))
	p.writePhys8(addr+3, uint8(val>>24))
}

// readPhys32 reads a 32-bit little-endian value from physical memory.
func (p *PC) readPhys32(addr uint32) uint32 {
	v0, _ := p.memMap.Read8(uint64(addr))
	v1, _ := p.memMap.Read8(uint64(addr + 1))
	v2, _ := p.memMap.Read8(uint64(addr + 2))
	v3, _ := p.memMap.Read8(uint64(addr + 3))
	return uint32(v0) | uint32(v1)<<8 | uint32(v2)<<16 | uint32(v3)<<24
}

// setupBootParams constructs a minimal Linux boot_params structure.
func (p *PC) setupBootParams(addr uint32, cmdLine string, initrdData []byte) {
	// Zero the entire boot_params page
	for i := uint32(0); i < 4096; i++ {
		p.writePhys8(addr+i, 0)
	}

	// Setup header at offset 0x1F1
	p.writePhys8(addr+0x1F1, 0)               // setup_sects (0 = 4 sectors, doesn't matter for direct boot)
	p.writePhys16(addr+0x1F2, 0)              // root_flags
	p.writePhys32(addr+0x1F4, 0)              // syssize
	p.writePhys16(addr+0x1F8, 0)              // ram_size
	p.writePhys16(addr+0x1FA, 0xFFFF)         // vid_mode (0xFFFD = normal)
	p.writePhys16(addr+0x1FC, 0)              // root_dev
	p.writePhys16(addr+0x1FE, 0xAA55)         // boot_flag
	p.writePhys16(addr+0x200, 0xEBC)          // jump (JMP rel8 near)
	p.writePhys32(addr+0x202, 0x53726448)     // header "HdrS"
	p.writePhys16(addr+0x206, 0x020F)         // version 2.15
	p.writePhys32(addr+0x208, 0)              // realmode_swtch
	p.writePhys16(addr+0x20C, 0)              // start_sys_seg
	p.writePhys16(addr+0x20E, 0)              // kernel_version
	p.writePhys8(addr+0x210, 0xFF)            // type_of_loader = unknown
	p.writePhys8(addr+0x211, 0x01|0x20)       // loadflags: LOADED_HIGH | KEEP_SEGMENTS
	p.writePhys16(addr+0x212, 0)              // setup_move_size
	p.writePhys32(addr+0x214, 0x100000)       // code32_start
	p.writePhys32(addr+0x218, 0)              // ramdisk_image (patched below)
	p.writePhys32(addr+0x21C, 0)              // ramdisk_size
	p.writePhys32(addr+0x220, 0)              // bootsect_kludge
	p.writePhys16(addr+0x224, 0)              // heap_end_ptr
	p.writePhys8(addr+0x226, 0)               // ext_loader_ver
	p.writePhys8(addr+0x227, 0)               // ext_loader_type

	// Command line at 0x91000
	cmdLineAddr := uint32(0x91000)
	cmdLineBuf := append([]byte(cmdLine), 0)
	for i, b := range cmdLineBuf {
		p.writePhys8(cmdLineAddr+uint32(i), b)
	}
	p.writePhys32(addr+0x228, cmdLineAddr) // cmd_line_ptr
	p.writePhys32(addr+0x238, 2048)        // cmdline_size

	// Initrd
	if len(initrdData) > 0 {
		initrdAddr := uint32(p.ramSize) - uint32(len(initrdData))
		initrdAddr &^= 0xFFF // page align
		if initrdAddr < 0x100000 {
			// initrd too large, skip it
		} else {
			for i, b := range initrdData {
				p.writePhys8(initrdAddr+uint32(i), b)
			}
			p.writePhys32(addr+0x218, initrdAddr)
			p.writePhys32(addr+0x21C, uint32(len(initrdData)))
		}
	}

	// E820 memory map
	p.setupE820Map(addr)
}

// setupE820Map writes a minimal e820 memory map into boot_params.
func (p *PC) setupE820Map(bootParamsAddr uint32) {
	const e820MapOffset = 0x2D0 // e820_map offset in boot_params
	const e820EntrySize = 20    // 8 + 8 + 4 bytes

	ramSize := uint64(p.ramSize)
	// Guard the subtraction: for ramSize < 1 MB it would wrap to a
	// near-2^64 size and hand the kernel a bogus E820 map.
	extMem := uint64(0)
	if ramSize > 0x100000 {
		extMem = ramSize - 0x100000
	}
	entries := []struct {
		addr uint64
		size uint64
		typ  uint32
	}{
		{0x00000000, 0x000A0000, 1}, // Low RAM (640KB)
		{0x000A0000, 0x00060000, 2}, // Reserved: VGA + BIOS shadow
		{0x00100000, extMem, 1},     // Extended RAM
	}

	for i, e := range entries {
		off := bootParamsAddr + e820MapOffset + uint32(i*e820EntrySize)
		p.writePhys64(off, e.addr)
		p.writePhys64(off+8, e.size)
		p.writePhys32(off+16, e.typ)
	}

	p.writePhys8(bootParamsAddr+0x1E8, uint8(len(entries))) // e820_entries
}

func (p *PC) writePhys16(addr uint32, val uint16) {
	p.writePhys8(addr, uint8(val))
	p.writePhys8(addr+1, uint8(val>>8))
}

func (p *PC) writePhys64(addr uint32, val uint64) {
	for i := uint32(0); i < 8; i++ {
		p.writePhys8(addr+i, uint8(val>>(i*8)))
	}
}
