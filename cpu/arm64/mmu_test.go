package arm64

import (
	"encoding/binary"
	"testing"

	asmarm64 "github.com/sorins/tinyemu-go/asm/arm64"
	"github.com/sorins/tinyemu-go/mem"
)

// MMU test fixture: a 4 KiB-granule, 48-bit (T0SZ=16, start L0) translation that
// maps one virtual page through a full L0→L1→L2→L3 walk. The four table
// descriptors are laid out at fixed physical pages; each test writes the L3
// leaf descriptor as an explicit hex word (ARM-ARM bit positions) so the
// attribute/fault checks can't pass by sharing a bug with an encode helper.
const (
	mmuL0    = 0x1000
	mmuL1    = 0x2000
	mmuL2    = 0x3000
	mmuL3    = 0x4000
	mmuPA    = 0x10000 // the mapped physical page
	mmuVA    = 0x8080604000
	mmuL0Idx = 1 // (VA>>39)&0x1FF
	mmuL1Idx = 2 // (VA>>30)&0x1FF
	mmuL2Idx = 3 // (VA>>21)&0x1FF
	mmuL3Idx = 4 // (VA>>12)&0x1FF
)

// newMMUCPU builds a CPU with the table skeleton in place (L0→L1→L2→L3) and the
// MMU enabled for 48-bit TTBR0 translation. The caller writes the L3 leaf.
func newMMUCPU(t *testing.T) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	// Table descriptors (bits[1:0]=0b11=table; next-table base in [47:12]).
	_ = mm.Write64(mmuL0+mmuL0Idx*8, mmuL1|0b11)
	_ = mm.Write64(mmuL1+mmuL1Idx*8, mmuL2|0b11)
	_ = mm.Write64(mmuL2+mmuL2Idx*8, mmuL3|0b11)
	c := New(mm)
	c.TTBR0 = mmuL0
	c.TCR = 16 // T0SZ=16 → 48-bit VA, walk starts at L0
	c.SCTLR = 1 // M: MMU on
	return c
}

// TestMMU_RoundTrip is the ground-truth address-path check: map VA→PA, then
// confirm a value written to PA *physically* reads back through VA (translated),
// and vice versa — independent of whether the descriptor mental model is right.
func TestMMU_RoundTrip(t *testing.T) {
	c := newMMUCPU(t)
	// L3 page descriptor: output PA, AF set (bit10), type 0b11.
	_ = c.Mem.Write64(mmuL3+mmuL3Idx*8, mmuPA|(1<<10)|0b11)

	if pa, ab := c.translate(mmuVA, accessRead); ab != nil || pa != mmuPA {
		t.Fatalf("translate(va) = %#x, %v; want %#x", pa, ab, uint64(mmuPA))
	}

	// PA written physically → read back through the VA.
	const sentinel = 0xDEADBEEFCAFEBABE
	_ = c.Mem.Write64(mmuPA, sentinel)
	if v, err := c.readMem(mmuVA, 8); err != nil || v != sentinel {
		t.Errorf("read through VA = %#x, %v; want %#x", v, err, uint64(sentinel))
	}
	// VA written (translated) → read back from PA physically.
	const other = 0x0123456789ABCDEF
	if err := c.writeMem(mmuVA+0x40, other, 8); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Mem.Read64(mmuPA + 0x40); v != other {
		t.Errorf("PA after VA write = %#x; want %#x", v, uint64(other))
	}
}

// TestMMU_CrossPage maps two adjacent virtual pages to two NON-adjacent physical
// pages, then does an 8-byte access straddling the boundary — it must read/write
// the correct physical page for each half (the bug the per-segment walk fixes).
func TestMMU_CrossPage(t *testing.T) {
	c := newMMUCPU(t)
	// L3[4] → mmuPA, L3[5] → a deliberately far physical page.
	const pa2 = 0x80000
	_ = c.Mem.Write64(mmuL3+mmuL3Idx*8, mmuPA|(1<<10)|0b11)
	_ = c.Mem.Write64(mmuL3+(mmuL3Idx+1)*8, pa2|(1<<10)|0b11)

	// Access at VA page-end - 4, size 8: 4 bytes in page A, 4 in page B.
	va := uint64(mmuVA + 0x1000 - 4)
	if err := c.writeMem(va, 0x1122334455667788, 8); err != nil {
		t.Fatal(err)
	}
	// Low 4 bytes land at the end of mmuPA, high 4 at the start of pa2.
	if v, _ := c.Mem.Read32(mmuPA + 0x1000 - 4); v != 0x55667788 {
		t.Errorf("page A tail = %#x; want 55667788", v)
	}
	if v, _ := c.Mem.Read32(pa2); v != 0x11223344 {
		t.Errorf("page B head = %#x; want 11223344", v)
	}
}

