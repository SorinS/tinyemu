package riscv

import (
	"fmt"
	"strings"
)

// The C (compressed) extension: 16-bit forms of common instructions, in three
// quadrants selected by the low two bits. Each instruction has its own
// immediate bit-permutation (verified byte-for-byte against llvm-mc). The
// "compressed" register fields address only x8–x15 (a 3-bit field = reg − 8).

// cReg3 resolves a register that must be x8–x15 to its 3-bit compressed field.
func cReg3(s string) (uint32, error) {
	r, err := reg(s)
	if err != nil {
		return 0, err
	}
	if r < 8 || r > 15 {
		return 0, fmt.Errorf("%q is not a compressed register (must be x8–x15)", s)
	}
	return uint32(r - 8), nil
}

// cFReg3 is cReg3 for floating-point registers (f8–f15).
func cFReg3(s string) (uint32, error) {
	r, err := fpReg(s)
	if err != nil {
		return 0, err
	}
	if r < 8 || r > 15 {
		return 0, fmt.Errorf("%q is not a compressed FP register (must be f8–f15)", s)
	}
	return uint32(r - 8), nil
}

// isCompressed reports whether a mnemonic is a C-extension instruction.
func isCompressed(mnem string) bool { return strings.HasPrefix(mnem, "c.") }

// cMnemonics lists the compressed instructions (for editor completion).
var cMnemonics = []string{
	"c.nop", "c.ebreak", "c.mv", "c.add", "c.jr", "c.jalr",
	"c.addi", "c.addiw", "c.li", "c.lui", "c.addi16sp", "c.addi4spn", "c.slli",
	"c.lwsp", "c.ldsp", "c.fldsp", "c.swsp", "c.sdsp", "c.fsdsp",
	"c.lw", "c.ld", "c.fld", "c.sw", "c.sd", "c.fsd",
	"c.srli", "c.srai", "c.andi", "c.sub", "c.xor", "c.or", "c.and", "c.subw", "c.addw",
	"c.j", "c.beqz", "c.bnez",
}

func cFull(s string) (uint32, error) {
	r, err := reg(s)
	return u(r), err
}

