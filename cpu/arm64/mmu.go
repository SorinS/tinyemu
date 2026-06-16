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

// translate resolves a virtual to a physical address for the given access. It
// records FAR/FaultKind on a fault.
func (c *CPU) translate(vaddr uint64, access accessType) (uint64, *abort) {
	if !c.mmuEnabled() {
		return vaddr, nil
	}
	write := access == accessWrite
	t0sz := c.TCR & 0x3F
	level := startLevel(t0sz)
	table := c.TTBR0 & descAddrMask

	for {
		shift := uint(12 + 9*(3-level))
		idx := (vaddr >> shift) & 0x1FF
		descAddr := table + idx*8
		desc, err := c.Mem.Read64(descAddr)
		if err != nil || desc&1 == 0 { // unreadable table or invalid descriptor
			return c.fault("translation", level, vaddr, write)
		}
		if desc&2 != 0 { // bit1 set
			if level == 3 { // 0b11 at L3 = page (leaf)
				return c.finishLeaf(desc&descAddrMask|(vaddr&0xFFF), desc, level, vaddr, write)
			}
			// 0b11 at L0–L2 = table descriptor; descend.
			table = desc & descAddrMask
			level++
			continue
		}
		// bit1 clear → 0b01 = block descriptor. Valid only at L1/L2 (4 KiB):
		// L0 has no block form, and 0b01 at L3 is reserved/invalid.
		if level == 1 || level == 2 {
			blockMask := descAddrMask &^ ((uint64(1) << shift) - 1)
			pa := (desc & blockMask) | (vaddr & ((uint64(1) << shift) - 1))
			return c.finishLeaf(pa, desc, level, vaddr, write)
		}
		return c.fault("translation", level, vaddr, write)
	}
}

// finishLeaf applies the access-flag and permission checks of a leaf descriptor.
func (c *CPU) finishLeaf(pa, desc uint64, level int, vaddr uint64, write bool) (uint64, *abort) {
	if desc&(1<<10) == 0 { // AF: access flag not set
		return c.fault("accessflag", level, vaddr, write)
	}
	// AP[2] (bit 7): read-only when set. (AP[1]/bit 6 = EL0 access, and
	// UXN/PXN, are decoded once EL0 execution is modelled.)
	if write && desc&(1<<7) != 0 {
		return c.fault("permission", level, vaddr, write)
	}
	return pa, nil
}

func (c *CPU) fault(kind string, level int, vaddr uint64, write bool) (uint64, *abort) {
	c.FAR = vaddr
	c.FaultKind = kind
	return 0, &abort{kind: kind, level: level, far: vaddr, write: write}
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
		n := pageRemaining(vaddr+uint64(got), size-got)
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
		n := pageRemaining(vaddr+uint64(done), size-done)
		if err := c.Mem.Write(pa, val>>(8*done), n); err != nil {
			return err
		}
		done += n
	}
	return nil
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
