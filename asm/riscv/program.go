package riscv

import (
	"strconv"
	"strings"
)

// LineSpan locates one assembled instruction: its 0-based source line, its
// start address (origin 0), and its byte length (always 4 here).
type LineSpan struct {
	Line int
	Addr int64
	Len  int
}

// Listing is an assembled program with per-line source positions, for tooling
// that maps an executing PC back to a source line.
type Listing struct {
	Bytes []byte
	Spans []LineSpan
}

// AssembleProgram assembles a multi-line program (labels + instructions) to a
// flat byte stream, resolving branch/jal targets to labels. Origin is 0.
func AssembleProgram(src string) ([]byte, error) {
	l, err := AssembleListing(src)
	if err != nil {
		return nil, err
	}
	return l.Bytes, nil
}

// AssembleListing is AssembleProgram with per-line spans retained. Because
// every RISC-V instruction is 4 bytes, addresses are known in a single pass —
// no branch relaxation needed.
func AssembleListing(src string) (*Listing, error) {
	type item struct {
		srcLine int
		insn    string
		addr    int64
	}
	var items []item
	labels := map[string]int64{}
	addr := int64(0)

	for ln, raw := range strings.Split(src, "\n") {
		line, ok := stripLabels(raw, func(name string) { labels[name] = addr })
		if !ok || line == "" {
			continue
		}
		items = append(items, item{ln, line, addr})
		addr += int64(insnSize(line))
	}

	out := &Listing{}
	for _, it := range items {
		b, err := assembleAt(it.insn, it.addr, labels)
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			continue
		}
		out.Spans = append(out.Spans, LineSpan{it.srcLine, it.addr, len(b)})
		out.Bytes = append(out.Bytes, b...)
	}
	return out, nil
}

// CollectLabels returns the label→address map of a program. Instruction sizes
// are fixed by mnemonic (4 bytes, or 2 for a compressed "c.*"), so addresses
// are exact without assembling. Useful for assembling individual lines.
func CollectLabels(src string) map[string]int64 {
	labels := map[string]int64{}
	addr := int64(0)
	for _, raw := range strings.Split(src, "\n") {
		line, ok := stripLabels(raw, func(name string) { labels[name] = addr })
		if ok && line != "" {
			addr += int64(insnSize(line))
		}
	}
	return labels
}

// insnSize returns an instruction's byte length from its mnemonic: 2 for a
// compressed (c.*) instruction, 4 otherwise.
func insnSize(line string) int {
	mnem, _ := parseLine(line)
	if isCompressed(mnem) {
		return 2
	}
	return 4
}

// AssembleLine assembles a single source line — comment and leading label(s)
// stripped — resolving a branch/jal target against labels (typically from
// CollectLabels). For editor tooling. A label-only or blank line yields nil.
func AssembleLine(line string, labels map[string]int64) ([]byte, error) {
	s, ok := stripLabels(line, nil)
	if !ok || s == "" {
		return nil, nil
	}
	return assembleAt(s, 0, labels)
}

// stripLabels removes a '#'/';' comment and any leading "label:" definitions
// from a line, calling onLabel for each. It returns the remaining instruction
// text and ok=false only if the line is purely a comment/blank (no callback
// fired, nothing to assemble). A label-only line returns ("", true) after
// firing the callback(s).
func stripLabels(raw string, onLabel func(string)) (string, bool) {
	line := raw
	if i := strings.IndexAny(line, "#;"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	for {
		c := strings.IndexByte(line, ':')
		if c < 0 || !isLabelName(line[:c]) {
			break
		}
		if onLabel != nil {
			onLabel(strings.TrimSpace(line[:c]))
		}
		line = strings.TrimSpace(line[c+1:])
		if line == "" {
			return "", true
		}
	}
	return line, true
}

// assembleAt encodes one instruction at address addr. For a branch/jal whose
// target operand names a label, the label is resolved to a PC-relative byte
// offset; everything else falls through to Assemble.
func assembleAt(src string, addr int64, labels map[string]int64) ([]byte, error) {
	mnem, ops := parseLine(src)
	if mnem == "" {
		return nil, nil
	}
	if isLabelBranch(mnem) && len(ops) > 0 {
		ti := len(ops) - 1
		if target, ok := labels[strings.TrimSpace(ops[ti])]; ok {
			ops = append([]string{}, ops...)
			ops[ti] = strconv.FormatInt(target-addr, 10)
			return Assemble(mnem + " " + strings.Join(ops, ", "))
		}
	}
	return Assemble(src)
}

// isLabelBranch reports whether a mnemonic's last operand is a PC-relative
// target that may be a label (conditional branches and jal/j; jalr is
// register-indirect and excluded).
func isLabelBranch(mnem string) bool {
	switch mnem {
	case "beq", "bne", "blt", "bge", "bltu", "bgeu", "beqz", "bnez", "jal", "j":
		return true
	}
	return false
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
