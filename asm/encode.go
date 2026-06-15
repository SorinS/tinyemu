package asm

import (
	"fmt"
	"strconv"
	"strings"
)

// Mode is the CPU operating mode an instruction is assembled for. It selects
// which table forms are valid and how the encoder emits prefixes (REX,
// address-size, absolute vs RIP-relative memory).
type Mode int

const (
	Bits64 Mode = 64 // long mode (default)
	Bits32 Mode = 32 // 32-bit protected mode
)

// Assemble encodes a single NASM/Intel-syntax instruction, in 64-bit mode,
// to machine code. Coverage is data-driven from the NASM table and grows as
// code-string tokens are implemented; unsupported forms return an error
// rather than wrong bytes. Byte-exactness is checked against nasm in the
// differential tests.
func Assemble(src string) ([]byte, error) { return AssembleMode(src, Bits64) }

// dataWidths maps the data-definition directives to their element byte width.
var dataWidths = map[string]int{"DB": 1, "DW": 2, "DD": 4, "DQ": 8}

// prefixBytes maps a leading repeat/lock prefix mnemonic (upper-case) to its
// group-1 legacy prefix byte. These prefix an instruction on the same line —
// "rep stosb", "lock add [rax], 1" — and are emitted before the rest of the
// encoding (REX/66/67 follow), matching nasm's byte order.
var prefixBytes = map[string]byte{
	"REP": 0xF3, "REPE": 0xF3, "REPZ": 0xF3,
	"REPNE": 0xF2, "REPNZ": 0xF2,
	"LOCK": 0xF0,
}

// splitPrefix reports a leading rep/repe/repne/lock prefix on a source line and
// returns the prefix byte plus the remaining instruction text (comment
// stripped). ok is false when the line has no such prefix (or is bare).
func splitPrefix(src string) (pfx byte, rest string, ok bool) {
	s := strings.TrimSpace(stripComment(src))
	sp := strings.IndexAny(s, " \t")
	if sp < 0 {
		return 0, "", false
	}
	if b, isPfx := prefixBytes[strings.ToUpper(s[:sp])]; isPfx {
		return b, strings.TrimSpace(s[sp+1:]), true
	}
	return 0, "", false
}

// AssembleMode is Assemble for an explicit CPU mode (Bits32 or Bits64).
func AssembleMode(src string, mode Mode) ([]byte, error) {
	if pfx, rest, ok := splitPrefix(src); ok {
		b, err := AssembleMode(rest, mode)
		if err != nil {
			return nil, err
		}
		return append([]byte{pfx}, b...), nil
	}
	mnem, opStrs := parseInsn(src)
	if mnem == "" {
		return nil, nil
	}
	if w, ok := dataWidths[mnem]; ok {
		return assembleData(w, opStrs)
	}
	ops := make([]operand, len(opStrs))
	for i, s := range opStrs {
		op, ok := parseOperand(s)
		if !ok {
			return nil, fmt.Errorf("asm %q: cannot parse operand %q", src, s)
		}
		ops[i] = op
	}
	return encodeOps(src, mnem, ops, mode)
}

// encodeOps finds the matching table form for parsed operands and encodes it.
func encodeOps(src, mnem string, ops []operand, mode Mode) ([]byte, error) {
	// nasm peephole: "mov r64, imm" with imm in [0, 0xFFFFFFFF] is emitted as
	// the 32-bit "mov r32, imm" (which zero-extends to 64 bits) — 5-6 bytes
	// instead of the REX.W imm32 form's 7. Negative or wider immediates keep
	// the 64-bit form. 64-bit-only (no r64 in 32-bit mode).
	if mode == Bits64 && mnem == "MOV" && len(ops) == 2 && ops[0].kind == opReg && ops[0].size == 64 &&
		ops[1].kind == opImm && ops[1].imm >= 0 && ops[1].imm <= 0xFFFFFFFF {
		ops[0].size = 32
	}

	var firstErr error
	for i := range table {
		f := &table[i]
		if f.Mnemonic != mnem || len(f.Operands) != len(ops) {
			continue
		}
		if !matchForm(f, ops, mode) {
			continue
		}
		b, err := encodeForm(f, ops, mode)
		if err == nil {
			return b, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, fmt.Errorf("asm %q: %w", src, firstErr)
	}
	return nil, fmt.Errorf("asm %q: no matching encoding form", src)
}

// assembleData emits the bytes for a db/dw/dd/dq directive: comma-separated
// integers, and (for db) simple "…"/'…' string literals.
func assembleData(width int, args []string) ([]byte, error) {
	var out []byte
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if width == 1 && len(a) >= 2 && (a[0] == '"' || a[0] == '\'') && a[len(a)-1] == a[0] {
			out = append(out, a[1:len(a)-1]...) // simple string literal (no escapes)
			continue
		}
		v, ok := parseImm(a)
		if !ok {
			return nil, fmt.Errorf("asm: bad data value %q", a)
		}
		for i := 0; i < width; i++ {
			out = append(out, byte(uint64(v)>>(8*i)))
		}
	}
	return out, nil
}

