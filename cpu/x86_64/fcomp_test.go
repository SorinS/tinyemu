package x86_64

// x87 FCOM / FCOMI / FCMOV tests for the long-mode backend. Ported
// from cpu/x86/fcomp_test.go. EBX/ECX 32-bit base-register addressing
// keeps working in long mode (the 0x67 address-size override would
// pick 32-bit explicitly; without it, the registers' high bits stay
// zero in these tests so the 64-bit effective address resolves to the
// same low-page address either way).

import "testing"

// TestFcomFnstswSahf_1lt2: 1.0 < 2.0 → CF=1 after SAHF → setae returns 0.
func TestFcomFnstswSahf_1lt2(t *testing.T) {
	c, mm := longModeCPU(t)
	// Place doubles at 0x2000 (1.0) and 0x2008 (2.0).
	_ = mm.Write64(0x2000, 0x3FF0000000000000) // 1.0
	_ = mm.Write64(0x2008, 0x4000000000000000) // 2.0
	c.SetReg64(RBX, 0x2000)
	c.SetReg64(RCX, 0x2008)
	// fldl [rbx];  fcompl [rcx];  fnstsw %ax;  sahf;  setae %al;  movzbl %al, %eax
	code := []byte{
		0xDD, 0x03,
		0xDC, 0x19,
		0xDF, 0xE0,
		0x9E,
		0x0F, 0x93, 0xC0,
		0x0F, 0xB6, 0xC0,
		0xF4,
	}
	runMMXCode(t, c, mm, code)
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = %d, want 0 (CF=1 from 1<2 → setae = 0)", got)
	}
}

// TestFcomFnstswSahf_2ge1: 2.0 >= 1.0 → CF=0 → setae returns 1.
func TestFcomFnstswSahf_2ge1(t *testing.T) {
	c, mm := longModeCPU(t)
	_ = mm.Write64(0x2000, 0x4000000000000000) // 2.0
	_ = mm.Write64(0x2008, 0x3FF0000000000000) // 1.0
	c.SetReg64(RBX, 0x2000)
	c.SetReg64(RCX, 0x2008)
	code := []byte{
		0xDD, 0x03,
		0xDC, 0x19,
		0xDF, 0xE0,
		0x9E,
		0x0F, 0x93, 0xC0,
		0x0F, 0xB6, 0xC0,
		0xF4,
	}
	runMMXCode(t, c, mm, code)
	if got := c.GetReg32(EAX); got != 1 {
		t.Errorf("EAX = %d, want 1 (CF=0 from 2>=1)", got)
	}
}

// TestFucompp: DA E9 — compare ST(0) with ST(1), pop both.
func TestFucompp_NonZero(t *testing.T) {
	c, mm := longModeCPU(t)
	_ = mm.Write64(0x2000, 0x4014000000000000) // 5.0
	c.SetReg64(RBX, 0x2000)
	code := []byte{
		0xD9, 0xEE, // fldz
		0xDD, 0x03, // fldl [rbx]
		0xDA, 0xE9, // fucompp
		0xDF, 0xE0,
		0x9E,
		0x0F, 0x95, 0xC0, // setne %al
		0x0F, 0xB6, 0xC0,
		0xF4,
	}
	runMMXCode(t, c, mm, code)
	if got := c.GetReg32(EAX); got != 1 {
		t.Errorf("EAX = %d, want 1 (5 != 0)", got)
	}
}

func TestFucompp_Zero(t *testing.T) {
	c, mm := longModeCPU(t)
	_ = mm.Write64(0x2000, 0)
	c.SetReg64(RBX, 0x2000)
	code := []byte{
		0xD9, 0xEE,
		0xDD, 0x03,
		0xDA, 0xE9,
		0xDF, 0xE0,
		0x9E,
		0x0F, 0x95, 0xC0,
		0x0F, 0xB6, 0xC0,
		0xF4,
	}
	runMMXCode(t, c, mm, code)
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = %d, want 0 (0 != 0 is false)", got)
	}
}

// TestFCMOVNB: DB C1 — move ST(1)→ST(0) when CF=0.
func TestFCMOVNB_CFclear_movesValue(t *testing.T) {
	c, mm := longModeCPU(t)
	c.fpu[7] = 42.0
	c.fpu[6] = 7.0
	c.fpuTop = 6
	c.rflags &^= RFLAGS_CF
	runMMXCode(t, c, mm, []byte{0xDB, 0xC1, 0xF4})
	if got := c.fpuST(0); got != 42.0 {
		t.Errorf("FCMOVNB CF=0: ST(0)=%v, want 42.0", got)
	}
}

func TestFCMOVNB_CFset_noMove(t *testing.T) {
	c, mm := longModeCPU(t)
	c.fpu[7] = 42.0
	c.fpu[6] = 7.0
	c.fpuTop = 6
	c.rflags |= RFLAGS_CF
	runMMXCode(t, c, mm, []byte{0xDB, 0xC1, 0xF4})
	if got := c.fpuST(0); got != 7.0 {
		t.Errorf("FCMOVNB CF=1: ST(0)=%v, want 7.0", got)
	}
}

// TestFCOMPP — DE D9 — compare ST(0) with ST(1), pop both.
// Verifies the operand-order bug fix (2026-05-15): FCOMPP must pass
// (ST(0), ST(1)) to fpuCompareSetFlags, NOT (ST(1), ST(0)).
func TestFcompp_GreaterThan_ClearsCF(t *testing.T) {
	c, mm := longModeCPU(t)
	_ = mm.Write64(0x2000, 0x3FF0000000000000) // 1.0 (becomes ST(1))
	_ = mm.Write64(0x2008, 0x4000000000000000) // 2.0 (becomes ST(0))
	c.SetReg64(RBX, 0x2000)
	c.SetReg64(RCX, 0x2008)
	code := []byte{
		0xDD, 0x03, // fldl [rbx]  -> ST(0) = 1
		0xDD, 0x01, // fldl [rcx]  -> ST(0) = 2, ST(1) = 1
		0xDE, 0xD9, // fcompp      -> compare ST(0)=2 vs ST(1)=1
		0xDF, 0xE0,
		0x9E,
		0x0F, 0x92, 0xC0, // setb %al — set if CF=1
		0x0F, 0xB6, 0xC0,
		0xF4,
	}
	runMMXCode(t, c, mm, code)
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = %d, want 0 (2 > 1 → CF=0)", got)
	}
}

// TestFCOMI: DB F1 — set RFLAGS.ZF/PF/CF without FSW shuffle.
func TestFCOMI_lessThan(t *testing.T) {
	c, mm := longModeCPU(t)
	c.fpu[7] = 2.0 // ST(1)
	c.fpu[6] = 1.0 // ST(0)
	c.fpuTop = 6
	runMMXCode(t, c, mm, []byte{0xDB, 0xF1, 0xF4})
	if c.rflags&RFLAGS_CF == 0 {
		t.Errorf("FCOMI(1.0, 2.0): want CF=1, got rflags=%016X", c.rflags)
	}
	if c.rflags&RFLAGS_ZF != 0 {
		t.Errorf("FCOMI(1.0, 2.0): want ZF=0, got rflags=%016X", c.rflags)
	}
}
