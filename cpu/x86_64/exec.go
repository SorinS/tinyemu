package x86_64

import (
	"errors"
	"fmt"

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
		// Interrupt delivery is wired up in Phase 5; until then we
		// honor the interrupt by clearing the pending state but not
		// vectoring through the IDT.
		_ = c.ackInterruptFunc
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
