package x86_64

import "fmt"

// Page-table entry bit positions. These live at the same offsets in
// every level (PML4E, PDPTE, PDE, PTE).
const (
	pteP  uint64 = 1 << 0  // Present
	pteRW uint64 = 1 << 1  // Read/Write — 1 allows writes
	pteUS uint64 = 1 << 2  // User/Supervisor — 1 allows CPL=3 access
	pteA  uint64 = 1 << 5  // Accessed (set by CPU on first ref)
	pteD  uint64 = 1 << 6  // Dirty (set by CPU on first write to a leaf)
	ptePS uint64 = 1 << 7  // Page Size — set on a non-leaf turns it into a leaf
	pteG  uint64 = 1 << 8  // Global — leaf-only, gated by CR4.PGE
	pteNX uint64 = 1 << 63 // No-Execute (only honored when EFER.NXE=1)
)

// Physical-address slices inside a 64-bit entry. Long mode reserves
// bits 52..62 for "available to software" / protection-key bits; M1
// treats anything in that window as a reserved-bit violation. Bit 63
// is NX and is gated by EFER.NXE — see translateOne.
//
//	pte4KMask  — leaf at PT level. Aligned to 4 KiB.
//	pte2MMask  — leaf at PD level with PS=1. Aligned to 2 MiB.
//	pte1GMask  — leaf at PDPT level with PS=1. Aligned to 1 GiB.
//
// The interior-entry "next-table" address is also masked with
// pte4KMask: the next-level table base is always 4 KiB-aligned.
const (
	pte4KMask uint64 = 0x000F_FFFF_FFFF_F000
	pte2MMask uint64 = 0x000F_FFFF_FFE0_0000
	pte1GMask uint64 = 0x000F_FFFF_C000_0000

	// Reserved-bit window in long mode. Per Intel SDM Vol 3 §4.5 (Tables
	// 4-14 .. 4-19): on a CPU with MAXPHYADDR == 52 (the spec maximum),
	// bits 52..58 are "ignored" (software-available — Linux uses them for
	// bookkeeping such as _PAGE_BIT_KERNEL / split-lock markers / etc.),
	// bits 59..62 are protection keys when CR4.PKE=1 and ignored when
	// off, and bit 63 is XD/NX (checked separately because its semantics
	// depend on EFER.NXE). We don't currently model a sub-52 MAXPHYADDR
	// or PKE, so leave this empty. Marking 52..62 as reserved here was a
	// real bug that broke Linux's early #PF handler — the kernel's PMD
	// entries set bit 55 for software accounting and would faulted on
	// our overstrict mask.
	pteReservedMask uint64 = 0
)

// Page-fault error-code bits. Same layout as the value the CPU pushes
// on the stack for #PF on real hardware.
const (
	PFErrP     uint32 = 1 << 0 // 1 = protection violation, 0 = not-present
	PFErrWrite uint32 = 1 << 1
	PFErrUser  uint32 = 1 << 2
	PFErrRsvd  uint32 = 1 << 3
	PFErrFetch uint32 = 1 << 4
)

// PageFaultError is returned by Translate when a long-mode page-table
// walk fails. ErrorCode mirrors the #PF error code that real hardware
// pushes on the stack at exception delivery.
type PageFaultError struct {
	Addr      uint64
	ErrorCode uint32
}

func (e *PageFaultError) Error() string {
	return fmt.Sprintf("x86_64: page fault at %#x (errorcode=%#x)", e.Addr, e.ErrorCode)
}

// IsCanonicalAddr reports whether addr is canonical for 48-bit linear
// addressing — bits 63..47 must all be the sign-extension of bit 47.
// LA57 (5-level paging) extends this to bit 56; not modeled.
func IsCanonicalAddr(addr uint64) bool {
	top := addr >> 47
	return top == 0 || top == 0x1FFFF
}

