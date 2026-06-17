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
	return 0, fmt.Errorf("unsupported scalar-FP op %q", mnem)
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
