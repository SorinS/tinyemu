package arm64

import (
	"fmt"
	"strings"
)

// Disassemble decodes one 32-bit AArch64 instruction word to its textual form.
// It covers exactly the classes the assembler encodes (the first integer
// slice), so a decode round-trips through Assemble. PC-relative branch targets
// are rendered as a signed byte offset "#n" (origin-independent — the form the
// assembler re-encodes identically); a symbol-aware variant can come later.
func Disassemble(w uint32) (string, error) {
	switch {
	case (w>>23)&0x3F == 0x22: // add/sub immediate
		return disAddSubImm(w), nil
	case (w>>23)&0x3F == 0x24: // logical immediate (not in the assembler slice yet)
		return "", fmt.Errorf("arm64 disasm: logical-immediate not yet supported (%08x)", w)
	case (w>>23)&0x3F == 0x25: // move wide
		return disMoveWide(w)
	case (w>>24)&0x1F == 0x0B: // add/sub register (shifted or extended)
		return disAddSubReg(w), nil
	case (w>>24)&0x1F == 0x0A: // logical shifted register
		return disLogicalReg(w), nil
	case (w>>24)&0x3F == 0x39: // load/store register, unsigned offset
		return disLoadStore(w)
	case (w>>26)&0x1F == 0x05: // unconditional branch immediate (b/bl)
		return disBranchImm(w), nil
	case (w>>24)&0xFF == 0x54: // conditional branch
		return disBranchCond(w), nil
	case (w>>25)&0x3F == 0x1A: // compare and branch (cbz/cbnz)
		return disCompareBranch(w), nil
	case (w>>25)&0x7F == 0x6B: // unconditional branch register (br/blr/ret)
		return disBranchReg(w)
	}
	return "", fmt.Errorf("arm64 disasm: unknown encoding %08x", w)
}

// rname formats register number n. is64 picks x/w; sp picks sp/wsp vs xzr/wzr
// for field value 31.
func rname(n uint32, is64, sp bool) string {
	if n == 31 {
		switch {
		case sp && is64:
			return "sp"
		case sp:
			return "wsp"
		case is64:
			return "xzr"
		default:
			return "wzr"
		}
	}
	if is64 {
		return fmt.Sprintf("x%d", n)
	}
	return fmt.Sprintf("w%d", n)
}

var condNames = [16]string{
	"eq", "ne", "cs", "cc", "mi", "pl", "vs", "vc",
	"hi", "ls", "ge", "lt", "gt", "le", "al", "nv",
}

var shiftNames = [4]string{"lsl", "lsr", "asr", "ror"}

// signExtend sign-extends the low `bits` of v.
func signExtend(v uint32, bits int) int64 {
	shift := 32 - bits
	return int64(int32(v<<uint(shift)) >> uint(shift))
}

func disAddSubImm(w uint32) string {
	sf := (w>>31)&1 == 1
	op := (w >> 30) & 1
	s := (w >> 29) & 1
	sh := (w >> 22) & 1
	imm12 := (w >> 10) & 0xFFF
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	mnem := [...]string{"add", "adds", "sub", "subs"}[op<<1|s]
	// Rd is SP for add/sub (S=0), XZR for adds/subs (S=1); Rn is always SP.
	rdName := rname(rd, sf, s == 0)
	imm := fmt.Sprintf("#%d", imm12) // add/sub immediates render in decimal (llvm/objdump convention)
	if sh == 1 {
		imm += ", lsl #12"
	}
	return fmt.Sprintf("%s %s, %s, %s", mnem, rdName, rname(rn, sf, true), imm)
}

func disAddSubReg(w uint32) string {
	sf := (w>>31)&1 == 1
	op := (w >> 30) & 1
	s := (w >> 29) & 1
	rm := (w >> 16) & 0x1F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	mnem := [...]string{"add", "adds", "sub", "subs"}[op<<1|s]
	if (w>>21)&1 == 1 { // extended register — our SP form (option = LSL)
		imm3 := (w >> 10) & 7
		out := fmt.Sprintf("%s %s, %s, %s", mnem,
			rname(rd, sf, true), rname(rn, sf, true), rname(rm, sf, false))
		if imm3 != 0 {
			out += fmt.Sprintf(", lsl #%d", imm3)
		}
		return out
	}
	shift := (w >> 22) & 3
	imm6 := (w >> 10) & 0x3F
	out := fmt.Sprintf("%s %s, %s, %s", mnem,
		rname(rd, sf, false), rname(rn, sf, false), rname(rm, sf, false))
	if imm6 != 0 {
		out += fmt.Sprintf(", %s #%d", shiftNames[shift], imm6)
	}
	return out
}

