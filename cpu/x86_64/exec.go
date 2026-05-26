package x86_64

import (
	"errors"
	"fmt"
	"os"

	"github.com/jtolio/tinyemu-go/cpu"
)

// Compile-time assertion: *CPU implements cpu.X86Core. Catches signature
// drift between the i386 and long-mode backends at build time.
var _ cpu.X86Core = (*CPU)(nil)

// ErrNotImplemented wraps every "opcode not yet implemented" error so
// callers can distinguish "instruction-level missing feature" from
// other Step failures via errors.Is.
var ErrNotImplemented = errors.New("x86_64: decoder feature not implemented yet")

// REX prefix bit positions. The four bits extend the ModR/M, SIB, and
// immediate-size fields:
//
//	rexW (bit 3) — promote default operand size to 64-bit
//	rexR (bit 2) — extend ModR/M.reg from 3 to 4 bits
//	rexX (bit 1) — extend SIB.index from 3 to 4 bits
//	rexB (bit 0) — extend ModR/M.rm or SIB.base from 3 to 4 bits
const (
	rexB uint8 = 1 << 0
	rexX uint8 = 1 << 1
	rexR uint8 = 1 << 2
	rexW uint8 = 1 << 3
)

// Step executes a single instruction. On a page-fault the linear
// address that triggered the walk is reported via the returned
// *PageFaultError (Phase 5 will route this back through the IDT as a
// #PF). For any other failure mode it returns a wrapped
// ErrNotImplemented or a plain error.
// ripRing is a debug-only circular buffer of the last 32 RIPs we've
// executed. Enabled by setting TINYEMU_X64_RIPTRAP=1. When a user-mode
// instruction-fetch fault hits (the fault address equals RIP), the
// ring is dumped so the caller can identify the indirect CALL/JMP/RET
// that landed on a non-executable page.  Off by default to avoid
// per-Step overhead in production.
const ripRingSize = 32

var (
	ripRing       [ripRingSize]uint64
	ripRingIdx    int
	ripRingFull   bool
	ripTrapEnabled = os.Getenv("TINYEMU_X64_RIPTRAP") == "1"
)

func recordRIP(r uint64) {
	if !ripTrapEnabled {
		return
	}
	ripRing[ripRingIdx] = r
	ripRingIdx = (ripRingIdx + 1) % ripRingSize
	if ripRingIdx == 0 {
		ripRingFull = true
	}
}

func dumpRipRing(label string) {
	if !ripTrapEnabled {
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] last %d RIPs (oldest first):\n", label, ripRingSize)
	start := 0
	count := ripRingIdx
	if ripRingFull {
		start = ripRingIdx
		count = ripRingSize
	}
	for i := 0; i < count; i++ {
		idx := (start + i) % ripRingSize
		fmt.Fprintf(os.Stderr, "  %d: %#x\n", i, ripRing[idx])
	}
}

