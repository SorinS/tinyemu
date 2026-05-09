package machine

import (
	"encoding/binary"
	"io"
	"testing"

	"github.com/jtolio/tinyemu-go/virtio"
)

// TestFDTStateBasic tests basic FDT state operations.
func TestFDTStateBasic(t *testing.T) {
	s := newFDTState()

	// Test put32
	s.put32(0x12345678)
	if len(s.tab) != 1 {
		t.Errorf("expected tab length 1, got %d", len(s.tab))
	}
	if s.tab[0] != 0x12345678 {
		t.Errorf("expected 0x12345678, got 0x%08x", s.tab[0])
	}
}

// TestFDTStatePutData tests putData with padding.
func TestFDTStatePutData(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected int // expected number of uint32s
	}{
		{"empty", []byte{}, 0},
		{"1 byte", []byte{0x12}, 1},
		{"2 bytes", []byte{0x12, 0x34}, 1},
		{"3 bytes", []byte{0x12, 0x34, 0x56}, 1},
		{"4 bytes", []byte{0x12, 0x34, 0x56, 0x78}, 1},
		{"5 bytes", []byte{0x12, 0x34, 0x56, 0x78, 0x9A}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFDTState()
			s.putData(tc.data)
			if len(s.tab) != tc.expected {
				t.Errorf("expected tab length %d, got %d", tc.expected, len(s.tab))
			}
		})
	}
}

// TestFDTStateBeginEndNode tests node creation.
func TestFDTStateBeginEndNode(t *testing.T) {
	s := newFDTState()

	s.beginNode("test-node")
	if s.openNodeCount != 1 {
		t.Errorf("expected openNodeCount 1, got %d", s.openNodeCount)
	}

	s.endNode()
	if s.openNodeCount != 0 {
		t.Errorf("expected openNodeCount 0, got %d", s.openNodeCount)
	}

	// Check structure: FDT_BEGIN_NODE, name (padded), FDT_END_NODE
	if len(s.tab) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(s.tab))
	}
	if s.tab[0] != FDTBeginNode {
		t.Errorf("expected FDT_BEGIN_NODE, got %d", s.tab[0])
	}
	if s.tab[len(s.tab)-1] != FDTEndNode {
		t.Errorf("expected FDT_END_NODE at end, got %d", s.tab[len(s.tab)-1])
	}
}

// TestFDTStateStringTable tests string table management.
func TestFDTStateStringTable(t *testing.T) {
	s := newFDTState()

	// First string
	off1 := s.getStringOffset("prop1")
	if off1 != 0 {
		t.Errorf("expected first string offset 0, got %d", off1)
	}

	// Same string should return same offset
	off1b := s.getStringOffset("prop1")
	if off1b != off1 {
		t.Errorf("expected same offset for same string, got %d vs %d", off1b, off1)
	}

	// Different string
	off2 := s.getStringOffset("prop2")
	if off2 == off1 {
		t.Errorf("expected different offset for different string")
	}
	// offset should be len("prop1") + 1 (null terminator)
	if off2 != 6 {
		t.Errorf("expected offset 6, got %d", off2)
	}
}

// TestFDTStatePropU64 tests u64 property creation.
// Reference: tinyemu-2019-12-21/riscv_machine.c:465-472 (fdt_prop_tab_u64)
func TestFDTStatePropU64(t *testing.T) {
	s := newFDTState()
	s.beginNode("test")
	s.propU64("value", 0x123456789ABCDEF0)
	s.endNode()

	buf := make([]byte, 4096)
	s.output(buf)

	// Find the property value in the output by looking for the FDT_PROP marker
	// The u64 should be stored as two big-endian u32s: 0x12345678, 0x9ABCDEF0
	found := false
	for i := 0; i < len(s.tab)-1; i++ {
		if s.tab[i] == 0x12345678 && s.tab[i+1] == 0x9ABCDEF0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("u64 value not encoded correctly as two big-endian u32s")
	}
}

// TestFDTStatePropU64x2 tests double u64 property creation.
// Reference: tinyemu-2019-12-21/riscv_machine.c:474-483 (fdt_prop_tab_u64_2)
func TestFDTStatePropU64x2(t *testing.T) {
	s := newFDTState()
	s.beginNode("test")
	s.propU64x2("reg", 0x0000000080000000, 0x0000000040000000)
	s.endNode()

	// The values should appear as four u32s:
	// 0x00000000, 0x80000000, 0x00000000, 0x40000000
	found := false
	for i := 0; i < len(s.tab)-3; i++ {
		if s.tab[i] == 0x00000000 && s.tab[i+1] == 0x80000000 &&
			s.tab[i+2] == 0x00000000 && s.tab[i+3] == 0x40000000 {
			found = true
			break
		}
	}
	if !found {
		t.Error("u64x2 values not encoded correctly as four big-endian u32s")
	}
}

