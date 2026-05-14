package x86

import (
	"encoding/binary"
	"math/big"
	"testing"
)

// TestBignum_ADC_LongCarryChain exercises a 128-bit ADD with full carry
// propagation across 4 32-bit limbs — exactly the pattern OpenSSL's RSA
// modexp uses inside Montgomery reduction. The inputs are chosen so the
// carry ripples through every limb, then escapes into a fifth word.
//
// If any of ADD32/ADC32 leak or drop the CF, the result diverges.
func TestBignum_ADC_LongCarryChain(t *testing.T) {
	c := newTestCPU(t)

	src1 := uint32(0x2000)
	src2 := uint32(0x2010)
	dst := uint32(0x2020)
	c.writeMem32(src1+0, 0xFFFFFFFF)
	c.writeMem32(src1+4, 0xFFFFFFFF)
	c.writeMem32(src1+8, 0xFFFFFFFF)
	c.writeMem32(src1+12, 0xFFFFFFFF)
	c.writeMem32(src2+0, 0x00000001)
	c.writeMem32(src2+4, 0x00000000)
	c.writeMem32(src2+8, 0x00000000)
	c.writeMem32(src2+12, 0x00000000)

	// 4-limb add with carry, then a final "ADC EAX, 0" to capture the
	// escaped CF into limb 4.
	//   MOV EAX, [src1+0]     A1 00 20 00 00
	//   ADD EAX, [src2+0]     03 05 10 20 00 00
	//   MOV [dst+0], EAX      A3 20 20 00 00
	//   MOV EAX, [src1+4]
	//   ADC EAX, [src2+4]     13 05 14 20 00 00
	//   MOV [dst+4], EAX
	//   ... and so on for limbs 2 and 3
	//   XOR EAX, EAX
	//   ADC EAX, 0           (capture final carry)
	//   MOV [dst+16], EAX
	//   HLT
	code := []byte{
		// limb 0: ADD
		0xA1, byte(src1), byte(src1 >> 8), 0, 0, // MOV EAX, [src1+0]
		0x03, 0x05, byte(src2), byte(src2 >> 8), 0, 0, // ADD EAX, [src2+0]
		0xA3, byte(dst), byte(dst >> 8), 0, 0, // MOV [dst+0], EAX
		// limb 1: ADC
		0xA1, byte(src1 + 4), byte((src1 + 4) >> 8), 0, 0,
		0x13, 0x05, byte(src2 + 4), byte((src2 + 4) >> 8), 0, 0, // ADC EAX, [src2+4]
		0xA3, byte(dst + 4), byte((dst + 4) >> 8), 0, 0,
		// limb 2: ADC
		0xA1, byte(src1 + 8), byte((src1 + 8) >> 8), 0, 0,
		0x13, 0x05, byte(src2 + 8), byte((src2 + 8) >> 8), 0, 0,
		0xA3, byte(dst + 8), byte((dst + 8) >> 8), 0, 0,
		// limb 3: ADC
		0xA1, byte(src1 + 12), byte((src1 + 12) >> 8), 0, 0,
		0x13, 0x05, byte(src2 + 12), byte((src2 + 12) >> 8), 0, 0,
		0xA3, byte(dst + 12), byte((dst + 12) >> 8), 0, 0,
		// escape: XOR EAX,EAX (CF preserved? NO — XOR clears CF!)
		// Use a CF-preserving zero: MOV EAX, 0  (5 bytes, no flag changes)
		0xB8, 0x00, 0x00, 0x00, 0x00, // MOV EAX, 0
		0x83, 0xD0, 0x00, // ADC EAX, 0 (imm8 sign-extended)
		0xA3, byte(dst + 16), byte((dst + 16) >> 8), 0, 0, // MOV [dst+16], EAX
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}

	want := []uint32{0x00000000, 0x00000000, 0x00000000, 0x00000000, 0x00000001}
	for i, w := range want {
		got := c.readMem32(dst + uint32(i)*4)
		if got != w {
			t.Errorf("limb %d: got %08X, want %08X", i, got, w)
		}
	}
}

