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

// TestARM64_CarryCondCmp covers add/subtract-with-carry and conditional
// compare (register and immediate forms) byte-exact vs llvm-mc.
func TestARM64_CarryCondCmp(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"adc x0, x1, x2", "adcs x0, x1, x2", "sbc x0, x1, x2", "sbcs x0, x1, x2",
		"adc w0, w1, w2", "sbcs w3, w4, w5", "ngc x0, x1", "ngcs x3, x4",
		"ccmp x0, x1, #0, eq", "ccmp x0, #5, #0, eq", "ccmn x0, x1, #15, ne",
		"ccmp w0, w1, #4, lt", "ccmn x0, #31, #2, ge", "ccmp x9, #0, #0, al",
	}
	runDiff(t, cases)
}

// TestARM64_System covers hints, barriers, system-register move, and exception
// generation byte-exact vs llvm-mc.
func TestARM64_System(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"nop", "yield", "wfe", "wfi", "sev", "sevl", "hint #7",
		"dmb sy", "dmb ish", "dmb ishst", "dsb sy", "dsb nsh", "isb",
		"mrs x0, nzcv", "mrs x1, tpidr_el0", "mrs x2, midr_el1",
		"mrs x3, fpcr", "mrs x4, cntvct_el0", "mrs x5, s3_3_c4_c2_0",
		"msr nzcv, x0", "msr tpidr_el0, x1", "msr fpsr, x2",
		"svc #0", "svc #1", "hvc #2", "smc #3", "brk #0", "brk #0xf000", "hlt #1",
		"msr sctlr_el1, x0", "msr ttbr0_el1, x1", "msr tcr_el1, x2",
		"mrs x3, sctlr_el1", "msr vbar_el1, x4",
		"tlbi vmalle1", "tlbi vae1, x0", "tlbi alle1", "tlbi aside1, x3",
		"tlbi vmalle1is", "tlbi vale1, x5", "tlbi vaae1, x6",
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
