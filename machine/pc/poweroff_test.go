package pc

import "testing"

// TestACPIPowerOff models a guest powering the machine off through the
// PIIX4 ACPI PM1a control register (PM1a_CNT at PMBASE+4): writing SLP_EN
// (bit 13) with SLP_TYP=5 (S5 soft-off) must request shutdown, while an
// ordinary SCI_EN write (enabling ACPI mode) must not — and must read
// back. This is the path OVMF's EfiResetShutdown and Linux poweroff take;
// without it a guest that asks to power off just spins.
func TestACPIPowerOff(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	const pm1aCnt = acpiPMBase + 0x04 // 0xB004

	if p.IsShutdownRequested() {
		t.Fatal("shutdown requested before any PM1a_CNT write")
	}

	// SCI_EN (bit 0) without SLP_EN: enables ACPI mode, must not power off
	// and must read back so the OS sees ACPI is enabled.
	p.io.Write16(pm1aCnt, 0x0001)
	if p.IsShutdownRequested() {
		t.Errorf("SCI_EN write (no SLP_EN) wrongly requested shutdown")
	}
	if got := p.io.Read16(pm1aCnt); got != 0x0001 {
		t.Errorf("PM1a_CNT readback = %#x, want 0x0001 (SCI_EN)", got)
	}

	// SLP_EN (bit 13) with SLP_TYP=5 (S5): soft-off.
	p.io.Write16(pm1aCnt, (5<<10)|(1<<13))
	if !p.IsShutdownRequested() {
		t.Fatalf("SLP_EN write did not request shutdown")
	}
	if got := p.GetShutdownExitCode(); got != 0 {
		t.Errorf("shutdown exit code = %d, want 0", got)
	}
	// SLP_EN is self-clearing — it must not have been stored.
	if got := p.io.Read16(pm1aCnt) & (1 << 13); got != 0 {
		t.Errorf("SLP_EN read back set (%#x); it is write-only/self-clearing", got)
	}
}
