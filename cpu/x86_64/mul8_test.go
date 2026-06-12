package x86_64

import "testing"

// 8-bit MUL/IMUL (F6 /4, /5) produce a 16-bit product that lands entirely
// in AX (AH:AL) — the high byte must go to AH, never DL. These regression
// tests pin that, plus the CF/OF "did the product fit" semantics.

// TestMul8WritesAH: 8-bit MUL high byte goes to AH, not DL; DX is untouched.
func TestMul8WritesAH(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg64(RAX, 0xFF)   // AL = 0xFF
	c.SetReg64(RBX, 0xFF)   // BL = 0xFF
	c.SetReg64(RDX, 0xAAAA) // sentinel: DL/DX must be untouched
	// F6 E3 = MUL BL  (ModRM mod=11 reg=4 rm=3)
	if err := runInsn(t, c, mm, []byte{0xF6, 0xE3}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := uint16(c.GetReg64(RAX)); got != 0xFE01 { // 255*255 = 0xFE01
		t.Errorf("AX = %#04x, want 0xFE01 (AH:AL)", got)
	}
	if got := uint16(c.GetReg64(RDX)); got != 0xAAAA {
		t.Errorf("DX = %#04x, want 0xAAAA unchanged (8-bit MUL must not touch DX)", got)
	}
	if c.rflags&RFLAGS_CF == 0 || c.rflags&RFLAGS_OF == 0 {
		t.Errorf("CF/OF should be set when AH != 0 (rflags=%#x)", c.rflags)
	}
}

// TestMul8FitsClearsCarry: when the product fits in AL (AH == 0), CF/OF clear.
func TestMul8FitsClearsCarry(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg64(RAX, 0x10) // AL = 16
	c.SetReg64(RBX, 0x0F) // BL = 15
	if err := runInsn(t, c, mm, []byte{0xF6, 0xE3}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := uint16(c.GetReg64(RAX)); got != 0x00F0 { // 16*15 = 240
		t.Errorf("AX = %#04x, want 0x00F0", got)
	}
	if c.rflags&RFLAGS_CF != 0 || c.rflags&RFLAGS_OF != 0 {
		t.Errorf("CF/OF should be clear when AH == 0 (rflags=%#x)", c.rflags)
	}
}

// TestImul8WritesAH: 8-bit IMUL signed product fills AX; the high byte goes
// to AH, not DL. CF/OF set when it doesn't fit a signed byte.
func TestImul8WritesAH(t *testing.T) {
	// (-1) * (-1) = 1, fits a signed byte → AX=0x0001, CF/OF clear.
	c, mm := longModeCPU(t)
	c.SetReg64(RAX, 0xFF)                                         // AL = -1
	c.SetReg64(RBX, 0xFF)                                         // BL = -1
	c.SetReg64(RDX, 0xAAAA)                                       // sentinel
	if err := runInsn(t, c, mm, []byte{0xF6, 0xEB}); err != nil { // IMUL BL
		t.Fatalf("Step: %v", err)
	}
	if got := uint16(c.GetReg64(RAX)); got != 0x0001 {
		t.Errorf("AX = %#04x, want 0x0001", got)
	}
	if got := uint16(c.GetReg64(RDX)); got != 0xAAAA {
		t.Errorf("DX = %#04x, want 0xAAAA unchanged", got)
	}
	if c.rflags&RFLAGS_CF != 0 || c.rflags&RFLAGS_OF != 0 {
		t.Errorf("CF/OF should be clear: -1*-1 fits a byte (rflags=%#x)", c.rflags)
	}

	// 127 * 127 = 16129 (0x3F01), does NOT fit a signed byte → CF/OF set,
	// and the high byte 0x3F must be in AH.
	c, mm = longModeCPU(t)
	c.SetReg64(RAX, 0x7F)
	c.SetReg64(RBX, 0x7F)
	if err := runInsn(t, c, mm, []byte{0xF6, 0xEB}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := uint16(c.GetReg64(RAX)); got != 0x3F01 {
		t.Errorf("AX = %#04x, want 0x3F01 (AH=0x3F)", got)
	}
	if c.rflags&RFLAGS_CF == 0 || c.rflags&RFLAGS_OF == 0 {
		t.Errorf("CF/OF should be set: 127*127 overflows a signed byte (rflags=%#x)", c.rflags)
	}
}
