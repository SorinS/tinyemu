package x86

import "testing"

// Intel SDM:
//   INC dst — OF set iff the signed wrap from largest positive to smallest
//   negative occurred (i.e. operand was 0x7F/0x7FFF/0x7FFFFFFF before). CF
//   is NOT modified.
//   DEC dst — OF set iff signed wrap from smallest negative to largest
//   positive occurred (operand was 0x80/0x8000/0x80000000). CF NOT modified.
//
// Prior to this fix the OF condition was inverted (INC used DEC's formula
// and vice versa), so e.g. `INC EAX` of 0x7FFFFFFF left OF=0.

func TestInc32OFOnPositiveToNegative(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x7FFFFFFF)
	c.setCF(true) // pre-set to verify INC doesn't modify it
	// INC EAX = 0x40
	code := []byte{0x40, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x80000000 {
		t.Errorf("EAX = 0x%08X, want 0x80000000", v)
	}
	if !c.getOF() {
		t.Error("expected OF=1 (0x7FFFFFFF + 1 overflows signed)")
	}
	if !c.getCF() {
		t.Error("CF was modified by INC")
	}
	if !c.getSF() {
		t.Error("expected SF=1 (result is negative)")
	}
}

func TestInc32NoOFOnNegativeOne(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xFFFFFFFF) // -1 + 1 = 0; not signed overflow
	c.setCF(true)
	code := []byte{0x40, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0 {
		t.Errorf("EAX = 0x%08X, want 0", v)
	}
	if c.getOF() {
		t.Error("OF should be 0 (-1 + 1 = 0 is not signed overflow)")
	}
	if !c.getZF() {
		t.Error("expected ZF=1")
	}
	if !c.getCF() {
		t.Error("CF was modified by INC")
	}
}

func TestDec32OFOnNegativeToPositive(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x80000000) // -2^31 - 1 wraps to 2^31 - 1
	c.setCF(true)
	// DEC EAX = 0x48
	code := []byte{0x48, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x7FFFFFFF {
		t.Errorf("EAX = 0x%08X, want 0x7FFFFFFF", v)
	}
	if !c.getOF() {
		t.Error("expected OF=1 (0x80000000 - 1 overflows signed)")
	}
	if !c.getCF() {
		t.Error("CF was modified by DEC")
	}
}

func TestDec32NoOFOnZero(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0) // 0 - 1 = -1; not signed overflow
	c.setCF(false)
	code := []byte{0x48, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xFFFFFFFF {
		t.Errorf("EAX = 0x%08X, want 0xFFFFFFFF", v)
	}
	if c.getOF() {
		t.Error("OF should be 0 (0 - 1 = -1 is not signed overflow)")
	}
	if !c.getSF() {
		t.Error("expected SF=1")
	}
	if c.getCF() {
		t.Error("CF was modified by DEC (should stay 0)")
	}
}

func TestInc16OFAndCFPreserved(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(AX, 0x7FFF)
	c.setCF(true)
	// INC AX = 0x40 (16-bit operand: uses 16-bit mode CS, opcode 0x40 = INC AX)
	code := []byte{0x40, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x8000 {
		t.Errorf("AX = 0x%04X, want 0x8000", v)
	}
	if !c.getOF() {
		t.Error("expected OF=1")
	}
	if !c.getCF() {
		t.Error("CF was modified by INC AX")
	}
}

func TestDec16OFAndCFPreserved(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(AX, 0x8000)
	c.setCF(true)
	// DEC AX
	code := []byte{0x48, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x7FFF {
		t.Errorf("AX = 0x%04X, want 0x7FFF", v)
	}
	if !c.getOF() {
		t.Error("expected OF=1")
	}
	if !c.getCF() {
		t.Error("CF was modified by DEC AX")
	}
}
