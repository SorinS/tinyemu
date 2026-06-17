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
