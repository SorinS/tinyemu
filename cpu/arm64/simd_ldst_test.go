package arm64

import "testing"

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
