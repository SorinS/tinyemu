package riscv

import (
	"fmt"
	"strings"
)

// The F (single-precision) and D (double-precision) floating-point extensions.
// FP arithmetic is R-type at opcode 0x53 (OP-FP): funct7 selects the operation
// and precision (the .d variant is the .s funct7 with bit 25 set), funct3 is
// either the rounding mode (default "dyn" = 0b111) or a fixed selector, and the
// register classes (FP vs integer) vary per instruction. Loads/stores use
// dedicated opcodes; the fused multiply-add family is R4-type.

// fpAbi maps an f-register number to its ABI name.
var fpAbi = [32]string{
	"ft0", "ft1", "ft2", "ft3", "ft4", "ft5", "ft6", "ft7",
	"fs0", "fs1", "fa0", "fa1", "fa2", "fa3", "fa4", "fa5",
	"fa6", "fa7", "fs2", "fs3", "fs4", "fs5", "fs6", "fs7",
	"fs8", "fs9", "fs10", "fs11", "ft8", "ft9", "ft10", "ft11",
}

var fpRegByName = func() map[string]int {
	m := map[string]int{}
	for i := 0; i < 32; i++ {
		m[fmt.Sprintf("f%d", i)] = i
		m[fpAbi[i]] = i
	}
	return m
}()

func fpReg(s string) (int, error) {
	if r, ok := fpRegByName[strings.TrimSpace(strings.ToLower(s))]; ok {
		return r, nil
	}
	return 0, fmt.Errorf("bad FP register %q", s)
}

// roundingModes maps the static rounding-mode mnemonics to their funct3 value.
var roundingModes = map[string]uint32{
	"rne": 0, "rtz": 1, "rdn": 2, "rup": 3, "rmm": 4, "dyn": 7,
}

// fpForm is the operand shape (and register classes) of an FP instruction.
type fpForm int

const (
	fpLoad  fpForm = iota // rd(F), offset(rs1 int)
	fpStore               // rs2(F), offset(rs1 int)
	fpFFF                 // rd F, rs1 F, rs2 F
	fpFF                  // rd F, rs1 F        (rs2 fixed)
	fpIFF                 // rd int, rs1 F, rs2 F
	fpIF                  // rd int, rs1 F      (rs2 fixed)
	fpFI                  // rd F, rs1 int      (rs2 fixed)
	fpR4                  // rd F, rs1 F, rs2 F, rs3 F (fused multiply-add)
)

type fpInsn struct {
	name   string
	form   fpForm
	opcode uint32
	funct7 uint32 // [31:25]; for fpR4 the low bit is the fmt (0=S, 1=D)
	funct3 uint32 // used when !hasRM
	hasRM  bool   // funct3 = rounding mode (default dyn)
	rs2fix int    // rs2 field value when rs2 is not an operand
}

