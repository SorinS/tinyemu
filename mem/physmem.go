// Package mem provides physical memory mapping and device I/O dispatch
// for the TinyEMU RISC-V emulator.
package mem

import (
	"encoding/binary"
	"errors"
)

// Memory subsystem constants
const (
	MaxRanges        = 32
	PageSizeLog2     = 12
	PageSize         = 1 << PageSizeLog2
	DirtyBitsPerWord = 32
)

// RAM flags for memory regions
const (
	RAMFlagROM       = 1 << 0 // Region is read-only
	RAMFlagDirtyBits = 1 << 1 // Track dirty pages
	RAMFlagDisabled  = 1 << 2 // Allocated but not mapped
)

// Device I/O flags
const (
	DevIOSize8    = 1 << 0
	DevIOSize16   = 1 << 1
	DevIOSize32   = 1 << 2
	DevIOSize64   = 1 << 3
	DevIODisabled = 1 << 4
)

// Error definitions
var (
	ErrTooManyRanges     = errors.New("too many memory ranges")
	ErrInvalidSize       = errors.New("invalid size (must be page-aligned and non-zero)")
	ErrInvalidAddress    = errors.New("invalid address")
	ErrNoMemoryRange     = errors.New("no memory range at address")
	ErrReadOnly          = errors.New("write to read-only memory")
	ErrUnsupportedSize   = errors.New("unsupported access size for device")
	ErrInvalidAccessSize = errors.New("invalid access size (must be 1, 2, 4, or 8 bytes)")
)

// DeviceReadFunc is called when a device region is read.
// offset is the byte offset within the device region.
// sizeLog2 is log2 of the access size (0=1 byte, 1=2 bytes, 2=4 bytes, 3=8 bytes).
type DeviceReadFunc func(opaque any, offset uint32, sizeLog2 int) uint32

// DeviceWriteFunc is called when a device region is written.
// offset is the byte offset within the device region.
// val is the value to write.
// sizeLog2 is log2 of the access size (0=1 byte, 1=2 bytes, 2=4 bytes, 3=8 bytes).
type DeviceWriteFunc func(opaque any, offset uint32, val uint32, sizeLog2 int)

// TLBFlushFunc is called when TLB entries need to be invalidated.
// This is typically used when dirty bits are cleared or memory mappings change.
type TLBFlushFunc func(ramAddr []byte, ramSize uint64)

// PhysMemoryRange represents a single memory region (RAM or device I/O).
type PhysMemoryRange struct {
	Map     *PhysMemoryMap
	Addr    uint64 // Base physical address
	OrgSize uint64 // Original size (before disable)
	Size    uint64 // Current size (0 if disabled)
	IsRAM   bool   // True for RAM, false for device I/O

	// RAM-specific fields
	RAMFlags   int
	PhysMem    []byte   // Backing memory for RAM regions
	DirtyBits  []uint32 // Current dirty bits bitmap
	dirtyBits0 []uint32 // Double buffer 0
	dirtyBits1 []uint32 // Double buffer 1
	dirtyIndex int      // Current dirty buffer index (0 or 1)

	// Device I/O fields
	Opaque     any
	ReadFunc   DeviceReadFunc
	WriteFunc  DeviceWriteFunc
	DevIOFlags int
}

// PhysMemoryMap manages the physical address space mapping.
type PhysMemoryMap struct {
	ranges       []PhysMemoryRange
	opaque       any
	flushTLBFunc TLBFlushFunc
	// lastIdx is the index of the last range that satisfied GetRange.
	// Memory accesses cluster — instruction fetch within a page, stack
	// pushes near ESP — so checking the most-recent hit first turns a
	// linear scan into one compare on the hit path. Stored as an index
	// (not a *PhysMemoryRange) so the cache survives any future
	// append() that reallocates the backing array.
	lastIdx int
}

// NewPhysMemoryMap creates a new physical memory map.
// Reference: iomem.c:41-50 (phys_mem_map_init)
func NewPhysMemoryMap() *PhysMemoryMap {
	return &PhysMemoryMap{
		ranges: make([]PhysMemoryRange, 0, MaxRanges),
	}
}

