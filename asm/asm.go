// Package asm is a NASM/Intel-syntax x86/x86-64 assembler and disassembler.
//
// The instruction set is data-driven: every encoding form is read from
// NASM's canonical instruction table (insns.dat, vendored here under
// BSD-2-Clause — see insns.dat's SPDX header). The table is complete, so
// coverage grows by implementing the finite set of encoding code-string
// tokens, not by hand-adding instructions.
//
// Disassembly is delegated to golang.org/x/arch/x86asm. Assembly is a
// hand-rolled encoder that interprets the table's code-strings, validated
// for byte-exactness against nasm (see the differential tests).
package asm

import (
	_ "embed"
	"strings"
)

//go:embed insns.dat
var insnsDat string

// table is the complete, macro-expanded instruction table, parsed once.
var table = buildTable(insnsDat)

// buildTable parses insns.dat, dispatching the generator macros ($arith,
// $shift) and size-expanding the rest. Generator/modifier macros not yet
// reimplemented ($br jumps, $eshift/$xshift APX, $k mask, $hint) are skipped
// here rather than mis-expanded — tracked by skippedMacros for visibility.
var skippedMacros []string

func buildTable(data string) []Form {
	var forms []Form
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || line[0] == ';' {
			continue
		}
		switch {
		case strings.HasPrefix(line, "$arith"):
			forms = append(forms, genEightfold(arithMembers, arithTemplates)...)
		case strings.HasPrefix(line, "$shift"):
			forms = append(forms, genEightfold(shiftMembers, shiftTemplates)...)
		case strings.HasPrefix(line, "$eshift"), strings.HasPrefix(line, "$xshift"),
			strings.HasPrefix(line, "$br"), strings.HasPrefix(line, "$k "),
			strings.HasPrefix(line, "$hint"):
			skippedMacros = append(skippedMacros, line)
		default:
			if f, ok := parseLine(line); ok {
				if f.Macro != "" {
					forms = append(forms, expandForm(f)...)
				} else {
					forms = append(forms, f)
				}
			}
		}
	}
	return forms
}

// Table returns the complete set of x86/x86-64 encoding forms from NASM's
// instruction table, with all size-macro families expanded. The slice is
// shared; callers must not mutate it.
func Table() []Form { return table }

// Form is one encoding form from the instruction table: a mnemonic, its
// operand-type signature, the operand-encoding order, the byte-code string,
// and the instruction flags. Size-macro template lines (e.g. "$bwdq MOV …")
// are kept verbatim with Macro set; expandMacros turns each into the
// concrete per-size forms.
type Form struct {
	Mnemonic string   // e.g. "MOV"
	Operands []string // operand-type tokens, e.g. ["reg64", "rm64"]; nil = void
	EncOrder string   // operand-encoding order ("mr", "rm", "mi", …); "" if none
	Code     string   // space-normalized byte-code tokens, e.g. "o64 89 /r"
	Flags    []string // e.g. ["X64", "SM"]
	Macro    string   // size-macro prefix if a template line (e.g. "$bwdq"); else ""
}

// parseTable parses NASM insns.dat text into raw forms. Size-macro template
// lines are returned unexpanded (Macro != ""); call expandMacros to flatten.
func parseTable(data string) []Form {
	var forms []Form
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || line[0] == ';' {
			continue
		}
		if f, ok := parseLine(line); ok {
			forms = append(forms, f)
		}
	}
	return forms
}

// parseLine parses a single functional line. The four fields are mnemonic,
// operands, code-string, and flags; the code-string is bracket-delimited and
// may contain spaces, so we split around the brackets rather than on
// whitespace. A leading "$macro" token marks a size-macro template.
func parseLine(line string) (Form, bool) {
	var f Form

	var head, code, tail string
	if open := strings.IndexByte(line, '['); open >= 0 {
		rel := strings.IndexByte(line[open:], ']')
		if rel < 0 {
			return f, false
		}
		closeIdx := open + rel
		head, code, tail = line[:open], line[open+1:closeIdx], line[closeIdx+1:]
	} else {
		// No code-string: pseudo-ops ("DB … ignore …"). Treat the field
		// after the mnemonic as operands; the rest is flags.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return f, false
		}
		head = fields[0] + " " + fields[1]
		if len(fields) >= 4 {
			tail = fields[len(fields)-1]
		}
		code = "" // pseudo / "ignore"
	}

	hf := strings.Fields(head)
	idx := 0
	if len(hf) > 0 && strings.HasPrefix(hf[0], "$") {
		f.Macro = hf[0]
		idx = 1
	}
	if len(hf) <= idx {
		return f, false
	}
	f.Mnemonic = hf[idx]
	if len(hf) > idx+1 && hf[idx+1] != "void" {
		f.Operands = splitList(hf[idx+1], ",")
	}

	// The code-string may carry an "enc:" prefix (mr/rm/mi/…) giving the
	// operand-encoding order; the remainder is the byte-code token stream.
	code = strings.TrimSpace(code)
	if c := strings.IndexByte(code, ':'); c >= 0 {
		f.EncOrder = strings.TrimSpace(code[:c])
		code = code[c+1:]
	}
	f.Code = strings.Join(strings.Fields(code), " ")
	f.Flags = splitList(strings.TrimSpace(tail), ",")

	return f, true
}

// splitList splits s on sep, trimming whitespace and dropping empties.
func splitList(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
