package x86_64

import (
	"fmt"
	"os"
)

// pageFaultPanic is what the memory accessors and fetch helpers panic
// with on a translation failure. Step's deferred recover converts it
// into a returned error (and, in Phase 5, into actual #PF delivery).
type pageFaultPanic struct{ Err *PageFaultError }

// exceptionPanic is raised by instruction handlers that need to deliver a
// processor exception computed during execution — e.g. #DE from DIV/IDIV
// (divide-by-zero or quotient overflow). It is delivered as a FAULT: the
// saved RIP must point at the faulting instruction, which Step()'s recover
// restores from origRIP before vectoring through the IDT, exactly like the
// pageFaultPanic path. HasErr/ErrorCode carry an error code for the
// vectors that push one (#DF, #TS, #NP, #SS, #GP, #PF); #DE pushes none.
type exceptionPanic struct {
	Vec       uint8
	HasErr    bool
	ErrorCode uint32
}

// raiseDE raises #DE (vector 0), the divide-error fault. Never returns.
func (c *CPU) raiseDE() {
	panic(exceptionPanic{Vec: 0})
}

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

// readMem16 / readMem32 / readMem64 load a multi-byte value.
//
// When the access lies wholly within one page they translate once and
// issue a single sized read, so an MMIO device sees ONE atomic register
// access of the right width. This is required, not just an optimization:
// a register file like the local APIC returns the whole 32-bit register
// for its dword offset and zero for the in-between byte offsets, so a
// decomposed byte-at-a-time read would return only the low byte and drop
// the upper 24 bits (e.g. the SVR software-enable bit 8). A page-
// straddling access still goes byte-at-a-time so each byte faults on its
// own page (the 2026-05-15 unaligned cross-page fix); the per-byte path
// is also taken whenever a VA watch is armed so its logging still fires.
func (c *CPU) readMem16(addr uint64) uint16 {
	if addr&0xFFF <= 0xFFE && !vaWatchEnabled {
		phys, perr := c.translateForData(addr, false)
		if perr != nil {
			panic(pageFaultPanic{Err: perr})
		}
		v, err := c.memMap.Read16(phys)
		if err != nil {
			panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
		}
		return v
	}
	return uint16(c.readMem8(addr)) |
		uint16(c.readMem8(addr+1))<<8
}

func (c *CPU) readMem32(addr uint64) uint32 {
	if addr&0xFFF <= 0xFFC && !vaWatchEnabled {
		phys, perr := c.translateForData(addr, false)
		if perr != nil {
			panic(pageFaultPanic{Err: perr})
		}
		v, err := c.memMap.Read32(phys)
		if err != nil {
			panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
		}
		return v
	}
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
	c.maybeInvalidateFetchBufferPhys(phys&c.a20Mask, 1)
	if err := c.memMap.Write8(phys, v); err != nil {
		panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
	}
}

// writeMem16 / writeMem32 mirror the read helpers: a single-page access
// translates once and issues one sized write so an MMIO device receives
// one atomic register write of the correct width (a byte-decomposed store
// would land only the low byte of, say, the APIC SVR and drop the
// software-enable bit). Page-straddling stores, and any store while a
// write watch is armed, fall back to the per-byte path.
func (c *CPU) writeMem16(addr uint64, v uint16) {
	if addr&0xFFF <= 0xFFE && !vaWatchEnabled && !physWatchEnabled {
		phys, perr := c.translateForData(addr, true)
		if perr != nil {
			panic(pageFaultPanic{Err: perr})
		}
		c.maybeInvalidateFetchBufferPhys(phys&c.a20Mask, 2)
		if err := c.memMap.Write16(phys, v); err != nil {
			panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
		}
		return
	}
	c.writeMem8(addr, uint8(v))
	c.writeMem8(addr+1, uint8(v>>8))
}