// SetTLBFlushFunc sets the callback for TLB invalidation.
func (m *PhysMemoryMap) SetTLBFlushFunc(opaque any, fn TLBFlushFunc) {
	m.opaque = opaque
	m.flushTLBFunc = fn
}

// Close frees all allocated memory regions.
// Reference: iomem.c:52-64 (phys_mem_map_end)
func (m *PhysMemoryMap) Close() {
	// In Go, we don't need to explicitly free memory, but we clear references
	for i := range m.ranges {
		m.ranges[i].PhysMem = nil
		m.ranges[i].DirtyBits = nil
		m.ranges[i].dirtyBits0 = nil
		m.ranges[i].dirtyBits1 = nil
	}
	m.ranges = nil
}

// GetRange returns the memory range containing the given physical address.
// Returns nil if no range contains the address.
// Reference: iomem.c:68-78 (get_phys_mem_range)
//
// Fast path: most accesses hit the same range as the previous one (the
// RAM range dominates), so check m.lastIdx first.
func (m *PhysMemoryMap) GetRange(paddr uint64) *PhysMemoryRange {
	if m.lastIdx < len(m.ranges) {
		pr := &m.ranges[m.lastIdx]
		if pr.Size > 0 && paddr >= pr.Addr && paddr < pr.Addr+pr.Size {
			return pr
		}
	}
	for i := range m.ranges {
		pr := &m.ranges[i]
		if pr.Size > 0 && paddr >= pr.Addr && paddr < pr.Addr+pr.Size {
			m.lastIdx = i
			return pr
		}
	}
	return nil
}

// RegisterRAM allocates and registers a RAM region at the given address.
// Reference: iomem.c:80-127 (register_ram_entry + default_register_ram)
func (m *PhysMemoryMap) RegisterRAM(addr, size uint64, flags int) (*PhysMemoryRange, error) {
	if len(m.ranges) >= MaxRanges {
		return nil, ErrTooManyRanges
	}
	if size == 0 || (size&(PageSize-1)) != 0 {
		return nil, ErrInvalidSize
	}

	pr := PhysMemoryRange{
		Map:      m,
		Addr:     addr,
		OrgSize:  size,
		IsRAM:    true,
		RAMFlags: flags &^ RAMFlagDisabled,
	}

	if flags&RAMFlagDisabled != 0 {
		pr.Size = 0
	} else {
		pr.Size = size
	}

	// Allocate backing memory
	pr.PhysMem = make([]byte, size)

	// Allocate dirty bits if requested
	if flags&RAMFlagDirtyBits != 0 {
		numPages := size >> PageSizeLog2
		numWords := (numPages + DirtyBitsPerWord - 1) / DirtyBitsPerWord
		pr.dirtyBits0 = make([]uint32, numWords)
		pr.dirtyBits1 = make([]uint32, numWords)
		pr.DirtyBits = pr.dirtyBits0
		pr.dirtyIndex = 0
	}

	m.ranges = append(m.ranges, pr)
	return &m.ranges[len(m.ranges)-1], nil
}

// RegisterDevice registers a memory-mapped I/O device region.
// Reference: iomem.c:184-206 (cpu_register_device)
func (m *PhysMemoryMap) RegisterDevice(addr, size uint64, opaque any,
	readFunc DeviceReadFunc, writeFunc DeviceWriteFunc, flags int) (*PhysMemoryRange, error) {
	if len(m.ranges) >= MaxRanges {
		return nil, ErrTooManyRanges
	}
	if size == 0 || size > 0xFFFFFFFF {
		return nil, ErrInvalidSize
	}

	pr := PhysMemoryRange{
		Map:        m,
		Addr:       addr,
		OrgSize:    size,
		IsRAM:      false,
		Opaque:     opaque,
		ReadFunc:   readFunc,
		WriteFunc:  writeFunc,
		DevIOFlags: flags,
	}

	if flags&DevIODisabled != 0 {
		pr.Size = 0
	} else {
		pr.Size = size
	}

	m.ranges = append(m.ranges, pr)
	return &m.ranges[len(m.ranges)-1], nil
}