func (c *CPU) Step() (err error) {
	// IP wrapping in real / pm16: tempting to mask to 0xFFFF here, but
	// SeaBIOS routinely runs code that crosses the 64-KB boundary via
	// segment-cached extended limits ("big real mode"). Real hardware
	// would #GP on access beyond CS.limit; we don't model that and the
	// guest depends on the leniency. Pm32 still masks because every
	// pm32 boot we care about uses 32-bit linear addresses with a flat
	// segment — high bits in c.rip are always stale.
	if c.mode == ModeProtected32 {
		c.rip &= 0xFFFFFFFF
	}
	origRIP := c.rip
	recordRIP(origRIP)
	if stepTrace || (ripTraceLo < ripTraceHi && origRIP >= ripTraceLo && origRIP < ripTraceHi) {
		var bytes [8]byte
		for i := uint64(0); i < 8; i++ {
			func() {
				defer func() { _ = recover() }()
				bytes[i] = c.readMem8(origRIP + i)
			}()
		}
		fmt.Fprintf(os.Stderr, "[step] RIP=%#x bytes=% x RAX=%#x RBX=%#x RCX=%#x RDX=%#x RSI=%#x RDI=%#x R12=%#x R13=%#x R14=%#x R15=%#x\n",
			origRIP, bytes[:], c.reg64[RAX], c.reg64[RBX], c.reg64[RCX], c.reg64[RDX],
			c.reg64[RSI], c.reg64[RDI], c.reg64[R12], c.reg64[R13], c.reg64[R14], c.reg64[R15])
	}
	if stringWatchRIP != 0 && origRIP == stringWatchRIP {
		rdi := c.GetReg64(RDI)
		var buf [256]byte
		n := 0
		for n < len(buf) {
			func() {
				defer func() { _ = recover() }()
				b := c.readMem8(rdi + uint64(n))
				if b == 0 {
					n = len(buf) // sentinel to break
					return
				}
				buf[n] = b
				n++
			}()
			if n >= len(buf) || buf[n-1] == 0 {
				break
			}
		}
		// Trim trailing zeros for readability.
		end := 0
		for end < len(buf) && buf[end] != 0 {
			end++
		}
		fmt.Fprintf(os.Stderr, "[strwatch] RDI=%#x RAX=%#x RDX=%#x RSI=%#x str=%q\n",
			rdi, c.GetReg64(RAX), c.GetReg64(RDX), c.GetReg64(RSI), string(buf[:end]))
	}
	defer func() {
		if r := recover(); r != nil {
			switch ex := r.(type) {
			case pageFaultPanic:
				c.rip = origRIP
				c.cr[2] = ex.Err.Addr
				// Try IDT-based delivery first. If no IDT is configured
				// (limit 0) or the gate is bogus, surface the original
				// fault to the host so the caller can decide what to do.
				if c.segLimit[IDTR] > 0 {
					if intrTrace && ex.Err.ErrorCode != 0 {
						// On a "page present" fault, dump the walk so we
						// can see WHICH entry triggered the rsvd check.
						c.dumpWalk(ex.Err.Addr)
					}
					// Dump the RIP ring on a user-mode "jumped to NX page"
					// fault — i.e. an instruction-fetch fault on a
					// page that's present (P=1) with NX set. We do NOT
					// fire on P=0 faults: those are normal lazy-paging
					// where the kernel will install the PTE and resume.
					const X86_PF_P = uint32(1 << 0)
					const X86_PF_INSTR = uint32(1 << 4)
					if c.cpl == 3 && ex.Err.Addr == origRIP &&
						ex.Err.ErrorCode&X86_PF_INSTR != 0 &&
						ex.Err.ErrorCode&X86_PF_P != 0 {
						dumpRipRing("rip-trap")
					}
					if derr := c.deliverInterrupt(14, true, ex.Err.ErrorCode); derr == nil {
						err = nil
						return
					} else {
						// Log the delivery failure once, regardless of
						// intrTrace, because otherwise the caller only
						// sees the original PF and has no clue why the
						// kernel handler didn't run.
						fmt.Fprintf(os.Stderr, "[pf-deliver] FAILED for vec=14 addr=%#x ec=%#x cpl=%d TR.base=%#x CR3=%#x: %v\n",
							ex.Err.Addr, ex.Err.ErrorCode, c.cpl, c.segBase[TR], c.cr[3], derr)
					}
				}
				if pfDebug {
					fmt.Fprintf(os.Stderr, "[pf] addr=%#x ec=%#x RIP=%#x CR3=%#x IDTR.base=%#x IDTR.limit=%#x\n",
						ex.Err.Addr, ex.Err.ErrorCode, c.rip, c.cr[3], c.segBase[IDTR], c.segLimit[IDTR])
					// Walk the page tables manually and report what's
					// present at each level.
					cr3 := c.cr[3] & 0xFFFFFFFFF000
					addr := ex.Err.Addr
					pml4i := (addr >> 39) & 0x1FF
					pdpti := (addr >> 30) & 0x1FF
					pdi := (addr >> 21) & 0x1FF
					pti := (addr >> 12) & 0x1FF
					// Print a few neighbouring PML4 entries for context.
					for _, idx := range []uint64{0, pml4i, 0x100, 0x111, 0x1FF} {
						v, _ := c.memMap.Read64(cr3 + idx*8)
						fmt.Fprintf(os.Stderr, "[pf]   PML4[%#x] @ %#x = %#x\n",
							idx, cr3+idx*8, v)
					}
					pml4e, _ := c.memMap.Read64(cr3 + pml4i*8)
					if pml4e&1 != 0 {
						pdpt := pml4e & 0xFFFFFFFFF000
						pdpte, _ := c.memMap.Read64(pdpt + pdpti*8)
						fmt.Fprintf(os.Stderr, "[pf]   PDPT[%#x] @ %#x = %#x\n",
							pdpti, pdpt+pdpti*8, pdpte)
						if pdpte&1 != 0 && pdpte&0x80 == 0 {
							pd := pdpte & 0xFFFFFFFFF000
							pde, _ := c.memMap.Read64(pd + pdi*8)
							fmt.Fprintf(os.Stderr, "[pf]   PD[%#x]   @ %#x = %#x\n",
								pdi, pd+pdi*8, pde)
							if pde&1 != 0 && pde&0x80 == 0 {
								pt := pde & 0xFFFFFFFFF000
								pte, _ := c.memMap.Read64(pt + pti*8)
								fmt.Fprintf(os.Stderr, "[pf]   PT[%#x]  @ %#x = %#x\n",
									pti, pt+pti*8, pte)
							}
						}
					}
				}
				err = ex.Err
			default:
				panic(r)
			}
		}
	}()

	if c.intrLineState != 0 && (c.rflags&RFLAGS_IF) != 0 && !c.interruptsBlocked {
		// Hardware interrupt pending and IF set. Ack the PIC to learn
		// the vector, then vector through the IDT. If no ack handler
		// is wired (test harnesses without a PIC) we just consume the
		// interrupt-line state without delivery.
		if c.ackInterruptFunc != nil {
			if vec, ok := c.ackInterruptFunc(); ok {
				if intrTrace {
					fmt.Fprintf(os.Stderr, "[intr] external vec=%d ack RIP=%#x RFLAGS=%#x\n",
						vec, c.rip, c.rflags)
				}
				if derr := c.deliverInterrupt(vec, false, 0); derr != nil {
					return derr
				}
				c.interruptsBlocked = false
				return nil
			}
		}
	}
	c.interruptsBlocked = false

	var rex uint8
	// Default operand/address size depends on the current CPU mode.
	// 0x66/0x67 prefixes flip from this default; REX.W (long mode)
	// promotes operand size to 8 unconditionally.
	//
	//                    operand   address
	//   ModeReal16          2         2
	//   ModeProtected16     2         2
	//   ModeProtected32     4         4
	//   ModeCompat32        4         4
	//   ModeLong64          4         8
	operandSize := uint8(4)
	addressSize := uint8(8)
	switch c.mode {
	case ModeReal16, ModeProtected16:
		operandSize = 2
		addressSize = 2
	case ModeProtected32, ModeCompat32:
		operandSize = 4
		addressSize = 4
	}
	operandOverride := false
	addressOverride := false
	segOverride := -1
	// String-op repetition: 0 = none, 1 = REP/REPE (0xF3), 2 = REPNE
	// (0xF2). The decoder for MOVS/STOS/LODS/SCAS consults it; on any
	// other opcode the prefix is silently dropped. That's correct for
	// well-formed kernel/user code but masks two subtle cases:
	//   - 0xF3 has been repurposed as part of multi-byte VEX/XOP
	//     prefixes for SSE3+/AVX (e.g. F3 0F BD = LZCNT vs plain BSR).
	//     Until we wire SSE+ decoding, those would silently fall
	//     through to the legacy 0F BD = BSR semantics.
	//   - REP on instructions where the SDM lists it as "undefined"
	//     would silently succeed instead of #UD.
	// Both are accepted risk until they bite — kernel boot exercises
	// REP/REPE/REPNE only on the string-op family the dispatcher
	// recognises.
	repPrefix := uint8(0)

	for {
		b := c.fetch8()
		switch {
		case b == 0x66:
			operandOverride = true
		case b == 0x67:
			addressOverride = true
		case b == 0xF0:
			// LOCK. Single-threaded emulation: every instruction is
			// already serialized, so LOCK is functionally a no-op.
			// The real-hardware rule that LOCK on an instruction that
			// doesn't support it raises #UD is NOT enforced; a
			// malformed LOCK prefix would silently succeed.
		case b == 0xF2:
			repPrefix = 2
		case b == 0xF3:
			repPrefix = 1
		case b == 0x2E:
			segOverride = CS
		case b == 0x36:
			segOverride = SS
		case b == 0x3E:
			segOverride = DS
		case b == 0x26:
			segOverride = ES
		case b == 0x64:
			segOverride = FS
		case b == 0x65:
			segOverride = GS
		case b >= 0x40 && b <= 0x4F && c.mode == ModeLong64:
			// REX prefix. Encodings 0x40..0x4F are REX prefixes ONLY in
			// long mode (CS.L=1). In real / pm16 / pm32 / compat32 they
			// are `INC reg` (0x40..0x47) and `DEC reg` (0x48..0x4F) —
			// real instructions, not prefixes. Capturing them as REX
			// outside long mode would silently mis-decode every INC/DEC
			// in a 32-bit BIOS or kernel, which was the SeaBIOS-format-
			// walker bug ("SeaBIOS (version rrrrrrrr..." because INC
			// EBX never advanced).
			rex = b
		default:
			// Apply prefix overrides relative to the mode-derived
			// defaults set above. 0x66 flips operand size between 16
			// and 32; REX.W overrides to 64. 0x67 flips address size:
			// in 16-bit modes -> 32, in 32-bit modes -> 16, in long
			// mode -> 32 (no 16-bit addressing under long mode).
			if rex&rexW != 0 {
				operandSize = 8
			} else if operandOverride {
				if operandSize == 2 {
					operandSize = 4
				} else {
					operandSize = 2
				}
			}
			if addressOverride {
				switch c.mode {
				case ModeReal16, ModeProtected16:
					addressSize = 4
				case ModeProtected32, ModeCompat32:
					addressSize = 2
				case ModeLong64:
					addressSize = 4
				}
			}
			// Stash the override so memory accessors can apply the
			// right segment base without each opcode handler having
			// to thread it through manually. Reset after dispatch
			// so a stale override from the previous instruction
			// can't leak into the next.
			c.currentSegOverride = segOverride
			c.currentAddressSize = addressSize
			err := c.executeOpcode(b, rex, operandSize, addressSize, segOverride, repPrefix, operandOverride)
			c.currentSegOverride = -1
			return err
		}
	}
}

