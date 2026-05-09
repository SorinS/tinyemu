package x86

import (
	"testing"
)

// TestSLDT tests SLDT r/m16 (0F 00 /0)
func TestSLDT(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0)
	// 0F 00 C0 = SLDT AX
	code := []byte{0x0F, 0x00, 0xC0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg16(AX); got != 0 {
		t.Errorf("AX = 0x%04X, want 0", got)
	}
}

// TestSTR tests STR r/m16 (0F 00 /1)
func TestSTR(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1234)
	// 0F 00 C8 = STR AX
	code := []byte{0x0F, 0x00, 0xC8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg16(AX); got != 0 {
		t.Errorf("AX = 0x%04X, want 0", got)
	}
}

// TestLLDT tests LLDT r/m16 (0F 00 /2) as no-op
func TestLLDT(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg16(AX, 0x0028)
	// 0F 00 D0 = LLDT AX
	code := []byte{0x0F, 0x00, 0xD0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// Should not error; AX unchanged
	if got := c.GetReg16(AX); got != 0x0028 {
		t.Errorf("AX = 0x%04X, want 0x0028", got)
	}
}

// TestLTR tests LTR r/m16 (0F 00 /3) as no-op
func TestLTR(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg16(AX, 0x0030)
	// 0F 00 D8 = LTR AX
	code := []byte{0x0F, 0x00, 0xD8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg16(AX); got != 0x0030 {
		t.Errorf("AX = 0x%04X, want 0x0030", got)
	}
}

// TestVERR tests VERR r/m16 (0F 00 /4) clears ZF
func TestVERR(t *testing.T) {
	c := newTestCPU(t)
	c.eflags |= EFLAGS_ZF
	// 0F 00 E0 = VERR AX
	code := []byte{0x0F, 0x00, 0xE0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF should be cleared by VERR")
	}
}

// TestVERW tests VERW r/m16 (0F 00 /5) clears ZF
func TestVERW(t *testing.T) {
	c := newTestCPU(t)
	c.eflags |= EFLAGS_ZF
	// 0F 00 E8 = VERW AX
	code := []byte{0x0F, 0x00, 0xE8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF should be cleared by VERW")
	}
}

// TestSLDTMem tests SLDT to memory
func TestSLDTMem(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x200)
	// 0F 00 00 = SLDT [EAX]
	code := []byte{0x0F, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.readMem16(0x200); got != 0 {
		t.Errorf("mem[0x200] = 0x%04X, want 0", got)
	}
}
