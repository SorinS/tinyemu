//go:build darwin && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	asmarm64 "github.com/jtolio/tinyemu-go/asm/arm64"
	"github.com/jtolio/tinyemu-go/mem"
)

// The native oracle: assemble a straight-line block, splice its bytes into a
// trampoline that loads X0–X30/SP/NZCV from an input struct, runs the block on
// the actual Apple Silicon CPU, and writes the resulting registers back out.
// Comparing this to cpu/arm64's Step() validates execution semantics against
// hardware — and the assembler's bytes a second way (real execution, not just
// llvm byte-diff). X16/X17 are used as scratch in the dump and are not
// validated as outputs.

// oracleRegs mirrors the C struct `regs_t` byte-for-byte (all uint64, no pad).
type oracleRegs struct {
	X    [31]uint64
	SP   uint64
	NZCV uint64
}

const harnessC = `#include <stdint.h>
#include <stdio.h>
typedef struct { uint64_t x[31]; uint64_t sp; uint64_t nzcv; } regs_t;
void run_test(const regs_t* in, regs_t* out);
int main(void){
    static regs_t in, out;
    if (fread(&in, sizeof(in), 1, stdin) != 1) return 2;
    run_test(&in, &out);
    if (fwrite(&out, sizeof(out), 1, stdout) != 1) return 3;
    return 0;
}
`

// trampolineTmpl is the AArch64 trampoline; %s is replaced by the payload words.
const trampolineTmpl = `	.section __DATA,__data
	.p2align 3
g_ctx:
	.space 256

	.section __TEXT,__text
	.globl _run_test
	.p2align 2
_run_test:
	adrp x16, g_ctx@PAGE
	add  x16, x16, g_ctx@PAGEOFF
	mov  x17, sp
	str  x17, [x16, #0]
	str  x30, [x16, #8]
	str  x1,  [x16, #16]
	stp  x19, x20, [x16, #24]
	stp  x21, x22, [x16, #40]
	stp  x23, x24, [x16, #56]
	stp  x25, x26, [x16, #72]
	stp  x27, x28, [x16, #88]
	str  x29, [x16, #104]
	ldr  x17, [x0, #248]
	mov  sp, x17
	ldr  x17, [x0, #256]
	msr  nzcv, x17
	ldp  x2,  x3,  [x0, #16]
	ldp  x4,  x5,  [x0, #32]
	ldp  x6,  x7,  [x0, #48]
	ldp  x8,  x9,  [x0, #64]
	ldp  x10, x11, [x0, #80]
	ldp  x12, x13, [x0, #96]
	ldp  x14, x15, [x0, #112]
	ldp  x18, x19, [x0, #144]
	ldp  x20, x21, [x0, #160]
	ldp  x22, x23, [x0, #176]
	ldp  x24, x25, [x0, #192]
	ldp  x26, x27, [x0, #208]
	ldp  x28, x29, [x0, #224]
	ldr  x30, [x0, #240]
	ldp  x16, x17, [x0, #128]
	ldr  x1,  [x0, #8]
	ldr  x0,  [x0, #0]
%s
	adrp x16, g_ctx@PAGE
	add  x16, x16, g_ctx@PAGEOFF
	ldr  x16, [x16, #16]
	str  x0,  [x16, #0]
	str  x1,  [x16, #8]
	stp  x2,  x3,  [x16, #16]
	stp  x4,  x5,  [x16, #32]
	stp  x6,  x7,  [x16, #48]
	stp  x8,  x9,  [x16, #64]
	stp  x10, x11, [x16, #80]
	stp  x12, x13, [x16, #96]
	stp  x14, x15, [x16, #112]
	str  xzr, [x16, #128]
	str  xzr, [x16, #136]
	stp  x18, x19, [x16, #144]
	stp  x20, x21, [x16, #160]
	stp  x22, x23, [x16, #176]
	stp  x24, x25, [x16, #192]
	stp  x26, x27, [x16, #208]
	stp  x28, x29, [x16, #224]
	str  x30, [x16, #240]
	mov  x17, sp
	str  x17, [x16, #248]
	mrs  x17, nzcv
	str  x17, [x16, #256]
	adrp x16, g_ctx@PAGE
	add  x16, x16, g_ctx@PAGEOFF
	ldr  x17, [x16, #0]
	mov  sp, x17
	ldr  x30, [x16, #8]
	ldp  x19, x20, [x16, #24]
	ldp  x21, x22, [x16, #40]
	ldp  x23, x24, [x16, #56]
	ldp  x25, x26, [x16, #72]
	ldp  x27, x28, [x16, #88]
	ldr  x29, [x16, #104]
	ret
`

