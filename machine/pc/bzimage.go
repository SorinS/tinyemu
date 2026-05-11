// Package pc implements an x86 PC machine/board emulator.
package pc

import (
	"encoding/binary"
	"fmt"

	"github.com/jtolio/tinyemu-go/cpu/x86"
)

// Boot protocol constants from Linux Documentation/x86/boot.rst.
const (
	bootFlagMagic = 0xAA55
	headerMagic   = "HdrS"
)

// bzImageHeader represents the bzImage setup header fields we need.
type bzImageHeader struct {
	SetupSects      uint8
	RootFlags       uint16
	Syssize         uint32
	RamSize         uint16
	VidMode         uint16
	RootDev         uint16
	BootFlag        uint16
	Jump            uint16
	Header          uint32
	Version         uint16
	RealmodeSwtch   uint32
	StartSysSeg     uint16
	KernelVersion   uint16
	TypeOfLoader    uint8
	LoadFlags       uint8
	SetupMoveSize   uint16
	Code32Start     uint32
	RamdiskImage    uint32
	RamdiskSize     uint32
	BootsectKludge  uint32
	HeapEndPtr      uint16
	ExtLoaderVer    uint8
	ExtLoaderType   uint8
	CmdLinePtr      uint32
	InitrdAddrMax   uint32
	KernelAlignment uint32
	Relocatable     uint8
	MinAlignment    uint8
	Xloadflags      uint16
	CmdlineSize     uint32
	HardwareSubarch uint32
	HardwareSubarchData uint64
	PayloadOffset   uint32
	PayloadLength   uint32
	SetupData       uint64
	PrefAddress     uint64
	InitSize        uint32
	HandoverOffset  uint32
}

// parseBZImageHeader parses the bzImage setup header from kernel data.
func parseBZImageHeader(data []byte) (*bzImageHeader, error) {
	if len(data) < 0x264+4 {
		return nil, fmt.Errorf("kernel data too short (%d bytes)", len(data))
	}

	h := &bzImageHeader{}

	// Boot sector fields (offset 0x1F1-0x1FF)
	h.SetupSects = data[0x1F1]
	h.RootFlags = binary.LittleEndian.Uint16(data[0x1F2:])
	h.Syssize = binary.LittleEndian.Uint32(data[0x1F4:])
	h.RamSize = binary.LittleEndian.Uint16(data[0x1F8:])
	h.VidMode = binary.LittleEndian.Uint16(data[0x1FA:])
	h.RootDev = binary.LittleEndian.Uint16(data[0x1FC:])
	h.BootFlag = binary.LittleEndian.Uint16(data[0x1FE:])

	if h.BootFlag != bootFlagMagic {
		return nil, fmt.Errorf("invalid boot flag: 0x%04X (expected 0x%04X)", h.BootFlag, bootFlagMagic)
	}

	// Setup header fields (offset 0x200+)
	h.Jump = binary.LittleEndian.Uint16(data[0x200:])
	h.Header = binary.LittleEndian.Uint32(data[0x202:])

	if string(data[0x202:0x206]) != headerMagic {
		return nil, fmt.Errorf("invalid header magic: %q (expected %q)", string(data[0x202:0x206]), headerMagic)
	}

	h.Version = binary.LittleEndian.Uint16(data[0x206:])
	h.RealmodeSwtch = binary.LittleEndian.Uint32(data[0x208:])
	h.StartSysSeg = binary.LittleEndian.Uint16(data[0x20C:])
	h.KernelVersion = binary.LittleEndian.Uint16(data[0x20E:])
	h.TypeOfLoader = data[0x210]
	h.LoadFlags = data[0x211]
	h.SetupMoveSize = binary.LittleEndian.Uint16(data[0x212:])
	h.Code32Start = binary.LittleEndian.Uint32(data[0x214:])
	h.RamdiskImage = binary.LittleEndian.Uint32(data[0x218:])
	h.RamdiskSize = binary.LittleEndian.Uint32(data[0x21C:])
	h.BootsectKludge = binary.LittleEndian.Uint32(data[0x220:])
	h.HeapEndPtr = binary.LittleEndian.Uint16(data[0x224:])
	h.ExtLoaderVer = data[0x226]
	h.ExtLoaderType = data[0x227]
	h.CmdLinePtr = binary.LittleEndian.Uint32(data[0x228:])
	h.InitrdAddrMax = binary.LittleEndian.Uint32(data[0x22C:])
	h.KernelAlignment = binary.LittleEndian.Uint32(data[0x230:])
	h.Relocatable = data[0x234]
	h.MinAlignment = data[0x235]
	h.Xloadflags = binary.LittleEndian.Uint16(data[0x236:])
	h.CmdlineSize = binary.LittleEndian.Uint32(data[0x238:])
	h.HardwareSubarch = binary.LittleEndian.Uint32(data[0x23C:])
	h.HardwareSubarchData = binary.LittleEndian.Uint64(data[0x240:])
	h.PayloadOffset = binary.LittleEndian.Uint32(data[0x248:])
	h.PayloadLength = binary.LittleEndian.Uint32(data[0x24C:])
	h.SetupData = binary.LittleEndian.Uint64(data[0x250:])
	h.PrefAddress = binary.LittleEndian.Uint64(data[0x258:])
	h.InitSize = binary.LittleEndian.Uint32(data[0x260:])
	h.HandoverOffset = binary.LittleEndian.Uint32(data[0x264:])

	return h, nil
}

