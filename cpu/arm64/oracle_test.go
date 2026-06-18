//go:build darwin && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
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
// The V field holds V0–V31 as 32×{low64,high64}. The integer fields keep their
// original offsets so the GPR trampoline code is unchanged; V/FPCR/FPSR are
// appended.
type oracleRegs struct {
	X    [31]uint64    // off 0
	SP   uint64        // off 248
	NZCV uint64        // off 256
	V    [32][2]uint64 // off 264 (each reg low64 then high64)
	FPCR uint64        // off 776
	FPSR uint64        // off 784
}

const harnessC = `#include <stdint.h>
#include <stdio.h>
typedef struct {
    uint64_t x[31]; uint64_t sp; uint64_t nzcv;
    uint64_t v[64]; uint64_t fpcr; uint64_t fpsr;
} regs_t;
void run_test(const regs_t* in, regs_t* out);
int main(void){
    static regs_t in, out;
    if (fread(&in, sizeof(in), 1, stdin) != 1) return 2;
    run_test(&in, &out);
    if (fwrite(&out, sizeof(out), 1, stdout) != 1) return 3;
    return 0;
}
`

// vBlock emits `op qN, [base, #(start+16*N)]` for N=0..31 — the input-load and
// output-store sweeps over the V register file.
func vBlock(op, base string, start int) string {
	var b strings.Builder
	for n := 0; n < 32; n++ {
		fmt.Fprintf(&b, "\t%s q%d, [%s, #%d]\n", op, n, base, start+16*n)
	}
	return b.String()
}

// buildTrampoline assembles the full AArch64 trampoline around the payload. It
// preserves the C ABI callee-saved registers (x19–x29 plus the low 64 bits of
// v8–v15) in g_ctx, loads the input GPRs/SP/NZCV/FPCR/V from the in struct, runs
// the payload, then writes back GPRs/NZCV/FPCR/FPSR/V to the out struct.
func buildTrampoline(payload string) string {
	const vOff = 264 // offset of regs_t.v
	return `	.section __DATA,__data
	.p2align 4
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
	str  q8,  [x16, #112]
	str  q9,  [x16, #128]
	str  q10, [x16, #144]
	str  q11, [x16, #160]
	str  q12, [x16, #176]
	str  q13, [x16, #192]
	str  q14, [x16, #208]
	str  q15, [x16, #224]
	ldr  x17, [x0, #248]
	mov  sp, x17
	ldr  x17, [x0, #256]
	msr  nzcv, x17
	ldr  x17, [x0, #776]
	msr  fpcr, x17
	add  x16, x0, #` + fmt.Sprint(vOff) + `
` + vBlock("ldr", "x16", 0) + `	ldp  x2,  x3,  [x0, #16]
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
` + payload + `
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
	mrs  x17, fpcr
	str  x17, [x16, #776]
	mrs  x17, fpsr
	str  x17, [x16, #784]
	add  x16, x16, #` + fmt.Sprint(vOff) + `
` + vBlock("str", "x16", 0) + `	adrp x16, g_ctx@PAGE
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
	ldr  q8,  [x16, #112]
	ldr  q9,  [x16, #128]
	ldr  q10, [x16, #144]
	ldr  q11, [x16, #160]
	ldr  q12, [x16, #176]
	ldr  q13, [x16, #192]
	ldr  q14, [x16, #208]
	ldr  q15, [x16, #224]
	ret
`
}

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
	sPath := must("tramp.s", buildTrampoline(pay.String()))
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
	c.Vreg = in.V
	c.FPCR = in.FPCR
	end := uint64(base) + uint64(len(code))
	for step := 0; c.PC < end && step < 1000; step++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step at %#x: %v", c.PC, err)
		}
	}
	out := oracleRegs{X: c.X, SP: c.SP, NZCV: c.nzcv(), V: c.Vreg, FPCR: c.FPCR, FPSR: c.FPSR}
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
	for i := 0; i < 32; i++ {
		if native.V[i] != got.V[i] {
			t.Errorf("%s: v%d native=%016x_%016x cpu=%016x_%016x", label, i,
				native.V[i][1], native.V[i][0], got.V[i][1], got.V[i][0])
			bad = true
		}
	}
	// FPSR exception flags aren't modelled, so they're intentionally not compared.
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
		// system: hints/barriers are no-ops; mrs/msr round-trip NZCV and the
		// EL0-accessible tpidr_el0 (read-only/EL1 regs would trap natively).
		{"nop", "add x0, x1, x2", "dmb sy", "isb", "yield"},
		{"msr nzcv, x1", "mrs x0, nzcv"},        // x0 = x1 & 0xF0000000
		{"msr tpidr_el0, x1", "mrs x0, tpidr_el0"}, // x0 = x1
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

