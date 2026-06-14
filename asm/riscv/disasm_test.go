package riscv

import "testing"

func TestRISCV_DisassembleKnown(t *testing.T) {
	cases := []struct {
		bytes []byte
		want  string
	}{
		{[]byte{0x93, 0x00, 0x50, 0x00}, "addi ra, zero, 5"},
		{[]byte{0x33, 0x81, 0x10, 0x00}, "add sp, ra, ra"},
		{[]byte{0x83, 0x22, 0x81, 0x00}, "lw t0, 8(sp)"},
		{[]byte{0xb7, 0x51, 0x34, 0x12}, "lui gp, 0x12345"},
		{[]byte{0x33, 0x85, 0xc5, 0x02}, "mul a0, a1, a2"},
		{[]byte{0x73, 0x00, 0x00, 0x00}, "ecall"},
	}
	for _, c := range cases {
		got, n, err := Disassemble(c.bytes)
		if err != nil || n != 4 {
			t.Errorf("Disassemble(% x): %q n=%d err=%v", c.bytes, got, n, err)
			continue
		}
		if got != c.want {
			t.Errorf("Disassemble(% x) = %q, want %q", c.bytes, got, c.want)
		}
	}
}

// TestRISCV_RoundTrip assembles, disassembles, and re-assembles, requiring the
// bytes to be stable — proving the encoder and decoder agree.
func TestRISCV_RoundTrip(t *testing.T) {
	insns := []string{
		"add x2, x1, x1", "sub a0, a1, a2", "and t0, t1, t2", "xor a0, a1, a2",
		"sll a0, a1, a2", "srl a0, a1, a2", "sra a0, a1, a2", "slt a0, a1, a2",
		"addw a0, a1, a2", "subw a0, a1, a2", "sraw a0, a1, a2",
		"mul a0, a1, a2", "div a0, a1, a2", "remu a0, a1, a2", "mulw a0, a1, a2",
		"addi sp, sp, -16", "andi a0, a1, 255", "ori a0, a1, 1", "xori a0, a1, -1",
		"slti a0, a1, 5", "addiw a0, a1, 7",
		"slli a0, a1, 3", "srli a0, a1, 40", "srai a0, a1, 63",
		"slliw a0, a1, 3", "sraiw a0, a1, 31",
		"lw t0, 8(sp)", "ld a0, 0(a1)", "lb a0, -4(a1)", "lbu a0, 1(a1)",
		"sw a0, 8(sp)", "sd ra, 0(sp)", "sh a0, 2(a1)",
		"jalr ra, 0(a0)",
		"beq x1, x2, 16", "bne a0, a1, -8", "blt a0, a1, 256", "bgeu a0, a1, 2048",
		"jal ra, 32", "jal zero, -16",
		"lui gp, 0x12345", "auipc a0, 0x1000",
		"ecall", "ebreak", "mret", "sret", "wfi",
		"csrrw a0, mstatus, a1", "csrrs a0, mtvec, zero", "csrrwi a0, 0x340, 5",
		"fence", "fence rw, rw", "fence.i", "fence r, w",
		"lr.w a0, (a1)", "sc.w a0, a1, (a2)", "amoadd.w a0, a1, (a2)",
		"amoswap.d a0, a1, (a2)", "amoadd.w.aqrl a0, a1, (a2)", "lr.d.aq a0, (a1)",
		// F/D
		"flw fa0, 8(sp)", "fsd fa0, 0(a1)",
		"fadd.s fa0, fa1, fa2", "fsqrt.d fa0, fa1", "fmin.s fa0, fa1, fa2",
		"feq.s a0, fa1, fa2", "fcvt.w.s a0, fa1", "fcvt.s.w fa0, a1",
		"fmv.x.w a0, fa1", "fcvt.s.d fa0, fa1", "fcvt.d.s fa0, fa1",
		"fmadd.s fa0, fa1, fa2, fa3", "fnmadd.d fa0, fa1, fa2, fa3",
		"fadd.s fa0, fa1, fa2, rtz", "fsgnj.d fa0, fa1, fa2",
	}
	for _, src := range insns {
		mine, err := Assemble(src)
		if err != nil {
			t.Errorf("assemble %q: %v", src, err)
			continue
		}
		text, n, err := Disassemble(mine)
		if err != nil || n != 4 {
			t.Errorf("disasm %q (% x): %q n=%d err=%v", src, mine, text, n, err)
			continue
		}
		again, err := Assemble(text)
		if err != nil {
			t.Errorf("re-assemble %q -> %q: %v", src, text, err)
			continue
		}
		if !eq(again, mine) {
			t.Errorf("UNSTABLE %q: % x -> %q -> % x", src, mine, text, again)
		}
	}
}
