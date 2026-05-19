package x86_64_test

// M2 asm tests: conditional jumps + comparison + logical ops + INC/DEC.
// Programs are richer than the M1 set — loops, branches, and flag-
// driven control flow. Failure inside a NASM program is signaled by
// jumping into a 0xCC byte (INT3) which surfaces as an unimplemented-
// opcode error at the Go level, since our decoder doesn't yet deliver
// #BP through an IDT. The Go assertions inspect register state after
// the program halts cleanly.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
)

// TestAsm_LoopSum — the classic "sum 1..10" pattern using dec+jnz.
func TestAsm_LoopSum(t *testing.T) {
	src := `
bits 64
	mov rcx, 10
	xor rax, rax        ; rax = 0
loop:
	add rax, rcx
	dec rcx
	jnz loop
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 1000); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 55 {
		t.Errorf("RAX = %d, want 55 (1+2+...+10)", got)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 0 {
		t.Errorf("RCX = %d, want 0 (loop terminator)", got)
	}
}

// TestAsm_LoopSumRet — same shape but the loop body is a callee.
// Exercises CALL+RET inside a Jcc-driven loop.
func TestAsm_LoopSumRet(t *testing.T) {
	src := `
bits 64
	mov rsp, 0x100000
	mov rcx, 5
	xor rax, rax
top:
	call add_rcx
	dec rcx
	jnz top
	hlt

add_rcx:
	add rax, rcx
	ret
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 1000); err != nil {
		t.Fatalf("run: %v", err)
	}
	// 5+4+3+2+1 = 15
	if got := r.cpu.GetReg64(x86_64.RAX); got != 15 {
		t.Errorf("RAX = %d, want 15", got)
	}
}

// TestAsm_CmpEqual — branches based on CMP result.
func TestAsm_CmpEqual(t *testing.T) {
	src := `
bits 64
	mov rax, 42
	cmp rax, 42
	jne fail
	mov rbx, 1            ; reached when equal
	jmp done
fail:
	mov rbx, 99
done:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RBX); got != 1 {
		t.Errorf("RBX = %d, want 1 (equal branch)", got)
	}
}

// TestAsm_CmpUnsigned — JA (CF=0 && ZF=0), JB (CF=1).
func TestAsm_CmpUnsigned(t *testing.T) {
	src := `
bits 64
	mov rax, 100
	mov rbx, 50
	cmp rax, rbx          ; 100 > 50 unsigned ⇒ CF=0, ZF=0 ⇒ JA taken
	ja  bigger
	mov rcx, 0
	jmp done
bigger:
	mov rcx, 1
done:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 1 {
		t.Errorf("RCX = %d, want 1 (JA path)", got)
	}
}

// TestAsm_CmpSigned — JL (SF != OF) for signed less-than.
func TestAsm_CmpSigned(t *testing.T) {
	src := `
bits 64
	mov rax, -5
	mov rbx, 5
	cmp rax, rbx          ; -5 < 5 signed ⇒ JL taken
	jl  less
	mov rcx, 0
	jmp done
less:
	mov rcx, 1
done:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 1 {
		t.Errorf("RCX = %d, want 1 (JL path)", got)
	}
}

// TestAsm_XorSelf — the idiomatic register-zero plus ZF observation.
func TestAsm_XorSelf(t *testing.T) {
	src := `
bits 64
	mov rax, 0xDEADBEEFCAFEBABE
	xor rax, rax           ; rax = 0, ZF=1, CF=0, OF=0, SF=0
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
	fl := r.cpu.GetRFLAGS()
	if fl&x86_64.RFLAGS_ZF == 0 {
		t.Errorf("ZF clear after xor self")
	}
	if fl&x86_64.RFLAGS_CF != 0 {
		t.Errorf("CF set after xor (logical ops clear CF)")
	}
	if fl&x86_64.RFLAGS_OF != 0 {
		t.Errorf("OF set after xor (logical ops clear OF)")
	}
}

