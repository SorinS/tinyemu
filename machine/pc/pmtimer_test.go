package pc

import (
	"testing"
	"time"
)

// TestACPIPMTimerRelocates: the ACPI PM timer must follow the PIIX4 PMBA
// when firmware reprograms it. OVMF keeps PMBASE at 0xB000 (timer at
// 0xB008); SeaBIOS moves it to 0x600 and reads the timer at 0x608. If the
// timer doesn't relocate, that read hits nothing and SeaBIOS's PM-timer
// delays spin forever (it hung booting MenuetOS).
func TestACPIPMTimerRelocates(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	// Reprogram PIIX4 PMBA (bus 0, dev 1, fn 3, config 0x40) to base 0x600
	// via the Type-1 config mechanism (0xCF8 address, 0xCFC data).
	const cfgAddr = 0x80000000 | (1 << 11) | (3 << 8) | 0x40
	p.io.Write32(0xCF8, cfgAddr)
	p.io.Write32(0xCFC, 0x0601) // base 0x600 | I/O-space indicator

	// The PM timer now answers at base+8 = 0x608 with a live 24-bit counter.
	v1 := p.io.Read32(0x608)
	if v1 >= 0x01000000 {
		t.Fatalf("PM timer at 0x608 = %#x (high bits set) — not relocated/live", v1)
	}
	time.Sleep(3 * time.Millisecond)
	v2 := p.io.Read32(0x608)
	if v2 == v1 {
		t.Errorf("PM timer did not advance (stuck at %#x); delay loops would hang", v1)
	}
}
