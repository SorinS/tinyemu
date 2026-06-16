package arm64

import "testing"

// TestARM64_LoadStore holds the single-register load/store encoder byte-exact
// against llvm-mc across every size/sign variant and addressing mode.
func TestARM64_LoadStore(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// size/sign variants, unsigned offset
		"strb w0, [x1]", "strb w0, [x1, #1]", "ldrb w0, [x1, #4095]",
		"strh w0, [x1, #2]", "ldrh w0, [x1, #8]",
		"ldrsb x0, [x1, #1]", "ldrsb w0, [x1, #1]",
		"ldrsh x0, [x1, #2]", "ldrsh w0, [x1, #2]", "ldrsw x0, [x1, #4]",
		"ldr x0, [x1, #8]", "str w0, [x1, #4]",
		// explicit unscaled (STUR/LDUR family)
		"stur x0, [x1, #7]", "ldur x0, [x1, #-8]", "stur w0, [x1, #3]",
		"sturb w0, [x1, #-1]", "ldursw x0, [x1, #1]", "ldur x3, [sp, #255]",
		// str/ldr auto-fall-back to unscaled for unaligned/negative offsets
		"str x0, [x1, #7]", "ldr x0, [x1, #-16]", "ldr w0, [x1, #-4]",
		// pre-index
		"str x0, [x1, #8]!", "ldr x0, [x1, #-16]!", "strb w0, [x1, #1]!",
		"ldr x29, [sp, #16]!",
		// post-index
		"str x0, [x1], #8", "ldr x0, [x1], #-16", "ldrb w0, [x1], #1",
		"ldr x30, [sp], #32",
		// register offset
		"ldr x0, [x1, x2]", "str x0, [x1, x2, lsl #3]", "ldr w0, [x1, x2, lsl #2]",
		"ldr x0, [x1, w2, sxtw]", "ldr x0, [x1, w2, uxtw #3]", "ldr x0, [x1, x2, sxtx]",
		"ldrb w0, [x1, x2]", "strb w0, [x1, w2, uxtw]", "ldrh w0, [x1, x2, lsl #1]",
	}
	for _, src := range cases {
		want, ok := mcEncode(t, src)
		if !ok {
			t.Logf("skip %q (llvm-mc gave no concrete bytes)", src)
			continue
		}
		got, err := Assemble(src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%-32q got % x, llvm % x", src, got, want)
		}
	}
}