// TestBignum_SBB_LongBorrowChain — the analogue for subtraction. Montgomery
// reduction's final conditional subtract uses an SBB chain to compute
// (T - N) and steers the result via the final CF.
func TestBignum_SBB_LongBorrowChain(t *testing.T) {
	c := newTestCPU(t)

	src1 := uint32(0x2000) // minuend
	src2 := uint32(0x2010) // subtrahend
	dst := uint32(0x2020)
	// src1 - src2, choose so every limb borrows from the next.
	c.writeMem32(src1+0, 0x00000000)
	c.writeMem32(src1+4, 0x00000000)
	c.writeMem32(src1+8, 0x00000000)
	c.writeMem32(src1+12, 0x00000000)
	c.writeMem32(src2+0, 0x00000001)
	c.writeMem32(src2+4, 0x00000000)
	c.writeMem32(src2+8, 0x00000000)
	c.writeMem32(src2+12, 0x00000000)
	// Expected: 0 - 1 across 128 bits = 0xFFFF...FF, final CF=1.

	code := []byte{
		0xA1, byte(src1), byte(src1 >> 8), 0, 0,
		0x2B, 0x05, byte(src2), byte(src2 >> 8), 0, 0, // SUB EAX, [src2+0]
		0xA3, byte(dst), byte(dst >> 8), 0, 0,
		0xA1, byte(src1 + 4), byte((src1 + 4) >> 8), 0, 0,
		0x1B, 0x05, byte(src2 + 4), byte((src2 + 4) >> 8), 0, 0, // SBB EAX, [src2+4]
		0xA3, byte(dst + 4), byte((dst + 4) >> 8), 0, 0,
		0xA1, byte(src1 + 8), byte((src1 + 8) >> 8), 0, 0,
		0x1B, 0x05, byte(src2 + 8), byte((src2 + 8) >> 8), 0, 0,
		0xA3, byte(dst + 8), byte((dst + 8) >> 8), 0, 0,
		0xA1, byte(src1 + 12), byte((src1 + 12) >> 8), 0, 0,
		0x1B, 0x05, byte(src2 + 12), byte((src2 + 12) >> 8), 0, 0,
		0xA3, byte(dst + 12), byte((dst + 12) >> 8), 0, 0,
		// Capture final borrow without disturbing CF:
		0xB8, 0x00, 0x00, 0x00, 0x00, // MOV EAX, 0  (does not touch flags)
		0x83, 0xD8, 0x00, // SBB EAX, 0 → EAX = -CF = 0xFFFFFFFF if CF=1
		0xA3, byte(dst + 16), byte((dst + 16) >> 8), 0, 0,
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}

	want := []uint32{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF}
	for i, w := range want {
		got := c.readMem32(dst + uint32(i)*4)
		if got != w {
			t.Errorf("limb %d: got %08X, want %08X", i, got, w)
		}
	}
}

// TestBignum_MUL_HighLowSplit — single MUL r/m32 producing EDX:EAX. The
// pattern is the inner step of school multiplication; if MUL drops the
// high half or computes it incorrectly, every Montgomery multiply
// diverges after the second limb.
func TestBignum_MUL_HighLowSplit(t *testing.T) {
	cases := []struct {
		a, b uint32
	}{
		{0xDEADBEEF, 0x12345678},
		{0xFFFFFFFF, 0xFFFFFFFF},
		{0xFFFFFFFF, 0x00000001},
		{0x80000000, 0x00000002},
		{0x12345678, 0x9ABCDEF0},
	}
	for _, tc := range cases {
		c := newTestCPU(t)
		c.SetReg32(EAX, tc.a)
		c.SetReg32(ECX, tc.b)
		// F7 E1 = MUL ECX; F4 = HLT
		code := []byte{0xF7, 0xE1, 0xF4}
		if err := runCode(t, c, code, 0x1000); err != nil {
			t.Fatalf("runCode(%08X*%08X): %v", tc.a, tc.b, err)
		}
		want := uint64(tc.a) * uint64(tc.b)
		gotLo := c.GetReg32(EAX)
		gotHi := c.GetReg32(EDX)
		got := (uint64(gotHi) << 32) | uint64(gotLo)
		if got != want {
			t.Errorf("MUL %08X*%08X: got EDX:EAX=%08X_%08X, want %016X",
				tc.a, tc.b, gotHi, gotLo, want)
		}
		// MUL sets CF/OF = (EDX != 0).
		wantCF := want > 0xFFFFFFFF
		if c.getCF() != wantCF {
			t.Errorf("MUL %08X*%08X: CF=%v, want %v", tc.a, tc.b, c.getCF(), wantCF)
		}
		if c.getOF() != wantCF {
			t.Errorf("MUL %08X*%08X: OF=%v, want %v", tc.a, tc.b, c.getOF(), wantCF)
		}
	}
}

