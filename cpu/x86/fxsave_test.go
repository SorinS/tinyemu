package x86

import "testing"

// TestFxsaveFxrstor_RoundTrip exercises the round-trip:
//   - Set FPU + XMM state.
//   - FXSAVE to memory.
//   - Clobber FPU + XMM state.
//   - FXRSTOR from memory.
//   - Verify state restored.
//
// Previously FXSAVE/FXRSTOR were NOPs. The kernel uses them on every
// task switch to preserve per-task FPU state. Without real impls, a
// long-running FP user (e.g. busybox awk) had its FPU registers
// corrupted whenever it was preempted — manifesting as `if (...)`
// statements taking random branches, because awk evaluates booleans
// through the x87 stack and that stack got clobbered.
func TestFxsaveFxrstor_RoundTrip(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x2000) // FXSAVE area

	// Seed FPU stack with known values via FLD1, FLDZ, FLDPI.
	c.fpu[0] = 3.14159
	c.fpu[1] = 2.71828
	c.fpu[2] = 1.41421
	c.fpu[3] = 0
	c.fpu[4] = 0
	c.fpu[5] = 0
	c.fpu[6] = 0
	c.fpu[7] = 0
	c.fpuTop = 5
	c.fpuStatusWord = 0
	c.fpuStatusWriteTop()
	c.fpuTag = 0xFFC0 // ST(0..2) valid, rest empty (just an example)
	c.fpuControlWord = 0x027F
	c.mxcsr = 0x00001F80

	c.xmm[0] = [2]uint64{0x1111111111111111, 0x2222222222222222}
	c.xmm[1] = [2]uint64{0x3333333333333333, 0x4444444444444444}
	c.xmm[3] = [2]uint64{0xDEADBEEFDEADBEEF, 0xCAFEBABECAFEBABE}

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
	// Check ST(0..2). FPU values stored as 80-bit then read back; tolerate
	// rounding artefacts on the order of 1e-12 from the float64 ⇄ 80-bit
	// round-trip.
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
}

// TestFxsave_FullInstructionForm wires it through the actual 0F AE /0
// dispatcher to make sure the case-0 branch routes correctly.
func TestFxsave_FullInstructionForm(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x2000)
	c.fpuControlWord = 0xDEAD
	// 0F AE /0   FXSAVE [ebx]    bytes: 0F AE 03    ModRM 03 = mod=00 reg=000 rm=011
	code := []byte{0x0F, 0xAE, 0x03, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.readMem16(0x2000); got != 0xDEAD {
		t.Errorf("FCW saved at offset 0 = %04X, want 0xDEAD", got)
	}
}
