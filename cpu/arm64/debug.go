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

// wfiDebug, when TINYEMU_ARM64_WFI=1, dumps the timer/interrupt state each time
// the core parks in WFI with no interrupt pending — to see what a wedged guest
// is waiting for.
var wfiDebug = os.Getenv("TINYEMU_ARM64_WFI") == "1"

// excDebug, when TINYEMU_ARM64_EXC=1, logs the first exceptions taken (EC, ELR,
// FAR) — to see the original exception that led into a handler, not just MMU
// aborts.
var excDebug = os.Getenv("TINYEMU_ARM64_EXC") == "1"
var excLogCount int

// watchPA (TINYEMU_ARM64_WATCHPA=pa, hex) logs every write whose translated
// physical address lands in the same 8-byte slot — to see who writes a PTE.
var watchPA = func() uint64 {
	v := os.Getenv("TINYEMU_ARM64_WATCHPA")
	if v == "" {
		return 0
	}
	var n uint64
	fmt.Sscanf(v, "%x", &n)
	return n
}()

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

// hangLo/hangHi (TINYEMU_ARM64_HANG=lo:hi, hex) bound a PC window; the first
// time each PC in it executes, the instruction word + a few regs are dumped —
// for decoding a tight spin loop (disassemble the dump to read the loop body).
var hangSeen = map[uint64]bool{}
var hangLo, hangHi = func() (uint64, uint64) {
	v := os.Getenv("TINYEMU_ARM64_HANG")
	if v == "" {
		return 0, 0
	}
	var lo, hi uint64
	fmt.Sscanf(v, "%x:%x", &lo, &hi)
	return lo, hi
}()

// loadPC (TINYEMU_ARM64_LOADPC=pc, hex) logs the X0 value (a load/MMIO address)
// each distinct time the given PC executes. Names what a poll loop reads.
var loadPC = func() uint64 {
	v := os.Getenv("TINYEMU_ARM64_LOADPC")
	if v == "" {
		return 0
	}
	var n uint64
	fmt.Sscanf(v, "%x", &n)
	return n
}()
