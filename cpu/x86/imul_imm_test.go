package x86

import (
	"testing"
)

// TestImulRegRegImm32 tests IMUL r32, r/m32, imm32 (69)
func TestImulRegRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 5)
	// 69 C1 03 00 00 00 = IMUL EAX, ECX, 3
	code := []byte{0x69, 0xC1, 0x03, 0x00, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 15 {
		t.Errorf("EAX = %d, want 15", got)
	}
}

// TestImulRegMemImm32 tests IMUL r32, r/m32, imm32 with memory
func TestImulRegMemImm32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x200, 7)
	c.SetReg32(EBX, 0x200)
	// 69 03 04 00 00 00 = IMUL EAX, [EBX], 4
	code := []byte{0x69, 0x03, 0x04, 0x00, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 28 {
		t.Errorf("EAX = %d, want 28", got)
	}
}

// TestImulRegRegImm8 tests IMUL r32, r/m32, imm8 (6B)
func TestImulRegRegImm8(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EDX, 10)
	// 6B C2 FE = IMUL EAX, EDX, -2
	code := []byte{0x6B, 0xC2, 0xFE, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xFFFFFFEC { // -20
		t.Errorf("EAX = %d, want -20", int32(got))
	}
}

// TestImulRegRegImm16 tests IMUL r16, r/m16, imm16 with 0x66 prefix
func TestImulRegRegImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg16(CX, 6)
	// 66 69 C1 05 00 = IMUL AX, CX, 5
	code := []byte{0x66, 0x69, 0xC1, 0x05, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg16(AX); got != 30 {
		t.Errorf("AX = %d, want 30", got)
	}
}

// TestImulOverflow tests IMUL sets OF/CF on overflow
func TestImulOverflow(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 0x40000000)
	// 69 C1 04 00 00 00 = IMUL EAX, ECX, 4 (overflows)
	code := []byte{0x69, 0xC1, 0x04, 0x00, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = 0x%08X, want 0", got)
	}
	if !c.getOF() || !c.getCF() {
		t.Errorf("OF and CF should be set on overflow")
	}
}
