package main

import (
	"fmt"
	"sort"
	"strings"

	riscv "github.com/jtolio/tinyemu-go/asm/riscv"
)

// RISC-V mnemonic set + sorted list, for diagnostics and completion.
var (
	rvMnemonicSet  = map[string]bool{}
	rvMnemonicList []string
)

func init() {
	for _, m := range riscv.Mnemonics() {
		if !rvMnemonicSet[m] {
			rvMnemonicSet[m] = true
			rvMnemonicList = append(rvMnemonicList, m)
		}
	}
	sort.Strings(rvMnemonicList)
}

// rvInstr returns the instruction part of a RISC-V source line: comment ('#'
// or ';') and a leading "label:" removed.
func rvInstr(line string) string {
	if i := strings.IndexAny(line, "#;"); i >= 0 {
		line = line[:i]
	}
	s := strings.TrimSpace(line)
	if c := strings.IndexByte(s, ':'); c >= 0 && isLabelName(s[:c]) {
		s = strings.TrimSpace(s[c+1:])
	}
	return s
}

// lineDiagnosticRV is the RISC-V diagnostic: unknown mnemonic → error, known
// but unencodable → hint, clean → none. labels (from the whole buffer) let a
// branch/jal to a label resolve.
func lineDiagnosticRV(line string, labels map[string]int64) *diagnostic {
	insn := rvInstr(line)
	if insn == "" {
		return nil
	}
	mnem := strings.ToLower(firstWord(insn))
	_, err := riscv.AssembleLine(line, labels)
	if err == nil {
		return nil
	}
	if rvMnemonicSet[mnem] {
		return &diagnostic{severity: 3, message: "riscv: cannot encode (" + cleanErr(err) + ")"}
	}
	return &diagnostic{severity: 1, message: "unknown instruction: " + mnem}
}

// hoverRV shows a RISC-V instruction's bytes and canonical disassembly.
func hoverRV(line string, labels map[string]int64) string {
	insn := rvInstr(line)
	if insn == "" {
		return ""
	}
	mnem := strings.ToLower(firstWord(insn))
	if !rvMnemonicSet[mnem] {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** (RISC-V)\n\n", mnem)
	if bytes, err := riscv.AssembleLine(line, labels); err == nil && len(bytes) > 0 {
		fmt.Fprintf(&b, "encodes to `%s` (%d bytes)\n\n", bytesHex(bytes), len(bytes))
		if text, n, derr := riscv.Disassemble(bytes); derr == nil && n == len(bytes) {
			fmt.Fprintf(&b, "decodes to `%s`\n", text)
		}
	}
	return b.String()
}

// completionsRV returns RISC-V mnemonic completions for a prefix.
func completionsRV(prefix string) []string {
	low := strings.ToLower(prefix)
	var out []string
	for _, m := range rvMnemonicList {
		if strings.HasPrefix(m, low) {
			out = append(out, m)
		}
	}
	return out
}
