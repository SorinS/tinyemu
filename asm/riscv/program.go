package riscv

import "strings"

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

// AssembleListing assembles each instruction line of a program to a flat byte
// stream, recording where each landed. Blank lines, comments, and bare label
// definitions ("loop:") are skipped — label *resolution* for branch targets is
// not yet supported (numeric offsets only).
func AssembleListing(src string) (*Listing, error) {
	out := &Listing{}
	addr := int64(0)
	for ln, raw := range strings.Split(src, "\n") {
		line := raw
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || isLabelOnly(line) {
			continue
		}
		b, err := Assemble(line)
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			continue
		}
		out.Spans = append(out.Spans, LineSpan{Line: ln, Addr: addr, Len: len(b)})
		out.Bytes = append(out.Bytes, b...)
		addr += int64(len(b))
	}
	return out, nil
}

// isLabelOnly reports whether a (comment-stripped) line is just a label
// definition like "loop:".
func isLabelOnly(line string) bool {
	return strings.HasSuffix(line, ":") && !strings.ContainsAny(line[:len(line)-1], " \t")
}
