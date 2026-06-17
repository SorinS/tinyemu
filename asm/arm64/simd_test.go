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
