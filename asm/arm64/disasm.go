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
	case (w>>23)&0x3F == 0x24: // logical immediate
		return disLogicalImm(w)
	case (w>>23)&0x3F == 0x25: // move wide
		return disMoveWide(w)
	case (w>>24)&0x1F == 0x0B: // add/sub register (shifted or extended)
		return disAddSubReg(w), nil
	case (w>>24)&0x1F == 0x0A: // logical shifted register
		return disLogicalReg(w), nil
	case (w>>24)&0x3F == 0x39 || (w>>24)&0x3F == 0x38: // load/store register
		return disLoadStore(w)
	case (w>>25)&0x1F == 0x14: // load/store pair
		return disPair(w)
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

func disLogicalImm(w uint32) (string, error) {
	sf := (w>>31)&1 == 1
	opc := (w >> 29) & 3
	n := (w >> 22) & 1
	immr := (w >> 16) & 0x3F
	imms := (w >> 10) & 0x3F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	regSize := 32
	if sf {
		regSize = 64
	}
	val, ok := decodeBitmask(n, imms, immr, regSize)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: invalid logical immediate %08x", w)
	}
	mnem := [...]string{"and", "orr", "eor", "ands"}[opc]
	// Rd is SP for and/orr/eor, XZR for ands; Rn is always the zero register.
	return fmt.Sprintf("%s %s, %s, #%#x", mnem,
		rname(rd, sf, opc != 3), rname(rn, sf, false), val), nil
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

// lsSuffix reverses lsForm: from (size, opc) it gives the mnemonic suffix
// (b/h/sb/sh/sw/""), the data-register width, and whether it is a store.
func lsSuffix(size, opc uint32) (suffix string, rt64, store, ok bool) {
	switch opc {
	case 0: // store
		switch size {
		case 0:
			return "b", false, true, true
		case 1:
			return "h", false, true, true
		case 2:
			return "", false, true, true
		case 3:
			return "", true, true, true
		}
	case 1: // load, zero-extend
		switch size {
		case 0:
			return "b", false, false, true
		case 1:
			return "h", false, false, true
		case 2:
			return "", false, false, true
		case 3:
			return "", true, false, true
		}
	case 2: // load, sign-extend to 64
		switch size {
		case 0:
			return "sb", true, false, true
		case 1:
			return "sh", true, false, true
		case 2:
			return "sw", true, false, true
		}
	case 3: // load, sign-extend to 32
		switch size {
		case 0:
			return "sb", false, false, true
		case 1:
			return "sh", false, false, true
		}
	}
	return "", false, false, false
}

// lsName builds a load/store mnemonic from the store flag and the suffix.
func lsName(store bool, prefixLoad, prefixStore, suffix string) string {
	if store {
		return prefixStore + suffix
	}
	return prefixLoad + suffix
}

func disLoadStore(w uint32) (string, error) {
	size := (w >> 30) & 3
	opc := (w >> 22) & 3
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	suffix, rt64, store, ok := lsSuffix(size, opc)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: load/store size/opc %d/%d not in slice (%08x)", size, opc, w)
	}
	rtName := rname(rt, rt64, false)
	base := rname(rn, true, true)

	if (w>>24)&1 == 1 { // bits[25:24]=01 → unsigned offset
		imm12 := (w >> 10) & 0xFFF
		mem := fmt.Sprintf("[%s]", base)
		if imm12 != 0 {
			mem = fmt.Sprintf("[%s, #%d]", base, imm12*(uint32(1)<<size))
		}
		return fmt.Sprintf("%s %s, %s", lsName(store, "ldr", "str", suffix), rtName, mem), nil
	}
	// bits[25:24]=00 → register offset, or imm9 (unscaled / pre / post)
	if (w>>21)&1 == 1 && (w>>10)&3 == 0b10 {
		return disLoadStoreReg(w, store, suffix, rtName, base, size)
	}
	imm9 := signExtend((w>>12)&0x1FF, 9)
	switch (w >> 10) & 3 {
	case 0b00: // unscaled (stur/ldur)
		mem := fmt.Sprintf("[%s]", base)
		if imm9 != 0 {
			mem = fmt.Sprintf("[%s, #%d]", base, imm9)
		}
		return fmt.Sprintf("%s %s, %s", lsName(store, "ldur", "stur", suffix), rtName, mem), nil
	case 0b01: // post-index
		return fmt.Sprintf("%s %s, [%s], #%d", lsName(store, "ldr", "str", suffix), rtName, base, imm9), nil
	case 0b11: // pre-index
		return fmt.Sprintf("%s %s, [%s, #%d]!", lsName(store, "ldr", "str", suffix), rtName, base, imm9), nil
	}
	return "", fmt.Errorf("arm64 disasm: load/store form %08x", w)
}

func disLoadStoreReg(w uint32, store bool, suffix, rtName, base string, size uint32) (string, error) {
	rm := (w >> 16) & 0x1F
	option := (w >> 13) & 7
	s := (w >> 12) & 1
	idx := rname(rm, option&1 == 1, false) // option[0]=1 ⇒ 64-bit index
	var ext string
	switch option {
	case 0b011: // lsl / uxtx — rendered as lsl, omitted entirely when S=0
		if s == 1 {
			ext = fmt.Sprintf(", lsl #%d", size)
		}
	case 0b010:
		ext = ", uxtw"
	case 0b110:
		ext = ", sxtw"
	case 0b111:
		ext = ", sxtx"
	default:
		return "", fmt.Errorf("arm64 disasm: bad index extend %d (%08x)", option, w)
	}
	if s == 1 && option != 0b011 {
		ext += fmt.Sprintf(" #%d", size)
	}
	return fmt.Sprintf("%s %s, [%s, %s%s]", lsName(store, "ldr", "str", suffix), rtName, base, idx, ext), nil
}

func disPair(w uint32) (string, error) {
	opc := (w >> 30) & 3
	l := (w >> 22) & 1
	imm7 := signExtend((w>>15)&0x7F, 7)
	rt2 := (w >> 10) & 0x1F
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	var mnem string
	var is64 bool
	var scale int64
	switch opc {
	case 0:
		mnem, is64, scale = lsName(l == 0, "ldp", "stp", ""), false, 4
	case 1:
		if l == 0 {
			return "", fmt.Errorf("arm64 disasm: STGP/unsupported pair %08x", w)
		}
		mnem, is64, scale = "ldpsw", true, 4
	case 2:
		mnem, is64, scale = lsName(l == 0, "ldp", "stp", ""), true, 8
	default:
		return "", fmt.Errorf("arm64 disasm: bad pair opc %08x", w)
	}
	off := imm7 * scale
	rtN := rname(rt, is64, false)
	rt2N := rname(rt2, is64, false)
	base := rname(rn, true, true)
	switch (w >> 23) & 3 {
	case 0b010: // signed offset
		mem := fmt.Sprintf("[%s]", base)
		if off != 0 {
			mem = fmt.Sprintf("[%s, #%d]", base, off)
		}
		return fmt.Sprintf("%s %s, %s, %s", mnem, rtN, rt2N, mem), nil
	case 0b011: // pre-index
		return fmt.Sprintf("%s %s, %s, [%s, #%d]!", mnem, rtN, rt2N, base, off), nil
	case 0b001: // post-index
		return fmt.Sprintf("%s %s, %s, [%s], #%d", mnem, rtN, rt2N, base, off), nil
	}
	return "", fmt.Errorf("arm64 disasm: no-allocate pair unsupported %08x", w)
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