// Run executes up to maxCycles cycles.
func (c *CPU) Run(maxCycles int) error {
	for i := 0; i < maxCycles; i++ {
		if c.powerDown {
			if !c.HasPendingInterrupt() {
				return nil
			}
			c.powerDown = false
		}
		if ripSampleEvery > 0 && c.cycles%ripSampleEvery == 0 {
			fmt.Fprintf(os.Stderr, "[sample] cycle=%d mode=%v CS=%#x:%#x RIP=%#x lin=%#x\n",
				c.cycles, c.mode, c.seg[CS], c.segBase[CS], c.rip,
				c.segBase[CS]+c.rip)
		}
		// One-shot register dump at a cycle picked by TINYEMU_X64_DUMPCYCLE.
		// Also dumps bregs near [DS:EAX] so we can correlate values.
		if dumpCycle > 0 && c.cycles == dumpCycle {
			lin := c.segBase[DS] + uint64(uint32(c.reg64[RAX]))
			var b [0x28]byte
			for i := 0; i < len(b); i++ {
				func() {
					defer func() { _ = recover() }()
					b[i] = c.readMem8(lin + uint64(i))
				}()
			}
			fmt.Fprintf(os.Stderr,
				"[dump] cycle=%d EAX=%#x ESP=%#x DS=%#x:%#x SS=%#x:%#x bregs@%#x={%s}\n",
				c.cycles, uint32(c.reg64[RAX]),
				uint32(c.reg64[RSP]), c.seg[DS], c.segBase[DS],
				c.seg[SS], c.segBase[SS], lin, hexDump(b[:]))
			// Also dump top-of-stack 16 bytes — that's where the iretw
			// frame lives by the time IRET is reached.
			slin := c.segBase[SS] + uint64(uint32(c.reg64[RSP]))
			var st [16]byte
			for i := 0; i < len(st); i++ {
				func() {
					defer func() { _ = recover() }()
					st[i] = c.readMem8(slin + uint64(i))
				}()
			}
			fmt.Fprintf(os.Stderr, "[dump] stack@%#x={%s}\n", slin, hexDump(st[:]))
		}
		if err := c.Step(); err != nil {
			return err
		}
		c.cycles++
	}
	return nil
}

