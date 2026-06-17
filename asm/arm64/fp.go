package arm64

import (
	"fmt"
	"strconv"
	"strings"
)

// Scalar floating-point: the SIMD&FP register file (B/H/S/D/Q views of V0–V31)
// and scalar single/double arithmetic. Vector (arrangement-specifier) SIMD is a
// later slice. Byte-exactness is checked against llvm-mc; execution against
// native Apple Silicon.

// fpReg is a parsed scalar FP/SIMD register: number 0..31 and width in bits
// (8=b, 16=h, 32=s, 64=d, 128=q).
type fpReg struct {
	num  uint32
	size int
}

func parseFPReg(s string) (fpReg, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 {
		return fpReg{}, false
	}
	var size int
	switch s[0] {
	case 'b':
		size = 8
	case 'h':
		size = 16
	case 's':
		size = 32
	case 'd':
		size = 64
	case 'q':
		size = 128
	default:
		return fpReg{}, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 0 || n > 31 {
		return fpReg{}, false
	}
	return fpReg{uint32(n), size}, true
}

// fpType returns the 2-bit ftype field for a scalar FP width: S=00, D=01, H=11.
func fpType(size int) (uint32, bool) {
	switch size {
	case 32:
		return 0b00, true
	case 64:
		return 0b01, true
	case 16:
		return 0b11, true
	}
	return 0, false
}

// fpArith2 maps a 2-source scalar-FP mnemonic to its opcode (bits[15:12]).
var fpArith2 = map[string]uint32{
	"fmul": 0x0, "fdiv": 0x1, "fadd": 0x2, "fsub": 0x3,
	"fmax": 0x4, "fmin": 0x5, "fmaxnm": 0x6, "fminnm": 0x7, "fnmul": 0x8,
}

// fpArith1 maps a 1-source scalar-FP mnemonic to its opcode (bits[20:15]).
var fpArith1 = map[string]uint32{
	"fmov": 0x0, "fabs": 0x1, "fneg": 0x2, "fsqrt": 0x3,
}

func encodeFP(mnem string, ops []string) (uint32, error) {
	if op, ok := fpArith2[mnem]; ok {
		return encodeFPArith2(op, ops)
	}
	if mnem == "fmov" {
		return encodeFMov(ops)
	}
	if op, ok := fpArith1[mnem]; ok {
		return encodeFPArith1(op, ops)
	}
	switch mnem {
	case "fcvt":
		return encodeFcvt(ops)
	case "scvtf", "ucvtf", "fcvtzs", "fcvtzu":
		return encodeFcvtInt(mnem, ops)
	case "fcmp", "fcmpe":
		return encodeFcmp(mnem, ops)
	case "fcsel":
		return encodeFcsel(ops)
	}
	return 0, fmt.Errorf("unsupported scalar-FP op %q", mnem)
}

// encodeFcvt encodes fcvt (floating-point precision conversion): a 1-source op
// whose opcode encodes the *destination* precision and whose ftype is the
// *source* precision.
func encodeFcvt(ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("fcvt expects Rd, Rn")
	}
	rd, ok1 := parseFPReg(ops[0])
	rn, ok2 := parseFPReg(ops[1])
	if !ok1 || !ok2 || rd.size == rn.size {
		return 0, fmt.Errorf("bad fcvt operands")
	}
	srcType, ok := fpType(rn.size)
	if !ok {
		return 0, fmt.Errorf("bad fcvt source size")
	}
	// opcode = 0b0001 << 2 | dstType, with dstType S=00, D=01, H=11.
	dstType, ok := fpType(rd.size)
	if !ok {
		return 0, fmt.Errorf("bad fcvt dest size")
	}
	opcode := uint32(0b000100) | dstType
	return 0x1E204000 | srcType<<22 | opcode<<15 | rn.num<<5 | rd.num, nil
}

// fcvtIntForm gives the {opcode, rmode} for an int↔FP conversion.
var fcvtIntForm = map[string][2]uint32{
	"scvtf":  {0b010, 0b00}, // signed int → FP
	"ucvtf":  {0b011, 0b00}, // unsigned int → FP
	"fcvtzs": {0b000, 0b11}, // FP → signed int, round toward zero (rmode=11)
	"fcvtzu": {0b001, 0b11}, // FP → unsigned int, round toward zero
}

// encodeFcvtInt encodes the int↔FP conversions. scvtf/ucvtf take (FPd, GPRn);
// fcvtzs/fcvtzu take (GPRd, FPn).
func encodeFcvtInt(mnem string, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("%s expects two operands", mnem)
	}
	f := fcvtIntForm[mnem]
	opcode, rmode := f[0], f[1]
	toFP := mnem == "scvtf" || mnem == "ucvtf"
	var fp fpReg
	var g reg
	if toFP {
		var ok1, ok2 bool
		fp, ok1 = parseFPReg(ops[0])
		g, ok2 = parseReg(ops[1])
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("bad %s operands", mnem)
		}
	} else {
		var ok1, ok2 bool
		g, ok1 = parseReg(ops[0])
		fp, ok2 = parseFPReg(ops[1])
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("bad %s operands", mnem)
		}
	}
	ftype, ok := fpType(fp.size)
	if !ok {
		return 0, fmt.Errorf("bad FP size for %s", mnem)
	}
	sf := uint32(0)
	if g.is64 {
		sf = 1
	}
	rn, rd := fp.num, g.num
	if toFP {
		rn, rd = g.num, fp.num
	}
	return sf<<31 | 0x1E000000 | ftype<<22 | 1<<21 | rmode<<19 | opcode<<16 | rn<<5 | rd, nil
}