// SetDirtyBit marks a page as dirty within a RAM region.
// Reference: iomem.h:105-114 (phys_mem_set_dirty_bit inline)
func (pr *PhysMemoryRange) SetDirtyBit(offset uint64) {
	if pr.DirtyBits == nil {
		return
	}
	pageIndex := offset >> PageSizeLog2
	wordIndex := pageIndex >> 5
	bitIndex := pageIndex & 0x1F
	pr.DirtyBits[wordIndex] |= 1 << bitIndex
}

// IsDirtyBit checks if a page is dirty within a RAM region.
// Reference: iomem.h:117-126 (phys_mem_is_dirty_bit inline)
func (pr *PhysMemoryRange) IsDirtyBit(offset uint64) bool {
	if pr.DirtyBits == nil {
		return true // No dirty tracking means always considered dirty
	}
	pageIndex := offset >> PageSizeLog2
	wordIndex := pageIndex >> 5
	bitIndex := pageIndex & 0x1F
	return (pr.DirtyBits[wordIndex]>>bitIndex)&1 != 0
}

// ResetDirtyBit clears the dirty bit for a specific page and flushes TLB if needed.
// Reference: iomem.c:159-177 (phys_mem_reset_dirty_bit)
func (pr *PhysMemoryRange) ResetDirtyBit(offset uint64) {
	if pr.DirtyBits == nil {
		return
	}
	pageIndex := offset >> PageSizeLog2
	wordIndex := pageIndex >> 5
	bitIndex := pageIndex & 0x1F
	mask := uint32(1 << bitIndex)

	if pr.DirtyBits[wordIndex]&mask != 0 {
		pr.DirtyBits[wordIndex] &^= mask
		// Invalidate TLB for this page
		if pr.Map.flushTLBFunc != nil {
			pageStart := offset &^ (PageSize - 1)
			pr.Map.flushTLBFunc(pr.PhysMem[pageStart:pageStart+PageSize], PageSize)
		}
	}
}

// GetDirtyBits returns the current dirty bits and resets them.
// The returned slice is valid until the next call to GetDirtyBits.
// Reference: iomem.c:130-156 (default_get_dirty_bits)
func (pr *PhysMemoryRange) GetDirtyBits() []uint32 {
	if pr.DirtyBits == nil {
		return nil
	}

	// Check if any bits are dirty
	hasDirty := false
	for _, word := range pr.DirtyBits {
		if word != 0 {
			hasDirty = true
			break
		}
	}

	// If dirty and enabled, flush TLB
	if hasDirty && pr.Size != 0 && pr.Map.flushTLBFunc != nil {
		pr.Map.flushTLBFunc(pr.PhysMem, pr.OrgSize)
	}

	// Swap buffers
	result := pr.DirtyBits
	pr.dirtyIndex ^= 1
	if pr.dirtyIndex == 0 {
		pr.DirtyBits = pr.dirtyBits0
	} else {
		pr.DirtyBits = pr.dirtyBits1
	}

	// Clear new current buffer
	clear(pr.DirtyBits)

	return result
}

// SetAddr changes the address of a memory range or enables/disables it.
// Reference: iomem.c:208-242 (default_set_addr + phys_mem_set_addr)
func (pr *PhysMemoryRange) SetAddr(addr uint64, enabled bool) {
	if enabled {
		if pr.Size == 0 || pr.Addr != addr {
			// Enable or move mapping - flush TLB
			if pr.IsRAM && pr.Map.flushTLBFunc != nil {
				pr.Map.flushTLBFunc(pr.PhysMem, pr.OrgSize)
			}
			pr.Addr = addr
			pr.Size = pr.OrgSize
		}
	} else {
		if pr.Size != 0 {
			// Disable mapping - flush TLB
			if pr.IsRAM && pr.Map.flushTLBFunc != nil {
				pr.Map.flushTLBFunc(pr.PhysMem, pr.OrgSize)
			}
			pr.Addr = 0
			pr.Size = 0
		}
	}
}

