package arm64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestSIMDExtractNarrow covers the extract-narrow family (XTN/XTN2, SQXTN,
// UQXTN) that FreeBSD's libc uses — `xtn v.2s, v.2d` (opcode 0x0ea12821) was the
// gap that stopped the FreeBSD arm64 disk-image boot. Raw opcodes (the assembler
// doesn't encode these vector forms yet); values hand-computed.
func TestSIMDExtractNarrow(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	c := New(mm)

	cases := []struct {
		name string
		w    uint32
		vn   [2]uint64
		rdIn [2]uint64
		want [2]uint64
	}{
		// xtn v0.2s, v1.2d — truncate each 64-bit element to its low 32 bits
		{"xtn.2s", 0x0ea12820,
			[2]uint64{0x1122334455667788, 0xAABBCCDDEEFF0011}, [2]uint64{0xdead, 0xbeef},
			[2]uint64{0xEEFF001155667788, 0}},
		// sqxtn v0.2s, v1.2d — signed saturate to int32 (0x80000000 -> 0x7FFFFFFF)
		{"sqxtn.2s", 0x0ea14820,
			[2]uint64{0x000000007FFFFFFF, 0x0000000080000000}, [2]uint64{0, 0},
			[2]uint64{0x7FFFFFFF7FFFFFFF, 0}},
		// uqxtn v0.2s, v1.2d — unsigned saturate to uint32 (0x1_00000000 -> 0xFFFFFFFF)
		{"uqxtn.2s", 0x2ea14820,
			[2]uint64{0x00000000FFFFFFFF, 0x0000000100000000}, [2]uint64{0, 0},
			[2]uint64{0xFFFFFFFFFFFFFFFF, 0}},
		// xtn2 v0.4s, v1.2d — fill the upper 64 bits, keep the lower
		{"xtn2.4s", 0x4ea12820,
			[2]uint64{0x1122334455667788, 0xAABBCCDDEEFF0011}, [2]uint64{0x1111111122222222, 0},
			[2]uint64{0x1111111122222222, 0xEEFF001155667788}},
	}
	for _, tc := range cases {
		c.Vreg[1] = tc.vn
		c.Vreg[0] = tc.rdIn
		if err := c.execSIMD2RegMisc(tc.w); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if c.Vreg[0] != tc.want {
			t.Errorf("%s: got %#016x, want %#016x", tc.name, c.Vreg[0], tc.want)
		}
	}
}
