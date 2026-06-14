// Package emu executes a hand-written assembly program in a tinyemu-go CPU and
// reports the machine state attributable to each source line. It is the
// execution backend for editor tooling (the asm language server's "run to
// cursor", inline register/flag state, and breakpoints): assemble the buffer,
// run it instruction by instruction in a flat sandbox, and collapse the
// per-step trace down to "what each line did".
//
// Three targets, chosen by DetectArch (an explicit "arch:" directive, else a
// content heuristic, else x86): x86-64 (cpu/x86_64), 32-bit x86 via a BITS 32
// directive (cpu/x86), and RISC-V RV64I+M (cpu/riscv). x86 has condition flags;
// RISC-V does not, and returns via the ra register rather than a stack.
//
// The sandbox is deliberately minimal: paging off (linear == physical), code at
// a fixed base, a return sentinel so a clean return is detectable, and the
// power-on register file. There is no trap/IDT vectoring, so a guest fault is
// surfaced as a stop reason; every step is recover()-wrapped so malformed input
// can never panic the caller (e.g. a language server).
package emu

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jtolio/tinyemu-go/asm"
	rvasm "github.com/jtolio/tinyemu-go/asm/riscv"
	rvcpu "github.com/jtolio/tinyemu-go/cpu/riscv"
	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/cpu/x86_64"
	"github.com/jtolio/tinyemu-go/mem"
)

const (
	// x86 sandbox: RAM at 0, code at 1 MiB, stack at 3 MiB.
	codeBaseX86 = 0x100000
	stackTopX86 = 0x300000
	ramSizeX86  = 0x400000
	regSP       = 4 // (R/E)SP index

	// RISC-V sandbox: RAM/code at 0x8000_0000 (the conventional RAM base),
	// stack 3 MiB in.
	ramBaseRV  = 0x80000000
	codeBaseRV = 0x80000000
	stackTopRV = 0x80300000
	ramSizeRV  = 0x400000
	rvRegRA    = 1 // x1
	rvRegSP    = 2 // x2

	// Even so a RISC-V jalr (which clears bit 0) still lands exactly on it.
	sentinelRet  = 0xDEADBEEE
	defaultSteps = 100000
)

var regNames64 = []string{
	"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi",
	"r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15",
}

var regNames32 = []string{
	"eax", "ecx", "edx", "ebx", "esp", "ebp", "esi", "edi",
}

var regNamesRV = []string{
	"zero", "ra", "sp", "gp", "tp", "t0", "t1", "t2",
	"s0", "s1", "a0", "a1", "a2", "a3", "a4", "a5",
	"a6", "a7", "s2", "s3", "s4", "s5", "s6", "s7",
	"s8", "s9", "s10", "s11", "t3", "t4", "t5", "t6",
}

// fpNamesRV are the 32 RISC-V floating-point registers (F/D), ABI names.
var fpNamesRV = []string{
	"ft0", "ft1", "ft2", "ft3", "ft4", "ft5", "ft6", "ft7",
	"fs0", "fs1", "fa0", "fa1", "fa2", "fa3", "fa4", "fa5",
	"fa6", "fa7", "fs2", "fs3", "fs4", "fs5", "fs6", "fs7",
	"fs8", "fs9", "fs10", "fs11", "ft8", "ft9", "ft10", "ft11",
}

// regNamesRVall is the full RISC-V register view: 32 integer GPRs followed by
// the 32 FP registers (so index i<32 is a GPR, i>=32 an FP reg).
var regNamesRVall = append(append([]string{}, regNamesRV...), fpNamesRV...)

// fpNameSet marks which register names are floating-point (for display).
var fpNameSet = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range fpNamesRV {
		m[n] = true
	}
	return m
}()

// flagDefs are the x86 arithmetic-status flags, in display order, with their
// bit positions in (R/E)FLAGS.
var flagDefs = []struct {
	name string
	bit  uint
}{
	{"CF", 0}, {"PF", 2}, {"AF", 4}, {"ZF", 6}, {"SF", 7}, {"OF", 11},
}

// Arch is the target ISA of a program.
type Arch int

const (
	ArchX86 Arch = iota
	ArchRISCV
)

func (a Arch) String() string {
	if a == ArchRISCV {
		return "riscv"
	}
	return "x86"
}

