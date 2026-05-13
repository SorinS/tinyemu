package x86

import "testing"

// TestFcomFnstswSahf reproduces the busybox awk comparison pattern:
//
//   fldl  a            ; ST(0) = a
//   fcompl b           ; compare ST(0) with b (m64), pop. Sets C0/C2/C3.
//   fnstsw %ax         ; ax = status word
//   sahf               ; AH → EFLAGS (CF=C0, PF=C2, ZF=C3)
//   setae %al          ; AL = (CF=0)
//
// For a=1.0, b=2.0: a < b, so C0=1, C2=0, C3=0 → CF=1, ZF=0.
// `setae` sets AL=1 if CF=0. Since CF=1, AL should be 0.
//
// If our emulator gets this wrong (AL=1 for 1.0 >= 2.0), this test fails.
func TestFcomFnstswSahf_1lt2(t *testing.T) {
	c := newTestCPU(t)
	// Place doubles at known addresses.
	const aAddr = uint32(0x2000)
	const bAddr = uint32(0x2008)
	// Write 1.0 (0x3FF0000000000000) at aAddr.
	c.writeMem32(aAddr, 0)
	c.writeMem32(aAddr+4, 0x3FF00000)
	// Write 2.0 (0x4000000000000000) at bAddr.
	c.writeMem32(bAddr, 0)
	c.writeMem32(bAddr+4, 0x40000000)

	c.SetReg32(EBX, aAddr)
	c.SetReg32(ECX, bAddr)

	// Sequence:
	//   fldl  [ebx]              DD 03
	//   fcompl [ecx]              DC 19
	//   fnstsw %ax                DF E0
	//   sahf                       9E
	//   setae %al                  0F 93 C0   (set if above or equal: CF=0)
	//   movzbl %al, %eax           0F B6 C0
	code := []byte{
		0xDD, 0x03,
		0xDC, 0x19,
		0xDF, 0xE0,
		0x9E,
		0x0F, 0x93, 0xC0,
		0x0F, 0xB6, 0xC0,
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("(1 >= 2) result: EAX = %d, want 0\n"+
			"  eflags=%08X  AX=%04X", got, c.eflags, c.GetReg16(AX))
	}
}

// TestFucompp exercises DA E9 — FUCOMPP (compare ST(0) with ST(1),
// unordered, pop BOTH). This is busybox awk's compiled `v != 0`
// sequence inside istrue():
//
//   fldz                ; ST(0) = 0
//   fldl v              ; ST(0) = v, ST(1) = 0
//   fucompp             ; compare v vs 0, pop both
//   fnstsw %ax
//   sahf
//   setne %al           ; AL = 1 if !=
//
// With our DA reg-form being a stubbed NOP, FUCOMPP didn't run — flags
// reflected whatever the previous instruction left, and istrue() always
// returned true. That made every `if (numeric_expr)` in awk take the
// then branch.
func TestFucompp_NonZero(t *testing.T) {
	c := newTestCPU(t)
	const vAddr = uint32(0x2000)
	// v = 5.0
	c.writeMem32(vAddr, 0)
	c.writeMem32(vAddr+4, 0x40140000)
	c.SetReg32(EBX, vAddr)
	code := []byte{
		0xD9, 0xEE,           // fldz                  ST(0)=0
		0xDD, 0x03,           // fldl [ebx]             ST(0)=5, ST(1)=0
		0xDA, 0xE9,           // fucompp                compare 5 vs 0, pop both
		0xDF, 0xE0,           // fnstsw %ax
		0x9E,                 // sahf
		0x0F, 0x95, 0xC0,     // setne %al
		0x0F, 0xB6, 0xC0,     // movzbl %al, %eax
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 1 {
		t.Errorf("FUCOMPP(5,0); setne: EAX = %d, want 1 (5 != 0)", got)
	}
}

func TestFucompp_Zero(t *testing.T) {
	c := newTestCPU(t)
	const vAddr = uint32(0x2000)
	// v = 0.0
	c.writeMem32(vAddr, 0)
	c.writeMem32(vAddr+4, 0)
	c.SetReg32(EBX, vAddr)
	code := []byte{
		0xD9, 0xEE,           // fldz
		0xDD, 0x03,           // fldl [ebx]
		0xDA, 0xE9,           // fucompp
		0xDF, 0xE0,           // fnstsw %ax
		0x9E,                 // sahf
		0x0F, 0x95, 0xC0,     // setne %al
		0x0F, 0xB6, 0xC0,     // movzbl %al, %eax
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("FUCOMPP(0,0); setne: EAX = %d, want 0 (0 != 0 is false)", got)
	}
}

// Same shape, but 2 >= 1 should be true (AL = 1).
func TestFcomFnstswSahf_2ge1(t *testing.T) {
	c := newTestCPU(t)
	const aAddr = uint32(0x2000)
	const bAddr = uint32(0x2008)
	c.writeMem32(aAddr, 0)
	c.writeMem32(aAddr+4, 0x40000000) // 2.0
	c.writeMem32(bAddr, 0)
	c.writeMem32(bAddr+4, 0x3FF00000) // 1.0

	c.SetReg32(EBX, aAddr)
	c.SetReg32(ECX, bAddr)

	code := []byte{
		0xDD, 0x03,         // fldl [ebx]
		0xDC, 0x19,         // fcompl [ecx]
		0xDF, 0xE0,         // fnstsw %ax
		0x9E,               // sahf
		0x0F, 0x93, 0xC0,   // setae %al
		0x0F, 0xB6, 0xC0,   // movzbl %al, %eax
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 1 {
		t.Errorf("(2 >= 1) result: EAX = %d, want 1\n"+
			"  eflags=%08X  AX=%04X", got, c.eflags, c.GetReg16(AX))
	}
}