var fpTable = []fpInsn{
	// loads / stores
	{"flw", fpLoad, 0x07, 0, 2, false, -1}, {"fld", fpLoad, 0x07, 0, 3, false, -1},
	{"fsw", fpStore, 0x27, 0, 2, false, -1}, {"fsd", fpStore, 0x27, 0, 3, false, -1},
	// single precision
	{"fadd.s", fpFFF, 0x53, 0x00, 0, true, -1}, {"fsub.s", fpFFF, 0x53, 0x04, 0, true, -1},
	{"fmul.s", fpFFF, 0x53, 0x08, 0, true, -1}, {"fdiv.s", fpFFF, 0x53, 0x0C, 0, true, -1},
	{"fsqrt.s", fpFF, 0x53, 0x2C, 0, true, 0},
	{"fmin.s", fpFFF, 0x53, 0x14, 0, false, -1}, {"fmax.s", fpFFF, 0x53, 0x14, 1, false, -1},
	{"fsgnj.s", fpFFF, 0x53, 0x10, 0, false, -1}, {"fsgnjn.s", fpFFF, 0x53, 0x10, 1, false, -1},
	{"fsgnjx.s", fpFFF, 0x53, 0x10, 2, false, -1},
	{"feq.s", fpIFF, 0x53, 0x50, 2, false, -1}, {"flt.s", fpIFF, 0x53, 0x50, 1, false, -1},
	{"fle.s", fpIFF, 0x53, 0x50, 0, false, -1},
	{"fclass.s", fpIF, 0x53, 0x70, 1, false, 0}, {"fmv.x.w", fpIF, 0x53, 0x70, 0, false, 0},
	{"fmv.w.x", fpFI, 0x53, 0x78, 0, false, 0},
	{"fcvt.w.s", fpIF, 0x53, 0x60, 0, true, 0}, {"fcvt.wu.s", fpIF, 0x53, 0x60, 0, true, 1},
	{"fcvt.l.s", fpIF, 0x53, 0x60, 0, true, 2}, {"fcvt.lu.s", fpIF, 0x53, 0x60, 0, true, 3},
	{"fcvt.s.w", fpFI, 0x53, 0x68, 0, true, 0}, {"fcvt.s.wu", fpFI, 0x53, 0x68, 0, true, 1},
	{"fcvt.s.l", fpFI, 0x53, 0x68, 0, true, 2}, {"fcvt.s.lu", fpFI, 0x53, 0x68, 0, true, 3},
	// double precision (funct7 = single | 1)
	{"fadd.d", fpFFF, 0x53, 0x01, 0, true, -1}, {"fsub.d", fpFFF, 0x53, 0x05, 0, true, -1},
	{"fmul.d", fpFFF, 0x53, 0x09, 0, true, -1}, {"fdiv.d", fpFFF, 0x53, 0x0D, 0, true, -1},
	{"fsqrt.d", fpFF, 0x53, 0x2D, 0, true, 0},
	{"fmin.d", fpFFF, 0x53, 0x15, 0, false, -1}, {"fmax.d", fpFFF, 0x53, 0x15, 1, false, -1},
	{"fsgnj.d", fpFFF, 0x53, 0x11, 0, false, -1}, {"fsgnjn.d", fpFFF, 0x53, 0x11, 1, false, -1},
	{"fsgnjx.d", fpFFF, 0x53, 0x11, 2, false, -1},
	{"feq.d", fpIFF, 0x53, 0x51, 2, false, -1}, {"flt.d", fpIFF, 0x53, 0x51, 1, false, -1},
	{"fle.d", fpIFF, 0x53, 0x51, 0, false, -1},
	{"fclass.d", fpIF, 0x53, 0x71, 1, false, 0}, {"fmv.x.d", fpIF, 0x53, 0x71, 0, false, 0},
	{"fmv.d.x", fpFI, 0x53, 0x79, 0, false, 0},
	{"fcvt.w.d", fpIF, 0x53, 0x61, 0, true, 0}, {"fcvt.wu.d", fpIF, 0x53, 0x61, 0, true, 1},
	{"fcvt.l.d", fpIF, 0x53, 0x61, 0, true, 2}, {"fcvt.lu.d", fpIF, 0x53, 0x61, 0, true, 3},
	{"fcvt.d.w", fpFI, 0x53, 0x69, 0, false, 0}, {"fcvt.d.wu", fpFI, 0x53, 0x69, 0, false, 1},
	{"fcvt.d.l", fpFI, 0x53, 0x69, 0, true, 2}, {"fcvt.d.lu", fpFI, 0x53, 0x69, 0, true, 3},
	{"fcvt.s.d", fpFF, 0x53, 0x20, 0, true, 1}, {"fcvt.d.s", fpFF, 0x53, 0x21, 0, false, 0},
	// fused multiply-add (R4-type); funct7 low bit = fmt (0=S, 1=D)
	{"fmadd.s", fpR4, 0x43, 0, 0, true, -1}, {"fmsub.s", fpR4, 0x47, 0, 0, true, -1},
	{"fnmsub.s", fpR4, 0x4B, 0, 0, true, -1}, {"fnmadd.s", fpR4, 0x4F, 0, 0, true, -1},
	{"fmadd.d", fpR4, 0x43, 1, 0, true, -1}, {"fmsub.d", fpR4, 0x47, 1, 0, true, -1},
	{"fnmsub.d", fpR4, 0x4B, 1, 0, true, -1}, {"fnmadd.d", fpR4, 0x4F, 1, 0, true, -1},
}

var fpByName = func() map[string]*fpInsn {
	m := map[string]*fpInsn{}
	for i := range fpTable {
		m[fpTable[i].name] = &fpTable[i]
	}
	return m
}()

// regClass resolves s as a float register if isFP, else an integer register.
func regClass(s string, isFP bool) (uint32, error) {
	if isFP {
		r, err := fpReg(s)
		return u(r), err
	}
	r, err := reg(s)
	return u(r), err
}

