package asm

import (
	"fmt"
	"strconv"
	"strings"
)

// Generator macros. Beyond the size macros, NASM's insns.dat uses
// "def_eightfold" generator macros that emit a whole instruction family from
// one line: $arith -> ADD/OR/ADC/SBB/AND/SUB/XOR/CMP, $shift -> ROL/ROR/RCL/
// RCR/SHL/SAL/SHR/SAR. The base opcode of family member n is n<<3 and its
// ModRM /digit is n. These mirror nasm's x86/preinsns.pl arith/shift macros;
// the per-member templates are reproduced here and run back through parseLine
// + the size expander.

// eightfoldMember is one family member: a mnemonic and its index (0..7).
type eightfoldMember struct {
	name string
	n    int
}

// arithMembers mirrors the $arith trigger line in insns.dat.
var arithMembers = []eightfoldMember{
	{"ADD", 0}, {"OR", 1}, {"ADC", 2}, {"SBB", 3},
	{"AND", 4}, {"SUB", 5}, {"XOR", 6}, {"CMP", 7},
}

// shiftMembers mirrors the $shift trigger line (SHL and SAL share /4).
var shiftMembers = []eightfoldMember{
	{"ROL", 0}, {"ROR", 1}, {"RCL", 2}, {"RCR", 3},
	{"SHL", 4}, {"SAL", 4}, {"SHR", 5}, {"SAR", 7},
}

// arithTemplates are the per-member encoding forms (non-APX). Placeholders:
// $op = mnemonic, $n = digit, $00/$02/$04/$05 = base opcode + offset.
var arithTemplates = []string{
	"$bwdq $op rm#,reg#\t[mr: o# $00# /r]\t8086",
	"$bwdq $op reg#,rm#\t[rm: o# $02# /r]\t8086",
	"$op reg_al,imm8\t[-i: o8 $04 ib]\t8086",
	"$op rm8,imm8\t[mi: 80 /$n ib]\t8086",
	"$op rm16,sbyteword16\t[mi: o16 83 /$n ib,s]\t8086",
	"$op reg_ax,imm16\t[-i: o16 $05 iw]\t8086",
	"$op rm16,imm16\t[mi: o16 81 /$n iw]\t8086",
	"$op rm32,sbytedword32\t[mi: o32 83 /$n ib,s]\t386",
	"$op reg_eax,imm32\t[-i: o32 $05 id]\t386",
	"$op rm32,imm32\t[mi: o32 81 /$n id]\t386",
	"$op rm64,sbytedword64\t[mi: o64 83 /$n ib,s]\tX86_64",
	"$op reg_rax,sdword64\t[-i: o64 $05 id,s]\tX86_64",
	"$op rm64,sdword64\t[mi: o64 81 /$n id,s]\tX86_64",
}

// shiftTemplates are the per-member shift/rotate forms. d0/d2/c0 are the base
// opcodes (shift-by-1 / by-CL / by-imm8); the '#' picks the byte vs word+ form.
var shiftTemplates = []string{
	"$bwdq $op rm#,unity\t[m-: o# d0# /$n]\t8086",
	"$bwdq $op rm#,reg_cl\t[m-: o# d2# /$n]\t8086",
	"$bwdq $op rm#,imm8\t[mi: o# c0# /$n ib,u]\t186",
}

func hex2(v int) string { return fmt.Sprintf("%02x", v) }

// condCodes are the x86 condition mnemonics (with aliases) and their 4-bit
// codes — Jcc opcode is 70+cc (rel8) / 0F 80+cc (rel32).
var condCodes = []struct {
	name string
	cc   int
}{
	{"O", 0}, {"NO", 1}, {"B", 2}, {"C", 2}, {"NAE", 2}, {"AE", 3}, {"NB", 3}, {"NC", 3},
	{"E", 4}, {"Z", 4}, {"NE", 5}, {"NZ", 5}, {"BE", 6}, {"NA", 6}, {"A", 7}, {"NBE", 7},
	{"S", 8}, {"NS", 9}, {"P", 10}, {"PE", 10}, {"NP", 11}, {"PO", 11},
	{"L", 12}, {"NGE", 12}, {"GE", 13}, {"NL", 13}, {"LE", 14}, {"NG", 14}, {"G", 15}, {"NLE", 15},
}

// genBr produces the relative-branch forms NASM emits from the $br macro:
// JMP (short EB / near E9), CALL (near E8), and Jcc over all condition codes
// (short 70+cc / near 0F 80+cc). Operand type "short" = rel8, "near" = rel32.
func genBr() []Form {
	var out []Form
	add := func(line string) {
		if f, ok := parseLine(line); ok {
			out = append(out, f)
		}
	}
	add("JMP short\t[i: os eb rel8]\t8086")
	add("JMP near\t[i: os e9 rel]\t8086")
	add("CALL near\t[i: os e8 rel]\t8086")
	for _, c := range condCodes {
		add(fmt.Sprintf("J%s short\t[i: os %s rel8]\t8086", c.name, hex2(0x70+c.cc)))
		add(fmt.Sprintf("J%s near\t[i: os 0f %s rel]\t386", c.name, hex2(0x80+c.cc)))
	}
	return out
}

// genEightfold instantiates a set of templates for each family member and
// runs them through parseLine + the size expander.
func genEightfold(members []eightfoldMember, templates []string) []Form {
	var out []Form
	for _, m := range members {
		base := m.n << 3
		repl := strings.NewReplacer(
			"$op", m.name,
			"$00", hex2(base),
			"$02", hex2(base+2),
			"$04", hex2(base+4),
			"$05", hex2(base+5),
			"$n", strconv.Itoa(m.n),
		)
		for _, tmpl := range templates {
			f, ok := parseLine(repl.Replace(tmpl))
			if !ok {
				continue
			}
			if f.Macro != "" {
				out = append(out, expandForm(f)...)
			} else {
				out = append(out, f)
			}
		}
	}
	return out
}
