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

// TestLLDT tests LLDT r/m16 (0F 00 /2) loads the LDTR from a GDT descriptor.
func TestLLDT(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	for i := 0; i < 64; i++ {
		c.writeMem8(gdtBase+uint32(i), 0)
	}

	// Null descriptor at offset 0.
	// Code segment at offset 8 (selector 0x08).
	c.writeMem8(gdtBase+8, 0xFF)
	c.writeMem8(gdtBase+9, 0xFF)
	c.writeMem8(gdtBase+10, 0x00)
	c.writeMem8(gdtBase+11, 0x00)
	c.writeMem8(gdtBase+12, 0x00)
	c.writeMem8(gdtBase+13, 0x9A)
	c.writeMem8(gdtBase+14, 0xCF)
	c.writeMem8(gdtBase+15, 0x00)
	// Data segment at offset 16 (selector 0x10).
	c.writeMem8(gdtBase+16, 0xFF)
	c.writeMem8(gdtBase+17, 0xFF)
	c.writeMem8(gdtBase+18, 0x00)
	c.writeMem8(gdtBase+19, 0x00)
	c.writeMem8(gdtBase+20, 0x00)
	c.writeMem8(gdtBase+21, 0x92)
	c.writeMem8(gdtBase+22, 0xCF)
	c.writeMem8(gdtBase+23, 0x00)
	// LDT descriptor at offset 40 (selector 0x28): base=0x3000, limit=0xFF.
	c.writeMem8(gdtBase+40, 0xFF)
	c.writeMem8(gdtBase+41, 0x00)
	c.writeMem8(gdtBase+42, 0x00)
	c.writeMem8(gdtBase+43, 0x30)
	c.writeMem8(gdtBase+44, 0x00)
	c.writeMem8(gdtBase+45, 0x82) // P=1, DPL=0, type=0x02 (LDT)
	c.writeMem8(gdtBase+46, 0x00)
	c.writeMem8(gdtBase+47, 0x00)

	enterProtectedMode(t, c, gdtBase, 0x4000, 0x0F*8+7, 0x5000)
	c.segLimit[GDTR] = 0x002F // 6 entries

	c.SetReg16(AX, 0x0028)
	// 0F 00 D0 = LLDT AX
	c.writeMem8(0x5000, 0x0F)
	c.writeMem8(0x5001, 0x00)
	c.writeMem8(0x5002, 0xD0)
	c.writeMem8(0x5003, 0xF4)
	c.SetEIP(0x5000)
	c.SetSeg(CS, 0x0008)
	c.SetSegBase(CS, 0x00000)

	if err := c.Step(); err != nil {
		t.Fatalf("LLDT step error: %v", err)
	}
	if err := c.Step(); err != nil {
		t.Fatalf("HLT step error: %v", err)
	}

	if c.seg[LDTR] != 0x0028 {
		t.Errorf("LDTR = 0x%04X, want 0x0028", c.seg[LDTR])
	}
	if c.segBase[LDTR] != 0x3000 {
		t.Errorf("LDTR base = 0x%08X, want 0x00003000", c.segBase[LDTR])
	}
	if c.segLimit[LDTR] != 0xFF {
		t.Errorf("LDTR limit = 0x%08X, want 0x000000FF", c.segLimit[LDTR])
	}
}

// TestLTR tests LTR r/m16 (0F 00 /3) loads the TR from a GDT TSS descriptor.
func TestLTR(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	for i := 0; i < 64; i++ {
		c.writeMem8(gdtBase+uint32(i), 0)
	}

	// Null descriptor at offset 0.
	// Code segment at offset 8 (selector 0x08).
	c.writeMem8(gdtBase+8, 0xFF)
	c.writeMem8(gdtBase+9, 0xFF)
	c.writeMem8(gdtBase+10, 0x00)
	c.writeMem8(gdtBase+11, 0x00)
	c.writeMem8(gdtBase+12, 0x00)
	c.writeMem8(gdtBase+13, 0x9A)
	c.writeMem8(gdtBase+14, 0xCF)
	c.writeMem8(gdtBase+15, 0x00)
	// Data segment at offset 16 (selector 0x10).
	c.writeMem8(gdtBase+16, 0xFF)
	c.writeMem8(gdtBase+17, 0xFF)
	c.writeMem8(gdtBase+18, 0x00)
	c.writeMem8(gdtBase+19, 0x00)
	c.writeMem8(gdtBase+20, 0x00)
	c.writeMem8(gdtBase+21, 0x92)
	c.writeMem8(gdtBase+22, 0xCF)
	c.writeMem8(gdtBase+23, 0x00)
	// TSS descriptor at offset 48 (selector 0x30): base=0x4000, limit=0x67.
	c.writeMem8(gdtBase+48, 0x67)
	c.writeMem8(gdtBase+49, 0x00)
	c.writeMem8(gdtBase+50, 0x00)
	c.writeMem8(gdtBase+51, 0x40)
	c.writeMem8(gdtBase+52, 0x00)
	c.writeMem8(gdtBase+53, 0x89) // P=1, DPL=0, type=0x09 (32-bit available TSS)
	c.writeMem8(gdtBase+54, 0x40)
	c.writeMem8(gdtBase+55, 0x00)

	enterProtectedMode(t, c, gdtBase, 0x5000, 0x0F*8+7, 0x6000)
	c.segLimit[GDTR] = 0x0037 // 7 entries

	c.SetReg16(AX, 0x0030)
	// 0F 00 D8 = LTR AX
	c.writeMem8(0x6000, 0x0F)
	c.writeMem8(0x6001, 0x00)
	c.writeMem8(0x6002, 0xD8)
	c.writeMem8(0x6003, 0xF4)
	c.SetEIP(0x6000)
	c.SetSeg(CS, 0x0008)
	c.SetSegBase(CS, 0x00000)

	if err := c.Step(); err != nil {
		t.Fatalf("LTR step error: %v", err)
	}
	if err := c.Step(); err != nil {
		t.Fatalf("HLT step error: %v", err)
	}

	if c.seg[TR] != 0x0030 {
		t.Errorf("TR = 0x%04X, want 0x0030", c.seg[TR])
	}
	if c.segBase[TR] != 0x4000 {
		t.Errorf("TR base = 0x%08X, want 0x00004000", c.segBase[TR])
	}
	if c.segLimit[TR] != 0x67 {
		t.Errorf("TR limit = 0x%08X, want 0x00000067", c.segLimit[TR])
	}
	// TSS should be marked busy (type 0x0B).
	if got := c.readMem8(gdtBase + 53); got != 0x8B {
		t.Errorf("TSS access byte = 0x%02X, want 0x8B (busy)", got)
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