// encodeC encodes a compressed (16-bit) instruction.
func encodeC(mnem string, ops []string) (uint32, error) {
	switch mnem {
	case "c.nop":
		return 0x0001, nil
	case "c.ebreak":
		return 0x9002, nil

	// --- CR (register) : funct4 | rd/rs1 | rs2 | op=10 ---
	case "c.mv", "c.add":
		rd, rs2, err := twoFull(ops)
		if err != nil {
			return 0, err
		}
		f4 := uint32(0x8) // c.mv
		if mnem == "c.add" {
			f4 = 0x9
		}
		return f4<<12 | rd<<7 | rs2<<2 | 0b10, nil
	case "c.jr", "c.jalr":
		if len(ops) != 1 {
			return 0, fmt.Errorf("%s: want rs1", mnem)
		}
		rs1, err := cFull(ops[0])
		if err != nil {
			return 0, err
		}
		f4 := uint32(0x8) // c.jr
		if mnem == "c.jalr" {
			f4 = 0x9
		}
		return f4<<12 | rs1<<7 | 0b10, nil

	// --- CI (immediate), op=01 ---
	case "c.addi", "c.addiw", "c.li":
		rd, imm, err := fullImm(ops)
		if err != nil {
			return 0, err
		}
		f3 := map[string]uint32{"c.addi": 0, "c.addiw": 1, "c.li": 2}[mnem]
		return f3<<13 | bit(imm, 5)<<12 | rd<<7 | bits(imm, 4, 0)<<2 | 0b01, nil
	case "c.lui":
		rd, imm, err := fullImm(ops) // operand is the 6-bit nzimm field directly
		if err != nil {
			return 0, err
		}
		return 0b011<<13 | bit(imm, 5)<<12 | rd<<7 | bits(imm, 4, 0)<<2 | 0b01, nil
	case "c.addi16sp":
		if len(ops) != 2 {
			return 0, fmt.Errorf("c.addi16sp: want sp, imm")
		}
		imm, err := parseImm(ops[1])
		if err != nil {
			return 0, err
		}
		return 0b011<<13 | bit(imm, 9)<<12 | 2<<7 |
			bit(imm, 4)<<6 | bit(imm, 6)<<5 | bits(imm, 8, 7)<<3 | bit(imm, 5)<<2 | 0b01, nil
	case "c.addi4spn":
		if len(ops) != 3 {
			return 0, fmt.Errorf("c.addi4spn: want rd', sp, imm")
		}
		rd, err := cReg3(ops[0])
		if err != nil {
			return 0, err
		}
		imm, err := parseImm(ops[2])
		if err != nil {
			return 0, err
		}
		return bits(imm, 5, 4)<<11 | bits(imm, 9, 6)<<7 | bit(imm, 2)<<6 | bit(imm, 3)<<5 | rd<<2, nil

	// --- CI (Q2 stack-relative loads + c.slli) ---
	case "c.slli":
		rd, imm, err := fullImm(ops)
		if err != nil {
			return 0, err
		}
		return 0b000<<13 | bit(imm, 5)<<12 | rd<<7 | bits(imm, 4, 0)<<2 | 0b10, nil
	case "c.lwsp", "c.ldsp", "c.fldsp":
		rd, off, err := fullMem(ops, mnem == "c.fldsp")
		if err != nil {
			return 0, err
		}
		switch mnem {
		case "c.lwsp":
			return 0b010<<13 | bit(off, 5)<<12 | rd<<7 | bits(off, 4, 2)<<4 | bits(off, 7, 6)<<2 | 0b10, nil
		default: // c.ldsp / c.fldsp (8-byte)
			f3 := uint32(0b011)
			if mnem == "c.fldsp" {
				f3 = 0b001
			}
			return f3<<13 | bit(off, 5)<<12 | rd<<7 | bits(off, 4, 3)<<5 | bits(off, 8, 6)<<2 | 0b10, nil
		}
	case "c.swsp", "c.sdsp", "c.fsdsp":
		rs2, off, err := fullMem(ops, mnem == "c.fsdsp")
		if err != nil {
			return 0, err
		}
		if mnem == "c.swsp" {
			return 0b110<<13 | bits(off, 5, 2)<<9 | bits(off, 7, 6)<<7 | rs2<<2 | 0b10, nil
		}
		f3 := uint32(0b111)
		if mnem == "c.fsdsp" {
			f3 = 0b101
		}
		return f3<<13 | bits(off, 5, 3)<<10 | bits(off, 8, 6)<<7 | rs2<<2 | 0b10, nil

	// --- CL / CS (Q0 reg-relative loads/stores) ---
	case "c.lw", "c.ld", "c.fld":
		rd, off, rs1, err := cMem(ops, mnem == "c.fld")
		if err != nil {
			return 0, err
		}
		if mnem == "c.lw" {
			return 0b010<<13 | bits(off, 5, 3)<<10 | rs1<<7 | bit(off, 2)<<6 | bit(off, 6)<<5 | rd<<2, nil
		}
		f3 := uint32(0b011)
		if mnem == "c.fld" {
			f3 = 0b001
		}
		return f3<<13 | bits(off, 5, 3)<<10 | rs1<<7 | bits(off, 7, 6)<<5 | rd<<2, nil
	case "c.sw", "c.sd", "c.fsd":
		rs2, off, rs1, err := cMem(ops, mnem == "c.fsd")
		if err != nil {
			return 0, err
		}
		if mnem == "c.sw" {
			return 0b110<<13 | bits(off, 5, 3)<<10 | rs1<<7 | bit(off, 2)<<6 | bit(off, 6)<<5 | rs2<<2, nil
		}
		f3 := uint32(0b111)
		if mnem == "c.fsd" {
			f3 = 0b101
		}
		return f3<<13 | bits(off, 5, 3)<<10 | rs1<<7 | bits(off, 7, 6)<<5 | rs2<<2, nil

	// --- CB-immediate (c.srli/c.srai/c.andi), op=01 ---
	case "c.srli", "c.srai", "c.andi":
		rd, imm, err := cRegImm(ops)
		if err != nil {
			return 0, err
		}
		f2 := map[string]uint32{"c.srli": 0, "c.srai": 1, "c.andi": 2}[mnem]
		return 0b100<<13 | bit(imm, 5)<<12 | f2<<10 | rd<<7 | bits(imm, 4, 0)<<2 | 0b01, nil

	// --- CA (register-register), op=01 ---
	case "c.sub", "c.xor", "c.or", "c.and", "c.subw", "c.addw":
		rd, rs2, err := twoCReg(ops)
		if err != nil {
			return 0, err
		}
		f6, f2 := uint32(0b100011), uint32(0)
		switch mnem {
		case "c.sub":
			f2 = 0
		case "c.xor":
			f2 = 1
		case "c.or":
			f2 = 2
		case "c.and":
			f2 = 3
		case "c.subw":
			f6, f2 = 0b100111, 0
		case "c.addw":
			f6, f2 = 0b100111, 1
		}
		return f6<<10 | rd<<7 | f2<<5 | rs2<<2 | 0b01, nil

	// --- CJ / CB (branches), op=01 ---
	case "c.j":
		if len(ops) != 1 {
			return 0, fmt.Errorf("c.j: want offset")
		}
		off, err := parseImm(ops[0])
		if err != nil {
			return 0, err
		}
		return 0b101<<13 | cjImm(off) | 0b01, nil
	case "c.beqz", "c.bnez":
		if len(ops) != 2 {
			return 0, fmt.Errorf("%s: want rs1', offset", mnem)
		}
		rs1, err := cReg3(ops[0])
		if err != nil {
			return 0, err
		}
		off, err := parseImm(ops[1])
		if err != nil {
			return 0, err
		}
		f3 := uint32(0b110)
		if mnem == "c.bnez" {
			f3 = 0b111
		}
		return f3<<13 | bit(off, 8)<<12 | bits(off, 4, 3)<<10 | rs1<<7 |
			bits(off, 7, 6)<<5 | bits(off, 2, 1)<<3 | bit(off, 5)<<2 | 0b01, nil
	}
	return 0, fmt.Errorf("riscv: unknown compressed instruction %q", mnem)
}

