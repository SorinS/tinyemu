package pc

import (
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
	"github.com/sorins/tinyemu-go/mem"
)

// newPICTestCPU builds an x86 CPU connected to a 1 MB RAM region; used so the
// PIC can call cpu.SetINTR on raise/lower.
func newPICTestCPU(t *testing.T) *x86.CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("register ram: %v", err)
	}
	return x86.NewCPU(mm)
}

// initPIC mirrors what an OS would do to mask the PIC and set its vector
// offset before we start raising IRQs.
func initPIC(p *PIC8259, baseVec uint8, mask uint8) {
	p.writeCommand(0x11) // ICW1: cascade, ICW4
	p.writeData(baseVec) // ICW2: base vector
	p.writeData(0x04)    // ICW3
	p.writeData(0x01)    // ICW4: 8086 mode
	p.writeData(mask)    // OCW1: IMR
}

// TestPICRaiseAssertsCPUINTR verifies that RaiseIRQ flips the CPU's INTR line
// and LowerIRQ deasserts it once nothing is pending.
func TestPICRaiseAssertsCPUINTR(t *testing.T) {
	cpu := newPICTestCPU(t)
	p := NewPIC8259(cpu, 0x20)
	initPIC(p, 0x20, 0x00)

	if cpu.GetINTR() != 0 {
		t.Fatalf("INTR asserted before any raise")
	}
	p.RaiseIRQ(0)
	if cpu.GetINTR() == 0 {
		t.Fatalf("INTR not asserted after RaiseIRQ(0)")
	}
	p.LowerIRQ(0)
	if cpu.GetINTR() != 0 {
		t.Fatalf("INTR still asserted after LowerIRQ(0)")
	}
}

// TestPICMaskedIRQNoINTR verifies the IMR suppresses INTR.
func TestPICMaskedIRQNoINTR(t *testing.T) {
	cpu := newPICTestCPU(t)
	p := NewPIC8259(cpu, 0x20)
	initPIC(p, 0x20, 0xFF) // all masked

	p.RaiseIRQ(0)
	if cpu.GetINTR() != 0 {
		t.Fatalf("INTR asserted while IMR masks IRQ0")
	}
}

// TestPICDeliverInterruptDeassertsAfter verifies that delivering the only
// pending interrupt clears INTR.
func TestPICDeliverInterruptDeassertsAfter(t *testing.T) {
	cpu := newPICTestCPU(t)
	p := NewPIC8259(cpu, 0x20)
	initPIC(p, 0x20, 0x00)

	p.RaiseIRQ(3)
	if cpu.GetINTR() == 0 {
		t.Fatalf("INTR not asserted")
	}
	vec := p.DeliverInterrupt()
	if vec != 0x23 {
		t.Errorf("vector = 0x%02X, want 0x23", vec)
	}
	if cpu.GetINTR() != 0 {
		t.Fatalf("INTR still asserted after DeliverInterrupt cleared the only pending IRQ")
	}
}
