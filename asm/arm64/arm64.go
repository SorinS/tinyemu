// Package arm64 is a small, hand-written AArch64 (ARM64) assembler. Like the
// RISC-V assembler (and unlike the data-driven x86 one) AArch64 has a fixed
// 32-bit instruction width and a regular field layout, so a compact Go table
// with one encoder per instruction class is the maintainable choice.
//
// This first slice covers the integer core: add/sub (shifted-register and
// immediate), logical shifted-register (and/orr/eor/ands + bic/orn/eon/bics),
// move-wide (movz/movn/movk), load/store unsigned-offset (ldr/str, x and w),
// and branches (b/bl, b.cond, cbz/cbnz, ret/br/blr). Deliberately deferred to
// later slices: the logical-immediate bitmask encoding, MOV/CMP/… aliases, and
// FP/SIMD. Byte-exactness is checked differentially against llvm-mc.
package arm64

import (
	"strconv"
	"strings"
)

// reg is a parsed general-purpose register operand: its number 0–31 and width.
// Register 31 is the zero register (xzr/wzr) or stack pointer (sp/wsp) — both
// encode as field value 31; which one it means is fixed by the instruction, so
// the encoder never has to tell them apart.
type reg struct {
	num  uint32 // 0..31
	is64 bool   // true for x/sp, false for w/wsp
	isSP bool   // true for sp/wsp (vs xzr/wzr) — both encode as 31 but pick
	// different add/sub forms: SP forces the extended-register encoding.
}

// parseReg resolves a register operand to its number and width.
func parseReg(s string) (reg, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "xzr":
		return reg{num: 31, is64: true}, true
	case "wzr":
		return reg{num: 31, is64: false}, true
	case "sp":
		return reg{31, true, true}, true
	case "wsp":
		return reg{31, false, true}, true
	case "lr":
		return reg{num: 30, is64: true}, true
	case "fp":
		return reg{num: 29, is64: true}, true
	}
	if len(s) < 2 || (s[0] != 'x' && s[0] != 'w') {
		return reg{}, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 0 || n > 30 {
		return reg{}, false
	}
	return reg{num: uint32(n), is64: s[0] == 'x'}, true
}

// condCodes maps an AArch64 condition suffix to its 4-bit encoding.
var condCodes = map[string]uint32{
	"eq": 0, "ne": 1, "cs": 2, "hs": 2, "cc": 3, "lo": 3,
	"mi": 4, "pl": 5, "vs": 6, "vc": 7, "hi": 8, "ls": 9,
	"ge": 10, "lt": 11, "gt": 12, "le": 13, "al": 14, "nv": 15,
}

// parseImm parses an immediate operand: an optional '#', then a signed decimal
// or 0x-hex literal.
func parseImm(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
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

// isLabelName reports whether s is a plausible label (letters/digits/_/. and a
// non-digit first character).
func isLabelName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	c := s[0]
	if !(c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || c == '.' || (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}
