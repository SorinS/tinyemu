package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	a64 "github.com/sorins/tinyemu-go/asm/arm64"
)

const (
	llvmMC      = "/opt/homebrew/opt/llvm/bin/llvm-mc"
	llvmObjdump = "/opt/homebrew/opt/llvm/bin/llvm-objdump"
)

func requireLLVM(t *testing.T) {
	t.Helper()
	for _, p := range []string{llvmMC, llvmObjdump} {
		if _, err := os.Stat(p); err != nil {
			t.Skip("llvm tools not found: " + p)
		}
	}
}

var immRe = regexp.MustCompile(`#(-?0x[0-9a-fA-F]+|-?[0-9]+)`)

// normalize canonicalizes disassembly text for comparison: drop an objdump
// "// =…" comment, lower-case, collapse whitespace, and render every #immediate
// in decimal (objdump prefers hex, our decoder mixes).
func normalize(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	s = immRe.ReplaceAllStringFunc(s, func(m string) string {
		v, err := strconv.ParseInt(m[1:], 0, 64)
		if err != nil {
			return m
		}
		return "#" + strconv.FormatInt(v, 10)
	})
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.ReplaceAll(s, " ,", ",")
	return s
}

// objdumpInsns assembles src with llvm-mc to an ELF and returns llvm-objdump's
// per-instruction text, in order.
func objdumpInsns(t *testing.T, src string) []string {
	t.Helper()
	dir := t.TempDir()
	sPath := filepath.Join(dir, "t.s")
	oPath := filepath.Join(dir, "t.o")
	if err := os.WriteFile(sPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(llvmMC, "--triple=aarch64", "--filetype=obj", sPath, "-o", oPath).CombinedOutput(); err != nil {
		t.Fatalf("llvm-mc: %v\n%s", err, out)
	}
	out, err := exec.Command(llvmObjdump, "-d", "--no-show-raw-insn", oPath).CombinedOutput()
	if err != nil {
		t.Fatalf("llvm-objdump: %v\n%s", err, out)
	}
	var insns []string
	for _, line := range strings.Split(string(out), "\n") {
		// Instruction lines look like "      0:\tadd\tx0, x1, x2": the token
		// before the tab is a hex address. Skip the "<file>:\tfile format …"
		// header (whose token also ends in ':') and the "<.text>:" section line.
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		addr := strings.TrimSuffix(strings.TrimSpace(line[:tab]), ":")
		if addr == "" || strings.TrimLeft(addr, "0123456789abcdefABCDEF") != "" {
			continue
		}
		insns = append(insns, strings.TrimSpace(line[tab+1:]))
	}
	return insns
}

// TestDisasm_ARM64_VsObjdump decodes a curated, non-aliased instruction set and
// requires our AArch64 disassembly to match llvm-objdump's, instruction for
// instruction (modulo immediate radix). Aliased forms (mov/lsl/cmp/…) and
// branches are excluded because objdump renders them differently by design.
func TestDisasm_ARM64_VsObjdump(t *testing.T) {
	requireLLVM(t)
	insns := []string{
		"add x0, x1, x2", "sub w3, w4, w5", "add x0, x1, #8", "sub x0, x1, #0x10",
		"and x0, x1, x2", "orr x5, x6, x7", "eor x0, x1, x2", "ands x9, x10, x11",
		"and x0, x1, #0xff", "eor x2, x3, #0xf0f0f0f0f0f0f0f0",
		"ldr x0, [x1, #8]", "str w2, [x3, #4]", "ldrb w0, [x1, #1]", "ldrsw x0, [x1, #4]",
		"stp x29, x30, [sp, #-16]", "ldp x0, x1, [x2, #16]",
		"madd x0, x1, x2, x3", "msub w0, w1, w2, w3", "smulh x0, x1, x2",
		"udiv x0, x1, x2", "sdiv w0, w1, w2",
		"clz x0, x1", "rev x0, x1", "rev16 w0, w1", "rbit x0, x1",
		"csel x0, x1, x2, eq", "csinc w3, w4, w5, ne",
		"adc x0, x1, x2", "sbc x3, x4, x5",
		"ccmp x0, x1, #0, eq", "ccmn x2, x3, #15, lt",
	}
	want := objdumpInsns(t, strings.Join(insns, "\n")+"\n")
	if len(want) != len(insns) {
		t.Fatalf("objdump returned %d lines, want %d", len(want), len(insns))
	}
	for i, src := range insns {
		b, err := a64.Assemble(src)
		if err != nil {
			t.Fatalf("assemble %q: %v", src, err)
		}
		got, err := a64.DisassembleBytes(b)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if normalize(got) != normalize(want[i]) {
			t.Errorf("%q: ours %q  objdump %q", src, normalize(got), normalize(want[i]))
		}
	}
}

// TestDisasm_Tool exercises the tool's raw-bytes path for two ISAs.
func TestDisasm_Tool(t *testing.T) {
	var out bytes.Buffer
	// arm64: add x0,x1,x2 ; ret
	if err := Disassemble(&out, "arm64", []byte{0x20, 0x00, 0x02, 0x8b, 0xc0, 0x03, 0x5f, 0xd6}, 0x1000); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "1000:") || !strings.Contains(s, "add x0, x1, x2") || !strings.Contains(s, "ret") {
		t.Errorf("arm64 tool output:\n%s", s)
	}
	out.Reset()
	// riscv64: add a0,a0,a2 (33 05 c5 00)
	if err := Disassemble(&out, "riscv64", []byte{0x33, 0x05, 0xc5, 0x00}, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "add a0, a0, a2") {
		t.Errorf("riscv tool output:\n%s", out.String())
	}
}
