package arm64

import (
	"fmt"
	"os"
)

// Debug knobs, all driven by environment variables so they cost nothing in a
// normal run. dbgf is the shared sink (stderr) for the traces below.
func dbgf(format string, a ...any) { fmt.Fprintf(os.Stderr, format, a...) }

// faultDebug, when TINYEMU_ARM64_FAULT=1, dumps the instruction + register state
// at a data/instruction abort to a low (likely-bogus, emulator-bug) address.
var faultDebug = os.Getenv("TINYEMU_ARM64_FAULT") == "1"

// irqDebug, when TINYEMU_ARM64_IRQ=1, logs external IRQ deliveries.
var irqDebug = os.Getenv("TINYEMU_ARM64_IRQ") == "1"

// spDebug, when TINYEMU_ARM64_SP=1, traces stack-pointer banking across
// exception entry/eret — the diagnostic that surfaced the SP_EL1 drain behind
// the timer-PPI re-delivery bug.
var spDebug = os.Getenv("TINYEMU_ARM64_SP") == "1"

// pcSample, set to N via TINYEMU_ARM64_PCSAMPLE=N, logs the PC every N retired
// instructions — a cheap way to see where a hung guest is spinning.
var pcSample = func() uint64 {
	if v := os.Getenv("TINYEMU_ARM64_PCSAMPLE"); v != "" {
		var n uint64
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}()

// faultLogCount/lastFaultPC dedupe the abort trace, which can recurse when a
// guest's own fault handler re-faults on the same PC.
var faultLogCount int
var lastFaultPC uint64