// Translate walks the 4-level long-mode page tables rooted at CR3 and
// returns the physical address for linAddr. The access-mode flags
// (isWrite, isUser, isFetch) drive permission checking and the
// PFErr* bits in any fault returned.
//
// This is the canonical long-mode walker. It is called only when
// CR0.PG=1 and EFER.LMA=1; for any other mode the caller (the
// instruction-side memory accessor) is expected to short-circuit.
//
// The TLB is consulted first; on a hit (translation cached AND all
// requested perms satisfied) we skip the walk entirely. On a miss
// (including perm-shortfall) we walk and, if the walk succeeds, cache
// the effective permissions for next time. Matches real-silicon
// semantics — the cpu/x86 backend uses the same two-step pattern.
func (c *CPU) Translate(linAddr uint64, isWrite, isUser, isFetch bool) (uint64, *PageFaultError) {
	if !IsCanonicalAddr(linAddr) {
		// Non-canonical addresses raise #GP, not #PF, in real hardware
		// — but #PF is what M1's only caller (the data-path memory
		// accessors) reports today. Phase 4 (long-mode entry / fault
		// delivery) re-routes this to #GP. Until then we surface it
		// through PageFaultError with errorCode=0 so tests can detect
		// "translate refused" cleanly.
		return 0, &PageFaultError{Addr: linAddr}
	}

	// TLB fast path. A hit means the cached translation is valid and
	// the requested permissions are satisfied — skip the 4-level walk.
	// Hit semantics match real silicon: even if the underlying PTE has
	// since been cleared, a cached entry remains usable until the
	// software issues INVLPG or a CR3 reload (non-global) or a control-
	// register change that triggers a full flush.
	if phys, hit := c.tlb.lookup(linAddr, isWrite, isUser, isFetch); hit {
		return phys, nil
	}

	nxEnabled := c.efer&EFER_NXE != 0
	cr3 := c.cr[3] & pte4KMask

	// PML4
	pml4Addr := cr3 + ((linAddr>>39)&0x1FF)*8
	e, perr := c.readWalkEntry(pml4Addr, linAddr, isWrite, isUser, isFetch, nxEnabled)
	if perr != nil {
		return 0, perr
	}
	c.maybeMarkAccessed(pml4Addr, e)
	// Track combined permissions through the walk. AND for W/U (every
	// level must allow); OR for NX (any level with NX denies fetch).
	combinedW := e&pteRW != 0
	combinedU := e&pteUS != 0
	combinedNX := nxEnabled && e&pteNX != 0

	// PDPT
	pdptAddr := (e & pte4KMask) + ((linAddr>>30)&0x1FF)*8
	e, perr = c.readWalkEntry(pdptAddr, linAddr, isWrite, isUser, isFetch, nxEnabled)
	if perr != nil {
		return 0, perr
	}
	combinedW = combinedW && (e&pteRW != 0)
	combinedU = combinedU && (e&pteUS != 0)
	combinedNX = combinedNX || (nxEnabled && e&pteNX != 0)
	if e&ptePS != 0 {
		// 1 GiB huge page. Leaf at PDPT.
		// Verify bits 21..29 (which would otherwise index into PD/PT)
		// are zero — non-zero bits there are reserved-bit violations.
		if e&0x3FFF_E000 != 0 {
			return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, true)}
		}
		c.maybeMarkLeaf(pdptAddr, e, isWrite)
		phys := (e & pte1GMask) | (linAddr & 0x3FFF_FFFF)
		c.tlbInsert(linAddr, phys, combinedW, combinedU, combinedNX, e&pteG != 0)
		return phys, nil
	}
	c.maybeMarkAccessed(pdptAddr, e)

	// PD
	pdAddr := (e & pte4KMask) + ((linAddr>>21)&0x1FF)*8
	e, perr = c.readWalkEntry(pdAddr, linAddr, isWrite, isUser, isFetch, nxEnabled)
	if perr != nil {
		return 0, perr
	}
	combinedW = combinedW && (e&pteRW != 0)
	combinedU = combinedU && (e&pteUS != 0)
	combinedNX = combinedNX || (nxEnabled && e&pteNX != 0)
	if e&ptePS != 0 {
		// 2 MiB huge page. Leaf at PD.
		// Bits 12..20 must be zero.
		if e&0x1F_E000 != 0 {
			return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, true)}
		}
		c.maybeMarkLeaf(pdAddr, e, isWrite)
		phys := (e & pte2MMask) | (linAddr & 0x1F_FFFF)
		c.tlbInsert(linAddr, phys, combinedW, combinedU, combinedNX, e&pteG != 0)
		return phys, nil
	}
	c.maybeMarkAccessed(pdAddr, e)

	// PT
	ptAddr := (e & pte4KMask) + ((linAddr>>12)&0x1FF)*8
	e, perr = c.readWalkEntry(ptAddr, linAddr, isWrite, isUser, isFetch, nxEnabled)
	if perr != nil {
		return 0, perr
	}
	combinedW = combinedW && (e&pteRW != 0)
	combinedU = combinedU && (e&pteUS != 0)
	combinedNX = combinedNX || (nxEnabled && e&pteNX != 0)
	c.maybeMarkLeaf(ptAddr, e, isWrite)
	phys := (e & pte4KMask) | (linAddr & 0xFFF)
	c.tlbInsert(linAddr, phys, combinedW, combinedU, combinedNX, e&pteG != 0)
	return phys, nil
}

