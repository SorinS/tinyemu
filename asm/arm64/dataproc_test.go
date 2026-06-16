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
