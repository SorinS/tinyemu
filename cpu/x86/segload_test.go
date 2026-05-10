package x86

import (
	"strings"
	"testing"
)

// buildGDTWithDescriptors writes a flat GDT at gdtAddr. Slot 0 is null; slot 1
// is a ring-0 flat code segment; slot 2 is a ring-0 flat data segment; slot 3
// is a ring-3 flat data segment.
func buildGDTWithDescriptors(c *CPU, gdtAddr uint32) {
	for i := 0; i < 32; i++ {
		c.writeMem8(gdtAddr+uint32(i), 0)
	}
	// Ring-0 code (selector 0x08).
	c.writeMem8(gdtAddr+8, 0xFF)
	c.writeMem8(gdtAddr+9, 0xFF)
	c.writeMem8(gdtAddr+10, 0x00)
	c.writeMem8(gdtAddr+11, 0x00)
	c.writeMem8(gdtAddr+12, 0x00)
	c.writeMem8(gdtAddr+13, 0x9A) // P=1 DPL=0 S=1 type=1010 (code, ER)
	c.writeMem8(gdtAddr+14, 0xCF)
	c.writeMem8(gdtAddr+15, 0x00)
	// Ring-0 data (selector 0x10).
	c.writeMem8(gdtAddr+16, 0xFF)
	c.writeMem8(gdtAddr+17, 0xFF)
	c.writeMem8(gdtAddr+18, 0x00)
	c.writeMem8(gdtAddr+19, 0x00)
	c.writeMem8(gdtAddr+20, 0x00)
	c.writeMem8(gdtAddr+21, 0x92) // P=1 DPL=0 S=1 type=0010 (data, RW)
	c.writeMem8(gdtAddr+22, 0xCF)
	c.writeMem8(gdtAddr+23, 0x00)
	// Ring-3 data (selector 0x18+3 = 0x1B).
	c.writeMem8(gdtAddr+24, 0xFF)
	c.writeMem8(gdtAddr+25, 0xFF)
	c.writeMem8(gdtAddr+26, 0x00)
	c.writeMem8(gdtAddr+27, 0x00)
	c.writeMem8(gdtAddr+28, 0x00)
	c.writeMem8(gdtAddr+29, 0xF2) // P=1 DPL=3 S=1 type=0010
	c.writeMem8(gdtAddr+30, 0xCF)
	c.writeMem8(gdtAddr+31, 0x00)
	c.SetSegBase(GDTR, gdtAddr)
	c.SetSegLimit(GDTR, 31)
}

func TestLoadSegmentProtected_NullCS_Error(t *testing.T) {
	c := newTestCPU(t)
	if err := c.LoadSegmentProtected(CS, 0); err == nil {
		t.Errorf("expected error for null CS, got nil")
	}
}

func TestLoadSegmentProtected_NullSS_Error(t *testing.T) {
	c := newTestCPU(t)
	if err := c.LoadSegmentProtected(SS, 0); err == nil {
		t.Errorf("expected error for null SS, got nil")
	}
}

func TestLoadSegmentProtected_NullDS_Allowed(t *testing.T) {
	c := newTestCPU(t)
	if err := c.LoadSegmentProtected(DS, 0); err != nil {
		t.Errorf("null DS load should succeed, got: %v", err)
	}
	if c.GetSeg(DS) != 0 {
		t.Errorf("DS not zeroed after null load: 0x%04X", c.GetSeg(DS))
	}
}

func TestLoadSegmentProtected_DataIntoCS_Error(t *testing.T) {
	c := newTestCPU(t)
	buildGDTWithDescriptors(c, 0x4000)
	err := c.LoadSegmentProtected(CS, 0x10) // selector 0x10 is data
	if err == nil || !strings.Contains(err.Error(), "code segment") {
		t.Errorf("expected 'not a code segment' error, got: %v", err)
	}
}

func TestLoadSegmentProtected_CodeIntoSS_Error(t *testing.T) {
	c := newTestCPU(t)
	buildGDTWithDescriptors(c, 0x4000)
	err := c.LoadSegmentProtected(SS, 0x08) // selector 0x08 is code
	if err == nil || !strings.Contains(err.Error(), "writable data") {
		t.Errorf("expected 'writable data' error, got: %v", err)
	}
}

func TestLoadSegmentProtected_SelectorOutOfBounds_Error(t *testing.T) {
	c := newTestCPU(t)
	buildGDTWithDescriptors(c, 0x4000) // GDT limit = 31, so selectors > 0x18 are out
	err := c.LoadSegmentProtected(DS, 0x40)
	if err == nil || !strings.Contains(err.Error(), "table limit") {
		t.Errorf("expected 'table limit' error, got: %v", err)
	}
}

func TestLoadSegmentProtected_UserModeAccessesSupervisorData_Error(t *testing.T) {
	c := newTestCPU(t)
	buildGDTWithDescriptors(c, 0x4000)
	c.cpl = 3
	// Selector 0x10 is DPL=0 data, RPL=0. User-mode (cpl=3) load should #GP.
	err := c.LoadSegmentProtected(DS, 0x10)
	if err == nil || !strings.Contains(err.Error(), "privilege") {
		t.Errorf("expected privilege error, got: %v", err)
	}
}

func TestLoadSegmentProtected_UserModeAccessesUserData_OK(t *testing.T) {
	c := newTestCPU(t)
	buildGDTWithDescriptors(c, 0x4000)
	c.cpl = 3
	// Selector 0x18 is DPL=3 data; load with RPL=3 = 0x1B.
	if err := c.LoadSegmentProtected(DS, 0x1B); err != nil {
		t.Errorf("user-mode load of ring-3 data should succeed, got: %v", err)
	}
}