// TestBignum_SchoolMul_4x4 — full 4-limb × 4-limb schoolbook multiplication
// hand-coded as x86 instructions. Compares against math/big. This is the
// exact pattern OpenSSL's bn_mul_words / bn_mul_add_words uses; if our
// MUL/ADD/ADC produce a wrong carry on any single edge, a 128×128-bit
// product diverges. Repeats with several inputs including all-ones
// (worst-case carry rippling).
func TestBignum_SchoolMul_4x4(t *testing.T) {
	cases := []struct {
		name string
		a, b [4]uint32
	}{
		{
			name: "small",
			a:    [4]uint32{0x00000003, 0, 0, 0},
			b:    [4]uint32{0x00000005, 0, 0, 0},
		},
		{
			name: "all-ones-x-all-ones",
			a:    [4]uint32{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF},
			b:    [4]uint32{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF},
		},
		{
			name: "msb-set",
			a:    [4]uint32{0x80000000, 0x80000000, 0x80000000, 0x80000000},
			b:    [4]uint32{0x80000000, 0x80000000, 0x80000000, 0x80000000},
		},
		{
			name: "ramp",
			a:    [4]uint32{0xDEADBEEF, 0xCAFEBABE, 0x12345678, 0x9ABCDEF0},
			b:    [4]uint32{0x13579BDF, 0x2468ACE0, 0x0F0F0F0F, 0xF0F0F0F0},
		},
	}

	// Layout: A at 0x2000 (16 bytes), B at 0x2010 (16 bytes),
	// R at 0x2020 (32 bytes). All four-limb little-endian.
	aBase := uint32(0x2000)
	bBase := uint32(0x2010)
	rBase := uint32(0x2020)

	// Build code that does the school-multiplication once. We use:
	//   for i in 0..3:
	//     carry = 0
	//     for j in 0..3:
	//       MOV EAX, [A + j*4]
	//       MUL [B + i*4]            ; EDX:EAX = A[j]*B[i]
	//       ADD EAX, carry            ; add carry from previous j
	//       ADC EDX, 0                ; capture carry-out into EDX
	//       ADD EAX, [R + (i+j)*4]    ; add R[i+j]
	//       ADC EDX, 0                ; absorb second carry
	//       MOV [R + (i+j)*4], EAX
	//       MOV carry, EDX
	//     R[i+4] = carry
	// We keep `carry` in EBX and `B[i]` in a scratch we reload via MUL
	// memory operand. For clarity we use MUL with a memory operand
	// (F7 25 disp32) at each step.
	//
	// To keep the generated code small and avoid encoding a full loop,
	// we unroll all 4×4 = 16 inner iterations.
	var code []byte
	emit := func(b ...byte) { code = append(code, b...) }

	// Zero R[0..7] at the start.
	for k := 0; k < 8; k++ {
		// MOV [R + k*4], 0  →  C7 05 disp32 imm32
		emit(0xC7, 0x05)
		emit(byte(rBase+uint32(k)*4), byte((rBase+uint32(k)*4)>>8), 0, 0)
		emit(0, 0, 0, 0)
	}

	for i := 0; i < 4; i++ {
		// XOR EBX, EBX  → carry = 0
		emit(0x31, 0xDB)
		bOff := bBase + uint32(i)*4
		for j := 0; j < 4; j++ {
			aOff := aBase + uint32(j)*4
			rOff := rBase + uint32(i+j)*4
			// MOV EAX, [aOff]
			emit(0xA1, byte(aOff), byte(aOff>>8), 0, 0)
			// MUL [bOff] — F7 25 disp32
			emit(0xF7, 0x25, byte(bOff), byte(bOff>>8), 0, 0)
			// ADD EAX, EBX  → 01 D8  (ADD r/m32, r32 with r32=EBX)
			emit(0x01, 0xD8)
			// ADC EDX, 0   → 83 D2 00
			emit(0x83, 0xD2, 0x00)
			// ADD EAX, [rOff]  → 03 05 disp32
			emit(0x03, 0x05, byte(rOff), byte(rOff>>8), 0, 0)
			// ADC EDX, 0
			emit(0x83, 0xD2, 0x00)
			// MOV [rOff], EAX
			emit(0xA3, byte(rOff), byte(rOff>>8), 0, 0)
			// MOV EBX, EDX  → 89 D3
			emit(0x89, 0xD3)
		}
		// R[i+4] = carry (EBX). Final iteration's carry slot.
		rOff := rBase + uint32(i+4)*4
		// MOV [rOff], EBX  → 89 1D disp32
		emit(0x89, 0x1D, byte(rOff), byte(rOff>>8), 0, 0)
	}
	emit(0xF4) // HLT

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCPU(t)
			for k, w := range tc.a {
				c.writeMem32(aBase+uint32(k)*4, w)
			}
			for k, w := range tc.b {
				c.writeMem32(bBase+uint32(k)*4, w)
			}
			// Zero R
			for k := uint32(0); k < 8; k++ {
				c.writeMem32(rBase+k*4, 0)
			}
			if err := runCode(t, c, code, 0x1000); err != nil {
				t.Fatalf("runCode: %v", err)
			}
			// Read 8 limbs back.
			var got [8]uint32
			for k := range got {
				got[k] = c.readMem32(rBase + uint32(k)*4)
			}
			// Compute expected via math/big.
			A := limbsToBig(tc.a[:])
			B := limbsToBig(tc.b[:])
			want := new(big.Int).Mul(A, B)
			wantLimbs := bigToLimbs(want, 8)
			for k := 0; k < 8; k++ {
				if got[k] != wantLimbs[k] {
					t.Errorf("limb %d: got %08X, want %08X (full got=%v full want=%v)",
						k, got[k], wantLimbs[k], got, wantLimbs)
				}
			}
		})
	}
}

