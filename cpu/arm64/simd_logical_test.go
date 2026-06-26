package arm64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestSIMDLogical covers the three-same bitwise ops (opcode 0x03): U=0 →
// and/bic/orr/orn, U=1 → eor/bsl/bit/bif. BSL/BIT/BIF are insert-selects that
// read the original Vd — a regression here (treating every U=1 op as EOR) made
// FreeBSD's libc memchr/strlen `bif` corrupt its match mask and crash sh.
// Expected values are hand-computed for Vn/Vm/Vd below.
func TestSIMDLogical(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	c := New(mm)

	// three-same encoding: OP v0.<T>, v1.<T>, v2.<T> (Rd=0, Rn=1, Rm=2)
	word := func(q, u, size uint32) uint32 {
		return q<<30 | u<<29 | 0x0E<<24 | size<<22 | 1<<21 | 2<<16 | 0x03<<11 | 1<<10 | 1<<5 | 0
	}
	const vn = uint64(0xFF00FF00FF00FF00) // Vn
	const vm = uint64(0xFFFF0000FFFF0000) // Vm
	const vd = uint64(0xF0F0F0F0F0F0F0F0) // original Vd (used by bsl/bit/bif)

	cases := []struct {
		name    string
		u, size uint32
		want    uint64
	}{
		{"and", 0, 0b00, 0xFF000000FF000000},
		{"bic", 0, 0b01, 0x0000FF000000FF00},
		{"orr", 0, 0b10, 0xFFFFFF00FFFFFF00},
		{"orn", 0, 0b11, 0xFF00FFFFFF00FFFF},
		{"eor", 1, 0b00, 0x00FFFF0000FFFF00},
		{"bsl", 1, 0b01, 0xFF0FF000FF0FF000}, // (Vn&Vd)|(Vm&~Vd)
		{"bit", 1, 0b10, 0xFF00F0F0FF00F0F0}, // (Vn&Vm)|(Vd&~Vm)
		{"bif", 1, 0b11, 0xF0F0FF00F0F0FF00}, // (Vn&~Vm)|(Vd&Vm)
	}
	for _, tc := range cases {
		// Q=1 (.16b): full 128-bit, both halves identical.
		c.Vreg[1] = [2]uint64{vn, vn}
		c.Vreg[2] = [2]uint64{vm, vm}
		c.Vreg[0] = [2]uint64{vd, vd}
		if err := c.execSIMD(word(1, tc.u, tc.size)); err != nil {
			t.Fatalf("%s.16b: %v", tc.name, err)
		}
		if got := c.Vreg[0]; got != ([2]uint64{tc.want, tc.want}) {
			t.Errorf("%s.16b: got %#016x, want %#016x", tc.name, got, tc.want)
		}

		// Q=0 (.8b): low half computed, upper half zeroed.
		c.Vreg[1] = [2]uint64{vn, vn}
		c.Vreg[2] = [2]uint64{vm, vm}
		c.Vreg[0] = [2]uint64{vd, vd}
		if err := c.execSIMD(word(0, tc.u, tc.size)); err != nil {
			t.Fatalf("%s.8b: %v", tc.name, err)
		}
		if got := c.Vreg[0]; got != ([2]uint64{tc.want, 0}) {
			t.Errorf("%s.8b: got %#016x, want {%#016x,0}", tc.name, got, tc.want)
		}
	}
}