// rvOnly / x86Only are mnemonics distinctive to one ISA (shared ones like
// add/sub/and/or/xor are deliberately excluded). Used by DetectArch.
var rvOnly = words("addi addiw jal jalr lui auipc ecall ebreak beq bne blt bge bltu bgeu " +
	"lw lwu lh lhu lbu ld sd sb sh slli srli srai slliw srliw sraiw mv li mulw divw remw " +
	"fence csrrw csrrs csrrc beqz bnez snez seqz j jr")
var x86Only = words("mov push pop lea jmp call cmp test leave syscall int3 cdq cqo " +
	"movzx movsx je jne jz jnz jg jl inc dec stosb lodsb")

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// DetectArch picks the target ISA for a program: an explicit "arch:" directive
// (in a comment, e.g. "; arch: riscv64") wins; otherwise the count of
// ISA-distinctive mnemonics decides; ties and empty input default to x86.
func DetectArch(src string) Arch {
	for _, raw := range strings.Split(src, "\n") {
		line := strings.ToLower(raw)
		if i := strings.Index(line, "arch:"); i >= 0 {
			rest := line[i+5:]
			switch {
			case strings.Contains(rest, "riscv"), strings.Contains(rest, "rv32"), strings.Contains(rest, "rv64"):
				return ArchRISCV
			case strings.Contains(rest, "x86"), strings.Contains(rest, "amd64"), strings.Contains(rest, "i386"):
				return ArchX86
			}
		}
	}
	rv, x := 0, 0
	for _, raw := range strings.Split(src, "\n") {
		switch f := firstToken(raw); {
		case rvOnly[f], strings.HasPrefix(f, "c."): // c.* = RISC-V compressed
			rv++
		case x86Only[f]:
			x++
		}
	}
	if rv > x {
		return ArchRISCV
	}
	return ArchX86
}

// firstToken returns the lower-cased first word of a line, comment and trailing
// label ':' stripped.
func firstToken(line string) string {
	if i := strings.IndexAny(line, "#;"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		line = line[:i]
	}
	return strings.ToLower(strings.TrimSuffix(line, ":"))
}

// RegVal is a named register or flag with its value. Hex is the exact display
// form (JSON numbers lose precision above 2^53, so editors should render Hex,
// not Value). Float is the floating-point interpretation, set for FP registers.
type RegVal struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
	Hex   string `json:"hex"`
	Float string `json:"float,omitempty"`
}

// mkReg builds a RegVal, computing the exact hex string and, for FP registers,
// the float interpretation (NaN-boxed single, else double).
func mkReg(name string, value uint64) RegVal {
	rv := RegVal{Name: name, Value: value, Hex: fmt.Sprintf("%#x", value)}
	if fpNameSet[name] {
		rv.Float = fpDisplay(value)
	}
	return rv
}

func fpDisplay(bits uint64) string {
	if bits>>32 == 0xFFFFFFFF { // NaN-boxed single-precision value
		return strconv.FormatFloat(float64(math.Float32frombits(uint32(bits))), 'g', -1, 32)
	}
	return strconv.FormatFloat(math.Float64frombits(bits), 'g', -1, 64)
}

// LineState is the machine state attributable to one source line.
type LineState struct {
	Line    int      `json:"line"`
	Changed []RegVal `json:"changed"` // registers this line changed
	Flags   []RegVal `json:"flags"`   // status flags this line changed (x86 only)
	Regs    []RegVal `json:"regs"`    // full register file after this line
	RIP     uint64   `json:"rip"`     // program counter after (RIP / EIP / PC)
	RFLAGS  uint64   `json:"rflags"`
}

// Result is the outcome of running a program.
type Result struct {
	Arch     string      `json:"arch"`            // "x86" or "riscv"
	Bits     int         `json:"bits"`            // 32 or 64
	Lines    []LineState `json:"lines"`           // one per executed line, first-seen order
	Final    []RegVal    `json:"final"`           // full register file when the run ended
	Stop     string      `json:"stop"`            // why the run ended
	StopLine int         `json:"stopLine"`        // line about to execute when stopped, or -1
	Steps    int         `json:"steps"`           // instructions executed
	Error    string      `json:"error,omitempty"` // guest fault text, if any
}

// Stop reasons.
const (
	StopCompleted = "completed"
	StopReached   = "reached-line"
	StopFault     = "fault"
	StopMaxSteps  = "max-steps"
	StopOutside   = "ran-outside-program"
	StopHalted    = "halted" // HLT / WFI — a normal program end
)

// Options tunes a run.
type Options struct {
	StopBeforeLine int          // run-to-cursor; negative disables
	Breakpoints    map[int]bool // stop before executing a line in this set
	MaxSteps       int          // overrides the default step cap when > 0
}

