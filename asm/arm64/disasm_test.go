package arm64

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// mcDisassemble decodes 4 bytes with llvm-mc and returns the instruction text
// (first non-directive, non-comment line). ok=false if llvm-mc fails.
func mcDisassemble(t *testing.T, b []byte) (string, bool) {
	t.Helper()
	hex := fmt.Sprintf("0x%02x 0x%02x 0x%02x 0x%02x", b[0], b[1], b[2], b[3])
	cmd := exec.Command(llvmMC, "--triple=aarch64", "--disassemble")
	cmd.Stdin = strings.NewReader(hex + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ".") || strings.HasPrefix(line, "#") {
			continue
		}
		return line, true
	}
	return "", false
}

// TestARM64_RoundTrip is the strong decode check: take llvm-mc's (trusted)
// bytes for each instruction, disassemble them with our decoder, re-assemble
// the text with our (separately llvm-validated) encoder, and require the bytes
// come back identical. Independent of text formatting — only bytes are compared,
// so it proves the decode names the same instruction the bytes encode.
func TestARM64_RoundTrip(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"add x0, x1, x2", "sub w3, w4, w5", "adds x0, x1, x2", "subs x7, x8, x9",
		"add x0, x1, x2, lsl #4", "sub x0, x1, x2, asr #3",
		"add sp, sp, x0", "add x0, sp, x1",
		"add x0, x1, #8", "sub x0, x1, #0x10", "add x0, x1, #1, lsl #12",
		"add sp, sp, #16", "subs xzr, x0, #1",
		"and x0, x1, x2", "orr x0, x1, x2", "eor x0, x1, x2", "ands x0, x1, x2",
		"bic x0, x1, x2", "orn x0, x1, x2", "eon x0, x1, x2", "bics x0, x1, x2",
		"orr x0, x1, x2, lsl #8", "eor x0, x1, x2, ror #16",
		"and x0, x1, #0xff", "eor x0, x1, #0xf0f0f0f0f0f0f0f0",
		"ands x5, x6, #0x3c", "and w7, w8, #0x80000001", "orr x0, x1, #1",
		"movz x0, #0x1234", "movz x0, #0xffff, lsl #16", "movn x0, #0",
		"movk x0, #0xabcd, lsl #32", "movz w0, #0x10",
		"ldr x0, [x1]", "ldr x0, [x1, #8]", "str x0, [x1, #16]",
		"ldr w0, [x1, #8]", "str w2, [x3, #4]", "ldr x30, [x29, #2040]",
		"strb w0, [x1, #1]", "ldrh w0, [x1, #8]", "ldrsb x0, [x1, #1]",
		"ldrsw x0, [x1, #4]", "stur x0, [x1, #7]", "ldur x0, [x1, #-8]",
		"str x0, [x1, #8]!", "ldr x0, [x1], #-16", "ldrb w0, [x1], #1",
		"ldr x0, [x1, x2]", "str x0, [x1, x2, lsl #3]", "ldr x0, [x1, w2, sxtw]",
		"ldr x0, [x1, w2, uxtw #3]", "ldrb w0, [x1, x2]",
		"stp x29, x30, [sp, #-16]!", "ldp x29, x30, [sp], #16",
		"stp x0, x1, [x2, #16]", "ldp w0, w1, [sp, #4]", "ldpsw x0, x1, [x2, #4]",
		"madd x0, x1, x2, x3", "msub w0, w1, w2, w3", "smaddl x0, w1, w2, x3",
		"umsubl x0, w1, w2, x3", "smulh x0, x1, x2", "umulh x0, x1, x2",
		"udiv x0, x1, x2", "sdiv w0, w1, w2", "lslv x0, x1, x2", "rorv w0, w1, w2",
		"rbit x0, x1", "rev x0, x1", "rev w0, w1", "rev16 x0, x1", "rev32 x0, x1",
		"clz x0, x1", "cls w0, w1",
		"ubfm x0, x1, #8, #31", "sbfm w0, w1, #4, #15", "bfm x0, x1, #0, #7",
		"extr x0, x1, x2, #8", "lsl x0, x1, #4", "lsr w0, w1, #3", "asr x0, x1, #2",
		"ubfx x0, x1, #4, #8", "sxtb x0, w1", "uxtb w0, w1",
		"csel x0, x1, x2, eq", "csinc w0, w1, w2, ne", "csneg x0, x1, x2, gt",
		"adr x0, #4", "adr x0, #-4", "adrp x0, #4096", "adrp x0, #-8192",
		"adc x0, x1, x2", "sbcs w3, w4, w5",
		"ccmp x0, x1, #0, eq", "ccmp x0, #5, #3, lt", "ccmn x0, x1, #15, ne",
		"nop", "wfi", "dmb sy", "dmb ish", "isb", "hint #7", "eret",
		"mrs x0, nzcv", "mrs x1, tpidr_el0", "msr nzcv, x2", "msr fpcr, x3",
		"svc #5", "brk #0xf000", "hlt #1",
		"b #0", "b #8", "b #-8", "bl #16", "b.eq #8", "b.ne #-4", "b.lt #12",
		"cbz x0, #8", "cbnz w1, #-8",
		"ret", "ret x0", "br x1", "blr x2",
		// scalar floating-point
		"fadd d0, d1, d2", "fsub s3, s4, s5", "fmul d0, d1, d2", "fdiv s0, s1, s2",
		"fmax d7, d8, d9", "fmin s0, s1, s2", "fmaxnm d0, d1, d2", "fminnm s0, s1, s2",
		"fnmul d0, d1, d2", "fabs d0, d1", "fneg s0, s1", "fsqrt d0, d1", "fmov d0, d1",
		"fmov d0, x1", "fmov x2, d3", "fmov s0, w1", "fmov w2, s3",
		"fcvt d0, s1", "fcvt s0, d1", "fcvt h0, s1", "fcvt s0, h1",
		"scvtf d0, x1", "ucvtf s2, w3", "fcvtzs x0, d1", "fcvtzu w4, s5",
		"fcmp d0, d1", "fcmp s2, s3", "fcmp d0, #0.0", "fcmpe d0, d1",
		"fcsel d0, d1, d2, eq", "fcsel s0, s1, s2, gt",
		// FP load/store
		"ldr d0, [x1]", "ldr d0, [x1, #8]", "str s2, [x3, #4]", "ldr q0, [x1]",
		"str q1, [x2, #48]", "ldr d0, [x1, x2, lsl #3]", "str s0, [x1], #4",
		"ldr d0, [x1, #8]!", "ldur d0, [x1, #7]", "stur s2, [x3, #-4]",
		// vector (Advanced SIMD three-same)
		"add v0.16b, v1.16b, v2.16b", "add v0.4s, v1.4s, v2.4s", "add v0.2d, v1.2d, v2.2d",
		"sub v3.8h, v4.8h, v5.8h", "mul v0.2s, v1.2s, v2.2s",
		"and v0.8b, v1.8b, v2.8b", "orr v0.16b, v1.16b, v2.16b",
		"eor v0.16b, v1.16b, v2.16b", "bic v0.16b, v1.16b, v2.16b", "orn v0.8b, v1.8b, v2.8b",
		// vector copy group (dup/umov/smov/ins)
		"dup v0.4s, w1", "dup v0.2d, x1", "dup v0.16b, v1.b[3]", "dup v0.2d, v1.d[1]",
		"umov w0, v1.s[2]", "umov x0, v1.d[1]", "smov x0, v1.b[1]", "smov x0, v1.h[2]",
		"ins v0.s[1], w2", "ins v0.d[0], x3", "ins v0.s[1], v2.s[3]", "ins v0.b[2], v3.b[7]",
	}
	for _, src := range cases {
		want, ok := mcEncode(t, src)
		if !ok {
			t.Logf("skip %q (no concrete llvm bytes)", src)
			continue
		}
		text, err := DisassembleBytes(want)
		if err != nil {
			t.Errorf("%q -> %02x: disassemble: %v", src, want, err)
			continue
		}
		got, err := Assemble(text)
		if err != nil {
			t.Errorf("%q -> disasm %q -> reassemble: %v", src, text, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%q: disasm=%q reencoded %02x, llvm %02x", src, text, got, want)
		}
	}
}

