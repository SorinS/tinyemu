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
// differential tests.
func Assemble(src string) ([]byte, error) {
	mnem, ops := parseInsn(src)
	if mnem == "" {
		return nil, nil // blank / comment-only line
	}
	var firstErr error
	for i := range table {
		f := &table[i]
		if f.Mnemonic != mnem || len(f.Operands) != len(ops) {
			continue
		}
		if !operandsMatch(f.Operands, ops) {
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

// operandsMatch reports whether the given source operands satisfy a form's
// operand-type signature. (Initial coverage: void forms only; the operand
// matcher grows alongside the encoder.)
func operandsMatch(formOps []string, ops []string) bool {
	return len(formOps) == 0 && len(ops) == 0
}

// encodeForm interprets a form's code-string into machine-code bytes for the
// given operands. Tokens that require operand encoding (and aren't yet
// supported) return an error so coverage gaps are explicit.
func encodeForm(f *Form, ops []string) ([]byte, error) {
	var out []byte
	var rex byte
	rexNeeded := false

	for _, tok := range strings.Fields(f.Code) {
		switch {
		case isHexByte(tok):
			b, _ := strconv.ParseUint(tok, 16, 8)
			out = append(out, byte(b))
		case tok == "o16":
			out = append(out, 0x66)
		case tok == "o64":
			rex |= 0x48 // REX.W
			rexNeeded = true
		case tok == "f3i":
			out = append(out, 0xF3) // mandatory F3 prefix
		case tok == "f2i":
			out = append(out, 0xF2) // mandatory F2 prefix
		case tok == "66i":
			out = append(out, 0x66) // mandatory 66 prefix
		case tok == "o32", tok == "o8", tok == "osz", tok == "osm", tok == "odf", tok == "nw",
			tok == "a16", tok == "a32", tok == "a64", tok == "asz", tok == "adf",
			strings.HasPrefix(tok, "norex"), strings.HasPrefix(tok, "nof"),
			tok == "nohi", tok == "np", tok == "hle", tok == "hlexr",
			tok == "wait", tok == "resb":
			// Prefix/constraint markers with no byte output in this mode.
		default:
			return nil, fmt.Errorf("unsupported code token %q (in %q)", tok, f.Code)
		}
	}
	if rexNeeded {
		// REX prefix precedes the opcode; for the fixed forms handled so far
		// the opcode is a single trailing byte group, so prepend.
		out = append([]byte{rex}, out...)
	}
	return out, nil
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