// TestMMU_Faults checks each fault class with an explicit L3 leaf descriptor.
func TestMMU_Faults(t *testing.T) {
	leaf := func(desc uint64) *CPU {
		c := newMMUCPU(t)
		_ = c.Mem.Write64(mmuL3+mmuL3Idx*8, desc)
		return c
	}
	cases := []struct {
		name   string
		desc   uint64
		access accessType
		kind   string
	}{
		{"invalid (0b00)", mmuPA | (1 << 10), accessRead, "translation"},
		{"reserved 0b01 at L3", mmuPA | (1 << 10) | 0b01, accessRead, "translation"},
		{"access-flag (AF=0)", mmuPA | 0b11, accessRead, "accessflag"},
		{"permission (AP RO, store)", mmuPA | (1 << 10) | (1 << 7) | 0b11, accessWrite, "permission"},
	}
	for _, tc := range cases {
		c := leaf(tc.desc)
		_, ab := c.translate(mmuVA, tc.access)
		if ab == nil {
			t.Errorf("%s: expected a fault, got none", tc.name)
			continue
		}
		if ab.kind != tc.kind {
			t.Errorf("%s: fault kind %q, want %q", tc.name, ab.kind, tc.kind)
		}
		if c.FAR != mmuVA {
			t.Errorf("%s: FAR = %#x, want %#x", tc.name, c.FAR, uint64(mmuVA))
		}
	}
	// AP read-only permits reads.
	c := leaf(mmuPA | (1 << 10) | (1 << 7) | 0b11)
	if _, ab := c.translate(mmuVA, accessRead); ab != nil {
		t.Errorf("read of RO page faulted: %v", ab)
	}
}

// TestMMU_TTBR1 maps a high-half (kernel) VA via TTBR1 and round-trips it, and
// confirms a VA in the hole between the TTBR0 and TTBR1 regions faults.
func TestMMU_TTBR1(t *testing.T) {
	const (
		l0, l1, l2, l3 = 0x1000, 0x2000, 0x3000, 0x4000
		pa             = 0x10000
		kva            = 0xFFFF000040010000 // top 16 bits all 1 → TTBR1; low = 0x40010000
		sentinel       = 0x1122334455667788
	)
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	// Indices for low-48 bits 0x40010000: L0=0, L1=1, L2=0, L3=0x10.
	_ = mm.Write64(l0+0*8, l1|0b11)
	_ = mm.Write64(l1+1*8, l2|0b11)
	_ = mm.Write64(l2+0*8, l3|0b11)
	_ = mm.Write64(l3+0x10*8, pa|(1<<10)|0b11)
	c := New(mm)
	c.TTBR1 = l0
	c.TCR = 16 | (16 << 16) // T0SZ=16, T1SZ=16 (both halves 48-bit)
	c.SCTLR = 1

	if got, ab := c.translate(kva, accessRead); ab != nil || got != pa {
		t.Fatalf("translate(kva) = %#x, %v; want %#x", got, ab, uint64(pa))
	}
	_ = c.Mem.Write64(pa, sentinel)
	if v, err := c.readMem(kva, 8); err != nil || v != sentinel {
		t.Errorf("read through kernel VA = %#x, %v; want %#x", v, err, uint64(sentinel))
	}
	// A low VA with bits above the 48-bit region set lands in the hole → fault.
	if _, ab := c.translate(0x0001000000000000, accessRead); ab == nil || ab.kind != "address-size" {
		t.Errorf("VA-hole access: got %v, want address-size fault", ab)
	}
}

// TestMMU_TLB proves the translation is cached (a page-table edit isn't seen
// until a flush) and that both a TLBI instruction and a TTBR write flush it.
func TestMMU_TLB(t *testing.T) {
	exec := func(c *CPU, src string) {
		t.Helper()
		b, err := asmarm64.Assemble(src)
		if err != nil {
			t.Fatalf("assemble %q: %v", src, err)
		}
		next := c.PC
		if err := c.exec(binary.LittleEndian.Uint32(b), &next); err != nil {
			t.Fatalf("exec %q: %v", src, err)
		}
	}

	c := newMMUCPU(t)
	const pa2 = 0x20000
	_ = c.Mem.Write64(mmuL3+mmuL3Idx*8, mmuPA|(1<<10)|0b11)
	_ = c.Mem.Write64(mmuPA, 0xAAAA)
	if v, _ := c.readMem(mmuVA, 8); v != 0xAAAA { // fills the TLB
		t.Fatalf("initial read = %#x", v)
	}

	// Re-point the page to pa2 in memory, WITHOUT flushing: the cached entry
	// must still resolve to the old PA.
	_ = c.Mem.Write64(mmuL3+mmuL3Idx*8, pa2|(1<<10)|0b11)
	_ = c.Mem.Write64(pa2, 0xBBBB)
	if v, _ := c.readMem(mmuVA, 8); v != 0xAAAA {
		t.Errorf("expected stale TLB hit 0xAAAA, got %#x", v)
	}

	// A TLBI instruction flushes → the new mapping is seen.
	exec(c, "tlbi vmalle1")
	if v, _ := c.readMem(mmuVA, 8); v != 0xBBBB {
		t.Errorf("after tlbi expected 0xBBBB, got %#x", v)
	}

	// A TTBR write also flushes: re-point back to mmuPA, msr ttbr0, see old data.
	_ = c.Mem.Write64(mmuL3+mmuL3Idx*8, mmuPA|(1<<10)|0b11)
	c.X[0] = mmuL0
	exec(c, "msr ttbr0_el1, x0")
	if v, _ := c.readMem(mmuVA, 8); v != 0xAAAA {
		t.Errorf("after ttbr write expected 0xAAAA, got %#x", v)
	}
}

