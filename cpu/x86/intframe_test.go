package x86

import "testing"

// TestSameCPLInterruptFrame: when an interrupt is delivered without a
// privilege transition, the CPU must push only EFLAGS, CS, EIP (12 bytes),
// NOT also SS and the original ESP (which would be 20 bytes). The Linux
// SAVE_ALL trampoline relies on this exact layout to find EFLAGS at
// [esp+0x10] / [esp+0x8].
func TestSameCPLInterruptFrame(t *testing.T) {
	c := newTestCPU(t)

	// GDT: ring-0 flat code at 0x08, ring-0 flat data at 0x10.
	gdt := uint32(0x4000)
	for i := uint32(0); i < 24; i++ {
		c.writeMem8(gdt+i, 0)
	}
	c.writeMem8(gdt+8, 0xFF)
	c.writeMem8(gdt+9, 0xFF)
	c.writeMem8(gdt+13, 0x9A)
	c.writeMem8(gdt+14, 0xCF)
	c.writeMem8(gdt+16, 0xFF)
	c.writeMem8(gdt+17, 0xFF)
	c.writeMem8(gdt+21, 0x92)
	c.writeMem8(gdt+22, 0xCF)
	c.SetSegBase(GDTR, gdt)
	c.SetSegLimit(GDTR, 23)
	if err := c.LoadSegmentProtected(SS, 0x10); err != nil {
		t.Fatalf("SS load: %v", err)
	}

	// IDT vector 0x20: interrupt gate, selector 0x08, offset 0x6000.
	idt := uint32(0x5000)
	for i := uint32(0); i < 256*8; i++ {
		c.writeMem8(idt+i, 0)
	}
	g := idt + 0x20*8
	c.writeMem8(g+0, 0x00)
	c.writeMem8(g+1, 0x60)
	c.writeMem8(g+2, 0x08)
	c.writeMem8(g+5, 0x8E)
	c.SetSegBase(IDTR, idt)
	c.SetSegLimit(IDTR, 256*8-1)
	// Handler: HLT.
	c.writeMem8(0x6000, 0xF4)

	// Run INT 0x20 from kernel mode (CPL=0).
	c.SetReg32(ESP, 0x8000)
	c.cpl = 0
	startESP := uint32(0x8000)

	c.writeMem8(0x1000, 0xCD)
	c.writeMem8(0x1001, 0x20)
	c.SetEIP(0x1000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0)

	for i := 0; i < 5 && !c.IsPowerDown(); i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if !c.IsPowerDown() {
		t.Fatalf("handler did not run; EIP=0x%X ESP=0x%X", c.GetEIP(), c.GetReg32(ESP))
	}
	pushed := startESP - c.GetReg32(ESP)
	if pushed != 12 {
		t.Errorf("same-CPL interrupt pushed %d bytes, want 12 (EFLAGS, CS, EIP only)", pushed)
	}
}