// encodeFP encodes a floating-point instruction.
func encodeFP(in *fpInsn, ops []string) (uint32, error) {
	switch in.form {
	case fpLoad:
		if len(ops) != 2 {
			return 0, fmt.Errorf("%s: want rd, offset(rs1)", in.name)
		}
		rd, err := fpReg(ops[0])
		if err != nil {
			return 0, err
		}
		off, base, err := parseMem(ops[1])
		if err != nil {
			return 0, err
		}
		if !fits(off, 12) {
			return 0, fmt.Errorf("offset %d out of 12-bit range", off)
		}
		return in.opcode | uint32(off&0xFFF)<<20 | u(base)<<15 | in.funct3<<12 | u(rd)<<7, nil

	case fpStore:
		if len(ops) != 2 {
			return 0, fmt.Errorf("%s: want rs2, offset(rs1)", in.name)
		}
		rs2, err := fpReg(ops[0])
		if err != nil {
			return 0, err
		}
		off, base, err := parseMem(ops[1])
		if err != nil {
			return 0, err
		}
		if !fits(off, 12) {
			return 0, fmt.Errorf("offset %d out of 12-bit range", off)
		}
		hi := uint32(off>>5) & 0x7F
		lo := uint32(off) & 0x1F
		return in.opcode | hi<<25 | u(rs2)<<20 | u(base)<<15 | in.funct3<<12 | lo<<7, nil

	case fpR4:
		return encodeFPR4(in, ops)

	default:
		return encodeFPR(in, ops)
	}
}

// encodeFPR encodes the OP-FP R-type forms (everything but loads/stores/R4).
func encodeFPR(in *fpInsn, ops []string) (uint32, error) {
	var rdFP, rs1FP, rs2FP, threeReg bool
	switch in.form {
	case fpFFF:
		rdFP, rs1FP, rs2FP, threeReg = true, true, true, true
	case fpFF:
		rdFP, rs1FP = true, true
	case fpIFF:
		rs1FP, rs2FP, threeReg = true, true, true
	case fpIF:
		rs1FP = true
	case fpFI:
		rdFP = true
	}
	nReg := 2
	if threeReg {
		nReg = 3
	}
	if len(ops) < nReg {
		return 0, fmt.Errorf("%s: too few operands", in.name)
	}
	rd, err := regClass(ops[0], rdFP)
	if err != nil {
		return 0, err
	}
	rs1, err := regClass(ops[1], rs1FP)
	if err != nil {
		return 0, err
	}
	rs2 := uint32(in.rs2fix) & 0x1F
	idx := 2
	if threeReg {
		if rs2, err = regClass(ops[2], rs2FP); err != nil {
			return 0, err
		}
		idx = 3
	}
	funct3 := in.funct3
	if in.hasRM {
		funct3 = 7 // dyn
		if idx < len(ops) {
			rm, ok := roundingModes[strings.ToLower(strings.TrimSpace(ops[idx]))]
			if !ok {
				return 0, fmt.Errorf("%s: bad rounding mode %q", in.name, ops[idx])
			}
			funct3 = rm
			idx++
		}
	}
	if idx != len(ops) {
		return 0, fmt.Errorf("%s: too many operands", in.name)
	}
	return in.opcode | in.funct7<<25 | rs2<<20 | rs1<<15 | funct3<<12 | rd<<7, nil
}

// encodeFPR4 encodes the fused multiply-add R4-type forms.
func encodeFPR4(in *fpInsn, ops []string) (uint32, error) {
	if len(ops) < 4 {
		return 0, fmt.Errorf("%s: want rd, rs1, rs2, rs3", in.name)
	}
	rd, err := fpReg(ops[0])
	if err != nil {
		return 0, err
	}
	rs1, err := fpReg(ops[1])
	if err != nil {
		return 0, err
	}
	rs2, err := fpReg(ops[2])
	if err != nil {
		return 0, err
	}
	rs3, err := fpReg(ops[3])
	if err != nil {
		return 0, err
	}
	funct3 := uint32(7) // dyn
	if len(ops) == 5 {
		rm, ok := roundingModes[strings.ToLower(strings.TrimSpace(ops[4]))]
		if !ok {
			return 0, fmt.Errorf("%s: bad rounding mode %q", in.name, ops[4])
		}
		funct3 = rm
	} else if len(ops) > 5 {
		return 0, fmt.Errorf("%s: too many operands", in.name)
	}
	fmtBit := in.funct7 & 1
	return in.opcode | u(rs3)<<27 | fmtBit<<25 | u(rs2)<<20 | u(rs1)<<15 | funct3<<12 | u(rd)<<7, nil
}
