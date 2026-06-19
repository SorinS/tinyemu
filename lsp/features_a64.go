package main

import (
	"fmt"
	"sort"
	"strings"

	a64 "github.com/sorins/tinyemu-go/asm/arm64"
)

// AArch64 mnemonic set + sorted list, for diagnostics and completion.
var (
	a64MnemonicSet  = map[string]bool{}
	a64MnemonicList []string
)

func init() {
	for _, m := range a64.Mnemonics() {
		if !a64MnemonicSet[m] {
			a64MnemonicSet[m] = true
			a64MnemonicList = append(a64MnemonicList, m)
		}
	}
	sort.Strings(a64MnemonicList)
}

// a64Instr returns the instruction part of an AArch64 source line: comment
// ('//' or ';') and a leading "label:" removed.
func a64Instr(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		line = line[:i]
	}
	if i := strings.IndexByte(line, ';'); i >= 0 {
		line = line[:i]
	}
	s := strings.TrimSpace(line)
	if c := strings.IndexByte(s, ':'); c >= 0 && isLabelName(s[:c]) {
		s = strings.TrimSpace(s[c+1:])
	}
	return s
}

// lineDiagnosticA64 is the AArch64 diagnostic: unknown mnemonic → error, known
// but unencodable → hint, clean → none.
func lineDiagnosticA64(line string, labels map[string]int64) *diagnostic {
	insn := a64Instr(line)
	if insn == "" {
		return nil
	}
	mnem := strings.ToLower(firstWord(insn))
	if _, err := a64.AssembleLine(line, labels); err == nil {
		return nil
	} else if a64MnemonicSet[mnem] {
		return &diagnostic{severity: 3, message: "arm64: cannot encode (" + cleanErr(err) + ")"}
	}
	return &diagnostic{severity: 1, message: "unknown instruction: " + mnem}
}

// hoverA64 shows an AArch64 instruction's bytes and canonical disassembly.
func hoverA64(line string, labels map[string]int64) string {
	insn := a64Instr(line)
	if insn == "" {
		return ""
	}
	mnem := strings.ToLower(firstWord(insn))
	if !a64MnemonicSet[mnem] {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** (AArch64)\n\n", mnem)
	if bytes, err := a64.AssembleLine(line, labels); err == nil && len(bytes) > 0 {
		fmt.Fprintf(&b, "encodes to `%s` (%d bytes)\n\n", bytesHex(bytes), len(bytes))
		if text, derr := a64.DisassembleBytes(bytes); derr == nil {
			fmt.Fprintf(&b, "decodes to `%s`\n", text)
		}
	}
	return b.String()
}

// completionsA64 returns AArch64 mnemonic completions for a prefix.
func completionsA64(prefix string) []string {
	low := strings.ToLower(prefix)
	var out []string
	for _, m := range a64MnemonicList {
		if strings.HasPrefix(m, low) {
			out = append(out, m)
		}
	}
	return out
}
