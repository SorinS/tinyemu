package x86_64

// Spec-sweep coverage test for the x86_64 decoder. Walks every primary
// opcode (0x00..0xFF) and the 2-byte 0x0F escape (0x0F 0x00..0xFF) with
// a minimal valid encoding and classifies each outcome:
//
//   PREFIX   — byte is consumed by Step's prefix loop, then dispatch
//               continues with the next byte. We treat this as covered.
//   OK       — dispatcher handled it (may have produced an emulated
//               fault like #PF or #GP; we don't distinguish those here
//               because the goal is "is the opcode wired?" not "is
//               its result correct on these inputs?")
//   UNIMPL   — returned an error wrapping ErrNotImplemented.
//   OTHER    — some other error path. Usually means my test encoding
//               is malformed for this particular opcode — these need
//               case-by-case handling.
//
// The test prints a per-opcode matrix and FAILS if any opcode in the
// "must be wired" set is UNIMPL or OTHER. The "must be wired" set is
// what we know is needed for general-purpose user code (kernel boot +
// musl/glibc + busybox). Privileged / SSE3+/AVX opcodes that we
// deliberately don't model are listed under expectedUnimpl.
//
// Running:
//
//   go test ./cpu/x86_64/ -run TestOpcodeSweep -v
//
// The verbose output is the inventory you want; non-verbose just fails
// if the must-be-wired set regresses.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

type sweepResult int

const (
	resultPrefix sweepResult = iota
	resultOK
	resultUnimpl
	resultOther
)

func (r sweepResult) String() string {
	switch r {
	case resultPrefix:
		return "PREFIX"
	case resultOK:
		return "OK"
	case resultUnimpl:
		return "UNIMPL"
	case resultOther:
		return "OTHER"
	}
	return "?"
}

// trySingle runs a single instruction byte sequence and returns a result
// classification. The sequence must NOT modify control flow in a way
// that breaks the next iteration (CALL/JMP/RET would).
func trySingle(t *testing.T, bytes []byte) (sweepResult, string) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	defer mm.Close()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		return resultOther, fmt.Sprintf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()
	// Plant a sentinel HLT after our test bytes so the CPU has somewhere
	// to land even if our bytes happen to fall through.
	const codeAddr uint64 = 0x10000
	for i, b := range bytes {
		_ = mm.Write8(codeAddr+uint64(i), b)
	}
	_ = mm.Write8(codeAddr+uint64(len(bytes)), 0xF4) // HLT terminator
	c.SetRIP(codeAddr)
	// Set RSP to something safe in case PUSH/POP fires.
	c.reg64[RSP] = 0x8000
	err := c.Step()
	if err == nil {
		return resultOK, ""
	}
	if errors.Is(err, ErrNotImplemented) {
		return resultUnimpl, err.Error()
	}
	// Page faults are "OK" for the spec sweep — the opcode WAS dispatched
	// and tried to do something (e.g. access memory the harness didn't
	// map). We only care about "decoder feature missing".
	var pf *PageFaultError
	if errors.As(err, &pf) {
		return resultOK, ""
	}
	return resultOther, err.Error()
}