func assembleWords(t *testing.T, instrs []string) ([]byte, []uint32) {
	t.Helper()
	var code []byte
	var words []uint32
	for _, s := range instrs {
		b, err := asmarm64.Assemble(s)
		if err != nil {
			t.Fatalf("assemble %q: %v", s, err)
		}
		code = append(code, b...)
		words = append(words, binary.LittleEndian.Uint32(b))
	}
	return code, words
}

// oracleBin compiles the trampoline + harness for a payload and returns the
// executable path. Skips the test if a native compiler is unavailable.
func oracleBin(t *testing.T, words []uint32) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no cc on PATH")
	}
	var pay strings.Builder
	for _, wd := range words {
		fmt.Fprintf(&pay, "\t.long 0x%08x\n", wd)
	}
	dir := t.TempDir()
	must := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cPath := must("harness.c", harnessC)
	sPath := must("tramp.s", fmt.Sprintf(trampolineTmpl, pay.String()))
	bin := filepath.Join(dir, "oracle")
	out, err := exec.Command(cc, "-O0", "-o", bin, cPath, sPath).CombinedOutput()
	if err != nil {
		t.Fatalf("cc failed: %v\n%s", err, out)
	}
	return bin
}

// runBin runs a prebuilt oracle executable with the given input registers.
func runBin(t *testing.T, bin string, in oracleRegs) oracleRegs {
	t.Helper()
	var stdin bytes.Buffer
	if err := binary.Write(&stdin, binary.LittleEndian, &in); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = &stdin
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("native run failed: %v", err)
	}
	var out oracleRegs
	if err := binary.Read(&stdout, binary.LittleEndian, &out); err != nil {
		t.Fatalf("decode native output: %v", err)
	}
	return out
}

func cpuRun(t *testing.T, instrs []string, in oracleRegs) oracleRegs {
	t.Helper()
	code, _ := assembleWords(t, instrs)
	mm := mem.NewPhysMemoryMap()
	defer mm.Close()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	const base = 0x10000
	for i, b := range code {
		_ = mm.Write8(base+uint64(i), b)
	}
	c := New(mm)
	c.X = in.X
	c.SP = in.SP
	c.PC = base
	c.N = in.NZCV>>31&1 == 1
	c.Z = in.NZCV>>30&1 == 1
	c.C = in.NZCV>>29&1 == 1
	c.V = in.NZCV>>28&1 == 1
	end := uint64(base) + uint64(len(code))
	for step := 0; c.PC < end && step < 1000; step++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step at %#x: %v", c.PC, err)
		}
	}
	out := oracleRegs{X: c.X, SP: c.SP, NZCV: c.nzcv()}
	return out
}

// diffRegs reports register/flag mismatches, skipping the scratch X16/X17.
func diffRegs(t *testing.T, label string, native, got oracleRegs) {
	t.Helper()
	bad := false
	for i := 0; i < 31; i++ {
		if i == 16 || i == 17 {
			continue
		}
		if native.X[i] != got.X[i] {
			t.Errorf("%s: x%d native=%#x cpu=%#x", label, i, native.X[i], got.X[i])
			bad = true
		}
	}
	if native.SP != got.SP {
		t.Errorf("%s: sp native=%#x cpu=%#x", label, native.SP, got.SP)
		bad = true
	}
	if native.NZCV != got.NZCV {
		t.Errorf("%s: nzcv native=%032b cpu=%032b", label, native.NZCV>>28, got.NZCV>>28)
		bad = true
	}
	if bad {
		t.Logf("%s instrs failed", label)
	}
}

// baseInputs returns a couple of deterministic register fill patterns.
func baseInputs() []oracleRegs {
	var a, b oracleRegs
	for i := 0; i < 31; i++ {
		a.X[i] = uint64(i+1) * 0x0101010101010101
		b.X[i] = 0xDEADBEEF00000000 ^ (uint64(i) << 8) ^ uint64(i*2654435761)
	}
	a.SP, b.SP = 0x80000, 0x90000
	return []oracleRegs{a, b}
}

