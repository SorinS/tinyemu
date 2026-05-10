package pc

import "testing"

// TestPITTickAdvancesCounter verifies Tick(deltaCycles) decrements the channel
// count by deltaCycles/pitCyclesPerTick.
func TestPITTickAdvancesCounter(t *testing.T) {
	pic := NewPIC8259(nil, 0x20)
	p := NewPIT8254(pic)

	p.writeControl(0x36) // ch0, R/W LSB+MSB, mode 3
	p.writeChannel(0, 0x00)
	p.writeChannel(0, 0x10) // reload = 0x1000

	if p.channels[0].count != 0x1000 {
		t.Fatalf("count after load = 0x%04X, want 0x1000", p.channels[0].count)
	}

	p.Tick(uint64(pitCyclesPerTick * 100)) // 100 PIT ticks worth
	if got, want := p.channels[0].count, uint16(0x1000-100); got != want {
		t.Errorf("count after 100 ticks = 0x%04X, want 0x%04X", got, want)
	}
}

// TestPITTickResidualCarry verifies that small deltas accumulate across calls.
func TestPITTickResidualCarry(t *testing.T) {
	pic := NewPIC8259(nil, 0x20)
	p := NewPIT8254(pic)
	p.writeControl(0x36)
	p.writeChannel(0, 0x00)
	p.writeChannel(0, 0x01) // reload = 0x100

	// Each call with delta < pitCyclesPerTick should accumulate.
	for i := 0; i < pitCyclesPerTick; i++ {
		p.Tick(1)
	}
	// pitCyclesPerTick cycles total => 1 PIT tick.
	if got, want := p.channels[0].count, uint16(0x100-1); got != want {
		t.Errorf("count after %d 1-cycle ticks = 0x%04X, want 0x%04X",
			pitCyclesPerTick, got, want)
	}
}

// TestPITLazyAdvanceOnRead verifies that a registered cyclesFunc causes the
// PIT to advance lazily on each access.
func TestPITLazyAdvanceOnRead(t *testing.T) {
	pic := NewPIC8259(nil, 0x20)
	p := NewPIT8254(pic)

	now := uint64(0)
	p.SetCyclesFunc(func() uint64 { return now })

	p.writeControl(0xb0) // ch2, R/W LSB+MSB, mode 0
	p.writeChannel(2, 0xFF)
	p.writeChannel(2, 0xFF) // reload = 0xFFFF

	// Advance 256 PIT ticks of wall time.
	now += uint64(pitCyclesPerTick) * 256

	lo := p.readChannel(2)
	hi := p.readChannel(2)
	count := uint16(lo) | uint16(hi)<<8
	if count != 0xFFFF-256 {
		t.Errorf("count = 0x%04X, want 0x%04X (256 ticks past 0xFFFF)", count, 0xFFFF-256)
	}
}

// TestPITLSBMSBLatch verifies the read state machine: under accessMode 3,
// the first read latches the current count, second returns MSB of that
// snapshot, even if count decremented in between.
func TestPITLSBMSBLatch(t *testing.T) {
	pic := NewPIC8259(nil, 0x20)
	p := NewPIT8254(pic)

	now := uint64(0)
	p.SetCyclesFunc(func() uint64 { return now })

	p.writeControl(0xb0) // ch2, R/W LSB+MSB, mode 0
	p.writeChannel(2, 0x00)
	p.writeChannel(2, 0x10) // count = 0x1000

	now += uint64(pitCyclesPerTick) * 0x500
	// After 0x500 ticks: count = 0x0B00. LSB=0x00, MSB=0x0B.
	lo := p.readChannel(2)
	// Mutate state significantly between reads.
	now += uint64(pitCyclesPerTick) * 0x100
	hi := p.readChannel(2)
	if lo != 0x00 || hi != 0x0B {
		t.Errorf("got LSB=0x%02X MSB=0x%02X, want LSB=0x00 MSB=0x0B (latched at first read)", lo, hi)
	}
}

// TestPITLatchCommand verifies the counter-latch control word (RW bits = 00).
func TestPITLatchCommand(t *testing.T) {
	pic := NewPIC8259(nil, 0x20)
	p := NewPIT8254(pic)

	now := uint64(0)
	p.SetCyclesFunc(func() uint64 { return now })

	p.writeControl(0xb0) // ch2, R/W LSB+MSB, mode 0
	p.writeChannel(2, 0x34)
	p.writeChannel(2, 0x12) // count = 0x1234

	// Latch.
	p.writeControl(0x80) // ch2, latch
	now += uint64(pitCyclesPerTick) * 0x500
	lo := p.readChannel(2)
	hi := p.readChannel(2)
	if lo != 0x34 || hi != 0x12 {
		t.Errorf("got LSB=0x%02X MSB=0x%02X, want LSB=0x34 MSB=0x12", lo, hi)
	}
}

// TestPITChannel0RaisesIRQ verifies that when channel 0 expires, IRQ0 is
// raised on the PIC (and propagates to the CPU via PIC.updateINTR).
func TestPITChannel0RaisesIRQ(t *testing.T) {
	cpu := newPICTestCPU(t)
	pic := NewPIC8259(cpu, 0x20)
	initPIC(pic, 0x20, 0x00)
	p := NewPIT8254(pic)
	p.writeControl(0x36) // ch0, R/W LSB+MSB, mode 3
	p.writeChannel(0, 0x10)
	p.writeChannel(0, 0x00) // reload = 0x10

	if cpu.GetINTR() != 0 {
		t.Fatalf("INTR asserted before timer expired")
	}
	p.Tick(uint64(pitCyclesPerTick) * 0x10)
	if cpu.GetINTR() == 0 {
		t.Fatalf("INTR not asserted after PIT rollover")
	}
}