// TestFDTStatePropStr tests string property includes null terminator.
// Reference: tinyemu-2019-12-21/riscv_machine.c:485-489 (fdt_prop_str)
func TestFDTStatePropStr(t *testing.T) {
	s := newFDTState()
	s.beginNode("test")
	s.propStr("compatible", "test")
	s.endNode()

	// Check that FDT_PROP length field is strlen + 1 (for null terminator)
	// "test" has length 4, so property length should be 5
	for i := 0; i < len(s.tab)-1; i++ {
		if s.tab[i] == FDTProp {
			length := s.tab[i+1]
			if length != 5 { // "test" + null = 5 bytes
				t.Errorf("expected string property length 5, got %d", length)
			}
			return
		}
	}
	t.Error("FDT_PROP not found")
}

// TestFDTStatePropStrTab tests multiple string property.
// Reference: tinyemu-2019-12-21/riscv_machine.c:492-525 (fdt_prop_tab_str)
func TestFDTStatePropStrTab(t *testing.T) {
	s := newFDTState()
	s.beginNode("test")
	s.propStrTab("compatible", "vendor,device", "simple-bus")
	s.endNode()

	// Length should be: len("vendor,device") + 1 + len("simple-bus") + 1 = 13 + 1 + 10 + 1 = 25
	expectedLen := uint32(len("vendor,device") + 1 + len("simple-bus") + 1)

	for i := 0; i < len(s.tab)-1; i++ {
		if s.tab[i] == FDTProp {
			length := s.tab[i+1]
			if length != expectedLen {
				t.Errorf("expected string table property length %d, got %d", expectedLen, length)
			}
			return
		}
	}
	t.Error("FDT_PROP not found")
}

// TestFDTStatePropU32 tests u32 property creation.
func TestFDTStatePropU32(t *testing.T) {
	s := newFDTState()
	s.beginNode("test")
	s.propU32("value", 0x12345678)
	s.endNode()

	// Check that FDT_PROP token is present
	found := false
	for _, v := range s.tab {
		if v == FDTProp {
			found = true
			break
		}
	}
	if !found {
		t.Error("FDT_PROP not found in tab")
	}
}

// TestFDTOutputHeaderLayout tests FDT header matches C layout exactly.
// Reference: tinyemu-2019-12-21/riscv_machine.c:528-578 (fdt_output)
func TestFDTOutputHeaderLayout(t *testing.T) {
	s := newFDTState()
	s.beginNode("")
	s.propStr("test", "value")
	s.endNode()

	buf := make([]byte, 4096)
	size := s.output(buf)

	// Verify all header fields match C struct fdt_header layout:
	// offset 0:  magic
	// offset 4:  totalsize
	// offset 8:  off_dt_struct
	// offset 12: off_dt_strings
	// offset 16: off_mem_rsvmap
	// offset 20: version
	// offset 24: last_comp_version
	// offset 28: boot_cpuid_phys
	// offset 32: size_dt_strings
	// offset 36: size_dt_struct

	magic := binary.BigEndian.Uint32(buf[0:4])
	totalSize := binary.BigEndian.Uint32(buf[4:8])
	offDTStruct := binary.BigEndian.Uint32(buf[8:12])
	offDTStrings := binary.BigEndian.Uint32(buf[12:16])
	offMemRsvmap := binary.BigEndian.Uint32(buf[16:20])
	version := binary.BigEndian.Uint32(buf[20:24])
	lastCompVersion := binary.BigEndian.Uint32(buf[24:28])
	bootCPUIDPhys := binary.BigEndian.Uint32(buf[28:32])
	sizeDTStrings := binary.BigEndian.Uint32(buf[32:36])
	sizeDTStruct := binary.BigEndian.Uint32(buf[36:40])

	if magic != FDTMagic {
		t.Errorf("magic: expected 0x%08x, got 0x%08x", FDTMagic, magic)
	}
	if totalSize != uint32(size) {
		t.Errorf("totalsize: expected %d, got %d", size, totalSize)
	}
	if offDTStruct != 40 { // sizeof(struct fdt_header) = 40
		t.Errorf("off_dt_struct: expected 40, got %d", offDTStruct)
	}
	if offDTStrings <= offMemRsvmap {
		t.Errorf("off_dt_strings (%d) should be after off_mem_rsvmap (%d)", offDTStrings, offMemRsvmap)
	}
	if offMemRsvmap <= offDTStruct {
		t.Errorf("off_mem_rsvmap (%d) should be after off_dt_struct (%d)", offMemRsvmap, offDTStruct)
	}
	if version != FDTVersion {
		t.Errorf("version: expected %d, got %d", FDTVersion, version)
	}
	if lastCompVersion != 16 {
		t.Errorf("last_comp_version: expected 16, got %d", lastCompVersion)
	}
	if bootCPUIDPhys != 0 {
		t.Errorf("boot_cpuid_phys: expected 0, got %d", bootCPUIDPhys)
	}
	if sizeDTStrings == 0 {
		t.Errorf("size_dt_strings should not be 0")
	}
	if sizeDTStruct == 0 {
		t.Errorf("size_dt_struct should not be 0")
	}

	// Verify 8-byte alignment of offMemRsvmap (as required by C code)
	if offMemRsvmap%8 != 0 {
		t.Errorf("off_mem_rsvmap (%d) should be 8-byte aligned", offMemRsvmap)
	}

	// Verify totalsize is 8-byte aligned (as done by C code)
	if totalSize%8 != 0 {
		t.Errorf("totalsize (%d) should be 8-byte aligned", totalSize)
	}
}

