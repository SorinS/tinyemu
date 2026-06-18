package arm64

import "testing"

// TestARM64_SIMD3 holds the three-same vector encoders byte-exact vs llvm-mc.
func TestARM64_SIMD3(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// add/sub across every arrangement
		"add v0.8b, v1.8b, v2.8b", "add v0.16b, v1.16b, v2.16b",
		"add v0.4h, v1.4h, v2.4h", "add v0.8h, v1.8h, v2.8h",
		"add v0.2s, v1.2s, v2.2s", "add v0.4s, v1.4s, v2.4s",
		"add v0.2d, v1.2d, v2.2d",
		"sub v3.16b, v4.16b, v5.16b", "sub v0.2d, v1.2d, v2.2d",
		"sub v7.4s, v8.4s, v9.4s",
		// mul (not on .2d)
		"mul v0.8b, v1.8b, v2.8b", "mul v0.4h, v1.4h, v2.4h",
		"mul v0.2s, v1.2s, v2.2s", "mul v0.4s, v1.4s, v2.4s",
		// bitwise logicals (.8b / .16b only)
		"and v0.8b, v1.8b, v2.8b", "and v0.16b, v1.16b, v2.16b",
		"orr v0.16b, v1.16b, v2.16b", "eor v0.16b, v1.16b, v2.16b",
		"bic v0.8b, v1.8b, v2.8b", "orn v0.16b, v1.16b, v2.16b",
		// integer compares
		"cmgt v0.4s, v1.4s, v2.4s", "cmge v0.16b, v1.16b, v2.16b", "cmhi v0.8h, v1.8h, v2.8h",
		"cmhs v0.2d, v1.2d, v2.2d", "cmeq v0.4s, v1.4s, v2.4s", "cmtst v0.8b, v1.8b, v2.8b",
		"cmgt v0.2d, v1.2d, v2.2d", "cmeq v0.16b, v1.16b, v2.16b",
	}
	runDiff(t, cases)
}

// TestARM64_SIMDCopy holds the Advanced SIMD copy group (dup/umov/smov/ins)
// byte-exact vs llvm-mc.
func TestARM64_SIMDCopy(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// dup (general) — GPR to all lanes
		"dup v0.4s, w1", "dup v0.16b, w1", "dup v0.2d, x1", "dup v0.8h, w2",
		// dup (element) — lane to all lanes
		"dup v0.4s, v1.s[2]", "dup v0.2d, v1.d[1]", "dup v0.16b, v1.b[3]",
		// umov / smov — lane to GPR
		"umov w0, v1.s[2]", "umov x0, v1.d[1]", "umov w0, v1.b[5]", "umov w0, v1.h[3]",
		"smov x0, v1.b[1]", "smov x0, v1.h[2]", "smov w0, v1.b[0]", "smov x0, v1.s[1]",
		// ins (general) — GPR to lane
		"ins v0.s[1], w2", "ins v0.d[0], x3", "ins v0.b[4], w5",
		// ins (element) — lane to lane
		"ins v0.s[1], v2.s[3]", "ins v0.d[1], v2.d[0]", "ins v0.b[2], v3.b[7]",
	}
	runDiff(t, cases)
}

// TestARM64_SIMDShiftImm holds the vector shift-by-immediate encoders byte-exact
// vs llvm-mc (shl/sshr/ushr/ssra/usra over every arrangement).
func TestARM64_SIMDShiftImm(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"shl v0.4s, v1.4s, #3", "shl v0.16b, v1.16b, #1", "shl v0.2d, v1.2d, #40",
		"shl v0.8h, v1.8h, #15",
		"sshr v0.4s, v1.4s, #3", "ushr v0.4s, v1.4s, #3", "sshr v0.16b, v1.16b, #1",
		"ushr v0.2d, v1.2d, #40", "sshr v0.8h, v1.8h, #16", "sshr v0.2d, v1.2d, #64",
		"ssra v0.4s, v1.4s, #3", "usra v0.4s, v1.4s, #3", "ssra v0.8b, v1.8b, #8",
	}
	runDiff(t, cases)
}

// TestARM64_LdSt1 holds the LD1/ST1 multiple-structures encoders byte-exact vs
// llvm-mc (1..4 registers; no-offset, post-index imm and post-index reg).
func TestARM64_LdSt1(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"ld1 {v0.16b}, [x0]", "st1 {v0.16b}, [x0]", "ld1 {v0.8b}, [x0]",
		"ld1 {v0.4s}, [x1]", "ld1 {v0.2d}, [x1]", "ld1 {v0.4h}, [x2]",
		"ld1 {v0.16b, v1.16b}, [x0]", "ld1 {v0.4s, v1.4s, v2.4s}, [x0]",
		"ld1 {v0.2d, v1.2d, v2.2d, v3.2d}, [x0]",
		"ld1 {v0.16b}, [x1], #16", "st1 {v0.4s}, [x1], #16", "ld1 {v0.16b, v1.16b}, [x0], #32",
		"ld1 {v0.16b}, [x1], x2", "ld1 {v0.8b}, [x1], #8",
		"st1 {v5.8h, v6.8h}, [sp]", "ld1 {v30.4s, v31.4s}, [x0]",
	}
	runDiff(t, cases)
}

// TestARM64_SIMD3F holds the float three-same vector encoders byte-exact vs
// llvm-mc (fadd/fsub/fmul/fdiv/fmax/fmin/fmaxnm/fminnm over .2s/.4s/.2d).
func TestARM64_SIMD3F(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"fadd v0.4s, v1.4s, v2.4s", "fadd v0.2s, v1.2s, v2.2s", "fadd v0.2d, v1.2d, v2.2d",
		"fsub v0.4s, v1.4s, v2.4s", "fsub v0.2d, v1.2d, v2.2d",
		"fmul v0.4s, v1.4s, v2.4s", "fmul v0.2d, v1.2d, v2.2d", "fmul v0.2s, v1.2s, v2.2s",
		"fdiv v0.4s, v1.4s, v2.4s", "fdiv v0.2d, v1.2d, v2.2d",
		"fmax v3.4s, v4.4s, v5.4s", "fmin v0.4s, v1.4s, v2.4s",
		"fmaxnm v0.2d, v1.2d, v2.2d", "fminnm v0.4s, v1.4s, v2.4s",
	}
	runDiff(t, cases)
}
