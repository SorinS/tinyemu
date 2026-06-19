package riscv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/test/isatest/elfloader"
)

// Spike differential harness: build a bare-metal test ELF with the GNU
// toolchain (independent of our own assembler), run it under spike with a
// per-instruction commit log (the golden trace), then lockstep cpu/riscv
// against that trace and report the first divergence in PC or register state.
//
// The ISA is pinned to exactly what cpu/riscv implements and spike's --isa is
// set to match, so a divergence is unambiguously a CPU bug — never a coverage
// gap or a spike extension we didn't ask for.

const (
	spikeMarch   = "rv64imac" // gcc -march; must equal what cpu/riscv implements
	spikeISA     = "rv64imac" // spike --isa; kept equal to spikeMarch
	spikeRAMBase = 0x80000000
	spikeRAMSize = 16 << 20
)

func spikeTools(t *testing.T) (spike, gcc string) {
	t.Helper()
	var err error
	if spike, err = exec.LookPath("spike"); err != nil {
		t.Skip("spike not found")
	}
	if gcc, err = exec.LookPath("riscv64-unknown-elf-gcc"); err != nil {
		t.Skip("riscv64-unknown-elf-gcc not found")
	}
	return spike, gcc
}

// buildTestELF wraps a RISC-V assembly body in _start + a tohost exit, builds a
// bare-metal ELF at 0x8000_0000, and returns the path plus the loaded info.
func buildTestELF(t *testing.T, gcc, body string) (string, *elfloader.Info) {
	t.Helper()
	dir := t.TempDir()
	asm := ".section .text\n.globl _start\n_start:\n" + body + "\n" +
		// exit: store 1 to tohost (spike's HTIF halts), then spin.
		"  la t0, tohost\n  li t1, 1\n  sd t1, 0(t0)\n1: j 1b\n" +
		".section .tohost,\"aw\",@progbits\n.align 6\n.globl tohost\ntohost: .dword 0\n" +
		".align 6\n.globl fromhost\nfromhost: .dword 0\n"
	ld := `OUTPUT_ARCH("riscv")
ENTRY(_start)
SECTIONS {
  . = 0x80000000;
  .text : { *(.text) }
  . = ALIGN(0x1000);
  .tohost : { *(.tohost) }
}`
	asmPath := filepath.Join(dir, "t.S")
	ldPath := filepath.Join(dir, "t.ld")
	elfPath := filepath.Join(dir, "t.elf")
	if err := os.WriteFile(asmPath, []byte(asm), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ldPath, []byte(ld), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(gcc, "-march="+spikeMarch, "-mabi=lp64",
		"-nostdlib", "-nostartfiles", "-T", ldPath, asmPath, "-o", elfPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gcc: %v\n%s", err, out)
	}
	info, err := elfloader.Load(elfPath)
	if err != nil {
		t.Fatalf("elfloader: %v", err)
	}
	if info.ToHostAddr == nil {
		t.Fatal("ELF missing tohost symbol")
	}
	return elfPath, info
}

// goldenStep is one retired instruction from spike: the PC it executed at, the
// full GPR file after it retired, and spike's disassembly (for messages).
type goldenStep struct {
	pc     uint64
	regs   [32]uint64
	disasm string
}

var (
	// commit line: "core   0: 3 0x80000000 (0x4515) x10 0x...5  [mem 0x...]"
	commitRe = regexp.MustCompile(`^core\s+\d+:\s+\d+\s+0x([0-9a-fA-F]+)\s+\(0x[0-9a-fA-F]+\)(.*)$`)
	// disasm line: "core   0: 0x80000000 (0x4515) c.li a0, 5"
	disasmRe = regexp.MustCompile(`^core\s+\d+:\s+0x([0-9a-fA-F]+)\s+\(0x[0-9a-fA-F]+\)\s+(.+)$`)
	regWrRe  = regexp.MustCompile(`\bx(\d+)\s+0x([0-9a-fA-F]+)`)
	memRe    = regexp.MustCompile(`\bmem\s+0x([0-9a-fA-F]+)`)
)

// runSpikeTrace runs spike and returns the golden trace for the program body —
// every instruction from the ELF entry up to (not including) the tohost store —
// plus the register file at entry (after spike's reset vector, before the first
// body instruction). cpu/riscv must be seeded with that snapshot so the reset
// vector's a1=dtb / t0 / a0=hartid don't read as divergences.
func runSpikeTrace(t *testing.T, spike, elfPath string, entry, tohost uint64) ([]goldenStep, [32]uint64) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "spike.log")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, spike, "--isa="+spikeISA, "-l", "--log-commits",
		"--log="+logPath, elfPath)
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		// spike exits 0 on a tohost-pass; a non-zero exit with no timeout is fine
		// only if it still produced a log. Fall through and parse.
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read spike log: %v", err)
	}

	disasm := map[uint64]string{}
	var regs, entrySnap [32]uint64
	var golden []goldenStep
	entered := false

	for _, line := range strings.Split(string(data), "\n") {
		if m := commitRe.FindStringSubmatch(line); m != nil {
			pc, _ := strconv.ParseUint(m[1], 16, 64)
			rest := m[2]
			if mm := memRe.FindStringSubmatch(rest); mm != nil {
				addr, _ := strconv.ParseUint(mm[1], 16, 64)
				if addr == tohost {
					break // the exit store — stop the golden trace here
				}
			}
			if !entered && pc == entry {
				entered = true
				entrySnap = regs // state after the reset vector, before the body
			}
			if rw := regWrRe.FindStringSubmatch(rest); rw != nil {
				r, _ := strconv.Atoi(rw[1])
				v, _ := strconv.ParseUint(rw[2], 16, 64)
				if r != 0 {
					regs[r] = v
				}
			}
			if entered {
				golden = append(golden, goldenStep{pc: pc, regs: regs, disasm: disasm[pc]})
			}
			continue
		}
		if m := disasmRe.FindStringSubmatch(line); m != nil {
			pc, _ := strconv.ParseUint(m[1], 16, 64)
			disasm[pc] = strings.TrimSpace(m[2])
		}
	}
	// Backfill disasm captured after the matching commit (loop bodies).
	for i := range golden {
		if golden[i].disasm == "" {
			golden[i].disasm = disasm[golden[i].pc]
		}
	}
	return golden, entrySnap
}