// unimplemented wraps an opcode-not-yet-supported error so it can be
// identified via errors.Is(err, ErrNotImplemented).
func unimplemented(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrNotImplemented}, args...)...)
}

// unimplementedAt is the same but includes the current RIP and the
// 16 surrounding bytes so the failure has enough context to
// distinguish "missing opcode" from "jumped into data". The helper
// reads via the page tables; any fault during the dump is swallowed
// so it can't mask the original error.
//
// In non-long-mode (real / pm16 / pm32 / compat32) we apply CS.base
// when reading the surrounding bytes, otherwise the dump shows
// garbage at the low-physical address (or all-zero from unmapped
// memory) rather than the actual code bytes.
func (c *CPU) unimplementedAt(format string, args ...any) error {
	rip := c.rip
	addrBase := uint64(0)
	if c.mode != ModeLong64 && c.mode != ModeCompat32 {
		addrBase = c.segBase[CS]
	}
	const window = 16
	var pre, post [window]byte
	for i := uint64(0); i < window; i++ {
		func() {
			defer func() { _ = recover() }()
			if rip >= window {
				pre[i] = c.readMem8(addrBase + rip - window + i)
			}
			post[i] = c.readMem8(addrBase + rip + i)
		}()
	}
	ctx := fmt.Sprintf(" [mode=%v RIP=%#x CSbase=%#x CSlim=%#x addrSize=%d pre=% x post=% x]",
		c.mode, rip, c.segBase[CS], c.segLimit[CS], c.currentAddressSize, pre[:], post[:])
	return fmt.Errorf("%w: "+format+"%s",
		append([]any{ErrNotImplemented}, append(args, ctx)...)...)
}

