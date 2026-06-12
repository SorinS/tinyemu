package x86_64

import "testing"

// Regression tests for three correctness bugs surfaced by the code-review
// report: 8-bit MUL/IMUL writing the high byte to DL instead of AH, LMSW
// being able to clear CR0.PE, and INVLPG invalidating the effective
// address instead of the linear address.

// TestMul8WritesAH: the 8-bit MUL (F6 /4) result is a 16-bit product that
// lands entirely in AX (AH:AL). The high byte must go to AH, never DL.
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

// TestImul8WritesAH: 8-bit IMUL (F6 /5) signed product fills AX; the high
// byte goes to AH, not DL. CF/OF set when it doesn't fit a signed byte.
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

// TestLMSWCannotClearPE: LMSW may set CR0.PE but must never clear it — there
// is no escape from protected mode via LMSW. It must still update MP/EM/TS.
func TestLMSWCannotClearPE(t *testing.T) {
	c, mm := longModeCPU(t)
	c.cr[0] |= CR0_PE  // already in protected mode
	c.cr[0] &^= CR0_TS // TS starts clear
	// MSW source word 0x0008 (TS=1, PE=0) in memory at [RAX].
	c.SetReg64(RAX, 0x4000)
	_ = mm.Write8(0x4000, 0x08)
	_ = mm.Write8(0x4001, 0x00)
	// 0F 01 30 = LMSW [RAX] (ModRM mod=00 reg=6 rm=0)
	if err := runInsn(t, c, mm, []byte{0x0F, 0x01, 0x30}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if c.cr[0]&CR0_PE == 0 {
		t.Errorf("LMSW cleared CR0.PE (illegal — cannot leave protected mode)")
	}
	if c.cr[0]&CR0_TS == 0 {
		t.Errorf("LMSW failed to set CR0.TS (the legal bits must still update)")
	}
}

// TestInvlpgUsesLinearAddress: INVLPG must invalidate the operand's linear
// address (segment base + effective address), not the bare effective
// address. With a non-zero FS base the two differ; only the linear entry
// should be dropped.
func TestInvlpgUsesLinearAddress(t *testing.T) {
	c, mm := longModeCPU(t)
	const fsBase = uint64(0x40000)
	const ea = uint64(0x2000)
	lin := fsBase + ea // 0x42000

	c.segBase[FS] = fsBase
	c.SetReg64(RAX, ea)

	// Seed two distinct TLB entries: one at the linear address, one at the
	// bare EA. The fix must drop the linear one and leave the EA one.
	c.tlb.insert(lin, 0xAA000, true, false, false, false)
	c.tlb.insert(ea, 0xBB000, true, false, false, false)

	// 64 0F 01 38 = INVLPG FS:[RAX]  (0x64 = FS override, ModRM 0x38 = [RAX])
	if err := runInsn(t, c, mm, []byte{0x64, 0x0F, 0x01, 0x38}); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, ok := c.tlb.lookup(lin, false, false, false); ok {
		t.Errorf("INVLPG did not invalidate the linear address %#x", lin)
	}
	if _, ok := c.tlb.lookup(ea, false, false, false); !ok {
		t.Errorf("INVLPG wrongly invalidated the bare EA %#x instead of the linear address", ea)
	}
}
