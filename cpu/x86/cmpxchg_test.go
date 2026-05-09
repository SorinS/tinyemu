package x86

import (
	"testing"
)

// TestCmpxchg8Equal tests CMPXCHG r/m8, r8 when accumulator equals destination
func TestCmpxchg8Equal(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x42)
	c.SetReg8(BL, 0x42)
	// 0F B0 C3 = CMPXCHG BL, AL (modrm: 11 000 011)
	code := []byte{0x0F, 0xB0, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg8(BL); got != 0x42 {
		t.Errorf("BL = 0x%02X, want 0x42", got)
	}
	if !c.getZF() {
		t.Errorf("ZF should be set")
	}
}

// TestCmpxchg8NotEqual tests CMPXCHG r/m8, r8 when accumulator differs
func TestCmpxchg8NotEqual(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x10)
	c.SetReg8(BL, 0x20)
	code := []byte{0x0F, 0xB0, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg8(AL); got != 0x20 {
		t.Errorf("AL = 0x%02X, want 0x20", got)
	}
	if got := c.GetReg8(BL); got != 0x20 {
		t.Errorf("BL = 0x%02X, want 0x20 (unchanged)", got)
	}
	if c.getZF() {
		t.Errorf("ZF should be clear")
	}
}

// TestCmpxchg32Equal tests CMPXCHG r/m32, r32 when equal
func TestCmpxchg32Equal(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	c.SetReg32(EBX, 0x12345678)
	// 0F B1 C3 = CMPXCHG EBX, EAX
	code := []byte{0x0F, 0xB1, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EBX); got != 0x12345678 {
		t.Errorf("EBX = 0x%08X, want 0x12345678", got)
	}
	if !c.getZF() {
		t.Errorf("ZF should be set")
	}
}

// TestCmpxchg32NotEqual tests CMPXCHG r/m32, r32 when not equal
func TestCmpxchg32NotEqual(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x11111111)
	c.SetReg32(EBX, 0x22222222)
	code := []byte{0x0F, 0xB1, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0x22222222 {
		t.Errorf("EAX = 0x%08X, want 0x22222222", got)
	}
	if c.getZF() {
		t.Errorf("ZF should be clear")
	}
}

// TestCmpxchg32Mem tests CMPXCHG [mem], r32
func TestCmpxchg32Mem(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xAAAAAAAA)
	c.SetReg32(EBX, 0x200)
	c.SetReg32(ECX, 0xBBBBBBBB)
	c.writeMem32(0x200, 0xAAAAAAAA)
	// 0F B1 0B = CMPXCHG [EBX], ECX
	code := []byte{0x0F, 0xB1, 0x0B, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.readMem32(0x200); got != 0xBBBBBBBB {
		t.Errorf("mem[0x200] = 0x%08X, want 0xBBBBBBBB", got)
	}
	if !c.getZF() {
		t.Errorf("ZF should be set")
	}
}

// TestCmpxchg16Equal tests CMPXCHG r/m16, r16 with 0x66 prefix
func TestCmpxchg16Equal(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg16(AX, 0x1234)
	c.SetReg16(BX, 0x1234)
	// 66 0F B1 C3 = CMPXCHG BX, AX
	code := []byte{0x66, 0x0F, 0xB1, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg16(BX); got != 0x1234 {
		t.Errorf("BX = 0x%04X, want 0x1234", got)
	}
	if !c.getZF() {
		t.Errorf("ZF should be set")
	}
}
