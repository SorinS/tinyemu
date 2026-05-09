// Package machine provides FDT (Flattened Device Tree) generation for the RISC-V machine.
//
// Reference: TinyEMU riscv_machine.c lines 321-751 (FDT generation)
package machine

import (
	"encoding/binary"
	"fmt"
)

// FDT constants
// Reference: riscv_machine.c lines 323-348
const (
	FDTMagic     = 0xd00dfeed
	FDTVersion   = 17
	FDTBeginNode = 1
	FDTEndNode   = 2
	FDTProp      = 3
	FDTNop       = 4
	FDTEnd       = 9
)

// fdtState holds state during FDT construction.
// Reference: riscv_machine.c lines 350-359 (FDTState)
type fdtState struct {
	tab           []uint32
	openNodeCount int
	stringTable   []byte
}

// newFDTState creates a new FDT builder.
func newFDTState() *fdtState {
	return &fdtState{
		tab:         make([]uint32, 0, 256),
		stringTable: make([]byte, 0, 256),
	}
}

// put32 appends a big-endian 32-bit value.
func (s *fdtState) put32(v uint32) {
	s.tab = append(s.tab, v)
}

// putData appends data with zero padding to 4-byte boundary.
func (s *fdtState) putData(data []byte) {
	len32 := (len(data) + 3) / 4
	startIdx := len(s.tab)
	for i := 0; i < len32; i++ {
		s.tab = append(s.tab, 0)
	}
	// Copy data
	dst := make([]byte, len32*4)
	copy(dst, data)
	for i := 0; i < len32; i++ {
		s.tab[startIdx+i] = binary.BigEndian.Uint32(dst[i*4:])
	}
}

// beginNode starts a new node with the given name.
func (s *fdtState) beginNode(name string) {
	s.put32(FDTBeginNode)
	s.putData(append([]byte(name), 0))
	s.openNodeCount++
}

// beginNodeNum starts a new node with name@address format.
func (s *fdtState) beginNodeNum(name string, addr uint64) {
	s.beginNode(fmt.Sprintf("%s@%x", name, addr))
}

// endNode ends the current node.
func (s *fdtState) endNode() {
	s.put32(FDTEndNode)
	s.openNodeCount--
}

// getStringOffset returns the offset of a string in the string table,
// adding it if not already present.
func (s *fdtState) getStringOffset(name string) uint32 {
	nameBytes := []byte(name)

	// Search for existing string
	pos := 0
	for pos < len(s.stringTable) {
		end := pos
		for end < len(s.stringTable) && s.stringTable[end] != 0 {
			end++
		}
		if end > pos {
			existing := s.stringTable[pos:end]
			if string(existing) == name {
				return uint32(pos)
			}
		}
		pos = end + 1
	}

	// Add new string
	offset := len(s.stringTable)
	s.stringTable = append(s.stringTable, nameBytes...)
	s.stringTable = append(s.stringTable, 0)
	return uint32(offset)
}

// prop adds a property with raw data.
func (s *fdtState) prop(name string, data []byte) {
	s.put32(FDTProp)
	s.put32(uint32(len(data)))
	s.put32(s.getStringOffset(name))
	s.putData(data)
}

// propU32 adds a u32 property.
func (s *fdtState) propU32(name string, val uint32) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, val)
	s.prop(name, data)
}

// propU64 adds a u64 property.
// Reference: tinyemu-2019-12-21/riscv_machine.c:465-472 (fdt_prop_tab_u64)
func (s *fdtState) propU64(name string, val uint64) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:], uint32(val>>32))
	binary.BigEndian.PutUint32(data[4:], uint32(val))
	s.prop(name, data)
}

// propU64x2 adds a property with two u64 values.
// Reference: tinyemu-2019-12-21/riscv_machine.c:474-483 (fdt_prop_tab_u64_2)
func (s *fdtState) propU64x2(name string, v0, v1 uint64) {
	data := make([]byte, 16)
	binary.BigEndian.PutUint32(data[0:], uint32(v0>>32))
	binary.BigEndian.PutUint32(data[4:], uint32(v0))
	binary.BigEndian.PutUint32(data[8:], uint32(v1>>32))
	binary.BigEndian.PutUint32(data[12:], uint32(v1))
	s.prop(name, data)
}

// propU32Tab adds a property with an array of u32 values.
func (s *fdtState) propU32Tab(name string, vals ...uint32) {
	data := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.BigEndian.PutUint32(data[i*4:], v)
	}
	s.prop(name, data)
}

// propStr adds a string property.
// Reference: tinyemu-2019-12-21/riscv_machine.c:485-489 (fdt_prop_str)
func (s *fdtState) propStr(name string, val string) {
	s.prop(name, append([]byte(val), 0))
}

// propStrTab adds a property with multiple null-terminated strings.
// Reference: tinyemu-2019-12-21/riscv_machine.c:492-525 (fdt_prop_tab_str)
func (s *fdtState) propStrTab(name string, strs ...string) {
	var data []byte
	for _, str := range strs {
		data = append(data, []byte(str)...)
		data = append(data, 0)
	}
	s.prop(name, data)
}