// encodeFcmp encodes fcmp/fcmpe (sets NZCV from a scalar FP compare). The
// second operand may be another FP register or the literal #0.0.
func encodeFcmp(mnem string, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("%s expects two operands", mnem)
	}
	rn, ok := parseFPReg(ops[0])
	if !ok {
		return 0, fmt.Errorf("bad FP register")
	}
	ftype, ok := fpType(rn.size)
	if !ok {
		return 0, fmt.Errorf("bad FP size")
	}
	// opcode2[4:0]: cmp=00000 / cmp #0.0=01000 / cmpe=10000 / cmpe #0.0=11000.
	var opcode2, rm uint32
	if mnem == "fcmpe" {
		opcode2 = 0b10000
	}
	z := strings.TrimSpace(ops[1])
	if z == "#0.0" || z == "#0" || z == "0.0" {
		opcode2 |= 0b01000 // compare with zero (Rm field is 0)
	} else {
		r, ok := parseFPReg(z)
		if !ok || r.size != rn.size {
			return 0, fmt.Errorf("bad fcmp comparand")
		}
		rm = r.num
	}
	return 0x1E202000 | ftype<<22 | rm<<16 | rn.num<<5 | opcode2, nil
}

// encodeFcsel encodes fcsel Rd, Rn, Rm, cond.
func encodeFcsel(ops []string) (uint32, error) {
	if len(ops) != 4 {
		return 0, fmt.Errorf("fcsel expects Rd, Rn, Rm, cond")
	}
	rd, ok1 := parseFPReg(ops[0])
	rn, ok2 := parseFPReg(ops[1])
	rm, ok3 := parseFPReg(ops[2])
	cond, ok4 := condCodes[normCond(ops[3])]
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return 0, fmt.Errorf("bad fcsel operands")
	}
	ftype, ok := fpType(rd.size)
	if !ok || rd.size != rn.size || rd.size != rm.size {
		return 0, fmt.Errorf("fcsel size mismatch")
	}
	return 0x1E200C00 | ftype<<22 | rm.num<<16 | cond<<12 | rn.num<<5 | rd.num, nil
}

// encodeFPArith2 encodes a 2-source scalar-FP instruction (fadd/fmul/…).
func encodeFPArith2(opcode uint32, ops []string) (uint32, error) {
	if len(ops) != 3 {
		return 0, fmt.Errorf("expected Rd, Rn, Rm")
	}
	rd, ok1 := parseFPReg(ops[0])
	rn, ok2 := parseFPReg(ops[1])
	rm, ok3 := parseFPReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad FP register")
	}
	ftype, ok := fpType(rd.size)
	if !ok || rd.size != rn.size || rd.size != rm.size {
		return 0, fmt.Errorf("FP operand size mismatch")
	}
	return 0x1E200000 | ftype<<22 | rm.num<<16 | opcode<<12 | 0b10<<10 | rn.num<<5 | rd.num, nil
}

// encodeFPArith1 encodes a 1-source scalar-FP instruction (fabs/fneg/fsqrt).
func encodeFPArith1(opcode uint32, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("expected Rd, Rn")
	}
	rd, ok1 := parseFPReg(ops[0])
	rn, ok2 := parseFPReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad FP register")
	}
	ftype, ok := fpType(rd.size)
	if !ok || rd.size != rn.size {
		return 0, fmt.Errorf("FP operand size mismatch")
	}
	return 0x1E204000 | ftype<<22 | opcode<<15 | rn.num<<5 | rd.num, nil
}

// encodeFMov handles the fmov forms: reg-reg (1-source), and FP↔GPR moves.
// The 8-bit-immediate form is a later step.
func encodeFMov(ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("fmov expects two operands")
	}
	rd, rdFP := parseFPReg(ops[0])
	rn, rnFP := parseFPReg(ops[1])
	switch {
	case rdFP && rnFP: // fmov Dd, Dn — 1-source opcode 0
		ftype, ok := fpType(rd.size)
		if !ok || rd.size != rn.size {
			return 0, fmt.Errorf("fmov reg size mismatch")
		}
		return 0x1E204000 | ftype<<22 | rn.num<<5 | rd.num, nil
	case rdFP: // fmov Sd, Wn / fmov Dd, Xn — GPR → FP (opcode 111)
		g, ok := parseReg(ops[1])
		if !ok {
			return 0, fmt.Errorf("bad GPR operand")
		}
		ftype, sf, ok := fpMovGPR(rd.size, g.is64)
		if !ok {
			return 0, fmt.Errorf("fmov GPR/FP width mismatch")
		}
		return sf<<31 | 0x1E000000 | ftype<<22 | 1<<21 | 7<<16 | g.num<<5 | rd.num, nil
	case rnFP: // fmov Wd, Sn / fmov Xd, Dn — FP → GPR (opcode 110)
		g, ok := parseReg(ops[0])
		if !ok {
			return 0, fmt.Errorf("bad GPR operand")
		}
		ftype, sf, ok := fpMovGPR(rn.size, g.is64)
		if !ok {
			return 0, fmt.Errorf("fmov GPR/FP width mismatch")
		}
		return sf<<31 | 0x1E000000 | ftype<<22 | 1<<21 | 6<<16 | rn.num<<5 | g.num, nil
	}
	return 0, fmt.Errorf("fmov immediate not yet supported")
}

// fpMovGPR returns the ftype and sf bit for an fmov between a scalar FP reg of
// the given size and a GPR: S↔W (32-bit) or D↔X (64-bit).
func fpMovGPR(fpSize int, gpr64 bool) (ftype, sf uint32, ok bool) {
	if fpSize == 32 && !gpr64 {
		return 0b00, 0, true
	}
	if fpSize == 64 && gpr64 {
		return 0b01, 1, true
	}
	return 0, 0, false
}
