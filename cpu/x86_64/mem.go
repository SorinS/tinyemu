package x86_64

import (
	"fmt"
	"os"
)

// pageFaultPanic is what the memory accessors and fetch helpers panic
// with on a translation failure. Step's deferred recover converts it
// into a returned error (and, in Phase 5, into actual #PF delivery).
type pageFaultPanic struct{ Err *PageFaultError }

// physWatch{Lo,Hi} bracket a physical address range whose writes get
// logged with RIP context. Diagnostic for "where does this PML4 entry
// get written?" investigations. Set TINYEMU_X64_PHYSWATCH=<lo>-<hi>.
var (
	physWatchEnabled bool
	physWatchLo      uint64
	physWatchHi      uint64
)

// vaWatch{Lo,Hi} same idea but for guest-linear (virtual) addresses.
// Logs every write whose target VA falls in [lo, hi). Different from
// physWatch because we want to catch the LOGICAL write — e.g. "musl
// wrote zero to user VA 0x7f7dad3d1490". Set TINYEMU_X64_VAWATCH=<lo>-<hi>.
var (
	vaWatchEnabled bool
	vaWatchLo      uint64
	vaWatchHi      uint64
)

func init() {
	parseHex := func(s string) uint64 {
		if len(s) >= 2 && (s[0:2] == "0x" || s[0:2] == "0X") {
			s = s[2:]
		}
		var v uint64
		for _, ch := range s {
			var d uint64
			switch {
			case ch >= '0' && ch <= '9':
				d = uint64(ch - '0')
			case ch >= 'a' && ch <= 'f':
				d = uint64(ch-'a') + 10
			case ch >= 'A' && ch <= 'F':
				d = uint64(ch-'A') + 10
			default:
				return v
			}
			v = v*16 + d
		}
		return v
	}
	parseRange := func(spec string) (lo, hi uint64, ok bool) {
		dash := -1
		for i, ch := range spec {
			if ch == '-' {
				dash = i
				break
			}
		}
		if dash <= 0 {
			return 0, 0, false
		}
		lo = parseHex(spec[:dash])
		hi = parseHex(spec[dash+1:])
		return lo, hi, lo < hi
	}

	if s := os.Getenv("TINYEMU_X64_PHYSWATCH"); s != "" {
		if lo, hi, ok := parseRange(s); ok {
			physWatchLo, physWatchHi, physWatchEnabled = lo, hi, true
			fmt.Fprintf(os.Stderr, "[physw] watching writes in [%#x, %#x)\n", physWatchLo, physWatchHi)
		}
	}
	if s := os.Getenv("TINYEMU_X64_VAWATCH"); s != "" {
		if lo, hi, ok := parseRange(s); ok {
			vaWatchLo, vaWatchHi, vaWatchEnabled = lo, hi, true
			fmt.Fprintf(os.Stderr, "[vaw] watching virtual writes in [%#x, %#x)\n", vaWatchLo, vaWatchHi)
		}
	}
}

// readMem8 reads one byte from a guest-linear address. It honors the
// current paging state: in long mode the address is walked through the
// 4-level page tables; if paging is disabled (CR0.PG=0) the linear
// address is the physical address.
func (c *CPU) readMem8(addr uint64) uint8 {
	phys, perr := c.translateForData(addr, false)
	if perr != nil {
		panic(pageFaultPanic{Err: perr})
	}
	v, err := c.memMap.Read8(phys)
	if err != nil {
		panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
	}
	if vaWatchEnabled && addr >= vaWatchLo && addr < vaWatchHi {
		fmt.Fprintf(os.Stderr, "[vaR] VA=%#x phys=%#x RIP=%#x byte=%#x\n", addr, phys, c.rip, v)
	}
	return v
}

// readMem16 / readMem32 / readMem64 do byte-at-a-time loads. A
// natural-aligned access still hits a single page; the per-byte
// translate is conservative against the cross-page case. M1 prizes
// correctness over speed; the prefetch buffer + TLB are a later pass.
func (c *CPU) readMem16(addr uint64) uint16 {
	return uint16(c.readMem8(addr)) |
		uint16(c.readMem8(addr+1))<<8
}

func (c *CPU) readMem32(addr uint64) uint32 {
	return uint32(c.readMem8(addr)) |
		uint32(c.readMem8(addr+1))<<8 |
		uint32(c.readMem8(addr+2))<<16 |
		uint32(c.readMem8(addr+3))<<24
}

func (c *CPU) readMem64(addr uint64) uint64 {
	return uint64(c.readMem32(addr)) |
		uint64(c.readMem32(addr+4))<<32
}

