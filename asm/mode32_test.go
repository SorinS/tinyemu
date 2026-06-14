package asm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// nasmBits32 assembles src in BITS 32 mode with nasm; ok=false if nasm rejects.
func nasmBits32(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "p.asm")
	out := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(in, []byte("BITS 32\n"+src+"\n"), 0o644); err != nil {
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

// TestMode32_HandCases checks the 32-bit integer subset byte-for-byte against
// nasm. These are the instructions a person actually writes in 32-bit asm.
func TestMode32_HandCases(t *testing.T) {
	requireNasm(t)
	cases := []string{
		"mov eax, ebx",
		"mov eax, 5",
		"mov eax, 0x12345678",
		"mov ecx, edx",
		"add eax, ebx",
		"add eax, [ebx]",
		"add eax, [ebx + ecx*4 + 8]",
		"sub esp, 0x10",
		"and eax, 0xff",
		"or ebx, ecx",
		"xor eax, eax",
		"cmp eax, ebx",
		"cmp dword [ebx], 0",
		"push eax",
		"push 0x10",
		"pop ebx",
		"lea edi, [ebx + 8]",
		"lea esi, [eax + ebx*2]",
		"inc ecx",
		"dec edx",
		"neg eax",
		"not ebx",
		"shl eax, 1",
		"shl eax, cl",
		"shr edx, 3",
		"sar eax, 5",
		"mov dword [eax], 1",
		"mov byte [eax], 0x7f",
		"mov word [eax], 0x1234",
		"mov ebx, [0x1000]",
		"test eax, eax",
		"imul eax, ebx",
		"ret",
		"nop",
		"int3",
		"leave",
		"cdq",
	}
	var pass, fail int
	for _, src := range cases {
		want, ok := nasmBits32(t, src)
		if !ok {
			t.Logf("SKIP (nasm rejected): %s", src)
			continue
		}
		got, err := AssembleMode(src, Bits32)
		switch {
		case err != nil:
			t.Errorf("MISS %-32q: %v", src, err)
			fail++
		case !bytesEqual(got, want):
			t.Errorf("DIFF %-32q\n got  % x\n nasm % x", src, got, want)
			fail++
		default:
			pass++
		}
	}
	t.Logf("BITS 32 hand cases: %d pass, %d fail", pass, fail)
}

// TestMode32_Program checks a full 32-bit program — labels, a backward
// conditional branch, branch relaxation — against nasm.
func TestMode32_Program(t *testing.T) {
	requireNasm(t)
	programs := []string{
		"BITS 32\nstart:\n  mov ecx, 3\nl1:\n  dec ecx\n  jnz l1\n  ret\n",
		"BITS 32\n  xor eax, eax\n  cmp eax, 1\n  je done\n  inc eax\ndone:\n  ret\n",
		"BITS 32\n  push ebp\n  mov ebp, esp\n  jmp over\n  nop\nover:\n  pop ebp\n  ret\n",
	}
	for i, src := range programs {
		want, ok := nasmBits32(t, src) // nasm prepends another BITS 32 (harmless)
		if !ok {
			t.Logf("SKIP prog %d (nasm rejected)", i)
			continue
		}
		got, err := AssembleProgram(src) // DetectBits reads the BITS 32 line
		switch {
		case err != nil:
			t.Errorf("prog %d MISS: %v", i, err)
		case !bytesEqual(got, want):
			t.Errorf("prog %d DIFF:\n got  % x\n nasm % x", i, got, want)
		}
	}
}

// TestDetectBits pins the directive parsing and the default.
func TestDetectBits(t *testing.T) {
	cases := []struct {
		src  string
		want Mode
	}{
		{"mov rax, rbx", Bits64},             // no directive → default 64
		{"BITS 64\n mov rax, rbx", Bits64},   // explicit 64
		{"BITS 32\n mov eax, ebx", Bits32},   // explicit 32
		{"  bits 32\n", Bits32},              // case-insensitive, indented
		{"[BITS 32]\n", Bits32},              // bracketed form
		{"; BITS 32 in a comment\n", Bits64}, // comment, not a directive
		{"BITS 16\n", Bits64},                // unsupported value → default 64
	}
	for _, c := range cases {
		if got := DetectBits(c.src); got != c.want {
			t.Errorf("DetectBits(%q) = %d, want %d", c.src, got, c.want)
		}
	}
}

// TestMode32_RejectsLong: 64-bit-only constructs must not encode in 32-bit.
func TestMode32_RejectsLong(t *testing.T) {
	for _, src := range []string{
		"mov rax, rbx", // r64
		"mov r8d, eax", // extended reg needs REX
		"add rax, 1",   // r64
		"mov rax, [rip + 8]",
	} {
		if b, err := AssembleMode(src, Bits32); err == nil {
			t.Errorf("AssembleMode(%q, Bits32) = % x, want error (64-bit only)", src, b)
		}
	}
}
