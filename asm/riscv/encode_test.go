package riscv

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// llvmMC is the path to llvm-mc (brew LLVM). Tests are skipped if absent.
const llvmMC = "/opt/homebrew/opt/llvm/bin/llvm-mc"

func requireLLVMMC(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(llvmMC); err != nil {
		t.Skip("llvm-mc not found at " + llvmMC)
	}
}

var encRe = regexp.MustCompile(`encoding:\s*\[([^\]]*)\]`)

// mcEncode assembles one instruction with llvm-mc and returns its bytes.
// ok=false if llvm-mc errored or the encoding contains fixup placeholders
// (PC-relative operands show up as 'A'/0bAAAA…, not concrete bytes).
func mcEncode(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	cmd := exec.Command(llvmMC, "--triple=riscv64", "--mattr=+m", "--show-encoding")
	cmd.Stdin = strings.NewReader(src + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false
	}
	m := encRe.FindStringSubmatch(string(out))
	if m == nil {
		return nil, false
	}
	var b []byte
	for _, tok := range strings.Split(m[1], ",") {
		tok = strings.TrimSpace(tok)
		if !strings.HasPrefix(tok, "0x") || len(tok) != 4 {
			return nil, false // fixup placeholder, not a concrete byte
		}
		v, err := strconv.ParseUint(tok[2:], 16, 8)
		if err != nil {
			return nil, false
		}
		b = append(b, byte(v))
	}
	return b, true
}

func TestRISCV_DifferentialLLVMMC(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// R-type
		"add x2, x1, x1", "sub a0, a1, a2", "and t0, t1, t2", "or s0, s1, s2",
		"xor a0, a1, a2", "sll a0, a1, a2", "srl a0, a1, a2", "sra a0, a1, a2",
		"slt a0, a1, a2", "sltu a0, a1, a2",
		"addw a0, a1, a2", "subw a0, a1, a2", "sllw a0, a1, a2",
		// M extension
		"mul a0, a1, a2", "mulh a0, a1, a2", "mulhu a0, a1, a2", "div a0, a1, a2",
		"divu a0, a1, a2", "rem a0, a1, a2", "remu a0, a1, a2", "mulw a0, a1, a2",
		// I-type
		"addi sp, sp, -16", "addi x1, x0, 5", "andi a0, a1, 0xff", "ori a0, a1, 1",
		"xori a0, a1, -1", "slti a0, a1, 5", "sltiu a0, a1, 5", "addiw a0, a1, 7",
		// shifts
		"slli a0, a1, 3", "srli a0, a1, 3", "srai a0, a1, 3",
		"slli a0, a1, 40", // RV64 6-bit shamt
		"slliw a0, a1, 3", "srliw a0, a1, 3", "sraiw a0, a1, 3",
		// loads / stores
		"lw t0, 8(sp)", "ld a0, 0(a1)", "lb a0, -4(a1)", "lbu a0, 1(a1)",
		"sw a0, 8(sp)", "sd ra, 0(sp)", "sb a0, -4(a1)", "sh a0, 2(a1)",
		// jalr
		"jalr ra, 0(a0)",
		// branches (numeric offset → llvm-mc encodes concretely)
		"beq x1, x2, 16", "bne a0, a1, -8", "blt a0, a1, 256", "bge a0, a1, -256",
		"bltu a0, a1, 8", "bgeu a0, a1, 2048",
		// jal
		"jal ra, 32", "jal zero, -16", "jal ra, 1048574",
		// U-type
		"lui gp, 0x12345", "auipc a0, 0x1000",
		// system
		"ecall", "ebreak", "mret", "sret", "wfi",
		// Zicsr
		"csrrw a0, mstatus, a1", "csrrs a0, mtvec, zero", "csrrc t0, sepc, t1",
		"csrrwi a0, 0x340, 5", "csrrsi a0, mie, 1",
		"csrr a0, mhartid", "csrw mtvec, a0", "csrs sstatus, a1",
		// fence
		"fence", "fence rw, rw", "fence.i", "fence r, w",
	}
	var pass, fail int
	for _, src := range cases {
		want, ok := mcEncode(t, src)
		if !ok {
			t.Logf("SKIP (llvm-mc rejected/fixup): %s", src)
			continue
		}
		got, err := Assemble(src)
		switch {
		case err != nil:
			t.Errorf("MISS %-24q: %v", src, err)
			fail++
		case !eq(got, want):
			t.Errorf("DIFF %-24q\n mine % x\n llvm % x", src, got, want)
			fail++
		default:
			pass++
		}
	}
	t.Logf("RISC-V vs llvm-mc: %d pass, %d fail", pass, fail)
}

func eq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
