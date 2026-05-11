package x86

import "testing"

// TestLSSAtomicityOnBadDescriptor: when LSS loads a selector that fails the
// descriptor check (e.g. RPL/DPL mismatch, not-present), neither the segment
// register nor the destination GPR may be partially updated. The destination
// GPR must hold its pre-instruction value.
func TestLSSAtomicityOnBadDescriptor(t *testing.T) {
	c := newTestCPU(t)
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)
	c.SetSegBase(DS, 0)
	c.SetSegLimit(DS, 0xFFFFFFFF)

	// Empty GDT — selector 0x08 will have descriptor.P=0 (not present).
	gdt := uint32(0x4000)
	for i := uint32(0); i < 16; i++ {
		c.writeMem8(gdt+i, 0)
	}
	c.SetSegBase(GDTR, gdt)
	c.SetSegLimit(GDTR, 15)

	// Operand at 0x6000:  offset=0xCAFEBABE (4 bytes), selector=0x0008 (2 bytes).
	c.writeMem32(0x6000, 0xCAFEBABE)
	c.writeMem8(0x6004, 0x08)
	c.writeMem8(0x6005, 0x00)

	// Initial state we'll check.
	const preESI = uint32(0x11112222)
	c.SetReg32(ESI, preESI)
	c.SetSeg(SS, 0x1234) // not changed by LSS

	// LSS ESI, [0x6000] : 0F B2 35 00 60 00 00 ; HLT after
	code := []byte{0x0F, 0xB2, 0x35, 0x00, 0x60, 0x00, 0x00, 0xF4}
	for i, b := range code {
		c.writeMem8(0x1000+uint32(i), b)
	}
	c.SetEIP(0x1000)
	preSS := c.GetSeg(SS)

	defer func() {
		_ = recover()
		if got := c.GetReg32(ESI); got != preESI {
			t.Errorf("ESI = 0x%08X, want preserved 0x%08X — LSS leaked the offset on fault",
				got, preESI)
		}
		if got := c.GetSeg(SS); got != preSS {
			t.Errorf("SS = 0x%04X, want preserved 0x%04X", got, preSS)
		}
	}()
	_ = c.Step()
}
