package x86

import (
	"math"
	"testing"
)

// TestUCOMISS exercises 0F 2E — UCOMISS xmm1, xmm2. Compares two scalar
// single-precision floats and sets EFLAGS.ZF/PF/CF (clears OF/SF/AF).
//
// Per Intel SDM:
//
//	a > b  → ZF=0 PF=0 CF=0
//	a < b  → ZF=0 PF=0 CF=1
//	a == b → ZF=1 PF=0 CF=0
//	NaN    → ZF=1 PF=1 CF=1
//
// Busybox/musl emit UCOMISS for compiled FP comparisons; the SSE op was
// previously unimplemented (would fall through to "unimplemented 0F
// opcode" error).
func TestUCOMISS_lessThan(t *testing.T) {
	c := newTestCPU(t)
	// XMM0 = 1.0f (low 32 bits)
	c.xmm[0][0] = uint64(math.Float32bits(1.0))
	// XMM1 = 2.0f
	c.xmm[1][0] = uint64(math.Float32bits(2.0))
	// 0F 2E C1 = UCOMISS XMM0, XMM1
	code := []byte{0x0F, 0x2E, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.eflags&EFLAGS_CF == 0 {
		t.Errorf("UCOMISS(1.0, 2.0): expected CF=1 (less), got eflags=%08X", c.eflags)
	}
	if c.eflags&EFLAGS_ZF != 0 {
		t.Errorf("UCOMISS(1.0, 2.0): expected ZF=0, got eflags=%08X", c.eflags)
	}
}

func TestUCOMISS_equal(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[0][0] = uint64(math.Float32bits(5.0))
	c.xmm[1][0] = uint64(math.Float32bits(5.0))
	code := []byte{0x0F, 0x2E, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.eflags&EFLAGS_ZF == 0 {
		t.Errorf("UCOMISS(5.0, 5.0): expected ZF=1, got eflags=%08X", c.eflags)
	}
	if c.eflags&EFLAGS_CF != 0 {
		t.Errorf("UCOMISS(5.0, 5.0): expected CF=0, got eflags=%08X", c.eflags)
	}
}

// TestUCOMISD — 66 0F 2E, scalar double-precision.
func TestUCOMISD_greater(t *testing.T) {
	c := newTestCPU(t)
	c.xmm[0][0] = math.Float64bits(3.14)
	c.xmm[1][0] = math.Float64bits(1.41)
	// 66 0F 2E C1 = UCOMISD XMM0, XMM1
	code := []byte{0x66, 0x0F, 0x2E, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.eflags&EFLAGS_CF != 0 {
		t.Errorf("UCOMISD(3.14, 1.41): expected CF=0 (greater), got eflags=%08X", c.eflags)
	}
	if c.eflags&EFLAGS_ZF != 0 {
		t.Errorf("UCOMISD(3.14, 1.41): expected ZF=0 (not equal), got eflags=%08X", c.eflags)
	}
}

// TestMOVMSKPS — 0F 50, extract sign bits of 4 packed singles.
func TestMOVMSKPS(t *testing.T) {
	c := newTestCPU(t)
	// XMM0 with sign bits: lane0=+, lane1=-, lane2=+, lane3=-
	// Lanes (low to high): 1.0, -1.0, 2.0, -2.0
	c.xmm[0][0] = uint64(math.Float32bits(-1.0))<<32 | uint64(math.Float32bits(1.0))
	c.xmm[0][1] = uint64(math.Float32bits(-2.0))<<32 | uint64(math.Float32bits(2.0))
	// 0F 50 C8 = MOVMSKPS ECX, XMM0
	code := []byte{0x0F, 0x50, 0xC8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// Sign bits: bit0=0 (1.0+), bit1=1 (-1.0), bit2=0 (2.0+), bit3=1 (-2.0)
	// → 0b1010 = 0xA
	if got := c.GetReg32(ECX); got != 0xA {
		t.Errorf("MOVMSKPS: ECX=%X, want 0xA", got)
	}
}

// TestPMOVMSKB_XMM — 66 0F D7, extract sign bits of 16 packed bytes.
func TestPMOVMSKB_XMM(t *testing.T) {
	c := newTestCPU(t)
	// XMM1 with alternating sign bits: bytes 0xFF, 0x7F, 0xFF, 0x7F, ...
	c.xmm[1][0] = 0x7FFF7FFF7FFF7FFF
	c.xmm[1][1] = 0x7FFF7FFF7FFF7FFF
	// 66 0F D7 C1 = PMOVMSKB EAX, XMM1
	code := []byte{0x66, 0x0F, 0xD7, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// Bytes: 0xFF 0x7F 0xFF 0x7F 0xFF 0x7F 0xFF 0x7F (low qword in little-endian → byte 0 = 0xFF)
	// 0x7FFF7FFF7FFF7FFF little-endian = bytes FF 7F FF 7F FF 7F FF 7F
	// Bit 0 (sign of byte 0=0xFF) = 1, bit 1 (sign of byte 1=0x7F) = 0, etc.
	// → 0b0101010101010101 = 0x5555
	if got := c.GetReg32(EAX); got != 0x5555 {
		t.Errorf("PMOVMSKB: EAX=%X, want 0x5555", got)
	}
}

// TestPEXTRW — 66 0F C5, extract 16-bit lane from XMM.
func TestPEXTRW(t *testing.T) {
	c := newTestCPU(t)
	// XMM0 lanes [0..7] = 0x1111, 0x2222, 0x3333, 0x4444, 0x5555, 0x6666, 0x7777, 0x8888
	c.xmm[0][0] = 0x4444333322221111
	c.xmm[0][1] = 0x8888777766665555
	// 66 0F C5 C0 03 = PEXTRW EAX, XMM0, 3 → extract lane 3 = 0x4444
	code := []byte{0x66, 0x0F, 0xC5, 0xC0, 0x03, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0x4444 {
		t.Errorf("PEXTRW idx=3: EAX=%X, want 0x4444", got)
	}
}

// TestUNPCKLPS — 0F 14, interleave low packed singles.
//
//	dst = { d0, d1, d2, d3 }, src = { s0, s1, s2, s3 }
//	→ dst = { d0, s0, d1, s1 }
func TestUNPCKLPS(t *testing.T) {
	c := newTestCPU(t)
	// dst: XMM0 = { 0x11111111, 0x22222222, 0x33333333, 0x44444444 } little-endian
	c.xmm[0][0] = 0x2222222211111111
	c.xmm[0][1] = 0x4444444433333333
	// src: XMM1 = { 0xAAAAAAAA, 0xBBBBBBBB, 0xCCCCCCCC, 0xDDDDDDDD }
	c.xmm[1][0] = 0xBBBBBBBBAAAAAAAA
	c.xmm[1][1] = 0xDDDDDDDDCCCCCCCC
	// 0F 14 C1 = UNPCKLPS XMM0, XMM1
	code := []byte{0x0F, 0x14, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// Expected: { d0=11..., s0=AA..., d1=22..., s1=BB... }
	wantLo := uint64(0xAAAAAAAA)<<32 | 0x11111111
	wantHi := uint64(0xBBBBBBBB)<<32 | 0x22222222
	if c.xmm[0][0] != wantLo || c.xmm[0][1] != wantHi {
		t.Errorf("UNPCKLPS: got xmm0=%016X_%016X, want %016X_%016X",
			c.xmm[0][1], c.xmm[0][0], wantHi, wantLo)
	}
}
