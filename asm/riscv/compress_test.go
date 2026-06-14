package riscv

import (
	"os/exec"
	"strings"
	"testing"
)

// mcEncodeC assembles with --mattr=+c (auto-compresses, so write c.* explicitly)
// plus +d/+f for compressed FP.
func mcEncodeC(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	cmd := exec.Command(llvmMC, "--triple=riscv64", "--mattr=+c,+f,+d", "--show-encoding")
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
			return nil, false
		}
		v, err := parseHexByte(tok[2:])
		if err != nil {
			return nil, false
		}
		b = append(b, v)
	}
	return b, true
}

func parseHexByte(s string) (byte, error) {
	var v uint64
	for _, c := range s {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint64(c-'a') + 10
		default:
			return 0, errBadHex
		}
	}
	return byte(v), nil
}

var errBadHex = &hexErr{}

type hexErr struct{}

func (*hexErr) Error() string { return "bad hex" }

func TestRISCV_Compressed_Differential(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		"c.nop", "c.ebreak",
		"c.addi a0, 1", "c.addi a0, -1", "c.li a0, 5", "c.lui a0, 1", "c.addiw a0, 1",
		"c.mv a0, a1", "c.add a0, a1", "c.jr a0", "c.jalr a0", "c.slli a0, 3",
		"c.addi16sp sp, -16", "c.addi16sp sp, 32", "c.addi4spn a0, sp, 16",
		"c.lw a0, 4(a1)", "c.sw a0, 4(a1)", "c.ld a0, 8(a1)", "c.sd a0, 8(a1)",
		"c.lwsp a0, 4(sp)", "c.swsp a0, 4(sp)", "c.ldsp a0, 8(sp)", "c.sdsp a0, 8(sp)",
		"c.srli a0, 1", "c.srai a0, 1", "c.andi a0, 1", "c.andi a0, -1",
		"c.sub a0, a1", "c.xor a0, a1", "c.or a0, a1", "c.and a0, a1", "c.addw a0, a1", "c.subw a0, a1",
		"c.j 16", "c.j -16", "c.beqz a0, 16", "c.bnez a0, -16",
		"c.fld fa0, 8(a1)", "c.fsd fa0, 8(a1)", "c.fldsp fa0, 8(sp)", "c.fsdsp fa0, 8(sp)",
	}
	var pass, fail int
	for _, src := range cases {
		want, ok := mcEncodeC(t, src)
		if !ok {
			t.Logf("SKIP (llvm rejected): %s", src)
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
	t.Logf("compressed vs llvm-mc: %d pass, %d fail", pass, fail)
}

func TestRISCV_Compressed_RoundTrip(t *testing.T) {
	insns := []string{
		"c.nop", "c.ebreak", "c.addi a0, -3", "c.li a0, 5", "c.lui a0, 1", "c.addiw a0, 7",
		"c.mv a0, a1", "c.add a0, a1", "c.jr a0", "c.jalr a0", "c.slli a0, 40",
		"c.addi16sp sp, -16", "c.addi4spn a0, sp, 16",
		"c.lw a0, 4(a1)", "c.sw a0, 4(a1)", "c.ld a0, 8(a1)", "c.sd a0, 8(a1)",
		"c.lwsp a0, 4(sp)", "c.swsp a0, 4(sp)", "c.ldsp a0, 8(sp)", "c.sdsp a0, 8(sp)",
		"c.srli a0, 1", "c.srai a0, 5", "c.andi a0, -1",
		"c.sub a0, a1", "c.xor a0, a1", "c.or a0, a1", "c.and a0, a1", "c.addw a0, a1", "c.subw a0, a1",
		"c.j 16", "c.j -16", "c.beqz a0, 16", "c.bnez a0, -16",
		"c.fld fa0, 8(a1)", "c.fsd fa0, 8(a1)", "c.fldsp fa0, 8(sp)", "c.fsdsp fa0, 8(sp)",
	}
	for _, src := range insns {
		mine, err := Assemble(src)
		if err != nil {
			t.Errorf("assemble %q: %v", src, err)
			continue
		}
		if len(mine) != 2 {
			t.Errorf("%q: %d bytes, want 2 (compressed)", src, len(mine))
		}
		text, n, err := Disassemble(mine)
		if err != nil || n != 2 {
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
