package x86_64

// Regression for the hardware-IRQ → IDT delivery path. When INTR is
// asserted by an external device (PIC etc.) and IF=1, Step must:
//   - call the ackInterruptFunc to obtain the vector
//   - deliver the vector through the IDT (no error code)
//   - leave the saved RIP pointing at the instruction we were about
//     to execute (so IRETQ returns there).
// Before the fix the CPU silently dropped pending IRQs and stayed in
// its idle HLT spin forever — the kernel never ran its scheduler.

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

func TestExtIRQ_DeliveredThroughIDT(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)

	// IDT at 0x4000; gate for vector 32 (the typical PIT IRQ
	// after PIC remap) pointing at 0x70000.
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 32, 0x0008, 0x70000, 0, 0x8E)

	c.reg64[RSP] = 0x8000
	c.SetRFLAGS(RFLAGS_IF) // interrupts enabled

	// Wire an ack handler that returns vector 32 on the first call.
	acked := false
	c.SetInterruptAckHandler(func() (uint8, bool) {
		if acked {
			return 0, false
		}
		acked = true
		return 32, true
	})
	c.SetINTR(1)

	// Place a HLT at 0x1000 — if the CPU executed it before checking
	// IRQs we'd be in powerDown, which the test would observe.
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0x90) // NOP
	_ = mm.Write8(code+1, 0xF4)
	c.SetRIP(code)

	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	// Step should have vectored into the handler INSTEAD of executing
	// the NOP at 0x1000.
	if c.GetRIP() != 0x70000 {
		t.Errorf("RIP = %#x, want handler 0x70000 (IRQ delivered before NOP)", c.GetRIP())
	}
	// Saved RIP on the stack should be the NOP address (the instr
	// we'd have executed had the IRQ not fired).
	savedRIP, _ := mm.Read64(c.GetReg64(RSP))
	if savedRIP != code {
		t.Errorf("saved RIP = %#x, want %#x", savedRIP, code)
	}
	// IF should be cleared by the interrupt gate.
	if c.GetRFLAGS()&RFLAGS_IF != 0 {
		t.Errorf("IF still set after interrupt-gate delivery")
	}
}

// TestExtIRQ_Blocked_By_IF: with IF=0, pending IRQ is NOT delivered.
func TestExtIRQ_BlockedByIF(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 32, 0x0008, 0x70000, 0, 0x8E)
	c.reg64[RSP] = 0x8000
	// RFLAGS.IF deliberately clear.
	c.SetINTR(1)
	c.SetInterruptAckHandler(func() (uint8, bool) { return 32, true })

	const code uint64 = 0x1000
	_ = mm.Write8(code, 0x90) // NOP
	_ = mm.Write8(code+1, 0xF4)
	c.SetRIP(code)
	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	// IRQ blocked by IF=0 → NOP runs, RIP advances past it.
	if c.GetRIP() != code+1 {
		t.Errorf("RIP = %#x, want %#x (NOP should have executed)", c.GetRIP(), code+1)
	}
}