// propEmpty adds an empty property (marker).
func (s *fdtState) propEmpty(name string) {
	s.prop(name, nil)
}

// output writes the completed FDT to the destination buffer.
// Returns the size of the FDT in bytes.
// Reference: tinyemu-2019-12-21/riscv_machine.c:528-578 (fdt_output)
// Note: fdt_end (C:580-585) is not needed in Go as GC handles cleanup.
func (s *fdtState) output(dst []byte) int {
	if s.openNodeCount != 0 {
		panic("FDT: unclosed nodes")
	}

	// Add FDT_END
	s.put32(FDTEnd)

	dtStructSize := len(s.tab) * 4
	dtStringsSize := len(s.stringTable)

	// Header size
	headerSize := 40

	// Calculate offsets
	pos := headerSize
	offDTStruct := pos
	pos += dtStructSize

	// Align to 8
	for pos&7 != 0 {
		pos++
	}
	offMemRsvmap := pos
	pos += 16 // One empty reserve entry

	offDTStrings := pos
	pos += dtStringsSize

	// Align to 8
	for pos&7 != 0 {
		pos++
	}
	totalSize := pos

	// Write header
	binary.BigEndian.PutUint32(dst[0:], FDTMagic)
	binary.BigEndian.PutUint32(dst[4:], uint32(totalSize))
	binary.BigEndian.PutUint32(dst[8:], uint32(offDTStruct))
	binary.BigEndian.PutUint32(dst[12:], uint32(offDTStrings))
	binary.BigEndian.PutUint32(dst[16:], uint32(offMemRsvmap))
	binary.BigEndian.PutUint32(dst[20:], FDTVersion)
	binary.BigEndian.PutUint32(dst[24:], 16) // Last compatible version
	binary.BigEndian.PutUint32(dst[28:], 0)  // Boot CPU ID
	binary.BigEndian.PutUint32(dst[32:], uint32(dtStringsSize))
	binary.BigEndian.PutUint32(dst[36:], uint32(dtStructSize))

	// Write structure
	for i, v := range s.tab {
		binary.BigEndian.PutUint32(dst[offDTStruct+i*4:], v)
	}

	// Zero pad between structure and rsvmap
	for i := offDTStruct + dtStructSize; i < offMemRsvmap; i++ {
		dst[i] = 0
	}

	// Write empty memory reservation entry
	for i := 0; i < 16; i++ {
		dst[offMemRsvmap+i] = 0
	}

	// Write string table
	copy(dst[offDTStrings:], s.stringTable)

	// Zero pad to end
	for i := offDTStrings + dtStringsSize; i < totalSize; i++ {
		dst[i] = 0
	}

	return totalSize
}