func disLogicalReg(w uint32) string {
	sf := (w>>31)&1 == 1
	opc := (w >> 29) & 3
	n := (w >> 21) & 1
	shift := (w >> 22) & 3
	rm := (w >> 16) & 0x1F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	imm6 := (w >> 10) & 0x3F
	names := [...]string{"and", "orr", "eor", "ands", "bic", "orn", "eon", "bics"}
	mnem := names[opc+n*4]
	out := fmt.Sprintf("%s %s, %s, %s", mnem,
		rname(rd, sf, false), rname(rn, sf, false), rname(rm, sf, false))
	if imm6 != 0 {
		out += fmt.Sprintf(", %s #%d", shiftNames[shift], imm6)
	}
	return out
}

func disMoveWide(w uint32) (string, error) {
	sf := (w>>31)&1 == 1
	opc := (w >> 29) & 3
	hw := (w >> 21) & 3
	imm16 := (w >> 5) & 0xFFFF
	rd := w & 0x1F
	var mnem string
	switch opc {
	case 0:
		mnem = "movn"
	case 2:
		mnem = "movz"
	case 3:
		mnem = "movk"
	default:
		return "", fmt.Errorf("arm64 disasm: bad move-wide opc %08x", w)
	}
	out := fmt.Sprintf("%s %s, #%#x", mnem, rname(rd, sf, false), imm16)
	if hw != 0 {
		out += fmt.Sprintf(", lsl #%d", hw*16)
	}
	return out, nil
}

func disLoadStore(w uint32) (string, error) {
	size := (w >> 30) & 3
	opc := (w >> 22) & 3
	imm12 := (w >> 10) & 0xFFF
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	if size != 2 && size != 3 {
		return "", fmt.Errorf("arm64 disasm: load/store size %d not in slice (%08x)", size, w)
	}
	is64 := size == 3
	var mnem string
	switch opc {
	case 0:
		mnem = "str"
	case 1:
		mnem = "ldr"
	default:
		return "", fmt.Errorf("arm64 disasm: load/store opc %d not in slice (%08x)", opc, w)
	}
	scale := uint32(4)
	if is64 {
		scale = 8
	}
	mem := fmt.Sprintf("[%s]", rname(rn, true, true))
	if imm12 != 0 {
		mem = fmt.Sprintf("[%s, #%d]", rname(rn, true, true), imm12*scale) // byte offset, decimal
	}
	return fmt.Sprintf("%s %s, %s", mnem, rname(rt, is64, false), mem), nil
}

func disBranchImm(w uint32) string {
	off := signExtend(w&0x3FFFFFF, 26) << 2
	if (w>>31)&1 == 1 {
		return fmt.Sprintf("bl #%d", off)
	}
	return fmt.Sprintf("b #%d", off)
}

func disBranchCond(w uint32) string {
	cond := condNames[w&0xF]
	off := signExtend((w>>5)&0x7FFFF, 19) << 2
	return fmt.Sprintf("b.%s #%d", cond, off)
}

func disCompareBranch(w uint32) string {
	sf := (w>>31)&1 == 1
	mnem := "cbz"
	if (w>>24)&1 == 1 {
		mnem = "cbnz"
	}
	rt := w & 0x1F
	off := signExtend((w>>5)&0x7FFFF, 19) << 2
	return fmt.Sprintf("%s %s, #%d", mnem, rname(rt, sf, false), off)
}

func disBranchReg(w uint32) (string, error) {
	rn := (w >> 5) & 0x1F
	switch (w >> 21) & 0xF {
	case 0:
		return fmt.Sprintf("br %s", rname(rn, true, false)), nil
	case 1:
		return fmt.Sprintf("blr %s", rname(rn, true, false)), nil
	case 2:
		if rn == 30 {
			return "ret", nil
		}
		return fmt.Sprintf("ret %s", rname(rn, true, false)), nil
	}
	return "", fmt.Errorf("arm64 disasm: bad branch-register %08x", w)
}

// DisassembleBytes decodes 4 little-endian bytes to instruction text.
func DisassembleBytes(b []byte) (string, error) {
	if len(b) != 4 {
		return "", fmt.Errorf("arm64 disasm: need 4 bytes, got %d", len(b))
	}
	w := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return Disassemble(w)
}

// normalizeAsm canonicalizes assembly text for comparison: lower-case, single
// spaces, no space after commas collapsed to ", ".
func normalizeAsm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.ReplaceAll(s, " ,", ",")
	s = strings.ReplaceAll(s, ",", ", ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}