// RunAll executes the whole program.
func RunAll(src string) (*Result, error) { return Run(src, Options{StopBeforeLine: -1}) }

// RunToLine executes up to (but not including) the given 0-based source line.
func RunToLine(src string, line int) (*Result, error) {
	return Run(src, Options{StopBeforeLine: line})
}

// Run assembles src (arch auto-detected) and executes it. It errors only when
// the program fails to assemble; a guest fault is reported via Result.Stop.
func Run(src string, opts Options) (*Result, error) {
	sb, err := buildSandbox(src)
	if err != nil {
		return nil, err
	}
	defer sb.close()
	res := traceLoop(sb, opts)
	res.Arch = sb.arch.String()
	return res, nil
}

// span maps a code offset (from the load base) to a source line.
type span struct {
	line   int
	addr   int64
	length int
}

// sandbox is a loaded, ready-to-run program: a CPU positioned at the entry, the
// memory backing it, and the line map. Shared by Run (one-shot) and Session
// (stepping).
type sandbox struct {
	mm       *mem.PhysMemoryMap
	r        runner
	codeBase uint64
	sentinel uint64
	spans    []span
	names    []string
	hasFlags bool
	bits     int
	arch     Arch
}

func (sb *sandbox) close() { sb.mm.Close() }

func (sb *sandbox) lineAt(pc uint64) int {
	if pc < sb.codeBase {
		return -1
	}
	off := int64(pc - sb.codeBase)
	for _, s := range sb.spans {
		if off >= s.addr && off < s.addr+int64(s.length) {
			return s.line
		}
	}
	return -1
}

// instrLen returns the byte length of the instruction at pc (0 if unknown).
func (sb *sandbox) instrLen(pc uint64) int {
	if pc < sb.codeBase {
		return 0
	}
	off := pc - sb.codeBase
	for _, s := range sb.spans {
		if uint64(s.addr) == off {
			return s.length
		}
	}
	return 0
}

// returnedTo reports whether the last instruction was a call to a subroutine —
// i.e. a return address pointing at retAddr now lives in the linkage location
// (RISC-V: ra; x86: top of stack).
func (sb *sandbox) returnedTo(retAddr uint64) bool {
	if sb.arch == ArchRISCV {
		return sb.r.reg(rvRegRA) == retAddr
	}
	rsp := sb.r.reg(regSP)
	width := sb.bits / 8
	var v uint64
	for i := 0; i < width; i++ {
		b, _ := sb.mm.Read8(rsp + uint64(i))
		v |= uint64(b) << (8 * i)
	}
	return v == retAddr
}

// readBytes reads n bytes of guest memory at addr (0 for unmapped).
func (sb *sandbox) readBytes(addr uint64, n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		b, err := sb.mm.Read8(addr + uint64(i))
		if err == nil {
			out[i] = b
		}
	}
	return out
}

// buildSandbox detects the ISA, assembles src, loads it into a flat sandbox,
// and positions the CPU at the entry.
func buildSandbox(src string) (*sandbox, error) {
	if DetectArch(src) == ArchRISCV {
		return buildRISCV(src)
	}
	return buildX86(src)
}

func buildX86(src string) (*sandbox, error) {
	mode := asm.DetectBits(src)
	listing, err := asm.AssembleListing(src)
	if err != nil {
		return nil, err
	}
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, ramSizeX86, 0); err != nil {
		mm.Close()
		return nil, err
	}
	for i, b := range listing.Bytes {
		_ = mm.Write8(codeBaseX86+uint64(i), b)
	}
	_ = mm.Write64(stackTopX86, sentinelRet)
	m := newX86Machine(mode, mm)
	m.setReg(regSP, stackTopX86)
	m.setPC(codeBaseX86)
	spans := make([]span, len(listing.Spans))
	for i, s := range listing.Spans {
		spans[i] = span{s.Line, s.Addr, s.Len}
	}
	return &sandbox{mm, m, codeBaseX86, sentinelRet, spans, m.names(), true, int(mode), ArchX86}, nil
}

