package arm64

import (
	"math"
	"testing"

	asmarm64 "github.com/sorins/tinyemu-go/asm/arm64"
	"github.com/sorins/tinyemu-go/mem"
)

// newTestCPU builds a CPU with 1 MiB of RAM and the assembled program loaded at
// base, ready to single-step.
func newTestCPU(t *testing.T, base uint64, prog []string) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	var off uint64
	for _, s := range prog {
		b, err := asmarm64.Assemble(s)
		if err != nil {
			t.Fatalf("assemble %q: %v", s, err)
		}
		for _, x := range b {
			_ = mm.Write8(base+off, x)
			off++
		}
	}
	c := New(mm)
	c.PC = base
	t.Cleanup(func() { mm.Close() })
	return c
}

func runProg(t *testing.T, c *CPU, base uint64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d at %#x: %v", i, c.PC, err)
		}
	}
}

// TestFPLoadStoreRoundTrip stores each scalar FP width to RAM and loads it back
// into a different register, checking the value survives — the half the native
// oracle can't exercise (it can't deref an arbitrary guest address).
func TestFPLoadStoreRoundTrip(t *testing.T) {
	const base = 0x10000
	const buf = 0x40000
	prog := []string{
		"str d1, [x0]", "ldr d2, [x0]", // double
		"str s3, [x0, #16]", "ldr s4, [x0, #16]", // single
		"str q5, [x0, #32]", "ldr q6, [x0, #32]", // 128-bit
		"str h7, [x0, #48]", "ldr h8, [x0, #48]", // half (raw bits)
		"str b9, [x0, #56]", "ldr b10, [x0, #56]", // byte
	}
	c := newTestCPU(t, base, prog)
	c.X[0] = buf
	c.writeVD(1, math.Float64bits(-3.5))
	c.writeVS(3, math.Float32bits(2.25))
	c.Vreg[5] = [2]uint64{0x0123456789ABCDEF, 0xFEDCBA9876543210}
	c.Vreg[7] = [2]uint64{0xBEEF, 0}
	c.Vreg[9] = [2]uint64{0xA5, 0}
	runProg(t, c, base, len(prog))

	if got := math.Float64frombits(c.readVD(2)); got != -3.5 {
		t.Errorf("d2 round-trip = %v, want -3.5", got)
	}
	if got := math.Float32frombits(c.readVS(4)); got != 2.25 {
		t.Errorf("s4 round-trip = %v, want 2.25", got)
	}
	if c.Vreg[6] != c.Vreg[5] {
		t.Errorf("q6 round-trip = %#x, want %#x", c.Vreg[6], c.Vreg[5])
	}
	// Loading S/D/H/B must zero the rest of the destination register.
	if c.Vreg[4][1] != 0 || c.Vreg[4][0]>>32 != 0 {
		t.Errorf("ldr s4 left high bits set: %#x", c.Vreg[4])
	}
	if c.Vreg[8] != [2]uint64{0xBEEF, 0} {
		t.Errorf("h8 round-trip = %#x, want 0xBEEF", c.Vreg[8])
	}
	if c.Vreg[10] != [2]uint64{0xA5, 0} {
		t.Errorf("b10 round-trip = %#x, want 0xA5", c.Vreg[10])
	}
}

// TestFcvtzsSaturation checks the FCVTZS/FCVTZU saturation/NaN rules that Go's
// raw float→int conversion leaves undefined.
func TestFcvtzsSaturation(t *testing.T) {
	cases := []struct {
		f      float64
		is64   bool
		signed bool
		want   uint64
	}{
		{3.9, true, true, 3},                       // truncate toward zero
		{-3.9, true, true, ^uint64(0) - 2},         // -3
		{math.NaN(), true, true, 0},                // NaN -> 0
		{1e300, true, true, 0x7FFFFFFFFFFFFFFF},    // overflow -> INT64_MAX
		{-1e300, true, true, 0x8000000000000000},   // -overflow -> INT64_MIN
		{1e300, true, false, math.MaxUint64},       // unsigned overflow
		{-5.0, true, false, 0},                     // negative -> 0 (unsigned)
		{1e300, false, true, 0x7FFFFFFF},           // 32-bit signed sat
		{-1e300, false, true, 0x80000000},          // 32-bit signed -sat
		{1e300, false, false, 0xFFFFFFFF},          // 32-bit unsigned sat
		{2.5, false, true, 2},                      // round toward zero
	}
	for _, tc := range cases {
		if got := fpToIntSat(tc.f, tc.is64, tc.signed); got != tc.want {
			t.Errorf("fpToIntSat(%v, is64=%v, signed=%v) = %#x, want %#x",
				tc.f, tc.is64, tc.signed, got, tc.want)
		}
	}
}