// fpSeedValues are clean, exactly-representable values used to fill V0–V31 for
// the FP oracle. Kept free of NaN so single-precision reads of a double seed (or
// vice versa) never produce divergent NaN payloads; division/sqrt round
// identically on hardware and in Go (both IEEE-754 round-to-nearest).
var fpSeedValues = []float64{
	3.5, 1.25, 2.0, 8.0, -4.0, 0.5, 100.0, -0.25,
	16.0, 7.0, -2.5, 64.0, 0.125, -1.0, 9.0, 256.0,
	-0.5, 5.0, 1.0, -16.0, 32.0, 0.75, -8.0, 4.0,
	1.5, -32.0, 10.0, 0.0625, -100.0, 6.0, 0.25, -3.0,
}

// fpInputs returns register fills for the FP oracle. With dbl set, each V[i]
// holds float64(value); otherwise the low 32 bits hold float32(value). The GPRs
// carry small integers so scvtf/ucvtf have meaningful sources.
func fpInputs(dbl bool) []oracleRegs {
	var r oracleRegs
	for i := 0; i < 32; i++ {
		if dbl {
			r.V[i][0] = math.Float64bits(fpSeedValues[i])
		} else {
			r.V[i][0] = uint64(math.Float32bits(float32(fpSeedValues[i])))
		}
	}
	for i := 0; i < 31; i++ {
		r.X[i] = uint64(int64(i*7 - 80)) // mix of negative and positive
	}
	r.SP = 0x80000
	return []oracleRegs{r}
}

// fpSpecialInputs seeds V0–V7 with infinities, NaN and out-of-range magnitudes
// to drive the saturation and unordered-compare paths. With dbl set the slot
// holds a float64 special; otherwise its low 32 bits hold a float32 special.
// Used only by programs that don't write a V register, so the seed bits pass
// through unchanged on both sides.
func fpSpecialInputs(dbl bool) []oracleRegs {
	dv := []float64{
		math.Inf(1), math.Inf(-1), math.NaN(), 1e300, -1e300, 3.5, 2.0, 0.0,
	}
	var r oracleRegs
	for i := 0; i < 8; i++ {
		if dbl {
			r.V[i][0] = math.Float64bits(dv[i])
		} else {
			r.V[i][0] = uint64(math.Float32bits(float32(dv[i])))
		}
	}
	for i := 0; i < 31; i++ {
		r.X[i] = uint64(int64(i))
	}
	r.SP = 0x80000
	return []oracleRegs{r}
}

// vecInputs fills V0–V31 with varied 128-bit patterns for the integer-vector
// oracle (any bits work; both halves are seeded so .16b/.2d see real data).
func vecInputs() []oracleRegs {
	var r oracleRegs
	for i := 0; i < 32; i++ {
		lo := uint64(i+1) * 0x0102030405060708
		hi := 0xF0E0D0C0B0A09080 ^ (uint64(i) * 0x1111111111111111)
		r.V[i] = [2]uint64{lo, hi}
	}
	for i := 0; i < 31; i++ {
		r.X[i] = 0xCAFE0000_00000000 ^ (uint64(i+1) * 0x1234567)
	}
	r.SP = 0x80000
	return []oracleRegs{r}
}

