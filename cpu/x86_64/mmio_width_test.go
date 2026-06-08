package x86_64

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestMMIO32AtomicAccess guards the property that a single 32-bit guest
// load/store to an MMIO device reaches the device as ONE 32-bit access,
// not four byte accesses.
//
// This is the OVMF local-APIC bug in miniature. xAPIC register files
// return the whole 32-bit register for the register's aligned offset and
// zero for the in-between byte offsets. OVMF writes SVR with a single
// MmioWrite32 of 0x0000010F (vector 0xF + the software-enable bit 8).
// When writeMem32 decomposed that store into four Write8 calls the device
// saw off=0/val=0xF then off=1/val=0x1 (dropped — no byte-offset case)
// etc., so the stored register was 0x0F and the software-enable bit was
// lost; CpuMpPei then asserted (ReadLocalApicReg(0x0F0) & 0x100) != 0.
func TestMMIO32AtomicAccess(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)

	const base = 0x10000
	var reg uint32

	type access struct {
		off uint32
		val uint32
		sz  int
	}
	var reads, writes []access

	readFn := func(_ any, off uint32, sz int) uint32 {
		reads = append(reads, access{off, 0, sz})
		if off == 0 { // whole register is only visible at its aligned offset
			return reg
		}
		return 0
	}
	writeFn := func(_ any, off uint32, val uint32, sz int) {
		writes = append(writes, access{off, val, sz})
		if off == 0 {
			reg = val
		}
	}

	flags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	if _, err := mm.RegisterDevice(base, 0x1000, nil, readFn, writeFn, flags); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	// Fresh CPU is in real mode with paging off, so guest-linear == phys.
	c := NewCPU(mm)

	// 32-bit store of the SVR pattern.
	c.writeMem32(base, 0x0000010F)
	if len(writes) != 1 {
		t.Fatalf("32-bit MMIO store decomposed into %d device accesses, want 1: %+v", len(writes), writes)
	}
	if w := writes[0]; w.off != 0 || w.val != 0x0000010F || w.sz != 2 {
		t.Fatalf("device write = %+v, want {off:0 val:0x10F sz:2}", w)
	}
	if reg != 0x0000010F {
		t.Fatalf("device register = %#x, want 0x10F (byte-decomposed store dropped the upper bytes)", reg)
	}

	// 32-bit load must round-trip the full dword in a single access.
	got := c.readMem32(base)
	if len(reads) != 1 {
		t.Fatalf("32-bit MMIO load decomposed into %d device accesses, want 1: %+v", len(reads), reads)
	}
	if got != 0x0000010F {
		t.Fatalf("load-back = %#x, want 0x10F (byte-decomposed load drops bit 8 = software-enable)", got)
	}
}

// TestMMIO16AtomicAccess: the same one-access property for 16-bit MMIO.
func TestMMIO16AtomicAccess(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)

	const base = 0x20000
	var got16 uint32
	var nWrites int

	writeFn := func(_ any, off uint32, val uint32, sz int) {
		nWrites++
		got16 = val
		_ = off
		if sz != 1 {
			t.Errorf("16-bit MMIO write sizeLog2 = %d, want 1", sz)
		}
	}
	flags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	if _, err := mm.RegisterDevice(base, 0x1000, nil,
		func(any, uint32, int) uint32 { return 0 }, writeFn, flags); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	c := NewCPU(mm)
	c.writeMem16(base, 0xBEEF)
	if nWrites != 1 {
		t.Fatalf("16-bit MMIO store hit the device %d times, want 1", nWrites)
	}
	if got16 != 0xBEEF {
		t.Fatalf("device got %#x, want 0xBEEF", got16)
	}
}

// TestMMIO32CrossPageFallback: a 32-bit access whose bytes straddle two
// pages must still go byte-at-a-time so each byte translates on its own
// page (the 2026-05-15 unaligned cross-page fix is preserved by the fast
// path's same-page guard). Here paging is off and the device spans the
// straddle, so the device sees four byte accesses and the value is still
// assembled correctly.
func TestMMIO32CrossPageFallback(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)

	// Device covering the page boundary at 0x2000.
	const base = 0x1000
	store := map[uint32]uint8{}
	writeFn := func(_ any, off uint32, val uint32, sz int) {
		if sz != 0 {
			t.Errorf("cross-page store byte sizeLog2 = %d, want 0", sz)
		}
		store[off] = uint8(val)
	}
	readFn := func(_ any, off uint32, sz int) uint32 { return uint32(store[off]) }
	flags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	if _, err := mm.RegisterDevice(base, 0x2000, nil, readFn, writeFn, flags); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	c := NewCPU(mm)
	// 0x1FFE..0x2001 straddles the 0x2000 page boundary.
	c.writeMem32(0x1FFE, 0x11223344)
	if len(store) != 4 {
		t.Fatalf("cross-page store wrote %d bytes, want 4 (byte fallback)", len(store))
	}
	if got := c.readMem32(0x1FFE); got != 0x11223344 {
		t.Fatalf("cross-page round-trip = %#x, want 0x11223344", got)
	}
}