func cbit(w uint16, n uint) uint32       { return uint32(w>>n) & 1 }
func cbits(w uint16, hi, lo uint) uint32 { return uint32(w>>lo) & ((1 << (hi - lo + 1)) - 1) }

// disasmC decodes a 16-bit compressed instruction.
func disasmC(w uint16) (string, error) {
	op := w & 3
	f3 := uint32(w>>13) & 7
	rdFull, rs2Full := cbits(w, 11, 7), cbits(w, 6, 2)
	rd3, rs1_3, rs2_3 := cbits(w, 4, 2)+8, cbits(w, 9, 7)+8, cbits(w, 4, 2)+8
	ci6 := func() int64 { return signExtend(cbit(w, 12)<<5|cbits(w, 6, 2), 6) }
	shamt := func() uint32 { return cbit(w, 12)<<5 | cbits(w, 6, 2) }

	switch op {
	case 0: // quadrant 0
		switch f3 {
		case 0:
			nz := cbits(w, 12, 11)<<4 | cbits(w, 10, 7)<<6 | cbit(w, 6)<<2 | cbit(w, 5)<<3
			if nz == 0 {
				break
			}
			return fmt.Sprintf("c.addi4spn %s, sp, %d", xreg(rd3), nz), nil
		case 1:
			off := cbits(w, 12, 10)<<3 | cbits(w, 6, 5)<<6
			return fmt.Sprintf("c.fld %s, %d(%s)", freg(rd3), off, xreg(rs1_3)), nil
		case 2:
			off := cbits(w, 12, 10)<<3 | cbit(w, 6)<<2 | cbit(w, 5)<<6
			return fmt.Sprintf("c.lw %s, %d(%s)", xreg(rd3), off, xreg(rs1_3)), nil
		case 3:
			off := cbits(w, 12, 10)<<3 | cbits(w, 6, 5)<<6
			return fmt.Sprintf("c.ld %s, %d(%s)", xreg(rd3), off, xreg(rs1_3)), nil
		case 5:
			off := cbits(w, 12, 10)<<3 | cbits(w, 6, 5)<<6
			return fmt.Sprintf("c.fsd %s, %d(%s)", freg(rs2_3), off, xreg(rs1_3)), nil
		case 6:
			off := cbits(w, 12, 10)<<3 | cbit(w, 6)<<2 | cbit(w, 5)<<6
			return fmt.Sprintf("c.sw %s, %d(%s)", xreg(rs2_3), off, xreg(rs1_3)), nil
		case 7:
			off := cbits(w, 12, 10)<<3 | cbits(w, 6, 5)<<6
			return fmt.Sprintf("c.sd %s, %d(%s)", xreg(rs2_3), off, xreg(rs1_3)), nil
		}
	case 1: // quadrant 1
		switch f3 {
		case 0:
			if rdFull == 0 && ci6() == 0 {
				return "c.nop", nil
			}
			return fmt.Sprintf("c.addi %s, %d", xreg(rdFull), ci6()), nil
		case 1:
			return fmt.Sprintf("c.addiw %s, %d", xreg(rdFull), ci6()), nil
		case 2:
			return fmt.Sprintf("c.li %s, %d", xreg(rdFull), ci6()), nil
		case 3:
			if rdFull == 2 {
				nz := signExtend(cbit(w, 12)<<9|cbit(w, 6)<<4|cbit(w, 5)<<6|cbits(w, 4, 3)<<7|cbit(w, 2)<<5, 10)
				return fmt.Sprintf("c.addi16sp sp, %d", nz), nil
			}
			return fmt.Sprintf("c.lui %s, %d", xreg(rdFull), ci6()), nil
		case 4:
			switch cbits(w, 11, 10) {
			case 0:
				return fmt.Sprintf("c.srli %s, %d", xreg(rs1_3), shamt()), nil
			case 1:
				return fmt.Sprintf("c.srai %s, %d", xreg(rs1_3), shamt()), nil
			case 2:
				return fmt.Sprintf("c.andi %s, %d", xreg(rs1_3), ci6()), nil
			default:
				f2 := cbits(w, 6, 5)
				if cbit(w, 12) == 0 {
					return fmt.Sprintf("%s %s, %s", []string{"c.sub", "c.xor", "c.or", "c.and"}[f2], xreg(rs1_3), xreg(rs2_3)), nil
				}
				if f2 == 0 {
					return fmt.Sprintf("c.subw %s, %s", xreg(rs1_3), xreg(rs2_3)), nil
				}
				if f2 == 1 {
					return fmt.Sprintf("c.addw %s, %s", xreg(rs1_3), xreg(rs2_3)), nil
				}
			}
		case 5:
			off := signExtend(cbit(w, 12)<<11|cbit(w, 11)<<4|cbits(w, 10, 9)<<8|cbit(w, 8)<<10|
				cbit(w, 7)<<6|cbit(w, 6)<<7|cbits(w, 5, 3)<<1|cbit(w, 2)<<5, 12)
			return fmt.Sprintf("c.j %d", off), nil
		case 6, 7:
			off := signExtend(cbit(w, 12)<<8|cbits(w, 11, 10)<<3|cbits(w, 6, 5)<<6|cbits(w, 4, 3)<<1|cbit(w, 2)<<5, 9)
			name := "c.beqz"
			if f3 == 7 {
				name = "c.bnez"
			}
			return fmt.Sprintf("%s %s, %d", name, xreg(rs1_3), off), nil
		}
	case 2: // quadrant 2
		switch f3 {
		case 0:
			return fmt.Sprintf("c.slli %s, %d", xreg(rdFull), shamt()), nil
		case 1:
			off := cbit(w, 12)<<5 | cbits(w, 6, 5)<<3 | cbits(w, 4, 2)<<6
			return fmt.Sprintf("c.fldsp %s, %d(sp)", freg(rdFull), off), nil
		case 2:
			off := cbit(w, 12)<<5 | cbits(w, 6, 4)<<2 | cbits(w, 3, 2)<<6
			return fmt.Sprintf("c.lwsp %s, %d(sp)", xreg(rdFull), off), nil
		case 3:
			off := cbit(w, 12)<<5 | cbits(w, 6, 5)<<3 | cbits(w, 4, 2)<<6
			return fmt.Sprintf("c.ldsp %s, %d(sp)", xreg(rdFull), off), nil
		case 4:
			if cbit(w, 12) == 0 {
				if rs2Full == 0 {
					return fmt.Sprintf("c.jr %s", xreg(rdFull)), nil
				}
				return fmt.Sprintf("c.mv %s, %s", xreg(rdFull), xreg(rs2Full)), nil
			}
			if rdFull == 0 && rs2Full == 0 {
				return "c.ebreak", nil
			}
			if rs2Full == 0 {
				return fmt.Sprintf("c.jalr %s", xreg(rdFull)), nil
			}
			return fmt.Sprintf("c.add %s, %s", xreg(rdFull), xreg(rs2Full)), nil
		case 5:
			off := cbits(w, 12, 10)<<3 | cbits(w, 9, 7)<<6
			return fmt.Sprintf("c.fsdsp %s, %d(sp)", freg(rs2Full), off), nil
		case 6:
			off := cbits(w, 12, 9)<<2 | cbits(w, 8, 7)<<6
			return fmt.Sprintf("c.swsp %s, %d(sp)", xreg(rs2Full), off), nil
		case 7:
			off := cbits(w, 12, 10)<<3 | cbits(w, 9, 7)<<6
			return fmt.Sprintf("c.sdsp %s, %d(sp)", xreg(rs2Full), off), nil
		}
	}
	return "", fmt.Errorf("riscv: cannot decode compressed %#04x", w)
}

