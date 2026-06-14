package main

import (
	"strings"
	"testing"
)

func TestRVDiagnostics(t *testing.T) {
	if d := lineDiagnosticRV("  addi a0, zero, 5", nil); d != nil {
		t.Errorf("valid riscv flagged: %+v", d)
	}
	if d := lineDiagnosticRV("  ret", nil); d != nil {
		t.Errorf("ret (pseudo) flagged: %+v", d)
	}
	d := lineDiagnosticRV("  bogus a0, a1", nil)
	if d == nil || d.severity != 1 {
		t.Errorf("unknown riscv mnemonic should be severity-1 error, got %+v", d)
	}
	if lineDiagnosticRV("loop:", nil) != nil {
		t.Errorf("label line should not be flagged")
	}
	// A branch to a known label must not be flagged.
	if d := lineDiagnosticRV("  blt a0, a1, loop", map[string]int64{"loop": 0}); d != nil {
		t.Errorf("branch to known label flagged: %+v", d)
	}
}

func TestRVHover(t *testing.T) {
	h := hoverRV("  add a0, a1, a2", nil)
	if !strings.Contains(h, "encodes to") || !strings.Contains(h, "decodes to") {
		t.Errorf("riscv hover missing encode/decode:\n%s", h)
	}
	if !strings.Contains(h, "add a0, a1, a2") {
		t.Errorf("riscv hover should show canonical decode:\n%s", h)
	}
	if hoverRV("  notarealinsn x", nil) != "" {
		t.Errorf("hover on unknown riscv mnemonic should be empty")
	}
}

func TestRVCompletions(t *testing.T) {
	got := completionsRV("ad")
	var hasAdd, hasAddi bool
	for _, m := range got {
		if m == "add" {
			hasAdd = true
		}
		if m == "addi" {
			hasAddi = true
		}
	}
	if !hasAdd || !hasAddi {
		t.Errorf("completionsRV(ad) = %v, want add and addi", got)
	}
}
