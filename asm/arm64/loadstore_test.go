package arm64

import (
	"encoding/binary"
	"testing"
)

// TestARM64_Pair checks ldp/stp/ldpsw across the three addressing modes,
// byte-exact against llvm-mc, plus a hard-coded check of the canonical
// prologue (stp x29, x30, [sp, #-16]! = 0xA9BF7BFD).
func TestARM64_Pair(t *testing.T) {
	requireLLVMMC(t)
	if got, err := Assemble("stp x29, x30, [sp, #-16]!"); err != nil ||
		binary.LittleEndian.Uint32(got) != 0xA9BF7BFD {
		t.Fatalf("prologue stp: got %x err %v, want a9bf7bfd", got, err)
	}
	cases := []string{
		"stp x29, x30, [sp, #-16]!", "ldp x29, x30, [sp], #16",
		"stp x0, x1, [x2]", "stp x0, x1, [x2, #16]", "ldp x0, x1, [x2, #-64]",
		"stp w0, w1, [x2, #8]", "ldp w0, w1, [sp, #4]", "stp w3, w4, [x5], #-8",
		"ldpsw x0, x1, [x2, #4]", "ldp x21, x22, [sp, #504]",
		"stp x0, x1, [sp, #-512]!", "ldp x0, x1, [x2, #248]",
	}
	for _, src := range cases {
		want, ok := mcEncode(t, src)
		if !ok {
			t.Logf("skip %q", src)
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
