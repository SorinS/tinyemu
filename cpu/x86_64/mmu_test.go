package x86_64

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// ptBuilder lays out long-mode page tables in a guest-physical arena
// and exposes a map4K / map2M / map1G API. The non-leaf entries it
// writes always have P|RW|US set so they don't gate the leaf's
// permission bits — permission tests target the leaf entry explicitly.
type ptBuilder struct {
	t        *testing.T
	mm       *mem.PhysMemoryMap
	nextFree uint64
	pml4     uint64
}

func newPTBuilder(t *testing.T, mm *mem.PhysMemoryMap, arenaBase uint64) *ptBuilder {
	t.Helper()
	pml4 := arenaBase
	return &ptBuilder{t: t, mm: mm, nextFree: arenaBase + 0x1000, pml4: pml4}
}

func (b *ptBuilder) allocPT() uint64 {
	addr := b.nextFree
	b.nextFree += 0x1000
	return addr
}

func (b *ptBuilder) read(addr uint64) uint64 {
	v, err := b.mm.Read64(addr)
	if err != nil {
		b.t.Fatalf("Read64(%#x): %v", addr, err)
	}
	return v
}

func (b *ptBuilder) write(addr, val uint64) {
	if err := b.mm.Write64(addr, val); err != nil {
		b.t.Fatalf("Write64(%#x, %#x): %v", addr, val, err)
	}
}

// upsert returns the phys address of the table referenced by
// entryAddr, allocating it if not present.
func (b *ptBuilder) upsert(entryAddr, upperAttrs uint64) uint64 {
	e := b.read(entryAddr)
	if e&pteP != 0 {
		return e & pte4KMask
	}
	tbl := b.allocPT()
	b.write(entryAddr, tbl|upperAttrs)
	return tbl
}

// map4K installs a 4 KiB leaf for lin → phys with leafAttrs (e.g.
// pteRW|pteUS|pteNX). The leaf entry's Present bit and physical-
// address field are added automatically; the caller controls the rest.
func (b *ptBuilder) map4K(lin, phys, leafAttrs uint64) (leafAddr uint64) {
	upper := pteP | pteRW | pteUS
	pml4i := (lin >> 39) & 0x1FF
	pdpti := (lin >> 30) & 0x1FF
	pdi := (lin >> 21) & 0x1FF
	pti := (lin >> 12) & 0x1FF

	pdpt := b.upsert(b.pml4+pml4i*8, upper)
	pd := b.upsert(pdpt+pdpti*8, upper)
	pt := b.upsert(pd+pdi*8, upper)
	leafAddr = pt + pti*8
	b.write(leafAddr, (phys&pte4KMask)|pteP|leafAttrs)
	return leafAddr
}

// map2M installs a 2 MiB leaf at the PD level for lin → phys.
func (b *ptBuilder) map2M(lin, phys, leafAttrs uint64) (leafAddr uint64) {
	upper := pteP | pteRW | pteUS
	pml4i := (lin >> 39) & 0x1FF
	pdpti := (lin >> 30) & 0x1FF
	pdi := (lin >> 21) & 0x1FF

	pdpt := b.upsert(b.pml4+pml4i*8, upper)
	pd := b.upsert(pdpt+pdpti*8, upper)
	leafAddr = pd + pdi*8
	b.write(leafAddr, (phys&pte2MMask)|pteP|ptePS|leafAttrs)
	return leafAddr
}

// map1G installs a 1 GiB leaf at the PDPT level for lin → phys.
func (b *ptBuilder) map1G(lin, phys, leafAttrs uint64) (leafAddr uint64) {
	upper := pteP | pteRW | pteUS
	pml4i := (lin >> 39) & 0x1FF
	pdpti := (lin >> 30) & 0x1FF

	pdpt := b.upsert(b.pml4+pml4i*8, upper)
	leafAddr = pdpt + pdpti*8
	b.write(leafAddr, (phys&pte1GMask)|pteP|ptePS|leafAttrs)
	return leafAddr
}