// GetRAMPtr returns a pointer to RAM at the given physical address.
// Returns nil if the address is not in a RAM region.
// If isWrite is true, the dirty bit is set.
// Reference: iomem.c:245-255 (phys_mem_get_ram_ptr)
func (m *PhysMemoryMap) GetRAMPtr(paddr uint64, isWrite bool) []byte {
	pr := m.GetRange(paddr)
	if pr == nil || !pr.IsRAM {
		return nil
	}
	offset := paddr - pr.Addr
	if isWrite {
		pr.SetDirtyBit(offset)
	}
	return pr.PhysMem[offset:]
}

// Read8 reads a byte from physical memory.
// Reads from unmapped addresses return 0 (matches C behavior).
// Reference: riscv_cpu.c:376-382
func (m *PhysMemoryMap) Read8(paddr uint64) (uint8, error) {
	pr := m.GetRange(paddr)
	if pr == nil {
		return 0, nil // Return 0 for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		return pr.PhysMem[offset], nil
	}

	// Device I/O
	if pr.DevIOFlags&DevIOSize8 == 0 {
		return 0, ErrUnsupportedSize
	}
	return uint8(pr.ReadFunc(pr.Opaque, uint32(offset), 0)), nil
}

// Read16 reads a 16-bit value from physical memory (little-endian).
// Reads from unmapped addresses return 0 (matches C behavior).
// Reference: riscv_cpu.c:376-382
func (m *PhysMemoryMap) Read16(paddr uint64) (uint16, error) {
	pr := m.GetRange(paddr)
	if pr == nil {
		return 0, nil // Return 0 for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		return binary.LittleEndian.Uint16(pr.PhysMem[offset:]), nil
	}

	// Device I/O
	if pr.DevIOFlags&DevIOSize16 == 0 {
		return 0, ErrUnsupportedSize
	}
	return uint16(pr.ReadFunc(pr.Opaque, uint32(offset), 1)), nil
}

// DebugDeviceAccess enables logging of device I/O accesses
var DebugDeviceAccess bool

// Read32 reads a 32-bit value from physical memory (little-endian).
// Reads from unmapped addresses return 0 (matches C behavior).
// Reference: riscv_cpu.c:376-382
func (m *PhysMemoryMap) Read32(paddr uint64) (uint32, error) {
	pr := m.GetRange(paddr)
	if pr == nil {
		return 0, nil // Return 0 for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		return binary.LittleEndian.Uint32(pr.PhysMem[offset:]), nil
	}

	// Device I/O
	if pr.DevIOFlags&DevIOSize32 == 0 {
		return 0, ErrUnsupportedSize
	}
	return pr.ReadFunc(pr.Opaque, uint32(offset), 2), nil
}

// Read64 reads a 64-bit value from physical memory (little-endian).
// Reads from unmapped addresses return 0 (matches C behavior).
// Reference: riscv_cpu.c:376-382
func (m *PhysMemoryMap) Read64(paddr uint64) (uint64, error) {
	pr := m.GetRange(paddr)
	if pr == nil {
		return 0, nil // Return 0 for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		return binary.LittleEndian.Uint64(pr.PhysMem[offset:]), nil
	}

	// Device I/O - read as two 32-bit values
	if pr.DevIOFlags&DevIOSize32 == 0 {
		return 0, ErrUnsupportedSize
	}
	lo := pr.ReadFunc(pr.Opaque, uint32(offset), 2)
	hi := pr.ReadFunc(pr.Opaque, uint32(offset+4), 2)
	return uint64(lo) | (uint64(hi) << 32), nil
}

// Write8 writes a byte to physical memory.
// Writes to unmapped addresses are silently ignored (matches C behavior).
// Reference: riscv_cpu.c:462-468
func (m *PhysMemoryMap) Write8(paddr uint64, val uint8) error {
	pr := m.GetRange(paddr)
	if pr == nil {
		return nil // Silent ignore for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		if pr.RAMFlags&RAMFlagROM != 0 {
			return ErrReadOnly
		}
		pr.SetDirtyBit(offset)
		pr.PhysMem[offset] = val
		return nil
	}

	// Device I/O
	if pr.DevIOFlags&DevIOSize8 == 0 {
		return ErrUnsupportedSize
	}
	pr.WriteFunc(pr.Opaque, uint32(offset), uint32(val), 0)
	return nil
}

