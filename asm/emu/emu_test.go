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

// An ARM64 program runs end-to-end: arch detected from the directive, per-line
// register changes attributed, NZCV flags shown, clean return to the sentinel.
func TestRunAll_ARM64(t *testing.T) {
	src := "; arch: arm64\n" + // line 0
		"  movz x0, #5\n" + // line 1
		"  movz x1, #7\n" + // line 2
		"  add x0, x0, x1\n" + // line 3: x0 = 12
		"  subs x2, x0, x0\n" + // line 4: x2 = 0, sets Z
		"  ret\n" // line 5
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Arch != "arm64" {
		t.Fatalf("Arch = %q, want arm64", r.Arch)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed", r.Stop, r.Error)
	}
	if v, ok := changed(lineByNum(r, 1), "x0"); !ok || v != 5 {
		t.Errorf("line1 x0 = %#x ok=%v, want 5", v, ok)
	}
	if v, ok := changed(lineByNum(r, 3), "x0"); !ok || v != 12 {
		t.Errorf("line3 x0 = %#x ok=%v, want 12", v, ok)
	}
	if v, ok := flag(lineByNum(r, 4), "Z"); !ok || v != 1 {
		t.Errorf("line4 Z = %d ok=%v, want 1", v, ok)
	}
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

// A BITS 32 program runs in the i386 core: 8 GPRs with eax… names, clean RET.
func TestRunAll_Bits32(t *testing.T) {
	src := "" +
		"BITS 32\n" + // line 0: directive
		"  mov eax, 5\n" + // line 1
		"  add eax, 3\n" + // line 2: eax = 8
		"  mov ecx, eax\n" + // line 3
		"  ret\n" // line 4
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Bits != 32 {
		t.Errorf("Bits = %d, want 32", r.Bits)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed", r.Stop, r.Error)
	}
	if v, ok := changed(lineByNum(r, 2), "eax"); !ok || v != 8 {
		t.Errorf("line2 eax = %#x ok=%v, want 8", v, ok)
	}
	if v, ok := changed(lineByNum(r, 3), "ecx"); !ok || v != 8 {
		t.Errorf("line3 ecx = %#x ok=%v, want 8", v, ok)
	}
	// 32-bit reports 8 registers with e-names.
	ls := lineByNum(r, 1)
	if ls == nil || len(ls.Regs) != 8 || ls.Regs[0].Name != "eax" {
		t.Errorf("want 8 e-regs, got %d (first %v)", len(ls.Regs), ls)
	}
}

// 32-bit stack works (push/pop with ESP) and a balanced RET completes.
func TestRunAll_Bits32_Stack(t *testing.T) {
	src := "" +
		"BITS 32\n" +
		"  push ebp\n" +
		"  mov ebp, esp\n" +
		"  mov eax, 0x99\n" +
		"  pop ebp\n" +
		"  ret\n"
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s), want completed (32-bit stack/ret broken?)", r.Stop, r.Error)
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

func TestSession_Step(t *testing.T) {
	s, err := emu.NewSession("  mov rax, 5\n  add rax, 3\n  hlt\n")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st := s.State() // at entry, line 0, nothing run
	if st.Line != 0 || st.Steps != 0 {
		t.Fatalf("entry: line=%d steps=%d, want 0/0", st.Line, st.Steps)
	}
	st = s.Step() // mov rax,5
	if v, ok := regOf(st.Regs, "rax"); !ok || v != 5 {
		t.Errorf("after step1 rax=%#x, want 5", v)
	}
	if st.Line != 1 {
		t.Errorf("after step1 current line=%d, want 1", st.Line)
	}
	st = s.Step() // add rax,3 → rax=8; changed should list rax
	if v, ok := regOf(st.Changed, "rax"); !ok || v != 8 {
		t.Errorf("step2 changed rax=%#x, want 8", v)
	}
	st = s.Step() // hlt
	if st.Stop != emu.StopHalted {
		t.Errorf("after hlt stop=%q, want halted", st.Stop)
	}
	if st.Line != -1 {
		t.Errorf("ended: line=%d, want -1", st.Line)
	}
}

func TestSession_Continue_Breakpoint(t *testing.T) {
	src := "  li a0, 0\n  li a1, 5\nloop:\n  addi a0, a0, 1\n  blt a0, a1, loop\n  ret\n"
	s, err := emu.NewSession(src)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// breakpoint on line 4 (the blt). Continue should stop there first iteration.
	st := s.Continue(map[int]bool{4: true}, nil)
	if st.Line != 4 {
		t.Fatalf("continue stopped at line %d, want 4 (breakpoint)", st.Line)
	}
	if v, ok := regOf(st.Regs, "a0"); !ok || v != 1 {
		t.Errorf("at first bp a0=%#x, want 1", v)
	}
	// continue to completion (no breakpoint now)
	st = s.Continue(nil, nil)
	if st.Stop != emu.StopCompleted {
		t.Fatalf("final stop=%q, want completed", st.Stop)
	}
	if v, _ := regOf(st.Regs, "a0"); v != 5 {
		t.Errorf("final a0=%#x, want 5", v)
	}
}

func regOf(list []emu.RegVal, name string) (uint64, bool) {
	for _, r := range list {
		if r.Name == name {
			return r.Value, true
		}
	}
	return 0, false
}

func TestSession_StepBackRestart(t *testing.T) {
	s, _ := emu.NewSession("  mov rax, 1\n  mov rax, 2\n  mov rax, 3\n  hlt\n")
	defer s.Close()
	s.Step() // rax=1
	s.Step() // rax=2
	s.Step() // rax=3, steps=3, paused at hlt (line 3)
	// step back one: replays to step 2 → rax=2
	st := s.StepBack()
	if v, _ := regOf(st.Regs, "rax"); v != 2 || st.Steps != 2 {
		t.Errorf("stepback: rax=%#x steps=%d, want 2/2", v, st.Steps)
	}
	st = s.Restart()
	if st.Steps != 0 || st.Line != 0 {
		t.Errorf("restart: steps=%d line=%d, want 0/0", st.Steps, st.Line)
	}
}

func TestSession_StepOver(t *testing.T) {
	src := "" +
		"  call sub\n" + // line 0
		"  hlt\n" + // line 1
		"sub:\n" + // line 2
		"  mov rax, 0x42\n" + // line 3
		"  ret\n" // line 4
	s, _ := emu.NewSession(src)
	defer s.Close()
	st := s.StepOver() // over the call → runs sub, stops at hlt (line 1)
	if st.Line != 1 {
		t.Fatalf("stepover landed on line %d, want 1 (after the call)", st.Line)
	}
	if v, _ := regOf(st.Regs, "rax"); v != 0x42 {
		t.Errorf("after step-over rax=%#x, want 0x42 (subroutine ran)", v)
	}
}

func TestSession_ConditionalBreak(t *testing.T) {
	src := "  li a0, 0\n  li a1, 5\nloop:\n  addi a0, a0, 1\n  blt a0, a1, loop\n  ret\n"
	s, _ := emu.NewSession(src)
	defer s.Close()
	// break at the blt (line 4) only when a0 == 3
	st := s.Continue(nil, []emu.Cond{{Line: 4, Reg: "a0", Op: "==", Value: 3}})
	if v, _ := regOf(st.Regs, "a0"); v != 3 || st.Line != 4 {
		t.Errorf("conditional break: a0=%#x line=%d, want 3 @ line 4", v, st.Line)
	}
}

func TestSession_ReadMem(t *testing.T) {
	s, _ := emu.NewSession("  mov rax, rax\n  hlt\n")
	defer s.Close()
	// the sandbox seeds the sentinel return (0xDEADBEEE) at the stack top (0x300000).
	b := s.ReadMem(0x300000, 4)
	got := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	if got != 0xDEADBEEE {
		t.Errorf("ReadMem stack top = %#x, want 0xDEADBEEE", got)
	}
}

func TestSession_FPRegisters(t *testing.T) {
	// a0=2 → fa0=2.0 → fa1=4.0; FP registers appear in the view with float values.
	r, err := emu.RunAll("  li a0, 2\n  fcvt.s.w fa0, a0\n  fadd.s fa1, fa0, fa0\n  ret\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopCompleted {
		t.Fatalf("Stop = %q (%s)", r.Stop, r.Error)
	}
	find := func(name string) *emu.RegVal {
		for i := range r.Final {
			if r.Final[i].Name == name {
				return &r.Final[i]
			}
		}
		return nil
	}
	if fa0 := find("fa0"); fa0 == nil || fa0.Float != "2" {
		t.Errorf("fa0 = %+v, want float 2", fa0)
	}
	if fa1 := find("fa1"); fa1 == nil || fa1.Float != "4" {
		t.Errorf("fa1 = %+v, want float 4", fa1)
	}
	// the RISC-V view is 32 GPRs + 32 FP registers.
	if ls := lineByNum(r, 1); ls == nil || len(ls.Regs) != 64 {
		t.Errorf("want 64 registers, got %d", len(ls.Regs))
	}
}

// A program with a data table and a [rel sym] load must read the right value
// at runtime (RIP-relative is origin-independent, so it survives the load base).
func TestRunAll_RelData(t *testing.T) {
	src := "" +
		"bits 64\n" +
		"  lea rsi, [rel arr]\n" +
		"  mov rax, [rsi+8]\n" + // arr[1] = 43
		"  hlt\n" +
		"arr: dq 42, 43, 44\n"
	r, err := emu.RunAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stop != emu.StopHalted {
		t.Fatalf("stop=%q (%s)", r.Stop, r.Error)
	}
	var rax uint64
	for _, rv := range r.Final {
		if rv.Name == "rax" {
			rax = rv.Value
		}
	}
	if rax != 43 {
		t.Errorf("rax = %d, want 43 (arr[1] via [rel]+disp)", rax)
	}
}
