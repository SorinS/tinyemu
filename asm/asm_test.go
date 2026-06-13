package asm

import "testing"

// TestParseTable proves the parser ingests the entire NASM table and
// recovers the four fields correctly on representative entries.
func TestParseTable(t *testing.T) {
	forms := parseTable(insnsDat)
	if len(forms) < 5000 {
		t.Fatalf("parsed %d forms, want >5000 (whole table)", len(forms))
	}

	// Spot-check a few well-known fixed encodings.
	checks := map[string]struct {
		operands int
		code     string
	}{
		"SYSCALL": {0, "0f 05"},
		"CPUID":   {0, "0f a2"},
		"INT3":    {0, "cc"},
	}
	seen := map[string]bool{}
	var macros, plain int
	for _, f := range forms {
		if f.Macro != "" {
			macros++
		} else {
			plain++
		}
		if want, ok := checks[f.Mnemonic]; ok && len(f.Operands) == want.operands && f.Macro == "" {
			seen[f.Mnemonic] = true
			if f.Code != want.code {
				t.Errorf("%s: code = %q, want %q", f.Mnemonic, f.Code, want.code)
			}
		}
	}
	for name := range checks {
		if !seen[name] {
			t.Errorf("expected form %s not found", name)
		}
	}

	t.Logf("ingested %d forms: %d plain, %d size-macro templates", len(forms), plain, macros)
}

// TestParseLineShapes exercises the field-splitting on hand-picked lines so a
// format regression is caught precisely.
func TestParseLineShapes(t *testing.T) {
	cases := []struct {
		line string
		want Form
	}{
		{
			"SYSCALL\t\tvoid\t\t\t[\t0f 05]\t\t\t\t\tP6,AMD",
			Form{Mnemonic: "SYSCALL", Code: "0f 05", Flags: []string{"P6", "AMD"}},
		},
		{
			"$bwdq MOV\trm#,reg#\t\t\t[mr:\thlexr o# 88# /r]\t\t\t8086,SM",
			Form{Macro: "$bwdq", Mnemonic: "MOV", Operands: []string{"rm#", "reg#"},
				EncOrder: "mr", Code: "hlexr o# 88# /r", Flags: []string{"8086", "SM"}},
		},
	}
	for _, tc := range cases {
		got, ok := parseLine(tc.line)
		if !ok {
			t.Errorf("parseLine(%q) failed", tc.line)
			continue
		}
		if got.Mnemonic != tc.want.Mnemonic || got.Macro != tc.want.Macro ||
			got.EncOrder != tc.want.EncOrder || got.Code != tc.want.Code ||
			!eq(got.Operands, tc.want.Operands) || !eq(got.Flags, tc.want.Flags) {
			t.Errorf("parseLine(%q)\n got %+v\nwant %+v", tc.line, got, tc.want)
		}
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