// Write16 writes a 16-bit value to physical memory (little-endian).
// Writes to unmapped addresses are silently ignored (matches C behavior).
// Reference: riscv_cpu.c:462-468
func (m *PhysMemoryMap) Write16(paddr uint64, val uint16) error {
	pr := m.GetRange(paddr)
	if pr == nil {
		return nil // Silent ignore for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		if pr.RAMFlags&RAMFlagROM != 0 {
			return ErrReadOnly
		}
		pr.SetDirtyBit(offset)
		binary.LittleEndian.PutUint16(pr.PhysMem[offset:], val)
		return nil
	}

	// Device I/O
	if pr.DevIOFlags&DevIOSize16 == 0 {
		return ErrUnsupportedSize
	}
	pr.WriteFunc(pr.Opaque, uint32(offset), uint32(val), 1)
	return nil
}

// Write32 writes a 32-bit value to physical memory (little-endian).
// Writes to unmapped addresses are silently ignored (matches C behavior).
// Reference: riscv_cpu.c:462-468
func (m *PhysMemoryMap) Write32(paddr uint64, val uint32) error {
	pr := m.GetRange(paddr)
	if pr == nil {
		return nil // Silent ignore for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		if pr.RAMFlags&RAMFlagROM != 0 {
			return ErrReadOnly
		}
		pr.SetDirtyBit(offset)
		binary.LittleEndian.PutUint32(pr.PhysMem[offset:], val)
		return nil
	}

	// Device I/O
	if pr.DevIOFlags&DevIOSize32 == 0 {
		return ErrUnsupportedSize
	}
	pr.WriteFunc(pr.Opaque, uint32(offset), val, 2)
	return nil
}

// Write64 writes a 64-bit value to physical memory (little-endian).
// Writes to unmapped addresses are silently ignored (matches C behavior).
// Reference: riscv_cpu.c:462-468
func (m *PhysMemoryMap) Write64(paddr uint64, val uint64) error {
	pr := m.GetRange(paddr)
	if pr == nil {
		return nil // Silent ignore for unmapped address (C behavior)
	}
	offset := paddr - pr.Addr

	if pr.IsRAM {
		if pr.RAMFlags&RAMFlagROM != 0 {
			return ErrReadOnly
		}
		pr.SetDirtyBit(offset)
		binary.LittleEndian.PutUint64(pr.PhysMem[offset:], val)
		return nil
	}

	// Device I/O - write as two 32-bit values
	if pr.DevIOFlags&DevIOSize32 == 0 {
		return ErrUnsupportedSize
	}
	pr.WriteFunc(pr.Opaque, uint32(offset), uint32(val), 2)
	pr.WriteFunc(pr.Opaque, uint32(offset+4), uint32(val>>32), 2)
	return nil
}

// Read performs a sized read from physical memory.
// size must be 1, 2, 4, or 8 bytes.
func (m *PhysMemoryMap) Read(paddr uint64, size int) (uint64, error) {
	switch size {
	case 1:
		v, err := m.Read8(paddr)
		return uint64(v), err
	case 2:
		v, err := m.Read16(paddr)
		return uint64(v), err
	case 4:
		v, err := m.Read32(paddr)
		return uint64(v), err
	case 8:
		return m.Read64(paddr)
	default:
		return 0, ErrInvalidAccessSize
	}
}

// Write performs a sized write to physical memory.
// size must be 1, 2, 4, or 8 bytes.
func (m *PhysMemoryMap) Write(paddr uint64, val uint64, size int) error {
	switch size {
	case 1:
		return m.Write8(paddr, uint8(val))
	case 2:
		return m.Write16(paddr, uint16(val))
	case 4:
		return m.Write32(paddr, uint32(val))
	case 8:
		return m.Write64(paddr, val)
	default:
		return ErrInvalidAccessSize
	}
}

// NumRanges returns the number of registered memory ranges.
func (m *PhysMemoryMap) NumRanges() int {
	return len(m.ranges)
}

// RangeAt returns the memory range at the given index.
func (m *PhysMemoryMap) RangeAt(index int) *PhysMemoryRange {
	if index < 0 || index >= len(m.ranges) {
		return nil
	}
	return &m.ranges[index]
}
