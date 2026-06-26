package arm64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestSIMDFPAbsNeg covers the vector FP fabs/fneg (two-reg-misc opcode 0x0F):
// fneg flips each element's sign bit, fabs clears it. `fneg v.2d` (0x6ee0f800)
// was the gap that stopped FreeBSD's login after the BSL/BIT/BIF fix.
func TestSIMDFPAbsNeg(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	c := New(mm)

	const (
		p1d = 0x3FF0000000000000 // +1.0
		m1d = 0xBFF0000000000000 // -1.0
		p2d = 0x4000000000000000 // +2.0
		m2d = 0xC000000000000000 // -2.0
		p1s = 0x3F800000         // +1.0f
		m1s = 0xBF800000         // -1.0f
		p2s = 0x40000000         // +2.0f
		m2s = 0xC0000000         // -2.0f
	)

	// fneg v0.2d, v1.2d : {+1.0,-2.0} -> {-1.0,+2.0}
	c.Vreg[1] = [2]uint64{p1d, m2d}
	if err := c.execSIMD(0x6EE0F820); err != nil {
		t.Fatal(err)
	}
	if got := c.Vreg[0]; got != ([2]uint64{m1d, p2d}) {
		t.Errorf("fneg.2d: got %#016x", got)
	}

	// fabs v0.2d, v1.2d : {-1.0,-2.0} -> {+1.0,+2.0}
	c.Vreg[1] = [2]uint64{m1d, m2d}
	if err := c.execSIMD(0x4EE0F820); err != nil {
		t.Fatal(err)
	}
	if got := c.Vreg[0]; got != ([2]uint64{p1d, p2d}) {
		t.Errorf("fabs.2d: got %#016x", got)
	}

	// fneg v0.4s, v1.4s : each 32-bit lane's sign flipped
	c.Vreg[1] = [2]uint64{m2s<<32 | p1s, p1s<<32 | m2s}
	if err := c.execSIMD(0x6EA0F820); err != nil {
		t.Fatal(err)
	}
	want := [2]uint64{p2s<<32 | m1s, m1s<<32 | p2s}
	if got := c.Vreg[0]; got != want {
		t.Errorf("fneg.4s: got %#016x, want %#016x", got, want)
	}
}