// encodeSingleByte builds a minimal encoding for a single-byte opcode.
// For opcodes that require a ModR/M, we use 0xC0 (mod=11 reg=0 rm=0 =
// reg-reg, no memory). For opcodes with immediates, we append zeros.
func encodeSingleByte(op byte) []byte {
	switch {
	// Prefixes — consumed by Step's prefix loop. Append HLT so the
	// loop has something to dispatch after consuming the prefix.
	case op == 0x26 || op == 0x2E || op == 0x36 || op == 0x3E ||
		op == 0x64 || op == 0x65 || op == 0x66 || op == 0x67 ||
		op == 0xF0 || op == 0xF2 || op == 0xF3 ||
		(op >= 0x40 && op <= 0x4F):
		return []byte{op}
	// 2-byte escape — handled separately
	case op == 0x0F:
		return nil
	// Two-byte opcodes 0x9A/0xEA are far jump/call — invalid in 64-bit
	case op == 0x9A || op == 0xEA:
		return []byte{op}
	// ALU r/m, r forms (need ModR/M)
	case op <= 0x3F && (op&0x07) <= 3:
		return []byte{op, 0xC0}
	// AL imm8 / rAX imm32 forms (just an immediate, no ModR/M)
	case op <= 0x3F && (op&0x07) == 4:
		return []byte{op, 0x00}
	case op <= 0x3F && (op&0x07) == 5:
		return []byte{op, 0x00, 0x00, 0x00, 0x00}
	// REX.W mov r/m64, imm32 (0xC7 /0)
	case op == 0xC7:
		return []byte{op, 0xC0, 0x00, 0x00, 0x00, 0x00}
	case op == 0xC6:
		return []byte{op, 0xC0, 0x00}
	// String ops — no ModR/M
	case op >= 0xA4 && op <= 0xAF:
		return []byte{op}
	// PUSH/POP r64 (0x50-0x5F)
	case op >= 0x50 && op <= 0x5F:
		return []byte{op}
	// MOV r8/r32, imm
	case op >= 0xB0 && op <= 0xB7:
		return []byte{op, 0x00}
	case op >= 0xB8 && op <= 0xBF:
		return []byte{op, 0x00, 0x00, 0x00, 0x00}
	// Jcc short (0x70-0x7F)
	case op >= 0x70 && op <= 0x7F:
		return []byte{op, 0x00}
	// LOOP / LOOPE / LOOPNE / JECXZ (0xE0-0xE3)
	case op >= 0xE0 && op <= 0xE3:
		return []byte{op, 0x00}
	// IN/OUT imm8 (0xE4-0xE7)
	case op >= 0xE4 && op <= 0xE7:
		return []byte{op, 0x00}
	// CALL/JMP rel32, JMP rel8
	case op == 0xE8 || op == 0xE9:
		return []byte{op, 0x00, 0x00, 0x00, 0x00}
	case op == 0xEB:
		return []byte{op, 0x00}
	// INT imm8
	case op == 0xCD:
		return []byte{op, 0x80}
	// Group 1 imm — uses ModR/M
	case op >= 0x80 && op <= 0x83:
		switch op {
		case 0x80, 0x82, 0x83:
			return []byte{op, 0xC0, 0x00}
		case 0x81:
			return []byte{op, 0xC0, 0x00, 0x00, 0x00, 0x00}
		}
	// TEST r/m, r — ModR/M
	case op == 0x84 || op == 0x85 || op == 0x86 || op == 0x87:
		return []byte{op, 0xC0}
	// MOV r/m, r / r, r/m — ModR/M
	case op >= 0x88 && op <= 0x8F:
		if op == 0x8F {
			// POP r/m64 - need ModR/M
			return []byte{op, 0xC0}
		}
		return []byte{op, 0xC0}
	// IMUL imm — 0x68/0x69/0x6A/0x6B
	case op == 0x68:
		return []byte{op, 0x00, 0x00, 0x00, 0x00}
	case op == 0x69:
		return []byte{op, 0xC0, 0x00, 0x00, 0x00, 0x00}
	case op == 0x6A:
		return []byte{op, 0x00}
	case op == 0x6B:
		return []byte{op, 0xC0, 0x00}
	// MOVSXD r64, r/m32
	case op == 0x63:
		return []byte{op, 0xC0}
	// Group 2 shifts by imm/CL
	case op == 0xC0 || op == 0xC1:
		return []byte{op, 0xE0, 0x01} // /4 = SHL, imm8=1
	case op >= 0xD0 && op <= 0xD3:
		return []byte{op, 0xE0}
	// RET / RETN imm16 / RETF / RETF imm16
	case op == 0xC2 || op == 0xCA:
		return []byte{op, 0x00, 0x00}
	case op == 0xC3 || op == 0xCB:
		return []byte{op}
	// Single-byte no-operand ops
	case op == 0x90 || op == 0x98 || op == 0x99 ||
		op == 0x9C || op == 0x9D || op == 0x9E || op == 0x9F ||
		op == 0x9B ||
		op == 0xC9 || op == 0xCC || op == 0xCE || op == 0xCF ||
		op == 0xF4 || op == 0xF5 || op == 0xF8 || op == 0xF9 ||
		op == 0xFA || op == 0xFB || op == 0xFC || op == 0xFD:
		return []byte{op}
	// XCHG rAX, rN — 0x91-0x97 (0x90 is NOP / XCHG RAX,RAX)
	case op >= 0x91 && op <= 0x97:
		return []byte{op}
	// MOV moffs8/16/32/64 (0xA0-0xA3) — abs address, 8 bytes in 64-bit mode
	case op >= 0xA0 && op <= 0xA3:
		return []byte{op, 0, 0, 0, 0, 0, 0, 0, 0}
	// Group 3 (TEST/NOT/NEG/MUL/IMUL/DIV/IDIV)
	case op == 0xF6:
		return []byte{op, 0xD0} // /2 = NOT
	case op == 0xF7:
		return []byte{op, 0xD0}
	// Group 4 (INC/DEC byte) — 0xFE
	case op == 0xFE:
		return []byte{op, 0xC0}
	// Group 5 (INC/DEC/CALL/JMP/PUSH) — 0xFF
	case op == 0xFF:
		return []byte{op, 0xC0} // /0 = INC r/m
	// INT3 (0xCC) already handled above
	}
	// Default: just the opcode byte. Won't be valid for all but tests
	// the dispatcher's recognition.
	return []byte{op}
}

