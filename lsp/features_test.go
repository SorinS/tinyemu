package main

import (
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/asm"
)

func TestLineDiagnostic(t *testing.T) {
	// Labels the buffer defines, so branch operands resolve.
	labels := map[string]int64{"done": 0, "start": 0}
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
		{"  je    done", false, 0},             // branch to a known label → no diagnostic
		{"  jmp   start", false, 0},            // branch to a known label → no diagnostic
		{"  je    nowhere", true, 1},           // branch to an undefined label → error
		{"  movxx rax, rbx", true, 1},          // unknown mnemonic → error
		{"  vaddps xmm0, xmm1, xmm2", true, 3}, // real insn, unsupported → hint
	}
	for _, c := range cases {
		d, _ := lineDiagnostic(c.line, labels)
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
	h := hover("  mov rax, rbx", nil, asm.Bits64)
	if !strings.Contains(h, "MOV") || !strings.Contains(h, "48 89 d8") {
		t.Errorf("hover(mov rax,rbx) missing mnemonic/bytes:\n%s", h)
	}
	if !strings.Contains(h, "decodes to") {
		t.Errorf("hover should show the canonical decode:\n%s", h)
	}
	if hover("  notaninsn x, y", nil, asm.Bits64) != "" {
		t.Errorf("hover on unknown mnemonic should be empty")
	}
	// 32-bit hover decodes in 32-bit: 89 d8 is "mov eax, ebx".
	h32 := hover("  mov eax, ebx", nil, asm.Bits32)
	if !strings.Contains(h32, "89 d8") || !strings.Contains(h32, "mov eax, ebx") {
		t.Errorf("32-bit hover wrong:\n%s", h32)
	}
}

func TestSignatureHelp(t *testing.T) {
	// Typing the second operand of "add eax, " → active parameter 1, and at
	// least one signature with two operands.
	line := "  add eax, "
	r := buildSignatureHelp(line, len(line))
	if r == nil {
		t.Fatal("expected signature help for 'add'")
	}
	if r.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1", r.ActiveParameter)
	}
	if len(r.Signatures) == 0 {
		t.Fatal("no signatures")
	}
	// Every signature label should start with the mnemonic.
	for _, s := range r.Signatures {
		if !strings.HasPrefix(s.Label, "ADD ") {
			t.Errorf("signature label %q doesn't start with mnemonic", s.Label)
		}
		// Parameter offsets must index into the label.
		for _, p := range s.Parameters {
			if p.Label[0] < 0 || p.Label[1] > len(s.Label) || p.Label[0] >= p.Label[1] {
				t.Errorf("bad parameter offsets %v for %q", p.Label, s.Label)
			}
		}
	}
	// The active signature must actually have a parameter at the active index.
	if as := r.Signatures[r.ActiveSignature]; len(as.Parameters) <= r.ActiveParameter {
		t.Errorf("active signature %q has too few params for active param %d", as.Label, r.ActiveParameter)
	}
	// Unknown mnemonic → no help.
	if buildSignatureHelp("  movxx eax, ", 12) != nil {
		t.Error("expected nil signature help for unknown mnemonic")
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
