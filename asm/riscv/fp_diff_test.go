package riscv

import "testing"

func TestRISCV_FP_Differential(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"flw fa0, 8(sp)", "fld fa0, 0(a1)", "fsw fa0, 8(sp)", "fsd fa0, 0(a1)",
		"fadd.s fa0, fa1, fa2", "fsub.s fa0, fa1, fa2", "fmul.s fa0, fa1, fa2", "fdiv.s fa0, fa1, fa2",
		"fsqrt.s fa0, fa1", "fmin.s fa0, fa1, fa2", "fmax.s fa0, fa1, fa2",
		"fsgnj.s fa0, fa1, fa2", "fsgnjn.s fa0, fa1, fa2", "fsgnjx.s fa0, fa1, fa2",
		"feq.s a0, fa1, fa2", "flt.s a0, fa1, fa2", "fle.s a0, fa1, fa2", "fclass.s a0, fa1",
		"fcvt.w.s a0, fa1", "fcvt.wu.s a0, fa1", "fcvt.l.s a0, fa1", "fcvt.lu.s a0, fa1",
		"fcvt.s.w fa0, a1", "fcvt.s.wu fa0, a1", "fcvt.s.l fa0, a1", "fcvt.s.lu fa0, a1",
		"fmv.x.w a0, fa1", "fmv.w.x fa0, a1",
		"fadd.s fa0, fa1, fa2, rtz", "fcvt.w.s a0, fa1, rmm",
		"fmadd.s fa0, fa1, fa2, fa3", "fmsub.s fa0, fa1, fa2, fa3", "fnmsub.s fa0, fa1, fa2, fa3", "fnmadd.s fa0, fa1, fa2, fa3",
		// double
		"fadd.d fa0, fa1, fa2", "fmul.d fa0, fa1, fa2", "fsqrt.d fa0, fa1",
		"feq.d a0, fa1, fa2", "fcvt.w.d a0, fa1", "fcvt.d.w fa0, a1",
		"fcvt.s.d fa0, fa1", "fcvt.d.s fa0, fa1", "fmv.x.d a0, fa1", "fmv.d.x fa0, a1",
		"fmadd.d fa0, fa1, fa2, fa3", "fsgnj.d fa0, fa1, fa2",
		// pseudos
		"fmv.s fa0, fa1", "fabs.s fa0, fa1", "fneg.s fa0, fa1", "fmv.d fa0, fa1",
		// register-name forms
		"fadd.s f0, f1, f2",
	}
	var pass, fail int
	for _, src := range cases {
		want, ok := mcEncode(t, src)
		if !ok {
			t.Logf("SKIP (llvm rejected): %s", src)
			continue
		}
		got, err := Assemble(src)
		switch {
		case err != nil:
			t.Errorf("MISS %-28q: %v", src, err)
			fail++
		case !eq(got, want):
			t.Errorf("DIFF %-28q\n mine % x\n llvm % x", src, got, want)
			fail++
		default:
			pass++
		}
	}
	t.Logf("FP vs llvm-mc: %d pass, %d fail", pass, fail)
}
