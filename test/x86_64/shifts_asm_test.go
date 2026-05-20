package x86_64_test

// M3 asm tests: shifts, zero/sign-extending moves, CMOVcc, SETcc.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
)

// TestAsm_ShlBy1 — shift left by an implicit 1.
func TestAsm_ShlBy1(t *testing.T) {
	src := `
bits 64
	mov rax, 1
	shl rax, 1
	shl rax, 1
	shl rax, 1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 8 {
		t.Errorf("RAX = %d, want 8", got)
	}
}

// TestAsm_ShlByImm8 — large shifts via the imm8 form.
func TestAsm_ShlByImm8(t *testing.T) {
	src := `
bits 64
	mov rax, 1
	shl rax, 32     ; 0x100000000
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0x1_00000000 {
		t.Errorf("RAX = %#x, want 0x100000000", got)
	}
}

// TestAsm_ShlByCL — shift count comes from CL.
func TestAsm_ShlByCL(t *testing.T) {
	src := `
bits 64
	mov rax, 1
	mov rcx, 8
	shl rax, cl
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0x100 {
		t.Errorf("RAX = %#x, want 0x100", got)
	}
}

// TestAsm_ShlCarryOut — CF gets the bit shifted out of the high end.
func TestAsm_ShlCarryOut(t *testing.T) {
	src := `
bits 64
	mov rax, 0x8000000000000000   ; high bit set
	shl rax, 1                    ; ⇒ rax=0, CF=1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0 {
		t.Errorf("RAX = %#x", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF not set on SHL high bit out")
	}
}

// TestAsm_Shr — logical right shift fills high bits with zero.
func TestAsm_Shr(t *testing.T) {
	src := `
bits 64
	mov rax, 0x80
	shr rax, 4
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0x8 {
		t.Errorf("RAX = %#x", got)
	}
}

// TestAsm_SarNegative — arithmetic right shift preserves the sign bit.
func TestAsm_SarNegative(t *testing.T) {
	src := `
bits 64
	mov rax, -16     ; 0xFFFFFFFFFFFFFFF0
	sar rax, 4       ; ⇒ 0xFFFFFFFFFFFFFFFF (=-1)
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("RAX = %#x, want all-ones", got)
	}
}

// TestAsm_MovzxByte — zero-extending byte load into a 64-bit reg.
func TestAsm_MovzxByte(t *testing.T) {
	src := `
bits 64
	mov rax, 0xFFFFFFFFFFFFFFFF
	mov rbx, 0xAABBCCDDEEFF1180
	movzx rax, bl
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0x80 {
		t.Errorf("RAX = %#x, want 0x80 (low byte zero-extended)", got)
	}
}

// TestAsm_MovsxByte — sign-extending byte load.
func TestAsm_MovsxByte(t *testing.T) {
	src := `
bits 64
	mov rbx, 0x80     ; -128 as signed byte
	movsx rax, bl
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xFFFFFFFFFFFFFF80 {
		t.Errorf("RAX = %#x, want 0xFFFFFFFFFFFFFF80 (sign-extended)", got)
	}
}

// TestAsm_Movsxd — 32→64 sign extension (the long-mode unique encoding).
func TestAsm_Movsxd(t *testing.T) {
	src := `
bits 64
	mov rbx, 0xFFFFFFFF80000000   ; arbitrary upper bits + negative i32
	movsxd rax, ebx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	// EBX = 0x80000000 (negative i32 = -2^31). Sign-extend to 64 → 0xFFFFFFFF80000000.
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xFFFFFFFF80000000 {
		t.Errorf("RAX = %#x, want 0xFFFFFFFF80000000", got)
	}
}

// TestAsm_CmovZ — conditional move only fires when ZF is set.
func TestAsm_CmovZ(t *testing.T) {
	src := `
bits 64
	mov rax, 100
	mov rbx, 200
	cmp rax, 100        ; ZF=1
	cmovz rcx, rbx      ; should fire, rcx ← rbx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 200 {
		t.Errorf("RCX = %d, want 200 (cmovz took)", got)
	}
}

