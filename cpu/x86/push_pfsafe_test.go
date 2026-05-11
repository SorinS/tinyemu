package x86

import "testing"

// TestPushPFSafeESPUnchangedOnFault: when a 32-bit push faults (because the
// target page isn't writable), ESP must not be advanced. The earlier
// implementation decremented ESP *before* the memory write, so a fault left
// ESP in an inconsistent state — every subsequent push compounded the
// problem. The kernel's #PF handler then read a bogus pushed frame.
func TestPushPFSafeESPUnchangedOnFault(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE) // real mode for simplicity
	c.SetSegAccess(CS, 0)
	c.SetSeg(SS, 0)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0x100) // small expand-up stack: valid 0..0x100
	c.SetSegAccess(SS, 0x92)
	c.SetReg16(SP, 0x102) // push16 will write at 0x100, out-of-range
	preESP := c.GetReg16(SP)

	c.writeMem8(0x1000, 0x50) // PUSH AX
	c.SetEIP(0x1000)

	defer func() {
		_ = recover()
		if got := c.GetReg16(SP); got != preESP {
			t.Errorf("SP advanced on faulting push: got 0x%04X, want preserved 0x%04X", got, preESP)
		}
	}()
	_ = c.Step()
}
