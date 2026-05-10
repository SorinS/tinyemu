package x86

import "testing"

func TestBSF32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000100) // bit 8
	c.SetReg32(EAX, 0xDEADBEEF) // overwritten on match
	code := []byte{0x0F, 0xBC, 0xC3, 0xF4} // BSF EAX, EBX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF set despite non-zero source")
	}
	if got := c.GetReg32(EAX); got != 8 {
		t.Errorf("BSF result = %d, want 8", got)
	}
}

func TestBSF32_ZeroSource(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0)
	c.SetReg32(EAX, 0xDEADBEEF)
	code := []byte{0x0F, 0xBC, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF not set despite zero source")
	}
	// Per spec, destination is undefined when source = 0. We leave it.
	if got := c.GetReg32(EAX); got != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want preserved 0xDEADBEEF", got)
	}
}

func TestBSR32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00010001) // bits 0 and 16
	code := []byte{0x0F, 0xBD, 0xC3, 0xF4} // BSR EAX, EBX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF set despite non-zero source")
	}
	if got := c.GetReg32(EAX); got != 16 {
		t.Errorf("BSR result = %d, want 16", got)
	}
}

func TestBSF16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xFFFF0000) // upper bits should stay
	c.SetReg32(EBX, 0x0010)     // bit 4
	code := []byte{0x66, 0x0F, 0xBC, 0xC3, 0xF4} // BSF AX, BX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF set despite non-zero source")
	}
	if got := c.GetReg32(EAX); got != 0xFFFF0004 {
		t.Errorf("EAX = 0x%08X, want 0xFFFF0004", got)
	}
}