func (c *CPU) writeMem8(addr uint64, v uint8) {
	phys, perr := c.translateForData(addr, true)
	if perr != nil {
		panic(pageFaultPanic{Err: perr})
	}
	if vaWatchEnabled && addr >= vaWatchLo && addr < vaWatchHi {
		fmt.Fprintf(os.Stderr, "[vaW] VA=%#x phys=%#x RIP=%#x byte=%#x\n", addr, phys, c.rip, v)
	}
	if physWatchEnabled {
		// One-shot watch on a physical-address range. Logs every write,
		// then reads back the surrounding qword so the caller can see
		// what the guest actually committed. A multi-byte store fires
		// the trace on every byte — useful when the store width is
		// unknown, painful when it's known and large, so use a narrow
		// watch range when chasing a specific address.
		if phys >= physWatchLo && phys < physWatchHi {
			defer func() {
				base := phys &^ uint64(7)
				if v8, rerr := c.memMap.Read64(base); rerr == nil {
					fmt.Fprintf(os.Stderr, "[physw] cyc=%d phys=%#x RIP=%#x byte=%#x qword@%#x=%#x\n",
						c.cycles, phys, c.rip, v, base, v8)
				}
			}()
		}
	}
	if err := c.memMap.Write8(phys, v); err != nil {
		panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
	}
}

func (c *CPU) writeMem16(addr uint64, v uint16) {
	c.writeMem8(addr, uint8(v))
	c.writeMem8(addr+1, uint8(v>>8))
}

func (c *CPU) writeMem32(addr uint64, v uint32) {
	c.writeMem8(addr, uint8(v))
	c.writeMem8(addr+1, uint8(v>>8))
	c.writeMem8(addr+2, uint8(v>>16))
	c.writeMem8(addr+3, uint8(v>>24))
}

func (c *CPU) writeMem64(addr uint64, v uint64) {
	c.writeMem32(addr, uint32(v))
	c.writeMem32(addr+4, uint32(v>>32))
}

// readMem128 / writeMem128 — 128-bit SIMD loads/stores. Returned and
// passed as [lo, hi] uint64s where the low qword sits at addr+0 and the
// high qword at addr+8. Cross-page accesses are handled implicitly:
// the underlying readMem64/writeMem64 helpers do byte-at-a-time through
// per-byte translation, so a 16-byte read straddling a page boundary
// faults on the correct page (relevant for the 2026-05-15 unaligned
// cross-page fix).
func (c *CPU) readMem128(addr uint64) [2]uint64 {
	return [2]uint64{c.readMem64(addr), c.readMem64(addr + 8)}
}

func (c *CPU) writeMem128(addr uint64, val [2]uint64) {
	c.writeMem64(addr, val[0])
	c.writeMem64(addr+8, val[1])
}

// translateForData runs the 4-level walker for a data load/store. When
// paging is disabled (CR0.PG=0, which in long mode is illegal but we
// model the bare case for tests) the linear address is returned as the
// physical address.
func (c *CPU) translateForData(addr uint64, isWrite bool) (uint64, *PageFaultError) {
	if c.cr[0]&CR0_PG == 0 {
		return addr, nil
	}
	return c.Translate(addr, isWrite, c.cpl == 3, false)
}

// translateForFetch is the instruction-fetch counterpart. Marks the
// access as an instruction fetch so NX pages fault correctly.
func (c *CPU) translateForFetch(addr uint64) (uint64, *PageFaultError) {
	if c.cr[0]&CR0_PG == 0 {
		return addr, nil
	}
	return c.Translate(addr, false, c.cpl == 3, true)
}

// lip returns the linear instruction pointer for the current mode. In
// long mode CS.base is architecturally zero (the hardware ignores any
// value loaded into the descriptor cache). Other modes add segBase[CS].
func (c *CPU) lip() uint64 {
	if c.mode == ModeLong64 || c.mode == ModeCompat32 {
		return c.rip
	}
	return c.segBase[CS] + c.rip
}

// fetch8 reads the next instruction byte and advances RIP.
func (c *CPU) fetch8() uint8 {
	phys, perr := c.translateForFetch(c.lip())
	if perr != nil {
		panic(pageFaultPanic{Err: perr})
	}
	v, err := c.memMap.Read8(phys)
	if err != nil {
		panic(pageFaultPanic{Err: &PageFaultError{Addr: c.lip()}})
	}
	c.rip++
	return v
}

func (c *CPU) fetch16() uint16 {
	a := uint16(c.fetch8())
	b := uint16(c.fetch8())
	return a | b<<8
}

func (c *CPU) fetch32() uint32 {
	a := uint32(c.fetch8())
	b := uint32(c.fetch8())
	cc := uint32(c.fetch8())
	d := uint32(c.fetch8())
	return a | b<<8 | cc<<16 | d<<24
}

func (c *CPU) fetch64() uint64 {
	return uint64(c.fetch32()) | uint64(c.fetch32())<<32
}
