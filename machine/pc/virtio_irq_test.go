package pc

import "testing"

// TestVirtIOIRQCaptured: each IRQ signal from GetVirtIOIRQ must stay bound
// to the line the device had when the signal was created. The old closure
// read p.virtioCount at fire time, so adding a second device silently
// re-routed the first device's IRQ. virtio IRQs (8+) land on the slave
// PIC, so its IRR bit identifies the line: device 0 -> IRQ 8 (slave bit 0),
// device 1 -> IRQ 9 (slave bit 1).
func TestVirtIOIRQCaptured(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	sig0 := p.GetVirtIOIRQ() // virtioCount = 0 -> IRQ 8
	if _, err := p.AddVirtIODevice(nil); err != nil {
		t.Fatalf("AddVirtIODevice: %v", err)
	}
	sig1 := p.GetVirtIOIRQ() // virtioCount = 1 -> IRQ 9
	if _, err := p.AddVirtIODevice(nil); err != nil {
		t.Fatalf("AddVirtIODevice: %v", err)
	}
	// virtioCount is now 2; the old code would route both signals to IRQ 10.

	sig0.Raise()
	if p.pic.slave.irr&0x01 == 0 {
		t.Errorf("sig0 did not raise IRQ 8 (slave IRR=%#x) — dynamic virtioCount re-routed it", p.pic.slave.irr)
	}
	if p.pic.slave.irr&0x04 != 0 {
		t.Errorf("sig0 wrongly raised IRQ 10 (slave IRR bit 2 set)")
	}

	sig1.Raise()
	if p.pic.slave.irr&0x02 == 0 {
		t.Errorf("sig1 did not raise IRQ 9 (slave IRR=%#x)", p.pic.slave.irr)
	}
}
