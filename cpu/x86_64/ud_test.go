package x86_64

import (
	"errors"
	"testing"
)

// TestInvalidEncoding_RaisesUD: architecturally-invalid encodings must be
// delivered to the guest as #UD (vector 6), not surfaced as
// ErrNotImplemented (which halts the emulator). This is the distinction
// that bit go-boot: it executed 0xFF /7 (a reserved Group 5 encoding) and
// temu aborted with "decoder feature not implemented" instead of letting
// the guest's own #UD handler run.
func TestInvalidEncoding_RaisesUD(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
	}{
		{"Group5 /7 (0xFF /7, reg form)", []byte{0xFF, 0xF8}}, // ModRM 0xF8: mod=11 reg=7 rm=0
		{"Group4 /2 (0xFE /2)", []byte{0xFE, 0xD0}},           // ModRM 0xD0: mod=11 reg=2 rm=0
	}
	const idtBase uint64 = 0x4000
	const udHandler uint64 = 0x90000
	const codeAddr uint64 = 0x1000

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, mm := longModeCPU(t)
			c.segBase[IDTR] = idtBase
			c.segLimit[IDTR] = 0x1000 - 1
			installIDTGate(t, mm, idtBase, 6, 0x0008, udHandler, 0, 0x8E)
			c.reg64[RSP] = 0x8000
			for i, b := range tc.bytes {
				_ = mm.Write8(codeAddr+uint64(i), b)
			}
			c.SetRIP(codeAddr)

			err := c.Step()
			if errors.Is(err, ErrNotImplemented) {
				t.Fatalf("invalid encoding returned ErrNotImplemented (emulator halt); want #UD delivery: %v", err)
			}
			if err != nil {
				t.Fatalf("Step: %v", err)
			}
			if c.GetRIP() != udHandler {
				t.Errorf("RIP = %#x after invalid opcode, want #UD handler %#x", c.GetRIP(), udHandler)
			}
			// The saved RIP on the stack should point at the faulting
			// instruction (#UD is a fault, not a trap).
			if rip, _ := mm.Read64(c.GetReg64(RSP)); rip != codeAddr {
				t.Errorf("saved RIP = %#x, want faulting insn %#x", rip, codeAddr)
			}
		})
	}
}
