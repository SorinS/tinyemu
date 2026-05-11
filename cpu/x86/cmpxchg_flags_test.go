package x86

import "testing"

// Intel SDM (CMPXCHG): the CF, OF, SF, ZF, AF, PF flags are all set
// according to a CMP between the accumulator and the destination operand —
// i.e. as though `CMP acc, dst` had executed. Prior to this fix we only set
// ZF, leaving the other flags stale; that's fine for Linux's typical
// "JNE retry" loops but can break code that branches on JL/JG or SF
// after a cmpxchg.

func TestCmpxchg32SetsAllFlagsOnMatch(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1000)
	c.SetReg32(EBX, 0xDEAD)
	c.SetReg32(ECX, 0x1000) // dest
	// CMPXCHG ECX, EBX — 0F B1 D9 (modrm: mod=11, reg=011 EBX, rm=001 ECX)
	code := []byte{0x0F, 0xB1, 0xD9, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg32(ECX) != 0xDEAD {
		t.Errorf("ECX = 0x%X, want 0xDEAD (match → write src)", c.GetReg32(ECX))
	}
	if !c.getZF() {
		t.Error("expected ZF=1 (acc==dst)")
	}
	if c.getCF() {
		t.Error("expected CF=0 (CMP equal → no borrow)")
	}
	if c.getSF() {
		t.Error("expected SF=0 (CMP equal → 0 result)")
	}
}

func TestCmpxchg32SetsAllFlagsOnMismatch(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 5)   // accumulator
	c.SetReg32(EBX, 100)
	c.SetReg32(ECX, 10)  // dest > acc; CMP 5, 10 borrows → CF=1, SF=1
	code := []byte{0x0F, 0xB1, 0xD9, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg32(EAX) != 10 {
		t.Errorf("EAX = 0x%X, want 10 (mismatch → load dst into accumulator)", c.GetReg32(EAX))
	}
	if c.GetReg32(ECX) != 10 {
		t.Errorf("ECX = 0x%X, want 10 (mismatch → dst unchanged)", c.GetReg32(ECX))
	}
	if c.getZF() {
		t.Error("expected ZF=0 (acc != dst)")
	}
	if !c.getCF() {
		t.Error("expected CF=1 (CMP 5,10 borrows)")
	}
	if !c.getSF() {
		t.Error("expected SF=1 (5-10 is negative)")
	}
}

func TestCmpxchg8SetsCFOnLessThan(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x10)
	c.SetReg32(EBX, 0xCC) // BL as src
	c.SetReg32(ECX, 0x80) // CL as dest > AL
	// CMPXCHG CL, BL — 0F B0 D9 (reg=BL=3, rm=CL=1)
	code := []byte{0x0F, 0xB0, 0xD9, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg8(AL) != 0x80 {
		t.Errorf("AL = 0x%X, want 0x80", c.GetReg8(AL))
	}
	if !c.getCF() {
		t.Error("expected CF=1 (CMP 0x10, 0x80 → borrow)")
	}
}