// TestARM64_DisasmVsLLVM compares our disassembly text directly against
// llvm-mc's, for instructions llvm does not render with an alias (so the
// mnemonics line up). This is the decode check that does NOT lean on our own
// encoder. Alias-preferred forms (movz->mov, subs xzr->cmp, orr xzr->mov) are
// deliberately excluded.
func TestARM64_DisasmVsLLVM(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"add x0, x1, x2", "sub w3, w4, w5", "adds x5, x6, x7",
		"add x0, x1, x2, lsl #4", "add x0, x1, #8", "sub x0, x1, #0x10",
		"and x0, x1, x2", "eor x0, x1, x2", "ands x9, x10, x11",
		"bic x0, x1, x2", "orn x0, x1, x2",
		"ldr x0, [x1, #8]", "str w2, [x3, #4]", "ldr x0, [x1]",
		"strb w0, [x1, #1]", "ldrsw x0, [x1, #4]", "stur x0, [x1, #7]",
		"ldur x0, [x1, #-8]", "str x0, [x1, #8]!", "ldr x0, [x1], #16",
		"ldr x0, [x1, x2]", "str x0, [x1, x2, lsl #3]", "ldr x0, [x1, w2, sxtw]",
		"madd x0, x1, x2, x3", "msub w0, w1, w2, w3", "smaddl x0, w1, w2, x3",
		"smulh x0, x1, x2", "udiv x0, x1, x2", "sdiv w0, w1, w2",
		"rbit x0, x1", "rev16 x0, x1", "clz x0, x1", "cls w0, w1",
		"csel x0, x1, x2, eq", "csinc w3, w4, w5, ne", "csinv x0, x1, x2, lt",
		"csneg x0, x1, x2, gt", "adr x0, #4", "adrp x0, #4096",
		"cbz x0, #8", "cbnz w1, #16",
		"br x1", "blr x2",
		// scalar floating-point (none of these are alias-preferred by llvm)
		"fadd d0, d1, d2", "fsub s3, s4, s5", "fmul d0, d1, d2", "fdiv s0, s1, s2",
		"fmax d7, d8, d9", "fminnm s0, s1, s2", "fnmul d0, d1, d2",
		"fabs d0, d1", "fneg s0, s1", "fsqrt d0, d1", "fmov d0, d1",
		"fmov d0, x1", "fmov w2, s3", "fcvt d0, s1", "fcvt s0, d1",
		"scvtf d0, x1", "ucvtf s2, w3", "fcvtzs x0, d1", "fcvtzu w4, s5",
		"fcmp d0, d1", "fcmpe d0, d1", "fcsel d0, d1, d2, eq",
		"ldr d0, [x1, #8]", "str s2, [x3, #4]", "ldr q0, [x1]",
		"ldr d0, [x1, x2, lsl #3]", "ldur d0, [x1, #7]",
	}
	for _, src := range cases {
		b, ok := mcEncode(t, src)
		if !ok {
			t.Logf("skip %q", src)
			continue
		}
		ours, err := DisassembleBytes(b)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		theirs, ok := mcDisassemble(t, b)
		if !ok {
			t.Logf("skip %q (llvm disasm failed)", src)
			continue
		}
		if normalizeAsm(ours) != normalizeAsm(theirs) {
			t.Errorf("%q: ours %q  llvm %q", src, normalizeAsm(ours), normalizeAsm(theirs))
		}
	}
}
