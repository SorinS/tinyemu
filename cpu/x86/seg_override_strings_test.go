package x86

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestSegmentOverrideMOVS verifies that a segment override prefix on MOVS
// changes the source segment from DS to the overridden segment.
func TestSegmentOverrideMOVS(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	_, _ = mm.RegisterRAM(0, 1<<20, 0)
	c := NewCPU(mm)
	c.SetCR(0, c.GetCR(0)|CR0_PE)
	c.SetSegAccess(CS, 0x0400)

	// Set up segments with different bases
	c.SetSeg(DS, 0x0008)
	c.SetSegBase(DS, 0x01000)
	c.SetSeg(ES, 0x0010)
	c.SetSegBase(ES, 0x02000)
	c.SetSeg(FS, 0x0018)
	c.SetSegBase(FS, 0x03000) // override segment

	// Source data in FS segment at base 0x03000 + SI=0x0100 -> 0x03100
	c.writeMem8(0x03100, 0xAB)
	// Destination in ES segment at base 0x02000 + DI=0x0200 -> 0x02200
	c.writeMem8(0x02200, 0x00)

	c.SetReg32(ESI, 0x0100)
	c.SetReg32(EDI, 0x0200)
	c.eflags = 0

	// 0x64 = FS segment override, 0xA4 = MOVSB
	code := []byte{0x64, 0xA4, 0xF4}
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)
	c.SetEIP(0x100)
	for i, b := range code {
		c.writeMem8(0x100+uint32(i), b)
	}

	// Step 1: FS override + MOVSB
	if err := c.Step(); err != nil {
		t.Fatalf("MOVS step error: %v", err)
	}

	// Without override, it would read from DS:0x0100 = 0x01100 (contains 0x00)
	// With FS override, it should read from FS:0x0100 = 0x03100 (contains 0xAB)
	dst := c.readMem8(0x02200)
	if dst != 0xAB {
		t.Fatalf("MOVS with FS override: wrote 0x%02X to ES:DI, want 0xAB. "+
			"Segment override was ignored (read from DS instead of FS)", dst)
	}
}

// TestSegmentOverrideLODS verifies that a segment override prefix on LODS
// changes the source segment from DS to the overridden segment.
func TestSegmentOverrideLODS(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	_, _ = mm.RegisterRAM(0, 1<<20, 0)
	c := NewCPU(mm)
	c.SetCR(0, c.GetCR(0)|CR0_PE)
	c.SetSegAccess(CS, 0x0400)

	c.SetSeg(DS, 0x0008)
	c.SetSegBase(DS, 0x01000)
	c.SetSeg(FS, 0x0018)
	c.SetSegBase(FS, 0x03000)

	// Source data in FS segment at 0x03100
	c.writeMem8(0x03100, 0xCD)
	// Source data in DS segment at 0x01100
	c.writeMem8(0x01100, 0xEF)

	c.SetReg32(ESI, 0x0100)
	c.eflags = 0

	// 0x64 = FS override, 0xAC = LODSB
	code := []byte{0x64, 0xAC, 0xF4}
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)
	c.SetEIP(0x100)
	for i, b := range code {
		c.writeMem8(0x100+uint32(i), b)
	}

	if err := c.Step(); err != nil {
		t.Fatalf("LODS step error: %v", err)
	}

	al := c.GetReg8(AL)
	if al != 0xCD {
		t.Fatalf("LODS with FS override: AL=0x%02X, want 0xCD. "+
			"Segment override was ignored", al)
	}
}

// TestSegmentOverrideCMPS verifies that a segment override prefix on CMPS
// changes the source segment from DS to the overridden segment.
func TestSegmentOverrideCMPS(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	_, _ = mm.RegisterRAM(0, 1<<20, 0)
	c := NewCPU(mm)
	c.SetCR(0, c.GetCR(0)|CR0_PE)
	c.SetSegAccess(CS, 0x0400)

	c.SetSeg(DS, 0x0008)
	c.SetSegBase(DS, 0x01000)
	c.SetSeg(ES, 0x0010)
	c.SetSegBase(ES, 0x02000)
	c.SetSeg(FS, 0x0018)
	c.SetSegBase(FS, 0x03000)

	// Source in FS segment at 0x03100 = 0xAB
	c.writeMem8(0x03100, 0xAB)
	// Source in DS segment at 0x01100 = 0xEF
	c.writeMem8(0x01100, 0xEF)
	// Dest in ES segment at 0x02200 = 0xAB (should match FS source)
	c.writeMem8(0x02200, 0xAB)

	c.SetReg32(ESI, 0x0100)
	c.SetReg32(EDI, 0x0200)
	c.eflags = 0

	// 0x64 = FS override, 0xA6 = CMPSB
	code := []byte{0x64, 0xA6, 0xF4}
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)
	c.SetEIP(0x100)
	for i, b := range code {
		c.writeMem8(0x100+uint32(i), b)
	}

	if err := c.Step(); err != nil {
		t.Fatalf("CMPS step error: %v", err)
	}

	zf := c.getZF()
	if !zf {
		t.Fatalf("CMPS with FS override: ZF=false, want true. "+
			"Compared DS source (0xEF) instead of FS source (0xAB)")
	}
}