// TestAsm_AndMask — AND clears bits and sets the flag profile.
func TestAsm_AndMask(t *testing.T) {
	src := `
bits 64
	mov rax, 0xFF00FF00FF00FF00
	mov rbx, 0x0F0F0F0F0F0F0F0F
	and rax, rbx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := uint64(0x0F000F000F000F00)
	if got := r.cpu.GetReg64(x86_64.RAX); got != want {
		t.Errorf("RAX = %#x, want %#x", got, want)
	}
}

// TestAsm_OrFill — OR sets bits and clears the CF/OF logical-op flags.
func TestAsm_OrFill(t *testing.T) {
	src := `
bits 64
	mov rax, 0xF000000000000000
	mov rbx, 0x000000000000000F
	or  rax, rbx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := uint64(0xF00000000000000F)
	if got := r.cpu.GetReg64(x86_64.RAX); got != want {
		t.Errorf("RAX = %#x, want %#x", got, want)
	}
	fl := r.cpu.GetRFLAGS()
	if fl&x86_64.RFLAGS_SF == 0 {
		t.Errorf("SF clear despite high bit set in result")
	}
	if fl&x86_64.RFLAGS_CF != 0 {
		t.Errorf("CF set after OR")
	}
}

// TestAsm_TestNonZero — TEST sets ZF only if AND-result is zero.
func TestAsm_TestNonZero(t *testing.T) {
	src := `
bits 64
	mov rax, 0xFF
	mov rbx, 0x80
	test rax, rbx        ; 0xFF & 0x80 = 0x80 ⇒ ZF=0
	jnz set_one
	mov rcx, 0
	jmp done
set_one:
	mov rcx, 1
done:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 1 {
		t.Errorf("RCX = %d, want 1 (JNZ path)", got)
	}
}

// TestAsm_TestZero — TEST sets ZF when AND-result is zero.
func TestAsm_TestZero(t *testing.T) {
	src := `
bits 64
	mov rax, 0xF0
	mov rbx, 0x0F
	test rax, rbx        ; 0xF0 & 0x0F = 0 ⇒ ZF=1
	jz  set_one
	mov rcx, 0
	jmp done
set_one:
	mov rcx, 1
done:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 1 {
		t.Errorf("RCX = %d, want 1 (JZ path)", got)
	}
}

// TestAsm_IncDec — INC/DEC preserve CF (Intel SDM).
func TestAsm_IncDec(t *testing.T) {
	src := `
bits 64
	mov rax, 5
	stc                  ; set CF
	inc rax              ; rax=6, CF must remain set
	hlt
`
	r := newAsmRunner(t)
	// STC is opcode 0xF9 — not yet implemented in our decoder. Skip
	// this test if STC fails (we'll add STC in M3).
	// Instead, exercise the CF-preserve property at the Go level: pre-
	// load RFLAGS.CF=1 before running.
	r.cpu.SetRFLAGS(r.cpu.GetRFLAGS() | x86_64.RFLAGS_CF)
	// Drop STC from the program for now.
	src = `
bits 64
	mov rax, 5
	inc rax
	hlt
`
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 6 {
		t.Errorf("RAX = %d, want 6", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF cleared by INC (SDM says it must be preserved)")
	}
}

// TestAsm_DecToZero — DEC sets ZF when result is zero.
func TestAsm_DecToZero(t *testing.T) {
	src := `
bits 64
	mov rax, 1
	dec rax           ; rax = 0 ⇒ ZF=1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0 {
		t.Errorf("RAX = %d", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_ZF == 0 {
		t.Errorf("ZF not set after DEC produced zero")
	}
}

// TestAsm_JmpNearRel32 — the 0x0F 0x80..0x8F long-distance Jcc encoding
// kicks in when the assembler picks a 32-bit offset (e.g. when the
// target is far). Force NASM into the long form via `jne near`.
func TestAsm_JmpNearRel32(t *testing.T) {
	src := `
bits 64
	mov rax, 1
	cmp rax, 1
	jne near far_target  ; 0x0F 0x85 disp32
	mov rbx, 7           ; reached when equal
	jmp done
far_target:
	mov rbx, 99
done:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RBX); got != 7 {
		t.Errorf("RBX = %d, want 7 (fallthrough path)", got)
	}
}

// TestAsm_Group1Imm — 0x83 (sign-extended imm8) and 0x81 (imm32).
// NASM picks whichever encoding fits.
func TestAsm_Group1Imm(t *testing.T) {
	src := `
bits 64
	mov rax, 100
	add rax, 1           ; small imm ⇒ 0x83
	add rax, 0x12345     ; large imm ⇒ 0x81
	sub rax, 5           ; 0x83 /5
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := uint64(100 + 1 + 0x12345 - 5)
	if got := r.cpu.GetReg64(x86_64.RAX); got != want {
		t.Errorf("RAX = %#x, want %#x", got, want)
	}
}
