package x86

import (
	"testing"
)

// TestMovImmWithOperandSizePrefix verifies that C7 /0 respects the 0x66 prefix.
// This was the root cause of the Alpine kernel crash: handleMovImm32 always
// fetched a 32-bit immediate, corrupting the instruction stream.
func TestMovImmWithOperandSizePrefix(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x100)
	// 66 C7 00 BE EF  = MOV WORD [EAX], 0xEFBE (6 bytes: prefix + opcode + modrm + imm16)
	// F4              = HLT
	code := []byte{0x66, 0xC7, 0x00, 0xBE, 0xEF, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem16(0x100); v != 0xEFBE {
		t.Errorf("mem[0x100] = 0x%04X, want 0xEFBE", v)
	}
	// EIP should be 0x1000 + 5 + 1 = 0x1006 (5 bytes for MOV + 1 for HLT)
	if c.GetEIP() != 0x1006 {
		t.Errorf("EIP = 0x%04X, want 0x1006", c.GetEIP())
	}
}

// TestMovImm32NoPrefix verifies that C7 /0 still works without the 0x66 prefix.
func TestMovImm32NoPrefix(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x100)
	// C7 00 BE EF AD DE  = MOV DWORD [EAX], 0xDEADEFBE (6 bytes)
	// F4                 = HLT
	code := []byte{0xC7, 0x00, 0xBE, 0xEF, 0xAD, 0xDE, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x100); v != 0xDEADEFBE {
		t.Errorf("mem[0x100] = 0x%08X, want 0xDEADEFBE", v)
	}
}

// TestMovImmReg16 verifies C7 /0 with 0x66 prefix on a register destination.
func TestMovImmReg16(t *testing.T) {
	c := newTestCPU(t)
	// 66 C7 C0 BE EF  = MOV AX, 0xEFBE
	// F4              = HLT
	code := []byte{0x66, 0xC7, 0xC0, 0xBE, 0xEF, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xEFBE {
		t.Errorf("AX = 0x%04X, want 0xEFBE", v)
	}
	if v := c.GetReg32(EAX); v != 0xEFBE {
		t	.Errorf("EAX = 0x%08X, want 0x0000EFBE", v)
	}
}

// TestAddEaxImm16 verifies 0x05 with 0x66 prefix.
func TestAddEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340010)
	// 66 05 20 00  = ADD AX, 0x0020
	// F4           = HLT
	code := []byte{0x66, 0x05, 0x20, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x0030 {
		t.Errorf("AX = 0x%04X, want 0x0030", v)
	}
	// High 16 bits of EAX should be unchanged
	if v := c.GetReg32(EAX); v != 0x12340030 {
		t.Errorf("EAX = 0x%08X, want 0x12340030", v)
	}
}

// TestXorEaxImm16 verifies 0x35 with 0x66 prefix.
func TestXorEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1234FFFF)
	// 66 35 FF 00  = XOR AX, 0x00FF
	// F4           = HLT
	code := []byte{0x66, 0x35, 0xFF, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xFF00 {
		t.Errorf("AX = 0x%04X, want 0xFF00", v)
	}
}

// TestCmpEaxImm16 verifies 0x3D with 0x66 prefix.
func TestCmpEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340010)
	// 66 3D 10 00  = CMP AX, 0x0010
	// F4           = HLT
	code := []byte{0x66, 0x3D, 0x10, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF not set after CMP AX, 0x0010")
	}
}

// TestTestEaxImm16 verifies 0xA9 with 0x66 prefix.
func TestTestEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1234000F)
	// 66 A9 08 00  = TEST AX, 0x0008
	// F4           = HLT
	code := []byte{0x66, 0xA9, 0x08, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF set after TEST AX, 0x0008 (expected clear)")
	}
}

// TestAndEaxImm16 verifies 0x25 with 0x66 prefix.
func TestAndEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1234FFFF)
	// 66 25 0F 00  = AND AX, 0x000F
	// F4           = HLT
	code := []byte{0x66, 0x25, 0x0F, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x000F {
		t.Errorf("AX = 0x%04X, want 0x000F", v)
	}
}

