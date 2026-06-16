package arm64

import "fmt"

// Conditional select (csel/csinc/csinv/csneg) and PC-relative address
// (adr/adrp). The cset/csetm/cinc/cinv/cneg aliases (which invert the
// condition) are handled in alias.go.

// condSelForm gives the op (bit30) and op2 (bits[11:10]) of a conditional
// select.
var condSelForm = map[string][2]uint32{
	"csel":  {0, 0b00},
	"csinc": {0, 0b01},
	"csinv": {1, 0b00},
	"csneg": {1, 0b01},
}

func encodeCondSel(mnem string, ops []string) (uint32, error) {
	f, ok := condSelForm[mnem]
	if !ok {
		return 0, fmt.Errorf("unknown conditional select %q", mnem)
	}
	if len(ops) != 4 {
		return 0, fmt.Errorf("%s expects Rd, Rn, Rm, cond", mnem)
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	rm, ok3 := parseReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad register operand")
	}
	cond, ok := condCodes[normCond(ops[3])]
	if !ok {
		return 0, fmt.Errorf("unknown condition %q", ops[3])
	}
	op, op2 := f[0], f[1]
	return sfBit(rd) | op<<30 | 0x1A800000 | rm.num<<16 | cond<<12 | op2<<10 | rn.num<<5 | rd.num, nil
}

// encodeAddr encodes adr/adrp with a numeric operand (PC-relative byte offset
// for adr; a page-aligned byte offset for adrp). Label operands are resolved at
// the program level (see assembleAt).
func encodeAddr(mnem string, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("%s expects Rd, offset", mnem)
	}
	rd, ok := parseReg(ops[0])
	if !ok || !rd.is64 {
		return 0, fmt.Errorf("adr/adrp destination must be a 64-bit register")
	}
	off, ok := parseImm(ops[1])
	if !ok {
		return 0, fmt.Errorf("adr/adrp needs a numeric offset here (labels resolve at program level)")
	}
	imm := off
	op := uint32(0)
	if mnem == "adrp" {
		if off%4096 != 0 {
			return 0, fmt.Errorf("adrp offset %d must be a multiple of 4096", off)
		}
		imm = off >> 12
		op = 1
	}
	if imm < -(1<<20) || imm > (1<<20)-1 {
		return 0, fmt.Errorf("%s offset out of ±1MB range", mnem)
	}
	immlo := uint32(imm&3) << 29
	immhi := uint32((imm>>2)&0x7FFFF) << 5
	return op<<31 | 0x10000000 | immlo | immhi | rd.num, nil
}

// normCond lower-cases a condition operand.
func normCond(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
