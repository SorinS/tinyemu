package x86_64

// MMX / SSE2 unit tests for the long-mode backend. Ported from
// cpu/x86/mmx_test.go. The arithmetic semantics are identical; only
// the harness differs — long mode requires CR0.PE + CR4.PAE + EFER.LMA
// + CS.L set, which longModeCPU (defined in intr_test.go) takes care
// of.

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// runMMXCode writes `code` at codeAddr=0x1000, sets RIP, and steps
// until either the HLT (0xF4) terminator or an error. Reuses the
// long-mode bring-up boilerplate from longModeCPU.
func runMMXCode(t *testing.T, c *CPU, mm *mem.PhysMemoryMap, code []byte) {
	t.Helper()
	const codeAddr uint64 = 0x1000
	for i, b := range code {
		if err := mm.Write8(codeAddr+uint64(i), b); err != nil {
			t.Fatalf("Write8: %v", err)
		}
	}
	c.SetRIP(codeAddr)
	// Bounded loop so a runaway test can't spin forever.
	for i := 0; i < 64; i++ {
		if c.IsPowerDown() {
			return
		}
		// Peek next byte: if it's HLT (0xF4) we stop here without
		// executing it (HLT in CPL=0 sets powerDown, but we'd rather
		// just exit the loop and let the test inspect post-state).
		if rip := c.GetRIP(); rip == codeAddr+uint64(len(code)-1) {
			if b, _ := mm.Read8(rip); b == 0xF4 {
				return
			}
		}
		if err := c.Step(); err != nil {
			t.Fatalf("Step at RIP=%#x: %v", c.GetRIP(), err)
		}
	}
	t.Fatalf("test exceeded 64 steps without reaching terminator")
}

// TestMMX_MOVD_RegToMM: 0F 6E /r MOVD mm, r/m32 — 32-bit zero-extend.
func TestMMX_MOVD_RegToMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	// MOVD MM3, EAX = 0F 6E D8 (mod=11 reg=011 rm=000)
	runMMXCode(t, c, mm, []byte{0x0F, 0x6E, 0xD8, 0xF4})
	if c.mm[3] != 0x00000000DEADBEEF {
		t.Errorf("mm3 = %016X, want 0x00000000DEADBEEF", c.mm[3])
	}
}

// TestMMX_MOVD_MMToReg: 0F 7E /r MOVD r/m32, mm.
func TestMMX_MOVD_MMToReg(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[2] = 0xAABBCCDD11223344
	// MOVD EBX, MM2 = 0F 7E D3 (mod=11 reg=010 rm=011)
	runMMXCode(t, c, mm, []byte{0x0F, 0x7E, 0xD3, 0xF4})
	if got := c.GetReg32(EBX); got != 0x11223344 {
		t.Errorf("ebx = %08X, want 0x11223344", got)
	}
}

// TestMMX_MOVQ_RegToReg: 0F 6F + 0F 7F.
func TestMMX_MOVQ_RegToReg(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[5] = 0x0123456789ABCDEF
	// MOVQ MM1, MM5 = 0F 6F CD; MOVQ MM7, MM1 = 0F 7F CF
	runMMXCode(t, c, mm, []byte{0x0F, 0x6F, 0xCD, 0x0F, 0x7F, 0xCF, 0xF4})
	if c.mm[1] != 0x0123456789ABCDEF {
		t.Errorf("mm1 = %016X", c.mm[1])
	}
	if c.mm[7] != 0x0123456789ABCDEF {
		t.Errorf("mm7 = %016X", c.mm[7])
	}
}

// TestMMX_EMMS: 0F 77 — no ModR/M, no observable state change.
func TestMMX_EMMS(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[0] = 0xDEADBEEFCAFEBABE
	runMMXCode(t, c, mm, []byte{0x0F, 0x77, 0xF4})
	if c.mm[0] != 0xDEADBEEFCAFEBABE {
		t.Errorf("EMMS modified mm0 = %016X", c.mm[0])
	}
}

// TestMMX_PXOR: 0F EF.
func TestMMX_PXOR(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[0] = 0xFF00FF00FF00FF00
	c.mm[1] = 0x00FF00FF00FF00FF
	runMMXCode(t, c, mm, []byte{0x0F, 0xEF, 0xC1, 0xF4})
	if c.mm[0] != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("PXOR result = %016X", c.mm[0])
	}
}

