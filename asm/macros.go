package asm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Size-macro expansion. NASM's insns.dat encodes families like "$bwdq MOV
// rm#,reg# [mr: o# 88# /r]" as a single template that expands into one
// concrete form per operand size. The size letters of the macro name index
// sizeName; for each, the '#' placeholders in the mnemonic, operands, code,
// and flags are rewritten per the rules in nasm's x86/preinsns.pl
// (func_multisize). This is a faithful Go reimplementation of those rules —
// nasm's perl is the spec, not a dependency.

// sizeName[i]: 0=z (default/null-prefixed), 1=b(8), 2=w(16), 3=d(32), 4=q(64).
var sizeName = []string{"z", "b", "w", "d", "q"}

// sizeBits returns the operand size in bits for size index i (0 for 'z').
func sizeBits(i int) int {
	if i == 0 {
		return 0
	}
	return 4 << uint(i) // 8, 16, 32, 64
}

// expandMacros flattens size-macro template forms (Macro != "") into their
// concrete per-size forms, leaving plain forms untouched.
func expandMacros(forms []Form) []Form {
	out := make([]Form, 0, len(forms))
	for _, f := range forms {
		if f.Macro == "" {
			out = append(out, f)
			continue
		}
		out = append(out, expandForm(f)...)
	}
	return out
}

// macroSizes returns the size indices a macro name expands over, e.g.
// "$bwdq" -> [1,2,3,4], "$wdq" -> [2,3,4], "$zwd" -> [0,2,3].
func macroSizes(name string) []int {
	letters := strings.TrimPrefix(name, "$")
	var idxs []int
	for _, ch := range letters {
		for i, sn := range sizeName {
			if string(ch) == sn {
				idxs = append(idxs, i)
			}
		}
	}
	return idxs
}

func expandForm(f Form) []Form {
	var out []Form
	for _, i := range macroSizes(f.Macro) {
		s := sizeBits(i)
		nf := Form{
			Mnemonic: substWord(f.Mnemonic, i, s),
			EncOrder: f.EncOrder,
			Flags:    append([]string(nil), f.Flags...),
		}
		for _, op := range f.Operands {
			nf.Operands = append(nf.Operands, substWord(resolveConds(op, i), i, s))
		}
		nf.Code = substCode(resolveConds(f.Code, i), i, s)
		// 32/64-bit forms of 8086/186/286 base instructions imply 386.
		if s >= 32 {
			has386 := false
			for _, fl := range nf.Flags {
				if fl == "386" {
					has386 = true
				}
			}
			if !has386 {
				for _, fl := range nf.Flags {
					if fl == "8086" || fl == "186" || fl == "286" {
						nf.Flags = append(nf.Flags, "386")
						break
					}
				}
			}
		}
		out = append(out, nf)
	}
	return out
}

// resolveConds handles inclusion patterns "(b:foo/w:bar/baz)": pick the first
// alternative whose size-letter set contains the current size, else the
// colon-less "else" alternative.
var condRe = regexp.MustCompile(`\(([^)]*)\)`)

func resolveConds(s string, i int) string {
	sn := sizeName[i]
	for {
		loc := condRe.FindStringSubmatchIndex(s)
		if loc == nil {
			return s
		}
		body := s[loc[2]:loc[3]]
		repl := ""
		for _, alt := range strings.Split(body, "/") {
			if c := strings.IndexByte(alt, ':'); c >= 0 {
				if strings.Contains(alt[:c], sn) {
					repl = alt[c+1:]
					break
				}
			} else {
				repl = alt // unconditional else
				break
			}
		}
		s = s[:loc[0]] + repl + s[loc[1]:]
	}
}

// substCode applies '#' substitution token-by-token over a code-string.
func substCode(code string, i, s int) string {
	toks := strings.Fields(code)
	for j, t := range toks {
		toks[j] = substWord(t, i, s)
	}
	return strings.Join(toks, " ")
}

var (
	opcodeRe = regexp.MustCompile(`^([0-9a-f]{2})(\+r)?(#)?#$`) // XX#, XX##, XX+r#
	osRe     = regexp.MustCompile(`^([oa])(d?)#$`)              // o#, a#, od#, ad#
	gprRe    = regexp.MustCompile(`^(k?(?:reg|rm))(##?)$`)      // reg#, rm##, kreg#, …
	accRe    = regexp.MustCompile(`^(reg_)?([abcd])x#$`)        // ax#, reg_cx#, …
)