// setupSectsToBytes returns the size of the setup area in bytes.
func setupSectsToBytes(setupSects uint8) int {
	// setup_sects = 0 means 4 setup sectors (backward compatibility)
	sectors := int(setupSects)
	if sectors == 0 {
		sectors = 4
	}
	// Total setup size = boot sector (1) + setup sectors
	return (sectors + 1) * 512
}

// loadBZImage loads a bzImage into memory and sets up the CPU for 32-bit entry.
// It returns the 32-bit entry point address.
func (p *PC) loadBZImage(kernelData, initrdData []byte, cmdLine string) (uint32, error) {
	h, err := parseBZImageHeader(kernelData)
	if err != nil {
		return 0, fmt.Errorf("failed to parse bzImage header: %w", err)
	}

	setupBytes := setupSectsToBytes(h.SetupSects)
	if len(kernelData) < setupBytes {
		return 0, fmt.Errorf("kernel data shorter than setup area (%d < %d)", len(kernelData), setupBytes)
	}

	// boot_params is a 4 KB "zero page" the bootloader must construct fresh.
	// The bzImage file's first 0x1F0 bytes are real-mode boot sector code +
	// PE/DOS header junk — copying them verbatim into boot_params clobbers
	// fields like alt_mem_k (= 0xFFFFFFFF), e820_entries (= 0xFF), and
	// sentinel (= 0xFF). A non-zero sentinel makes Linux's
	// `sanitize_boot_params()` wipe the very e820 fields we want the kernel
	// to read. So: zero the 4 KB page first, then overlay only the
	// `setup_header` slice (offsets 0x1F1..0x268) from the bzImage on top.
	setupAddr := uint32(0x90000)
	for i := uint32(0); i < 0x1000; i++ {
		p.writePhys8(setupAddr+i, 0)
	}
	const hdrStart = 0x1F1
	const hdrEnd = 0x268
	for i := hdrStart; i < hdrEnd && i < len(kernelData); i++ {
		p.writePhys8(setupAddr+uint32(i), kernelData[i])
	}

	// Load protected-mode kernel to 0x100000
	kernelStart := setupBytes
	kernelAddr := uint32(0x100000)
	for i := kernelStart; i < len(kernelData); i++ {
		p.writePhys8(kernelAddr+uint32(i-kernelStart), kernelData[i])
	}

	// Set up command line
	cmdLineAddr := uint32(0x99000) // Place cmdline in setup area vicinity
	cmdLineBuf := append([]byte(cmdLine), 0)
	for i, b := range cmdLineBuf {
		p.writePhys8(cmdLineAddr+uint32(i), b)
	}

	// Patch boot params in zero page
	p.patchBootParam(setupAddr+0x210, 0xFF)              // type_of_loader = 0xFF (generic bootloader)
	p.patchBootParam(setupAddr+0x211, h.LoadFlags|0x01)  // LOADED_HIGH | keep existing flags
	p.patchBootParam32(setupAddr+0x228, cmdLineAddr)     // cmd_line_ptr

	// Populate the E820 memory map. Without this the kernel falls back to
	// the legacy BIOS int15 alt_mem_k field (zero here) and assumes only
	// 16 MB of RAM is present. The kernel then proceeds to unmap every
	// kernel-direct-map PTE for memory it thinks doesn't exist — which
	// includes its own .text, leading to a triple-fault when it next
	// tries to fetch an instruction from the just-unmapped page.
	//
	// boot_params layout (Documentation/x86/boot.rst):
	//   0x1E8: u8  e820_entries
	//   0x2D0: e820_entry[E820_MAX_ENTRIES_ZEROPAGE] (20 bytes each:
	//          u64 addr; u64 size; u32 type)
	p.writeE820Map(setupAddr)

	// Set up initrd if provided
	if len(initrdData) > 0 {
		// Load initrd just below the top of RAM, aligned to page boundary
		initrdAddr := uint32(p.ramSize) - uint32(len(initrdData))
		initrdAddr &= ^uint32(0xFFF) // Page align
		if initrdAddr < 0x100000 {
			return 0, fmt.Errorf("initrd too large for available RAM")
		}
		for i, b := range initrdData {
			p.writePhys8(initrdAddr+uint32(i), b)
		}
		p.patchBootParam32(setupAddr+0x218, initrdAddr)   // ramdisk_image
		p.patchBootParam32(setupAddr+0x21C, uint32(len(initrdData))) // ramdisk_size
	}

	// Determine 32-bit entry point
	var entryPoint uint32
	if h.Code32Start != 0 {
		entryPoint = h.Code32Start
	} else {
		entryPoint = 0x100000
	}

	// Set up CPU state for 32-bit protected mode entry
	// The kernel expects flat segments
	p.cpu.SetCR(0, p.cpu.GetCR(0)|x86.CR0_PE) // Enable protected mode

	// Set up flat segments
	p.cpu.SetSeg(x86.CS, 0x0008)
	p.cpu.SetSegBase(x86.CS, 0x00000)
	p.cpu.SetSegLimit(x86.CS, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.CS, 0xC9A) // Code, present, DPL=0, 32-bit

	p.cpu.SetSeg(x86.DS, 0x0010)
	p.cpu.SetSegBase(x86.DS, 0x00000)
	p.cpu.SetSegLimit(x86.DS, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.DS, 0xC92) // Data, present, DPL=0, 32-bit

	p.cpu.SetSeg(x86.ES, 0x0010)
	p.cpu.SetSegBase(x86.ES, 0x00000)
	p.cpu.SetSegLimit(x86.ES, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.ES, 0xC92)

	p.cpu.SetSeg(x86.SS, 0x0010)
	p.cpu.SetSegBase(x86.SS, 0x00000)
	p.cpu.SetSegLimit(x86.SS, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.SS, 0xC92)

	p.cpu.SetSeg(x86.FS, 0x0010)
	p.cpu.SetSegBase(x86.FS, 0x00000)
	p.cpu.SetSegLimit(x86.FS, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.FS, 0xC92)

	p.cpu.SetSeg(x86.GS, 0x0010)
	p.cpu.SetSegBase(x86.GS, 0x00000)
	p.cpu.SetSegLimit(x86.GS, 0xFFFFFFFF)
	p.cpu.SetSegAccess(x86.GS, 0xC92)

	// Boot protocol registers
	p.cpu.SetReg32(x86.EAX, 0x53726448) // "HdrS"
	p.cpu.SetReg32(x86.EBX, setupAddr)  // Pointer to boot_params
	p.cpu.SetReg32(x86.ESP, setupAddr-4) // Stack just below setup area
	p.cpu.SetReg32(x86.EDI, setupAddr)  // Some bootloaders set EDI to boot_params
	p.cpu.SetReg32(x86.ESI, setupAddr)  // Some bootloaders set ESI to boot_params

	p.cpu.SetEIP(entryPoint)

	return entryPoint, nil
}

