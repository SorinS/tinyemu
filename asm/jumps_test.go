package asm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func nasmProgram(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "p.asm")
	out := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(in, []byte("BITS 64\n"+src+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("nasm", "-f", "bin", "-o", out, in).Run(); err != nil {
		return nil, false
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b, true
}

func TestAssembleProgram_Jumps(t *testing.T) {
	requireNasm(t)
	progs := []string{
		"start:\n  xor eax, eax\n  inc eax\n  jmp start",
		"  jmp fwd\n  nop\n  nop\nfwd:\n  ret",
		"loop1:\n  dec rcx\n  jnz loop1\n  ret",
		"  cmp rax, rbx\n  je equal\n  mov rax, 1\n  ret\nequal:\n  xor eax, eax\n  ret",
		"  test rax, rax\n  jz done\n  call sub1\ndone:\n  ret\nsub1:\n  inc rax\n  ret",
		// forces a rel32 forward jump (target > 127 bytes away)
		"  jmp far_away\n" + strings.Repeat("  nop\n", 200) + "far_away:\n  ret",
		// rel32 backward
		"back:\n" + strings.Repeat("  nop\n", 200) + "  jmp back",
	}
	var pass, fail int
	for i, src := range progs {
		want, ok := nasmProgram(t, src)
		if !ok {
			t.Logf("SKIP prog %d (nasm rejected)", i)
			continue
		}
		got, err := AssembleProgram(src)
		switch {
		case err != nil:
			t.Logf("MISS prog %d: %v", i, err)
			fail++
		case !bytesEqual(got, want):
			t.Logf("DIFF prog %d:\n got  % x\n nasm % x", i, got, want)
			fail++
		default:
			pass++
		}
	}
	t.Logf("jump programs: %d match nasm, %d miss/diff", pass, fail)
	if fail > 0 {
		t.Errorf("%d jump programs differ from nasm", fail)
	}
}
