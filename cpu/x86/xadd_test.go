package x86

import "testing"

func TestXADD8(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x10)
	c.SetReg8(BL, 0x05)
	code := []byte{0x0F, 0xC0, 0xD8, 0xF4} // XADD AL, BL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg8(AL); got != 0x15 {
		t.Errorf("AL = 0x%02X, want 0x15 (sum)", got)
	}
	if got := c.GetReg8(BL); got != 0x10 {
		t.Errorf("BL = 0x%02X, want 0x10 (original AL)", got)
	}
}

func TestXADD32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x10000000)
	c.SetReg32(EBX, 0x00000005)
	code := []byte{0x0F, 0xC1, 0xD8, 0xF4} // XADD EAX, EBX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0x10000005 {
		t.Errorf("EAX = 0x%08X, want 0x10000005 (sum)", got)
	}
	if got := c.GetReg32(EBX); got != 0x10000000 {
		t.Errorf("EBX = 0x%08X, want 0x10000000 (original EAX)", got)
	}
}

func TestXADD16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xAAAA1000)
	c.SetReg32(EBX, 0xBBBB0005)
	code := []byte{0x66, 0x0F, 0xC1, 0xD8, 0xF4} // XADD AX, BX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xAAAA1005 {
		t.Errorf("EAX = 0x%08X, want 0xAAAA1005", got)
	}
	if got := c.GetReg32(EBX); got != 0xBBBB1000 {
		t.Errorf("EBX = 0x%08X, want 0xBBBB1000 (original AX in low half)", got)
	}
}
