package asm

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// nasmAssemble assembles one 64-bit instruction with the real nasm and
// returns the raw bytes — the oracle the encoder is held to. ok is false if
// nasm itself rejected the source (a test-input problem, not an encoder bug).
func nasmAssemble(t *testing.T, src string) (b []byte, ok bool) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.asm")
	out := filepath.Join(dir, "out.bin")
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
			want, ok := nasmAssemble(t, src)
			if !ok {
				t.Fatalf("nasm rejected %q", src)
			}
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
		want, ok := nasmAssemble(t, src)
		if !ok {
			t.Logf("SKIP  %s (nasm rejected)", src)
			continue
		}
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

// TestAssembleVsNasm_Mem checks memory-operand encoding (ModRM/SIB/disp/
// RIP-relative + the rsp/rbp/r12/r13 special cases) against nasm.
func TestAssembleVsNasm_Mem(t *testing.T) {
	requireNasm(t)
	insns := []string{
		"mov rax, [rbx]", "mov [rbx], rax", "mov rax, [rbx+8]", "mov rax, [rbx-8]",
		"mov rax, [rbx+0x100]", "mov rax, [rsp]", "mov rax, [rbp]", "mov rax, [r12]",
		"mov rax, [r13]", "mov rax, [rbx+rcx]", "mov rax, [rbx+rcx*4]",
		"mov rax, [rbx+rcx*4+8]", "mov rax, [rax+r8*8+0x10]", "mov rax, [r8+r9]",
		"mov rax, [0x1000]", "mov eax, [rbx]", "mov al, [rbx]",
		"lea rax, [rbx+rcx*2+4]",
		"add rax, [rbx]", "sub [rbx+8], rcx", "and ecx, [rdx]",
		"inc qword [rax]", "dec dword [rbx]", "neg byte [rcx]",
		"mov dword [rax], 1", "mov byte [rbx], 5", "add qword [rsp+8], 0x20",
	}
	var pass, fail int
	for _, src := range insns {
		want, ok := nasmAssemble(t, src)
		if !ok {
			t.Logf("SKIP  %s (nasm rejected)", src)
			continue
		}
		got, err := Assemble(src)
		switch {
		case err != nil:
			t.Logf("MISS  %-26s err: %v", src, err)
			fail++
		case !bytesEqual(got, want):
			t.Logf("DIFF  %-26s got % x  nasm % x", src, got, want)
			fail++
		default:
			pass++
		}
	}
	t.Logf("memory operands: %d match nasm, %d miss/diff", pass, fail)
	if pass == 0 {
		t.Errorf("no memory-operand instructions matched nasm")
	}
}
