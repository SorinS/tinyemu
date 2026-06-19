package x86_64_test

// M4 asm tests: Group 3 (NOT/NEG/MUL/IMUL/DIV/IDIV), three-operand
// IMUL forms, BSWAP.

import (
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86_64"
)

// TestAsm_Not — bitwise NOT, no flag changes.
func TestAsm_Not(t *testing.T) {
	src := `
bits 64
	mov rax, 0x00000000FFFFFFFF
	not rax
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xFFFFFFFF00000000 {
		t.Errorf("RAX = %#x, want 0xFFFFFFFF00000000", got)
	}
}

// TestAsm_Neg — two's-complement negation, CF set when source non-zero.
func TestAsm_Neg(t *testing.T) {
	src := `
bits 64
	mov rax, 5
	neg rax
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xFFFFFFFFFFFFFFFB {
		t.Errorf("RAX = %#x, want -5", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF should be set (source was non-zero)")
	}
}

// TestAsm_NegZero — NEG of 0 leaves CF clear.
func TestAsm_NegZero(t *testing.T) {
	src := `
bits 64
	xor rax, rax
	neg rax
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.cpu.GetReg64(x86_64.RAX) != 0 {
		t.Errorf("NEG 0 should be 0")
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF != 0 {
		t.Errorf("CF should be clear (source was zero)")
	}
}

// TestAsm_Mul64 — unsigned 64-bit multiply with a small product.
func TestAsm_Mul64(t *testing.T) {
	src := `
bits 64
	mov rax, 1000
	mov rbx, 2000
	mul rbx                  ; RDX:RAX = RAX * RBX
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 2_000_000 {
		t.Errorf("RAX = %d, want 2000000", got)
	}
	if got := r.cpu.GetReg64(x86_64.RDX); got != 0 {
		t.Errorf("RDX = %d, want 0 (no overflow)", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF != 0 {
		t.Errorf("CF set on small product (RDX=0)")
	}
}

// TestAsm_Mul64_Overflow — product straddles 64 bits; CF/OF must set.
func TestAsm_Mul64_Overflow(t *testing.T) {
	src := `
bits 64
	mov rax, 0x100000000      ; 2^32
	mov rbx, 0x100000000      ; 2^32
	mul rbx                   ; product = 2^64 → RDX=1, RAX=0
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0 {
		t.Errorf("RAX = %#x, want 0", got)
	}
	if got := r.cpu.GetReg64(x86_64.RDX); got != 1 {
		t.Errorf("RDX = %#x, want 1", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF not set on overflow")
	}
}

// TestAsm_Imul2Op — IMUL r, r/m (low half only).
func TestAsm_Imul2Op(t *testing.T) {
	src := `
bits 64
	mov rax, -7
	mov rbx, 6
	imul rax, rbx           ; RAX = -42
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := int64(r.cpu.GetReg64(x86_64.RAX)); got != -42 {
		t.Errorf("RAX = %d, want -42", got)
	}
}

// TestAsm_Imul3Op_Imm8 — three-operand IMUL with imm8 form (0x6B).
func TestAsm_Imul3Op_Imm8(t *testing.T) {
	src := `
bits 64
	mov rbx, 7
	imul rax, rbx, 6        ; RAX = 7 * 6 = 42
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 42 {
		t.Errorf("RAX = %d, want 42", got)
	}
}

// TestAsm_Imul3Op_Imm32 — three-operand IMUL with imm32 form (0x69).
func TestAsm_Imul3Op_Imm32(t *testing.T) {
	src := `
bits 64
	mov rbx, 7
	imul rax, rbx, 0x10000      ; RAX = 7 * 65536 = 458752
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 458752 {
		t.Errorf("RAX = %d, want 458752", got)
	}
}

// TestAsm_Div64 — unsigned 64-bit divide.
func TestAsm_Div64(t *testing.T) {
	src := `
bits 64
	mov rax, 100
	xor rdx, rdx            ; RDX:RAX = 100
	mov rbx, 7
	div rbx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 14 {
		t.Errorf("RAX (quotient) = %d, want 14", got)
	}
	if got := r.cpu.GetReg64(x86_64.RDX); got != 2 {
		t.Errorf("RDX (remainder) = %d, want 2", got)
	}
}

// TestAsm_Idiv64 — signed 64-bit divide. RDX must be sign-extension of
// RAX before IDIV, normally done by CQO; for the test we just set RDX
// directly to -1 to match the sign extension of negative RAX.
func TestAsm_Idiv64(t *testing.T) {
	src := `
bits 64
	mov rax, -100
	mov rdx, -1              ; sign extension of RAX
	mov rbx, 7
	idiv rbx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := int64(r.cpu.GetReg64(x86_64.RAX)); got != -14 {
		t.Errorf("RAX = %d, want -14", got)
	}
	if got := int64(r.cpu.GetReg64(x86_64.RDX)); got != -2 {
		t.Errorf("RDX = %d, want -2", got)
	}
}

// TestAsm_Bswap — byte-reverse a 64-bit register.
func TestAsm_Bswap(t *testing.T) {
	src := `
bits 64
	mov rax, 0x0102030405060708
	bswap rax
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0x0807060504030201 {
		t.Errorf("RAX = %#x, want 0x0807060504030201", got)
	}
}