func TestARM64_NativeOracleSIMD(t *testing.T) {
	programs := [][]string{
		// add/sub across arrangements
		{"add v0.16b, v1.16b, v2.16b"}, {"add v0.8b, v1.8b, v2.8b"},
		{"add v0.8h, v1.8h, v2.8h"}, {"add v0.4s, v1.4s, v2.4s"},
		{"add v0.2d, v1.2d, v2.2d"}, {"add v0.2s, v1.2s, v2.2s"},
		{"sub v0.16b, v3.16b, v4.16b"}, {"sub v0.4s, v3.4s, v4.4s"},
		{"sub v0.2d, v3.2d, v4.2d"},
		// mul (not on .2d)
		{"mul v0.16b, v1.16b, v2.16b"}, {"mul v0.8h, v1.8h, v2.8h"},
		{"mul v0.4s, v1.4s, v2.4s"}, {"mul v0.2s, v1.2s, v2.2s"},
		// bitwise logicals
		{"and v0.16b, v1.16b, v2.16b"}, {"orr v0.16b, v1.16b, v2.16b"},
		{"eor v0.16b, v1.16b, v2.16b"}, {"bic v0.16b, v1.16b, v2.16b"},
		{"orn v0.16b, v1.16b, v2.16b"}, {"and v0.8b, v1.8b, v2.8b"},
		// copy group: dup (general/element), umov, smov, ins
		{"dup v0.4s, w1"}, {"dup v0.16b, w2"}, {"dup v0.2d, x3"}, {"dup v0.8h, w4"},
		{"dup v0.4s, v1.s[2]"}, {"dup v0.2d, v1.d[1]"}, {"dup v0.16b, v1.b[5]"},
		{"umov w0, v1.s[2]", "umov x3, v2.d[1]", "umov w4, v3.b[7]", "umov w5, v4.h[2]"},
		{"smov x0, v1.b[3]", "smov x3, v2.h[1]", "smov x4, v3.s[1]", "smov w5, v4.b[6]"},
		{"ins v0.s[1], w2", "ins v3.d[0], x4", "ins v5.b[9], w6"},
		{"ins v0.s[1], v2.s[3]", "ins v3.d[1], v4.d[0]", "ins v5.b[2], v6.b[11]"},
		// integer compares (per-lane all-ones/all-zeros)
		{"cmgt v0.4s, v1.4s, v2.4s"}, {"cmge v0.16b, v3.16b, v4.16b"},
		{"cmhi v0.8h, v1.8h, v2.8h"}, {"cmhs v0.2d, v3.2d, v4.2d"},
		{"cmeq v0.4s, v1.4s, v2.4s"}, {"cmtst v0.16b, v1.16b, v2.16b"},
		{"cmgt v0.2d, v1.2d, v2.2d"}, {"cmeq v0.8b, v3.8b, v4.8b"},
		// two-register misc (abs/neg/not/cnt)
		{"abs v0.16b, v1.16b"}, {"abs v0.4s, v3.4s"}, {"abs v0.2d, v1.2d"}, {"abs v0.8h, v3.8h"},
		{"neg v0.16b, v1.16b"}, {"neg v0.4s, v3.4s"}, {"neg v0.2d, v1.2d"},
		{"not v0.16b, v1.16b"}, {"cnt v0.16b, v1.16b"}, {"cnt v0.8b, v3.8b"},
		// across-lanes reductions (addv/smaxv/umaxv/sminv/uminv)
		{"addv b0, v1.16b"}, {"addv b0, v1.8b"}, {"addv h0, v1.8h"}, {"addv s0, v1.4s"},
		{"smaxv b0, v1.16b"}, {"sminv b0, v1.16b"}, {"umaxv s0, v1.4s"}, {"uminv h0, v1.8h"},
		{"smaxv h0, v3.8h"}, {"uminv b0, v4.16b"},
		// shift by immediate (shl/sshr/ushr/ssra/usra)
		{"shl v0.4s, v1.4s, #3"}, {"shl v0.16b, v3.16b, #5"}, {"shl v0.2d, v1.2d, #40"},
		{"sshr v0.4s, v1.4s, #3"}, {"ushr v0.4s, v1.4s, #3"}, {"sshr v0.16b, v3.16b, #2"},
		{"ushr v0.2d, v1.2d, #40"}, {"sshr v0.8h, v3.8h, #16"}, {"sshr v0.2d, v1.2d, #64"},
		{"ssra v6.4s, v1.4s, #3"}, {"usra v6.4s, v1.4s, #3"}, // accumulate into v6
		// chained
		{"add v0.4s, v1.4s, v2.4s", "mul v0.4s, v0.4s, v3.4s", "eor v0.16b, v0.16b, v4.16b"},
		{"dup v0.4s, w1", "ins v0.s[0], w2", "umov w3, v0.s[3]"},
		{"cmgt v0.4s, v1.4s, v2.4s", "and v3.16b, v0.16b, v5.16b"}, // mask then select bits
		{"shl v0.4s, v1.4s, #4", "sshr v0.4s, v0.4s, #2"},         // shift left then arithmetic right
	}
	runVec := func(progs [][]string, inputs []oracleRegs) {
		for _, prog := range progs {
			label := strings.Join(prog, "; ")
			_, words := assembleWords(t, prog)
			bin := oracleBin(t, words)
			for _, in := range inputs {
				native := runBin(t, bin, in)
				got := cpuRun(t, prog, in)
				diffRegs(t, label, native, got)
			}
		}
	}
	runVec(programs, vecInputs())

	// Float three-same, seeded with clean float values (split by precision so a
	// .4s read of a double seed never produces a divergent NaN, as with the
	// scalar oracle). Single = two float32 lanes per 64-bit word; double = one.
	fpSingle := [][]string{
		{"fadd v0.4s, v1.4s, v2.4s"}, {"fadd v0.2s, v1.2s, v2.2s"},
		{"fsub v0.4s, v3.4s, v4.4s"}, {"fmul v0.4s, v1.4s, v5.4s"},
		{"fdiv v0.4s, v6.4s, v1.4s"}, {"fmax v0.4s, v4.4s, v5.4s"},
		{"fmin v0.4s, v4.4s, v5.4s"}, {"fmaxnm v0.4s, v1.4s, v4.4s"},
		{"fminnm v0.4s, v1.4s, v4.4s"},
		{"fcmeq v0.4s, v1.4s, v2.4s"}, {"fcmge v0.4s, v1.4s, v4.4s"}, {"fcmgt v0.4s, v4.4s, v1.4s"},
		{"fadd v0.4s, v1.4s, v2.4s", "fmul v0.4s, v0.4s, v3.4s", "fsub v0.4s, v0.4s, v4.4s"},
	}
	fpDouble := [][]string{
		{"fadd v0.2d, v1.2d, v2.2d"}, {"fsub v0.2d, v3.2d, v4.2d"},
		{"fmul v0.2d, v1.2d, v5.2d"}, {"fdiv v0.2d, v6.2d, v1.2d"},
		{"fmax v0.2d, v4.2d, v5.2d"}, {"fmin v0.2d, v4.2d, v5.2d"},
		{"fmaxnm v0.2d, v1.2d, v4.2d"}, {"fminnm v0.2d, v1.2d, v4.2d"},
		{"fcmeq v0.2d, v1.2d, v2.2d"}, {"fcmge v0.2d, v1.2d, v4.2d"}, {"fcmgt v0.2d, v4.2d, v1.2d"},
	}
	runVec(fpSingle, vecFPInputs(false))
	runVec(fpDouble, vecFPInputs(true))
}