// parseInsn splits a source line into an upper-case mnemonic and operands.
func parseInsn(src string) (mnem string, ops []string) {
	if c := strings.IndexByte(src, ';'); c >= 0 {
		src = src[:c]
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return "", nil
	}
	sp := strings.IndexAny(src, " \t")
	if sp < 0 {
		return strings.ToUpper(src), nil
	}
	mnem = strings.ToUpper(src[:sp])
	for _, o := range strings.Split(src[sp+1:], ",") {
		if o = strings.TrimSpace(o); o != "" {
			ops = append(ops, o)
		}
	}
	return mnem, ops
}

func matchForm(f *Form, ops []operand, mode Mode) bool {
	if len(f.Operands) != len(ops) {
		return false
	}
	for _, fl := range f.Flags {
		// NOLONG: not valid in 64-bit (long) mode. LONG: valid only in long
		// mode (i.e. not in 32-bit). Filter by the target mode.
		if fl == "NOLONG" && mode == Bits64 {
			return false
		}
		if fl == "LONG" && mode != Bits64 {
			return false
		}
	}
	for i, tok := range f.Operands {
		if !matchOperand(stripMods(tok), ops[i]) {
			return false
		}
	}
	return true
}

// stripMods removes operand-type modifiers we don't yet act on.
func stripMods(tok string) string {
	if i := strings.IndexByte(tok, '|'); i >= 0 {
		tok = tok[:i]
	}
	return strings.TrimRight(tok, "?*")
}

func matchOperand(tok string, op operand) bool {
	switch {
	case strings.HasPrefix(tok, "reg_"):
		if op.kind != opReg {
			return false
		}
		g, ok := gprByName[tok[4:]]
		return ok && op.reg == g.num && op.size == g.size && op.highByte == g.highByte
	case tok == "reg8", tok == "reg16", tok == "reg32", tok == "reg64":
		return op.kind == opReg && op.size == regTokSize(tok)
	case tok == "rm8", tok == "rm16", tok == "rm32", tok == "rm64":
		n := regTokSize(tok)
		if op.kind == opReg {
			return op.size == n
		}
		return op.kind == opMem && (op.memSize == n || op.memSize == 0)
	case tok == "mem":
		return op.kind == opMem
	case tok == "mem8", tok == "mem16", tok == "mem32", tok == "mem64":
		return op.kind == opMem && (op.memSize == regTokSize(tok) || op.memSize == 0)
	case tok == "imm":
		return op.kind == opImm
	case tok == "imm8", tok == "imm16", tok == "imm32":
		return op.kind == opImm && fitsImm(op.imm, regTokSize(tok))
	case tok == "imm64":
		return op.kind == opImm
	case tok == "unity":
		return op.kind == opImm && op.imm == 1
	case tok == "sbyteword16", tok == "sbytedword32", tok == "sbytedword64":
		return op.kind == opImm && fitsSigned(op.imm, 8)
	case tok == "sdword64":
		return op.kind == opImm && fitsSigned(op.imm, 32)
	case tok == "short", tok == "near", tok == "near|short":
		return op.kind == opTarget
	}
	return false
}

func regTokSize(tok string) int {
	switch {
	case strings.HasSuffix(tok, "8"):
		return 8
	case strings.HasSuffix(tok, "16"):
		return 16
	case strings.HasSuffix(tok, "32"):
		return 32
	case strings.HasSuffix(tok, "64"):
		return 64
	}
	return 0
}

