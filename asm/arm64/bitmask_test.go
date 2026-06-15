package arm64

import (
	"math/bits"
	"testing"
)

// TestBitmask_EncodeDecodeInverse sweeps the entire 13-bit (N:imms:immr) field
// space: every combination that decodes to a valid logical immediate must
// encode back to exactly those fields. This proves encode∘decode = identity
// over all encodable values without an external oracle (the sample-vs-llvm test
// anchors the convention to the real ISA).
func TestBitmask_EncodeDecodeInverse(t *testing.T) {
	for _, regSize := range []int{32, 64} {
		seen := 0
		for n := uint32(0); n <= 1; n++ {
			if n == 1 && regSize == 32 {
				continue // N=1 ⇒ esize 64, invalid for 32-bit registers
			}
			for imms := uint32(0); imms < 64; imms++ {
				// Element size is fixed by (N:imms); immr's bits at or above it
				// are ignored by hardware, so only its low bits are canonical.
				combined := (n << 6) | ((^imms) & 0x3F)
				if combined == 0 {
					continue
				}
				esize := 1 << uint(31-bits.LeadingZeros32(combined))
				for immr := uint32(0); immr < uint32(esize); immr++ {
					val, ok := decodeBitmask(n, imms, immr, regSize)
					if !ok {
						continue
					}
					gn, gimmr, gimms, ok := encodeBitmask(val, regSize)
					if !ok {
						t.Fatalf("regSize=%d N=%d imms=%#x immr=%#x decoded %#x but failed to encode",
							regSize, n, imms, immr, val)
					}
					if gn != n || gimmr != immr || gimms != imms {
						t.Fatalf("regSize=%d value %#x: encode gave N=%d immr=%#x imms=%#x, want N=%d immr=%#x imms=%#x",
							regSize, val, gn, gimmr, gimms, n, immr, imms)
					}
					seen++
				}
			}
		}
		// Sanity: there really are thousands of distinct logical immediates.
		if seen < 1000 {
			t.Errorf("regSize=%d: only %d encodable fields swept — expected thousands", regSize, seen)
		}
	}
}

// TestBitmask_Rejects checks that non-encodable values are refused (not encoded
// to garbage): 0, all-ones, and a non-contiguous pattern.
func TestBitmask_Rejects(t *testing.T) {
	bad := []uint64{0, ^uint64(0), 0x0000000500000005, 0x0102010201020102}
	for _, v := range bad {
		if _, _, _, ok := encodeBitmask(v, 64); ok {
			t.Errorf("encodeBitmask(%#x, 64) accepted; want rejected", v)
		}
	}
	// 32-bit: a value with bits above bit 31 is not a 32-bit immediate.
	if _, _, _, ok := encodeBitmask(0x1_0000_0001, 32); ok {
		t.Errorf("encodeBitmask(0x100000001, 32) accepted; want rejected")
	}
}