// TestAsm_CmovNZ_NotTaken — CMOVcc must leave the destination unchanged
// when the condition fails. In particular, the upper bits do NOT
// zero-extend even in 32-bit operand mode (that's the whole point —
// CMOVcc is a "conditional move" not a "zero-extending conditional
// move").
func TestAsm_CmovNZ_NotTaken(t *testing.T) {
	src := `
bits 64
	mov rax, 0xAAAAAAAAAAAAAAAA
	mov rbx, 0xBBBBBBBBBBBBBBBB
	cmp rax, rax        ; equal ⇒ ZF=1
	cmovnz rax, rbx     ; should NOT fire because ZF=1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xAAAAAAAAAAAAAAAA {
		t.Errorf("RAX = %#x, want 0xAA...AA (cmov skipped)", got)
	}
}

// TestAsm_SetCC — SETcc writes 1 or 0 into a byte destination.
func TestAsm_SetCC(t *testing.T) {
	src := `
bits 64
	xor rax, rax       ; rax = 0
	xor rbx, rbx
	cmp rax, rbx       ; equal ⇒ ZF=1
	sete al            ; al = 1
	mov rcx, rax       ; capture
	mov rdx, 5
	cmp rdx, 10
	setg dl            ; dl = 0 (5 > 10 is false signed)
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RCX); got != 1 {
		t.Errorf("RCX (sete result) = %d, want 1", got)
	}
	if got := r.cpu.GetReg8(x86_64.DL); got != 0 {
		t.Errorf("DL (setg result) = %d, want 0", got)
	}
}

// TestAsm_ShlByte — byte-form shift (0xD0/D2/C0). The Linux 6.18 boot
// hit this on `rcl bl, 1` in early init; we cover the SHL byte form too
// since the same opcode family handles them all.
func TestAsm_ShlByte(t *testing.T) {
	src := `
bits 64
	mov al, 0x01
	shl al, 1            ; D0 /4 → AL = 0x02
	shl al, 1            ; D0 /4 → AL = 0x04
	shl al, 3            ; C0 /4 → AL = 0x20
	mov cl, 2
	shl al, cl           ; D2 /4 → AL = 0x80
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg8(x86_64.AL); got != 0x80 {
		t.Errorf("AL = %#x, want 0x80", got)
	}
}

// TestAsm_RclBl1 — RCL byte by 1, the exact instruction from the Linux
// 6.18 early-init code that surfaced the missing 0xD0 opcode.
//
//	stc                  ; CF=1
//	mov bl, 0x80         ; high bit set
//	rcl bl, 1            ; BL high bit (1) -> CF; old CF (1) -> bit 0
//	                     ; ⇒ BL = 0x01, CF = 1
func TestAsm_RclBl1(t *testing.T) {
	src := `
bits 64
	stc
	mov bl, 0x80
	rcl bl, 1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg8(x86_64.BL); got != 0x01 {
		t.Errorf("BL = %#x, want 0x01", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF cleared after rcl that rotated 1 out the top")
	}
}

// TestAsm_RcrBl1 — RCR mirror.
func TestAsm_RcrBl1(t *testing.T) {
	src := `
bits 64
	stc
	mov bl, 0x01
	rcr bl, 1            ; bit 0 (1) -> CF; old CF (1) -> bit 7
	                     ; ⇒ BL = 0x80, CF = 1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg8(x86_64.BL); got != 0x80 {
		t.Errorf("BL = %#x, want 0x80", got)
	}
	if r.cpu.GetRFLAGS()&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF cleared after rcr that rotated 1 out the bottom")
	}
}

// TestAsm_RolRor — plain rotates without carry. 8-bit ROL by 4 swaps
// the nibbles; ROR by the same amount swaps them back.
func TestAsm_RolRor(t *testing.T) {
	src := `
bits 64
	mov al, 0xA5         ; 1010_0101
	rol al, 4            ; ⇒ 0101_1010 = 0x5A
	mov bl, al
	ror bl, 4            ; ⇒ back to 0xA5
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg8(x86_64.AL); got != 0x5A {
		t.Errorf("AL = %#x, want 0x5A", got)
	}
	if got := r.cpu.GetReg8(x86_64.BL); got != 0xA5 {
		t.Errorf("BL = %#x, want 0xA5", got)
	}
}

// TestAsm_RolRorWord — same for 16-bit (covers the count-mask path).
func TestAsm_RolRorWord(t *testing.T) {
	src := `
bits 64
	mov ax, 0x1234
	rol ax, 8            ; ⇒ 0x3412
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX) & 0xFFFF; got != 0x3412 {
		t.Errorf("AX = %#x, want 0x3412", got)
	}
}