// encodeTwoByte builds an encoding for 0x0F XX. Same approach as
// encodeSingleByte but for the escape.
func encodeTwoByte(op2 byte) []byte {
	switch {
	// SYSCALL, SYSRET, RDTSC etc. — no operand
	case op2 == 0x05 || op2 == 0x07 || op2 == 0x09 || op2 == 0x0B ||
		op2 == 0x30 || op2 == 0x31 || op2 == 0x32 || op2 == 0x33 ||
		op2 == 0x34 || op2 == 0x35 || op2 == 0x77 || op2 == 0xA2:
		return []byte{0x0F, op2}
	// Group 6 / 7 / 15 — ModR/M
	case op2 == 0x00 || op2 == 0x01 || op2 == 0xAE:
		return []byte{0x0F, op2, 0xC0}
	// Multi-byte NOP / prefetch (0F 18-1F) — ModR/M but memory-only forms
	case op2 >= 0x18 && op2 <= 0x1F:
		// Use a register-form ModR/M; impl might handle or skip
		return []byte{0x0F, op2, 0xC0}
	// MOV CR/DR — ModR/M with reg-form only
	case op2 >= 0x20 && op2 <= 0x23:
		return []byte{0x0F, op2, 0xC0}
	// CMOVcc — 0x40-0x4F (ModR/M)
	case op2 >= 0x40 && op2 <= 0x4F:
		return []byte{0x0F, op2, 0xC0}
	// Jcc near (0x80-0x8F) — disp32
	case op2 >= 0x80 && op2 <= 0x8F:
		return []byte{0x0F, op2, 0x00, 0x00, 0x00, 0x00}
	// SETcc (0x90-0x9F) — ModR/M
	case op2 >= 0x90 && op2 <= 0x9F:
		return []byte{0x0F, op2, 0xC0}
	// PUSH/POP FS/GS (0xA0/A1/A8/A9), shifts (0xA4/A5/AC/AD), bit-test (0xA3/AB/BA/BB)
	case op2 == 0xA0 || op2 == 0xA1 || op2 == 0xA8 || op2 == 0xA9:
		return []byte{0x0F, op2}
	case op2 == 0xA3 || op2 == 0xAB || op2 == 0xBB:
		return []byte{0x0F, op2, 0xC0}
	case op2 == 0xA4 || op2 == 0xAC:
		return []byte{0x0F, op2, 0xC0, 0x00}
	case op2 == 0xA5 || op2 == 0xAD:
		return []byte{0x0F, op2, 0xC0}
	case op2 == 0xBA:
		return []byte{0x0F, op2, 0xE0, 0x00} // /4 = BT
	// CMPXCHG (0xB0/B1), MOVZX (0xB6/B7), MOVSX (0xBE/BF), BSF/BSR (0xBC/BD)
	case op2 == 0xB0 || op2 == 0xB1 || op2 == 0xB6 || op2 == 0xB7 ||
		op2 == 0xBE || op2 == 0xBF || op2 == 0xBC || op2 == 0xBD ||
		op2 == 0xB2 || op2 == 0xB3 || op2 == 0xB4 || op2 == 0xB5 ||
		op2 == 0xB8 || op2 == 0xB9:
		return []byte{0x0F, op2, 0xC0}
	// XADD (0xC0/C1), CMPXCHG8B/16B (0xC7 group 9), SHUFPS (0xC6)
	case op2 == 0xC0 || op2 == 0xC1:
		return []byte{0x0F, op2, 0xC0}
	case op2 == 0xC2:
		return []byte{0x0F, op2, 0xC0, 0x00}
	case op2 == 0xC6:
		return []byte{0x0F, op2, 0xC0, 0x00}
	case op2 == 0xC7:
		return []byte{0x0F, op2, 0x08} // /1 = CMPXCHG8B, memory required
	// BSWAP (0xC8-0xCF) — register form
	case op2 >= 0xC8 && op2 <= 0xCF:
		return []byte{0x0F, op2}
	// SSE/MMX/SSE2 — vast majority use ModR/M
	case op2 >= 0x10 && op2 <= 0x17:
		return []byte{0x0F, op2, 0xC0}
	case op2 == 0x28 || op2 == 0x29 || op2 == 0x2A || op2 == 0x2B ||
		op2 == 0x2C || op2 == 0x2D || op2 == 0x2E || op2 == 0x2F:
		return []byte{0x0F, op2, 0xC0}
	case op2 >= 0x50 && op2 <= 0x5F:
		return []byte{0x0F, op2, 0xC0}
	case op2 >= 0x60 && op2 <= 0x7F:
		// Most are ModR/M; 0x70 / 0x71 / 0x72 / 0x73 take an extra imm8
		switch op2 {
		case 0x70, 0x71, 0x72, 0x73:
			return []byte{0x0F, op2, 0xC0, 0x00}
		}
		return []byte{0x0F, op2, 0xC0}
	case op2 >= 0xD0 && op2 <= 0xFF:
		return []byte{0x0F, op2, 0xC0}
	}
	return []byte{0x0F, op2, 0xC0}
}

