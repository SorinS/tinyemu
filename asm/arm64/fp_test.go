package arm64

import "testing"

// TestARM64_FPScalar holds the scalar floating-point encoders byte-exact vs
// llvm-mc: 2-source/1-source arithmetic and the fmov reg-reg / FP↔GPR moves.
func TestARM64_FPScalar(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// 2-source arithmetic (single + double)
		"fadd d0, d1, d2", "fadd s0, s1, s2", "fsub d3, d4, d5", "fsub s3, s4, s5",
		"fmul d0, d1, d2", "fmul s6, s7, s8", "fdiv d0, d1, d2", "fdiv s0, s1, s2",
		"fmax d7, d8, d9", "fmin s0, s1, s2", "fmaxnm d0, d1, d2", "fminnm s0, s1, s2",
		"fnmul d0, d1, d2", "fnmul s0, s1, s2",
		// 1-source
		"fabs d0, d1", "fabs s0, s1", "fneg s0, s1", "fneg d0, d1",
		"fsqrt d0, d1", "fsqrt s0, s1",
		// fmov: reg-reg
		"fmov d0, d1", "fmov s0, s1",
		// fmov: FP ↔ GPR
		"fmov d0, x1", "fmov x2, d3", "fmov s0, w1", "fmov w2, s3",
	}
	runDiff(t, cases)
}
