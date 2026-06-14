package asm

import "testing"

// TestDisassemble sanity-checks the x/arch-backed disassembler on a few
// hand-known encodings.
func TestDisassemble(t *testing.T) {
	cases := []struct {
		bytes []byte
		want  string
	}{
		{[]byte{0x48, 0x89, 0xd8}, "mov rax, rbx"},
		{[]byte{0x0f, 0x05}, "syscall"},
		{[]byte{0x41, 0x54}, "push r12"},
		{[]byte{0xc3}, "ret"},
	}
	for _, c := range cases {
		got, n, err := Disassemble(c.bytes, 64)
		if err != nil {
			t.Errorf("Disassemble(% x): %v", c.bytes, err)
			continue
		}
		if got != c.want || n != len(c.bytes) {
			t.Errorf("Disassemble(% x) = %q (len %d), want %q (len %d)", c.bytes, got, n, c.want, len(c.bytes))
		}
	}
}

// TestDisassemble32 sanity-checks 32-bit decoding (same opcodes, different
// default operand size: 0x89 0xd8 is "mov eax, ebx" in 32-bit, "mov rax, rbx"
// would need REX.W).
func TestDisassemble32(t *testing.T) {
	cases := []struct {
		bytes []byte
		want  string
	}{
		{[]byte{0x89, 0xd8}, "mov eax, ebx"},
		{[]byte{0x83, 0xc0, 0x05}, "add eax, 0x5"},
		{[]byte{0x40}, "inc eax"}, // 0x40 is INC eax in 32-bit (a REX prefix in 64-bit)
		{[]byte{0xc3}, "ret"},
	}
	for _, c := range cases {
		got, n, err := DisassembleMode(c.bytes, Bits32)
		if err != nil {
			t.Errorf("DisassembleMode(% x, 32): %v", c.bytes, err)
			continue
		}
		if got != c.want || n != len(c.bytes) {
			t.Errorf("DisassembleMode(% x, 32) = %q (len %d), want %q", c.bytes, got, n, c.want)
		}
	}
}

// TestRoundTrip32 proves the 32-bit assembler and disassembler agree: assemble
// in BITS 32, disassemble at 32, re-assemble, require stable bytes.
func TestRoundTrip32(t *testing.T) {
	insns := []string{
		"mov eax, ebx", "add eax, ecx", "sub ecx, edx", "and eax, ebx", "xor eax, eax",
		"cmp ebx, edx", "or eax, esi", "mov eax, 0x1234", "add eax, 5", "and ecx, 0xff",
		"push eax", "pop ebx", "inc ecx", "dec edx", "not eax", "neg ebx",
		"mov eax, [ebx]", "mov [ebx], eax", "mov eax, [ebx+8]", "mov eax, [ebx+ecx*4+8]",
		"add eax, [ebx]", "lea eax, [ebx+ecx*2+4]", "mov eax, [esp]", "mov eax, [ebp]",
		"shl eax, 1", "sar ebx, 1", "mov dword [eax], 1", "ret", "nop", "leave",
	}
	var pass, fail int
	for _, src := range insns {
		mine, err := AssembleMode(src, Bits32)
		if err != nil {
			continue
		}
		text, n, err := DisassembleMode(mine, Bits32)
		if err != nil || n != len(mine) {
			t.Logf("DISASM %-24s % x -> %q (len %d, err %v)", src, mine, text, n, err)
			fail++
			continue
		}
		again, err := AssembleMode(text, Bits32)
		if err != nil {
			t.Logf("REASM  %-24s -> %q : %v", src, text, err)
			fail++
			continue
		}
		if !bytesEqual(again, mine) {
			t.Logf("UNSTABLE %-22s % x -> %q -> % x", src, mine, text, again)
			fail++
			continue
		}
		pass++
	}
	t.Logf("32-bit round-trip: %d stable, %d fail", pass, fail)
	if fail > 0 {
		t.Errorf("%d 32-bit round-trip failures", fail)
	}
}

// TestRoundTrip assembles an instruction, disassembles the bytes with x/arch,
// re-assembles the disassembly, and requires the bytes to be stable — proving
// the two halves agree.
func TestRoundTrip(t *testing.T) {
	insns := []string{
		"mov rax, rbx", "add rax, rcx", "sub ecx, edx", "and r8, r9", "xor eax, eax",
		"cmp rbx, rdx", "or rax, r15", "mov eax, 0x1234", "add rax, 5", "and ecx, 0xff",
		"push rax", "push r12", "pop rbx", "inc rcx", "dec rdx", "not rax", "neg r8",
		"mov rax, [rbx]", "mov [rbx], rax", "mov rax, [rbx+8]", "mov rax, [rbx+rcx*4+8]",
		"add rax, [rbx]", "lea rax, [rbx+rcx*2+4]", "mov rax, [rsp]", "mov rax, [rbp]",
		"mov rax, [r12]", "mov rax, [r13]", "shl rax, 1", "sar rbx, 1",
		"inc qword [rax]", "mov dword [rax], 1", "syscall", "ret", "nop", "leave",
	}
	var pass, fail int
	for _, src := range insns {
		mine, err := Assemble(src)
		if err != nil {
			continue // not encodable yet; covered elsewhere
		}
		text, n, err := Disassemble(mine, 64)
		if err != nil || n != len(mine) {
			t.Logf("DISASM %-24s % x -> %q (len %d, err %v)", src, mine, text, n, err)
			fail++
			continue
		}
		again, err := Assemble(text)
		if err != nil {
			t.Logf("REASM  %-24s -> %q : %v", src, text, err)
			fail++
			continue
		}
		if !bytesEqual(again, mine) {
			t.Logf("UNSTABLE %-22s % x -> %q -> % x", src, mine, text, again)
			fail++
			continue
		}
		pass++
	}
	t.Logf("round-trip: %d stable, %d fail", pass, fail)
	if fail > 0 {
		t.Errorf("%d round-trip failures", fail)
	}
}
