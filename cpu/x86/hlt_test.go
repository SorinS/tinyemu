package x86

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

func newTestCPURealMode(t *testing.T) *CPU {
	mm := mem.NewPhysMemoryMap()
	_, err := mm.RegisterRAM(0, 1<<20, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	return c
}

// TestHLTStepBehavior verifies that Step() does not advance past HLT when
// called directly (the caller is expected to check IsPowerDown).
func TestHLTStepBehavior(t *testing.T) {
	c := newTestCPURealMode(t)

	// Set up flat real mode so CS:IP = 0:0x100 maps to physical 0x100
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)

	// HLT at 0x100, followed by MOV EAX, 0xDEADBEEF at 0x101
	code := []byte{
		0xF4,                   // HLT
		0xB8, 0xEF, 0xBE, 0xAD, 0xDE, // MOV EAX, 0xDEADBEEF
	}
	base := c.GetSegBase(CS)
	for i, b := range code {
		c.writeMem8(base+0x100+uint32(i), b)
	}
	c.SetEIP(0x100)
	c.eflags = 0

	// Step 1: HLT with IF=false
	if err := c.Step(); err != nil {
		t.Fatalf("HLT step error: %v", err)
	}
	if !c.IsPowerDown() {
		t.Fatalf("CPU not powered down after HLT")
	}

	// Current behavior: HLT advances EIP to the next instruction.
	// Step() does not block on powerDown; the caller must check
	// IsPowerDown() to stop calling Step().
	if c.GetEIP() != 0x101 {
		t.Fatalf("EIP after HLT: got 0x%04X, want 0x101 (HLT advances EIP)", c.GetEIP())
	}
}

// TestHLTWithInterrupt verifies that a hardware interrupt wakes the CPU from HLT.
func TestHLTWithInterrupt(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)

	// Set up IVT for vector 8 at 0x0000:0x0020 = physical 0x00020
	c.writeMem16(0x20, 0x0200) // IP = 0x0200
	c.writeMem16(0x22, 0x0000) // CS = 0x0000

	// Handler at 0x0200: IRET
	c.writeMem8(0x200, 0xCF)

	// Main code at 0x100: STI; HLT
	code := []byte{0xFB, 0xF4}
	base := c.GetSegBase(CS)
	for i, b := range code {
		c.writeMem8(base+0x100+uint32(i), b)
	}
	c.SetEIP(0x100)
	c.eflags = 0

	// Step STI
	if err := c.Step(); err != nil {
		t.Fatalf("STI error: %v", err)
	}

	// Step HLT
	if err := c.Step(); err != nil {
		t.Fatalf("HLT error: %v", err)
	}
	if !c.IsPowerDown() {
		t.Fatalf("CPU not halted")
	}

	// Inject interrupt
	c.SetINTR(1)
	c.SetInterruptAckHandler(func() (uint8, bool) {
		c.SetINTR(0)
		return 0x08, true
	})

	// Next Step should service interrupt, not stay at HLT
	if err := c.Step(); err != nil {
		t.Fatalf("interrupt step error: %v", err)
	}
	if c.IsPowerDown() {
		t.Fatalf("CPU still halted after interrupt")
	}
	if c.GetEIP() != 0x200 {
		t.Fatalf("EIP wrong after interrupt: got 0x%04X, want 0x200", c.GetEIP())
	}
}
