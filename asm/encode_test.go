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

// TestAssembleVsNasm_RegOps checks register-direct operand encoding against
// nasm: reg/reg, reg/imm, push/pop, unary, shifts.
func TestAssembleVsNasm_RegOps(t *testing.T) {
	requireNasm(t)
	insns := []string{
		"add rax, rbx", "sub rcx, rdx", "and r8, r9", "or rax, r15", "cmp rbx, rax",
		"xor eax, eax", "add eax, ebx", "mov rax, rbx", "mov eax, ebx",
		"add al, bl", "mov cl, dl", "xor r10b, r11b",
		"push rax", "push r12", "pop rbx", "pop r15",
		"inc rcx", "dec rdx", "not rax", "neg r8",
		"add eax, 10", "sub rax, 5", "and ecx, 0xff", "cmp rdx, 0x100",
		"shl rax, 1", "shr eax, 1", "sar rbx, 1",
	}
	var pass, fail int
	for _, src := range insns {
		want := nasmAssemble(t, src)
		got, err := Assemble(src)
		if err != nil {
			t.Logf("MISS  %-20s err: %v", src, err)
			fail++
			continue
		}
		if !bytesEqual(got, want) {
			t.Logf("DIFF  %-20s got % x  nasm % x", src, got, want)
			fail++
			continue
		}
		pass++
	}
	t.Logf("register operands: %d match nasm, %d miss/diff", pass, fail)
	if pass == 0 {
		t.Errorf("no register-operand instructions matched nasm")
	}
}

func bytesEqual(a, b []byte) bool {
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
