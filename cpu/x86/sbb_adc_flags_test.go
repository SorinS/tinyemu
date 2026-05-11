package x86

import "testing"

// The previous sbb/adc implementations went through updateArithFlags{8,16,32}
// passing `b + cf_in` as the second operand. When b == max and cf_in == 1 the
// sum truncated to 0 inside the helper's CF/OF/AF computation, producing the
// wrong flags. The Linux kernel's __e820__range_update uses a 64-bit comparison
// implemented as `cmp lo, lo; sbb hi, hi; jae/jb`. When start_hi == 0 and the
// inverted-saturation trick feeds 0xFFFFFFFF + cf_in into SBB, the wrong CF
// caused range_update to think kernel-text addresses overlapped a 0..0x1000
// window — which converted *all* RAM e820 entries to RESERVED and triggered the
// kernel to clear its own page tables.

// TestSBB32_OverflowingBPlusCF: SBB(0, 0xFFFFFFFF, CF=1) must set CF=1.
//
//	   0
//	-  0xFFFFFFFF
//	-  1   (carry in)
//	= -0x100000000  → result 0, borrow out
//
// The bug: `b + cf = 0xFFFFFFFF + 1 = 0 (mod 2^32)` made the old helper think
// the operation was `0 - 0` and report CF=0.
func TestSBB32_OverflowingBPlusCF(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(true) // CF_in = 1
	r := c.sbb32(0, 0xFFFFFFFF)
	if r != 0 {
		t.Errorf("SBB32(0, 0xFFFFFFFF, CF=1) result = 0x%X, want 0", r)
	}
	if !c.getCF() {
		t.Errorf("SBB32(0, 0xFFFFFFFF, CF=1) CF_out = 0, want 1 (borrow needed)")
	}
}

// TestSBB16_OverflowingBPlusCF: 16-bit variant of the same bug.
func TestSBB16_OverflowingBPlusCF(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(true)
	r := c.sbb16(0, 0xFFFF)
	if r != 0 {
		t.Errorf("SBB16(0, 0xFFFF, CF=1) result = 0x%X, want 0", r)
	}
	if !c.getCF() {
		t.Errorf("SBB16(0, 0xFFFF, CF=1) CF_out = 0, want 1")
	}
}

// TestSBB8_OverflowingBPlusCF: 8-bit variant.
func TestSBB8_OverflowingBPlusCF(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(true)
	r := c.sbb8(0, 0xFF)
	if r != 0 {
		t.Errorf("SBB8(0, 0xFF, CF=1) result = 0x%X, want 0", r)
	}
	if !c.getCF() {
		t.Errorf("SBB8(0, 0xFF, CF=1) CF_out = 0, want 1")
	}
}

// TestADC32_OverflowingBPlusCF: ADC(0xFFFFFFFE, 0xFFFFFFFF, CF=1) — the carry-
// chain analog. Sum = 0x1FFFFFFFE, low 32 bits = 0xFFFFFFFE, CF_out = 1.
func TestADC32_OverflowingBPlusCF(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(true)
	r := c.adc32(0xFFFFFFFE, 0xFFFFFFFF)
	if r != 0xFFFFFFFE {
		t.Errorf("ADC32(0xFFFFFFFE, 0xFFFFFFFF, CF=1) result = 0x%X, want 0xFFFFFFFE", r)
	}
	if !c.getCF() {
		t.Errorf("ADC32(0xFFFFFFFE, 0xFFFFFFFF, CF=1) CF_out = 0, want 1")
	}
}

// TestSBB32_NoBPlusCFOverflow: regression test for the "normal" case.
// SBB(0x100, 0x50, CF=1) = 0x100 - 0x50 - 1 = 0xAF, no borrow.
func TestSBB32_NoBPlusCFOverflow(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(true)
	r := c.sbb32(0x100, 0x50)
	if r != 0xAF {
		t.Errorf("SBB32(0x100, 0x50, CF=1) result = 0x%X, want 0xAF", r)
	}
	if c.getCF() {
		t.Errorf("SBB32(0x100, 0x50, CF=1) CF_out = 1, want 0")
	}
}

// TestSBB32_WithoutCarryIn: SBB with CF_in=0 should behave like SUB.
func TestSBB32_WithoutCarryIn(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(false)
	r := c.sbb32(0, 0xFFFFFFFF)
	if r != 1 {
		t.Errorf("SBB32(0, 0xFFFFFFFF, CF=0) result = 0x%X, want 1", r)
	}
	if !c.getCF() {
		t.Errorf("SBB32(0, 0xFFFFFFFF, CF=0) CF_out = 0, want 1")
	}
}

// TestADC32_WithoutCarryIn: ADC with CF_in=0 behaves like ADD.
func TestADC32_WithoutCarryIn(t *testing.T) {
	c := newTestCPU(t)
	c.setCF(false)
	r := c.adc32(0xFFFFFFFF, 1)
	if r != 0 {
		t.Errorf("ADC32(0xFFFFFFFF, 1, CF=0) result = 0x%X, want 0", r)
	}
	if !c.getCF() {
		t.Errorf("ADC32(0xFFFFFFFF, 1, CF=0) CF_out = 0, want 1")
	}
}

// TestSBB32_E820RangeUpdatePattern: replays the exact sequence the Linux
// kernel's `__e820__range_update` uses for its overlap check. Without the
// fix, this 64-bit-via-32-bit comparison silently mis-decides whether an
// e820 entry overlaps the query range, and Linux ends up reserving all of
// RAM as `E820_TYPE_RESERVED`, leading to a triple-fault on boot.
//
// The pattern is the "saturating end" math at the top of the function:
//
//	%eax = ~start_lo;  %edx = ~start_hi   ; bitwise NOT
//	cmp  %eax, %esi    ; ESI = size_lo
//	sbb  %edx, %edi    ; EDI = size_hi
//	cmovae %edx, %ecx  ; saturate if (size_hi | ext) > ~start
//	cmovae %eax, %esi
//
// For start=0 (so ~start = 0xFFFFFFFFFFFFFFFF) and size=0x1000:
//
//	cmp  0xFFFFFFFF, 0x1000      → CF=1
//	sbb  0xFFFFFFFF, 0           → 0 - 0xFFFFFFFF - 1 = -0x100000000, CF=1
//
// CF=1 means cmovae must NOT fire, so ESI/ECX keep the non-saturated values.
// With the bug, CF=0 was reported and cmovae fired, replacing the size
// with 0xFFFFFFFF, which made the subsequent overlap check claim every
// entry overlapped 0..end-of-memory.
func TestSBB32_E820RangeUpdatePattern(t *testing.T) {
	c := newTestCPU(t)

	// cmp 0xFFFFFFFF, 0x1000  — emitted as `sub` on a scratch register so
	// we only set flags. Use the low-half compare via sub32 (compare = sub
	// without storing result).
	_ = c.sub32(0x1000, 0xFFFFFFFF)
	if !c.getCF() {
		t.Fatalf("low-half cmp 0x1000 - 0xFFFFFFFF should set CF=1, got 0 (flags=0x%X)", c.eflags)
	}

	// Now sbb the high halves: EDI=0, EDX=0xFFFFFFFF, CF=1.
	res := c.sbb32(0, 0xFFFFFFFF)
	if res != 0 {
		t.Errorf("sbb32 result = 0x%X, want 0", res)
	}
	if !c.getCF() {
		t.Errorf("sbb32 CF_out = 0, want 1 (this is the bug that caused the e820 corruption)")
	}
}
