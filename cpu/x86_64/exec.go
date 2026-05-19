package x86_64

import (
	"errors"

	"github.com/jtolio/tinyemu-go/cpu"
)

// Compile-time assertion: *CPU implements cpu.X86Core. Catches signature
// drift between the i386 and long-mode backends at build time.
var _ cpu.X86Core = (*CPU)(nil)

// ErrNotImplemented is returned by Step when the decoder for the
// current mode has no implementation yet. It is wrapped (not equality-
// compared) by tests; comparison should use errors.Is.
var ErrNotImplemented = errors.New("x86_64: instruction decoder not implemented yet")

// Step executes a single instruction. The decoder is built up across
// subsequent milestones; until then Step returns ErrNotImplemented so
// callers that wire the CPU into a Run loop fail loudly instead of
// spinning silently.
func (c *CPU) Step() error {
	return ErrNotImplemented
}

// Run executes up to maxCycles cycles. Same shape as cpu/x86's Run.
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