// mustBeWired lists opcodes we consider critical for general userspace.
// If any of these comes back UNIMPL or OTHER, the test fails.
var mustBeWiredSingle = []byte{
	// ALU r/m, r forms (each op has 0..5 sub-encodings)
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05,
	0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15,
	0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D,
	0x20, 0x21, 0x22, 0x23, 0x24, 0x25,
	0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35,
	0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D,
	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57,
	0x58, 0x59, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F,
	0x63, 0x68, 0x69, 0x6A, 0x6B,
	0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
	0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F,
	0x80, 0x81, 0x83,
	0x84, 0x85, 0x86, 0x87,
	0x88, 0x89, 0x8A, 0x8B, 0x8D,
	0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97,
	0x98, 0x99,
	0xA4, 0xA5, 0xA8, 0xA9, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF,
	0xB0, 0xB1, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7,
	0xB8, 0xB9, 0xBA, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF,
	0xC1, 0xC3, 0xC6, 0xC7, 0xC9, 0xCB, 0xCD,
	0xD0, 0xD1, 0xD2, 0xD3,
	0xE8, 0xE9, 0xEB,
	0xF4, 0xF6, 0xF7, 0xFC, 0xFD, 0xFE, 0xFF,
}

var mustBeWiredTwoByte = []byte{
	0x05, 0x07,             // SYSCALL, SYSRET
	0x1F,                   // multibyte NOP
	0x20, 0x21, 0x22, 0x23, // MOV CR/DR
	0x30, 0x31, 0x32,       // WRMSR, RDTSC, RDMSR
	0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
	0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F, // CMOVcc
	0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
	0x88, 0x89, 0x8A, 0x8B, 0x8C, 0x8D, 0x8E, 0x8F, // Jcc near
	0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97,
	0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F, // SETcc
	0xA2, 0xA3, 0xA4, 0xA5, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF,
	0xB0, 0xB1, 0xB3, 0xB6, 0xB7, 0xBA, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF,
	0xC0, 0xC1, 0xC8, 0xC9, 0xCA, 0xCB, 0xCC, 0xCD, 0xCE, 0xCF, // XADD, BSWAP
	// Critical SSE-FP we added today
	0x2A, 0x2C, 0x2D, 0x51, 0x58, 0x59, 0x5A, 0x5C, 0x5D, 0x5E, 0x5F, 0xC2, 0xC6,
	// Common SSE2 moves/arith
	0x10, 0x11, 0x28, 0x29, 0x6E, 0x6F, 0x7E, 0x7F,
	0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x6C, 0x6D,
	0x70, 0x74, 0x75, 0x76,
	0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xDB, 0xDF,
	0xE7, 0xEB, 0xEF, 0xF4, 0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD, 0xFE,
}

