package x86

import (
	"testing"
)

// TestSubAlImm8 tests SUB AL, imm8 (0x2C)
func TestSubAlImm8(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x50)
	code := []byte{0x2C, 0x20, 0xF4} // SUB AL, 0x20; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg8(AL); got != 0x30 {
		t.Errorf("AL = 0x%02X, want 0x30", got)
	}
	if c.getZF() || c.getCF() || c.getSF() || c.getOF() {
		t.Errorf("flags wrong for 0x50 - 0x20 = 0x30: ZF=%v CF=%v SF=%v OF=%v", c.getZF(), c.getCF(), c.getSF(), c.getOF())
	}
}

// TestSubAlImm8Borrow tests SUB AL, imm8 with borrow
func TestSubAlImm8Borrow(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x10)
	code := []byte{0x2C, 0x30, 0xF4} // SUB AL, 0x30; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg8(AL); got != 0xE0 {
		t.Errorf("AL = 0x%02X, want 0xE0", got)
	}
	if !c.getCF() || !c.getSF() {
		t.Errorf("flags wrong for 0x10 - 0x30")
	}
}

// TestSubEaxImm32 tests SUB EAX, imm32 (0x2D)
func TestSubEaxImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	code := []byte{0x2D, 0x00, 0x10, 0x00, 0x00, 0xF4} // SUB EAX, 0x00001000; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0x12344678 {
		t.Errorf("EAX = 0x%08X, want 0x12344678", got)
	}
}

// TestSubAxImm16 tests SUB AX, imm16 with 0x66 prefix (0x66 0x2D)
func TestSubAxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg16(AX, 0x5000)
	code := []byte{0x66, 0x2D, 0x20, 0x10, 0xF4} // SUB AX, 0x1020; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg16(AX); got != 0x3FE0 {
		t.Errorf("AX = 0x%04X, want 0x3FE0", got)
	}
	if got := c.GetReg32(EAX); got != 0x3FE0 {
		t.Errorf("EAX = 0x%08X, want 0x00003FE0", got)
	}
}

// TestSubEaxImm32Zero tests SUB EAX, imm32 resulting in zero
func TestSubEaxImm32Zero(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1000)
	code := []byte{0x2D, 0x00, 0x10, 0x00, 0x00, 0xF4} // SUB EAX, 0x1000; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = 0x%08X, want 0", got)
	}
	if !c.getZF() || c.getCF() {
		t.Errorf("flags wrong for 0x1000 - 0x1000")
	}
}

// TestSubEaxImm32Negative tests SUB EAX, imm32 where result is negative
func TestSubEaxImm32Negative(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x100)
	code := []byte{0x2D, 0x01, 0x01, 0x00, 0x00, 0xF4} // SUB EAX, 0x101; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xFFFFFFFF {
		t.Errorf("EAX = 0x%08X, want 0xFFFFFFFF", got)
	}
	if !c.getCF() || !c.getSF() {
		t.Errorf("flags wrong for 0x100 - 0x101")
	}
}

