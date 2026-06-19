package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sorins/tinyemu-go/asm/emu"
)

func TestServerFlow(t *testing.T) {
	srv := &server{docs: map[string]string{}}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	srv.handle(&rpcMessage{Method: "initialize", ID: json.RawMessage("1")}, w)
	w.Flush()
	if !strings.Contains(buf.String(), "hoverProvider") {
		t.Fatalf("initialize result missing capabilities:\n%s", buf.String())
	}

	buf.Reset()
	p, _ := json.Marshal(didOpenParams{TextDocument: textDocumentItem{
		URI: "file:///t.asm", Text: "  mov rax, rbx\n  badmnem rax\nstart:\n  ret\n",
	}})
	srv.handle(&rpcMessage{Method: "textDocument/didOpen", Params: p}, w)
	w.Flush()
	out := buf.String()
	if !strings.Contains(out, "publishDiagnostics") {
		t.Fatal("no diagnostics notification")
	}
	if !strings.Contains(out, "unknown instruction") {
		t.Errorf("expected an 'unknown instruction' diagnostic for badmnem:\n%s", out)
	}
	if strings.Count(out, `"message"`) != 1 {
		t.Errorf("expected exactly one diagnostic (only badmnem), got:\n%s", out)
	}

	buf.Reset()
	hp, _ := json.Marshal(posParams{TextDocument: textDocumentID{URI: "file:///t.asm"}, Position: position{Line: 0, Character: 4}})
	srv.handle(&rpcMessage{Method: "textDocument/hover", ID: json.RawMessage("2"), Params: hp}, w)
	w.Flush()
	if !strings.Contains(buf.String(), "48 89 d8") {
		t.Errorf("hover over 'mov rax, rbx' should show its bytes:\n%s", buf.String())
	}
}

// A clean buffer must publish "diagnostics":[] (an array), never null —
// Neovim's diagnostic handler does #diagnostics and throws on a null value.
func TestServerCleanDiagnosticsIsArray(t *testing.T) {
	srv := &server{docs: map[string]string{}}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	p, _ := json.Marshal(didOpenParams{TextDocument: textDocumentItem{
		URI: "file:///clean.asm", Text: "  mov rax, rbx\n  ret\n",
	}})
	srv.handle(&rpcMessage{Method: "textDocument/didOpen", Params: p}, w)
	w.Flush()
	if !strings.Contains(buf.String(), `"diagnostics":[]`) {
		t.Errorf("clean buffer should publish an empty array, got:\n%s", buf.String())
	}
}

func TestServerRun(t *testing.T) {
	srv := &server{docs: map[string]string{
		"file:///r.asm": "  mov rax, 5\n  add rax, 3\n  ret\n",
	}}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	rp, _ := json.Marshal(runParams{TextDocument: textDocumentID{URI: "file:///r.asm"}, Line: -1})
	srv.handle(&rpcMessage{Method: "asm/run", ID: json.RawMessage("3"), Params: rp}, w)
	w.Flush()

	var resp struct {
		Result runResult `json:"result"`
	}
	body := buf.String()
	if i := strings.Index(body, "\r\n\r\n"); i >= 0 {
		body = body[i+4:]
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if resp.Result.Stop != "completed" {
		t.Fatalf("stop = %q, want completed:\n%s", resp.Result.Stop, buf.String())
	}
	// line 1 (add rax, 3) leaves rax = 8.
	var found bool
	for _, l := range resp.Result.Lines {
		if l.Line == 1 && strings.Contains(l.Text, "rax=0x8") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected line 1 annotation 'rax=0x8', got %+v", resp.Result.Lines)
	}
}

func TestServerDebug(t *testing.T) {
	srv := &server{
		docs:     map[string]string{"file:///d.asm": "  mov rax, 5\n  add rax, 3\n  hlt\n"},
		sessions: map[string]*emu.Session{},
	}
	p := runParams{TextDocument: textDocumentID{URI: "file:///d.asm"}}
	// start → armed at entry, nothing run
	st := srv.debugStart(p)
	if st.Line != 0 || st.Steps != 0 {
		t.Fatalf("start: line=%d steps=%d, want 0/0", st.Line, st.Steps)
	}
	// step → mov rax,5
	st = srv.debugStep(p)
	var rax uint64
	for _, r := range st.Regs {
		if r.Name == "rax" {
			rax = r.Value
		}
	}
	if rax != 5 {
		t.Errorf("after step rax=%d, want 5", rax)
	}
	srv.debugStop(p)
	if srv.sessions["file:///d.asm"] != nil {
		t.Errorf("session not cleaned up after stop")
	}
}
