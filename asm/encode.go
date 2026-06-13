package asm

import (
	"fmt"
	"strconv"
	"strings"
)

// Assemble encodes a single NASM/Intel-syntax instruction, in 64-bit mode,
// to machine code. Coverage is data-driven from the NASM table and grows as
// code-string tokens are implemented; unsupported forms return an error
// rather than wrong bytes. Byte-exactness is checked against nasm in the
// differential tests. (Memory operands arrive in a later slice.)
func Assemble(src string) ([]byte, error) {
	mnem, opStrs := parseInsn(src)
	if mnem == "" {
		return nil, nil // blank / comment-only line
	}
	ops := make([]operand, len(opStrs))
	for i, s := range opStrs {
		op, ok := parseOperand(s)
		if !ok {
			return nil, fmt.Errorf("asm %q: cannot parse operand %q", src, s)
		}
		ops[i] = op
	}

	var firstErr error
	for i := range table {
		f := &table[i]
		if f.Mnemonic != mnem || len(f.Operands) != len(ops) {
			continue
		}
		if !matchForm(f, ops) {
			continue
		}
		b, err := encodeForm(f, ops)
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

// parseInsn splits a source line into an upper-case mnemonic and its operand
// strings. Comments (';') are stripped.
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

// matchForm reports whether the parsed operands satisfy a form's operand-type
// signature.
func matchForm(f *Form, ops []operand) bool {
	if len(f.Operands) != len(ops) {
		return false
	}
	for i, tok := range f.Operands {
		if !matchOperand(stripMods(tok), ops[i]) {
			return false
		}
	}
	return true
}

// stripMods removes operand-type modifiers we don't yet act on: '?', '*' and
// '|flag' suffixes.
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
		return op.kind == opReg && op.size == regTokSize(tok) // register form (mem later)
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
	}
	return false
}

// regTokSize returns the bit width encoded in a reg8/rm32/imm16-style token.
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
func encodeForm(f *Form, ops []operand) ([]byte, error) {
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

	var legacy []byte // 66/F2/F3 legacy prefixes
	var rexW, rexR, rexB, rexForced bool
	var opcode []byte
	haveModRM := false
	var modReg, modRM byte
	var imm []byte

	// "nw" marks an operand whose 64-bit size is the long-mode default
	// (push/pop, near jumps): REX.W must NOT be emitted even though o64 is
	// present.
	noW := false
	for _, t := range strings.Fields(f.Code) {
		if t == "nw" {
			noW = true
		}
	}

	use := func(o *operand, isReg bool) {
		if o == nil || o.kind != opReg {
			return
		}
		if o.num8() >= 8 {
			if isReg {
				rexR = true
			} else {
				rexB = true
			}
		}
		if o.needRex {
			rexForced = true
		}
	}

	for _, tok := range strings.Fields(f.Code) {
		switch {
		case isHexByte(tok):
			b, _ := strconv.ParseUint(tok, 16, 8)
			opcode = append(opcode, byte(b))
		case len(tok) == 4 && tok[2:] == "+r" && isHexByte(tok[:2]):
			b, _ := strconv.ParseUint(tok[:2], 16, 8)
			r := regOp
			if r == nil {
				r = rmOp
			}
			if r == nil {
				return nil, fmt.Errorf("%s: +r with no register operand", tok)
			}
			opcode = append(opcode, byte(b)+byte(r.reg&7))
			if r.reg >= 8 {
				rexB = true
			}
			if r.needRex {
				rexForced = true
			}
		case tok == "/r":
			if regOp == nil || rmOp == nil {
				return nil, fmt.Errorf("/r needs reg and rm operands")
			}
			haveModRM, modReg, modRM = true, byte(regOp.reg&7), byte(rmOp.reg&7)
			use(regOp, true)
			use(rmOp, false)
		case len(tok) == 2 && tok[0] == '/' && tok[1] >= '0' && tok[1] <= '7':
			if rmOp == nil {
				return nil, fmt.Errorf("/digit needs an rm operand")
			}
			haveModRM, modReg, modRM = true, tok[1]-'0', byte(rmOp.reg&7)
			use(rmOp, false)
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
		case tok == "o32" || tok == "o8" || tok == "osz" || tok == "osm" || tok == "odf" || tok == "nw" ||
			tok == "a16" || tok == "a32" || tok == "a64" || tok == "asz" || tok == "adf" ||
			strings.HasPrefix(tok, "norex") || strings.HasPrefix(tok, "nof") ||
			tok == "nohi" || tok == "np" || tok == "hle" || tok == "hlexr" || tok == "wait" || tok == "resb":
			// Prefix/constraint markers with no byte output in this mode.
		default:
			return nil, fmt.Errorf("unsupported code token %q (in %q)", tok, f.Code)
		}
	}

	// Assemble in canonical order: legacy prefixes, REX, opcode, ModRM, imm.
	if noW {
		rexW = false
	}
	out := legacy
	if rexW || rexR || rexB || rexForced {
		var rex byte = 0x40
		if rexW {
			rex |= 0x08
		}
		if rexR {
			rex |= 0x04
		}
		if rexB {
			rex |= 0x01
		}
		out = append(out, rex)
	}
	out = append(out, opcode...)
	if haveModRM {
		out = append(out, 0xC0|modReg<<3|modRM) // mod=11 (register-direct)
	}
	out = append(out, imm...)
	return out, nil
}

func (o *operand) num8() int { return o.reg }

func immVal(o *operand) int64 {
	if o == nil {
		return 0
	}
	return o.imm
}

// appendLE appends the low n bytes of v in little-endian order.
func appendLE(b []byte, v int64, n int) []byte {
	for i := 0; i < n; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// isHexByte reports whether tok is a two-digit lowercase-hex opcode byte.
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