// encodeForm interprets a form's code-string into machine code for the given
// operands. Operand roles come from the form's EncOrder ("mr", "rm", "mi", …).
func encodeForm(f *Form, ops []operand, mode Mode) ([]byte, error) {
	var regOp, rmOp, immOp *operand
	for i := range ops {
		role := byte('-')
		if i < len(f.EncOrder) {
			role = f.EncOrder[i]
		}
		switch role {
		case 'r':
			regOp = &ops[i]
		case 'm':
			rmOp = &ops[i]
		case 'i':
			immOp = &ops[i]
		}
	}

	var legacy []byte
	var rexW, rexR, rexX, rexB, rexForced bool
	var opcode, imm []byte
	haveModRM := false
	var regField byte

	noW := false
	for _, t := range strings.Fields(f.Code) {
		if t == "nw" || t == "o64nw" {
			noW = true
		}
	}

	for _, tok := range strings.Fields(f.Code) {
		switch {
		case isHexByte(tok):
			b, _ := strconv.ParseUint(tok, 16, 8)
			opcode = append(opcode, byte(b))
		case tok == "0f38":
			opcode = append(opcode, 0x0f, 0x38)
		case tok == "0f3a":
			opcode = append(opcode, 0x0f, 0x3a)
		case tok == "wait":
			opcode = append(opcode, 0x9b) // x87 FWAIT prefix (F-variant of FNxxx)
		case len(tok) == 4 && tok[2:] == "+r" && isHexByte(tok[:2]):
			b, _ := strconv.ParseUint(tok[:2], 16, 8)
			r := regOp
			if r == nil {
				r = rmOp
			}
			if r == nil || r.kind != opReg {
				return nil, fmt.Errorf("+r needs a register operand")
			}
			opcode = append(opcode, byte(b)+byte(r.reg&7))
			if r.reg >= 8 {
				rexB = true
			}
			if r.needRex {
				rexForced = true
			}
		case tok == "/r":
			if regOp == nil {
				return nil, fmt.Errorf("/r needs a reg operand")
			}
			haveModRM, regField = true, byte(regOp.reg&7)
			if regOp.reg >= 8 {
				rexR = true
			}
			if regOp.needRex {
				rexForced = true
			}
		case len(tok) == 2 && tok[0] == '/' && tok[1] >= '0' && tok[1] <= '7':
			haveModRM, regField = true, tok[1]-'0'
		case tok == "o16":
			legacy = append(legacy, 0x66)
		case tok == "o64":
			rexW = true
		case tok == "f3i":
			legacy = append(legacy, 0xF3)
		case tok == "f2i":
			legacy = append(legacy, 0xF2)
		case tok == "66i":
			legacy = append(legacy, 0x66)
		case tok == "ib" || tok == "ib,s" || tok == "ib,u":
			imm = append(imm, byte(immVal(immOp)))
		case tok == "iw":
			imm = appendLE(imm, immVal(immOp), 2)
		case tok == "id" || tok == "id,s":
			imm = appendLE(imm, immVal(immOp), 4)
		case tok == "iq":
			imm = appendLE(imm, immVal(immOp), 8)
		case tok == "rel8":
			imm = append(imm, byte(immVal(immOp))) // branch displacement (rel8)
		case tok == "rel" || tok == "rel32":
			imm = appendLE(imm, immVal(immOp), 4) // branch displacement (rel32)
		case tok == "os":
			// branch operand-size marker — no prefix in 64-bit mode.
		case tok == "o32" || tok == "o8" || tok == "osz" || tok == "osm" || tok == "odf" || tok == "nw" || tok == "o64nw" ||
			tok == "a16" || tok == "a32" || tok == "a64" || tok == "asz" || tok == "adf" ||
			strings.HasPrefix(tok, "norex") || strings.HasPrefix(tok, "nof") || strings.HasPrefix(tok, "norep") ||
			tok == "repe" || tok == "repne" || tok == "rep" ||
			tok == "nohi" || tok == "np" || tok == "hle" || tok == "hlexr" || strings.HasPrefix(tok, "hlen") ||
			tok == "wait" || tok == "resb":
			// Prefix/constraint markers with no byte output in this mode.
		default:
			return nil, fmt.Errorf("unsupported code token %q (in %q)", tok, f.Code)
		}
	}

	// ModRM (+ SIB + displacement), built once the reg field and rm operand
	// are known.
	var modrm []byte
	if haveModRM {
		if rmOp == nil {
			return nil, fmt.Errorf("ModRM needs an rm operand")
		}
		if rmOp.kind == opReg {
			modrm = []byte{0xC0 | regField<<3 | byte(rmOp.reg&7)}
			if rmOp.reg >= 8 {
				rexB = true
			}
			if rmOp.needRex {
				rexForced = true
			}
		} else {
			mb, rX, rB, err := encodeMem(rmOp, regField, mode)
			if err != nil {
				return nil, err
			}
			modrm, rexX, rexB = mb, rexX || rX, rexB || rB
			// Address-size override (0x67) is needed when the memory operand's
			// register width differs from the mode's default address size:
			// 32-bit registers in 64-bit mode, or 16-bit in 32-bit mode (the
			// latter unsupported here).
			if mode == Bits64 && rmOp.baseSize == 32 {
				legacy = append([]byte{0x67}, legacy...)
			}
		}
	}

	if noW {
		rexW = false
	}
	// 32-bit protected mode has no REX prefix: a form that needs one (a 64-bit
	// operand via o64, or an extended register r8–r15 / SPL-style byte reg) is
	// simply not encodable here.
	if mode != Bits64 && (rexW || rexR || rexX || rexB || rexForced) {
		return nil, fmt.Errorf("form requires REX (64-bit only), not valid in 32-bit mode")
	}
	out := legacy
	if rexW || rexR || rexX || rexB || rexForced {
		var rex byte = 0x40
		if rexW {
			rex |= 0x08
		}
		if rexR {
			rex |= 0x04
		}
		if rexX {
			rex |= 0x02
		}
		if rexB {
			rex |= 0x01
		}
		out = append(out, rex)
	}
	out = append(out, opcode...)
	out = append(out, modrm...)
	out = append(out, imm...)
	return out, nil
}

