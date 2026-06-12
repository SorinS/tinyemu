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

// TestPSE4MBSetsAccessedDirty: a 4 MB PSE page must set the Accessed bit in
// its PDE on access and the Dirty bit on write, like 4 KB pages (§3.3).
func TestPSE4MBSetsAccessedDirty(t *testing.T) {
	c := newTestCPU(t)
	pgdAddr := uint32(0x4000)
	for i := uint32(0); i < 0x1000; i += 4 {
		c.writeMem32(pgdAddr+i, 0)
	}
	// 4 MB PSE PDE for linear 0..4 MB → phys 0..4 MB. PS(0x80) + P/RW/US,
	// base 0. A (0x20) and D (0x40) start clear.
	c.writeMem32(pgdAddr+0, 0x80|0x07)
	c.SetCR(4, c.GetCR(4)|CR4_PSE)
	c.SetCR(3, pgdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	// Read access (page 0x1): sets A, not D.
	_ = c.translateAddress(0x00001000, false, false, false)
	if pde := c.readPhys32(pgdAddr); pde&0x20 == 0 {
		t.Errorf("PSE read did not set Accessed (PDE=%#x)", pde)
	} else if pde&0x40 != 0 {
		t.Errorf("PSE read wrongly set Dirty (PDE=%#x)", pde)
	}

	// Write access (different 4 KB page → TLB miss → re-walk): sets D.
	_ = c.translateAddress(0x00002000, true, false, false)
	if pde := c.readPhys32(pgdAddr); pde&0x40 == 0 {
		t.Errorf("PSE write did not set Dirty (PDE=%#x)", pde)
	}
}

// TestCheckStackLimitZeroSize: a zero-size stack check must not underflow
// size-1 and spuriously fault (§3.3).
func TestCheckStackLimitZeroSize(t *testing.T) {
	c := newTestCPU(t)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("checkStackLimit(0, 0) faulted: %v", r)
		}
	}()
	c.checkStackLimit(0, 0)
}
