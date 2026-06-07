package pc

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
	"github.com/jtolio/tinyemu-go/mem"
)

// newTestLAPIC builds a LocalAPIC backed by a real x86_64 CPU and a
// caller-controlled cycle counter.
func newTestLAPIC(t *testing.T) (*LocalAPIC, *x86_64.CPU, *uint64) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := x86_64.NewCPU(mm)
	var cyc uint64
	l := NewLocalAPIC(c, func() uint64 { return cyc })
	return l, c, &cyc
}

// A software-disabled APIC presents no interrupts even with a pending IRR.
func TestLAPIC_DisabledNoDelivery(t *testing.T) {
	l, c, _ := newTestLAPIC(t)
	l.requestVector(0x40)
	l.updateINTR()
	if c.GetINTR() != 0 {
		t.Errorf("INTR asserted while APIC software-disabled")
	}
}

// Timer fires the LVT vector, asserts INTR, Ack delivers it, EOI clears it.
func TestLAPIC_TimerDeliverAckEOI(t *testing.T) {
	l, c, cyc := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2)     // enable (bit 8) + spurious 0xFF
	l.MMIOWrite(nil, lapicLVTTimer, 0x40, 2) // unmasked, vector 0x40
	l.MMIOWrite(nil, lapicTimerDCR, 0xB, 2)  // divide by 1
	l.MMIOWrite(nil, lapicTimerICR, 100, 2)  // one-shot, count 100

	if cc := l.MMIORead(nil, lapicTimerCCR, 2); cc == 0 {
		t.Errorf("current count = 0 right after load, want ~100")
	}

	*cyc = 150 // past expiry
	l.Tick(0)
	if c.GetINTR() != 1 {
		t.Fatalf("INTR not asserted after timer expiry")
	}
	vec, ok := l.Ack(func() (uint8, bool) { return 0, false })
	if !ok || vec != 0x40 {
		t.Fatalf("Ack = (%#x,%v), want (0x40,true)", vec, ok)
	}
	if c.GetINTR() != 0 {
		t.Errorf("INTR still asserted after Ack (nothing else pending)")
	}
	// Vector now in service; EOI clears it.
	if isr := l.MMIORead(nil, lapicISRBase+(0x40/32)*16, 2); isr&(1<<(0x40%32)) == 0 {
		t.Errorf("ISR bit for vec 0x40 not set after Ack")
	}
	l.MMIOWrite(nil, lapicEOI, 0, 2)
	if isr := l.MMIORead(nil, lapicISRBase+(0x40/32)*16, 2); isr&(1<<(0x40%32)) != 0 {
		t.Errorf("ISR bit for vec 0x40 still set after EOI")
	}
}

// A masked timer LVT does not deliver.
func TestLAPIC_MaskedTimerNoDelivery(t *testing.T) {
	l, c, cyc := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2)
	l.MMIOWrite(nil, lapicLVTTimer, lvtMasked|0x40, 2) // masked
	l.MMIOWrite(nil, lapicTimerDCR, 0xB, 2)
	l.MMIOWrite(nil, lapicTimerICR, 50, 2)
	*cyc = 100
	l.Tick(0)
	if c.GetINTR() != 0 {
		t.Errorf("masked timer delivered an interrupt")
	}
}

// TPR gates delivery: a vector whose priority class is not above the TPR
// class is held back until the TPR is lowered.
func TestLAPIC_TPRBlocks(t *testing.T) {
	l, c, _ := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2) // enabled
	l.MMIOWrite(nil, lapicTPR, 0x40, 2)  // priority class 4
	l.requestVector(0x35)                // class 3 — below TPR
	l.updateINTR()
	if c.GetINTR() != 0 {
		t.Fatalf("vec 0x35 delivered despite TPR class 4")
	}
	l.MMIOWrite(nil, lapicTPR, 0x20, 2) // lower to class 2 — now deliverable
	if c.GetINTR() != 1 {
		t.Errorf("vec 0x35 not delivered after lowering TPR")
	}
}

// A periodic timer re-arms and fires again after each interval.
func TestLAPIC_PeriodicTimer(t *testing.T) {
	l, c, cyc := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2)
	l.MMIOWrite(nil, lapicLVTTimer, lvtTimerPeriodic|0x40, 2)
	l.MMIOWrite(nil, lapicTimerDCR, 0xB, 2)
	l.MMIOWrite(nil, lapicTimerICR, 100, 2)

	*cyc = 150
	l.Tick(0)
	vec, ok := l.Ack(func() (uint8, bool) { return 0, false })
	if !ok || vec != 0x40 {
		t.Fatalf("first period: Ack=(%#x,%v)", vec, ok)
	}
	l.MMIOWrite(nil, lapicEOI, 0, 2)
	// Next interval (timer reloaded at cyc=150, fires again by 260).
	*cyc = 260
	l.Tick(0)
	if c.GetINTR() != 1 {
		t.Errorf("periodic timer did not re-fire")
	}
}

// A Fixed self-IPI via ICR injects the vector.
func TestLAPIC_SelfIPI(t *testing.T) {
	l, c, _ := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2)
	// ICR: vector 0x50, Fixed delivery, self shorthand (bits 18-19 = 01).
	l.MMIOWrite(nil, lapicICRLow, (1<<18)|icrDeliveryFixed|0x50, 2)
	if c.GetINTR() != 1 {
		t.Fatalf("self-IPI did not assert INTR")
	}
	vec, ok := l.Ack(func() (uint8, bool) { return 0, false })
	if !ok || vec != 0x50 {
		t.Errorf("self-IPI Ack = (%#x,%v), want (0x50,true)", vec, ok)
	}
}

// INIT/SIPI (AP startup) are accepted and dropped — no APs, no delivery.
func TestLAPIC_InitSipiNoDelivery(t *testing.T) {
	l, c, _ := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2)
	l.MMIOWrite(nil, lapicICRLow, icrDeliveryInit, 2)
	l.MMIOWrite(nil, lapicICRLow, icrDeliveryStartup|0x08, 2) // SIPI vector 0x08
	if c.GetINTR() != 0 {
		t.Errorf("INIT/SIPI injected an interrupt (no APs to start)")
	}
}

// The PIC's INTR line, routed in as ExtINT, delivers via the PIC fetch.
func TestLAPIC_ExtINTDelegates(t *testing.T) {
	l, c, _ := newTestLAPIC(t)
	l.MMIOWrite(nil, lapicSVR, 0x1FF, 2) // enabled
	l.SetExtINT(1)
	if c.GetINTR() != 1 {
		t.Fatalf("ExtINT did not assert INTR")
	}
	vec, ok := l.Ack(func() (uint8, bool) { return 0x33, true })
	if !ok || vec != 0x33 {
		t.Errorf("ExtINT Ack = (%#x,%v), want (0x33,true)", vec, ok)
	}
}