// cjImm packs a CJ jump offset into bits [12:2].
func cjImm(off int64) uint32 {
	return bit(off, 11)<<12 | bit(off, 4)<<11 | bits(off, 9, 8)<<9 | bit(off, 10)<<8 |
		bit(off, 6)<<7 | bit(off, 7)<<6 | bits(off, 3, 1)<<3 | bit(off, 5)<<2
}

// --- operand-shape helpers --------------------------------------------------

func twoFull(ops []string) (rd, rs2 uint32, err error) {
	if len(ops) != 2 {
		return 0, 0, fmt.Errorf("want rd, rs2")
	}
	if rd, err = cFull(ops[0]); err != nil {
		return
	}
	rs2, err = cFull(ops[1])
	return
}

func twoCReg(ops []string) (rd, rs2 uint32, err error) {
	if len(ops) != 2 {
		return 0, 0, fmt.Errorf("want rd', rs2'")
	}
	if rd, err = cReg3(ops[0]); err != nil {
		return
	}
	rs2, err = cReg3(ops[1])
	return
}

func fullImm(ops []string) (rd uint32, imm int64, err error) {
	if len(ops) != 2 {
		return 0, 0, fmt.Errorf("want rd, imm")
	}
	if rd, err = cFull(ops[0]); err != nil {
		return
	}
	imm, err = parseImm(ops[1])
	return
}

