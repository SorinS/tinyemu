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
func (c *CPU) Step() (err error) {
	origRIP := c.rip
	if stepTrace {
		var bytes [8]byte
		for i := uint64(0); i < 8; i++ {
			func() {
				defer func() { _ = recover() }()
				bytes[i] = c.readMem8(origRIP + i)
			}()
		}
		fmt.Fprintf(os.Stderr, "[step] RIP=%#x bytes=% x\n", origRIP, bytes[:])
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
		fmt.Fprintf(os.Stderr, "[strwatch] RDI=%#x str=%q\n", rdi, string(buf[:n]))
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
					if derr := c.deliverInterrupt(14, true, ex.Err.ErrorCode); derr == nil {
						err = nil
						return
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
	operandSize := uint8(4)
	addressSize := uint8(8)
	operandOverride := false
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
			addressSize = 4
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
		case b >= 0x40 && b <= 0x4F:
			// REX prefix. Only the last REX before the opcode takes
			// effect, per Intel SDM. We capture by overwrite.
			rex = b
		default:
			if rex&rexW != 0 {
				operandSize = 8
			} else if operandOverride {
				operandSize = 2
			}
			_ = segOverride // wired in Phase 2/3 when memory operands appear
			return c.executeOpcode(b, rex, operandSize, addressSize, segOverride, repPrefix)
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
func (c *CPU) unimplementedAt(format string, args ...any) error {
	rip := c.rip
	const window = 16
	var pre, post [window]byte
	for i := uint64(0); i < window; i++ {
		func() {
			defer func() { _ = recover() }()
			if rip >= window {
				pre[i] = c.readMem8(rip - window + i)
			}
			post[i] = c.readMem8(rip + i)
		}()
	}
	ctx := fmt.Sprintf(" [RIP=%#x pre=% x post=% x]", rip, pre[:], post[:])
	return fmt.Errorf("%w: "+format+"%s",
		append([]any{ErrNotImplemented}, append(args, ctx)...)...)
}

// stepTrace is enabled by TINYEMU_X64_TRACE=1 and prints every
// instruction's RIP + opcode bytes to stderr. Extremely verbose for
// a kernel boot — use to bisect "where did we jump wrong" failures.
var stepTrace = os.Getenv("TINYEMU_X64_TRACE") == "1"

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