func (c *CPU) writeMem32(addr uint64, v uint32) {
	if addr&0xFFF <= 0xFFC && !vaWatchEnabled && !physWatchEnabled {
		phys, perr := c.translateForData(addr, true)
		if perr != nil {
			panic(pageFaultPanic{Err: perr})
		}
		c.maybeInvalidateFetchBufferPhys(phys&c.a20Mask, 4)
		if err := c.memMap.Write32(phys, v); err != nil {
			panic(pageFaultPanic{Err: &PageFaultError{Addr: addr}})
		}
		return
	}
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
// fetch8 reads the next instruction byte and advances RIP. Fast path: serve
// from the prefetch buffer (no per-byte translate/GetRange). Slow path:
// refill the buffer, or — for non-RAM fetches — read the single byte.
func (c *CPU) fetch8() uint8 {
	lip := c.lip()
	off := lip - c.ifBufLip
	if off < uint64(c.ifBufValid) {
		b := c.ifBuf[off]
		c.rip++
		return b
	}
	return c.fetch8Slow(lip)
}

// fetch8Slow handles a prefetch-buffer miss: try to refill from RAM and
// serve byte 0; if the fetch target is not RAM, fall back to the per-byte
// translate + Read8 path (this leaves the buffer empty so MMIO fetches are
// never speculatively read-ahead). Separated so fetch8's fast path stays
// inlinable.
func (c *CPU) fetch8Slow(lip uint64) uint8 {
	if c.fillFetchBuffer(lip) {
		b := c.ifBuf[0]
		c.rip++
		return b
	}
	// Non-RAM fetch (rare): translate + read one byte, no buffering.
	phys, perr := c.translateForFetch(lip)
	if perr != nil {
		panic(pageFaultPanic{Err: perr})
	}
	v, err := c.memMap.Read8(phys & c.a20Mask)
	if err != nil {
		panic(pageFaultPanic{Err: &PageFaultError{Addr: lip}})
	}
	c.rip++
	return v
}

// fillFetchBuffer translates `lip` and, if it lands in RAM, copies up to
// len(ifBuf) bytes (stopping at the page boundary) straight from the
// backing slice into ifBuf. Returns true if the buffer was filled (RAM),
// false if the target is non-RAM (caller falls back to a single byte read).
// A page fault during translation propagates via panic, unwinding the
// in-flight Step exactly as the old per-byte path did.
func (c *CPU) fillFetchBuffer(lip uint64) bool {
	pageOff := lip & 0xFFF
	n := uint64(len(c.ifBuf))
	if pageOff+n > 0x1000 {
		n = 0x1000 - pageOff
	}
	phys, perr := c.translateForFetch(lip)
	if perr != nil {
		panic(pageFaultPanic{Err: perr})
	}
	paddr := phys & c.a20Mask
	pr := c.memMap.GetRange(paddr)
	if pr == nil || !pr.IsRAM {
		c.ifBufValid = 0
		return false
	}
	regOff := paddr - pr.Addr
	// Clamp to what the range actually backs (a page may straddle the end
	// of a RAM region — fill only the part that's really RAM).
	if avail := uint64(len(pr.PhysMem)) - regOff; avail < n {
		n = avail
	}
	copy(c.ifBuf[:n], pr.PhysMem[regOff:regOff+n])
	c.ifBufLip = lip
	c.ifBufPhys = paddr
	c.ifBufValid = uint8(n)
	return true
}

// invalidateFetchBuffer drops the prefetch buffer so the next fetch
// refills. Called from every TLB-flush path (Reset, CR3/CR0/CR4/EFER
// changes, INVLPG, mode switch) so a translation change can't surface
// stale instruction bytes.
func (c *CPU) invalidateFetchBuffer() {
	c.ifBufValid = 0
}

// maybeInvalidateFetchBufferPhys clears the prefetch buffer if the
// physical range [addr, addr+size) overlaps the buffered bytes. Called
// from the physical-write paths so self-modifying code (and two-pass test
// fixtures) doesn't serve stale instruction bytes.
func (c *CPU) maybeInvalidateFetchBufferPhys(addr uint64, size uint64) {
	if c.ifBufValid == 0 {
		return
	}
	bufEnd := c.ifBufPhys + uint64(c.ifBufValid)
	if addr < bufEnd && c.ifBufPhys < addr+size {
		c.ifBufValid = 0
	}
}

func (c *CPU) fetch16() uint16 {
	lip := c.lip()
	off := lip - c.ifBufLip
	if off+2 <= uint64(c.ifBufValid) {
		v := uint16(c.ifBuf[off]) | uint16(c.ifBuf[off+1])<<8
		c.rip += 2
		return v
	}
	a := uint16(c.fetch8())
	b := uint16(c.fetch8())
	return a | b<<8
}

func (c *CPU) fetch32() uint32 {
	lip := c.lip()
	off := lip - c.ifBufLip
	if off+4 <= uint64(c.ifBufValid) {
		v := uint32(c.ifBuf[off]) | uint32(c.ifBuf[off+1])<<8 |
			uint32(c.ifBuf[off+2])<<16 | uint32(c.ifBuf[off+3])<<24
		c.rip += 4
		return v
	}
	a := uint32(c.fetch8())
	b := uint32(c.fetch8())
	cc := uint32(c.fetch8())
	d := uint32(c.fetch8())
	return a | b<<8 | cc<<16 | d<<24
}

func (c *CPU) fetch64() uint64 {
	return uint64(c.fetch32()) | uint64(c.fetch32())<<32
}
