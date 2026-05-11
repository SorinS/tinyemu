package x86

// Translation Lookaside Buffer (TLB).
//
// Real x86 CPUs cache page-table walks in a TLB. Software relies on this
// caching: when Linux's free_initmem() clears the PTEs for .init.text, it
// continues to execute from those pages briefly (and dispatches one last
// no-op stub call) before calling flush_tlb_all(). Without TLB modelling we
// would page-fault the moment a PTE is cleared, which is what was happening
// in tinyemu-go before this file existed.
//
// The implementation here is intentionally minimal:
//   * Direct-mapped, 4096 entries.
//   * Per-page (4 KB) granularity. 2 MB / 4 MB super-pages are split into
//     individual TLB entries lazily as accesses happen.
//   * Permissions (writable, user-accessible, executable) are cached as
//     boolean flags. A lookup that requires a permission the entry lacks
//     returns "miss" and forces a re-walk, which will then either re-cache
//     with broader perms (rare — the page-table state hasn't changed) or
//     raise #PF (the normal case).
//   * Global (PTE.G) entries survive a CR3 reload. All others get flushed.
//
// The TLB is **not** a correctness layer over the MMU; it is a *cache*. Any
// time software changes paging state, the relevant entries must be flushed.

const (
	tlbEntries = 65536
	tlbMask    = tlbEntries - 1
)

type tlbEntry struct {
	valid    bool
	global   bool
	writable bool
	user     bool
	noExec   bool
	linTag   uint32 // (lin & 0xFFFFF000)
	phys     uint32 // (phys & 0xFFFFF000)
}

type tlb struct {
	entries [tlbEntries]tlbEntry
}

// lookup returns (physical address, hit?). On hit, all requested perm checks
// pass. On miss (including perm-shortfall), caller should walk the page
// table.
func (t *tlb) lookup(lin uint32, write, user, fetch bool) (uint32, bool) {
	idx := (lin >> 12) & tlbMask
	e := &t.entries[idx]
	if !e.valid {
		return 0, false
	}
	if e.linTag != (lin & 0xFFFFF000) {
		return 0, false
	}
	if write && !e.writable {
		return 0, false
	}
	if user && !e.user {
		return 0, false
	}
	if fetch && e.noExec {
		return 0, false
	}
	return e.phys | (lin & 0xFFF), true
}

// insert caches a successfully-resolved translation.
func (t *tlb) insert(lin, phys uint32, writable, user, noExec, global bool) {
	idx := (lin >> 12) & tlbMask
	t.entries[idx] = tlbEntry{
		valid:    true,
		global:   global,
		writable: writable,
		user:     user,
		noExec:   noExec,
		linTag:   lin & 0xFFFFF000,
		phys:     phys & 0xFFFFF000,
	}
}

// flushAll drops every entry. Used on CR0.PG flip and CR4.PGE/PSE/PAE
// toggle — Linux uses the latter as its __flush_tlb_global primitive.
func (t *tlb) flushAll() {
	for i := range t.entries {
		t.entries[i].valid = false
	}
}

// flushNonGlobal drops every entry that doesn't have the G bit. Used on CR3
// reload (PCID-less; matches non-PCID semantics).
func (t *tlb) flushNonGlobal() {
	for i := range t.entries {
		if !t.entries[i].global {
			t.entries[i].valid = false
		}
	}
}

// invalidatePage drops the entry covering a specific linear address. Used by
// INVLPG.
func (t *tlb) invalidatePage(lin uint32) {
	idx := (lin >> 12) & tlbMask
	if t.entries[idx].linTag == (lin & 0xFFFFF000) {
		t.entries[idx].valid = false
	}
}