// TestMMX_PandPorPandn covers PAND/POR/PANDN bitwise on 64-bit MMX.
func TestMMX_PandPorPandn(t *testing.T) {
	cases := []struct {
		name    string
		opcode2 byte
		a, b    uint64
		want    uint64
	}{
		{"PAND", 0xDB, 0xF0F0F0F0F0F0F0F0, 0x0F0F0F0F0F0F0F0F, 0x0000000000000000},
		{"POR", 0xEB, 0xF0F0F0F0F0F0F0F0, 0x0F0F0F0F0F0F0F0F, 0xFFFFFFFFFFFFFFFF},
		{"PANDN", 0xDF, 0xFF00FF00FF00FF00, 0xFFFFFFFFFFFFFFFF, 0x00FF00FF00FF00FF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, mm := longModeCPU(t)
			c.mm[0] = tc.a
			c.mm[1] = tc.b
			runMMXCode(t, c, mm, []byte{0x0F, tc.opcode2, 0xC1, 0xF4})
			if c.mm[0] != tc.want {
				t.Errorf("got %016X want %016X", c.mm[0], tc.want)
			}
		})
	}
}

// TestMMX_PCMPEQ: 0F 74/75/76.
func TestMMX_PCMPEQ(t *testing.T) {
	cases := []struct {
		name    string
		opcode2 byte
		a, b    uint64
		want    uint64
	}{
		{"PCMPEQB", 0x74, 0xAABBCCDDEEFF1122, 0xAABBCCDDEEFF1122, 0xFFFFFFFFFFFFFFFF},
		{"PCMPEQW", 0x75, 0x1111222233334444, 0x1111ABCD33334444, 0xFFFF0000FFFFFFFF},
		{"PCMPEQD", 0x76, 0xDEADBEEF11223344, 0xDEADBEEF99887766, 0xFFFFFFFF00000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, mm := longModeCPU(t)
			c.mm[2] = tc.a
			c.mm[3] = tc.b
			runMMXCode(t, c, mm, []byte{0x0F, tc.opcode2, 0xD3, 0xF4})
			if c.mm[2] != tc.want {
				t.Errorf("got %016X want %016X", c.mm[2], tc.want)
			}
		})
	}
}

// TestMMX_PADD: packed-add with byte/word/dword/qword, no carry
// propagation across element boundaries.
func TestMMX_PADD(t *testing.T) {
	cases := []struct {
		name    string
		opcode2 byte
		a, b    uint64
		want    uint64
	}{
		{"PADDB wrap", 0xFC, 0xFFFFFFFFFFFFFFFF, 0x0101010101010101, 0x0000000000000000},
		{"PADDW wrap", 0xFD, 0xFFFFFFFFFFFFFFFF, 0x0001000100010001, 0x0000000000000000},
		{"PADDD wrap", 0xFE, 0xFFFFFFFFFFFFFFFF, 0x0000000100000001, 0x0000000000000000},
		{"PADDQ", 0xD4, 0x00000000FFFFFFFF, 0x0000000000000001, 0x0000000100000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, mm := longModeCPU(t)
			c.mm[0] = tc.a
			c.mm[1] = tc.b
			runMMXCode(t, c, mm, []byte{0x0F, tc.opcode2, 0xC1, 0xF4})
			if c.mm[0] != tc.want {
				t.Errorf("got %016X want %016X", c.mm[0], tc.want)
			}
		})
	}
}

// TestMMX_PSUB: 0F F8 PSUBB. Each byte 0 - 1 wraps to 0xFF.
func TestMMX_PSUB(t *testing.T) {
	c, mm := longModeCPU(t)
	c.mm[0] = 0x0000000000000000
	c.mm[1] = 0x0101010101010101
	runMMXCode(t, c, mm, []byte{0x0F, 0xF8, 0xC1, 0xF4})
	if c.mm[0] != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("PSUBB got %016X", c.mm[0])
	}
}

// TestSSE2_MOVQ_RegToXMM_REXW: the opcode that triggered this entire
// port — 66 REX.W 0F 6E /r = MOVQ xmm, r/m64. The 64-bit GPR value
// must end up in the LOW lane of the XMM register; the high lane
// must zero-extend.
//
// musl's _start emits `66 48 0F 6E C7` as its first vector op =
// MOVQ XMM0, RDI (passing argc/argv into the SSE side). If that
// fails, we don't reach the second instruction in userspace.
func TestSSE2_MOVQ_RegToXMM_REXW(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg64(RDI, 0xDEADBEEFCAFEBABE)
	c.xmm[0] = [2]uint64{0xAAAAAAAAAAAAAAAA, 0xBBBBBBBBBBBBBBBB}
	// 66 48 0F 6E C7 = MOVQ XMM0, RDI (mod=11 reg=000 rm=111)
	runMMXCode(t, c, mm, []byte{0x66, 0x48, 0x0F, 0x6E, 0xC7, 0xF4})
	if c.xmm[0][0] != 0xDEADBEEFCAFEBABE {
		t.Errorf("xmm0 lo = %016X, want 0xDEADBEEFCAFEBABE", c.xmm[0][0])
	}
	if c.xmm[0][1] != 0 {
		t.Errorf("xmm0 hi = %016X, want 0 (REX.W must zero-extend high lane)", c.xmm[0][1])
	}
}

