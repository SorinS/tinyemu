package x86_64

import (
	"testing"
	"time"
)

// TestCPUID_TSCFrequencyLeaves: the TSC frequency must be discoverable via
// CPUID leaf 0x15 (and 0x16), so guests convert RDTSC ticks to time instead
// of falling back to a bogus default (TamaGo's detectCoreFrequency sets
// freq=1 → a 1 GHz multiplier → time off by ~1e9). Leaf 0x15 reports
// TSC_freq = ECX * EBX / EAX; we target 1 GHz to match the wall-clock RDTSC.
func TestCPUID_TSCFrequencyLeaves(t *testing.T) {
	c := newTestCPU(t)

	// Max basic leaf must cover 0x16, else guests skip 0x15/0x16.
	c.SetReg64(RAX, 0)
	c.SetReg64(RCX, 0)
	if err := c.opCPUID(); err != nil {
		t.Fatalf("opCPUID(0): %v", err)
	}
	if max := c.GetReg32(EAX); max < 0x16 {
		t.Fatalf("max basic leaf = %#x, want >= 0x16", max)
	}

	// Leaf 0x15: TSC/crystal ratio. EAX (denominator) must be non-zero.
	c.SetReg64(RAX, 0x15)
	c.SetReg64(RCX, 0)
	if err := c.opCPUID(); err != nil {
		t.Fatalf("opCPUID(0x15): %v", err)
	}
	den, num, crystal := c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX)
	if den == 0 {
		t.Fatalf("leaf 0x15 EAX (denominator) = 0; guests ignore the leaf")
	}
	if got := uint64(num) * uint64(crystal) / uint64(den); got != 1_000_000_000 {
		t.Errorf("leaf 0x15 TSC freq = %d Hz, want 1e9 (num=%d crystal=%d den=%d)",
			got, num, crystal, den)
	}

	// Leaf 0x16: processor base frequency in MHz (1000 = 1 GHz).
	c.SetReg64(RAX, 0x16)
	c.SetReg64(RCX, 0)
	if err := c.opCPUID(); err != nil {
		t.Fatalf("opCPUID(0x16): %v", err)
	}
	if base := c.GetReg32(EAX); base != 1000 {
		t.Errorf("leaf 0x16 base MHz = %d, want 1000", base)
	}
}

// TestRDTSC_WallClock: RDTSC must advance with real time at the advertised
// 1 GHz (nanoseconds since reset), not as an instruction count. A bug here
// was the root cause of TamaGo reporting a 1M-iteration loop as "1388h":
// with no TSC frequency it assumed 1 tick = 1 second.
func TestRDTSC_WallClock(t *testing.T) {
	c, mm := longModeCPU(t)

	rdtsc := func() uint64 {
		// RDTSC = 0F 31; result in EDX:EAX.
		if err := runInsn(t, c, mm, []byte{0x0F, 0x31}); err != nil {
			t.Fatalf("RDTSC: %v", err)
		}
		return uint64(c.GetReg32(EAX)) | uint64(c.GetReg32(EDX))<<32
	}

	t0 := rdtsc()
	const slept = 20 * time.Millisecond
	time.Sleep(slept)
	t1 := rdtsc()

	if t1 <= t0 {
		t.Fatalf("RDTSC not monotonic: %d then %d", t0, t1)
	}

	// At 1 GHz, 20 ms ≈ 2e7 ticks. Allow a wide band for scheduler jitter
	// and sleep overshoot, but reject the old behaviour (a near-zero
	// instruction-count delta, or the absurdly large value a wrong
	// multiplier would imply).
	delta := t1 - t0
	if delta < uint64(slept.Nanoseconds())/4 {
		t.Errorf("RDTSC delta = %d ns over a %v sleep, far too small for a 1 GHz wall-clock TSC", delta, slept)
	}
	if delta > uint64((10 * time.Second).Nanoseconds()) {
		t.Errorf("RDTSC delta = %d ns over a %v sleep, implausibly large", delta, slept)
	}
}