// encodeMem builds the ModRM (+ SIB + displacement) bytes for a memory
// operand, given the ModRM.reg field value, and reports whether REX.X / REX.B
// are needed.
func encodeMem(m *operand, reg byte, mode Mode) (out []byte, rexX, rexB bool, err error) {
	if m.memRip {
		if mode != Bits64 {
			return nil, false, false, fmt.Errorf("RIP-relative addressing is 64-bit only")
		}
		out = []byte{reg<<3 | 0x05} // mod=00, rm=101 → RIP-relative disp32
		return appendLE(out, m.memDisp, 4), false, false, nil
	}
	base, index := m.memBase, m.memIndex
	if index == 4 {
		return nil, false, false, fmt.Errorf("rsp cannot be an index register")
	}
	if index >= 0 && m.indexRex {
		rexX = true
	}
	if base >= 0 && m.baseRex {
		rexB = true
	}

	// A no-base absolute [disp32]: in 64-bit mode mod=00/rm=101 means
	// RIP-relative, so an absolute address must go through a SIB with no base.
	// In 32-bit mode mod=00/rm=101 *is* absolute disp32, so use it directly
	// (unless there's an index, which still needs a SIB).
	if base < 0 && index < 0 && mode != Bits64 {
		out = []byte{reg<<3 | 0x05}
		return appendLE(out, m.memDisp, 4), rexX, rexB, nil
	}

	useSIB := index >= 0 || base < 0 || (base&7) == 4

	var mod byte
	var disp []byte
	switch {
	case base < 0:
		mod, disp = 0, appendLE(nil, m.memDisp, 4) // disp32, no base
	case !m.memHasDisp && (base&7) != 5:
		mod = 0
	case fitsSigned(m.memDisp, 8):
		mod, disp = 1, []byte{byte(m.memDisp)}
	default:
		mod, disp = 2, appendLE(nil, m.memDisp, 4)
	}
	if base >= 0 && (base&7) == 5 && mod == 0 { // rbp/r13: force disp8=0
		mod, disp = 1, []byte{0}
	}

	if useSIB {
		var scaleBits byte
		switch m.memScale {
		case 2:
			scaleBits = 1
		case 4:
			scaleBits = 2
		case 8:
			scaleBits = 3
		}
		idx := byte(4) // 100 = no index
		if index >= 0 {
			idx = byte(index & 7)
		}
		bse := byte(5) // 101 = no base (with mod=00 → disp32)
		if base >= 0 {
			bse = byte(base & 7)
		}
		out = append(out, mod<<6|reg<<3|4) // rm=100 → SIB follows
		out = append(out, scaleBits<<6|idx<<3|bse)
	} else {
		out = append(out, mod<<6|reg<<3|byte(base&7))
	}
	return append(out, disp...), rexX, rexB, nil
}

func immVal(o *operand) int64 {
	if o == nil {
		return 0
	}
	return o.imm
}

func appendLE(b []byte, v int64, n int) []byte {
	for i := 0; i < n; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

func isHexByte(tok string) bool {
	if len(tok) != 2 {
		return false
	}
	for _, c := range tok {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
