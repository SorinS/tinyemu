// Command go-asm is a Language Server for NASM/Intel x86-64 assembly, backed
// by the tinyemu-go assembler. It speaks LSP over stdin/stdout (for Neovim's
// built-in client) and provides live diagnostics (does this line assemble?),
// hover (encoding + operand forms), and mnemonic completion.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jtolio/tinyemu-go/asm"
	"github.com/jtolio/tinyemu-go/asm/emu"
	riscv "github.com/jtolio/tinyemu-go/asm/riscv"
)

// labelsFor collects the label→address map for a buffer in its ISA's scheme.
func labelsFor(text string, arch emu.Arch) map[string]int64 {
	if arch == emu.ArchRISCV {
		return riscv.CollectLabels(text)
	}
	return asm.CollectLabels(text)
}

func main() {
	srv := &server{docs: map[string]string{}}
	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	for {
		msg, err := readMessage(r)
		if err != nil {
			return // stdin closed → exit
		}
		srv.handle(msg, w)
		w.Flush()
	}
}

// --- JSON-RPC framing -------------------------------------------------------

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func readMessage(r *bufio.Reader) (*rpcMessage, error) {
	length := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if length == 0 {
		return &rpcMessage{}, nil
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func writeMessage(w *bufio.Writer, msg any) {
	body, _ := json.Marshal(msg)
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body))
	w.Write(body)
}

// --- server -----------------------------------------------------------------

type server struct {
	docs map[string]string // uri -> full text
}

func (s *server) handle(msg *rpcMessage, w *bufio.Writer) {
	switch msg.Method {
	case "initialize":
		s.reply(w, msg.ID, initializeResult())
	case "initialized", "$/setTrace", "$/cancelRequest":
		// no-op notifications
	case "shutdown":
		s.reply(w, msg.ID, nil)
	case "exit":
		os.Exit(0)
	case "textDocument/didOpen":
		var p didOpenParams
		json.Unmarshal(msg.Params, &p)
		s.docs[p.TextDocument.URI] = p.TextDocument.Text
		s.publishDiagnostics(w, p.TextDocument.URI)
	case "textDocument/didChange":
		var p didChangeParams
		json.Unmarshal(msg.Params, &p)
		if len(p.ContentChanges) > 0 {
			s.docs[p.TextDocument.URI] = p.ContentChanges[len(p.ContentChanges)-1].Text
		}
		s.publishDiagnostics(w, p.TextDocument.URI)
	case "textDocument/didClose":
		var p didCloseParams
		json.Unmarshal(msg.Params, &p)
		delete(s.docs, p.TextDocument.URI)
	case "textDocument/hover":
		var p posParams
		json.Unmarshal(msg.Params, &p)
		s.reply(w, msg.ID, s.hover(p))
	case "textDocument/completion":
		var p posParams
		json.Unmarshal(msg.Params, &p)
		s.reply(w, msg.ID, s.completion(p))
	case "textDocument/signatureHelp":
		var p posParams
		json.Unmarshal(msg.Params, &p)
		s.reply(w, msg.ID, s.signatureHelp(p))
	case "asm/run":
		var p runParams
		json.Unmarshal(msg.Params, &p)
		s.reply(w, msg.ID, s.runProgram(p))
	default:
		if len(msg.ID) > 0 { // a request we don't handle — must answer
			s.reply(w, msg.ID, nil)
		}
	}
}

