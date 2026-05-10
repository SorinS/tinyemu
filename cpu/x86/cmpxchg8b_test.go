package x86

import "testing"

// TestCMPXCHG8B_Match: EDX:EAX == [mem], so [mem] is replaced with ECX:EBX
// and ZF is set.
func TestCMPXCHG8B_Match(t *testing.T) {
	c := newTestCPU(t)
	const addr = uint32(0x3000)
	c.writeMem32(addr, 0xCAFEBABE)
	c.writeMem32(addr+4, 0xDEADBEEF)
	c.SetReg32(EAX, 0xCAFEBABE)
	c.SetReg32(EDX, 0xDEADBEEF)
	c.SetReg32(EBX, 0x11111111)
	c.SetReg32(ECX, 0x22222222)
	// CMPXCHG8B [DS:0x3000] = 0F C7 0D 00 30 00 00
	code := []byte{0x0F, 0xC7, 0x0D, 0x00, 0x30, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF not set despite match")
	}
	if got := c.readMem32(addr); got != 0x11111111 {
		t.Errorf("mem lo = 0x%08X, want 0x11111111", got)
	}
	if got := c.readMem32(addr + 4); got != 0x22222222 {
		t.Errorf("mem hi = 0x%08X, want 0x22222222", got)
	}
}

// TestCMPXCHG8B_NoMatch: EDX:EAX != [mem], so [mem] is preserved and EDX:EAX
// is loaded with the current memory value. ZF cleared.
func TestCMPXCHG8B_NoMatch(t *testing.T) {
	c := newTestCPU(t)
	const addr = uint32(0x3000)
	c.writeMem32(addr, 0x11111111)
	c.writeMem32(addr+4, 0x22222222)
	c.SetReg32(EAX, 0xAAAAAAAA) // mismatch
	c.SetReg32(EDX, 0xBBBBBBBB) // mismatch
	c.SetReg32(EBX, 0xCCCCCCCC)
	c.SetReg32(ECX, 0xDDDDDDDD)
	code := []byte{0x0F, 0xC7, 0x0D, 0x00, 0x30, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF set despite mismatch")
	}
	if got := c.readMem32(addr); got != 0x11111111 {
		t.Errorf("mem lo = 0x%08X, want preserved 0x11111111", got)
	}
	if got := c.GetReg32(EAX); got != 0x11111111 {
		t.Errorf("EAX = 0x%08X, want 0x11111111 (loaded from mem)", got)
	}
	if got := c.GetReg32(EDX); got != 0x22222222 {
		t.Errorf("EDX = 0x%08X, want 0x22222222 (loaded from mem)", got)
	}
}
