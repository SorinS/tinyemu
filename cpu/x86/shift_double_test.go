package x86

import "testing"

// 0F A4 SHLD r/m32, r32, imm8
func TestSHLDRegRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x12345678)
	c.SetReg32(ECX, 0xABCDEF01)
	// SHLD EBX, ECX, 4
	// result = (0x12345678 << 4) | (0xABCDEF01 >> 28)
	//        = 0x23456780 | 0xA = 0x2345678A
	code := []byte{0x0F, 0xA4, 0xCB, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x2345678A {
		t.Errorf("EBX = 0x%08X, want 0x2345678A", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 31 of original EBX was 0)")
	}
	if c.getZF() {
		t.Error("expected ZF=0")
	}
	if c.getSF() {
		t.Error("expected SF=0 (bit 31 of result is 0)")
	}
}

// 0F A5 SHLD r/m32, r32, CL
func TestSHLDRegRegCL32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x87654321)
	c.SetReg32(ECX, 0x11111111)
	c.SetReg8(CL, 8)
	// SHLD EBX, ECX, CL
	// result = (0x87654321 << 8) | (0x11111111 >> 24)
	//        = 0x65432100 | 0x11 = 0x65432111
	code := []byte{0x0F, 0xA5, 0xCB, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x65432111 {
		t.Errorf("EBX = 0x%08X, want 0x65432111", v)
	}
}

// 0F AC SHRD r/m32, r32, imm8
func TestSHRDRegRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x12345678)
	c.SetReg32(ECX, 0xABCDEF01)
	// SHRD EBX, ECX, 4
	// result = (0x12345678 >> 4) | (0xABCDEF01 << 28)
	//        = 0x01234567 | 0x10000000 = 0x11234567
	code := []byte{0x0F, 0xAC, 0xCB, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x11234567 {
		t.Errorf("EBX = 0x%08X, want 0x11234567", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 3 of original EBX was 1)")
	}
}

// 0F AD SHRD r/m32, r32, CL
func TestSHRDRegRegCL32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x87654321)
	c.SetReg32(ECX, 0x22222222)
	c.SetReg8(CL, 8)
	// SHRD EBX, ECX, CL
	// Note: ECX becomes 0x22222208 after setting CL=8
	// result = (0x87654321 >> 8) | (0x22222208 << 24)
	//        = 0x00876543 | 0x08000000 = 0x08876543
	code := []byte{0x0F, 0xAD, 0xCB, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x08876543 {
		t.Errorf("EBX = 0x%08X, want 0x08876543", v)
	}
}

// SHLD count=0 should not affect flags
func TestSHLDCountZero32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x80000000)
	c.SetReg32(ECX, 0x12345678)
	c.setCF(false)
	c.setZF(true)
	c.setSF(false)
	// SHLD EBX, ECX, 0
	code := []byte{0x0F, 0xA4, 0xCB, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x80000000 {
		t.Errorf("EBX = 0x%08X, want 0x80000000", v)
	}
	if c.getCF() {
		t.Error("expected CF unchanged (false)")
	}
	if !c.getZF() {
		t.Error("expected ZF unchanged (true)")
	}
	if c.getSF() {
		t.Error("expected SF unchanged (false)")
	}
}

// SHRD count=0 should not affect flags
func TestSHRDCountZero32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000001)
	c.SetReg32(ECX, 0x12345678)
	c.setCF(false)
	c.setZF(true)
	c.setSF(false)
	// SHRD EBX, ECX, 0
	code := []byte{0x0F, 0xAC, 0xCB, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x00000001 {
		t.Errorf("EBX = 0x%08X, want 0x00000001", v)
	}
	if c.getCF() {
		t.Error("expected CF unchanged (false)")
	}
	if !c.getZF() {
		t.Error("expected ZF unchanged (true)")
	}
	if c.getSF() {
		t.Error("expected SF unchanged (false)")
	}
}

// SHLD memory form
func TestSHLDMemRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0x12345678)
	c.SetReg32(ECX, 0xABCDEF01)
	// SHRD [0x2000], ECX, 4
	code := []byte{0x0F, 0xA4, 0x0D, 0x00, 0x20, 0x00, 0x00, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0x2345678A {
		t.Errorf("mem[0x2000] = 0x%08X, want 0x2345678A", v)
	}
}

// SHRD memory form
func TestSHRDMemRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0x12345678)
	c.SetReg32(ECX, 0xABCDEF01)
	// SHRD [0x2000], ECX, 4
	code := []byte{0x0F, 0xAC, 0x0D, 0x00, 0x20, 0x00, 0x00, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0x11234567 {
		t.Errorf("mem[0x2000] = 0x%08X, want 0x11234567", v)
	}
}

// 16-bit SHLD
func TestSHLDRegRegImm16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0x1234)
	c.SetReg16(CX, 0xABCD)
	// SHLD BX, CX, 4
	// result = (0x1234 << 4) | (0xABCD >> 12)
	//        = 0x2340 | 0xA = 0x234A
	code := []byte{0x0F, 0xA4, 0xCB, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(BX); v != 0x234A {
		t.Errorf("BX = 0x%04X, want 0x234A", v)
	}
}

// 16-bit SHRD
func TestSHRDRegRegImm16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0x1234)
	c.SetReg16(CX, 0xABCD)
	// SHRD BX, CX, 4
	// result = (0x1234 >> 4) | (0xABCD << 12)
	//        = 0x0123 | 0xD000 = 0xD123
	code := []byte{0x0F, 0xAC, 0xCB, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(BX); v != 0xD123 {
		t.Errorf("BX = 0x%04X, want 0xD123", v)
	}
}

// SHLD with count > operand size (should be masked to 0x1F)
func TestSHLDCountMask32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000001)
	c.SetReg32(ECX, 0x80000000)
	// SHLD EBX, ECX, 32 (stored as 0x20, masked to 0)
	code := []byte{0x0F, 0xA4, 0xCB, 0x20, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x00000001 {
		t.Errorf("EBX = 0x%08X, want 0x00000001", v)
	}
}

// SHRD with count > operand size (should be masked to 0x1F)
func TestSHRDCountMask32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x80000000)
	c.SetReg32(ECX, 0x00000001)
	// SHRD EBX, ECX, 32 (stored as 0x20, masked to 0)
	code := []byte{0x0F, 0xAC, 0xCB, 0x20, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x80000000 {
		t.Errorf("EBX = 0x%08X, want 0x80000000", v)
	}
}

// SHLD count=1 sets OF correctly
func TestSHLDCountOneOF32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x40000000) // bit 30 = 1, bit 31 = 0
	c.SetReg32(ECX, 0x00000000)
	// SHLD EBX, ECX, 1
	// result = 0x80000000, bit 31 changed from 0 to 1 -> OF = 1
	code := []byte{0x0F, 0xA4, 0xCB, 0x01, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getOF() {
		t.Error("expected OF=1 (MSB changed)")
	}
}

// SHRD count=1 sets OF correctly
func TestSHRDCountOneOF32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x40000000) // bit 30 = 1, bit 31 = 0
	c.SetReg32(ECX, 0x00000000)
	// SHRD EBX, ECX, 1
	// result = 0x20000000, bit 31 changed from 0 to 0 -> OF = 0
	code := []byte{0x0F, 0xAC, 0xCB, 0x01, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getOF() {
		t.Error("expected OF=0 (MSB unchanged)")
	}
}
