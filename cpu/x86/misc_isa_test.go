package x86

import "testing"

// TestMultiByteNOP exercises the `0F 1F /0..7` family.
func TestMultiByteNOP(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	// 8-byte NOP: 0F 1F 84 00 00 00 00 00
	code := []byte{0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want preserved 0xDEADBEEF", got)
	}
}

// TestWAIT_AsNOP confirms 0x9B (WAIT/FWAIT) does nothing.
func TestWAIT_AsNOP(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xABCD1234)
	code := []byte{0x9B, 0xF4} // WAIT; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xABCD1234 {
		t.Errorf("EAX = 0x%08X, want preserved", got)
	}
}

// TestX87Stub verifies an FNINIT-like instruction is consumed without error.
// We don't model x87 state; just confirm the decoder doesn't trap.
func TestX87Stub(t *testing.T) {
	c := newTestCPU(t)
	// DB E3 = FNINIT; followed by HLT.
	code := []byte{0xDB, 0xE3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
}

// TestCLTS clears CR0.TS in ring 0.
func TestCLTS(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)|CR0_TS)
	code := []byte{0x0F, 0x06, 0xF4} // CLTS; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.GetCR(0)&CR0_TS != 0 {
		t.Errorf("CR0.TS still set after CLTS")
	}
}

// TestINVD_WBINVD treat as NOPs at CPL=0.
func TestINVD_WBINVD(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{0x0F, 0x08, 0x0F, 0x09, 0xF4} // INVD; WBINVD; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
}

// TestMOVDR validates write+read of DR0.
func TestMOVDR(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	// MOV DR0, EAX = 0F 23 C0
	// MOV EBX, DR0 = 0F 21 C3
	code := []byte{0x0F, 0x23, 0xC0, 0x0F, 0x21, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EBX); got != 0x12345678 {
		t.Errorf("EBX = 0x%08X, want 0x12345678", got)
	}
}

// setupFlatGDT places a single flat data descriptor at selector 0x10 so
// LSS/LFS/LGS tests can load a valid segment in protected mode.
func setupFlatDataSelector(c *CPU) {
	gdtBase := uint32(0x4000)
	for i := 0; i < 24; i++ {
		c.writeMem8(gdtBase+uint32(i), 0)
	}
	c.writeMem8(gdtBase+16, 0xFF)
	c.writeMem8(gdtBase+17, 0xFF)
	c.writeMem8(gdtBase+18, 0x00)
	c.writeMem8(gdtBase+19, 0x00)
	c.writeMem8(gdtBase+20, 0x00)
	c.writeMem8(gdtBase+21, 0x92)
	c.writeMem8(gdtBase+22, 0xCF)
	c.writeMem8(gdtBase+23, 0x00)
	c.SetSegBase(GDTR, gdtBase)
	c.SetSegLimit(GDTR, 23)
}

func TestLSS(t *testing.T) {
	c := newTestCPU(t)
	setupFlatDataSelector(c)
	c.writeMem32(0x5000, 0xCAFEBABE)
	c.writeMem16(0x5004, 0x0010)
	// LSS ESP, [DS:0x5000] = 0F B2 25 00 50 00 00
	code := []byte{0x0F, 0xB2, 0x25, 0x00, 0x50, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0xCAFEBABE {
		t.Errorf("ESP = 0x%08X, want 0xCAFEBABE", got)
	}
	if got := c.GetSeg(SS); got != 0x0010 {
		t.Errorf("SS = 0x%04X, want 0x0010", got)
	}
}

func TestLFS(t *testing.T) {
	c := newTestCPU(t)
	setupFlatDataSelector(c)
	c.writeMem32(0x5000, 0x11223344)
	c.writeMem16(0x5004, 0x0010)
	// LFS EDI, [DS:0x5000] = 0F B4 3D 00 50 00 00
	code := []byte{0x0F, 0xB4, 0x3D, 0x00, 0x50, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EDI); got != 0x11223344 {
		t.Errorf("EDI = 0x%08X, want 0x11223344", got)
	}
	if got := c.GetSeg(FS); got != 0x0010 {
		t.Errorf("FS = 0x%04X, want 0x0010", got)
	}
}

func TestLGS(t *testing.T) {
	c := newTestCPU(t)
	setupFlatDataSelector(c)
	c.writeMem32(0x5000, 0xAABBCCDD)
	c.writeMem16(0x5004, 0x0010)
	// LGS ESI, [DS:0x5000] = 0F B5 35 00 50 00 00
	code := []byte{0x0F, 0xB5, 0x35, 0x00, 0x50, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(ESI); got != 0xAABBCCDD {
		t.Errorf("ESI = 0x%08X, want 0xAABBCCDD", got)
	}
	if got := c.GetSeg(GS); got != 0x0010 {
		t.Errorf("GS = 0x%04X, want 0x0010", got)
	}
}
