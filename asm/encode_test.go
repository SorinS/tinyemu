package asm

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// nasmAssemble assembles one 64-bit instruction with the real nasm and
// returns the raw bytes — the oracle the encoder is held to.
func nasmAssemble(t *testing.T, src string) []byte {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.asm")
	out := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(in, []byte("BITS 64\n"+src+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("nasm", "-f", "bin", "-o", out, in)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nasm failed for %q: %v\n%s", src, err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func requireNasm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nasm"); err != nil {
		t.Skip("nasm not on PATH")
	}
}

// TestAssembleVsNasm_Fixed checks the encoder byte-for-byte against nasm on
// fixed-encoding (no-operand) instructions — the first vertical slice of the
// data-driven encoder.
func TestAssembleVsNasm_Fixed(t *testing.T) {
	requireNasm(t)
	insns := []string{
		"nop", "ret", "leave", "int3", "syscall", "cpuid",
		"cwde", "cdqe", "cdq", "cqo", "cbw", "cwd",
		"clc", "stc", "cmc", "cld", "std", "cli", "sti",
		"hlt", "pause", "lfence", "sfence", "mfence",
		"rdtsc", "rdtscp", "wbinvd", "sahf", "lahf",
	}
	for _, src := range insns {
		t.Run(src, func(t *testing.T) {
			want := nasmAssemble(t, src)
			got, err := Assemble(src)
			if err != nil {
				t.Fatalf("Assemble(%q): %v", src, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("Assemble(%q) = % x, nasm = % x", src, got, want)
			}
		})
	}
}
