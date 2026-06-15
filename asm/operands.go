package asm

import (
	"strconv"
	"strings"
)

// opKind classifies a parsed source operand.
type opKind int

const (
	opNone   opKind = iota
	opReg           // a general-purpose register
	opImm           // an integer immediate
	opMem           // a memory reference
	opTarget        // a relative branch target (imm holds the displacement)
)

// operand is a parsed source operand.
type operand struct {
	kind     opKind
	reg      int   // register number 0..15 (opReg)
	size     int   // register width in bits: 8/16/32/64 (opReg)
	highByte bool  // ah/ch/dh/bh — legacy high-byte registers (no REX)
	needRex  bool  // spl/bpl/sil/dil or r8..r15 — presence forces/uses REX
	imm      int64 // immediate value (opImm)

	// Memory reference (opMem): [base + index*scale + disp].
	memBase    int   // base register 0..15, or -1 for none
	memIndex   int   // index register 0..15, or -1 for none
	memScale   int   // 1/2/4/8
	memDisp    int64 // displacement
	memHasDisp bool
	memSize    int    // operand size from a size keyword (byte/word/…); 0 if absent
	memRip     bool   // RIP-relative ([rel …] / [rip+…])
	memSym     string // unresolved symbol in the address ([rel arr], [arr+…]); "" if none
	baseRex    bool   // base is r8..r15
	indexRex   bool   // index is r8..r15
	baseSize   int    // address-register width (32 → needs 67 prefix; default 64)
}

// gpr is a general-purpose register's encoding facts.
type gpr struct {
	num      int
	size     int
	highByte bool
	needRex  bool
}

var gprByName = buildGPRTable()

func buildGPRTable() map[string]gpr {
	m := make(map[string]gpr, 80)
	add := func(name string, num, size int, high, rex bool) { m[name] = gpr{num, size, high, rex} }
	for i, n := range []string{"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi"} {
		add(n, i, 64, false, false)
	}
	for i, n := range []string{"eax", "ecx", "edx", "ebx", "esp", "ebp", "esi", "edi"} {
		add(n, i, 32, false, false)
	}
	for i, n := range []string{"ax", "cx", "dx", "bx", "sp", "bp", "si", "di"} {
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

// sizeKeywords maps NASM operand-size keywords to bit widths.
var sizeKeywords = []struct {
	word string
	bits int
}{
	{"byte", 8}, {"word", 16}, {"dword", 32}, {"qword", 64},
	{"tword", 80}, {"oword", 128}, {"yword", 256}, {"zword", 512},
}

// parseOperand classifies one source operand string.
func parseOperand(s string) (operand, bool) {
	s = strings.TrimSpace(s)
	size := 0
	low := strings.ToLower(s)
	for _, kw := range sizeKeywords {
		if strings.HasPrefix(low, kw.word+" ") || strings.HasPrefix(low, kw.word+"[") {
			size = kw.bits
			s = strings.TrimSpace(s[len(kw.word):])
			break
		}
	}
	// Accept the MASM-style "ptr" keyword (as emitted by x/arch's Intel
	// syntax: "qword ptr [rax]", "ptr [rax]") — treat it as a no-op.
	if l := strings.ToLower(s); strings.HasPrefix(l, "ptr ") || strings.HasPrefix(l, "ptr[") {
		s = strings.TrimSpace(s[3:])
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return parseMem(s[1:len(s)-1], size)
	}
	if g, ok := gprByName[strings.ToLower(s)]; ok {
		return operand{kind: opReg, reg: g.num, size: g.size, highByte: g.highByte, needRex: g.needRex}, true
	}
	if v, ok := parseImm(s); ok {
		return operand{kind: opImm, imm: v}, true
	}
	return operand{}, false
}

// parseMem parses the inside of a memory reference: base + index*scale + disp.
func parseMem(inner string, size int) (operand, bool) {
	op := operand{kind: opMem, memSize: size, memBase: -1, memIndex: -1, memScale: 1, baseSize: 64}
	inner = strings.TrimSpace(inner)
	// NASM keywords: [rel sym] is RIP-relative, [abs sym] is the (default) absolute.
	if rest, ok := cutWord(inner, "rel"); ok {
		op.memRip, inner = true, rest
	} else if rest, ok := cutWord(inner, "abs"); ok {
		inner = rest
	}
	inner = strings.ReplaceAll(inner, "-", "+-") // keep signs when splitting on '+'
	for _, term := range strings.Split(inner, "+") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		neg := false
		if strings.HasPrefix(term, "-") {
			neg, term = true, strings.TrimSpace(term[1:])
		}
		if star := strings.IndexByte(term, '*'); star >= 0 { // index*scale
			rname := strings.ToLower(strings.TrimSpace(term[:star]))
			scale, err := strconv.Atoi(strings.TrimSpace(term[star+1:]))
			g, ok := gprByName[rname]
			if !ok || err != nil || (scale != 1 && scale != 2 && scale != 4 && scale != 8) {
				return op, false
			}
			op.memIndex, op.memScale, op.indexRex, op.baseSize = g.num, scale, g.needRex, g.size
			continue
		}
		lower := strings.ToLower(term)
		if lower == "rip" {
			op.memRip = true
			continue
		}
		if g, ok := gprByName[lower]; ok {
			switch {
			case op.memBase == -1:
				op.memBase, op.baseRex, op.baseSize = g.num, g.needRex, g.size
			case op.memIndex == -1:
				op.memIndex, op.memScale, op.indexRex = g.num, 1, g.needRex
			default:
				return op, false
			}
			continue
		}
		if v, ok := parseImm(lower); ok {
			if neg {
				v = -v
			}
			op.memDisp += v
			op.memHasDisp = true
			continue
		}
		// A bare identifier is a symbol reference (resolved at program assembly
		// against the label table). At most one per address.
		if op.memSym != "" || neg || !isLabelName(term) {
			return op, false
		}
		op.memSym = term
	}
	return op, true
}

// cutWord removes a leading space-separated keyword (case-insensitive) from s,
// returning the remainder and whether the keyword was present.
func cutWord(s, word string) (string, bool) {
	if len(s) > len(word) && strings.EqualFold(s[:len(word)], word) &&
		(s[len(word)] == ' ' || s[len(word)] == '\t') {
		return strings.TrimSpace(s[len(word)+1:]), true
	}
	return s, false
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
		if v, err = strconv.ParseInt(s, 10, 64); err != nil {
			return 0, false
		}
	}
	if neg {
		v = -v
	}
	return v, true
}

func fitsSigned(v int64, bits int) bool {
	if bits >= 64 {
		return true
	}
	return v >= int64(-1)<<(bits-1) && v <= int64(1)<<(bits-1)-1
}

func fitsImm(v int64, bits int) bool {
	if bits >= 64 {
		return true
	}
	return v >= int64(-1)<<(bits-1) && v <= int64(1)<<bits-1
}