// TestMMU_StartLevel checks the T0SZ→start-level derivation.
func TestMMU_StartLevel(t *testing.T) {
	for _, tc := range []struct {
		t0sz uint64
		want int
	}{{16, 0}, {25, 1}, {34, 2}} {
		if got := startLevel(tc.t0sz); got != tc.want {
			t.Errorf("startLevel(T0SZ=%d) = %d, want %d", tc.t0sz, got, tc.want)
		}
	}
}

// TestMMU_EndToEnd is the wiring check: an assembled program turns the MMU on
// via msr (TCR/TTBR0/SCTLR) + isb, then loads through a virtual address that is
// NOT identity-mapped. It exercises the full path — writeSysreg populating the
// fields the walker reads, the M-bit enable taking effect, instruction fetch
// surviving (the code page is identity-mapped), and the translated load.
func TestMMU_EndToEnd(t *testing.T) {
	const (
		l0, l1, l2, l3 = 0x1000, 0x2000, 0x3000, 0x4000
		codeBase       = 0x100000
		dataPA         = 0x10000
		testVA         = 0x40010000 // L1 idx 1, L2 idx 0, L3 idx 0x10
		sentinel       = 0xCAFEBABEDEADBEEF
	)
	mm := mem.NewPhysMemoryMap()
	defer mm.Close()
	if _, err := mm.RegisterRAM(0, 1<<21, 0); err != nil {
		t.Fatal(err)
	}
	// L0[0] → L1. L1[0] = 1 GiB identity block (covers code + tables). L1[1] →
	// L2 → L3, with L3[0x10] mapping testVA → dataPA.
	_ = mm.Write64(l0+0*8, l1|0b11)
	_ = mm.Write64(l1+0*8, 0x0|(1<<10)|0b01)  // identity block [0,1GiB), AF
	_ = mm.Write64(l1+1*8, l2|0b11)           // VA[1GiB,2GiB) → L2
	_ = mm.Write64(l2+0*8, l3|0b11)           // VA[1GiB,1GiB+2MiB) → L3
	_ = mm.Write64(l3+0x10*8, dataPA|(1<<10)|0b11) // testVA → dataPA page, AF
	_ = mm.Write64(dataPA, sentinel)

	prog := []string{
		"movz x0, #16", "msr tcr_el1, x0", // T0SZ=16 (48-bit VA)
		"movz x1, #0x1000", "msr ttbr0_el1, x1", // table base
		"movz x2, #1", "msr sctlr_el1, x2", "isb", // enable MMU
		"movz x3, #0x4001, lsl #16", // x3 = 0x40010000 (testVA)
		"ldr x4, [x3]",
		"ret",
	}
	var code []byte
	for _, s := range prog {
		b, err := asmarm64.Assemble(s)
		if err != nil {
			t.Fatalf("assemble %q: %v", s, err)
		}
		code = append(code, b...)
	}
	for i, b := range code {
		_ = mm.Write8(codeBase+uint64(i), b)
	}
	c := New(mm)
	c.PC = codeBase
	c.X[30] = 0xDEAD0000 // ret sentinel
	end := uint64(codeBase) + uint64(len(code))
	for step := 0; c.PC >= codeBase && c.PC < end && step < 100; step++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step at %#x: %v", c.PC, err)
		}
	}
	if c.X[4] != sentinel {
		t.Errorf("x4 (loaded through MMU) = %#x, want %#x", c.X[4], uint64(sentinel))
	}
}

// TestMMU_DisabledIsIdentity confirms translation is the identity with M=0.
func TestMMU_DisabledIsIdentity(t *testing.T) {
	c := newMMUCPU(t)
	c.SCTLR = 0
	if pa, ab := c.translate(0xCAFE000, accessRead); ab != nil || pa != 0xCAFE000 {
		t.Errorf("MMU-off translate = %#x, %v; want identity", pa, ab)
	}
}