// vecFPInputs fills V0–V31 with clean float values for the float-vector oracle.
// With dbl set, each 64-bit word holds one float64; otherwise it holds two
// float32 lanes. Values stay free of NaN/inf so no divergent-NaN concern.
func vecFPInputs(dbl bool) []oracleRegs {
	var r oracleRegs
	for i := 0; i < 32; i++ {
		if dbl {
			lo := fpSeedValues[(i)%len(fpSeedValues)]
			hi := fpSeedValues[(i+5)%len(fpSeedValues)]
			r.V[i] = [2]uint64{math.Float64bits(lo), math.Float64bits(hi)}
		} else {
			l0 := float32(fpSeedValues[(i)%len(fpSeedValues)])
			l1 := float32(fpSeedValues[(i+3)%len(fpSeedValues)])
			h0 := float32(fpSeedValues[(i+7)%len(fpSeedValues)])
			h1 := float32(fpSeedValues[(i+11)%len(fpSeedValues)])
			lo := uint64(math.Float32bits(l0)) | uint64(math.Float32bits(l1))<<32
			hi := uint64(math.Float32bits(h0)) | uint64(math.Float32bits(h1))<<32
			r.V[i] = [2]uint64{lo, hi}
		}
	}
	r.SP = 0x80000
	return []oracleRegs{r}
}

