package main

import (
	"strings"
	"testing"
)

func TestLineDiagnostic(t *testing.T) {
	cases := []struct {
		line     string
		wantDiag bool
		sev      int
	}{
		{"  mov rax, rbx", false, 0}, // valid → no diagnostic
		{"start:", false, 0},         // label
		{"  ; a comment", false, 0},  // comment
		{"  BITS 64", false, 0},      // directive
		{"  add rax, [rbx+rcx*4]", false, 0},
		{"  movxx rax, rbx", true, 1},          // unknown mnemonic → error
		{"  vaddps xmm0, xmm1, xmm2", true, 3}, // real insn, unsupported → hint
	}
	for _, c := range cases {
		d, _ := lineDiagnostic(c.line)
		if (d != nil) != c.wantDiag {
			t.Errorf("lineDiagnostic(%q): diag=%v, want %v", c.line, d, c.wantDiag)
			continue
		}
		if d != nil && d.severity != c.sev {
			t.Errorf("lineDiagnostic(%q): severity=%d, want %d (%q)", c.line, d.severity, c.sev, d.message)
		}
	}
}

func TestHover(t *testing.T) {
	h := hover("  mov rax, rbx")
	if !strings.Contains(h, "MOV") || !strings.Contains(h, "48 89 d8") {
		t.Errorf("hover(mov rax,rbx) missing mnemonic/bytes:\n%s", h)
	}
	if hover("  notaninsn x, y") != "" {
		t.Errorf("hover on unknown mnemonic should be empty")
	}
}

func TestCompletions(t *testing.T) {
	got := completions("PUS")
	found := false
	for _, m := range got {
		if m == "PUSH" {
			found = true
		}
	}
	if !found {
		t.Errorf("completions(PUS) = %v, want to include PUSH", got)
	}
}
