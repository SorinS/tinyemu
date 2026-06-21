package arm64

import "fmt"

// AArch64 stage-1 address translation (4 KiB granule, TTBR0). When the MMU is
// disabled (SCTLR_EL1.M == 0) translation is the identity. Faults are returned
// as *abort, carrying the kind and faulting address so a future VBAR_EL1
// exception path is a slot-in rather than a rewrite.

type accessType int

const (
	accessRead accessType = iota
	accessWrite
	accessExec
)

// abort is a translation/permission fault. It satisfies error so the executor
// can propagate it through the existing (value, error) memory paths.
type abort struct {
	kind  string // "translation", "permission", "accessflag", "address-size"
	level int
	far   uint64 // faulting virtual address
	write bool
}

func (a *abort) Error() string {
	return fmt.Sprintf("arm64 MMU %s fault at va=%#x (level %d)", a.kind, a.far, a.level)
}

const descAddrMask = 0xFFFFFFFFF000 // output/next-table address, bits [47:12]

// mmuEnabled reports whether stage-1 translation is on (SCTLR_EL1.M).
func (c *CPU) mmuEnabled() bool { return c.SCTLR&1 == 1 }

// startLevel derives the initial walk level for the 4 KiB granule from T0SZ.
// VAwidth = 64-T0SZ; each level resolves 9 bits above the 12-bit page offset.
func startLevel(t0sz uint64) int {
	vaWidth := 64 - int(t0sz)
	levels := (vaWidth - 12 + 8) / 9 // ceil((VAwidth-12)/9)
	return 4 - levels
}

// The TLB is set-associative: tlbSets sets of tlbWays entries each. A direct-
// mapped cache evicts a still-valid translation as soon as another VA collides
// on its index — but software relies on translations surviving across a
// break-before-make page-table split (an OS points a table entry at a new,
// not-yet-populated table and accesses in-region pages via their cached
// translation until it issues TLBI). Associativity keeps those entries alive.
const (
	tlbSets = 512
	tlbWays = 8
)

// tlbEntry caches one 4 KiB VA→PA translation plus whether the page is writable.
type tlbEntry struct {
	valid    bool
	vpn      uint64
	ppn      uint64
	writable bool
}

// flushTLB invalidates every cached translation.
func (c *CPU) flushTLB() {
	c.tlb = [tlbSets][tlbWays]tlbEntry{}
	c.tlbNext = [tlbSets]uint8{}
}

// flushTLBPage invalidates only the cached translation for one VA page (by VA
// page number), as a VA-targeted TLBI does. Other entries survive — an OS relies
// on this during break-before-make page-table splits.
func (c *CPU) flushTLBPage(vpn uint64) {
	vpn &= tlbVPNMask
	set := &c.tlb[vpn&(tlbSets-1)]
	for i := range set {
		if set[i].valid && set[i].vpn&tlbVPNMask == vpn {
			set[i] = tlbEntry{}
		}
	}
}

// tlbVPNMask is VA[55:12] — the page-number width a TLBI VA operand encodes.
const tlbVPNMask = (1 << 44) - 1

// tlbLookup returns a cached entry for vpn, or nil on a miss.
func (c *CPU) tlbLookup(vpn uint64) *tlbEntry {
	set := &c.tlb[vpn&(tlbSets-1)]
	for i := range set {
		if set[i].valid && set[i].vpn == vpn {
			return &set[i]
		}
	}
	return nil
}

// tlbInsert caches a translation, round-robin within the set.
func (c *CPU) tlbInsert(e tlbEntry) {
	s := e.vpn & (tlbSets - 1)
	way := c.tlbNext[s]
	c.tlb[s][way] = e
	c.tlbNext[s] = (way + 1) % tlbWays
}

// translate resolves a virtual to a physical address for the given access,
// consulting the TLB and walking the page tables on a miss.
func (c *CPU) translate(vaddr uint64, access accessType) (uint64, *abort) {
	if !c.mmuEnabled() {
		return vaddr, nil
	}
	write := access == accessWrite
	vpn := vaddr >> 12
	if e := c.tlbLookup(vpn); e != nil {
		if write && !e.writable {
			return 0, c.fault("permission", 3, vaddr, write)
		}
		return e.ppn<<12 | (vaddr & 0xFFF), nil
	}
	pa, writable, ab := c.walk(vaddr, write)
	if ab != nil {
		return 0, ab
	}
	c.tlbInsert(tlbEntry{valid: true, vpn: vpn, ppn: pa >> 12, writable: writable})
	if write && !writable {
		return 0, c.fault("permission", 3, vaddr, write)
	}
	return pa, nil
}

