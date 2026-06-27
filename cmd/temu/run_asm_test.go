package main

import "testing"

// The per-arch run_asm-<arch>.sh wrappers force the ISA via -cpu-arch; the code
// must REJECT a source whose own BITS / "arch:" directive / detected ISA
// contradicts that force (rather than silently overriding it).
func TestAsmArchConflict(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		arch     string
		conflict bool
	}{
		// no force → never a conflict (auto-detect path)
		{"auto-empty", "mov rax, 5\nret\n", "", false},
		{"auto-word", "BITS 32\nmov eax, 5\n", "auto", false},
		// x86-64 wrapper
		{"x64 ok plain", "mov rax, 5\nret\n", "x86_64", false},
		{"x64 ok bits64", "BITS 64\nmov rax, 5\n", "x86_64", false},
		{"x64 rej bits32", "BITS 32\nmov eax, 5\n", "x86_64", true},
		{"x64 rej riscv", "addi a0, zero, 5\nret\n", "x86_64", true},
		{"x64 rej arm64dir", "; arch: arm64\nmovz x0, #5\n", "x86_64", true},
		{"x64 rej riscvdir", "; arch: riscv64\nadd a0, a1, a2\n", "x86_64", true},
		// 32-bit x86 wrapper
		{"x86 ok bits32", "BITS 32\nmov eax, 5\n", "x86_32", false},
		{"x86 ok plain", "mov eax, 5\nret\n", "x86_32", false},
		{"x86 rej bits64", "BITS 64\nmov rax, 5\n", "x86_32", true},
		// riscv wrapper
		{"riscv ok heur", "addi a0, zero, 5\nret\n", "riscv64", false},
		{"riscv ok dir", "; arch: riscv64\nadd a0, a1, a2\n", "riscv64", false},
		{"riscv rej bits", "BITS 32\nmov eax, 5\n", "riscv64", true},
		{"riscv rej x86dir", "; arch: x86\nmov rax, 5\n", "riscv64", true},
		// arm64 wrapper: directive matches, and un-annotated arm64 must NOT be
		// falsely rejected (there is no arm64 mnemonic heuristic — the force
		// supplies the directive).
		{"arm64 ok dir", "; arch: arm64\nmovz x0, #5\n", "arm64", false},
		{"arm64 ok plain", "movz x0, #5\nret\n", "arm64", false},
		{"arm64 rej riscv", "addi a0, zero, 5\nret\n", "arm64", true},
		{"arm64 rej bits", "BITS 64\nmov rax, 5\n", "arm64", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := asmArchConflict(c.src, c.arch)
			if (got != "") != c.conflict {
				t.Fatalf("asmArchConflict(%q, %q) = %q; want conflict=%v", c.src, c.arch, got, c.conflict)
			}
		})
	}
}

func TestAsmDirectiveArch(t *testing.T) {
	cases := map[string]string{
		"; arch: riscv64\n add a0, a1": "riscv",
		"; arch: arm64\n movz x0, #1":  "arm64",
		"; arch: x86\n mov rax, 5":     "x86",
		"  ; ARCH: AARCH64\n":          "arm64", // case-insensitive
		"mov rax, 5\n ret\n":           "",      // no directive
	}
	for src, want := range cases {
		if got := asmDirectiveArch(src); got != want {
			t.Errorf("asmDirectiveArch(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestAsmExplicitBits(t *testing.T) {
	cases := []struct {
		src  string
		bits int
		has  bool
	}{
		{"BITS 32\n mov eax, 1", 32, true},
		{"bits 64\n mov rax, 1", 64, true},     // case-insensitive
		{"[BITS 32]\n", 32, true},              // bracketed
		{"; BITS 32 in a comment\n", 0, false}, // comment, not a directive
		{"BITS 16\n", 0, false},                // unsupported value
		{"mov rax, 5\n ret\n", 0, false},       // none
	}
	for _, c := range cases {
		bits, has := asmExplicitBits(c.src)
		if bits != c.bits || has != c.has {
			t.Errorf("asmExplicitBits(%q) = (%d,%v), want (%d,%v)", c.src, bits, has, c.bits, c.has)
		}
	}
}

func TestCanonAsmArch(t *testing.T) {
	cases := []struct {
		arch   string
		base   string
		bits   int
		forced bool
	}{
		{"x86", "x86", 64, true},
		{"x86_64", "x86", 64, true},
		{"x86_32", "x86", 32, true},
		{"i386", "x86", 32, true},
		{"riscv64", "riscv", 0, true},
		{"arm64", "arm64", 0, true},
		{"aarch64", "arm64", 0, true},
		{"", "", 0, false},
		{"auto", "", 0, false},
		{"bogus", "", 0, false},
	}
	for _, c := range cases {
		base, bits, forced := canonAsmArch(c.arch)
		if base != c.base || bits != c.bits || forced != c.forced {
			t.Errorf("canonAsmArch(%q) = (%q,%d,%v), want (%q,%d,%v)", c.arch, base, bits, forced, c.base, c.bits, c.forced)
		}
	}
}
