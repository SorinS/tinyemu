package x86_64

// PSHUF / PSLLDQ / PSRLDQ / immediate-count-shift / MXCSR tests for
// the long-mode backend. Ported from cpu/x86/pshuf_test.go. The
// addressing examples that used 32-bit registers (EBX, ESP, EDI) are
// adapted to 64-bit equivalents — long mode forces 64-bit addressing
// by default.

import "testing"

// TestPSHUFW_Identity: 0F 70 /r imm8=0xE4 — pass-through.
func TestPSHUFW_Identity(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[3] = 0x4444333322221111
	// PSHUFW MM0, MM3, 0xE4 = 0F 70 C3 E4 (mod=11 reg=000 rm=011 imm=0xE4)
	runMMXCode(t, c, mm, []byte{0x0F, 0x70, 0xC3, 0xE4, 0xF4})
	if c.mm[0] != 0x4444333322221111 {
		t.Errorf("mm0 = %016X", c.mm[0])
	}
}

// TestPSHUFW_Reverse: imm8=0x1B reverses 4 word lanes.
func TestPSHUFW_Reverse(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[3] = 0x4444333322221111
	runMMXCode(t, c, mm, []byte{0x0F, 0x70, 0xC3, 0x1B, 0xF4})
	want := uint64(0x1111222233334444)
	if c.mm[0] != want {
		t.Errorf("mm0 = %016X, want %016X", c.mm[0], want)
	}
}

// TestPSHUFD: 66 0F 70 /r imm8 — SSE2 shuffle of 4 32-bit lanes.
func TestPSHUFD(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[3] = [2]uint64{
		uint64(0x22222222)<<32 | 0x11111111,
		uint64(0x44444444)<<32 | 0x33333333,
	}
	// PSHUFD XMM0, XMM3, 0x1B — reverse all 4 dwords
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0x70, 0xC3, 0x1B, 0xF4})
	wantLo := uint64(0x33333333)<<32 | 0x44444444
	wantHi := uint64(0x11111111)<<32 | 0x22222222
	if c.xmm[0][0] != wantLo || c.xmm[0][1] != wantHi {
		t.Errorf("PSHUFD reverse: xmm0 = %016X_%016X, want %016X_%016X",
			c.xmm[0][1], c.xmm[0][0], wantHi, wantLo)
	}
}

// TestPSHUFLW: F2 0F 70 — shuffle low 4 words, high 64 bits pass through.
func TestPSHUFLW(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[3] = [2]uint64{
		0x4444333322221111,
		0xCCCCBBBBAAAA9999,
	}
	runMMXCode(t, c, mm, []byte{0xF2, 0x0F, 0x70, 0xC3, 0x1B, 0xF4})
	if c.xmm[0][0] != 0x1111222233334444 {
		t.Errorf("low shuffled = %016X", c.xmm[0][0])
	}
	if c.xmm[0][1] != 0xCCCCBBBBAAAA9999 {
		t.Errorf("high passthrough = %016X", c.xmm[0][1])
	}
}

// TestPSHUFHW: F3 0F 70 — shuffle high 4 words, low 64 bits pass through.
func TestPSHUFHW(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[3] = [2]uint64{
		0x4444333322221111,
		0xCCCCBBBBAAAA9999,
	}
	runMMXCode(t, c, mm, []byte{0xF3, 0x0F, 0x70, 0xC3, 0x1B, 0xF4})
	if c.xmm[0][0] != 0x4444333322221111 {
		t.Errorf("low passthrough = %016X", c.xmm[0][0])
	}
	if c.xmm[0][1] != 0x9999AAAABBBBCCCC {
		t.Errorf("high shuffled = %016X", c.xmm[0][1])
	}
}

