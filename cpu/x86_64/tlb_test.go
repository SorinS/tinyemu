package x86_64

import "testing"

// TestTLB_HitMatchesWalk: a successful Translate primes the TLB, so a
// repeat lookup with no architectural changes must hit and return the
// same physical address.
func TestTLB_HitMatchesWalk(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	const phys = 0x40_0000
	b.map4K(lin, phys, pteRW)
	got1, perr := c.Translate(lin, false, false, false)
	if perr != nil {
		t.Fatalf("first translate: %v", perr)
	}
	if got1 != phys {
		t.Fatalf("first translate phys = %#x, want %#x", got1, phys)
	}
	// Second translate must hit the TLB.
	if _, hit := c.tlb.lookup(lin, false, false, false); !hit {
		t.Fatalf("TLB miss after successful Translate")
	}
	got2, _ := c.Translate(lin, false, false, false)
	if got2 != got1 {
		t.Errorf("repeat translate = %#x, want %#x", got2, got1)
	}
}

// TestTLB_FlushOnCR3 — non-global entries get dropped when CR3 is
// reloaded. A global entry survives.
func TestTLB_FlushOnCR3(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const linNonGlobal = 0x40_0000
	const linGlobal = 0x50_0000
	b.map4K(linNonGlobal, linNonGlobal, pteRW)
	b.map4K(linGlobal, linGlobal, pteRW|pteG)
	// Enable PGE so the global flag actually sticks in the TLB.
	c.cr[4] |= CR4_PGE
	if _, perr := c.Translate(linNonGlobal, false, false, false); perr != nil {
		t.Fatalf("translate non-global: %v", perr)
	}
	if _, perr := c.Translate(linGlobal, false, false, false); perr != nil {
		t.Fatalf("translate global: %v", perr)
	}
	// CR3 reload (same value — flush still happens).
	c.writeCR(3, c.cr[3])
	if _, hit := c.tlb.lookup(linNonGlobal, false, false, false); hit {
		t.Errorf("non-global entry survived CR3 reload")
	}
	if _, hit := c.tlb.lookup(linGlobal, false, false, false); !hit {
		t.Errorf("global entry dropped on CR3 reload")
	}
}

// TestTLB_FlushOnEFERNXE — flipping EFER.NXE changes the meaning of
// bit 63 of every PTE, so the whole TLB has to flush.
func TestTLB_FlushOnEFERNXE(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	b.map4K(lin, lin, pteRW)
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Fatalf("translate: %v", perr)
	}
	if _, hit := c.tlb.lookup(lin, false, false, false); !hit {
		t.Fatalf("TLB miss right after fill")
	}
	c.SetEFER(c.efer ^ EFER_NXE)
	if _, hit := c.tlb.lookup(lin, false, false, false); hit {
		t.Errorf("TLB entry survived EFER.NXE flip")
	}
}

// TestTLB_INVLPG — INVLPG drops the entry for a specific page only.
func TestTLB_INVLPG(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin1 = 0x40_0000
	const lin2 = 0x41_0000
	b.map4K(lin1, lin1, pteRW)
	b.map4K(lin2, lin2, pteRW)
	if _, perr := c.Translate(lin1, false, false, false); perr != nil {
		t.Fatalf("translate 1: %v", perr)
	}
	if _, perr := c.Translate(lin2, false, false, false); perr != nil {
		t.Fatalf("translate 2: %v", perr)
	}
	c.tlb.invalidatePage(lin1)
	if _, hit := c.tlb.lookup(lin1, false, false, false); hit {
		t.Errorf("lin1 entry survived INVLPG")
	}
	if _, hit := c.tlb.lookup(lin2, false, false, false); !hit {
		t.Errorf("INVLPG on lin1 collateral-flushed lin2")
	}
}

// TestTLB_WritePermShortfallMisses — a TLB entry cached on a read
// access must NOT short-circuit a write to the same page if the entry
// lacks write permission. The caller has to re-walk so a real #PF can
// surface (or so a broader entry gets re-cached on real hardware).
func TestTLB_WritePermShortfallMisses(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	// Map read-only (no pteRW).
	b.map4K(lin, lin, 0)
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Fatalf("read translate: %v", perr)
	}
	// Repeat read hits.
	if _, hit := c.tlb.lookup(lin, false, false, false); !hit {
		t.Fatalf("TLB miss after read-translate fill")
	}
	// Write lookup must miss (writable=false) → caller re-walks → fault.
	if _, hit := c.tlb.lookup(lin, true, false, false); hit {
		t.Errorf("TLB returned hit on write against read-only entry")
	}
}