func TestARM64_NativeOracleFP(t *testing.T) {
	// Double-precision programs: seeded with float64 values in V0–V31.
	dbl := [][]string{
		{"fadd d0, d1, d2"}, {"fsub d0, d3, d4"}, {"fmul d0, d1, d5"},
		{"fdiv d0, d6, d1"}, {"fnmul d0, d2, d3"},
		{"fmax d0, d4, d5", "fmin d3, d4, d5"},
		{"fmaxnm d0, d1, d4", "fminnm d3, d1, d4"},
		{"fabs d0, d4", "fneg d3, d1", "fsqrt d5, d6"},
		{"fmov d0, d7"},
		{"fmov x0, d1", "fmov d2, x3"}, // FP↔GPR bit moves
		{"fcvt s0, d1", "fcvt d3, s0"}, // round-trip precision
		{"scvtf d0, x1", "ucvtf d2, x3"},
		{"fcvtzs x0, d4", "fcvtzu x2, d1"}, // truncation + saturation
		{"fcmp d1, d2", "cset x0, mi", "cset x3, gt"},
		{"fcmp d4, d4", "cset x0, eq"},
		{"fcmp d1, #0.0", "cset x0, gt", "fcmp d4, #0.0", "cset x3, mi"},
		{"fcsel d0, d1, d2, eq", "fcmp d1, d2", "fcsel d3, d4, d5, lt"},
		{"fadd d0, d1, d2", "fmul d0, d0, d3", "fsub d0, d0, d4", "fdiv d0, d0, d5"},
	}
	// Single-precision programs: seeded with float32 values.
	sgl := [][]string{
		{"fadd s0, s1, s2"}, {"fsub s0, s3, s4"}, {"fmul s0, s1, s5"},
		{"fdiv s0, s6, s1"}, {"fnmul s0, s2, s3"},
		{"fmax s0, s4, s5", "fmin s3, s4, s5"},
		{"fabs s0, s4", "fneg s3, s1", "fsqrt s5, s6"},
		{"fmov w0, s1", "fmov s2, w3"},
		{"scvtf s0, w1", "ucvtf s2, w3"},
		{"fcvtzs w0, s4", "fcvtzu w2, s1"},
		{"fcmp s1, s2", "cset x0, mi", "cset x3, gt"},
		{"fcsel s0, s1, s2, gt"},
	}
	run := func(progs [][]string, inputs []oracleRegs) {
		for _, prog := range progs {
			label := strings.Join(prog, "; ")
			_, words := assembleWords(t, prog)
			bin := oracleBin(t, words)
			for _, in := range inputs {
				native := runBin(t, bin, in)
				got := cpuRun(t, prog, in)
				diffRegs(t, label, native, got)
			}
		}
	}
	run(dbl, fpInputs(true))
	run(sgl, fpInputs(false))

	// Saturation + unordered-compare paths, hardware-checked with inf/NaN seeds.
	// These programs write only GPRs/NZCV (never a propagated NaN into V), so the
	// divergent-NaN-payload concern that keeps NaN out of fpSeedValues doesn't
	// apply: the V registers carry the seed bits through unchanged on both sides.
	specialD := [][]string{
		{"fcvtzs x0, d0", "fcvtzs x3, d1", "fcvtzs x4, d2"}, // +inf, -inf, NaN -> MAX, MIN, 0
		{"fcvtzu x0, d0", "fcvtzu x3, d1", "fcvtzu x4, d2"}, // MAX, 0, 0
		{"fcvtzs x0, d3", "fcvtzs x3, d4"},                  // ±1e300 -> ±sat
		{"fcmp d2, d5", "cset x0, vs", "cset x3, cs", "cset x4, mi", "cset x5, eq", "cset x6, gt"}, // unordered: V=C=1
		{"fcmp d0, d1", "cset x0, gt", "cset x3, mi"}, // +inf > -inf
	}
	specialS := [][]string{
		{"fcvtzs w0, s0", "fcvtzs w3, s1", "fcvtzu w4, s0", "fcvtzs w5, s2"}, // 32-bit saturation + NaN
		{"fcmp s2, s5", "cset x0, vs", "cset x3, vc"},                        // single-precision unordered
	}
	run(specialD, fpSpecialInputs(true))
	run(specialS, fpSpecialInputs(false))
}
