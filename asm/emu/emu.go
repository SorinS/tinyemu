// Package emu executes a hand-written assembly program in the tinyemu-go
// x86-64 CPU and reports the machine state attributable to each source line.
// It is the execution backend for editor tooling (the asm language server's
// "run to cursor", inline register/flag state, and breakpoints): assemble the
// buffer, run it instruction by instruction in a flat 64-bit sandbox, and
// collapse the per-step trace down to "what each line did".
//
// The sandbox is deliberately minimal: long mode with paging disabled (linear
// == physical), code loaded at 1 MiB, a stack near 3 MiB seeded with a
// sentinel return address so a balanced RET is detectable, and the power-on
// register file (all GPRs zero except RDX = 0x600). There is no IDT, so a
// guest fault (a bad opcode, a #DE, a stray #PF) is surfaced as a stop reason
// rather than vectored — and every step is wrapped so malformed input can
// never panic the caller (e.g. a language server).
package emu

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/asm"
	"github.com/jtolio/tinyemu-go/cpu/x86_64"
	"github.com/jtolio/tinyemu-go/mem"
)

// Sandbox layout. Code sits well above zero so a guest write through a
// zero/low pointer (the reset GPRs are zero) corrupts scratch RAM, not code.
const (
	codeBase     = 0x100000          // 1 MiB — where the program is loaded
	stackTop     = 0x300000          // 3 MiB — initial RSP
	ramSize      = 0x400000          // 4 MiB total RAM
	sentinelRet  = 0xDEADBEEF        // return address marking "program returned"
	defaultSteps = 100000            // step cap (an unbroken loop ends here)
)

// regNames maps a GPR index to its 64-bit name, in x86 encoding order.
var regNames = [16]string{
	"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi",
	"r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15",
}

// flagDefs are the arithmetic-status flags, in display order, with their bit
// positions in RFLAGS.
var flagDefs = []struct {
	name string
	bit  uint
}{
	{"CF", 0}, {"PF", 2}, {"AF", 4}, {"ZF", 6}, {"SF", 7}, {"OF", 11},
}

// RegVal is a named register or flag and its value (a flag's value is 0/1).
type RegVal struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
}

// LineState is the machine state attributable to one source line: what it
// changed the last time it executed, plus the full register file after, for
// hover/panel views.
type LineState struct {
	Line    int      `json:"line"`    // 0-based source line
	Changed []RegVal `json:"changed"` // GPRs this line changed (name → new value)
	Flags   []RegVal `json:"flags"`   // status flags this line changed (value 0/1)
	Regs    []RegVal `json:"regs"`    // full GPR file after this line
	RIP     uint64   `json:"rip"`
	RFLAGS  uint64   `json:"rflags"`
}

// Result is the outcome of running a program.
type Result struct {
	Lines    []LineState `json:"lines"`           // one per executed line, in first-seen order
	Stop     string      `json:"stop"`            // why the run ended (see Stop* values)
	StopLine int         `json:"stopLine"`        // line about to execute when stopped, or -1
	Steps    int         `json:"steps"`           // instructions executed
	Error    string      `json:"error,omitempty"` // guest fault / exception text, if any
}

// Stop reasons.
const (
	StopCompleted = "completed"           // RET'd to the sentinel (clean exit)
	StopReached   = "reached-line"        // hit StopBeforeLine or a breakpoint
	StopFault     = "fault"               // guest fault/exception (see Result.Error)
	StopMaxSteps  = "max-steps"           // step cap reached (likely an infinite loop)
	StopOutside   = "ran-outside-program" // RIP left the assembled code (e.g. RET with no frame)
)

// Options tunes a run.
type Options struct {
	// StopBeforeLine halts the run just before executing the instruction on
	// this 0-based source line (run-to-cursor). A negative value disables it.
	StopBeforeLine int
	// Breakpoints halts the run just before executing any instruction whose
	// source line is in this set.
	Breakpoints map[int]bool
	// MaxSteps overrides the default step cap when > 0.
	MaxSteps int
}

// RunAll executes the whole program (no run-to-cursor stop), returning the
// per-line state for every line that executed.
func RunAll(src string) (*Result, error) {
	return Run(src, Options{StopBeforeLine: -1})
}

