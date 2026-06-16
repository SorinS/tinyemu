// Package arm64 is an AArch64 (ARM64) execution core for the integer ISA that
// asm/arm64 assembles. It runs in a flat 64-bit address space (no MMU yet) and
// is validated against native Apple Silicon hardware via a clang-compiled
// oracle (oracle_test.go) — the run-and-check half of the assembler's
// byte-exact differential.
package arm64

import (
	"math/bits"

	"github.com/jtolio/tinyemu-go/mem"
)

// CPU is the AArch64 machine state.
type CPU struct {
	X  [31]uint64 // X0..X30 (X30 = LR)
	SP uint64     // stack pointer (the "register 31" that means SP)
	PC uint64

	// NZCV condition flags.
	N, Z, C, V bool

	Mem *mem.PhysMemoryMap

	// Sys holds writable system registers other than NZCV, keyed by their
	// encoded field bits (so msr/mrs round-trip). NZCV lives in N/Z/C/V.
	Sys map[uint32]uint64

	Halted   bool   // an exception (svc/brk/hlt) or a real halt stopped the core
	ExcType  string // "svc"/"hvc"/"smc"/"brk"/"hlt" when Halted by an exception
	ExcImm   uint16 // the exception's immediate
	Sentinel uint64
}

// New creates a CPU over the given physical memory.
func New(m *mem.PhysMemoryMap) *CPU { return &CPU{Mem: m, Sys: map[uint32]uint64{}} }

// Reset clears architectural state.
func (c *CPU) Reset() {
	c.X = [31]uint64{}
	c.SP, c.PC = 0, 0
	c.N, c.Z, c.C, c.V = false, false, false, false
	c.Sys = map[uint32]uint64{}
	c.Halted, c.ExcType, c.ExcImm = false, "", 0
}

// a64NZCVField is the encoded sysreg field (bits 19:5) of NZCV
// (o0=1, op1=3, CRn=4, CRm=2, op2=0).
const a64NZCVField uint32 = 1<<19 | 3<<16 | 4<<12 | 2<<8

// readSysreg returns a system register's value (NZCV from the flags, else the
// Sys map; unknown reads as 0).
func (c *CPU) readSysreg(field uint32) uint64 {
	if field == a64NZCVField {
		return c.nzcv()
	}
	return c.Sys[field]
}

// writeSysreg writes a system register (NZCV updates the flags; else the Sys
// map records it).
func (c *CPU) writeSysreg(field uint32, v uint64) {
	if field == a64NZCVField {
		c.setFlags(v>>31&1 == 1, v>>30&1 == 1, v>>29&1 == 1, v>>28&1 == 1)
		return
	}
	c.Sys[field] = v
}

// nzcv packs the flags into the architectural NZCV position (bits 31..28), the
// layout MRS/MSR uses and the native oracle reads back.
func (c *CPU) nzcv() uint64 {
	var v uint64
	if c.N {
		v |= 1 << 31
	}
	if c.Z {
		v |= 1 << 30
	}
	if c.C {
		v |= 1 << 29
	}
	if c.V {
		v |= 1 << 28
	}
	return v
}

// NZCV returns the condition flags packed into bits 31..28 (the MRS layout).
func (c *CPU) NZCV() uint64 { return c.nzcv() }

// readX reads register n as a width-bit value. sp selects whether field 31 is
// the stack pointer (true) or the zero register (false).
func (c *CPU) readX(n uint32, is64, sp bool) uint64 {
	var v uint64
	switch {
	case n != 31:
		v = c.X[n]
	case sp:
		v = c.SP
	default:
		v = 0 // XZR
	}
	if !is64 {
		v &= 0xFFFFFFFF
	}
	return v
}

// writeX writes register n. A 32-bit (W) write zeros the upper 32 bits of the
// X register. Field 31 is the stack pointer when sp is set, else the zero
// register (write discarded).
func (c *CPU) writeX(n uint32, is64, sp bool, v uint64) {
	if !is64 {
		v &= 0xFFFFFFFF
	}
	switch {
	case n != 31:
		c.X[n] = v
	case sp:
		c.SP = v
	}
}

// addWithCarry computes a + b + carryIn at the given width and returns the
// result together with the four NZCV flags. ARM's subtraction is add-with-carry
// of the inverted operand (a - b = a + ~b + 1), and its C flag is the carry-out
// — i.e. C=1 means "no borrow", NOT a borrow. This is the single most error-
// prone flag rule, so it lives in one place.
func addWithCarry(a, b uint64, carryIn uint64, is64 bool) (res uint64, n, z, c, v bool) {
	if is64 {
		sum, carry := bits.Add64(a, b, carryIn)
		res = sum
		c = carry == 1
		n = res>>63 != 0
		z = res == 0
		// Signed overflow: a and b same sign, result differs from both.
		v = ((a^res)&(b^res))>>63 != 0
		return
	}
	a &= 0xFFFFFFFF
	b &= 0xFFFFFFFF
	full := a + b + carryIn
	res = full & 0xFFFFFFFF
	c = full>>32 != 0
	n = res>>31&1 != 0
	z = res == 0
	v = ((a^res)&(b^res))>>31&1 != 0
	return
}

// setFlags stores N/Z/C/V.
func (c *CPU) setFlags(n, z, cf, v bool) { c.N, c.Z, c.C, c.V = n, z, cf, v }

// condHolds evaluates a 4-bit AArch64 condition against the current flags.
func (c *CPU) condHolds(cond uint32) bool {
	switch cond >> 1 {
	case 0b000: // eq/ne
		return c.Z == (cond&1 == 0)
	case 0b001: // cs/cc (hs/lo)
		return c.C == (cond&1 == 0)
	case 0b010: // mi/pl
		return c.N == (cond&1 == 0)
	case 0b011: // vs/vc
		return c.V == (cond&1 == 0)
	case 0b100: // hi/ls
		return (c.C && !c.Z) == (cond&1 == 0)
	case 0b101: // ge/lt
		return (c.N == c.V) == (cond&1 == 0)
	case 0b110: // gt/le
		return (c.N == c.V && !c.Z) == (cond&1 == 0)
	default: // 0b111: al/nv — always
		return true
	}
}
