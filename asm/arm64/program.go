package arm64

import (
	"strconv"
	"strings"
)

// LineSpan locates one assembled instruction: its 0-based source line, its
// start address (origin 0), and its byte length (always 4).
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
// flat byte stream, resolving branch targets to labels. Origin is 0.
func AssembleProgram(src string) ([]byte, error) {
	l, err := AssembleListing(src)
	if err != nil {
		return nil, err
	}
	return l.Bytes, nil
}

// AssembleListing is AssembleProgram with per-line spans retained. Every
// AArch64 instruction is 4 bytes, so addresses are known in a single pass — no
// branch relaxation needed.
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
		addr += 4
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

// CollectLabels returns the label→address map of a program. Instruction size
// is a fixed 4 bytes, so addresses are exact without assembling. Useful for
// assembling individual lines (editor tooling).
func CollectLabels(src string) map[string]int64 {
	labels := map[string]int64{}
	addr := int64(0)
	for _, raw := range strings.Split(src, "\n") {
		line, ok := stripLabels(raw, func(name string) { labels[name] = addr })
		if ok && line != "" {
			addr += 4
		}
	}
	return labels
}

// AssembleLine assembles a single source line — comment and leading label(s)
// stripped — resolving a branch target against labels (typically from
// CollectLabels). For editor tooling. A label-only or blank line yields nil.
func AssembleLine(line string, labels map[string]int64) ([]byte, error) {
	s, ok := stripLabels(line, nil)
	if !ok || s == "" {
		return nil, nil
	}
	return assembleAt(s, 0, labels)
}

// stripLabels removes a comment and any leading "label:" definitions from a
// line, calling onLabel for each. It returns the remaining instruction text;
// ok=false only for a pure comment/blank line. A label-only line returns
// ("", true) after firing the callback(s).
func stripLabels(raw string, onLabel func(string)) (string, bool) {
	line := raw
	if i := strings.Index(line, "//"); i >= 0 {
		line = line[:i]
	}
	if i := strings.IndexByte(line, ';'); i >= 0 {
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

// assembleAt encodes one instruction at address addr. For a branch whose
// target operand names a label, the label is resolved to a PC-relative byte
// offset (target − addr); everything else falls through to Assemble.
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
// target that may be a label (b/bl, b.<cond>, cbz/cbnz). Register-indirect
// branches (br/blr/ret) are excluded.
func isLabelBranch(mnem string) bool {
	switch mnem {
	case "b", "bl", "cbz", "cbnz", "tbz", "tbnz":
		return true
	}
	return strings.HasPrefix(mnem, "b.")
}