// runDifferential builds the ELF, gets spike's golden trace, runs cpu/riscv
// against it, and fails on the first PC/register divergence or Step error.
func runDifferential(t *testing.T, spike, gcc, body string) {
	t.Helper()
	elfPath, info := buildTestELF(t, gcc, body)
	golden, entrySnap := runSpikeTrace(t, spike, elfPath, info.EntryPoint, *info.ToHostAddr)
	if len(golden) < 2 {
		t.Fatalf("golden trace too short (%d); harness/parse problem", len(golden))
	}

	mm := mem.NewPhysMemoryMap()
	defer mm.Close()
	ram, err := mm.RegisterRAM(spikeRAMBase, spikeRAMSize, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, seg := range info.Segments {
		if seg.VAddr < spikeRAMBase || seg.VAddr+uint64(len(seg.Data)) > spikeRAMBase+spikeRAMSize {
			t.Fatalf("segment 0x%x outside RAM", seg.VAddr)
		}
		copy(ram.PhysMem[seg.VAddr-spikeRAMBase:], seg.Data)
	}

	cpu := NewCPU(mm, XLEN64)
	cpu.PC = info.EntryPoint
	for r := 1; r < 32; r++ { // seed spike's entry state (reset vector left a1/t0/…)
		cpu.SetReg(r, entrySnap[r])
	}

	for i, g := range golden {
		if cpu.PC != g.pc {
			t.Fatalf("step %d: PC diverged — ours 0x%x, spike 0x%x (%s)", i, cpu.PC, g.pc, g.disasm)
		}
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d at 0x%x (%s): cpu.Step error: %v", i, g.pc, g.disasm, err)
		}
		for r := 1; r < 32; r++ {
			if got := cpu.GetReg(r); got != g.regs[r] {
				t.Fatalf("divergence at 0x%x (%s): x%d ours=0x%x spike=0x%x",
					g.pc, g.disasm, r, got, g.regs[r])
			}
		}
	}
	t.Logf("%d instructions matched spike", len(golden))
}

func TestSpikeDiff_Smoke(t *testing.T) {
	spike, gcc := spikeTools(t)
	// One trivial arithmetic program — proves the harness end-to-end before the
	// matrix, so a real divergence later is a CPU bug, not a harness bug.
	runDifferential(t, spike, gcc, ""+
		"  li a0, 5\n"+
		"  li a1, 7\n"+
		"  add a0, a0, a1\n"+
		"  sub a2, a1, a0\n")
}

func TestSpikeDiff_Matrix(t *testing.T) {
	spike, gcc := spikeTools(t)
	cases := []struct{ name, body string }{
		{"alu_reg", "li a0, 0x1234\nli a1, 0x5678\nadd a2,a0,a1\nsub a3,a1,a0\nand a4,a0,a1\nor a5,a0,a1\nxor a6,a0,a1\nslt a7,a0,a1\nsltu s2,a1,a0\n"},
		{"alu_imm", "li a0, 100\naddi a1,a0,-50\nandi a2,a0,0xf\nori a3,a0,1\nxori a4,a0,-1\nslti a5,a0,200\n"},
		{"shifts", "li a0, 0x1\nslli a1,a0,40\nli a2,-256\nsrli a3,a2,4\nsrai a4,a2,4\nsll a5,a0,a1\n"},
		{"word_ops", "li a0,0x1\nslli a0,a0,31\naddw a1,a0,a0\nli a2,-1\nsubw a3,a2,a0\nsraw a4,a0,a0\n"},
		{"mul_div", "li a0,7\nli a1,-3\nmul a2,a0,a1\nmulh a3,a0,a1\ndiv a4,a0,a1\nrem a5,a0,a1\ndivu a6,a0,a1\n"},
		{"div_by_zero", "li a0,42\nli a1,0\ndiv a2,a0,a1\nrem a3,a0,a1\ndivu a4,a0,a1\n"},
		{"branches", "li a0,0\nli a1,5\nloop:\naddi a0,a0,1\nblt a0,a1,loop\nbeq a0,a1,eq\nli a2,99\neq:\nli a3,7\n"},
		{"loads_stores", "li a0,0x80002000\nli a1,-1234\nsd a1,0(a0)\nld a2,0(a0)\nsw a1,8(a0)\nlw a3,8(a0)\nlwu a4,8(a0)\nsb a1,16(a0)\nlb a5,16(a0)\nlbu a6,16(a0)\n"},
		{"lui_auipc", "lui a0,0x12345\nauipc a1,0x10\naddi a2,a0,0x67\n"},
		{"jal_jalr", "jal ra,target\nli a0,1\ntarget:\nli a1,2\n"},
		{"sign_extend", "li a0,0xffffffff\naddiw a1,a0,1\nli a2,0x7fffffff\naddiw a3,a2,1\n"},
		{"atomics", "li a0,0x80002000\nli a1,5\nsd zero,0(a0)\namoadd.w a2,a1,(a0)\namoswap.w a3,a1,(a0)\nlr.w a4,(a0)\nsc.w a5,a1,(a0)\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runDifferential(t, spike, gcc, c.body)
		})
	}
}
