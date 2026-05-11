package x86

import "testing"

// TestTLBHitAfterPTEClear: after a successful walk caches a translation,
// clearing the PTE should NOT cause the next access to fault. The TLB
// supplies the cached translation until it's explicitly invalidated.
// This mirrors how Linux's free_initmem() can complete safely even
// though its own loop body lives in pages it just unmapped.
func TestTLBHitAfterPTEClear(t *testing.T) {
	c := newTestCPU(t)

	// Build a flat-ish page table for virtual 0xC1000000 → physical 0x01000000.
	pgdAddr := uint32(0x4000)
	ptAddr := uint32(0x5000)
	for i := uint32(0); i < 0x1000; i += 4 {
		c.writeMem32(pgdAddr+i, 0)
		c.writeMem32(ptAddr+i, 0)
	}
	// PDE for virtual 0xC1000000 (PD index 0x304).
	c.writeMem32(pgdAddr+0x304*4, ptAddr|0x07)
	// PTE for virtual 0xC1059000 → phys 0x01059000.
	c.writeMem32(ptAddr+0x059*4, 0x01059000|0x07)
	c.SetCR(3, pgdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	// First access: caches the translation.
	if got := c.translateAddress(0xC10597E8, false, false, false); got != 0x010597E8 {
		t.Fatalf("first translate: got 0x%08X, want 0x010597E8", got)
	}

	// Now clear the PTE in memory — without flushing the TLB.
	c.writePhys32(ptAddr+0x059*4, 0)

	// The TLB hit should still return the cached translation.
	if got := c.translateAddress(0xC10597E8, false, false, false); got != 0x010597E8 {
		t.Errorf("after PTE clear: got 0x%08X, want cached 0x010597E8 (TLB serves stale)", got)
	}
}

// TestTLBInvalidatedByINVLPG: after INVLPG, the cached entry must be gone
// so the next access re-walks (and faults, since the PTE is zero).
func TestTLBInvalidatedByINVLPG(t *testing.T) {
	c := newTestCPU(t)
	pgdAddr := uint32(0x4000)
	ptAddr := uint32(0x5000)
	for i := uint32(0); i < 0x1000; i += 4 {
		c.writeMem32(pgdAddr+i, 0)
		c.writeMem32(ptAddr+i, 0)
	}
	c.writeMem32(pgdAddr+0x304*4, ptAddr|0x07)
	c.writeMem32(ptAddr+0x059*4, 0x01059000|0x07)
	c.SetCR(3, pgdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	_ = c.translateAddress(0xC10597E8, false, false, false)
	c.writePhys32(ptAddr+0x059*4, 0)
	c.tlb.invalidatePage(0xC10597E8)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected page-fault panic after INVLPG + cleared PTE")
		}
		if _, ok := r.(pageFaultError); !ok {
			t.Errorf("expected pageFaultError, got %T: %v", r, r)
		}
	}()
	_ = c.translateAddress(0xC10597E8, false, false, false)
}

// TestTLBFlushAllInvalidates: a global flush evicts every entry.
func TestTLBFlushAllInvalidates(t *testing.T) {
	c := newTestCPU(t)
	pgdAddr := uint32(0x4000)
	ptAddr := uint32(0x5000)
	for i := uint32(0); i < 0x1000; i += 4 {
		c.writeMem32(pgdAddr+i, 0)
		c.writeMem32(ptAddr+i, 0)
	}
	c.writeMem32(pgdAddr+0x304*4, ptAddr|0x07)
	c.writeMem32(ptAddr+0x059*4, 0x01059000|0x07)
	c.SetCR(3, pgdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	_ = c.translateAddress(0xC10597E8, false, false, false)
	c.writePhys32(ptAddr+0x059*4, 0)
	c.tlb.flushAll()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected #PF after flushAll + cleared PTE")
		}
	}()
	_ = c.translateAddress(0xC10597E8, false, false, false)
}
