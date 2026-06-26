package arm64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestSIMDLdStSingle covers the load/store single-structure group (0x0D):
// per-lane ST1/LD1 and the replicating LD1R. `st1 {v0.s}[1],[x21]` (0x0d0092a0)
// was a gap hit by FreeBSD's sh running commands. Raw opcodes (the assembler
// doesn't encode the single-lane forms yet).
func TestSIMDLdStSingle(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	c := New(mm)
	c.X[21] = 0x2000
	c.Vreg[0] = [2]uint64{0x1122334455667788, 0xAABBCCDDEEFF0011}

	// st1 {v0.s}[1], [x21] : store S-lane 1 (bits[63:32] of v0[0] = 0x11223344)
	if err := c.execSIMDLdStSingle(0x0d0092a0); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.readMem(0x2000, 4); got != 0x11223344 {
		t.Errorf("st1.s[1]: stored %#x, want 0x11223344", got)
	}

	// ld1 {v1.s}[0], [x21] : load it into v1 lane 0, keeping the rest
	c.Vreg[1] = [2]uint64{0x9999999999999999, 0x8888888888888888}
	if err := c.execSIMDLdStSingle(0x0d4082a1); err != nil {
		t.Fatal(err)
	}
	if got := c.Vreg[1]; got != ([2]uint64{0x9999999911223344, 0x8888888888888888}) {
		t.Errorf("ld1.s[0]: got %#016x", got)
	}

	// ld1r {v2.4s}, [x21] : load 0x11223344, replicate to all 4 S lanes
	if err := c.execSIMDLdStSingle(0x4d40caa2); err != nil {
		t.Fatal(err)
	}
	if got := c.Vreg[2]; got != ([2]uint64{0x1122334411223344, 0x1122334411223344}) {
		t.Errorf("ld1r.4s: got %#016x", got)
	}
}

// TestSIMDLdSt1RoundTrip stores vector registers to RAM with ST1 and loads them
// back with LD1, across register counts and both element widths — the half the
// native oracle can't exercise (it can't deref an arbitrary guest address). It
// also checks ST1's byte layout agrees with a plain `ldr q`, and that the
// post-index forms advance the base register.
func TestSIMDLdSt1RoundTrip(t *testing.T) {
	const base = 0x10000
	const buf = 0x40000

	// Four-register round-trip (Q=1, 128-bit): st1 {v0..v3}, then ld1 {v4..v7}.
	t.Run("4regQ1", func(t *testing.T) {
		prog := []string{
			"st1 {v0.16b, v1.16b, v2.16b, v3.16b}, [x0]",
			"ld1 {v4.16b, v5.16b, v6.16b, v7.16b}, [x0]",
		}
		c := newTestCPU(t, base, prog)
		c.X[0] = buf
		for i := uint32(0); i < 4; i++ {
			c.Vreg[i] = [2]uint64{0x1111111100000000 * uint64(i+1), 0xABCDEF00 ^ uint64(i)<<40}
		}
		runProg(t, c, base, len(prog))
		for i := uint32(0); i < 4; i++ {
			if c.Vreg[4+i] != c.Vreg[i] {
				t.Errorf("v%d=%#x, want v%d=%#x", 4+i, c.Vreg[4+i], i, c.Vreg[i])
			}
		}
	})

	// 64-bit form (Q=0) loads zero the upper half of the register.
	t.Run("Q0zerosHigh", func(t *testing.T) {
		prog := []string{"st1 {v0.8b}, [x0]", "ld1 {v1.8b}, [x0]"}
		c := newTestCPU(t, base, prog)
		c.X[0] = buf
		c.Vreg[0] = [2]uint64{0x0123456789ABCDEF, 0xDEADBEEFDEADBEEF}
		runProg(t, c, base, len(prog))
		if c.Vreg[1] != [2]uint64{0x0123456789ABCDEF, 0} {
			t.Errorf("ld1 .8b = %#x, want low half only", c.Vreg[1])
		}
	})

	// ST1 byte layout must match a plain `ldr q` of the same address.
	t.Run("matchesLdrQ", func(t *testing.T) {
		prog := []string{"st1 {v0.16b}, [x0]", "ldr q1, [x0]"}
		c := newTestCPU(t, base, prog)
		c.X[0] = buf
		c.Vreg[0] = [2]uint64{0x1122334455667788, 0x99AABBCCDDEEFF00}
		runProg(t, c, base, len(prog))
		if c.Vreg[1] != c.Vreg[0] {
			t.Errorf("st1 layout %#x disagrees with ldr q %#x", c.Vreg[0], c.Vreg[1])
		}
	})

	// Post-index immediate advances the base by the transfer size.
	t.Run("postIndexImm", func(t *testing.T) {
		prog := []string{"ld1 {v0.16b, v1.16b}, [x0], #32"}
		c := newTestCPU(t, base, prog)
		c.X[0] = buf
		runProg(t, c, base, len(prog))
		if c.X[0] != buf+32 {
			t.Errorf("post-index x0 = %#x, want %#x", c.X[0], buf+32)
		}
	})

	// Post-index register advances the base by Xm.
	t.Run("postIndexReg", func(t *testing.T) {
		prog := []string{"st1 {v0.4s}, [x0], x2"}
		c := newTestCPU(t, base, prog)
		c.X[0] = buf
		c.X[2] = 64
		runProg(t, c, base, len(prog))
		if c.X[0] != buf+64 {
			t.Errorf("post-index x0 = %#x, want %#x", c.X[0], buf+64)
		}
	})
}
