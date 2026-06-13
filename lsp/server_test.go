package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
