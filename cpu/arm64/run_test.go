package arm64

import (
	"testing"

	asmarm64 "github.com/sorins/tinyemu-go/asm/arm64"
	"github.com/sorins/tinyemu-go/mem"
)

func loadAt(t *testing.T, c *CPU, addr uint64, prog []string) {
	t.Helper()
	var off uint64
	for _, s := range prog {
		b, err := asmarm64.Assemble(s)
		if err != nil {
			t.Fatalf("assemble %q: %v", s, err)
		}
		for _, x := range b {
			_ = c.Mem.Write8(addr+off, x)
			off++
		}
	}
}

func newRunCPU(t *testing.T) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mm.Close() })
	return New(mm)
}

// TestIRQDelivery: an asserted, unmasked IRQ vectors to VBAR+0x280 (current EL,
// SPx) and is suppressed while masked.
func TestIRQDelivery(t *testing.T) {
	c := newRunCPU(t)
	c.VBAR = 0x8000
	c.PC = 0x1000
	loadAt(t, c, 0x1000, []string{"nop", "nop", "nop"})

	// Masked (DAIF.I set): no delivery.
	c.DAIF = daifI
	c.SetIRQ(1)
	if c.deliverInterrupts() {
		t.Fatal("IRQ taken while masked")
	}
	// Unmask: delivers to the current-EL SPx IRQ vector (0x200 + 0x80).
	c.DAIF = 0
	if !c.deliverInterrupts() {
		t.Fatal("IRQ not taken when unmasked")
	}
	if c.PC != c.VBAR+0x200+excIRQ {
		t.Errorf("IRQ vector PC=%#x, want %#x", c.PC, c.VBAR+0x200+excIRQ)
	}
	if c.ELR != 0x1000 {
		t.Errorf("ELR=%#x, want 0x1000 (return to interrupted insn)", c.ELR)
	}
	if c.DAIF&daifI == 0 {
		t.Error("IRQ not masked on entry")
	}
}

// TestWFIPowerDown: WFI parks the core; an IRQ wakes it.
func TestWFIPowerDown(t *testing.T) {
	c := newRunCPU(t)
	c.PC = 0x1000
	loadAt(t, c, 0x1000, []string{"wfi", "nop"})
	if err := c.Run(10); err != nil {
		t.Fatal(err)
	}
	if !c.IsPowerDown() {
		t.Fatal("WFI did not power down")
	}
	c.SetIRQ(1)
	if c.IsPowerDown() {
		t.Fatal("IRQ did not wake the core")
	}
}

// TestGenericTimerRegs: CNTV_CVAL/CTL round-trip and the timer fires once the
// counter reaches the compare value.
func TestGenericTimerRegs(t *testing.T) {
	c := newRunCPU(t)
	c.cycles = 100
	c.cntvCval = 500
	c.cntvCtl = cntCtlEnable
	if c.VirtTimerPending() {
		t.Fatal("timer firing before deadline")
	}
	if got := c.NextTimerDeadline(); got != 500 {
		t.Errorf("NextTimerDeadline=%d, want 500", got)
	}
	c.AdvanceCounter(500)
	if !c.VirtTimerPending() {
		t.Fatal("timer not firing at deadline")
	}
	// CNTV_TVAL read = CVAL - counter (signed): now 0.
	if v, _ := c.readTimerReg(a64CNTVTVALField); v != 0 {
		t.Errorf("CNTV_TVAL=%d, want 0", v)
	}
	// Masking suppresses the PPI but ISTATUS still reads set.
	c.cntvCtl |= cntCtlIMask
	if c.VirtTimerPending() {
		t.Error("masked timer still pending")
	}
	if v, _ := c.readTimerReg(a64CNTVCTLField); v&cntCtlIStat == 0 {
		t.Error("ISTATUS not set after deadline")
	}
}