// walk performs the stage-1 table walk and the access-flag check, returning the
// physical address and whether the page is writable (AP[2]==0). Write-permission
// enforcement is left to translate so a cached entry can serve both reads and
// writes.
func (c *CPU) walk(vaddr uint64, write bool) (uint64, bool, *abort) {
	// Pick the translation base by the top VA bit: the low half uses TTBR0,
	// the high half TTBR1. The bits above the configured region must be all-0
	// (TTBR0) or all-1 (TTBR1), else it's an address-size fault (the VA hole).
	var base, tsz uint64
	if vaddr&(1<<63) == 0 {
		tsz = c.TCR & 0x3F
		if vaddr>>(64-tsz) != 0 {
			return 0, false, c.fault("address-size", 0, vaddr, write)
		}
		base = c.TTBR0 & descAddrMask
	} else {
		tsz = (c.TCR >> 16) & 0x3F
		if ^vaddr>>(64-tsz) != 0 {
			return 0, false, c.fault("address-size", 0, vaddr, write)
		}
		base = c.TTBR1 & descAddrMask
	}
	level := startLevel(tsz)
	table := base

	for {
		shift := uint(12 + 9*(3-level))
		idx := (vaddr >> shift) & 0x1FF
		descAddr := table + idx*8
		desc, err := c.Mem.Read64(descAddr)
		if err != nil || desc&1 == 0 { // unreadable table or invalid descriptor
			return 0, false, c.fault("translation", level, vaddr, write)
		}
		if desc&2 != 0 { // bit1 set
			if level == 3 { // 0b11 at L3 = page (leaf)
				return c.leaf(desc&descAddrMask|(vaddr&0xFFF), desc, descAddr, level, vaddr, write)
			}
			table = desc & descAddrMask // 0b11 at L0–L2 = table; descend
			level++
			continue
		}
		// bit1 clear → 0b01 = block descriptor. Valid only at L1/L2 (4 KiB):
		// L0 has no block form, and 0b01 at L3 is reserved/invalid.
		if level == 1 || level == 2 {
			blockMask := descAddrMask &^ ((uint64(1) << shift) - 1)
			pa := (desc & blockMask) | (vaddr & ((uint64(1) << shift) - 1))
			return c.leaf(pa, desc, descAddr, level, vaddr, write)
		}
		return 0, false, c.fault("translation", level, vaddr, write)
	}
}

// leaf finalises a leaf descriptor: it returns the physical address and the
// writable flag (AP[2]==0). The Access Flag is hardware-managed — if AF is clear
// we set it in the in-memory descriptor and proceed (what a FEAT_HAFDBS CPU
// does), rather than faulting. edk2 relies on this; Linux pre-sets AF so its
// behaviour is unchanged. AP[1]/bit 6 (EL0 access) and UXN/PXN are decoded once
// EL0 execution is modelled.
func (c *CPU) leaf(pa, desc, descAddr uint64, level int, vaddr uint64, write bool) (uint64, bool, *abort) {
	if desc&(1<<10) == 0 { // AF clear: set it (hardware access-flag management)
		_ = c.Mem.Write64(descAddr, desc|(1<<10))
	}
	return pa, desc&(1<<7) == 0, nil
}

// atS1E1 implements the AT S1E1R/S1E1W/S1E0R/S1E0W instructions: it runs the
// stage-1 translation for a VA and returns the PAR_EL1 result instead of taking
// an exception. On success bit0 (F) is 0 and the output PA sits in bits[47:12];
// on a fault bit0 is 1 and bits[6:1] hold the fault status code. FreeBSD's
// pmap_bootstrap uses this to discover the physical address of a freshly mapped
// page-table page when building the direct map — without it PAR reads 0 and the
// kernel installs a DMAP entry pointing at physical address 0.
func (c *CPU) atS1E1(va uint64, write bool) uint64 {
	access := accessRead
	if write {
		access = accessWrite
	}
	pa, ab := c.translate(va, access)
	if ab != nil {
		return 1 | dfscForAbort(ab)<<1 // F=1, FST = fault status code
	}
	return pa & descAddrMask // F=0, PA[47:12]
}

// fault records FAR/FaultKind and returns the abort.
func (c *CPU) fault(kind string, level int, vaddr uint64, write bool) *abort {
	c.FAR = vaddr
	c.FaultKind = kind
	return &abort{kind: kind, level: level, far: vaddr, write: write}
}

// fetch reads the instruction word at PC (a 4-byte aligned access never crosses
// a 4 KiB page).
func (c *CPU) fetch() (uint32, error) {
	pa, ab := c.translate(c.PC, accessExec)
	if ab != nil {
		return 0, ab
	}
	return c.Mem.Read32(pa)
}

// readMem reads size bytes at the virtual address, translating each page-
// segment separately so an unaligned access spanning a page boundary reads the
// right physical pages.
func (c *CPU) readMem(vaddr uint64, size int) (uint64, error) {
	var out uint64
	for got := 0; got < size; {
		pa, ab := c.translate(vaddr+uint64(got), accessRead)
		if ab != nil {
			return 0, ab
		}
		// The chunk must fit before the page boundary AND be a size the memory
		// layer accepts (1/2/4/8) — an unaligned page-crossing access otherwise
		// produces a 3/5/6/7-byte chunk.
		n := validChunk(pageRemaining(vaddr+uint64(got), size-got))
		v, err := c.Mem.Read(pa, n)
		if err != nil {
			return 0, err
		}
		out |= v << (8 * got)
		got += n
	}
	return out, nil
}

// writeMem writes the low size bytes of val at the virtual address, per
// page-segment.
func (c *CPU) writeMem(vaddr, val uint64, size int) error {
	for done := 0; done < size; {
		pa, ab := c.translate(vaddr+uint64(done), accessWrite)
		if ab != nil {
			return ab
		}
		n := validChunk(pageRemaining(vaddr+uint64(done), size-done))
		if watchPA != 0 && pa>>3 == watchPA>>3 {
			dbgf("[watchpa] pa=%#x va=%#x val=%#x size=%d pc=%#x\n", pa, vaddr, val>>(8*done), n, c.PC)
		}
		if err := c.Mem.Write(pa, val>>(8*done), n); err != nil {
			return err
		}
		done += n
	}
	return nil
}

// validChunk reduces a byte count to the largest memory-access size (1/2/4/8)
// not exceeding it, so each piece of a split access is individually valid.
func validChunk(n int) int {
	switch {
	case n >= 8:
		return 8
	case n >= 4:
		return 4
	case n >= 2:
		return 2
	default:
		return 1
	}
}

// pageRemaining returns how many of the wanted bytes fit before the next 4 KiB
// page boundary (at least 1, at most wanted).
func pageRemaining(vaddr uint64, wanted int) int {
	toEnd := int(0x1000 - (vaddr & 0xFFF))
	if toEnd < wanted {
		return toEnd
	}
	return wanted
}