func buildRISCV(src string) (*sandbox, error) {
	listing, err := rvasm.AssembleListing(src)
	if err != nil {
		return nil, err
	}
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(ramBaseRV, ramSizeRV, 0); err != nil {
		mm.Close()
		return nil, err
	}
	for i, b := range listing.Bytes {
		_ = mm.Write8(codeBaseRV+uint64(i), b)
	}
	c := rvcpu.NewCPU(mm, rvcpu.XLEN64)
	c.PC = codeBaseRV
	c.SetReg(rvRegRA, sentinelRet) // a final `ret` (jalr ra) lands on the sentinel
	c.SetReg(rvRegSP, stackTopRV)
	c.FS = rvcpu.FSInitial // enable the F/D floating-point unit (reset leaves it off)
	spans := make([]span, len(listing.Spans))
	for i, s := range listing.Spans {
		spans[i] = span{s.Line, s.Addr, s.Len}
	}
	m := rvMach{c}
	return &sandbox{mm, m, codeBaseRV, sentinelRet, spans, regNamesRVall, false, 64, ArchRISCV}, nil
}

// traceLoop is the shared, arch-independent stepping/attribution loop.
func traceLoop(sb *sandbox, opts Options) *Result {
	maxN := opts.MaxSteps
	if maxN <= 0 {
		maxN = defaultSteps
	}
	res := &Result{Bits: sb.bits, StopLine: -1}
	names := sb.names
	idx := map[int]int{}
	prev := snapshot(sb.r, names)

	for res.Steps < maxN {
		pc := sb.r.pc()
		if pc == sb.sentinel {
			res.Stop = StopCompleted
			break
		}
		cur := sb.lineAt(pc)
		if cur < 0 {
			res.Stop = StopOutside
			break
		}
		if (opts.StopBeforeLine >= 0 && cur == opts.StopBeforeLine) || opts.Breakpoints[cur] {
			res.Stop = StopReached
			res.StopLine = cur
			break
		}
		if ferr := safeStep(sb.r); ferr != nil {
			res.Stop = StopFault
			res.Error = ferr.Error()
			res.StopLine = cur
			break
		}
		res.Steps++
		now := snapshot(sb.r, names)
		ls := lineState(cur, prev, now, names, sb.hasFlags)
		if p, ok := idx[cur]; ok {
			res.Lines[p] = ls
		} else {
			idx[cur] = len(res.Lines)
			res.Lines = append(res.Lines, ls)
		}
		prev = now
		if sb.r.halted() { // HLT / WFI — a clean program end
			res.Stop = StopHalted
			break
		}
	}
	if res.Stop == "" {
		res.Stop = StopMaxSteps
	}
	// Final register file = the last snapshot (entry state if nothing ran).
	res.Final = make([]RegVal, len(names))
	for i := range names {
		res.Final[i] = mkReg(names[i], prev.gpr[i])
	}
	return res
}

// runner is what traceLoop needs from a CPU.
type runner interface {
	step() error
	pc() uint64
	reg(i int) uint64
	names() []string
	flags() uint64
	hasFlags() bool
	halted() bool // HLT (x86) / WFI (riscv) reached
}

type snap struct {
	gpr   []uint64
	pc    uint64
	flags uint64
}

func snapshot(r runner, names []string) snap {
	g := make([]uint64, len(names))
	for i := range names {
		g[i] = r.reg(i)
	}
	return snap{gpr: g, pc: r.pc(), flags: r.flags()}
}

func lineState(line int, prev, now snap, names []string, hasFlags bool) LineState {
	ls := LineState{Line: line, RIP: now.pc, RFLAGS: now.flags}
	for i := range names {
		if now.gpr[i] != prev.gpr[i] {
			ls.Changed = append(ls.Changed, mkReg(names[i], now.gpr[i]))
		}
		ls.Regs = append(ls.Regs, mkReg(names[i], now.gpr[i]))
	}
	if hasFlags {
		for _, f := range flagDefs {
			nb := (now.flags >> f.bit) & 1
			if pb := (prev.flags >> f.bit) & 1; nb != pb {
				ls.Flags = append(ls.Flags, mkReg(f.name, nb))
			}
		}
	}
	return ls
}

func safeStep(r runner) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic: %v", rec)
		}
	}()
	return r.step()
}

// --- stepping debugger ------------------------------------------------------

// DebugState is the machine state at a pause point of a Session.
type DebugState struct {
	Arch    string   `json:"arch"`
	Bits    int      `json:"bits"`
	Line    int      `json:"line"`            // line about to execute, -1 if the run ended
	Regs    []RegVal `json:"regs"`            // full register file now
	Changed []RegVal `json:"changed"`         // registers the last step changed
	Flags   []RegVal `json:"flags"`           // status flags now (x86; 0/1 each)
	Stop    string   `json:"stop"`            // "" while paused, else the end reason
	Steps   int      `json:"steps"`           // instructions executed so far
	Error   string   `json:"error,omitempty"` // guest fault text, if any
}

