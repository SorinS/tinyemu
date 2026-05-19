package x86_64_test

// These tests adapt cases from third-party Blink emulator's assembly
// test suite (bin/blink.git/test/asm/, by Justine Tunney; see that
// directory's README.md). Blink's tests are written in GAS AT&T syntax
// and use a Jcc + INT3 idiom to signal failure — neither of which our
// M1 decoder implements yet. So instead of running the .S files
// directly, we reimplement each scenario as a NASM-Intel-syntax block
// and inspect RFLAGS in Go after the program halts.
//
// As more opcodes land (Jcc, MOVZX/MOVSX, XOR, AND, INT3, SYSCALL),
// more of Blink's tests will become runnable directly. For now this
// file is a foothold — a way of saying "we're tracking the same
// behaviours Blink validates."

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
)

// runAndGetFlags assembles src, runs it, and returns RFLAGS for
// inspection. Common to every Blink-adapted test below.
func runAndGetFlags(t *testing.T, src string) (*x86_64.CPU, uint64) {
	t.Helper()
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 200); err != nil {
		t.Fatalf("run: %v", err)
	}
	return r.cpu, r.cpu.GetRFLAGS()
}

// Adapted from blink/test/asm/overflow.S "overflow add 64".
// Adds 1 to 0x7FFF...FFFE: first add OF=0, second add OF=1.
func TestBlink_OverflowAdd64(t *testing.T) {
	// First half: 0x7FFFFFFFFFFFFFFE + 1 = 0x7FFFFFFFFFFFFFFF, no signed overflow.
	cpu, fl := runAndGetFlags(t, `
bits 64
	mov rax, 0x7FFFFFFFFFFFFFFE
	add rax, 1
	hlt
`)
	if fl&x86_64.RFLAGS_OF != 0 {
		t.Errorf("OF set on 7F...FE + 1 (no signed overflow expected)")
	}
	if got := cpu.GetReg64(x86_64.RAX); got != 0x7FFFFFFFFFFFFFFF {
		t.Errorf("RAX = %#x", got)
	}

	// Second half: 0x7FFF...FFFF + 1 = 0x8000...0000, signed overflow.
	cpu, fl = runAndGetFlags(t, `
bits 64
	mov rax, 0x7FFFFFFFFFFFFFFF
	add rax, 1
	hlt
`)
	if fl&x86_64.RFLAGS_OF == 0 {
		t.Errorf("OF clear on 7F...FF + 1 (signed overflow expected)")
	}
	if got := cpu.GetReg64(x86_64.RAX); got != 0x8000000000000000 {
		t.Errorf("RAX = %#x", got)
	}
}

// Adapted from blink/test/asm/overflow.S "overflow sub 64".
// Subtracts -1 from 0x7FFF...FFFE: the SUB performs the same arithmetic
// as ADD 1, so the same overflow pattern shows up.
func TestBlink_OverflowSub64(t *testing.T) {
	// "sub $-1" with imm8=-1 → operand is sign-extended to 0xFFFF... → SUB.
	// 0x7FFFFFFFFFFFFFFE − (−1) = 0x7FFFFFFFFFFFFFFF, no overflow.
	cpu, fl := runAndGetFlags(t, `
bits 64
	mov rax, 0x7FFFFFFFFFFFFFFE
	sub rax, -1
	hlt
`)
	if fl&x86_64.RFLAGS_OF != 0 {
		t.Errorf("OF set on 7F...FE - (-1); want clear")
	}
	if got := cpu.GetReg64(x86_64.RAX); got != 0x7FFFFFFFFFFFFFFF {
		t.Errorf("RAX = %#x", got)
	}

	cpu, fl = runAndGetFlags(t, `
bits 64
	mov rax, 0x7FFFFFFFFFFFFFFF
	sub rax, -1
	hlt
`)
	if fl&x86_64.RFLAGS_OF == 0 {
		t.Errorf("OF clear on 7F...FF - (-1); want set")
	}
	if got := cpu.GetReg64(x86_64.RAX); got != 0x8000000000000000 {
		t.Errorf("RAX = %#x", got)
	}
}

// Adapted from blink/test/asm/add.S "add64".
// 0x1234567812345678 + 1 = 0x1234567812345679, all flag bits clear
// except none of CF/SF/OF/ZF/PF (PF is on the low byte; 0x79 has odd
// parity, so PF=0).
func TestBlink_Add64Flags(t *testing.T) {
	cpu, fl := runAndGetFlags(t, `
bits 64
	mov rax, 0x1234567812345678
	mov rbx, 1
	add rax, rbx
	hlt
`)
	if got := cpu.GetReg64(x86_64.RAX); got != 0x1234567812345679 {
		t.Errorf("RAX = %#x", got)
	}
	check := func(name string, mask uint64, want bool) {
		got := fl&mask != 0
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("CF", x86_64.RFLAGS_CF, false)
	check("ZF", x86_64.RFLAGS_ZF, false)
	check("SF", x86_64.RFLAGS_SF, false)
	check("OF", x86_64.RFLAGS_OF, false)
	check("PF", x86_64.RFLAGS_PF, false) // low byte = 0x79 (= 0b01111001), 5 bits set ⇒ odd parity ⇒ PF clear
}

// Adapted from blink/test/asm/add.S "add32".
// In 32-bit mode the operation is the same on the low 32 bits, but the
// long-mode rule says the upper 32 zero-extend. RBX is untouched (the
// add is to %eax, not %rax).
func TestBlink_Add32_ZeroExtends(t *testing.T) {
	cpu, _ := runAndGetFlags(t, `
bits 64
	mov rax, 0x1234567812345678
	mov rbx, 0x0001000100000001
	add eax, ebx
	hlt
`)
	// EAX = 0x12345678 + 0x00000001 = 0x12345679. Upper 32 of RAX zeroed.
	if got := cpu.GetReg64(x86_64.RAX); got != 0x0000000012345679 {
		t.Errorf("RAX = %#x, want 0x0000000012345679 (32-bit add zero-extends)", got)
	}
	// RBX untouched.
	if got := cpu.GetReg64(x86_64.RBX); got != 0x0001000100000001 {
		t.Errorf("RBX = %#x, want 0x0001000100000001 (source unchanged)", got)
	}
}

// Adapted from blink/test/asm/add.S "add16".
// 16-bit add: low 16 of RAX updated, upper 48 preserved.
func TestBlink_Add16_PreservesUpper(t *testing.T) {
	cpu, _ := runAndGetFlags(t, `
bits 64
	mov rax, 0x1234567812345678
	mov rbx, 0x0001000100010001
	add ax, bx
	hlt
`)
	// Low 16 of RAX: 0x5678 + 0x0001 = 0x5679. Upper 48 preserved.
	if got := cpu.GetReg64(x86_64.RAX); got != 0x1234567812345679 {
		t.Errorf("RAX = %#x", got)
	}
	if got := cpu.GetReg64(x86_64.RBX); got != 0x0001000100010001 {
		t.Errorf("RBX = %#x (source clobbered)", got)
	}
}