// TestBignum_SchoolMul_Large — parameterized schoolbook multiplication for
// large limb counts. Generates an unrolled multiplication for n×n limbs
// (n up to 128 = RSA-4096 size) and compares the in-emulator result to
// math/big. This is the only direct test of basic bignum at the exact
// limb count OpenSSL uses for RSA-4096 modexp; if there's a regression at
// scale (e.g., a memory-addressing edge case past a 4KB boundary) it'll
// surface here.
func TestBignum_SchoolMul_Large(t *testing.T) {
	for _, n := range []int{16, 32, 64, 128} {
		t.Run(testName(n), func(t *testing.T) {
			testSchoolMulN(t, n)
		})
	}
}

func testName(n int) string {
	switch n {
	case 16:
		return "16limb-512bit"
	case 32:
		return "32limb-1024bit"
	case 64:
		return "64limb-2048bit"
	case 128:
		return "128limb-4096bit"
	}
	return "unknown"
}

func testSchoolMulN(t *testing.T, n int) {
	// Generate deterministic but stressy inputs: all-ones × pattern with
	// MSB-bit toggles, which forces every limb's MUL to produce a high
	// half and every ADC to carry into the next limb.
	a := make([]uint32, n)
	b := make([]uint32, n)
	for i := 0; i < n; i++ {
		a[i] = 0xFFFFFFFF                                  // all ones
		b[i] = 0x80000000 | uint32(i*0x9E3779B9)&0x7FFFFFFF // MSB set + a varied lower
	}

	c := newTestCPU(t)
	// newTestCPU leaves CS limit at 0xFFFF (reset value). When unrolled
	// code exceeds 64KB we need a flat segment. Also place data above
	// the code so it doesn't get overwritten.
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	// Code lives at 0x1000+. Data lives at 0xC0000+ (well above code for n=128).
	aBase := uint32(0xC0000)
	bBase := aBase + uint32(n)*4
	rBase := bBase + uint32(n)*4
	if rBase+uint32(2*n)*4 >= 1<<20 {
		t.Skipf("n=%d exceeds 1MB test RAM", n)
		return
	}
	for i, w := range a {
		c.writeMem32(aBase+uint32(i)*4, w)
	}
	for i, w := range b {
		c.writeMem32(bBase+uint32(i)*4, w)
	}
	for i := 0; i < 2*n; i++ {
		c.writeMem32(rBase+uint32(i)*4, 0)
	}

	// Build unrolled schoolbook: for each i, for each j, multiply A[j]*B[i],
	// add into R[i+j] with carry chain.
	var code []byte
	emit := func(bs ...byte) { code = append(code, bs...) }

	for i := 0; i < n; i++ {
		// XOR EBX, EBX  → carry = 0
		emit(0x31, 0xDB)
		bOff := bBase + uint32(i)*4
		for j := 0; j < n; j++ {
			aOff := aBase + uint32(j)*4
			rOff := rBase + uint32(i+j)*4
			// MOV EAX, [aOff]: A1 disp32
			emit(0xA1, byte(aOff), byte(aOff>>8), byte(aOff>>16), byte(aOff>>24))
			// MUL [bOff]: F7 25 disp32
			emit(0xF7, 0x25, byte(bOff), byte(bOff>>8), byte(bOff>>16), byte(bOff>>24))
			// ADD EAX, EBX: 01 D8
			emit(0x01, 0xD8)
			// ADC EDX, 0: 83 D2 00
			emit(0x83, 0xD2, 0x00)
			// ADD EAX, [rOff]: 03 05 disp32
			emit(0x03, 0x05, byte(rOff), byte(rOff>>8), byte(rOff>>16), byte(rOff>>24))
			// ADC EDX, 0
			emit(0x83, 0xD2, 0x00)
			// MOV [rOff], EAX: A3 disp32
			emit(0xA3, byte(rOff), byte(rOff>>8), byte(rOff>>16), byte(rOff>>24))
			// MOV EBX, EDX: 89 D3
			emit(0x89, 0xD3)
		}
		// R[i+n] = carry (EBX). MOV [rOff], EBX → 89 1D disp32
		rOff := rBase + uint32(i+n)*4
		emit(0x89, 0x1D, byte(rOff), byte(rOff>>8), byte(rOff>>16), byte(rOff>>24))
	}
	emit(0xF4) // HLT

	// Memory layout requires the code itself not to overlap with R. Code
	// lives at CS:0x1000 by default. With n=128, code size = 128*128*(5+6+2+3+6+3+5+2)+128*5+1 = ~720000 bytes
	// Actually inner loop is 5+6+2+3+6+3+5+2 = 32 bytes; outer adds 2+5 = 7.
	// Total ≈ 128*(128*32 + 7) + 1 ≈ 524289 bytes. That's a lot.
	// We need code-base + code-size to be below R-base.
	// Plan: put code at high address, R near low.
	// Actually runCode writes at CS:IP. We use 0x1000. Code size for n=128
	// ~524KB. Memory ends at 1MB. R starts at rBase ~0x2000+128*8=0x2400.
	// Code 0x1000+524KB = 0x82400. That overlaps R!
	//
	// Fix: move R to high memory. Set rBase to 0xA0000 (well past code).
	// Recompute offsets.

	codeSize := len(code)
	if 0x1000+codeSize > int(aBase) {
		t.Fatalf("code (%d bytes) collides with data at 0x%X — increase aBase", codeSize, aBase)
	}
	_ = codeSize

	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v\n  eip=0x%08X cycles=%d cr0=0x%X cs=%04X esp=0x%08X codeSize=%d",
			err, c.eip, c.GetCycles(), c.GetCR(0), c.GetSeg(CS), c.GetReg32(ESP), len(code))
	}

	got := make([]uint32, 2*n)
	for i := range got {
		got[i] = c.readMem32(rBase + uint32(i)*4)
	}
	A := limbsToBig(a)
	B := limbsToBig(b)
	want := new(big.Int).Mul(A, B)
	wantLimbs := bigToLimbs(want, 2*n)
	for i := 0; i < 2*n; i++ {
		if got[i] != wantLimbs[i] {
			t.Errorf("n=%d limb %d: got %08X, want %08X", n, i, got[i], wantLimbs[i])
			if i > 5 {
				return // limit output
			}
		}
	}
}

func limbsToBig(limbs []uint32) *big.Int {
	buf := make([]byte, len(limbs)*4)
	// Little-endian limb 0 is the low word; math/big.SetBytes is big-endian.
	// So we'll build a big-endian byte slice by reversing.
	for i, w := range limbs {
		binary.LittleEndian.PutUint32(buf[i*4:], w)
	}
	// Reverse to big-endian for SetBytes.
	be := make([]byte, len(buf))
	for i := range buf {
		be[i] = buf[len(buf)-1-i]
	}
	return new(big.Int).SetBytes(be)
}

func bigToLimbs(x *big.Int, n int) []uint32 {
	buf := x.Bytes() // big-endian, no leading zeros
	out := make([]uint32, n)
	// Pad to n*4 bytes big-endian.
	pad := make([]byte, n*4)
	copy(pad[n*4-len(buf):], buf)
	// Reverse to little-endian-by-byte.
	le := make([]byte, n*4)
	for i := range pad {
		le[i] = pad[n*4-1-i]
	}
	for i := 0; i < n; i++ {
		out[i] = binary.LittleEndian.Uint32(le[i*4:])
	}
	return out
}