func TestARM64_NativeOracle(t *testing.T) {
	programs := [][]string{
		// arithmetic — exercises carry/overflow flags
		{"adds x0, x1, x2", "cset x3, cs", "cset x4, vs"},
		{"subs x0, x1, x2", "cset x3, cc", "cset x4, vs", "cset x5, mi"},
		{"add w0, w1, w2", "sub w3, w4, w5"}, // W ops must zero bits 63:32
		{"adds x0, x1, x2"}, {"subs x0, x1, x2"}, {"subs w0, w1, w2"},
		{"add x0, x1, x2, lsl #4"}, {"sub x0, x1, x2, asr #3"},
		{"cmp x1, x2", "cset x0, eq", "cset x3, lt", "cset x4, hi"},
		{"cmp w1, w2", "cset w0, ge", "cset w3, lo"},
		// logical
		{"and x0, x1, x2", "orr x3, x4, x5", "eor x6, x7, x8"},
		{"ands x0, x1, x2", "bic x3, x4, x5"},
		{"and x0, x1, #0xff", "orr x3, x4, #0xf0f0f0f0f0f0f0f0"},
		{"tst x1, x2", "cset x0, ne"},
		// move wide
		{"movz x0, #0x1234", "movk x0, #0xabcd, lsl #16", "movn x1, #0"},
		{"movz w0, #0xffff, lsl #16"},
		// bitfield / shifts / extend
		{"lsl x0, x1, #4", "lsr x3, x4, #8", "asr x5, x6, #2"},
		{"lsl w0, w1, #4", "asr w3, w4, #3"},
		{"ubfx x0, x1, #4, #8", "sbfx x3, x4, #4, #8", "bfi x5, x6, #8, #4"},
		{"uxtb x0, w1", "sxtb x3, w4", "sxth x5, w6", "sxtw x7, w8"},
		{"ror x0, x1, #13", "extr x3, x4, x5, #20"},
		// multiply / divide
		{"mul x0, x1, x2", "madd x3, x4, x5, x6", "msub x7, x8, x9, x10"},
		{"smull x0, w1, w2", "umull x3, w4, w5"},
		{"smulh x0, x1, x2", "umulh x3, x4, x5"},
		{"udiv x0, x1, x2", "sdiv x3, x4, x5"},
		{"lslv x0, x1, x2", "asrv x3, x4, x5", "rorv x6, x7, x8"},
		// 1-source
		{"clz x0, x1", "rbit x3, x4", "rev x5, x6", "rev16 x7, x8", "cls x9, x10"},
		// conditional select
		{"subs x9, x1, x2", "csel x0, x3, x4, ge", "csinc x5, x6, x7, lt", "csneg x8, x10, x11, eq"},
		// add/subtract with carry — read the C flag set by a prior adds/subs
		{"adds x9, x1, x2", "adc x0, x3, x4", "sbc x5, x6, x7"},
		{"subs x9, x1, x2", "sbcs x0, x3, x4", "cset x5, cc", "cset x6, vs"},
		{"adds w9, w1, w2", "adc w0, w3, w4"},
		// conditional compare — chained, then materialize the flags
		{"cmp x1, x2", "ccmp x3, x4, #0, eq", "cset x0, lt", "cset x5, ge"},
		{"cmp x1, x2", "ccmn x3, x4, #15, ne", "cset x0, mi", "cset x6, eq"},
		{"cmp w1, w2", "ccmp w3, #5, #0, hi", "cset w0, eq", "cset w5, ne"},
		{"subs x9, x1, x2", "ccmp x3, #0, #4, ge", "cset x0, eq"},
		// a small straight-line sequence mixing classes
		{"add x0, x1, x2", "mul x0, x0, x3", "eor x0, x0, x4", "lsr x0, x0, #7", "sub x0, x0, #1"},
	}
	inputs := baseInputs()
	for _, prog := range programs {
		label := strings.Join(prog, "; ")
		_, words := assembleWords(t, prog)
		bin := oracleBin(t, words) // compile once per program
		for _, in := range inputs {
			native := runBin(t, bin, in)
			got := cpuRun(t, prog, in)
			diffRegs(t, label, native, got)
		}
	}
}