// pagedCPU builds a CPU with a 16 MiB RAM region from 0 and the page-
// table arena rooted at arenaBase. CR3 is loaded with the arena base
// and the CPU is put in long mode (CR0.PG|PE, CR4.PAE, EFER.LME|LMA).
func pagedCPU(t *testing.T, arenaBase uint64) (*CPU, *ptBuilder) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 16*1024*1024, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE|CR0_PG)
	c.SetCR64(4, CR4_PAE)
	c.SetCR64(3, arenaBase)
	c.SetEFER(EFER_LME | EFER_LMA)
	return c, newPTBuilder(t, mm, arenaBase)
}

func TestIsCanonicalAddr(t *testing.T) {
	cases := []struct {
		addr uint64
		want bool
	}{
		{0x0000_0000_0000_0000, true},
		{0x0000_7FFF_FFFF_FFFF, true},  // last positive canonical
		{0x0000_8000_0000_0000, false}, // first non-canonical
		{0xFFFF_7FFF_FFFF_FFFF, false}, // last non-canonical
		{0xFFFF_8000_0000_0000, true},  // first negative canonical
		{0xFFFF_FFFF_FFFF_FFFF, true},
	}
	for _, tc := range cases {
		if got := IsCanonicalAddr(tc.addr); got != tc.want {
			t.Errorf("IsCanonicalAddr(%#x) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestTranslate_Identity4K(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	b.map4K(lin, lin, pteRW)
	phys, perr := c.Translate(lin, false, false, false)
	if perr != nil {
		t.Fatalf("Translate: %v", perr)
	}
	if phys != lin {
		t.Errorf("phys = %#x, want %#x", phys, lin)
	}
}

func TestTranslate_OffsetInPage(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const linBase = 0x40_0000
	const physBase = 0x60_0000
	b.map4K(linBase, physBase, pteRW)
	for _, off := range []uint64{0, 1, 0x123, 0xFFE, 0xFFF} {
		phys, perr := c.Translate(linBase+off, false, false, false)
		if perr != nil {
			t.Fatalf("off=%#x: %v", off, perr)
		}
		if phys != physBase+off {
			t.Errorf("off=%#x: phys=%#x, want %#x", off, phys, physBase+off)
		}
	}
}

func TestTranslate_2MHugePage(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const linBase = 0x80_0000  // 2 MiB aligned
	const physBase = 0xA0_0000 // 2 MiB aligned
	b.map2M(linBase, physBase, pteRW)
	for _, off := range []uint64{0, 0xFFF, 0x10_0000, 0x1F_FFFF} {
		phys, perr := c.Translate(linBase+off, false, false, false)
		if perr != nil {
			t.Fatalf("off=%#x: %v", off, perr)
		}
		if phys != physBase+off {
			t.Errorf("off=%#x: phys=%#x, want %#x", off, phys, physBase+off)
		}
	}
}

func TestTranslate_1GHugePage(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const linBase = uint64(0x4000_0000)  // 1 GiB aligned
	const physBase = uint64(0x8000_0000) // 1 GiB aligned (outside our 16 MiB RAM, but translate only computes — it doesn't dereference)
	b.map1G(linBase, physBase, pteRW)
	for _, off := range []uint64{0, 0xFFF, 0x10_0000, 0x3FFF_FFFF} {
		phys, perr := c.Translate(linBase+off, false, false, false)
		if perr != nil {
			t.Fatalf("off=%#x: %v", off, perr)
		}
		if phys != physBase+off {
			t.Errorf("off=%#x: phys=%#x, want %#x", off, phys, physBase+off)
		}
	}
}

func TestTranslate_NotPresent_PML4(t *testing.T) {
	c, _ := pagedCPU(t, 0x20_0000)
	// No mapping installed at all. CR3 points to a zeroed PML4, so the
	// very first entry the walker reads has P=0.
	_, perr := c.Translate(0x40_0000, false, false, false)
	if perr == nil {
		t.Fatalf("expected fault")
	}
	if perr.ErrorCode&PFErrP != 0 {
		t.Errorf("ErrorCode=%#x, expected !P", perr.ErrorCode)
	}
	if perr.Addr != 0x40_0000 {
		t.Errorf("fault addr=%#x", perr.Addr)
	}
}

func TestTranslate_NotPresent_PDPT(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	// Install PML4 entry pointing at a PDPT with P=0 for the relevant slot.
	upper := pteP | pteRW | pteUS
	pdpt := b.allocPT()
	b.write(b.pml4+((uint64(0x40_0000)>>39)&0x1FF)*8, pdpt|upper)
	// pdpt slot stays zero ⇒ not-present.
	_, perr := c.Translate(0x40_0000, false, false, false)
	if perr == nil || perr.ErrorCode&PFErrP != 0 {
		t.Errorf("expected not-present, got %v", perr)
	}
}

func TestTranslate_NotPresent_PD(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	upper := pteP | pteRW | pteUS
	pdpt := b.upsert(b.pml4, upper)
	pd := b.allocPT()
	b.write(pdpt, pd|upper) // pdpt[0] -> pd
	// pd entry for (0x40_0000>>21)&0x1FF stays zero.
	_, perr := c.Translate(0x40_0000, false, false, false)
	if perr == nil || perr.ErrorCode&PFErrP != 0 {
		t.Errorf("expected not-present, got %v", perr)
	}
}

func TestTranslate_NotPresent_PT(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	upper := pteP | pteRW | pteUS
	pdpt := b.upsert(b.pml4, upper)
	pd := b.upsert(pdpt, upper)
	pt := b.allocPT()
	b.write(pd+((uint64(0x40_0000)>>21)&0x1FF)*8, pt|upper)
	// PT slot is zero ⇒ not-present.
	_, perr := c.Translate(0x40_0000, false, false, false)
	if perr == nil || perr.ErrorCode&PFErrP != 0 {
		t.Errorf("expected not-present at PT level, got %v", perr)
	}
}

// TestTranslate_WriteToReadOnly exercises the full CR0.WP matrix for a
// write to a read-only page. Per Intel SDM Vol.3 §4.6.1: a user-mode
// write to a read-only page always faults; a supervisor (CPL<3) write
// faults only when CR0.WP=1 — with CR0.WP=0 the R/W bit is ignored for
// supervisor accesses and the write is permitted. OVMF's CpuDxe relies on
// the WP=0 case to edit its own page tables (which DxeIpl maps read-only)
// while WP is clear; mishandling it faulted CpuDxe during attribute sync.
func TestTranslate_WriteToReadOnly(t *testing.T) {
	const lin = 0x40_0000

	// A read is always allowed regardless of WP.
	c, b := pagedCPU(t, 0x20_0000)
	b.map4K(lin, lin, 0) // leaf RW bit clear → read-only page
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Errorf("read of RO page: %v", perr)
	}

	// Supervisor write, CR0.WP=0 → permitted (no fault). pagedCPU leaves
	// WP clear.
	c, b = pagedCPU(t, 0x20_0000)
	b.map4K(lin, lin, 0)
	if _, perr := c.Translate(lin, true /*write*/, false /*user*/, false); perr != nil {
		t.Errorf("supervisor write, WP=0: want success, got fault %#x", perr.ErrorCode)
	}

	// Supervisor write, CR0.WP=1 → faults with P|W.
	c, b = pagedCPU(t, 0x20_0000)
	b.map4K(lin, lin, 0)
	c.SetCR64(0, CR0_PE|CR0_PG|CR0_WP)
	if _, perr := c.Translate(lin, true, false, false); perr == nil {
		t.Fatalf("supervisor write, WP=1: expected fault")
	} else if want := PFErrP | PFErrWrite; perr.ErrorCode != want {
		t.Errorf("supervisor write, WP=1: ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}

	// User write, CR0.WP=0 → still faults (P|W|U).
	c, b = pagedCPU(t, 0x20_0000)
	b.map4K(lin, lin, pteUS) // user-accessible but read-only
	if _, perr := c.Translate(lin, true, true /*user*/, false); perr == nil {
		t.Fatalf("user write to RO: expected fault")
	} else if want := PFErrP | PFErrWrite | PFErrUser; perr.ErrorCode != want {
		t.Errorf("user write, WP=0: ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}
}

func TestTranslate_UserOnSupervisorPage(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	b.map4K(lin, lin, pteRW) // US=0 on leaf
	// Supervisor access OK.
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Errorf("supervisor: %v", perr)
	}
	// User access faults with P|U.
	_, perr := c.Translate(lin, false, true, false)
	if perr == nil {
		t.Fatalf("user access: expected fault")
	}
	want := PFErrP | PFErrUser
	if perr.ErrorCode != want {
		t.Errorf("ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}
}

func TestTranslate_NXFetch_NXEnabled(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	c.SetEFER(c.GetEFER() | EFER_NXE)
	const lin = 0x40_0000
	b.map4K(lin, lin, pteRW|pteNX)
	// Data read OK.
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Errorf("read: %v", perr)
	}
	// Instruction fetch faults with P|F.
	_, perr := c.Translate(lin, false, false, true)
	if perr == nil {
		t.Fatalf("fetch on NX page: expected fault")
	}
	want := PFErrP | PFErrFetch
	if perr.ErrorCode != want {
		t.Errorf("ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}
}

// TestTranslate_NXReserved_NXDisabled: with EFER.NXE clear, setting bit
// 63 in a PTE is a reserved-bit violation, even for non-fetch accesses.
func TestTranslate_NXReserved_NXDisabled(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	// EFER.NXE deliberately cleared.
	const lin = 0x40_0000
	b.map4K(lin, lin, pteRW|pteNX)
	_, perr := c.Translate(lin, false, false, false)
	if perr == nil {
		t.Fatalf("expected fault from NX-bit-as-reserved")
	}
	want := PFErrP | PFErrRsvd
	if perr.ErrorCode != want {
		t.Errorf("ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}
}

// TestTranslate_SoftwareBitsIgnored: per Intel SDM Vol 3 §4.5, bits
// 52..58 of any page-table entry are "ignored" — software-available
// for OS bookkeeping. Linux uses them for swap entries, page-tracking
// flags, etc. Marking those bits as "reserved" was a real bug that
// broke the early #PF handler on the x86_64 Linux boot — Linux sets
// bit 55 in its 2 MiB PDEs and our overstrict mask faulted on it.
// We've reverted to no upper-bit reservations (matching MAXPHYADDR=52);
// only PS-leaf bits and NX-when-NXE-off can be reserved-bit violations.
func TestTranslate_SoftwareBitsIgnored(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	leafAddr := b.map4K(lin, lin, pteRW)
	// Set bits 52..58 in the leaf — these are "available" to software.
	e := b.read(leafAddr)
	for bit := uint64(52); bit <= 58; bit++ {
		b.write(leafAddr, e|(1<<bit))
		if _, perr := c.Translate(lin, false, false, false); perr != nil {
			t.Errorf("bit %d should be ignored, got fault ec=%#x", bit, perr.ErrorCode)
		}
	}
}

func TestTranslate_NonCanonical(t *testing.T) {
	c, _ := pagedCPU(t, 0x20_0000)
	_, perr := c.Translate(0x0000_8000_0000_0000, false, false, false)
	if perr == nil {
		t.Errorf("expected non-canonical fault")
	}
}

func TestTranslate_1G_ReservedAddrBits(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = uint64(0x4000_0000)
	leafAddr := b.map1G(lin, lin, pteRW)
	// Pollute bits 21..29 (which would otherwise index into PD/PT but
	// must read zero for a 1 GiB leaf).
	e := b.read(leafAddr)
	b.write(leafAddr, e|(uint64(1)<<21))
	_, perr := c.Translate(lin, false, false, false)
	if perr == nil {
		t.Fatalf("expected fault from 1G page with non-zero addr bits 21..29")
	}
	want := PFErrP | PFErrRsvd
	if perr.ErrorCode != want {
		t.Errorf("ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}
}

func TestTranslate_2M_ReservedAddrBits(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x80_0000
	leafAddr := b.map2M(lin, lin, pteRW)
	// Pollute bits 12..20 (must be zero for a 2 MiB leaf).
	e := b.read(leafAddr)
	b.write(leafAddr, e|(uint64(1)<<13))
	_, perr := c.Translate(lin, false, false, false)
	if perr == nil {
		t.Fatalf("expected fault from 2M page with non-zero addr bits 12..20")
	}
	want := PFErrP | PFErrRsvd
	if perr.ErrorCode != want {
		t.Errorf("ErrorCode = %#x, want %#x", perr.ErrorCode, want)
	}
}

// TestTranslate_AccessedBitSet: every entry visited along a successful
// walk has its A bit set. The walk is a write so the leaf must also
// get D set.
func TestTranslate_AccessedBitSet(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	leafAddr := b.map4K(lin, lin, pteRW)

	// Snapshot the four entries' phys addresses by walking the tree
	// ourselves first.
	pml4Addr := b.pml4 + ((uint64(lin)>>39)&0x1FF)*8
	pdptBase := b.read(pml4Addr) & pte4KMask
	pdptAddr := pdptBase + ((uint64(lin)>>30)&0x1FF)*8
	pdBase := b.read(pdptAddr) & pte4KMask
	pdAddr := pdBase + ((uint64(lin)>>21)&0x1FF)*8

	// Before translate: none of the entries should have A set.
	for _, addr := range []uint64{pml4Addr, pdptAddr, pdAddr, leafAddr} {
		if b.read(addr)&pteA != 0 {
			t.Fatalf("A bit pre-set at %#x", addr)
		}
	}

	if _, perr := c.Translate(lin, true, false, false); perr != nil {
		t.Fatalf("Translate: %v", perr)
	}

	for _, addr := range []uint64{pml4Addr, pdptAddr, pdAddr, leafAddr} {
		if b.read(addr)&pteA == 0 {
			t.Errorf("A bit not set at %#x after translate", addr)
		}
	}
	if b.read(leafAddr)&pteD == 0 {
		t.Errorf("D bit not set on leaf after write-translate")
	}
}

// TestTranslate_DirtyBit_OnlyOnWrite: a read access does not set D.
func TestTranslate_DirtyBit_OnlyOnWrite(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	const lin = 0x40_0000
	leafAddr := b.map4K(lin, lin, pteRW)
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Fatal(perr)
	}
	if b.read(leafAddr)&pteD != 0 {
		t.Errorf("D bit set on a read-only access")
	}
	if b.read(leafAddr)&pteA == 0 {
		t.Errorf("A bit should be set on read-only access")
	}
}

// TestTranslate_2M_PermissionsAtLeaf: the leaf's RW=0 should make a
// write fault even when the leaf is a 2 MiB huge page. CR0.WP=1 so the
// read-only bit is enforced for the supervisor write (with WP=0 a
// supervisor write would be permitted regardless of the leaf RW bit —
// see TestTranslate_WriteToReadOnly).
func TestTranslate_2M_PermissionsAtLeaf(t *testing.T) {
	c, b := pagedCPU(t, 0x20_0000)
	c.SetCR64(0, CR0_PE|CR0_PG|CR0_WP)
	const lin = 0x80_0000
	b.map2M(lin, lin, 0) // RW clear on leaf
	if _, perr := c.Translate(lin, false, false, false); perr != nil {
		t.Errorf("read on 2M RO page: %v", perr)
	}
	_, perr := c.Translate(lin, true, false, false)
	if perr == nil {
		t.Fatalf("write on 2M RO page: expected fault")
	}
	if perr.ErrorCode != PFErrP|PFErrWrite {
		t.Errorf("ErrorCode = %#x", perr.ErrorCode)
	}
}
