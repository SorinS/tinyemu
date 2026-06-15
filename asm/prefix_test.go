package asm

import (
	"bytes"
	"testing"
)

// TestRepLockPrefix checks the rep/repe/repne/lock prefixes byte-for-byte
// against nasm, in both 64-bit and 32-bit modes. These prefix the instruction
// on the same line (rep stosb, lock add …) — the form real kernel asm uses.
func TestRepLockPrefix(t *testing.T) {
	requireNasm(t)

	cases64 := []string{
		"rep stosb", "rep stosd", "rep movsb", "rep movsd", "rep movsq",
		"repe cmpsb", "repne scasb", "repz cmpsd", "repnz scasd",
		"lock add qword [rax], 1", "lock xadd [rdi], rax",
		"lock cmpxchg [rsi], rbx", "lock inc dword [rax]",
	}
	for _, src := range cases64 {
		want, ok := nasmAssemble(t, src)
		if !ok {
			t.Fatalf("nasm rejected %q (test input)", src)
		}
		got, err := AssembleMode(src, Bits64)
		if err != nil {
			t.Errorf("Bits64 %q: %v", src, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("Bits64 %q: got % x, nasm % x", src, got, want)
		}
	}

	cases32 := []string{
		"rep stosb", "rep stosd", "rep movsb", "rep movsd",
		"repe cmpsb", "repne scasb", "lock add dword [eax], 1",
		"lock xadd [edi], eax", "lock inc dword [eax]",
	}
	for _, src := range cases32 {
		want, ok := nasmBits32(t, src)
		if !ok {
			t.Fatalf("nasm rejected %q (test input)", src)
		}
		got, err := AssembleMode(src, Bits32)
		if err != nil {
			t.Errorf("Bits32 %q: %v", src, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("Bits32 %q: got % x, nasm % x", src, got, want)
		}
	}
}

// TestRepPrefixProgram assembles a small program using a rep-prefixed
// instruction and a labelled branch, confirming the prefix byte doesn't throw
// off branch-displacement math (the prefixed instruction is one byte longer).
func TestRepPrefixProgram(t *testing.T) {
	requireNasm(t)
	src := "BITS 64\n" +
		"start:\n" +
		"  mov rcx, 4\n" +
		"  xor eax, eax\n" +
		"  rep stosb\n" +
		"  dec rbx\n" +
		"  jnz start\n" +
		"  ret\n"
	want, ok := nasmProgram(t, src)
	if !ok {
		t.Skip("nasm rejected program")
	}
	got, err := AssembleProgram(src)
	if err != nil {
		t.Fatalf("AssembleProgram: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("program: got % x, nasm % x", got, want)
	}
}
