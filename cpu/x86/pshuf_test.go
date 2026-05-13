package x86

import "testing"

// TestPSHUFW_Identity exercises 0F 70 /r ib with imm8=0xE4 (11_10_01_00) — the
// per-lane source indices are 0,1,2,3 (pass-through).
func TestPSHUFW_Identity(t *testing.T) {
	c := newTestCPU(t)
	c.mm[3] = 0x4444333322221111
	// PSHUFW MM0, MM3, 0xE4  =  0F 70 /reg=0 rm=3 imm
	code := []byte{0x0F, 0x70, 0xC3, 0xE4, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[0] != 0x4444333322221111 {
		t.Errorf("mm0 = %016X", c.mm[0])
	}
}

// TestPSHUFW_Reverse exercises 0F 70 /r ib with imm8=0x1B (00_01_10_11) — the
// per-lane source indices are 3,2,1,0 (reverse).
func TestPSHUFW_Reverse(t *testing.T) {
	c := newTestCPU(t)
	c.mm[3] = 0x4444333322221111
	code := []byte{0x0F, 0x70, 0xC3, 0x1B, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	want := uint64(0x1111222233334444)
	if c.mm[0] != want {
		t.Errorf("mm0 = %016X, want %016X", c.mm[0], want)
	}
}

// TestPSHUFD: 66 0F 70 /r ib — SSE2 shuffle of 4 32-bit lanes.
func TestPSHUFD(t *testing.T) {
	c := newTestCPU(t)
	// xmm3 = [d0, d1, d2, d3] where d0=0x11..., d1=0x22..., etc.
	c.xmm[3] = [2]uint64{
		uint64(0x22222222)<<32 | 0x11111111,
		uint64(0x44444444)<<32 | 0x33333333,
	}
	// PSHUFD XMM0, XMM3, 0x1B   (66 0F 70 /0 rm=3 imm=0x1B → reverse)
	code := []byte{0x66, 0x0F, 0x70, 0xC3, 0x1B, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	wantLo := uint64(0x33333333)<<32 | 0x44444444
	wantHi := uint64(0x11111111)<<32 | 0x22222222
	if c.xmm[0][0] != wantLo || c.xmm[0][1] != wantHi {
		t.Errorf("PSHUFD reverse: xmm0 = %016X_%016X, want %016X_%016X",
			c.xmm[0][1], c.xmm[0][0], wantHi, wantLo)
	}
}

// TestPSHUFLW: F2 0F 70 /r ib — shuffle low 4 words, high 64 bits passed through.
func TestPSHUFLW(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[3] = [2]uint64{
		0x4444333322221111, // low 64 bits — to be shuffled
		0xCCCCBBBBAAAA9999, // high 64 bits — passed through
	}
	// F2 0F 70 /reg=0 rm=3 imm=0x1B (reverse low 4 words)
	code := []byte{0xF2, 0x0F, 0x70, 0xC3, 0x1B, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.xmm[0][0] != 0x1111222233334444 {
		t.Errorf("low shuffled = %016X, want 0x1111222233334444", c.xmm[0][0])
	}
	if c.xmm[0][1] != 0xCCCCBBBBAAAA9999 {
		t.Errorf("high passthrough = %016X, want 0xCCCCBBBBAAAA9999", c.xmm[0][1])
	}
}

// TestPSHUFHW: F3 0F 70 /r ib — shuffle high 4 words, low 64 bits passed through.
func TestPSHUFHW(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[3] = [2]uint64{
		0x4444333322221111, // low — passed through
		0xCCCCBBBBAAAA9999, // high — shuffled
	}
	// F3 0F 70 /reg=0 rm=3 imm=0x1B (reverse high 4 words)
	code := []byte{0xF3, 0x0F, 0x70, 0xC3, 0x1B, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.xmm[0][0] != 0x4444333322221111 {
		t.Errorf("low passthrough = %016X", c.xmm[0][0])
	}
	if c.xmm[0][1] != 0x9999AAAABBBBCCCC {
		t.Errorf("high shuffled = %016X, want 0x9999AAAABBBBCCCC", c.xmm[0][1])
	}
}

// TestPSLLDQ exercises 66 0F 73 /7 — byte-shift the whole 128-bit XMM left.
//   v << 1 byte:  high half gets bit 7 of low gates; low gets v<<8.
func TestPSLLDQ(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[2] = [2]uint64{0x0807060504030201, 0x100F0E0D0C0B0A09}
	// PSLLDQ XMM2, 1  → byte shift left by 1
	// 66 0F 73 /7 rm=2 imm=1   ModRM = 11_111_010 = 0xFA
	code := []byte{0x66, 0x0F, 0x73, 0xFA, 0x01, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// Expected: bytes shift up by 1; byte 0 → 0, byte 15 lost.
	// Original low: 01 02 03 04 05 06 07 08
	// Original high:09 0A 0B 0C 0D 0E 0F 10
	// After: low = 00 01 02 03 04 05 06 07
	//        high= 08 09 0A 0B 0C 0D 0E 0F
	if c.xmm[2][0] != 0x0706050403020100 {
		t.Errorf("PSLLDQ low = %016X, want 0x0706050403020100", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0x0F0E0D0C0B0A0908 {
		t.Errorf("PSLLDQ high = %016X, want 0x0F0E0D0C0B0A0908", c.xmm[2][1])
	}
}

// TestPSRLDQ exercises 66 0F 73 /3 — byte-shift right.
func TestPSRLDQ(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[2] = [2]uint64{0x0807060504030201, 0x100F0E0D0C0B0A09}
	// PSRLDQ XMM2, 2  →  byte shift right by 2
	// 66 0F 73 /3 rm=2  ModRM = 11_011_010 = 0xDA
	code := []byte{0x66, 0x0F, 0x73, 0xDA, 0x02, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// Shift right 2 bytes:
	//   low (orig 01 02 03 04 05 06 07 08) → 03 04 05 06 07 08 09 0A
	//   high (orig 09 0A 0B 0C 0D 0E 0F 10) → 0B 0C 0D 0E 0F 10 00 00
	if c.xmm[2][0] != 0x0A09080706050403 {
		t.Errorf("PSRLDQ low = %016X, want 0x0A09080706050403", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0x0000100F0E0D0C0B {
		t.Errorf("PSRLDQ high = %016X, want 0x0000100F0E0D0C0B", c.xmm[2][1])
	}
}

// TestStmxcsr/Ldmxcsr exercise 0F AE /3 (store) and /2 (load).
func TestStmxcsr(t *testing.T) {
	c := newTestCPU(t)
	c.mxcsr = 0x1F80
	c.SetReg32(EDI, 0x2000)
	// STMXCSR [EDI]  =  0F AE /3 rm=7  ModRM = 00_011_111 = 0x1F
	code := []byte{0x0F, 0xAE, 0x1F, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.readMem32(0x2000); got != 0x1F80 {
		t.Errorf("stored MXCSR = %08X, want 0x1F80", got)
	}
}

func TestLdmxcsr(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0x00006080)
	c.SetReg32(EDI, 0x2000)
	// LDMXCSR [EDI]  =  0F AE /2 rm=7  ModRM = 00_010_111 = 0x17
	code := []byte{0x0F, 0xAE, 0x17, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mxcsr != 0x00006080 {
		t.Errorf("mxcsr after LDMXCSR = %08X, want 0x00006080", c.mxcsr)
	}
}

// TestPUNPCKLDQ_XMM_BusyboxSequence reproduces the exact code pattern busybox
// uses to merge two 32-bit GPRs into a 64-bit value via XMM. With our
// previous bug (no 66-prefix dispatch on PUNPCKLDQ), the high 32 bits would
// be silently zeroed, corrupting 64-bit arithmetic in printf-family
// routines and ultimately producing wild pointer values.
//
//   movd  %ebx, %xmm1           ; xmm1[0..31] = EBX, rest zero
//   movd  %edx, %xmm0           ; xmm0[0..31] = EDX, rest zero
//   punpckldq %xmm1, %xmm0      ; xmm0 = [EDX, EBX, 0, 0]
//   movq  %xmm0, 0x8(%esp)      ; write low 64 = (EBX << 32) | EDX
func TestPUNPCKLDQ_XMM_BusyboxSequence(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0xCAFEBABE)
	c.SetReg32(EDX, 0xDEADBEEF)
	c.SetReg32(ESP, 0x2000)
	// 66 0F 6E CB   movd %ebx, %xmm1
	// 66 0F 6E C2   movd %edx, %xmm0
	// 66 0F 62 C1   punpckldq %xmm1, %xmm0
	// 66 0F D6 44 24 08   movq %xmm0, 0x8(%esp)
	code := []byte{
		0x66, 0x0F, 0x6E, 0xCB,
		0x66, 0x0F, 0x6E, 0xC2,
		0x66, 0x0F, 0x62, 0xC1,
		0x66, 0x0F, 0xD6, 0x44, 0x24, 0x08,
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	gotLo := c.readMem32(0x2008)
	gotHi := c.readMem32(0x200C)
	if gotLo != 0xDEADBEEF {
		t.Errorf("low 32 at [esp+8] = %08X, want 0xDEADBEEF", gotLo)
	}
	if gotHi != 0xCAFEBABE {
		t.Errorf("high 32 at [esp+12] = %08X, want 0xCAFEBABE", gotHi)
	}
}

// TestNlplugDigitFormatSequence reproduces the full SIMD block in
// nlplug-findfs at 0x4530-0x45A7. This is a SWAR (SIMD-within-a-
// register) digit-formatting pattern: takes 4 GPR values, computes a
// formatted 16-byte string via packed shifts and bitmask combine.
//
// The exact computation isn't important; what matters is that all
// operations correctly route to XMM (not MMX) under the 66 prefix.
// With our previous bug, several would silently use MMX state, and
// the final ANDPS/ORPS combine would produce wrong bytes — propagating
// downstream into wild pointers passed to malloc.
func TestNlplugDigitFormatSequence(t *testing.T) {
	c := newTestCPU(t)

	// Load constants into memory at known addresses (DS-relative).
	const constLow = uint32(0x3000)
	mask1 := [2]uint64{0xFF00FF00FF00FF00, 0xFF00FF00FF00FF00}
	mask2 := [2]uint64{0x00FF00FF00FF00FF, 0x00FF00FF00FF00FF}
	c.writeMem32(constLow+0, uint32(mask1[0]))
	c.writeMem32(constLow+4, uint32(mask1[0]>>32))
	c.writeMem32(constLow+8, uint32(mask1[1]))
	c.writeMem32(constLow+12, uint32(mask1[1]>>32))
	c.writeMem32(constLow+16, uint32(mask2[0]))
	c.writeMem32(constLow+20, uint32(mask2[0]>>32))
	c.writeMem32(constLow+24, uint32(mask2[1]))
	c.writeMem32(constLow+28, uint32(mask2[1]>>32))

	c.SetReg32(EBX, constLow)
	c.SetReg32(ECX, 0x12345678)
	c.SetReg32(EDX, 0xABCDEF01)

	// Sequence (rewritten to use ds-relative loads via EBX):
	// movd %ecx, %xmm0        ; xmm0 low32 = ECX
	// movd %edx, %xmm1        ; xmm1 low32 = EDX
	// punpckldq %xmm0, %xmm1  ; xmm1 = [EDX, ECX, 0, 0]
	// movaps 0(%ebx), %xmm2   ; xmm2 = mask1
	// movaps (%xmm1), %xmm3    ; xmm3 = xmm1
	// psllq $0x8, %xmm1        ; xmm1 <<= 8 (per-lane 64-bit)
	// andps %xmm2, %xmm1       ; xmm1 &= mask1
	// movaps 16(%ebx), %xmm2  ; xmm2 = mask2
	// andps %xmm2, %xmm3       ; xmm3 &= mask2
	// orps %xmm1, %xmm3        ; xmm3 |= xmm1
	// psrlq $0x20, %xmm3       ; xmm3 >>= 32 per-lane
	// movd %xmm3, %eax         ; EAX = low32 of xmm3
	code := []byte{
		0x66, 0x0F, 0x6E, 0xC1, // movd %ecx, %xmm0
		0x66, 0x0F, 0x6E, 0xCA, // movd %edx, %xmm1
		0x66, 0x0F, 0x62, 0xC8, // punpckldq %xmm0, %xmm1
		0x0F, 0x28, 0x53, 0x00, // movaps 0(%ebx), %xmm2
		0x0F, 0x28, 0xD9, // movaps %xmm1, %xmm3
		0x66, 0x0F, 0x73, 0xF1, 0x08, // psllq $0x8, %xmm1
		0x0F, 0x54, 0xCA, // andps %xmm2, %xmm1
		0x0F, 0x28, 0x53, 0x10, // movaps 0x10(%ebx), %xmm2
		0x0F, 0x54, 0xDA, // andps %xmm2, %xmm3
		0x0F, 0x56, 0xD9, // orps %xmm1, %xmm3
		0x66, 0x0F, 0x73, 0xD3, 0x20, // psrlq $0x20, %xmm3
		0x66, 0x0F, 0x7E, 0xD8, // movd %xmm3, %eax
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}

	// Compute the expected value via Go logic mirroring the asm.
	xmm1Initial := [2]uint64{
		uint64(0xABCDEF01) | uint64(0x12345678)<<32, // [EDX, ECX, 0, 0] low 64
		0,
	}
	xmm3 := xmm1Initial
	xmm1Shifted := [2]uint64{
		xmm1Initial[0] << 8,
		xmm1Initial[1] << 8,
	}
	xmm1Masked := [2]uint64{
		xmm1Shifted[0] & mask1[0],
		xmm1Shifted[1] & mask1[1],
	}
	xmm3Masked := [2]uint64{
		xmm3[0] & mask2[0],
		xmm3[1] & mask2[1],
	}
	xmm3Combined := [2]uint64{
		xmm3Masked[0] | xmm1Masked[0],
		xmm3Masked[1] | xmm1Masked[1],
	}
	xmm3Shifted := [2]uint64{
		xmm3Combined[0] >> 32,
		xmm3Combined[1] >> 32,
	}
	want := uint32(xmm3Shifted[0])
	if got := c.GetReg32(EAX); got != want {
		t.Errorf("EAX = %08X, want %08X (xmm3 low after shift = %016X)",
			got, want, xmm3Shifted[0])
	}
}

// TestNlplugSIMDSequence reproduces the SIMD-heavy block in
// nlplug-findfs at offset 0x4534-0x45A7 — number-formatting via XMM
// packed shifts and unpacks. The block constructs a 16-byte hex/decimal
// representation by interleaving GPR halves through XMM, then PSRLQ
// $0x20, %xmm0; MOVD %xmm0, %eax extracts an upper 32 bits.
//
// With our previous bug, PSLLQ on XMM ran the MMX shift, so the high
// 64 bits of xmm1/3 silently stayed at zero and the eventual MOVD
// returned the wrong EAX value.
func TestNlplugSIMDSequence(t *testing.T) {
	c := newTestCPU(t)
	// Set up: simulate the pre-SIMD register state.
	c.SetReg32(EDX, 0xCAFEBABE)
	c.SetReg32(ECX, 0x12345678)
	code := []byte{
		// movd %ecx, %xmm0     ; xmm0 = [ECX, 0, 0, 0]
		0x66, 0x0F, 0x6E, 0xC1,
		// movd %edx, %xmm1     ; xmm1 = [EDX, 0, 0, 0]
		0x66, 0x0F, 0x6E, 0xCA,
		// punpckldq %xmm0, %xmm1  ; xmm1 = [EDX, ECX, 0, 0]
		0x66, 0x0F, 0x62, 0xC8,
		// movaps %xmm1, %xmm3    ; xmm3 = xmm1
		0x0F, 0x28, 0xD9,
		// psllq $0x20, %xmm3     ; xmm3 high-lane <<= 32 ; should shift each 64-bit lane
		0x66, 0x0F, 0x73, 0xF3, 0x20,
		// psrlq $0x20, %xmm3     ; xmm3 >>= 32 → recovers original
		0x66, 0x0F, 0x73, 0xD3, 0x20,
		// movd %xmm3, %eax       ; eax = low 32 of xmm3 = EDX
		0x66, 0x0F, 0x7E, 0xD8,
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xCAFEBABE {
		t.Errorf("EAX after PSLLQ+PSRLQ round-trip = %08X, want 0xCAFEBABE", got)
	}
}

// TestPMULUDQ_XMM exercises 66 0F F4 — SSE2 unsigned 32x32 → 64 multiply
// per 64-bit lane.
func TestPMULUDQ_XMM(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[1] = [2]uint64{0x0000_0000_0000_0003, 0x0000_0000_0000_0005}
	c.xmm[2] = [2]uint64{0x0000_0000_0000_0007, 0x0000_0000_0000_000B}
	// PMULUDQ xmm1, xmm2   →   66 0F F4 /reg=1 rm=2  ModRM = 11_001_010 = 0xCA
	code := []byte{0x66, 0x0F, 0xF4, 0xCA, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.xmm[1][0] != 21 || c.xmm[1][1] != 55 {
		t.Errorf("PMULUDQ: xmm1 = %016X_%016X, want 0x37_0x15",
			c.xmm[1][1], c.xmm[1][0])
	}
}

// TestPMULUDQ_MMX exercises plain 0F F4 — MMX 32x32 → 64 multiply.
func TestPMULUDQ_MMX(t *testing.T) {
	c := newTestCPU(t)
	c.mm[1] = 0x0000_0000_0000_000D // 13
	c.mm[2] = 0x0000_0000_0000_0011 // 17
	// PMULUDQ mm1, mm2  =  0F F4 /reg=1 rm=2  ModRM = 0xCA
	code := []byte{0x0F, 0xF4, 0xCA, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[1] != 221 {
		t.Errorf("PMULUDQ MMX: mm1 = %016X, want 0xDD", c.mm[1])
	}
}

// TestPSRLW_XMM exercises 66 0F 71 /2 — per-word logical right shift on
// XMM (NOT MMX). Verifies the 66-prefix dispatch added for the SSE2
// immediate-count shift forms.
func TestPSRLW_XMM(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[2] = [2]uint64{0x8000400020001000, 0xFFFFAAAA55550001}
	// PSRLW XMM2, 1   →   66 0F 71 /2 rm=2  ModRM = 11_010_010 = 0xD2
	code := []byte{0x66, 0x0F, 0x71, 0xD2, 0x01, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.xmm[2][0] != 0x4000200010000800 {
		t.Errorf("low = %016X, want 0x4000200010000800", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0x7FFF55552AAA0000 {
		t.Errorf("high = %016X, want 0x7FFF55552AAA0000", c.xmm[2][1])
	}
}
