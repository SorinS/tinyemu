package x86_64

// Translation Lookaside Buffer (TLB).
//
// Real x86_64 CPUs cache 4-level page-table walks in a multi-level TLB so
// repeated accesses to the same page skip ~12 memory loads. Linux's early
// boot relies on this caching aggressively: many of the first few hundred
// thousand instructions execute against pages that get populated lazily by
// the boot_pf_handler — each NEW page faults once and then runs hot. Without
// a TLB we re-walk the 4-level tables on EVERY access, which makes the boot
// 10–100× slower than necessary and gives the impression of a hang.
//
// This implementation is intentionally minimal but matches real-silicon
// semantics in the bits that matter for correctness:
//
//   - Direct-mapped, 64K entries (same size as the cpu/x86 backend).
//   - Per-page (4 KiB) granularity. 2 MiB / 1 GiB huge pages get split into
//     individual TLB entries lazily, one per touched 4 KiB sub-page.
//   - Permissions (writable, user-accessible, executable) are cached as
//     boolean flags. A lookup that requires a perm the entry lacks misses
//     and forces a re-walk, which on real hardware either re-caches with
//     broader perms or raises #PF — matching what software sees here.
//   - Global (PTE.G) entries survive a CR3 reload. All others get flushed.
//   - INVLPG drops the single entry covering a linear address.
//
// The TLB is **not** a correctness layer over the MMU; it is a *cache*. Any
// time software changes paging state in a way it cares about, the relevant
// entries must be flushed (we hook CR3 writes, EFER.NXE flips, CR0.PG /
// CR4 toggles, and INVLPG).

const (
	tlb64Entries = 65536
	tlb64Mask    = tlb64Entries - 1
)

type tlb64Entry struct {
	valid    bool
	global   bool
	writable bool
	user     bool
	noExec   bool
	linTag   uint64 // (lin & ~0xFFF)
	phys     uint64 // (phys & ~0xFFF)
}

type tlb64 struct {
	entries [tlb64Entries]tlb64Entry
}

// lookup returns (physical address, hit?). On hit, all requested perm
// checks pass and the caller can skip the page-table walk. On miss
// (including perm-shortfall) the caller must walk.
func (t *tlb64) lookup(lin uint64, write, user, fetch bool) (uint64, bool) {
	idx := (lin >> 12) & tlb64Mask
	e := &t.entries[idx]
	if !e.valid {
		return 0, false
	}
	if e.linTag != (lin &^ 0xFFF) {
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

// insert caches a successfully-resolved translation. linAddr and phys
// don't need to be page-aligned by the caller; the bottom 12 bits are
// masked off internally.
func (t *tlb64) insert(lin, phys uint64, writable, user, noExec, global bool) {
	idx := (lin >> 12) & tlb64Mask
	t.entries[idx] = tlb64Entry{
		valid:    true,
		global:   global,
		writable: writable,
		user:     user,
		noExec:   noExec,
		linTag:   lin &^ 0xFFF,
		phys:     phys &^ 0xFFF,
	}
}

// flushAll drops every entry. Called on CR0.PG flip, CR4.PGE/PAE toggle,
// EFER.LMA / NXE flip — any architectural change that broadens or
// narrows what a translation can resolve.
func (t *tlb64) flushAll() {
	for i := range t.entries {
		t.entries[i].valid = false
	}
}

// flushNonGlobal drops every entry that doesn't have the G bit. Used on
// CR3 reload (PCID-less; matches non-PCID semantics). Global entries
// (PTE.G) survive — that's the architectural contract software relies
// on for kernel mappings across address-space switches.
func (t *tlb64) flushNonGlobal() {
	for i := range t.entries {
		if !t.entries[i].global {
			t.entries[i].valid = false
		}
	}
}

// invalidatePage drops the entry covering a specific linear address.
// Used by INVLPG. INVLPG ignores the G bit on real hardware — even
// global mappings get invalidated for the specified page.
func (t *tlb64) invalidatePage(lin uint64) {
	idx := (lin >> 12) & tlb64Mask
	if t.entries[idx].linTag == (lin &^ 0xFFF) {
		t.entries[idx].valid = false
	}
}
