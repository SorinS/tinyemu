package arm64

import "testing"

// TestARM64_DataProc holds the multiply/divide/shift/1-source encoders (and
// their aliases) byte-exact against llvm-mc.
func TestARM64_DataProc(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// multiply (3-source) + aliases
		"madd x0, x1, x2, x3", "madd w0, w1, w2, w3", "msub x0, x1, x2, x3",
		"msub w4, w5, w6, w7", "mul x0, x1, x2", "mul w0, w1, w2",
		"mneg x0, x1, x2", "smull x0, w1, w2", "umull x0, w1, w2",
		"smaddl x0, w1, w2, x3", "umaddl x0, w1, w2, x3", "umsubl x0, w1, w2, x3",
		"smnegl x0, w1, w2", "umnegl x0, w1, w2", "smulh x0, x1, x2", "umulh x0, x1, x2",
		// divide + variable shift (2-source)
		"udiv x0, x1, x2", "udiv w0, w1, w2", "sdiv x0, x1, x2", "sdiv w0, w1, w2",
		"lslv x0, x1, x2", "lsrv w0, w1, w2", "asrv x0, x1, x2", "rorv w0, w1, w2",
		// 1-source
		"rbit x0, x1", "rbit w0, w1", "rev x0, x1", "rev w0, w1",
		"rev16 x0, x1", "rev16 w0, w1", "rev32 x0, x1", "clz x0, x1", "cls w0, w1",
	}
	runDiff(t, cases)
}

// TestARM64_Bitfield holds the bitfield ops and their alias arithmetic
// (lsl/lsr/asr-imm, ubfx/sbfx/bfi/…, uxtb/sxtw/…, ror) byte-exact vs llvm-mc.
func TestARM64_Bitfield(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// base bitfield + extract
		"ubfm x0, x1, #8, #31", "sbfm w0, w1, #4, #15", "bfm x0, x1, #0, #7",
		"extr x0, x1, x2, #8", "extr w0, w1, w2, #4",
		// shift-immediate aliases (ubfm/sbfm)
		"lsl x0, x1, #4", "lsl w0, w1, #3", "lsl x0, x1, #0", "lsl x0, x1, #63",
		"lsr x0, x1, #8", "lsr w0, w1, #1", "asr x0, x1, #2", "asr w0, w1, #31",
		// register-shift aliases (lslv/…) and ror
		"lsl x0, x1, x2", "lsr w0, w1, w2", "asr x0, x1, x2",
		"ror x0, x1, #8", "ror w0, w1, #4", "ror x0, x1, x2",
		// bitfield extract/insert aliases
		"ubfx x0, x1, #4, #8", "sbfx x0, x1, #4, #8", "bfxil x0, x1, #4, #8",
		"ubfiz x0, x1, #4, #8", "sbfiz w0, w1, #3, #5", "bfi x0, x1, #4, #8",
		// sign/zero extend aliases
		"uxtb w0, w1", "uxth w0, w1", "sxtb x0, w1", "sxth x0, w1",
		"sxtw x0, w1", "sxtb w0, w1", "sxth w0, w1",
	}
	runDiff(t, cases)
}

// TestARM64_CondSelAddr covers conditional-select (and its cset/cinc/… aliases)
// and adr/adrp, byte-exact vs llvm-mc.
func TestARM64_CondSelAddr(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// conditional select
		"csel x0, x1, x2, eq", "csel w0, w1, w2, ne", "csinc x0, x1, x2, lt",
		"csinv x0, x1, x2, ge", "csneg x0, x1, x2, gt", "csel x0, x1, x2, hs",
		// condition-inverting aliases
		"cset x0, eq", "cset w0, ne", "csetm x0, lt", "cinc x0, x1, ge",
		"cinv x0, x1, mi", "cneg x0, x1, pl", "cset x5, hi",
		// PC-relative address (numeric offsets)
		"adr x0, #0", "adr x0, #4", "adr x0, #-4", "adr x9, #1048572",
		"adrp x0, #0", "adrp x0, #4096", "adrp x0, #-8192", "adrp x3, #4198400",
	}
	runDiff(t, cases)
}

// runDiff asserts each instruction encodes byte-exact to llvm-mc.
func runDiff(t *testing.T, cases []string) {
	t.Helper()
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
			t.Errorf("%-24q got % x, llvm % x", src, got, want)
		}
	}
}
