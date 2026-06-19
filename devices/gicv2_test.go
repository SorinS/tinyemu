package devices

import "testing"

// TestGICDeliveryCycle exercises a level-triggered SPI through the full
// enable -> assert -> IAR -> EOIR cycle and checks the CPU IRQ line tracks it.
func TestGICDeliveryCycle(t *testing.T) {
	var cpuIRQ int
	g := NewGICv2(func(level int) { cpuIRQ = level })

	const id = 33 // an SPI (e.g. the UART)
	g.writeC(nil, giccCTLR, 1, 2)             // enable CPU interface
	g.writeC(nil, giccPMR, 0xFF, 2)           // unmask all priorities
	g.writeD(nil, gicdCTLR, 1, 2)             // enable distributor
	g.writeBytes(uint32(id), 0xA0)            // priority 0xA0
	g.writeD(nil, gicdISENABLER+uint32(id/32)*4, 1<<(id%32), 2)

	if cpuIRQ != 0 {
		t.Fatal("IRQ asserted before any source")
	}
	g.SetLine(id, true)
	if cpuIRQ != 1 {
		t.Fatal("IRQ not asserted after line raised")
	}
	// Acknowledge: IAR returns the id, and the line deasserts (now active).
	if got := g.acknowledge(); got != id {
		t.Fatalf("IAR = %d, want %d", got, id)
	}
	if cpuIRQ != 0 {
		t.Fatal("IRQ still asserted after IAR (should be active, not pending)")
	}
	// Device services and lowers the line; EOIR finishes — no re-fire.
	g.SetLine(id, false)
	g.writeC(nil, giccEOIR, id, 2)
	if cpuIRQ != 0 {
		t.Fatal("IRQ re-asserted after line lowered + EOIR")
	}
	// If the line is still high at EOIR, a level interrupt re-fires.
	g.SetLine(id, true)
	if g.acknowledge(); cpuIRQ != 0 {
		t.Fatal("not active after second IAR")
	}
	g.writeC(nil, giccEOIR, id, 2)
	if cpuIRQ != 1 {
		t.Fatal("level interrupt did not re-fire after EOIR with line still high")
	}
}

// TestGICMaskingAndPriority: PMR gates low-priority interrupts.
func TestGICMaskingAndPriority(t *testing.T) {
	var cpuIRQ int
	g := NewGICv2(func(level int) { cpuIRQ = level })
	g.writeC(nil, giccCTLR, 1, 2)
	g.writeD(nil, gicdCTLR, 1, 2)
	const id = 40
	g.prio[id] = 0xA0
	g.enabled[id] = true
	g.writeC(nil, giccPMR, 0x80, 2) // mask: only priorities < 0x80 pass; 0xA0 doesn't
	g.SetLine(id, true)
	if cpuIRQ != 0 {
		t.Fatal("interrupt below PMR was delivered")
	}
	g.writeC(nil, giccPMR, 0xF0, 2) // now 0xA0 < 0xF0 -> passes
	if cpuIRQ != 1 {
		t.Fatal("interrupt not delivered after raising PMR")
	}
}