// TestOrEaxImm16 verifies 0x0D with 0x66 prefix.
func TestOrEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x123400F0)
	// 66 0D 0F 00  = OR AX, 0x000F
	// F4           = HLT
	code := []byte{0x66, 0x0D, 0x0F, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x00FF {
		t.Errorf("AX = 0x%04X, want 0x00FF", v)
	}
}

// TestAdcEaxImm16 verifies 0x15 with 0x66 prefix.
func TestAdcEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x123400FF)
	c.setCF(true)
	// 66 15 01 00  = ADC AX, 0x0001
	// F4           = HLT
	code := []byte{0x66, 0x15, 0x01, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x0101 {
		t.Errorf("AX = 0x%04X, want 0x0101", v)
	}
}

// TestSbbEaxImm16 verifies 0x1D with 0x66 prefix.
func TestSbbEaxImm16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340100)
	c.setCF(true)
	// 66 1D 01 00  = SBB AX, 0x0001
	// F4           = HLT
	code := []byte{0x66, 0x1D, 0x01, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x00FE {
		t.Errorf("AX = 0x%04X, want 0x00FE", v)
	}
}

// TestGroup2Shift16 verifies C1 /n r/m16, imm8 with 0x66 prefix.
func TestGroup2Shift16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340001)
	// 66 C1 E0 04  = SHL AX, 4
	// F4           = HLT
	code := []byte{0x66, 0xC1, 0xE0, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x0010 {
		t.Errorf("AX = 0x%04X, want 0x0010", v)
	}
}

// TestGroup2Shift16Count1 verifies D1 /n r/m16, 1 with 0x66 prefix.
func TestGroup2Shift16Count1(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340001)
	// 66 D1 E0  = SHL AX, 1
	// F4        = HLT
	code := []byte{0x66, 0xD1, 0xE0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x0002 {
		t.Errorf("AX = 0x%04X, want 0x0002", v)
	}
}

// TestGroup3Test16 verifies F7 /0 r/m16, imm16 with 0x66 prefix.
func TestGroup3Test16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1234000F)
	// 66 F7 C0 08 00  = TEST AX, 0x0008
	// F4              = HLT
	code := []byte{0x66, 0xF7, 0xC0, 0x08, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF set after TEST AX, 0x0008")
	}
}

// TestGroup3Not16 verifies F7 /2 r/m16 with 0x66 prefix.
func TestGroup3Not16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x123400FF)
	// 66 F7 D0  = NOT AX
	// F4        = HLT
	code := []byte{0x66, 0xF7, 0xD0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xFF00 {
		t.Errorf("AX = 0x%04X, want 0xFF00", v)
	}
}

// TestGroup3Neg16 verifies F7 /3 r/m16 with 0x66 prefix.
func TestGroup3Neg16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340001)
	// 66 F7 D8  = NEG AX
	// F4        = HLT
	code := []byte{0x66, 0xF7, 0xD8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xFFFF {
		t.Errorf("AX = 0x%04X, want 0xFFFF", v)
	}
}

// TestGroup5Inc16 verifies FF /0 r/m16 with 0x66 prefix.
func TestGroup5Inc16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x123400FF)
	// 66 FF C0  = INC AX
	// F4        = HLT
	code := []byte{0x66, 0xFF, 0xC0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x0100 {
		t.Errorf("AX = 0x%04X, want 0x0100", v)
	}
}

// TestGroup5Dec16 verifies FF /1 r/m16 with 0x66 prefix.
func TestGroup5Dec16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340100)
	// 66 FF C8  = DEC AX
	// F4        = HLT
	code := []byte{0x66, 0xFF, 0xC8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x00FF {
		t.Errorf("AX = 0x%04X, want 0x00FF", v)
	}
}

// TestGroup5Push16 verifies FF /6 r/m16 with 0x66 prefix.
func TestGroup5Push16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x200)
	c.SetReg32(EAX, 0x1234BEEF)
	// 66 FF F0  = PUSH AX
	// F4        = HLT
	code := []byte{0x66, 0xFF, 0xF0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(ESP); v != 0x1FE {
		t.Errorf("ESP = 0x%08X, want 0x1FE", v)
	}
	if v := c.readMem16(0x1FE); v != 0xBEEF {
		t.Errorf("mem[0x1FE] = 0x%04X, want 0xBEEF", v)
	}
}
