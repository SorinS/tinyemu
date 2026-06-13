package asm

import (
	"strings"
	"testing"
)

// TestExpandMacros validates the size-macro expansion on a known family and
// checks that expansion leaves essentially no '#' placeholders behind.
func TestExpandMacros(t *testing.T) {
	forms := Table()

	// $bwdq MOV rm#,reg# [mr: hlexr o# 88# /r] expands to the four sizes,
	// with the opcode w-bit flipping from 88 (byte) to 89 (word+).
	want := map[string]string{
		"rm8,reg8":   "hlexr o8 88 /r",
		"rm16,reg16": "hlexr o16 89 /r",
		"rm32,reg32": "hlexr o32 89 /r",
		"rm64,reg64": "hlexr o64 89 /r",
	}
	got := map[string]string{}
	for _, f := range forms {
		if f.Mnemonic == "MOV" && f.EncOrder == "mr" && len(f.Operands) == 2 {
			got[strings.Join(f.Operands, ",")] = f.Code
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("MOV %s: code = %q, want %q", k, got[k], v)
		}
	}

	// After expansion, no form should retain a macro prefix, and very few
	// should retain a literal '#' (only encodings using placeholder tokens
	// not yet handled — track the count so regressions surface).
	leftover := 0
	for _, f := range forms {
		if f.Macro != "" {
			t.Errorf("unexpanded macro template survived: %+v", f)
		}
		if strings.Contains(f.Mnemonic+" "+f.Code+" "+strings.Join(f.Operands, " "), "#") {
			leftover++
		}
	}
	t.Logf("expanded table: %d forms, %d retain a '#' placeholder", len(forms), leftover)
	// Only the JCXZ/JECXZ/JRCXZ family (JCX#Z, address-size dependent) is not
	// yet rewritten; everything else expands cleanly.
	if leftover > 2 {
		t.Errorf("too many unexpanded '#' placeholders: %d (expander missing a rule?)", leftover)
	}
}