// Session is a live, single-steppable run of a program: the CPU persists
// between Step/Continue calls, so the caller can drive it like a debugger.
// Close it when done to release the backing memory.
type Session struct {
	src   string // kept for exact replay (step-back / restart)
	sb    *sandbox
	prev  snap // state before the last executed instruction (for Changed)
	steps int
	stop  string // non-empty once the run has ended
	serr  string
}

// NewSession assembles src and positions the CPU at the entry, paused.
func NewSession(src string) (*Session, error) {
	sb, err := buildSandbox(src)
	if err != nil {
		return nil, err
	}
	s := &Session{src: src, sb: sb}
	s.prev = snapshot(sb.r, sb.names)
	return s, nil
}

// Cond is a conditional breakpoint: stop at Line only when register Reg
// compares (Op ∈ ==,!=,<,>,<=,>=) against Value (unsigned).
type Cond struct {
	Line  int    `json:"line"`
	Reg   string `json:"reg"`
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}

func evalCond(v uint64, op string, val uint64) bool {
	switch op {
	case "==":
		return v == val
	case "!=":
		return v != val
	case "<":
		return v < val
	case ">":
		return v > val
	case "<=":
		return v <= val
	case ">=":
		return v >= val
	}
	return false
}

// Close releases the session's memory. Safe to call more than once.
func (s *Session) Close() {
	if s.sb != nil {
		s.sb.close()
		s.sb = nil
	}
}

func (s *Session) done() bool { return s.stop != "" }

func (s *Session) finish(reason string) { s.stop = reason }

// Step executes one instruction (a no-op once the run has ended).
func (s *Session) Step() *DebugState {
	if !s.done() {
		pc := s.sb.r.pc()
		switch {
		case pc == s.sb.sentinel:
			s.finish(StopCompleted)
		case s.sb.lineAt(pc) < 0:
			s.finish(StopOutside)
		default:
			s.prev = snapshot(s.sb.r, s.sb.names)
			if err := safeStep(s.sb.r); err != nil {
				s.serr = err.Error()
				s.finish(StopFault)
			} else {
				s.steps++
				if s.sb.r.halted() {
					s.finish(StopHalted)
				}
			}
		}
	}
	return s.State()
}

// Continue runs until the next breakpoint (a plain line breakpoint, or a
// conditional one whose register comparison holds), a clean end, a fault, or
// the step cap (after which the session ends with max-steps).
func (s *Session) Continue(breakpoints map[int]bool, conds []Cond) *DebugState {
	byLine := map[int][]Cond{}
	for _, c := range conds {
		byLine[c.Line] = append(byLine[c.Line], c)
	}
	regIdx := map[string]int{}
	for i, n := range s.sb.names {
		regIdx[n] = i
	}
	for n := 0; !s.done() && n < defaultSteps; n++ {
		s.Step()
		if s.done() {
			break
		}
		line := s.sb.lineAt(s.sb.r.pc())
		if breakpoints[line] {
			return s.State()
		}
		for _, c := range byLine[line] {
			if i, ok := regIdx[c.Reg]; ok && evalCond(s.sb.r.reg(i), c.Op, c.Value) {
				return s.State()
			}
		}
	}
	if !s.done() {
		s.finish(StopMaxSteps)
	}
	return s.State()
}

// StepOver executes one instruction, but runs a call to subroutine through to
// its return rather than stepping into it.
func (s *Session) StepOver() *DebugState {
	if s.done() {
		return s.State()
	}
	pc := s.sb.r.pc()
	retAddr := pc + uint64(s.sb.instrLen(pc))
	s.Step()
	// If that instruction called a subroutine (a return address now points at
	// the next instruction), run until we return there.
	if !s.done() && s.sb.r.pc() != retAddr && s.sb.returnedTo(retAddr) {
		for n := 0; !s.done() && n < defaultSteps && s.sb.r.pc() != retAddr; n++ {
			s.Step()
		}
	}
	return s.State()
}

// StepBack re-runs from the entry to one instruction before the current point
// — exact time-travel, since execution is deterministic.
func (s *Session) StepBack() *DebugState {
	target := s.steps - 1
	if target < 0 {
		target = 0
	}
	s.replayTo(target)
	return s.State()
}