// stepTrace is enabled by TINYEMU_X64_TRACE=1 and prints every
// instruction's RIP + opcode bytes to stderr. Extremely verbose for
// a kernel boot — use to bisect "where did we jump wrong" failures.
var stepTrace = os.Getenv("TINYEMU_X64_TRACE") == "1"

// hexDump renders a slice as space-separated 2-digit hex (no 0x prefix,
// no trailing space). Used by the one-shot dump trace.
func hexDump(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 3*len(b)-1)
	for i, x := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, hex[x>>4], hex[x&0xf])
	}
	return string(out)
}

// dumpCycle, set by TINYEMU_X64_DUMPCYCLE=N, triggers a one-shot
// register/segment dump immediately BEFORE Step() at cycle N. Used to
// snapshot EAX (the bregs pointer) right before a problematic PUSH so a
// trace can be matched against the bregs struct in memory.
var dumpCycle uint64 = func() uint64 {
	s := os.Getenv("TINYEMU_X64_DUMPCYCLE")
	if s == "" {
		return 0
	}
	var v uint64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		v = v*10 + uint64(ch-'0')
	}
	return v
}()

// ripSampleEvery is set by TINYEMU_X64_RIPSAMPLE=N. If non-zero, every
// N-th cycle prints RIP — useful for finding where a hung kernel is
// spinning without the noise of full step tracing.
var ripSampleEvery uint64 = func() uint64 {
	s := os.Getenv("TINYEMU_X64_RIPSAMPLE")
	if s == "" {
		return 0
	}
	var v uint64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		v = v*10 + uint64(ch-'0')
	}
	return v
}()

// ripTrace{Lo,Hi} bracket an RIP range to step-trace. Set
// TINYEMU_X64_RIPTRACE=<lo>-<hi> (hex). Useful for narrow windows
// where TINYEMU_X64_TRACE=1 would be too verbose.
var ripTraceLo, ripTraceHi uint64

func init() {
	s := os.Getenv("TINYEMU_X64_RIPTRACE")
	if s == "" {
		return
	}
	dash := -1
	for i, ch := range s {
		if ch == '-' {
			dash = i
			break
		}
	}
	if dash < 0 {
		return
	}
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
	ripTraceLo = parseHex(s[:dash])
	ripTraceHi = parseHex(s[dash+1:])
}

// pfDebug logs each page fault that surfaces to the host (i.e. not
// delivered via IDT). Set TINYEMU_X64_PF=1.
var pfDebug = os.Getenv("TINYEMU_X64_PF") == "1"

// stringWatchRIP enables TINYEMU_X64_STRWATCH=<hex>: when RIP equals
// that value, dump up to 256 bytes of memory pointed at by RDI as
// an ASCII string. Useful for capturing panic messages without
// dragging a full kernel symbol map into the emulator.
var stringWatchRIP = func() uint64 {
	s := os.Getenv("TINYEMU_X64_STRWATCH")
	if s == "" {
		return 0
	}
	var v uint64
	if len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
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
}()