// tlbInsert caches a freshly resolved translation. Centralised so the
// CR4.PGE gating on the "global" flag lives in one place (matches the
// architectural rule that PTE.G has no effect when PGE is off).
func (c *CPU) tlbInsert(lin, phys uint64, writable, user, noExec, leafGlobal bool) {
	global := leafGlobal && c.cr[4]&CR4_PGE != 0
	c.tlb.insert(lin, phys, writable, user, noExec, global)
}

// readWalkEntry loads a page-table entry and runs the common checks
// that apply at every level: physical-read success, Present bit,
// reserved-bit window, NX gating, write protection, user/supervisor.
// On success it returns the entry value with no side effects (A/D bits
// are set by maybeMarkAccessed / maybeMarkLeaf so the caller controls
// whether a non-leaf or leaf marking applies).
func (c *CPU) readWalkEntry(entryAddr, linAddr uint64, isWrite, isUser, isFetch, nxEnabled bool) (uint64, *PageFaultError) {
	v, err := c.memMap.Read64(entryAddr)
	if err != nil {
		// A page-table walk that hits unmapped phys memory is treated
		// as a not-present fault from the guest's perspective. Real
		// hardware would have to define what it does here; the guest
		// shouldn't be relying on this case anyway.
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(false, isWrite, isUser, isFetch, false)}
	}
	if v&pteP == 0 {
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(false, isWrite, isUser, isFetch, false)}
	}
	if v&pteReservedMask != 0 {
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, true)}
	}
	if !nxEnabled && v&pteNX != 0 {
		// NX bit set while EFER.NXE=0 is a reserved-bit violation.
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, true)}
	}
	if isWrite && v&pteRW == 0 && (isUser || c.cr[0]&CR0_WP != 0) {
		// Write to a read-only page. A user-mode write always faults; a
		// supervisor write faults only when CR0.WP=1. With CR0.WP=0 a
		// supervisor (CPL<3) write to a read-only page is permitted (Intel
		// SDM Vol.3 §4.6.1) — OVMF's CpuDxe relies on this to edit its own
		// page tables (which DxeIpl maps read-only) while WP is clear.
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, false)}
	}
	if isUser && v&pteUS == 0 {
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, false)}
	}
	if isFetch && nxEnabled && v&pteNX != 0 {
		return 0, &PageFaultError{Addr: linAddr, ErrorCode: faultCode(true, isWrite, isUser, isFetch, false)}
	}
	return v, nil
}

// maybeMarkAccessed sets the A bit on a non-leaf entry if not already
// set. The CPU writes A "atomically" on real hardware; from the
// emulator's perspective it is just a Read-modify-Write to the entry
// in physical memory.
func (c *CPU) maybeMarkAccessed(entryAddr, entry uint64) {
	if entry&pteA != 0 {
		return
	}
	_ = c.memMap.Write64(entryAddr, entry|pteA)
}

// maybeMarkLeaf sets the A bit, and on writes the D bit, on a leaf
// entry (regardless of page-size level).
func (c *CPU) maybeMarkLeaf(entryAddr, entry uint64, isWrite bool) {
	want := entry | pteA
	if isWrite {
		want |= pteD
	}
	if want == entry {
		return
	}
	_ = c.memMap.Write64(entryAddr, want)
}

// faultCode builds the standard x86 #PF error code from the access
// flags and the current outcome (protection-violation vs not-present;
// reserved-bit set).
func faultCode(present, isWrite, isUser, isFetch, rsvd bool) uint32 {
	var ec uint32
	if present {
		ec |= PFErrP
	}
	if isWrite {
		ec |= PFErrWrite
	}
	if isUser {
		ec |= PFErrUser
	}
	if rsvd {
		ec |= PFErrRsvd
	}
	if isFetch {
		ec |= PFErrFetch
	}
	return ec
}