// substWord rewrites the '#'/'%' placeholders in a single whitespace-delimited
// token for size index i (size s bits). Tokens with no placeholder pass
// through unchanged.
func substWord(t string, i, s int) string {
	if !strings.ContainsAny(t, "#%") {
		return t
	}
	// '%' / '%%' size-suffix substitution, in place within the token
	// (e.g. RET% -> RETW, ICEBP%% -> …). '%' -> upper size letter (empty for
	// z); '%%' -> the two-letter pair. Done first so the remaining '#'
	// handling sees a clean token.
	if strings.Contains(t, "%") {
		two := ""
		if i >= 2 {
			two = strings.ToUpper(sizeName[i-1]) + strings.ToUpper(sizeName[i])
		}
		one := ""
		if i != 0 {
			one = strings.ToUpper(sizeName[i])
		}
		t = strings.ReplaceAll(t, "%%", two)
		t = strings.ReplaceAll(t, "%", one)
		if !strings.Contains(t, "#") {
			return t
		}
	}
	// Colon-grouped operands (far pointers: imm16:imm32) — substitute each side.
	if strings.Contains(t, ":") {
		parts := strings.Split(t, ":")
		for k := range parts {
			parts[k] = substWord(parts[k], i, s)
		}
		return strings.Join(parts, ":")
	}
	// Operand-type modifier suffixes: '?' (optional operand), '*' (relaxed
	// match), or '|flag' (near/far/abs/…). Peel them off, substitute the
	// core, reattach.
	if strings.HasSuffix(t, "?") || strings.HasSuffix(t, "*") {
		return substWord(t[:len(t)-1], i, s) + t[len(t)-1:]
	}
	if bar := strings.IndexByte(t, '|'); bar >= 0 {
		return substWord(t[:bar], i, s) + t[bar:]
	}
	switch {
	case opcodeRe.MatchString(t):
		m := opcodeRe.FindStringSubmatch(t)
		base, _ := strconv.ParseInt(m[1], 16, 32)
		var n int64
		if m[3] != "" { // double '#': bit set for size > 16
			if s > 16 {
				n = 1
			}
		} else if s > 8 { // single '#': bit set for size > 8
			n = 1
		}
		if m[2] != "" { // +r: the size bit moves to bit 3
			n <<= 3
		}
		return fmt.Sprintf("%02x", base|n) + m[2]
	case osRe.MatchString(t):
		m := osRe.FindStringSubmatch(t)
		sz := "sm"
		if s > 0 {
			sz = strconv.Itoa(s)
		}
		if m[2] == "d" && sz == "sm" {
			sz = "df"
		}
		return m[1] + sz
	case t == "i#":
		switch {
		case i == 0:
			return "iwd"
		case s >= 64:
			return "id,s"
		default:
			return "i" + sizeName[i]
		}
	case t == "i##":
		if i == 0 {
			return "iwdq"
		}
		return "i" + sizeName[i]
	case t == "imm#":
		if s >= 64 {
			return "sdword64"
		}
		return "imm" + strconv.Itoa(s)
	case t == "imm##":
		return "imm" + strconv.Itoa(s)
	case t == "sbyte#":
		return []string{"imm8", "imm8", "sbyteword16", "sbytedword32", "sbytedword64"}[i]
	case accRe.MatchString(t):
		m := accRe.FindStringSubmatch(t)
		rl := m[2]
		switch i {
		case 1:
			return "reg_" + rl + "l"
		case 2:
			return "reg_" + rl + "x"
		case 3:
			return "reg_e" + rl + "x"
		case 4:
			return "reg_r" + rl + "x"
		}
	case gprRe.MatchString(t):
		m := gprRe.FindStringSubmatch(t)
		n := s
		if m[2] == "##" {
			n = s >> 1
		}
		return m[1] + strconv.Itoa(n)
	case strings.HasSuffix(t, "#") && !strings.HasSuffix(t, "##"):
		// Bare '#': append the size in bits (mem#, imm# variants handled above).
		return t[:len(t)-1] + strconv.Itoa(s)
	}
	return t // unhandled placeholder — left visible so tests can flag it
}