// TestPSLLDQ: 66 0F 73 /7 — byte-shift the full 128-bit XMM left.
func TestPSLLDQ(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[2] = [2]uint64{0x0807060504030201, 0x100F0E0D0C0B0A09}
	// 66 0F 73 /7 rm=2 imm=1 — ModRM = 11_111_010 = 0xFA
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0x73, 0xFA, 0x01, 0xF4})
	if c.xmm[2][0] != 0x0706050403020100 {
		t.Errorf("PSLLDQ low = %016X", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0x0F0E0D0C0B0A0908 {
		t.Errorf("PSLLDQ high = %016X", c.xmm[2][1])
	}
}

// TestPSRLDQ: 66 0F 73 /3 — byte-shift right.
func TestPSRLDQ(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[2] = [2]uint64{0x0807060504030201, 0x100F0E0D0C0B0A09}
	// 66 0F 73 /3 rm=2 imm=2 — ModRM = 11_011_010 = 0xDA
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0x73, 0xDA, 0x02, 0xF4})
	if c.xmm[2][0] != 0x0A09080706050403 {
		t.Errorf("PSRLDQ low = %016X", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0x0000100F0E0D0C0B {
		t.Errorf("PSRLDQ high = %016X", c.xmm[2][1])
	}
}

// TestStmxcsr: 0F AE /3 — store MXCSR.
//
// Note: uses RDI for the destination address. We set up a small RAM
// page at 0x2000 (longModeCPU only registers 0..1MiB, so 0x2000 is
// safely inside).
func TestStmxcsr(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mxcsr = 0x1F80
	c.SetReg64(RDI, 0x2000)
	// STMXCSR [RDI] — 0F AE /3 rm=7  ModRM = 00_011_111 = 0x1F
	runMMXCode(t, c, mm, []byte{0x0F, 0xAE, 0x1F, 0xF4})
	got, _ := mm.Read32(0x2000)
	if got != 0x1F80 {
		t.Errorf("stored MXCSR = %08X, want 0x1F80", got)
	}
}

// TestLdmxcsr: 0F AE /2 — load MXCSR.
func TestLdmxcsr(t *testing.T) {
	c, mm := longModeCPU(t)
	_ = mm.Write32(0x2000, 0x00006080)
	c.SetReg64(RDI, 0x2000)
	// LDMXCSR [RDI] — 0F AE /2 rm=7  ModRM = 00_010_111 = 0x17
	runMMXCode(t, c, mm, []byte{0x0F, 0xAE, 0x17, 0xF4})
	if c.mxcsr != 0x00006080 {
		t.Errorf("mxcsr after LDMXCSR = %08X, want 0x00006080", c.mxcsr)
	}
}

// TestPUNPCKLDQ_XMM merges two 32-bit GPR values via XMM. Long-mode
// version of the busybox sequence — addressing via RSP instead of ESP.
func TestPUNPCKLDQ_XMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg32(EBX, 0xCAFEBABE)
	c.SetReg32(EDX, 0xDEADBEEF)
	c.SetReg64(RSP, 0x2000)
	// 66 0F 6E CB   movd %ebx, %xmm1
	// 66 0F 6E C2   movd %edx, %xmm0
	// 66 0F 62 C1   punpckldq %xmm1, %xmm0
	// 66 0F D6 44 24 08   movq %xmm0, 0x8(%rsp)
	code := []byte{
		0x66, 0x0F, 0x6E, 0xCB,
		0x66, 0x0F, 0x6E, 0xC2,
		0x66, 0x0F, 0x62, 0xC1,
		0x66, 0x0F, 0xD6, 0x44, 0x24, 0x08,
		0xF4,
	}
	runMMXCode(t, c, mm, code)
	gotLo, _ := mm.Read32(0x2008)
	gotHi, _ := mm.Read32(0x200C)
	if gotLo != 0xDEADBEEF {
		t.Errorf("low 32 = %08X, want 0xDEADBEEF", gotLo)
	}
	if gotHi != 0xCAFEBABE {
		t.Errorf("high 32 = %08X, want 0xCAFEBABE", gotHi)
	}
}

// TestPMULUDQ_XMM: 66 0F F4 — unsigned 32x32 → 64 multiply per lane.
func TestPMULUDQ_XMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[1] = [2]uint64{0x0000_0000_0000_0003, 0x0000_0000_0000_0005}
	c.xmm[2] = [2]uint64{0x0000_0000_0000_0007, 0x0000_0000_0000_000B}
	// 66 0F F4 /reg=1 rm=2 — ModRM = 11_001_010 = 0xCA
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0xF4, 0xCA, 0xF4})
	if c.xmm[1][0] != 21 || c.xmm[1][1] != 55 {
		t.Errorf("xmm1 = %016X_%016X, want 0x37_0x15", c.xmm[1][1], c.xmm[1][0])
	}
}

// TestPMULUDQ_MMX: 0F F4 — MMX 32x32 → 64 multiply.
func TestPMULUDQ_MMX(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[1] = 13
	c.mm[2] = 17
	runMMXCode(t, c, mm, []byte{0x0F, 0xF4, 0xCA, 0xF4})
	if c.mm[1] != 221 {
		t.Errorf("mm1 = %016X, want 0xDD", c.mm[1])
	}
}

// TestPSRLW_XMM: 66 0F 71 /2 — immediate-count per-word logical right
// shift on XMM. Verifies the 66-prefix dispatch in the group-encoded
// shift family.
func TestPSRLW_XMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[2] = [2]uint64{0x8000400020001000, 0xFFFFAAAA55550001}
	// PSRLW XMM2, 1 — 66 0F 71 /2 rm=2  ModRM = 11_010_010 = 0xD2
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0x71, 0xD2, 0x01, 0xF4})
	if c.xmm[2][0] != 0x4000200010000800 {
		t.Errorf("low = %016X", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0x7FFF55552AAA0000 {
		t.Errorf("high = %016X", c.xmm[2][1])
	}
}