// RunToLine executes up to (but not including) the instruction on the given
// 0-based source line — classic run-to-cursor.
func RunToLine(src string, line int) (*Result, error) {
	return Run(src, Options{StopBeforeLine: line})
}

// Run assembles src and executes it under the given options. It returns an
// error only when the program fails to assemble; a guest fault is reported via
// Result.Stop / Result.Error, not as a Go error.
//
// Note: Options{} (zero value) has StopBeforeLine == 0, which stops before the
// first line. Prefer RunAll / RunToLine, or set StopBeforeLine explicitly.
func Run(src string, opts Options) (*Result, error) {
	listing, err := asm.AssembleListing(src)
	if err != nil {
		return nil, err
	}

	// rip → source line (linear scan; programs here are small).
	lineAt := func(rip uint64) int {
		if rip < codeBase {
			return -1
		}
		off := int64(rip - codeBase)
		for _, s := range listing.Spans {
			if off >= s.Addr && off < s.Addr+int64(s.Len) {
				return s.Line
			}
		}
		return -1
	}

	mm := mem.NewPhysMemoryMap()
	defer mm.Close()
	if _, err := mm.RegisterRAM(0, ramSize, 0); err != nil {
		return nil, err
	}
	for i, b := range listing.Bytes {
		_ = mm.Write8(codeBase+uint64(i), b)
	}
	_ = mm.Write64(stackTop, sentinelRet) // a balanced RET lands here

	c := x86_64.NewCPU(mm)
	c.SetupFlatLongMode()
	c.SetReg64(x86_64.RSP, stackTop)
	c.SetRIP(codeBase)

	maxN := opts.MaxSteps
	if maxN <= 0 {
		maxN = defaultSteps
	}

	res := &Result{StopLine: -1}
	idx := map[int]int{} // line → position in res.Lines
	prev := snapshot(c)  // state before the next instruction

	for res.Steps < maxN {
		rip := c.GetRIP()
		if rip == sentinelRet {
			res.Stop = StopCompleted
			break
		}
		cur := lineAt(rip)
		if cur < 0 {
			res.Stop = StopOutside
			break
		}
		if (opts.StopBeforeLine >= 0 && cur == opts.StopBeforeLine) || opts.Breakpoints[cur] {
			res.Stop = StopReached
			res.StopLine = cur
			break
		}

		if ferr := safeStep(c); ferr != nil {
			res.Stop = StopFault
			res.Error = ferr.Error()
			res.StopLine = cur
			break
		}
		res.Steps++

		now := snapshot(c)
		ls := lineState(cur, prev, now)
		if p, ok := idx[cur]; ok {
			res.Lines[p] = ls // last execution wins (loops show final iteration)
		} else {
			idx[cur] = len(res.Lines)
			res.Lines = append(res.Lines, ls)
		}
		prev = now
	}
	if res.Stop == "" {
		res.Stop = StopMaxSteps
	}
	return res, nil
}

// snap is a register-file snapshot used to diff one step.
type snap struct {
	gpr    [16]uint64
	rip    uint64
	rflags uint64
}

func snapshot(c *x86_64.CPU) snap {
	var s snap
	for i := 0; i < 16; i++ {
		s.gpr[i] = c.GetReg64(i)
	}
	s.rip = c.GetRIP()
	s.rflags = c.GetRFLAGS()
	return s
}

// lineState diffs prev→now to attribute changes to a line, and captures the
// full register file after.
func lineState(line int, prev, now snap) LineState {
	ls := LineState{Line: line, RIP: now.rip, RFLAGS: now.rflags}
	for i := 0; i < 16; i++ {
		if now.gpr[i] != prev.gpr[i] {
			ls.Changed = append(ls.Changed, RegVal{regNames[i], now.gpr[i]})
		}
		ls.Regs = append(ls.Regs, RegVal{regNames[i], now.gpr[i]})
	}
	for _, f := range flagDefs {
		nb := (now.rflags >> f.bit) & 1
		if pb := (prev.rflags >> f.bit) & 1; nb != pb {
			ls.Flags = append(ls.Flags, RegVal{f.name, nb})
		}
	}
	return ls
}

// safeStep runs one instruction, converting any panic into an error so a
// malformed program can never take down the caller. (Step already turns a
// guest fault through the absent IDT into a returned error; this is a backstop
// for anything that still escapes.)
func safeStep(c *x86_64.CPU) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return c.Step()
}
