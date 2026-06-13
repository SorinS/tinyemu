package asm

import (
	"strconv"
	"strings"
)

// opKind classifies a parsed source operand.
type opKind int

const (
	opNone opKind = iota
	opReg         // a general-purpose register
	opImm         // an integer immediate
)

// operand is a parsed source operand. (Memory operands arrive in a later
// slice; for now: registers and immediates.)
type operand struct {
	kind     opKind
	reg      int   // register number 0..15
	size     int   // register width in bits: 8/16/32/64
	highByte bool  // ah/ch/dh/bh — the legacy high-byte registers (no REX)
	needRex  bool  // spl/bpl/sil/dil or r8..r15 — presence forces/uses REX
	imm      int64 // immediate value
}

// gpr is a general-purpose register's encoding facts.
type gpr struct {
	num      int
	size     int
	highByte bool
	needRex  bool
}

// gprByName maps every GPR spelling to its encoding facts.
var gprByName = buildGPRTable()

func buildGPRTable() map[string]gpr {
	m := make(map[string]gpr, 80)
	add := func(name string, num, size int, high, rex bool) {
		m[name] = gpr{num, size, high, rex}
	}
	n64 := []string{"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi"}
	n32 := []string{"eax", "ecx", "edx", "ebx", "esp", "ebp", "esi", "edi"}
	n16 := []string{"ax", "cx", "dx", "bx", "sp", "bp", "si", "di"}
	for i, n := range n64 {
		add(n, i, 64, false, false)
	}
	for i, n := range n32 {
		add(n, i, 32, false, false)
	}
	for i, n := range n16 {
		add(n, i, 16, false, false)
	}
	for i, n := range []string{"al", "cl", "dl", "bl"} {
		add(n, i, 8, false, false)
	}
	for i, n := range []string{"ah", "ch", "dh", "bh"} {
		add(n, i+4, 8, true, false)
	}
	for i, n := range []string{"spl", "bpl", "sil", "dil"} {
		add(n, i+4, 8, false, true)
	}
	for i := 8; i <= 15; i++ {
		num := strconv.Itoa(i)
		add("r"+num, i, 64, false, true)
		add("r"+num+"d", i, 32, false, true)
		add("r"+num+"w", i, 16, false, true)
		add("r"+num+"b", i, 8, false, true)
	}
	return m
}

// parseOperand classifies one source operand string.
func parseOperand(s string) (operand, bool) {
	s = strings.TrimSpace(s)
	if g, ok := gprByName[strings.ToLower(s)]; ok {
		return operand{kind: opReg, reg: g.num, size: g.size, highByte: g.highByte, needRex: g.needRex}, true
	}
	if v, ok := parseImm(s); ok {
		return operand{kind: opImm, imm: v}, true
	}
	return operand{}, false
}

// parseImm parses a decimal or 0x-hex integer literal with optional sign.
func parseImm(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	var v int64
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		u, err := strconv.ParseUint(s[2:], 16, 64)
		if err != nil {
			return 0, false
		}
		v = int64(u)
	} else {
		var err error
		v, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	if neg {
		v = -v
	}
	return v, true
}

// fitsSigned reports whether v fits in a signed n-bit field.
func fitsSigned(v int64, bits int) bool {
	if bits >= 64 {
		return true
	}
	lo := int64(-1) << (bits - 1)
	hi := int64(1)<<(bits-1) - 1
	return v >= lo && v <= hi
}

// fitsUnsigned reports whether v fits in an n-bit field (signed or unsigned).
func fitsImm(v int64, bits int) bool {
	if bits >= 64 {
		return true
	}
	hi := int64(1)<<bits - 1
	lo := int64(-1) << (bits - 1)
	return v >= lo && v <= hi
}
