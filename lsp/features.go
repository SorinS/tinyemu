package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jtolio/tinyemu-go/asm"
	"github.com/jtolio/tinyemu-go/asm/emu"
)

// mnemonics is the set + sorted list of instruction mnemonics from the table,
// used for completion and to distinguish a typo from an unsupported encoding.
var (
	mnemonicSet  = map[string]bool{}
	mnemonicList []string
)

func init() {
	for _, f := range asm.Table() {
		if !mnemonicSet[f.Mnemonic] {
			mnemonicSet[f.Mnemonic] = true
			mnemonicList = append(mnemonicList, f.Mnemonic)
		}
	}
	sort.Strings(mnemonicList)
}

// directives are assembler keywords that are not instructions; lines starting
// with one are not diagnosed as instructions.
var directives = map[string]bool{
	"BITS": true, "SECTION": true, "SEGMENT": true, "GLOBAL": true, "EXTERN": true,
	"DEFAULT": true, "ORG": true, "TIMES": true, "EQU": true, "ALIGN": true, "ALIGNB": true,
	"DB": true, "DW": true, "DD": true, "DQ": true, "DT": true, "DO": true, "DY": true, "DZ": true,
	"RESB": true, "RESW": true, "RESD": true, "RESQ": true, "INCBIN": true, "CPU": true, "USE64": true,
}

// stripComment removes a NASM ';' line comment.
func stripComment(s string) string {
	if c := strings.IndexByte(s, ';'); c >= 0 {
		return s[:c]
	}
	return s
}

// instructionText returns the instruction part of a source line: the text with
// a leading "label:" (and comment) removed, trimmed. Empty if the line has no
// instruction (blank / comment / label-only).
func instructionText(line string) string {
	s := strings.TrimSpace(stripComment(line))
	if c := strings.IndexByte(s, ':'); c >= 0 && isLabelName(s[:c]) {
		s = strings.TrimSpace(s[c+1:])
	}
	return s
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func isLabelName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || c == '.' || c == '$' || c == '@' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// lineDiagnostic analyzes one source line. It returns a diagnostic (or nil) and
// the assembled-bytes hex (empty unless the line assembled cleanly). Severity:
// 1=Error, 3=Information.
type diagnostic struct {
	severity int
	message  string
}

func lineDiagnostic(line string, labels map[string]int64) (*diagnostic, string) {
	insn := instructionText(line)
	if insn == "" {
		return nil, ""
	}
	mnem := strings.ToUpper(firstWord(insn))
	if directives[mnem] {
		return nil, ""
	}
	b, err := asm.AssembleLine(line, labels)
	if err == nil {
		return nil, bytesHex(b)
	}
	msg := cleanErr(err)
	if strings.Contains(msg, "undefined branch target") {
		return &diagnostic{severity: 1, message: "asm: " + msg}, ""
	}
	if mnemonicSet[mnem] {
		// Real instruction, but our encoder doesn't reach it yet — a hint,
		// not an error, so valid code isn't flagged red.
		return &diagnostic{severity: 3, message: "asm: encoding not yet supported (" + msg + ")"}, ""
	}
	return &diagnostic{severity: 1, message: "unknown instruction: " + firstWord(insn)}, ""
}

// hover returns markdown describing the instruction on a line: its assembled
// bytes (if any) and the matching table forms (operand signatures + flags).
func hover(line string, labels map[string]int64) string {
	insn := instructionText(line)
	if insn == "" {
		return ""
	}
	mnem := strings.ToUpper(firstWord(insn))
	if !mnemonicSet[mnem] {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**\n\n", mnem)
	if bytes, err := asm.AssembleLine(line, labels); err == nil && len(bytes) > 0 {
		fmt.Fprintf(&b, "encodes to `%s` (%d bytes)\n\n", bytesHex(bytes), len(bytes))
	}
	b.WriteString("forms:\n")
	n := 0
	for _, f := range asm.Table() {
		if f.Mnemonic != mnem {
			continue
		}
		ops := "(none)"
		if len(f.Operands) > 0 {
			ops = strings.Join(f.Operands, ", ")
		}
		fmt.Fprintf(&b, "- `%s %s`\n", mnem, ops)
		if n++; n >= 12 {
			fmt.Fprintf(&b, "- …\n")
			break
		}
	}
	return b.String()
}

// completions returns mnemonic and register completions for a prefix.
func completions(prefix string) []string {
	up := strings.ToUpper(prefix)
	var out []string
	for _, m := range mnemonicList {
		if strings.HasPrefix(m, up) {
			out = append(out, m)
		}
	}
	return out
}

func bytesHex(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, " ")
}

// formatLineState renders the register/flag changes a line made as a compact
// inline annotation, e.g. "rax=0x5 rdi=0x8 ZF=1". Empty if the line changed
// nothing observable (the caller then skips the annotation).
func formatLineState(ls emu.LineState) string {
	parts := make([]string, 0, len(ls.Changed)+len(ls.Flags))
	for _, rv := range ls.Changed {
		parts = append(parts, fmt.Sprintf("%s=%#x", rv.Name, rv.Value))
	}
	for _, rv := range ls.Flags {
		parts = append(parts, fmt.Sprintf("%s=%d", rv.Name, rv.Value))
	}
	return strings.Join(parts, " ")
}

// cleanErr trims the "asm \"…\": " prefix Assemble adds, for a tidier message.
func cleanErr(err error) string {
	s := err.Error()
	if i := strings.Index(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}
