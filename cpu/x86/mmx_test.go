package x86

import "testing"

// TestMMX_MOVD_RegToMM covers `0F 6E /r` MOVD mm, r/m32 in the
// register-source form. The 32-bit value should be zero-extended into
// the 64-bit MMX register.
func TestMMX_MOVD_RegToMM(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	// MOVD MM3, EAX  =  0F 6E D8   (mod=11 reg=011 rm=000)
	code := []byte{0x0F, 0x6E, 0xD8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[3] != 0x00000000DEADBEEF {
		t.Errorf("mm3 = %016X, want 0x00000000DEADBEEF", c.mm[3])
	}
}

// TestMMX_MOVD_MMToReg covers `0F 7E /r` MOVD r/m32, mm. The low 32
// bits of the MMX register are written to the destination GPR.
func TestMMX_MOVD_MMToReg(t *testing.T) {
	c := newTestCPU(t)
	c.mm[2] = 0xAABBCCDD11223344
	// MOVD EBX, MM2  =  0F 7E D3   (mod=11 reg=010 rm=011)
	code := []byte{0x0F, 0x7E, 0xD3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EBX); got != 0x11223344 {
		t.Errorf("ebx = %08X, want 0x11223344", got)
	}
}

// TestMMX_MOVQ_RegToReg covers `0F 6F /r` MOVQ mm, mm/m64 in the
// register form, and `0F 7F /r` MOVQ mm/m64, mm symmetrically. The
// full 64 bits must round-trip.
func TestMMX_MOVQ_RegToReg(t *testing.T) {
	c := newTestCPU(t)
	c.mm[5] = 0x0123456789ABCDEF
	// MOVQ MM1, MM5  =  0F 6F CD   (mod=11 reg=001 rm=101)
	// MOVQ MM7, MM1  =  0F 7F CF   (mod=11 reg=001 rm=111)
	code := []byte{0x0F, 0x6F, 0xCD, 0x0F, 0x7F, 0xCF, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[1] != 0x0123456789ABCDEF {
		t.Errorf("mm1 = %016X, want 0x0123456789ABCDEF", c.mm[1])
	}
	if c.mm[7] != 0x0123456789ABCDEF {
		t.Errorf("mm7 = %016X, want 0x0123456789ABCDEF", c.mm[7])
	}
}

// TestMMX_MOVQ_Memory exercises the memory forms of MOVQ — the path
// musl's memcpy actually uses (load 8 bytes via MMX, store 8 bytes
// via MMX).
func TestMMX_MOVQ_Memory(t *testing.T) {
	c := newTestCPU(t)
	// Place a known qword at flat memory and have ESI point at it.
	const src = uint32(0x2000)
	const dst = uint32(0x2010)
	want := uint64(0xCAFEBABEF00DD00D)
	c.writeMem32(src, uint32(want))
	c.writeMem32(src+4, uint32(want>>32))
	c.SetReg32(ESI, src)
	c.SetReg32(EDI, dst)
	// MOVQ MM0, [ESI]  =  0F 6F 06
	// MOVQ [EDI], MM0  =  0F 7F 07
	code := []byte{0x0F, 0x6F, 0x06, 0x0F, 0x7F, 0x07, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[0] != want {
		t.Errorf("mm0 = %016X, want %016X", c.mm[0], want)
	}
	gotLo := uint64(c.readMem32(dst))
	gotHi := uint64(c.readMem32(dst + 4))
	if got := gotLo | gotHi<<32; got != want {
		t.Errorf("dst memory = %016X, want %016X", got, want)
	}
}

// TestMMX_EMMS verifies 0F 77 executes without faulting and consumes
// no operands. Our implementation is intentionally a no-op (we don't
// track the x87 tag word), but the opcode must decode cleanly.
func TestMMX_EMMS(t *testing.T) {
	c := newTestCPU(t)
	c.mm[0] = 0xDEADBEEFCAFEBABE
	// EMMS  =  0F 77
	code := []byte{0x0F, 0x77, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[0] != 0xDEADBEEFCAFEBABE {
		t.Errorf("EMMS modified mm0 = %016X", c.mm[0])
	}
}

// TestMMX_PXOR covers 0F EF — the most common kernel-side use of MMX,
// found in memset/optimised checksum loops.
func TestMMX_PXOR(t *testing.T) {
	c := newTestCPU(t)
	c.mm[0] = 0xFF00FF00FF00FF00
	c.mm[1] = 0x00FF00FF00FF00FF
	// PXOR MM0, MM1  =  0F EF C1
	code := []byte{0x0F, 0xEF, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[0] != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("PXOR result = %016X, want all-1s", c.mm[0])
	}
}

// TestMMX_PandPorPandn covers PAND, POR, PANDN as a single
// table-driven round-trip — each is bitwise on the full 64-bit value.
func TestMMX_PandPorPandn(t *testing.T) {
	cases := []struct {
		name    string
		opcode2 byte
		a, b    uint64
		want    uint64
	}{
		{"PAND", 0xDB, 0xF0F0F0F0F0F0F0F0, 0x0F0F0F0F0F0F0F0F, 0x0000000000000000},
		{"POR", 0xEB, 0xF0F0F0F0F0F0F0F0, 0x0F0F0F0F0F0F0F0F, 0xFFFFFFFFFFFFFFFF},
		// PANDN: (NOT a) AND b
		{"PANDN", 0xDF, 0xFF00FF00FF00FF00, 0xFFFFFFFFFFFFFFFF, 0x00FF00FF00FF00FF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCPU(t)
			c.mm[0] = tc.a
			c.mm[1] = tc.b
			// op MM0, MM1
			code := []byte{0x0F, tc.opcode2, 0xC1, 0xF4}
			if err := runCode(t, c, code, 0x1000); err != nil {
				t.Fatalf("runCode: %v", err)
			}
			if c.mm[0] != tc.want {
				t.Errorf("got %016X want %016X", c.mm[0], tc.want)
			}
		})
	}
}

// TestMMX_PCMPEQ covers byte/word/dword equality compare.
func TestMMX_PCMPEQ(t *testing.T) {
	cases := []struct {
		name    string
		opcode2 byte
		a, b    uint64
		want    uint64
	}{
		// 8 byte lanes; lanes 0,2,4,6 equal; 1,3,5,7 differ.
		{"PCMPEQB", 0x74,
			0xAABBCCDDEEFF1122, 0xAABBCCDDEEFF1122,
			0xFFFFFFFFFFFFFFFF},
		// Half the bytes match (every other byte equals; the rest differ).
		// Lane order is little-endian: byte 0 is the LSB of `a`/`b`.
		// a bytes: 11 00 33 00 55 00 77 00
		// b bytes: 11 22 33 44 55 66 77 88
		// → matches at lanes 0,2,4,6 → 0x00FF00FF00FF00FF
		{"PCMPEQB partial", 0x74,
			0x0077005500330011, 0x8877665544332211,
			0x00FF00FF00FF00FF},
		// 4 word lanes
		{"PCMPEQW", 0x75,
			0x1111222233334444, 0x1111ABCD33334444,
			0xFFFF0000FFFFFFFF},
		// 2 dword lanes
		{"PCMPEQD", 0x76,
			0xDEADBEEF11223344, 0xDEADBEEF99887766,
			0xFFFFFFFF00000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCPU(t)
			c.mm[2] = tc.a
			c.mm[3] = tc.b
			// op MM2, MM3
			code := []byte{0x0F, tc.opcode2, 0xD3, 0xF4}
			if err := runCode(t, c, code, 0x1000); err != nil {
				t.Fatalf("runCode: %v", err)
			}
			if c.mm[2] != tc.want {
				t.Errorf("got %016X want %016X", c.mm[2], tc.want)
			}
		})
	}
}

// TestMMX_PADD covers packed add with byte/word/dword/qword sizing —
// crucially verifying that carry does NOT propagate across element
// boundaries.
func TestMMX_PADD(t *testing.T) {
	cases := []struct {
		name    string
		opcode2 byte
		a, b    uint64
		want    uint64
	}{
		// PADDB: byte 0xFF + byte 0x01 = 0x00 (wraps, no carry out).
		{"PADDB wrap", 0xFC,
			0xFFFFFFFFFFFFFFFF, 0x0101010101010101,
			0x0000000000000000},
		// PADDW: 0xFFFF + 1 = 0 per word.
		{"PADDW wrap", 0xFD,
			0xFFFFFFFFFFFFFFFF, 0x0001000100010001,
			0x0000000000000000},
		// PADDD: 0xFFFFFFFF + 1 = 0 per dword.
		{"PADDD wrap", 0xFE,
			0xFFFFFFFFFFFFFFFF, 0x0000000100000001,
			0x0000000000000000},
		// PADDQ: full 64-bit add, single carry chain.
		{"PADDQ", 0xD4,
			0x00000000FFFFFFFF, 0x0000000000000001,
			0x0000000100000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCPU(t)
			c.mm[0] = tc.a
			c.mm[1] = tc.b
			code := []byte{0x0F, tc.opcode2, 0xC1, 0xF4}
			if err := runCode(t, c, code, 0x1000); err != nil {
				t.Fatalf("runCode: %v", err)
			}
			if c.mm[0] != tc.want {
				t.Errorf("got %016X want %016X", c.mm[0], tc.want)
			}
		})
	}
}

// TestMMX_PSUB sanity-checks packed subtraction.
func TestMMX_PSUB(t *testing.T) {
	c := newTestCPU(t)
	c.mm[0] = 0x0000000000000000
	c.mm[1] = 0x0101010101010101
	// PSUBB MM0, MM1 = 0F F8 C1 — each byte 0 - 1 wraps to 0xFF.
	code := []byte{0x0F, 0xF8, 0xC1, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[0] != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("PSUBB got %016X, want all-FF", c.mm[0])
	}
}