// Opcodes we deliberately don't implement; informational only.
var expectedUnimpl = map[byte]string{}

func TestOpcodeSweep(t *testing.T) {
	type row struct {
		op  uint16
		res sweepResult
		err string
	}

	var rows []row
	// Single-byte sweep
	for op := 0; op <= 0xFF; op++ {
		bytes := encodeSingleByte(byte(op))
		if bytes == nil {
			// e.g. 0x0F is handled below as escape; skip
			continue
		}
		// Treat prefixes as covered (Step's prefix loop accepts them,
		// then we land on the appended HLT).
		if (op == 0x26 || op == 0x2E || op == 0x36 || op == 0x3E ||
			op == 0x64 || op == 0x65 || op == 0x66 || op == 0x67 ||
			op == 0xF0 || op == 0xF2 || op == 0xF3 ||
			(op >= 0x40 && op <= 0x4F)) {
			rows = append(rows, row{uint16(op), resultPrefix, ""})
			continue
		}
		res, msg := trySingle(t, bytes)
		rows = append(rows, row{uint16(op), res, msg})
	}
	// Two-byte sweep (0x0F XX)
	for op2 := 0; op2 <= 0xFF; op2++ {
		bytes := encodeTwoByte(byte(op2))
		res, msg := trySingle(t, bytes)
		rows = append(rows, row{0x0F00 | uint16(op2), res, msg})
	}

	// Print the matrix
	if testing.Verbose() {
		t.Log("Opcode sweep — UNIMPL = must-add, OTHER = check encoding, OK = wired, PREFIX = prefix")
		for _, r := range rows {
			if r.res == resultOK || r.res == resultPrefix {
				continue
			}
			label := fmt.Sprintf("%02X", r.op)
			if r.op > 0xFF {
				label = fmt.Sprintf("0F %02X", r.op&0xFF)
			}
			t.Logf("  %s  %s  %s", label, r.res, truncate(r.err, 100))
		}
	}

	// Aggregate counts
	counts := map[sweepResult]int{}
	for _, r := range rows {
		counts[r.res]++
	}
	t.Logf("Coverage: PREFIX=%d  OK=%d  UNIMPL=%d  OTHER=%d  (total %d)",
		counts[resultPrefix], counts[resultOK], counts[resultUnimpl], counts[resultOther], len(rows))

	// List unimplemented ops for one-glance inventory
	var unimplSingle, unimplTwo []string
	for _, r := range rows {
		if r.res != resultUnimpl {
			continue
		}
		if r.op > 0xFF {
			unimplTwo = append(unimplTwo, fmt.Sprintf("%02X", r.op&0xFF))
		} else {
			unimplSingle = append(unimplSingle, fmt.Sprintf("%02X", r.op))
		}
	}
	sort.Strings(unimplSingle)
	sort.Strings(unimplTwo)
	if len(unimplSingle) > 0 {
		t.Logf("Unimplemented single-byte: %s", strings.Join(unimplSingle, " "))
	}
	if len(unimplTwo) > 0 {
		t.Logf("Unimplemented 0F XX:       %s", strings.Join(unimplTwo, " "))
	}

	// FAIL if any must-be-wired opcode is UNIMPL or OTHER
	resByOp := make(map[uint16]sweepResult, len(rows))
	for _, r := range rows {
		resByOp[r.op] = r.res
	}
	for _, op := range mustBeWiredSingle {
		res := resByOp[uint16(op)]
		if res != resultOK && res != resultPrefix {
			t.Errorf("must-be-wired single-byte 0x%02X: got %s", op, res)
		}
	}
	for _, op2 := range mustBeWiredTwoByte {
		res := resByOp[0x0F00|uint16(op2)]
		if res != resultOK && res != resultPrefix {
			t.Errorf("must-be-wired two-byte 0x0F 0x%02X: got %s", op2, res)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
