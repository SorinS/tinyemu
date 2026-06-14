package emu_test

import (
	"testing"

	"github.com/jtolio/tinyemu-go/asm/emu"
)

func TestDetectArch(t *testing.T) {
	cases := []struct {
		src  string
		want emu.Arch
	}{
		{"  mov rax, rbx\n  ret\n", emu.ArchX86},
		{"BITS 32\n  mov eax, ebx\n", emu.ArchX86},
		{"  addi a0, zero, 5\n  ret\n", emu.ArchRISCV},
		{"  add a0, a1, a2\n  jal ra, 8\n", emu.ArchRISCV},
		{"; arch: riscv64\n  add a0, a1, a2\n", emu.ArchRISCV},
		{"; arch: x86\n  addi a0, zero, 5\n", emu.ArchX86}, // directive overrides heuristic
		{"", emu.ArchX86}, // default
	}
	for _, c := range cases {
		if got := emu.DetectArch(c.src); got != c.want {
			t.Errorf("DetectArch(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// A RISC-V program runs in cpu/riscv: 32 GPRs (ABI names), returns via ra.
func TestRunAll_RISCV(t *testing.T) {
	src := "" +
		"  addi a0, zero, 5\n" + // line 0
		"  addi a1, zero, 7\n" + // line 1
		"  add a0, a0, a1\n" + // line 2: a0 = 12
		"  ret\n" // line 3
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Arch != "riscv" || r.Bits != 64 {
		t.Errorf("Arch/Bits = %s/%d, want riscv/64", r.Arch, r.Bits)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed", r.Stop, r.Error)
	}
	if v, ok := changed(lineByNum(r, 0), "a0"); !ok || v != 5 {
		t.Errorf("line0 a0 = %#x ok=%v, want 5", v, ok)
	}
	if v, ok := changed(lineByNum(r, 2), "a0"); !ok || v != 12 {
		t.Errorf("line2 a0 = %#x ok=%v, want 12", v, ok)
	}
	// RISC-V has no condition flags.
	for _, ls := range r.Lines {
		if len(ls.Flags) != 0 {
			t.Errorf("RISC-V line %d should have no flags, got %v", ls.Line, ls.Flags)
		}
	}
	// 32 registers, x0 named "zero" and always 0.
	ls := lineByNum(r, 0)
	if ls == nil || len(ls.Regs) != 32 || ls.Regs[0].Name != "zero" || ls.Regs[0].Value != 0 {
		t.Errorf("want 32 regs with zero=0, got %d regs", len(ls.Regs))
	}
}

// Pseudo-instructions (mv, li) work end to end.
func TestRunAll_RISCV_Pseudo(t *testing.T) {
	src := "" +
		"  li a0, 10\n" + // line 0
		"  mv a1, a0\n" + // line 1: a1 = 10
		"  add a2, a0, a1\n" + // line 2: a2 = 20
		"  ret\n"
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s)", r.Stop, r.Error)
	}
	if v, ok := changed(lineByNum(r, 2), "a2"); !ok || v != 20 {
		t.Errorf("line2 a2 = %#x, want 20", v)
	}
}

// A RISC-V loop with a backward branch to a label runs to completion.
func TestRunAll_RISCV_Loop(t *testing.T) {
	src := "" +
		"  li a0, 0\n" + // line 0
		"  li a1, 5\n" + // line 1
		"loop:\n" + // line 2 (label)
		"  addi a0, a0, 1\n" + // line 3
		"  blt a0, a1, loop\n" + // line 4: loop while a0 < 5
		"  ret\n" // line 5
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed", r.Stop, r.Error)
	}
	// After the loop, a0 == 5 (line 3's last execution).
	if v, ok := changed(lineByNum(r, 3), "a0"); !ok || v != 5 {
		t.Errorf("line3 final a0 = %#x ok=%v, want 5", v, ok)
	}
}

// Run-to-cursor works for RISC-V too.
func TestRunToLine_RISCV(t *testing.T) {
	src := "" +
		"  li t0, 1\n" + // line 0
		"  li t0, 2\n" + // line 1
		"  li t0, 3\n" + // line 2 (cursor)
		"  ret\n"
	r, err := emu.RunToLine(src, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopReached || r.StopLine != 2 {
		t.Fatalf("Stop = %q line %d, want reached-line @2", r.Stop, r.StopLine)
	}
	if v, ok := changed(lineByNum(r, 1), "t0"); !ok || v != 2 {
		t.Errorf("line1 t0 = %#x, want 2", v)
	}
	if lineByNum(r, 2) != nil {
		t.Errorf("line2 should not have executed")
	}
}
