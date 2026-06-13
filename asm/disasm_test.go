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