// writePhys8 writes a byte to physical memory.
func (p *PC) writePhys8(addr uint32, val uint8) {
	p.memMap.Write8(uint64(addr), val)
}

// patchBootParam patches a single byte in the zero page.
func (p *PC) patchBootParam(addr uint32, val uint8) {
	p.writePhys8(addr, val)
}

// patchBootParam32 patches a 32-bit little-endian value in the zero page.
func (p *PC) patchBootParam32(addr uint32, val uint32) {
	p.writePhys8(addr, uint8(val))
	p.writePhys8(addr+1, uint8(val>>8))
	p.writePhys8(addr+2, uint8(val>>16))
	p.writePhys8(addr+3, uint8(val>>24))
}

// patchBootParam64 patches a 64-bit little-endian value in the zero page.
func (p *PC) patchBootParam64(addr uint32, val uint64) {
	for i := uint32(0); i < 8; i++ {
		p.writePhys8(addr+i, uint8(val>>(i*8)))
	}
}

// writeE820Map fills in boot_params.e820_table with entries describing the
// emulator's RAM layout. Without this, the kernel assumes only the legacy
// 16 MB of RAM exists and unmaps the rest of its direct map, including its
// own .text — leading to a triple-fault.
//
// Entry layout for x86 (e820_entry, 20 bytes): u64 addr, u64 size, u32 type.
// type 1 = RAM, type 2 = RESERVED.
//
// We must ALSO zero the sentinel byte at offset 0x1EF: Linux's
// `sanitize_boot_params()` interprets any non-zero sentinel as "the
// bootloader corrupted my boot_params" and wipes out the very fields we
// just populated (alt_mem_k, e820_entries, e820_table, ...).
func (p *PC) writeE820Map(setupAddr uint32) {
	const (
		altMemKOff        = 0x1E0
		e820NumEntriesOff = 0x1E8
		sentinelOff       = 0x1EF
		e820TableOff      = 0x2D0
		entrySize         = 20
	)

	// Compute usable RAM above 1 MB and report it as alt_mem_k (kilobytes)
	// for the kernel's legacy fallback. (e820_table is preferred when
	// present.)
	memAbove1M := uint32(0)
	if p.ramSize > 0x100000 {
		memAbove1M = uint32((p.ramSize - 0x100000) / 1024)
	}
	p.patchBootParam32(setupAddr+altMemKOff, memAbove1M)

	type e820Entry struct {
		addr uint64
		size uint64
		typ  uint32
	}
	entries := []e820Entry{
		// Low memory: 0..640 KB usable RAM (standard PC layout).
		{addr: 0x00000000, size: 0x9F000, typ: 1},
		// 0xA0000..0x100000: reserved (BIOS/VGA region).
		{addr: 0x000A0000, size: 0x60000, typ: 2},
		// Extended memory: from 1 MB to end of RAM.
		{addr: 0x00100000, size: p.ramSize - 0x100000, typ: 1},
	}
	p.patchBootParam(setupAddr+e820NumEntriesOff, uint8(len(entries)))
	for i, e := range entries {
		base := setupAddr + e820TableOff + uint32(i)*entrySize
		p.patchBootParam64(base, e.addr)
		p.patchBootParam64(base+8, e.size)
		p.patchBootParam32(base+16, e.typ)
	}

	// Sentinel = 0 means "bootloader produced clean boot_params". Without
	// this the kernel calls sanitize_boot_params() which wipes out the
	// e820 fields we just wrote.
	p.patchBootParam(setupAddr+sentinelOff, 0)
}
