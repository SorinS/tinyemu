package asm

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sampleOperand returns a concrete source operand for an operand-type token,
// or ok=false if the type isn't one the encoder targets yet (xmm/ymm/k/fpu/
// segment/relative/…). This drives the table-wide coverage sweep.
func sampleOperand(tok string) (string, bool) {
	tok = stripMods(tok)
	switch tok {
	case "reg8", "rm8":
		return "bl", true
	case "reg16", "rm16":
		return "cx", true
	case "reg32", "rm32":
		return "ecx", true
	case "reg64", "rm64":
		return "rdx", true
	case "mem":
		return "[rax]", true
	case "mem8":
		return "byte [rax]", true
	case "mem16":
		return "word [rax]", true
	case "mem32":
		return "dword [rax]", true
	case "mem64":
		return "qword [rax]", true
	case "imm", "imm8":
		return "7", true
	case "imm16":
		return "0x100", true
	case "imm32":
		return "0x1234", true
	case "imm64":
		return "0x123456789a", true
	case "unity":
		return "1", true
	case "sbyteword16", "sbytedword32", "sbytedword64":
		return "7", true
	case "sdword64":
		return "0x100", true
	}
	if strings.HasPrefix(tok, "reg_") {
		if _, ok := gprByName[tok[4:]]; ok {
			return tok[4:], true // specific GPR (al/ax/eax/rax/cl/…)
		}
	}
	return "", false
}

var plainMnemonic = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// sampleSource builds a concrete source line for a form, or ok=false if it
// isn't a GPR-integer form we can sample (SIMD/FPU/pseudo/weird mnemonic).
func sampleSource(f *Form) (string, bool) {
	if !plainMnemonic.MatchString(f.Mnemonic) {
		return "", false
	}
	for _, fl := range f.Flags {
		if fl == "PSEUDO" {
			return "", false
		}
	}
	ops := make([]string, len(f.Operands))
	for i, tok := range f.Operands {
		s, ok := sampleOperand(tok)
		if !ok {
			return "", false
		}
		ops[i] = s
	}
	if len(ops) == 0 {
		return f.Mnemonic, true
	}
	return f.Mnemonic + " " + strings.Join(ops, ", "), true
}

var tokenReason = regexp.MustCompile(`unsupported code token "([^"]*)"`)

// TestCoverageSweep synthesizes a concrete instruction for every GPR-integer
// form in the table and assembles it with both the encoder and nasm, reporting
// the match rate and the most common gaps. nasm-rejected lines (e.g. 16-bit-
// only forms invalid in long mode) are excluded, not counted against us.
func TestCoverageSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("coverage sweep runs ~1000 nasm invocations")
	}
	requireNasm(t)
	seen := map[string]bool{}
	var total, pass, miss, diff, nrej int
	missTok := map[string]int{}
	diffEx := 0

	for i := range table {
		src, ok := sampleSource(&table[i])
		if !ok || seen[src] {
			continue
		}
		seen[src] = true
		want, nok := nasmAssemble(t, src)
		if !nok {
			nrej++
			continue
		}
		total++
		got, err := Assemble(src)
		switch {
		case err != nil:
			miss++
			key := "other"
			if m := tokenReason.FindStringSubmatch(err.Error()); m != nil {
				key = "token:" + m[1]
			} else if strings.Contains(err.Error(), "no matching") {
				key = "no-match"
			}
			missTok[key]++
		case !bytesEqual(got, want):
			diff++
			if diffEx < 15 {
				t.Logf("DIFF  %-28s got % x  nasm % x", src, got, want)
				diffEx++
			}
		default:
			pass++
		}
	}

	pct := 0.0
	if total > 0 {
		pct = 100 * float64(pass) / float64(total)
	}
	t.Logf("coverage sweep: %d GPR forms, %d match nasm (%.1f%%), %d miss, %d diff  [%d nasm-rejected, excluded]",
		total, pass, pct, miss, diff, nrej)

	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range missTok {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
	for n, e := range top {
		if n >= 15 {
			break
		}
		t.Logf("  miss %-22s %d", e.k, e.v)
	}
	// Regression floor: GPR-integer coverage and the (edge-case) diff count.
	// The remaining misses are deferred XOP/EVEX/APX encodings; the diffs are
	// known edge cases (nasm's small-imm-to-32-bit optimization, IRET default
	// size, system-instruction address-size prefixes).
	if pass < 870 {
		t.Errorf("coverage regressed: %d match nasm, want >= 870", pass)
	}
	if diff > 35 {
		t.Errorf("byte-diff count rose to %d (want <= 35) — encoder regression?", diff)
	}
}