// buildFDT generates the Flattened Device Tree for the machine.
// Reference: riscv_machine.c lines 587-751 (riscv_build_fdt)
func (m *Machine) buildFDT(dst []byte, kernelStart, kernelSize, initrdStart, initrdSize uint64, cmdLine string) int {
	s := newFDTState()

	var curPhandle uint32 = 1

	// Root node
	s.beginNode("")
	s.propU32("#address-cells", 2)
	s.propU32("#size-cells", 2)
	s.propStr("compatible", "ucbbar,riscvemu-bar_dev")
	s.propStr("model", "ucbbar,riscvemu-bare")

	// CPU list
	s.beginNode("cpus")
	s.propU32("#address-cells", 1)
	s.propU32("#size-cells", 0)
	s.propU32("timebase-frequency", RTCFreq)

	// CPU node
	s.beginNodeNum("cpu", 0)
	s.propStr("device_type", "cpu")
	s.propU32("reg", 0)
	s.propStr("status", "okay")
	s.propStr("compatible", "riscv")

	// Build ISA string
	isaString := m.buildISAString()
	s.propStr("riscv,isa", isaString)

	// MMU type - only advertise sv39 since that's what TinyEMU fully supports
	// (OpenSBI may try Sv57/Sv48 which aren't fully implemented)
	if m.maxXLEN <= 32 {
		s.propStr("mmu-type", "riscv,sv32")
	} else {
		s.propStr("mmu-type", "riscv,sv39")
	}
	s.propU32("clock-frequency", 2000000000)

	// CPU interrupt controller
	s.beginNode("interrupt-controller")
	s.propU32("#interrupt-cells", 1)
	s.propEmpty("interrupt-controller")
	s.propStr("compatible", "riscv,cpu-intc")
	intcPhandle := curPhandle
	curPhandle++
	s.propU32("phandle", intcPhandle)
	s.endNode() // interrupt-controller

	s.endNode() // cpu
	s.endNode() // cpus

	// Reserved memory node - marks OpenSBI region as unavailable to Linux
	// This prevents DMA buffers from being allocated in the OpenSBI region,
	// which would corrupt the trap handler and cause hangs.
	// Reference: https://www.kernel.org/doc/Documentation/devicetree/bindings/reserved-memory/reserved-memory.txt
	const opensbiReservedSize uint64 = 0x200000 // 2MB - matches kernel page alignment
	s.beginNode("reserved-memory")
	s.propU32("#address-cells", 2)
	s.propU32("#size-cells", 2)
	s.propEmpty("ranges")

	s.beginNodeNum("opensbi", RAMBaseAddr)
	s.propU32Tab("reg",
		uint32(RAMBaseAddr>>32), uint32(RAMBaseAddr),
		uint32(opensbiReservedSize>>32), uint32(opensbiReservedSize))
	s.propEmpty("no-map") // Critical: excludes from kernel memory map and DMA
	s.endNode()           // opensbi

	s.endNode() // reserved-memory

	// Memory node
	s.beginNodeNum("memory", RAMBaseAddr)
	s.propStr("device_type", "memory")
	s.propU32Tab("reg",
		uint32(RAMBaseAddr>>32), uint32(RAMBaseAddr),
		uint32(m.ramSize>>32), uint32(m.ramSize))
	s.endNode() // memory

	// HTIF node - Host-Target Interface for console
	// OpenSBI generic platform uses FDT to discover HTIF
	// OpenSBI reads reg[0] as fromhost, computes tohost = fromhost + 8
	// TinyEMU layout now matches: fromhost at base+0, tohost at base+8
	s.beginNode("htif")
	s.propStr("compatible", "ucb,htif0")
	s.propU64x2("reg", HTIFAddr, HTIFSize)
	s.endNode() // htif

	// SoC node
	s.beginNode("soc")
	s.propU32("#address-cells", 2)
	s.propU32("#size-cells", 2)
	s.propStrTab("compatible", "ucbbar,riscvemu-bar-soc", "simple-bus")
	s.propEmpty("ranges")

	// CLINT node
	s.beginNodeNum("clint", CLINTAddr)
	s.propStr("compatible", "riscv,clint0")
	s.propU32Tab("interrupts-extended",
		intcPhandle, 3, // M IPI irq
		intcPhandle, 7) // M timer irq
	s.propU64x2("reg", CLINTAddr, CLINTSize)
	s.endNode() // clint

	// PLIC node
	s.beginNodeNum("plic", PLICAddr)
	s.propU32("#interrupt-cells", 1)
	s.propEmpty("interrupt-controller")
	s.propStr("compatible", "riscv,plic0")
	s.propU32("riscv,ndev", 31)
	s.propU64x2("reg", PLICAddr, PLICSize)
	s.propU32Tab("interrupts-extended",
		intcPhandle, 9, // S ext irq
		intcPhandle, 11) // M ext irq
	plicPhandle := curPhandle
	curPhandle++
	s.propU32("phandle", plicPhandle)
	s.endNode() // plic

	// VirtIO devices
	for i := 0; i < m.virtioCount; i++ {
		addr := uint64(VirtIOAddr + i*VirtIOSize)
		s.beginNodeNum("virtio", addr)
		s.propStr("compatible", "virtio,mmio")
		s.propU64x2("reg", addr, VirtIOSize)
		s.propU32Tab("interrupts-extended", plicPhandle, uint32(VirtIOIRQ+i))
		s.endNode() // virtio
	}

	s.endNode() // soc

	// Chosen node
	s.beginNode("chosen")
	if cmdLine != "" {
		s.propStr("bootargs", cmdLine)
	} else {
		s.propStr("bootargs", "")
	}
	if kernelSize > 0 {
		s.propU64("riscv,kernel-start", kernelStart)
		s.propU64("riscv,kernel-end", kernelStart+kernelSize)
	}
	if initrdSize > 0 {
		s.propU64("linux,initrd-start", initrdStart)
		s.propU64("linux,initrd-end", initrdStart+initrdSize)
	}
	s.endNode() // chosen

	s.endNode() // root

	return s.output(dst)
}

// buildISAString builds the RISC-V ISA string from MISA.
// Reference: riscv_machine.c lines 622-630
//
// Note: Linux 6.x kernels have strict ISA parsing requirements:
//  1. Extensions must be in canonical order: i, m, a, f, d, c (not alphabetical)
//  2. The 's' and 'u' extensions (supervisor/user mode) should NOT be included
//     as they cause parsing failures in modern kernels
func (m *Machine) buildISAString() string {
	misa := m.cpu.GetMISA()
	result := fmt.Sprintf("rv%d", m.maxXLEN)

	// Add extensions in canonical RISC-V order (not alphabetical!)
	// The order follows the RISC-V ISA specification: base + standard extensions
	canonicalOrder := []struct {
		bit  int
		char byte
	}{
		{8, 'i'},  // Integer base (required)
		{12, 'm'}, // Integer multiply/divide
		{0, 'a'},  // Atomic
		{5, 'f'},  // Single-precision float
		{3, 'd'},  // Double-precision float
		{2, 'c'},  // Compressed
		// Note: 's' (bit 18) and 'u' (bit 20) are intentionally excluded
		// as they cause ISA parsing failures in Linux 6.x kernels
	}

	for _, ext := range canonicalOrder {
		if misa&(1<<ext.bit) != 0 {
			result += string(ext.char)
		}
	}

	return result
}
