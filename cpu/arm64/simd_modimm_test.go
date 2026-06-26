package arm64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestSIMDModImmLogical covers the modified-immediate ORR/BIC forms (cmode bit0
// set): they read-modify-write Vd, unlike MOVI/MVNI. `orr v.2s,#imm,lsl#8`
// (0x0f043500) was a gap hit by FreeBSD's sh.
func TestSIMDModImmLogical(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	c := New(mm)

	modimm := func(q, op, cmode, imm8, rd uint32) uint32 {
		return q<<30 | op<<29 | 0b0111100000<<19 | (imm8>>5&7)<<16 | cmode<<12 | 1<<10 | (imm8&0x1F)<<5 | rd
	}

	// orr v0.2s, #0x88, lsl #8 : OR 0x00008800 into each 32-bit lane, upper zeroed
	c.Vreg[0] = [2]uint64{0x1111111122222222, 0xdead}
	if w := modimm(0, 0, 0x3, 0x88, 0); w != 0x0f043500 {
		t.Fatalf("encoding mismatch: %#x", w)
	}
	if err := c.execSIMDModImm(0x0f043500); err != nil {
		t.Fatal(err)
	}
	if got := c.Vreg[0]; got != ([2]uint64{0x111199112222AA22, 0}) {
		t.Errorf("orr.2s: got %#016x", got)
	}

	// bic v0.4s, #0xFF : AND ~0x000000FF into each of 4 lanes (full 128)
	c.Vreg[0] = [2]uint64{0xAABBCCDDEEFF1122, 0x3344556677889900}
	if err := c.execSIMDModImm(modimm(1, 1, 0x1, 0xFF, 0)); err != nil {
		t.Fatal(err)
	}
	if got := c.Vreg[0]; got != ([2]uint64{0xAABBCC00EEFF1100, 0x3344550077889900}) {
		t.Errorf("bic.4s: got %#016x", got)
	}
}
