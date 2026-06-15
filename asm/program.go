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
	mode := DetectBits(src)
	type item struct {
		label   string // non-empty for a label definition
		insn    string // instruction source otherwise
		srcLine int    // 0-based source line of an instruction item
		bytes   []byte
	}
	var items []item

	for ln, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(stripComment(raw))
		if isBitsLine(line) {
			continue // the BITS directive selects mode (DetectBits); emits no bytes
		}
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
			b, err := assembleInsn(items[i].insn, addr, labels, mode)
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

// AssembleLine assembles a single source line in 64-bit mode — see
// AssembleLineMode for an explicit mode.
func AssembleLine(line string, labels map[string]int64) ([]byte, error) {
	return AssembleLineMode(line, labels, Bits64)
}

// AssembleLineMode assembles a single source line — stripping any leading
// label and comment — resolving a relative branch against labels (typically
// from CollectLabels over the whole buffer). It is the line-level counterpart
// of AssembleProgram, for editor tooling. A label-only or blank line yields
// (nil, nil).
func AssembleLineMode(line string, labels map[string]int64, mode Mode) ([]byte, error) {
	s := strings.TrimSpace(stripComment(line))
	if c := strings.IndexByte(s, ':'); c >= 0 && isLabelName(s[:c]) {
		s = strings.TrimSpace(s[c+1:])
	}
	if s == "" {
		return nil, nil
	}
	return assembleInsn(s, 0, labels, mode)
}

// isBitsLine reports whether a (comment-stripped, trimmed) line is a BITS
// directive — "BITS 32", "BITS 64", or the bracketed "[BITS 32]" form.
func isBitsLine(line string) bool {
	line = strings.TrimPrefix(line, "[")
	line = strings.TrimSuffix(line, "]")
	fields := strings.Fields(line)
	return len(fields) == 2 && strings.EqualFold(fields[0], "BITS")
}

// DetectBits returns the CPU mode a program targets, read from a NASM "BITS
// 32" / "BITS 64" directive (also the bracketed "[BITS 32]" form). Defaults to
// Bits64 when no directive is present.
func DetectBits(src string) Mode {
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(stripComment(raw))
		line = strings.TrimPrefix(line, "[")
		line = strings.TrimSuffix(line, "]")
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], "BITS") {
			switch fields[1] {
			case "32":
				return Bits32
			case "64":
				return Bits64
			}
		}
	}
	return Bits64
}

// assembleInsn encodes one instruction at address addr, resolving a relative
// branch or a symbol memory operand against the label table; everything else
// falls to Assemble.
func assembleInsn(src string, addr int64, labels map[string]int64, mode Mode) ([]byte, error) {
	// A rep/lock prefix sits on the same line as the instruction it modifies;
	// emit the prefix byte, then assemble the rest one byte further along (so a
	// RIP-relative operand or branch in the prefixed instruction still resolves
	// against its true address).
	if pfx, rest, ok := splitPrefix(src); ok {
		b, err := assembleInsn(rest, addr+1, labels, mode)
		if err != nil {
			return nil, err
		}
		return append([]byte{pfx}, b...), nil
	}
	mnem, opStrs := parseInsn(src)
	if mnem == "" {
		return nil, nil
	}
	if _, ok := dataWidths[mnem]; ok {
		return assembleData(dataWidths[mnem], opStrs)
	}
	if len(opStrs) == 1 && isBranch(mnem) {
		opnd := strings.TrimSpace(opStrs[0])
		opnd = strings.TrimPrefix(opnd, "short ")
		opnd = strings.TrimPrefix(opnd, "near ")
		opnd = strings.TrimSpace(opnd)
		// A register/memory operand is an indirect branch — not relative.
		if _, isReg := gprByName[strings.ToLower(opnd)]; !isReg && !strings.HasPrefix(opnd, "[") {
			target, ok := resolveTarget(opnd, labels)
			if !ok {
				return nil, fmt.Errorf("asm: undefined branch target %q", opnd)
			}
			return encodeRelJump(mnem, target, addr, mode)
		}
	}
	ops := make([]operand, len(opStrs))
	hasSym := false
	for i, s := range opStrs {
		op, ok := parseOperand(s)
		if !ok {
			return nil, fmt.Errorf("asm %q: cannot parse operand %q", src, s)
		}
		ops[i] = op
		if op.memSym != "" {
			hasSym = true
		}
	}
	if !hasSym {
		return encodeOps(src, mnem, ops, mode)
	}
	if err := resolveMemSyms(src, mnem, ops, addr, labels, mode); err != nil {
		return nil, err
	}
	return encodeOps(src, mnem, ops, mode)
}

// resolveMemSyms resolves a symbol in a memory operand to a displacement. A
// [rel sym] is RIP-relative (origin-independent — the form that runs correctly
// in the loaded image); a bare [sym] is absolute. At most one symbol per
// instruction (x86 allows a single memory operand).
func resolveMemSyms(src, mnem string, ops []operand, addr int64, labels map[string]int64, mode Mode) error {
	var sym *operand
	for i := range ops {
		if ops[i].memSym != "" {
			sym = &ops[i]
			break
		}
	}
	if sym == nil {
		return nil
	}
	symAddr, ok := labels[sym.memSym]
	if !ok {
		return fmt.Errorf("asm %q: undefined symbol %q", src, sym.memSym)
	}
	name := sym.memSym
	sym.memSym = "" // resolved; encode from memDisp/memRip below
	if !sym.memRip {
		sym.memDisp += symAddr // absolute
		sym.memHasDisp = true
		return nil
	}
	// RIP-relative: disp = sym − (next-insn address). The displacement is always
	// disp32, so the instruction length is fixed; probe it once with disp 0.
	base := sym.memDisp
	sym.memDisp = 0
	probe, err := encodeOps(src, mnem, ops, mode)
	if err != nil {
		sym.memSym = name // restore for a useful error
		return err
	}
	sym.memDisp = base + symAddr - (addr + int64(len(probe)))
	return nil
}

// encodeRelJump encodes a relative branch to target from address addr,
// preferring the short (rel8) form when the displacement fits, else near
// (rel32) — matching nasm's branch relaxation.
func encodeRelJump(mnem string, target, addr int64, mode Mode) ([]byte, error) {
	if short := findBranchForm(mnem, "short"); short != nil {
		probe, err := encodeForm(short, []operand{{kind: opTarget}}, mode)
		if err != nil {
			return nil, err
		}
		disp := target - (addr + int64(len(probe)))
		if fitsSigned(disp, 8) {
			return encodeForm(short, []operand{{kind: opTarget, imm: disp}}, mode)
		}
	}
	near := findBranchForm(mnem, "near")
	if near == nil {
		return nil, fmt.Errorf("asm: %s target out of rel8 range and no near form", mnem)
	}
	probe, err := encodeForm(near, []operand{{kind: opTarget}}, mode)
	if err != nil {
		return nil, err
	}
	disp := target - (addr + int64(len(probe)))
	return encodeForm(near, []operand{{kind: opTarget, imm: disp}}, mode)
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
