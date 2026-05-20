package x86_64

// FXSAVE / FXRSTOR round-trip tests for the long-mode backend. Ported
// from cpu/x86/fxsave_test.go. The long-mode FXSAVE area covers XMM0..
// XMM15 (16 × 16 bytes at offset 160) — the i386 version only covers
// XMM0..XMM7.

import "testing"

// TestFxsaveFxrstor_RoundTrip directly drives fxsave/fxrstor (not via
// the opcode) to verify the persistence path itself.
func TestFxsaveFxrstor_RoundTrip(t *testing.T) {
	c, _ := longModeCPU(t)

	c.fpu[0] = 3.14159
	c.fpu[1] = 2.71828
	c.fpu[2] = 1.41421
	c.fpuTop = 5
	c.fpuStatusWord = 0
	c.fpuStatusWriteTop()
	c.fpuTag = 0xFFC0
	c.fpuControlWord = 0x027F
	c.mxcsr = 0x00001F80

	c.xmm[0] = [2]uint64{0x1111111111111111, 0x2222222222222222}
	c.xmm[1] = [2]uint64{0x3333333333333333, 0x4444444444444444}
	c.xmm[3] = [2]uint64{0xDEADBEEFDEADBEEF, 0xCAFEBABECAFEBABE}
	// Long-mode adds XMM8..XMM15. Exercise the upper half too.
	c.xmm[10] = [2]uint64{0xAAAAAAAAAAAAAAAA, 0xBBBBBBBBBBBBBBBB}
	c.xmm[15] = [2]uint64{0xCCCCCCCCCCCCCCCC, 0xDDDDDDDDDDDDDDDD}

	c.fxsave(0x2000)

	// Clobber.
	for i := range c.fpu {
		c.fpu[i] = 0
	}
	c.fpuTop = 0
	c.fpuStatusWord = 0
	c.fpuTag = 0
	c.fpuControlWord = 0
	c.mxcsr = 0
	for i := range c.xmm {
		c.xmm[i] = [2]uint64{}
	}

	c.fxrstor(0x2000)

	if c.fpuControlWord != 0x027F {
		t.Errorf("FCW after restore = %04X, want 0x027F", c.fpuControlWord)
	}
	if c.mxcsr != 0x00001F80 {
		t.Errorf("MXCSR after restore = %08X, want 0x1F80", c.mxcsr)
	}
	if c.fpuTop != 5 {
		t.Errorf("fpuTop after restore = %d, want 5", c.fpuTop)
	}
	tolerance := 1e-9
	if diff := c.fpu[0] - 3.14159; diff > tolerance || diff < -tolerance {
		t.Errorf("fpu[0] = %v, want 3.14159", c.fpu[0])
	}
	if diff := c.fpu[1] - 2.71828; diff > tolerance || diff < -tolerance {
		t.Errorf("fpu[1] = %v, want 2.71828", c.fpu[1])
	}
	if c.xmm[0][0] != 0x1111111111111111 || c.xmm[0][1] != 0x2222222222222222 {
		t.Errorf("xmm[0] = %016X_%016X", c.xmm[0][1], c.xmm[0][0])
	}
	if c.xmm[3][0] != 0xDEADBEEFDEADBEEF || c.xmm[3][1] != 0xCAFEBABECAFEBABE {
		t.Errorf("xmm[3] = %016X_%016X", c.xmm[3][1], c.xmm[3][0])
	}
	// Long-mode-only XMM registers must round-trip too.
	if c.xmm[10][0] != 0xAAAAAAAAAAAAAAAA || c.xmm[10][1] != 0xBBBBBBBBBBBBBBBB {
		t.Errorf("xmm[10] = %016X_%016X (high half not preserved)",
			c.xmm[10][1], c.xmm[10][0])
	}
	if c.xmm[15][0] != 0xCCCCCCCCCCCCCCCC || c.xmm[15][1] != 0xDDDDDDDDDDDDDDDD {
		t.Errorf("xmm[15] = %016X_%016X", c.xmm[15][1], c.xmm[15][0])
	}
}

// TestFxsave_FullInstructionForm wires the round-trip through the
// real 0F AE /0 dispatcher in opGroup15.
func TestFxsave_FullInstructionForm(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg64(RBX, 0x2000)
	c.fpuControlWord = 0xDEAD
	// FXSAVE [rbx] — 0F AE 03 (ModRM 03 = mod=00 reg=000 rm=011)
	runMMXCode(t, c, mm, []byte{0x0F, 0xAE, 0x03, 0xF4})
	got, _ := mm.Read16(0x2000)
	if got != 0xDEAD {
		t.Errorf("FCW saved at offset 0 = %04X, want 0xDEAD", got)
	}
}