func cRegImm(ops []string) (rd uint32, imm int64, err error) {
	if len(ops) != 2 {
		return 0, 0, fmt.Errorf("want rd', imm")
	}
	if rd, err = cReg3(ops[0]); err != nil {
		return
	}
	imm, err = parseImm(ops[1])
	return
}

// fullMem parses "rd, offset(sp)" → (rd, offset). isFP picks the FP register class.
func fullMem(ops []string, isFP bool) (rd uint32, off int64, err error) {
	if len(ops) != 2 {
		return 0, 0, fmt.Errorf("want reg, offset(sp)")
	}
	if isFP {
		var r int
		if r, err = fpReg(ops[0]); err != nil {
			return
		}
		rd = u(r)
	} else if rd, err = cFull(ops[0]); err != nil {
		return
	}
	var base int
	off, base, err = parseMem(ops[1])
	if err == nil && base != 2 {
		err = fmt.Errorf("compressed stack op must use sp, got x%d", base)
	}
	return
}

// cMem parses "rd', offset(rs1')" with compressed registers.
func cMem(ops []string, isFP bool) (rd uint32, off int64, rs1 uint32, err error) {
	if len(ops) != 2 {
		return 0, 0, 0, fmt.Errorf("want reg', offset(rs1')")
	}
	if isFP {
		rd, err = cFReg3(ops[0])
	} else {
		rd, err = cReg3(ops[0])
	}
	if err != nil {
		return
	}
	var baseStr string
	off, baseStr, err = parseCMem(ops[1])
	if err != nil {
		return
	}
	rs1, err = cReg3(baseStr)
	return
}

// parseCMem splits "offset(reg)" returning the offset and the base register name.
func parseCMem(s string) (off int64, base string, err error) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return 0, "", fmt.Errorf("bad memory operand %q (want offset(reg))", s)
	}
	if o := strings.TrimSpace(s[:open]); o != "" {
		if off, err = parseImm(o); err != nil {
			return
		}
	}
	return off, s[open+1 : len(s)-1], nil
}
