package arm64

import "fmt"

// Data-processing (register) instructions: multiply (3-source), divide and
// variable shift (2-source), and the 1-source unary ops.

// mulForm describes a 3-source multiply encoding.
type mulForm struct {
	op31 uint32 // bits [23:21]
	o0   uint32 // bit 15
	widen bool  // smaddl/umaddl/…: Xd/Xa are 64-bit, Wn/Wm 32-bit
	high  bool  // smulh/umulh: no Ra operand (Ra = ZR)
}

var mulForms = map[string]mulForm{
	"madd":   {0b000, 0, false, false},
	"msub":   {0b000, 1, false, false},
	"smaddl": {0b001, 0, true, false},
	"smsubl": {0b001, 1, true, false},
	"umaddl": {0b101, 0, true, false},
	"umsubl": {0b101, 1, true, false},
	"smulh":  {0b010, 0, false, true},
	"umulh":  {0b110, 0, false, true},
}

func encodeMul(mnem string, ops []string) (uint32, error) {
	f, ok := mulForms[mnem]
	if !ok {
		return 0, fmt.Errorf("unknown multiply %q", mnem)
	}
	want := 3
	if !f.high {
		want = 4 // madd/msub/…l take an explicit accumulator
	}
	if len(ops) != want {
		return 0, fmt.Errorf("%s expects %d operands", mnem, want)
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	rm, ok3 := parseReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad register operand")
	}
	ra := reg{num: 31, is64: true}
	if !f.high {
		r, ok := parseReg(ops[3])
		if !ok {
			return 0, fmt.Errorf("bad accumulator register")
		}
		ra = r
	}
	// The widening and high-multiply forms are always 64-bit (sf=1); madd/msub
	// take their width from Rd.
	sf := uint32(1) << 31
	if !f.widen && !f.high {
		sf = sfBit(rd)
	}
	return sf | 0x1B000000 | f.op31<<21 | rm.num<<16 | f.o0<<15 | ra.num<<10 | rn.num<<5 | rd.num, nil
}

// dp2Op is the opcode[15:10] field of a 2-source data-processing instruction.
var dp2Op = map[string]uint32{
	"udiv": 0x02, "sdiv": 0x03,
	"lslv": 0x08, "lsrv": 0x09, "asrv": 0x0A, "rorv": 0x0B,
}

func encodeDataProc2(mnem string, ops []string) (uint32, error) {
	opcode, ok := dp2Op[mnem]
	if !ok {
		return 0, fmt.Errorf("unknown 2-source op %q", mnem)
	}
	if len(ops) != 3 {
		return 0, fmt.Errorf("%s expects 3 operands", mnem)
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	rm, ok3 := parseReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad register operand")
	}
	return sfBit(rd) | 0x1AC00000 | rm.num<<16 | opcode<<10 | rn.num<<5 | rd.num, nil
}

func encodeDataProc1(mnem string, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("%s expects 2 operands", mnem)
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad register operand")
	}
	var opcode uint32
	switch mnem {
	case "rbit":
		opcode = 0x00
	case "rev16":
		opcode = 0x01
	case "rev32":
		if !rd.is64 {
			return 0, fmt.Errorf("rev32 is 64-bit only")
		}
		opcode = 0x02
	case "rev":
		// rev's opcode encodes the width: 0b10 for 32-bit, 0b11 for 64-bit.
		if rd.is64 {
			opcode = 0x03
		} else {
			opcode = 0x02
		}
	case "clz":
		opcode = 0x04
	case "cls":
		opcode = 0x05
	default:
		return 0, fmt.Errorf("unknown 1-source op %q", mnem)
	}
	return sfBit(rd) | 0x5AC00000 | opcode<<10 | rn.num<<5 | rd.num, nil
}
