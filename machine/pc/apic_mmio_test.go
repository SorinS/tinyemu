package pc

import "testing"

// TestLAPIC_MMIOReadsAsZero: writes & reads to the LAPIC MMIO region
// (0xFEE00000-0xFEE00FFF) must not page-fault; reads return 0; writes are
// silently absorbed. Linux's early CPU init pokes these even with
// `nolapic noapic` on the cmdline.
func TestLAPIC_MMIOReadsAsZero(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	for _, addr := range []uint64{0xFEE00000, 0xFEE003F0, 0xFEE00FF8} {
		if v, _ := p.memMap.Read32(addr); v != 0 {
			t.Errorf("read 0x%X = 0x%X, want 0", addr, v)
		}
		p.memMap.Write32(addr, 0xDEADBEEF)
	}
}

// TestIOAPIC_MMIOReadsAsZero: same property for the IOAPIC region.
func TestIOAPIC_MMIOReadsAsZero(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	for _, addr := range []uint64{0xFEC00000, 0xFEC00100, 0xFEC00FF0} {
		if v, _ := p.memMap.Read32(addr); v != 0 {
			t.Errorf("read 0x%X = 0x%X, want 0", addr, v)
		}
		p.memMap.Write32(addr, 0xDEADBEEF)
	}
}