// Restart re-arms the session at the entry.
func (s *Session) Restart() *DebugState {
	s.replayTo(0)
	return s.State()
}

// replayTo rebuilds a fresh sandbox and steps it n times.
func (s *Session) replayTo(n int) {
	sb, err := buildSandbox(s.src)
	if err != nil { // src already assembled once in NewSession, so this is unexpected
		s.serr = err.Error()
		s.finish("assemble-error")
		return
	}
	s.sb.close()
	s.sb = sb
	s.steps, s.stop, s.serr = 0, "", ""
	s.prev = snapshot(s.sb.r, s.sb.names)
	for i := 0; i < n && !s.done(); i++ {
		s.Step()
	}
}

// ReadMem reads n bytes of guest memory at addr (physical == linear in the flat
// sandbox).
func (s *Session) ReadMem(addr uint64, n int) []byte {
	return s.sb.readBytes(addr, n)
}

// State reports the current pause point.
func (s *Session) State() *DebugState {
	now := snapshot(s.sb.r, s.sb.names)
	ds := &DebugState{
		Arch: s.sb.arch.String(), Bits: s.sb.bits, Steps: s.steps,
		Stop: s.stop, Error: s.serr, Line: -1,
	}
	if !s.done() {
		ds.Line = s.sb.lineAt(now.pc)
	}
	for i, name := range s.sb.names {
		ds.Regs = append(ds.Regs, mkReg(name, now.gpr[i]))
		if now.gpr[i] != s.prev.gpr[i] {
			ds.Changed = append(ds.Changed, mkReg(name, now.gpr[i]))
		}
	}
	if s.sb.hasFlags {
		for _, f := range flagDefs {
			ds.Flags = append(ds.Flags, mkReg(f.name, (now.flags>>f.bit)&1))
		}
	}
	return ds
}

// --- x86 machines -----------------------------------------------------------

type x86machine interface {
	runner
	setPC(uint64)
	setReg(int, uint64)
}

func newX86Machine(mode asm.Mode, mm *mem.PhysMemoryMap) x86machine {
	if mode == asm.Bits32 {
		c := x86.NewCPU(mm)
		c.SetupFlatProtected32()
		return x86mach{c}
	}
	c := x86_64.NewCPU(mm)
	c.SetupFlatLongMode()
	return x64mach{c}
}

type x64mach struct{ c *x86_64.CPU }

func (m x64mach) step() error            { return m.c.Step() }
func (m x64mach) pc() uint64             { return m.c.GetRIP() }
func (m x64mach) setPC(v uint64)         { m.c.SetRIP(v) }
func (m x64mach) reg(i int) uint64       { return m.c.GetReg64(i) }
func (m x64mach) setReg(i int, v uint64) { m.c.SetReg64(i, v) }
func (m x64mach) flags() uint64          { return m.c.GetRFLAGS() }
func (m x64mach) names() []string        { return regNames64 }
func (m x64mach) hasFlags() bool         { return true }
func (m x64mach) halted() bool           { return m.c.IsPowerDown() }

type x86mach struct{ c *x86.CPU }

func (m x86mach) step() error            { return m.c.Step() }
func (m x86mach) pc() uint64             { return uint64(m.c.GetEIP()) }
func (m x86mach) setPC(v uint64)         { m.c.SetEIP(uint32(v)) }
func (m x86mach) reg(i int) uint64       { return uint64(m.c.GetReg32(i)) }
func (m x86mach) setReg(i int, v uint64) { m.c.SetReg32(i, uint32(v)) }
func (m x86mach) flags() uint64          { return uint64(m.c.GetEFLAGS()) }
func (m x86mach) names() []string        { return regNames32 }
func (m x86mach) hasFlags() bool         { return true }
func (m x86mach) halted() bool           { return m.c.IsPowerDown() }

// --- RISC-V machine ---------------------------------------------------------

type rvMach struct{ c *rvcpu.CPU }

func (m rvMach) step() error { return m.c.Step() }
func (m rvMach) pc() uint64  { return m.c.PC }
func (m rvMach) reg(i int) uint64 {
	if i >= 32 { // FP registers f0–f31 are appended after the 32 GPRs
		return m.c.FPReg[i-32]
	}
	return m.c.GetReg(i)
}
func (m rvMach) names() []string { return regNamesRVall }
func (m rvMach) flags() uint64   { return 0 }
func (m rvMach) hasFlags() bool  { return false }
func (m rvMach) halted() bool    { return m.c.IsPowerDown() }