func (s *server) reply(w *bufio.Writer, id json.RawMessage, result any) {
	if len(id) == 0 {
		return
	}
	writeMessage(w, rpcMessage{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) notify(w *bufio.Writer, method string, params any) {
	writeMessage(w, struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", method, params})
}

// --- features ---------------------------------------------------------------

func (s *server) publishDiagnostics(w *bufio.Writer, uri string) {
	text := s.docs[uri]
	arch := emu.DetectArch(text)
	labels := labelsFor(text, arch)
	// Non-nil so JSON encodes "[]" not "null": some clients (Neovim's
	// diagnostic handler) do #diagnostics and throw on a null value.
	diags := []lspDiagnostic{}
	for i, line := range strings.Split(text, "\n") {
		d, _ := lineDiagnostic(line, labels, arch)
		if d == nil {
			continue
		}
		start := len(line) - len(strings.TrimLeft(line, " \t"))
		diags = append(diags, lspDiagnostic{
			Range:    rng(i, start, i, len(line)),
			Severity: d.severity,
			Source:   "asm",
			Message:  d.message,
		})
	}
	s.notify(w, "textDocument/publishDiagnostics", publishParams{URI: uri, Diagnostics: diags})
}

func (s *server) hover(p posParams) any {
	line := s.lineAt(p)
	doc := s.docs[p.TextDocument.URI]
	arch := emu.DetectArch(doc)
	md := hover(line, labelsFor(doc, arch), asm.DetectBits(doc), arch)
	if md == "" {
		return nil
	}
	return hoverResult{Contents: markupContent{Kind: "markdown", Value: md}}
}

func (s *server) completion(p posParams) any {
	line := s.lineAt(p)
	col := p.Position.Character
	if col > len(line) {
		col = len(line)
	}
	// Prefix = the word ending at the cursor.
	start := col
	for start > 0 {
		c := line[start-1]
		if c == ' ' || c == '\t' || c == ',' {
			break
		}
		start--
	}
	items := []completionItem{}
	if emu.DetectArch(s.docs[p.TextDocument.URI]) == emu.ArchRISCV {
		for _, m := range completionsRV(line[start:col]) {
			items = append(items, completionItem{Label: m, Kind: 3, Detail: "riscv instruction"})
			if len(items) >= 200 {
				break
			}
		}
		return items
	}
	for _, m := range completions(line[start:col]) {
		it := completionItem{Label: m, Kind: 3, Detail: "instruction"} // 3 = Function
		if doc := formsMarkdown(m, 8); doc != "" {
			it.Documentation = &markupContent{Kind: "markdown", Value: doc}
		}
		items = append(items, it)
		if len(items) >= 200 {
			break
		}
	}
	return items
}

func (s *server) signatureHelp(p posParams) any {
	if emu.DetectArch(s.docs[p.TextDocument.URI]) == emu.ArchRISCV {
		return nil // RISC-V operands are positional; no multi-form signature help
	}
	r := buildSignatureHelp(s.lineAt(p), p.Position.Character)
	if r == nil {
		return nil
	}
	return r
}

// runProgram is the custom "asm/run" request: assemble the buffer, execute it
// in the emulator, and return per-line register/flag changes for inline
// display. Run-to-cursor when Line >= 0; whole program otherwise. This is
// on-demand only (an editor command/keymap), never tied to didChange.
func (s *server) runProgram(p runParams) any {
	src := s.docs[p.TextDocument.URI]
	opts := emu.Options{StopBeforeLine: -1}
	if p.Line >= 0 {
		opts.StopBeforeLine = p.Line
	}
	if len(p.Breakpoints) > 0 {
		opts.Breakpoints = map[int]bool{}
		for _, l := range p.Breakpoints {
			opts.Breakpoints[l] = true
		}
	}
	res, err := emu.Run(src, opts)
	if err != nil {
		return runResult{Stop: "assemble-error", StopLine: -1, Error: cleanErr(err)}
	}
	out := runResult{
		Arch: res.Arch, Bits: res.Bits, Stop: res.Stop, StopLine: res.StopLine,
		Steps: res.Steps, Error: res.Error, Final: res.Final,
	}
	for _, ls := range res.Lines {
		out.Lines = append(out.Lines, runLine{Line: ls.Line, Text: formatLineState(ls), Regs: ls.Regs})
	}
	return out
}

func (s *server) lineAt(p posParams) string {
	lines := strings.Split(s.docs[p.TextDocument.URI], "\n")
	if p.Position.Line < 0 || p.Position.Line >= len(lines) {
		return ""
	}
	return lines[p.Position.Line]
}

// --- LSP types (minimal subset) --------------------------------------------

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

func rng(l1, c1, l2, c2 int) lspRange {
	return lspRange{position{l1, c1}, position{l2, c2}}
}

type textDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}
type textDocumentID struct {
	URI string `json:"uri"`
}
type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}
type contentChange struct {
	Text string `json:"text"`
}
type didChangeParams struct {
	TextDocument   textDocumentID  `json:"textDocument"`
	ContentChanges []contentChange `json:"contentChanges"`
}
type didCloseParams struct {
	TextDocument textDocumentID `json:"textDocument"`
}
type posParams struct {
	TextDocument textDocumentID `json:"textDocument"`
	Position     position       `json:"position"`
}
type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Source   string   `json:"source"`
	Message  string   `json:"message"`
}
type publishParams struct {
	URI         string          `json:"uri"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}
type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type hoverResult struct {
	Contents markupContent `json:"contents"`
}
type completionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind"`
	Detail        string         `json:"detail"`
	Documentation *markupContent `json:"documentation,omitempty"`
}

// signatureHelp request result types.
type parameterInformation struct {
	Label [2]int `json:"label"` // [start,end] offsets into the signature label
}
type signatureInformation struct {
	Label      string                 `json:"label"`
	Parameters []parameterInformation `json:"parameters"`
}
type signatureHelpResult struct {
	Signatures      []signatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

// asm/run request + result (custom, non-standard LSP method).
type runParams struct {
	TextDocument textDocumentID `json:"textDocument"`
	Line         int            `json:"line"`        // run-to-cursor line; <0 = whole program
	Breakpoints  []int          `json:"breakpoints"` // optional stop-before lines
}
type runLine struct {
	Line int          `json:"line"`           // 0-based source line
	Text string       `json:"text"`           // inline annotation, e.g. "rax=0x5 ZF=1"
	Regs []emu.RegVal `json:"regs,omitempty"` // full register file after this line
}
type runResult struct {
	Arch     string       `json:"arch"`     // "x86" or "riscv"
	Bits     int          `json:"bits"`     // 32 or 64
	Lines    []runLine    `json:"lines"`
	Final    []emu.RegVal `json:"final"`    // full register file when the run ended
	Stop     string       `json:"stop"`     // why the run ended
	StopLine int          `json:"stopLine"` // line about to execute when stopped, or -1
	Steps    int          `json:"steps"`
	Error    string       `json:"error,omitempty"`
}

func initializeResult() any {
	return map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync":      1, // full document sync
			"hoverProvider":         true,
			"completionProvider":    map[string]any{"triggerCharacters": []string{}},
			"signatureHelpProvider": map[string]any{"triggerCharacters": []string{" ", ","}},
		},
		"serverInfo": map[string]any{"name": "go-asm", "version": "0.1"},
	}
}
