package asm

import (
	"fmt"
	"strings"
)

// LineSpan locates one assembled instruction in the output: its source line
// (0-based), its start address (origin 0), and its byte length. It lets
// tooling map an address back to the source line that produced it.
type LineSpan struct {
	Line int   // 0-based source line index
	Addr int64 // start address from origin 0
	Len  int   // number of bytes
}

// Listing is the result of assembling a program with source positions
// retained — for debuggers, run-to-cursor, and other address↔line tooling.
type Listing struct {
	Bytes []byte     // the flat byte stream (== AssembleProgram output)
	Spans []LineSpan // one entry per instruction line that emitted bytes
}

// AssembleProgram assembles a multi-line NASM/Intel program (labels +
// instructions) in 64-bit mode to a flat byte stream, resolving relative
// branches (jmp/call/jcc) to labels. Branch displacement size (rel8 vs rel32)
// is chosen to match nasm — short when the target fits, near otherwise —
// settled by a fixed-point pass since each size choice shifts later labels.
//
// Origin is 0; labels are simple identifiers; numeric branch targets are
// treated as absolute addresses from that origin.
func AssembleProgram(src string) ([]byte, error) {
	l, err := AssembleListing(src)
	if err != nil {
		return nil, err
	}
	return l.Bytes, nil
}

// AssembleListing assembles a program like AssembleProgram, additionally
// recording where each instruction line's bytes landed (address + length).
func AssembleListing(src string) (*Listing, error) {
	type item struct {
		label   string // non-empty for a label definition
		insn    string // instruction source otherwise
		srcLine int    // 0-based source line of an instruction item
		bytes   []byte
	}
	var items []item

	for ln, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(stripComment(raw))
		for line != "" {
			c := strings.IndexByte(line, ':')
			if c < 0 || !isLabelName(line[:c]) {
				break
			}
			items = append(items, item{label: strings.TrimSpace(line[:c])})
			line = strings.TrimSpace(line[c+1:])
		}
		if line != "" {
			items = append(items, item{insn: line, srcLine: ln})
		}
	}

	for pass := 0; pass < 20; pass++ {
		labels := map[string]int64{}
		addr := int64(0)
		for i := range items {
			if items[i].label != "" {
				labels[items[i].label] = addr
			} else {
				addr += int64(len(items[i].bytes))
			}
		}
		changed := false
		addr = 0
		for i := range items {
			if items[i].label != "" {
				continue
			}
			b, err := assembleInsn(items[i].insn, addr, labels)
			if err != nil {
				return nil, err
			}
			if len(b) != len(items[i].bytes) {
				changed = true
			}
			items[i].bytes = b
			addr += int64(len(b))
		}
		if !changed {
			break
		}
	}

	out := &Listing{}
	addr := int64(0)
	for i := range items {
		if items[i].label != "" {
			continue
		}
		if len(items[i].bytes) > 0 {
			out.Spans = append(out.Spans, LineSpan{
				Line: items[i].srcLine,
				Addr: addr,
				Len:  len(items[i].bytes),
			})
		}
		out.Bytes = append(out.Bytes, items[i].bytes...)
		addr += int64(len(items[i].bytes))
	}
	return out, nil
}

// CollectLabels returns the label names defined in a program, each mapped to
// 0. It lets editor tooling assemble individual lines (e.g. for diagnostics)
// with relative-branch targets resolved — the addresses are irrelevant to
// whether a line is valid.
func CollectLabels(src string) map[string]int64 {
	labels := map[string]int64{}
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(stripComment(raw))
		for line != "" {
			c := strings.IndexByte(line, ':')
			if c < 0 || !isLabelName(line[:c]) {
				break
			}
			labels[strings.TrimSpace(line[:c])] = 0
			line = strings.TrimSpace(line[c+1:])
		}
	}
	return labels
}

// AssembleLine assembles a single source line — stripping any leading label
// and comment — resolving a relative branch against labels (typically from
// CollectLabels over the whole buffer). It is the line-level counterpart of
// AssembleProgram, for editor tooling. A label-only or blank line yields
// (nil, nil).
func AssembleLine(line string, labels map[string]int64) ([]byte, error) {
	s := strings.TrimSpace(stripComment(line))
	if c := strings.IndexByte(s, ':'); c >= 0 && isLabelName(s[:c]) {
		s = strings.TrimSpace(s[c+1:])
	}
	if s == "" {
		return nil, nil
	}
	return assembleInsn(s, 0, labels)
}

// assembleInsn encodes one instruction at address addr, resolving a relative
// branch to a label/numeric target; everything else falls to Assemble.
func assembleInsn(src string, addr int64, labels map[string]int64) ([]byte, error) {
	mnem, ops := parseInsn(src)
	if len(ops) == 1 && isBranch(mnem) {
		opnd := strings.TrimSpace(ops[0])
		opnd = strings.TrimPrefix(opnd, "short ")
		opnd = strings.TrimPrefix(opnd, "near ")
		opnd = strings.TrimSpace(opnd)
		// A register/memory operand is an indirect branch — not relative.
		if _, isReg := gprByName[strings.ToLower(opnd)]; !isReg && !strings.HasPrefix(opnd, "[") {
			target, ok := resolveTarget(opnd, labels)
			if !ok {
				return nil, fmt.Errorf("asm: undefined branch target %q", opnd)
			}
			return encodeRelJump(mnem, target, addr)
		}
	}
	return Assemble(src)
}

// encodeRelJump encodes a relative branch to target from address addr,
// preferring the short (rel8) form when the displacement fits, else near
// (rel32) — matching nasm's branch relaxation.
func encodeRelJump(mnem string, target, addr int64) ([]byte, error) {
	if short := findBranchForm(mnem, "short"); short != nil {
		probe, err := encodeForm(short, []operand{{kind: opTarget}})
		if err != nil {
			return nil, err
		}
		disp := target - (addr + int64(len(probe)))
		if fitsSigned(disp, 8) {
			return encodeForm(short, []operand{{kind: opTarget, imm: disp}})
		}
	}
	near := findBranchForm(mnem, "near")
	if near == nil {
		return nil, fmt.Errorf("asm: %s target out of rel8 range and no near form", mnem)
	}
	probe, err := encodeForm(near, []operand{{kind: opTarget}})
	if err != nil {
		return nil, err
	}
	disp := target - (addr + int64(len(probe)))
	return encodeForm(near, []operand{{kind: opTarget, imm: disp}})
}

func findBranchForm(mnem, kind string) *Form {
	for i := range table {
		f := &table[i]
		if f.Mnemonic == mnem && len(f.Operands) == 1 && f.Operands[0] == kind {
			return f
		}
	}
	return nil
}

func isBranch(mnem string) bool {
	return findBranchForm(mnem, "short") != nil || findBranchForm(mnem, "near") != nil
}

// resolveTarget resolves a branch operand to an absolute address: a known
// label, or a numeric literal.
func resolveTarget(s string, labels map[string]int64) (int64, bool) {
	if a, ok := labels[s]; ok {
		return a, true
	}
	return parseImm(s)
}

func stripComment(s string) string {
	if c := strings.IndexByte(s, ';'); c >= 0 {
		return s[:c]
	}
	return s
}

// isLabelName reports whether s is a plausible NASM label (no spaces; starts
// with a letter, '_', '.', or '$').
func isLabelName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	c := s[0]
	if !(c == '_' || c == '.' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || c == '.' || c == '$' || c == '@' || (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}