// TestFDTOutput tests FDT binary output.
func TestFDTOutput(t *testing.T) {
	s := newFDTState()
	s.beginNode("")
	s.propStr("compatible", "test,device")
	s.propU32("reg", 0x1000)
	s.endNode()

	// Allocate buffer for output
	buf := make([]byte, 4096)
	size := s.output(buf)

	// Check magic
	magic := binary.BigEndian.Uint32(buf[0:4])
	if magic != FDTMagic {
		t.Errorf("expected magic 0x%08x, got 0x%08x", FDTMagic, magic)
	}

	// Check total size matches
	totalSize := binary.BigEndian.Uint32(buf[4:8])
	if totalSize != uint32(size) {
		t.Errorf("expected total size %d, got %d", size, totalSize)
	}

	// Check version
	version := binary.BigEndian.Uint32(buf[20:24])
	if version != FDTVersion {
		t.Errorf("expected version %d, got %d", FDTVersion, version)
	}
}

// TestBuildFDT tests full FDT generation for a machine.
func TestBuildFDT(t *testing.T) {
	cfg := Config{
		RAMSize: 128 * 1024 * 1024,
		MaxXLEN: 64,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Build FDT
	buf := make([]byte, 8192)
	size := m.buildFDT(buf, 0x80200000, 0x400000, 0x84000000, 0x100000, "console=hvc0")

	if size <= 0 {
		t.Fatalf("FDT size should be positive, got %d", size)
	}

	// Check magic
	magic := binary.BigEndian.Uint32(buf[0:4])
	if magic != FDTMagic {
		t.Errorf("expected magic 0x%08x, got 0x%08x", FDTMagic, magic)
	}

	// Check that size is reasonable (typical FDT is 1-4 KB)
	if size < 500 || size > 8192 {
		t.Errorf("FDT size %d seems unreasonable", size)
	}

	t.Logf("Generated FDT size: %d bytes", size)
}

// TestBuildFDTRV32 tests FDT generation for RV32.
func TestBuildFDTRV32(t *testing.T) {
	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 32,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	buf := make([]byte, 8192)
	size := m.buildFDT(buf, 0, 0, 0, 0, "")

	if size <= 0 {
		t.Fatalf("FDT size should be positive, got %d", size)
	}

	// Check magic
	magic := binary.BigEndian.Uint32(buf[0:4])
	if magic != FDTMagic {
		t.Errorf("expected magic 0x%08x, got 0x%08x", FDTMagic, magic)
	}
}

// TestBuildISAString tests ISA string generation.
func TestBuildISAString(t *testing.T) {
	tests := []struct {
		xlen     int
		expected string
	}{
		{32, "rv32"},
		{64, "rv64"},
		{128, "rv128"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			cfg := Config{
				RAMSize: 64 * 1024 * 1024,
				MaxXLEN: tc.xlen,
			}

			m, err := New(cfg)
			if err != nil {
				t.Fatalf("failed to create machine: %v", err)
			}
			defer m.Close()

			isa := m.buildISAString()
			if len(isa) < len(tc.expected) {
				t.Errorf("ISA string too short: %s", isa)
			}
			if isa[:len(tc.expected)] != tc.expected {
				t.Errorf("expected ISA to start with %s, got %s", tc.expected, isa)
			}

			// Check for common extensions (i, m, a, f, d, c, s, u are typically enabled)
			for _, ext := range "imafdc" {
				found := false
				for _, c := range isa[len(tc.expected):] {
					if c == ext {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected extension %c in ISA string %s", ext, isa)
				}
			}
		})
	}
}

// TestFDTWithVirtIO tests FDT generation with VirtIO devices.
func TestFDTWithVirtIO(t *testing.T) {
	console := &virtio.CharacterDevice{
		Writer: io.Discard,
	}

	cfg := Config{
		RAMSize: 64 * 1024 * 1024,
		MaxXLEN: 64,
		Console: console,
	}

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}
	defer m.Close()

	// Should have 1 VirtIO device (console)
	if m.VirtIOCount() != 1 {
		t.Fatalf("expected 1 VirtIO device, got %d", m.VirtIOCount())
	}

	buf := make([]byte, 8192)
	size := m.buildFDT(buf, 0, 0, 0, 0, "")

	if size <= 0 {
		t.Fatalf("FDT size should be positive, got %d", size)
	}
}
