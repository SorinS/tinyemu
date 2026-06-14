package emu_test

import (
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/asm/emu"
)

func lineByNum(r *emu.Result, line int) *emu.LineState {
	for i := range r.Lines {
		if r.Lines[i].Line == line {
			return &r.Lines[i]
		}
	}
	return nil
}

func changed(ls *emu.LineState, name string) (uint64, bool) {
	if ls == nil {
		return 0, false
	}
	for _, rv := range ls.Changed {
		if rv.Name == name {
			return rv.Value, true
		}
	}
	return 0, false
}

func flag(ls *emu.LineState, name string) (uint64, bool) {
	if ls == nil {
		return 0, false
	}
	for _, rv := range ls.Flags {
		if rv.Name == name {
			return rv.Value, true
		}
	}
	return 0, false
}

// A clean program that RETs to the sentinel: per-line register changes are
// attributed correctly and the run reports completion.
func TestRunAll_CleanReturn(t *testing.T) {
	src := "" +
		"  mov rax, 5\n" + // line 0
		"  mov rbx, 7\n" + // line 1
		"  add rax, rbx\n" + // line 2: rax = 12
		"  ret\n" // line 3
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed", r.Stop, r.Error)
	}
	if v, ok := changed(lineByNum(r, 0), "rax"); !ok || v != 5 {
		t.Errorf("line0 rax = %#x ok=%v, want 5", v, ok)
	}
	if v, ok := changed(lineByNum(r, 1), "rbx"); !ok || v != 7 {
		t.Errorf("line1 rbx = %#x ok=%v, want 7", v, ok)
	}
	if v, ok := changed(lineByNum(r, 2), "rax"); !ok || v != 12 {
		t.Errorf("line2 rax = %#x ok=%v, want 12", v, ok)
	}
}

// xor eax, eax changes no GPR (rax was 0) but sets ZF/PF — flags must be
// attributed to the line.
func TestRunAll_FlagsAttributed(t *testing.T) {
	r, err := emu.RunAll("  xor eax, eax\n  ret\n")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := flag(lineByNum(r, 0), "ZF"); !ok || v != 1 {
		t.Errorf("line0 ZF = %d ok=%v, want 1", v, ok)
	}
}

// Run-to-cursor stops before the cursor line; later lines have no state.
func TestRunToLine_StopsBefore(t *testing.T) {
	src := "" +
		"  mov rax, 1\n" + // line 0
		"  mov rax, 2\n" + // line 1
		"  mov rax, 3\n" + // line 2 (cursor)
		"  ret\n" // line 3
	r, err := emu.RunToLine(src, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopReached || r.StopLine != 2 {
		t.Fatalf("Stop = %q line=%d, want reached-line @2", r.Stop, r.StopLine)
	}
	if v, ok := changed(lineByNum(r, 1), "rax"); !ok || v != 2 {
		t.Errorf("line1 rax = %#x, want 2", v)
	}
	if lineByNum(r, 2) != nil {
		t.Errorf("line2 should not have executed")
	}
}

// A guest fault (divide by zero, #DE) is reported as a stop reason, never a
// panic, with no IDT configured.
func TestRun_FaultDoesNotPanic(t *testing.T) {
	src := "" +
		"  xor edx, edx\n" +
		"  mov eax, 1\n" +
		"  xor ecx, ecx\n" +
		"  div ecx\n" + // #DE
		"  ret\n"
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if r.Stop != emu.StopFault {
		t.Fatalf("Stop = %q, want fault (Error=%q)", r.Stop, r.Error)
	}
	if r.Error == "" {
		t.Errorf("fault stop should carry an Error message")
	}
}

// An unbroken loop terminates at the step cap rather than hanging.
func TestRun_InfiniteLoopHitsCap(t *testing.T) {
	src := "loop:\n  jmp loop\n" // jmp on line 1
	r, err := emu.Run(src, emu.Options{StopBeforeLine: -1, MaxSteps: 50})
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopMaxSteps {
		t.Fatalf("Stop = %q, want max-steps", r.Stop)
	}
	if r.Steps != 50 {
		t.Errorf("Steps = %d, want 50", r.Steps)
	}
}

// Branches resolve and the post-branch line runs (labels work end to end).
func TestRun_BranchTaken(t *testing.T) {
	src := "" +
		"  mov rax, 0\n" + // line 0
		"  cmp rax, 0\n" + // line 1
		"  jne skip\n" + // line 2: not taken (rax==0)
		"  mov rbx, 0x42\n" + // line 3: executes
		"skip:\n" + // line 4 (label)
		"  ret\n" // line 5
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed", r.Stop, r.Error)
	}
	if v, ok := changed(lineByNum(r, 3), "rbx"); !ok || v != 0x42 {
		t.Errorf("line3 rbx = %#x ok=%v, want 0x42 (jne not taken)", v, ok)
	}
}

func TestRun_AssembleError(t *testing.T) {
	if _, err := emu.RunAll("  movxx rax, rbx\n"); err == nil {
		t.Errorf("expected assemble error for unknown mnemonic")
	}
}

func TestResult_FullRegsPresent(t *testing.T) {
	r, err := emu.RunAll("  mov rsp, rsp\n  ret\n")
	if err != nil {
		t.Fatal(err)
	}
	ls := lineByNum(r, 0)
	if ls == nil || len(ls.Regs) != 16 {
		t.Fatalf("want 16 full regs, got %d", len(ls.Regs))
	}
	if ls.Regs[0].Name != "rax" || !strings.HasPrefix(ls.Regs[8].Name, "r8") {
		t.Errorf("reg names off: %q ... %q", ls.Regs[0].Name, ls.Regs[8].Name)
	}
}