// TestSSE2_MOVQ_XMMToReg_REXW: the inverse — 66 REX.W 0F 7E /r =
// MOVQ r/m64, xmm. Stores the low 64 bits into the destination GPR.
func TestSSE2_MOVQ_XMMToReg_REXW(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[3] = [2]uint64{0x1122334455667788, 0xFFFFFFFFFFFFFFFF}
	c.SetReg64(RAX, 0)
	// 66 48 0F 7E D8 = MOVQ RAX, XMM3 (mod=11 reg=011 rm=000)
	runMMXCode(t, c, mm, []byte{0x66, 0x48, 0x0F, 0x7E, 0xD8, 0xF4})
	if c.GetReg64(RAX) != 0x1122334455667788 {
		t.Errorf("rax = %016X, want 0x1122334455667788", c.GetReg64(RAX))
	}
}

// TestSSE2_MOVD_RegToXMM: 66 0F 6E /r — no REX.W → 32-bit transfer
// with zero-extension of the upper 96 bits of the destination XMM.
func TestSSE2_MOVD_RegToXMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.SetReg32(EAX, 0xCAFEBABE)
	c.xmm[2] = [2]uint64{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}
	// 66 0F 6E D0 = MOVD XMM2, EAX
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0x6E, 0xD0, 0xF4})
	if c.xmm[2][0] != 0xCAFEBABE {
		t.Errorf("xmm2 lo = %016X, want 0x00000000CAFEBABE", c.xmm[2][0])
	}
	if c.xmm[2][1] != 0 {
		t.Errorf("xmm2 hi = %016X, want 0 (must zero-extend)", c.xmm[2][1])
	}
}

// TestSSE2_PXOR_XMM: 66 0F EF /r — XOR full 128 bits.
func TestSSE2_PXOR_XMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[0] = [2]uint64{0x1111111111111111, 0x2222222222222222}
	c.xmm[1] = [2]uint64{0x1111111111111111, 0x2222222222222222}
	// 66 0F EF C1 = PXOR XMM0, XMM1
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0xEF, 0xC1, 0xF4})
	if c.xmm[0][0] != 0 || c.xmm[0][1] != 0 {
		t.Errorf("PXOR xmm0 = %016X_%016X, want all-0", c.xmm[0][1], c.xmm[0][0])
	}
}

// TestSSE2_PADD_XMM: 66 0F FE /r — packed add of dwords across full
// 128-bit XMM. Carry must NOT propagate across the 64-bit boundary
// (the two 64-bit lanes are independent).
func TestSSE2_PADD_XMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[0] = [2]uint64{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}
	c.xmm[1] = [2]uint64{0x0000000100000001, 0x0000000100000001}
	// 66 0F FE C1 = PADDD XMM0, XMM1
	runMMXCode(t, c, mm, []byte{0x66, 0x0F, 0xFE, 0xC1, 0xF4})
	if c.xmm[0][0] != 0 || c.xmm[0][1] != 0 {
		t.Errorf("PADDD xmm0 = %016X_%016X, want 0_0 (no carry across lanes)", c.xmm[0][1], c.xmm[0][0])
	}
}

// TestSSE2_REX_R_ExtendsXMM: REX.R extends mr.reg to access XMM8..15
// even on the basic MOVDQA. Verifies the index extension is correct
// (no &7 masking that would silently alias XMM8 to XMM0).
func TestSSE2_REX_R_ExtendsXMM(t *testing.T) {
	c, mm := longModeCPU(t)
	c.xmm[3] = [2]uint64{0xCAFEBABEFEEDFACE, 0xDEADBEEF12345678}
	// 66 44 0F 6F C3 = MOVDQA XMM8, XMM3 (REX.R=1 makes reg=8)
	runMMXCode(t, c, mm, []byte{0x66, 0x44, 0x0F, 0x6F, 0xC3, 0xF4})
	if c.xmm[8] != c.xmm[3] {
		t.Errorf("xmm8 = %016X_%016X, want copy of xmm3 = %016X_%016X",
			c.xmm[8][1], c.xmm[8][0], c.xmm[3][1], c.xmm[3][0])
	}
	if c.xmm[0] != ([2]uint64{}) {
		t.Errorf("xmm0 should be untouched (REX.R must not silently alias xmm8→xmm0)")
	}
}
