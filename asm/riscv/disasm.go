package riscv

import (
	"fmt"
	"strings"
)

// xreg / freg return the ABI name for integer / float register n.
func xreg(n uint32) string { return abiNames[n&0x1F] }
func freg(n uint32) string { return fpAbi[n&0x1F] }

var rmNames = [8]string{"rne", "rtz", "rdn", "rup", "rmm", "?5", "?6", "dyn"}

// rmSuffix renders an explicit rounding mode (none for the default "dyn", so a
// round-trip re-assembles to the same bytes).
func rmSuffix(funct3 uint32) string {
	if funct3 == 7 {
		return ""
	}
	return ", " + rmNames[funct3&7]
}

// disasmFPR decodes an OP-FP (0x53) R-type instruction by reverse table lookup.
func disasmFPR(funct7, funct3, rs2, rs1, rd uint32) (string, bool) {
	for i := range fpTable {
		f := &fpTable[i]
		if f.opcode != 0x53 || f.form == fpR4 {
			continue
		}
		if f.funct7 != funct7 {
			continue
		}
		if f.rs2fix >= 0 && rs2 != uint32(f.rs2fix) {
			continue
		}
		if !f.hasRM && funct3 != f.funct3 {
			continue
		}
		rm := ""
		if f.hasRM {
			rm = rmSuffix(funct3)
		}
		switch f.form {
		case fpFFF:
			return fmt.Sprintf("%s %s, %s, %s%s", f.name, freg(rd), freg(rs1), freg(rs2), rm), true
		case fpFF:
			return fmt.Sprintf("%s %s, %s%s", f.name, freg(rd), freg(rs1), rm), true
		case fpIFF:
			return fmt.Sprintf("%s %s, %s, %s%s", f.name, xreg(rd), freg(rs1), freg(rs2), rm), true
		case fpIF:
			return fmt.Sprintf("%s %s, %s%s", f.name, xreg(rd), freg(rs1), rm), true
		case fpFI:
			return fmt.Sprintf("%s %s, %s%s", f.name, freg(rd), xreg(rs1), rm), true
		}
	}
	return "", false
}

// fenceStr renders a 4-bit fence ordering set (i=8,o=4,r=2,w=1) as flag letters.
func fenceStr(v uint32) string {
	s := ""
	if v&8 != 0 {
		s += "i"
	}
	if v&4 != 0 {
		s += "o"
	}
	if v&2 != 0 {
		s += "r"
	}
	if v&1 != 0 {
		s += "w"
	}
	return s
}

// signExtend sign-extends the low n bits of v.
func signExtend(v uint32, n uint) int64 {
	shift := 64 - n
	return int64(uint64(v)<<shift) >> shift
}

// Disassemble decodes one 32-bit RISC-V instruction (RV64I + M) into its
// assembly text. PC-relative branch/jump immediates are rendered as the
// numeric byte offset (consistent with how Assemble accepts them), so
// disassembling then re-assembling is byte-stable. Returns the text and the
// instruction length (always 4 here; compressed insns are not handled).
func Disassemble(code []byte) (text string, length int, err error) {
	if len(code) < 4 {
		return "", 0, fmt.Errorf("riscv: need 4 bytes, have %d", len(code))
	}
	w := uint32(code[0]) | uint32(code[1])<<8 | uint32(code[2])<<16 | uint32(code[3])<<24
	opcode := w & 0x7F
	funct3 := (w >> 12) & 0x7
	funct7 := (w >> 25) & 0x7F
	rd := (w >> 7) & 0x1F
	rs1 := (w >> 15) & 0x1F
	rs2 := (w >> 20) & 0x1F

	// Find the matching table entry for opcode/funct3/funct7 (funct7 only for
	// R-type; shifts match on the funct6/7 selector).
	find := func(match func(*insn) bool) *insn {
		for i := range table {
			if match(&table[i]) {
				return &table[i]
			}
		}
		return nil
	}

	switch opcode {
	case 0x33, 0x3B: // R-type (OP / OP-32)
		in := find(func(x *insn) bool {
			return x.opcode == opcode && x.format == fmtR && x.funct3 == funct3 && x.funct7 == funct7
		})
		if in == nil {
			break
		}
		return fmt.Sprintf("%s %s, %s, %s", in.name, xreg(rd), xreg(rs1), xreg(rs2)), 4, nil

	case 0x13, 0x1B: // OP-IMM / OP-IMM-32: arithmetic or shift
		if funct3 == 0x1 || funct3 == 0x5 { // shift
			var sh, sel uint32
			if opcode == 0x13 {
				sh = (w >> 20) & 0x3F  // 6-bit shamt
				sel = (w >> 26) & 0x3F // funct6
			} else {
				sh = (w >> 20) & 0x1F  // 5-bit shamt
				sel = (w >> 25) & 0x7F // funct7
			}
			in := find(func(x *insn) bool {
				return x.opcode == opcode && x.format == fmtIShift && x.funct3 == funct3 && x.funct7 == sel
			})
			if in == nil {
				break
			}
			return fmt.Sprintf("%s %s, %s, %d", in.name, xreg(rd), xreg(rs1), sh), 4, nil
		}
		in := find(func(x *insn) bool {
			return x.opcode == opcode && x.format == fmtI && x.funct3 == funct3
		})
		if in == nil {
			break
		}
		imm := signExtend(w>>20, 12)
		return fmt.Sprintf("%s %s, %s, %d", in.name, xreg(rd), xreg(rs1), imm), 4, nil

	case 0x03: // loads
		in := find(func(x *insn) bool { return x.format == fmtILoad && x.funct3 == funct3 })
		if in == nil {
			break
		}
		imm := signExtend(w>>20, 12)
		return fmt.Sprintf("%s %s, %d(%s)", in.name, xreg(rd), imm, xreg(rs1)), 4, nil

	case 0x67: // jalr
		imm := signExtend(w>>20, 12)
		return fmt.Sprintf("jalr %s, %d(%s)", xreg(rd), imm, xreg(rs1)), 4, nil

	case 0x23: // stores
		in := find(func(x *insn) bool { return x.format == fmtS && x.funct3 == funct3 })
		if in == nil {
			break
		}
		imm := signExtend(((w>>25)&0x7F)<<5|((w>>7)&0x1F), 12)
		return fmt.Sprintf("%s %s, %d(%s)", in.name, xreg(rs2), imm, xreg(rs1)), 4, nil

	case 0x63: // branches
		in := find(func(x *insn) bool { return x.format == fmtB && x.funct3 == funct3 })
		if in == nil {
			break
		}
		imm := signExtend(
			((w>>31)&1)<<12|((w>>25)&0x3F)<<5|((w>>8)&0xF)<<1|((w>>7)&1)<<11, 13)
		return fmt.Sprintf("%s %s, %s, %d", in.name, xreg(rs1), xreg(rs2), imm), 4, nil

	case 0x2F: // A extension (atomics)
		funct5 := (w >> 27) & 0x1F
		in := find(func(x *insn) bool {
			return x.opcode == 0x2F && (x.format == fmtAtomic || x.format == fmtAtomicLR) &&
				x.funct3 == funct3 && x.funct7 == funct5
		})
		if in == nil {
			break
		}
		suffix := ""
		if aq, rl := (w>>26)&1, (w>>25)&1; aq != 0 && rl != 0 {
			suffix = ".aqrl"
		} else if aq != 0 {
			suffix = ".aq"
		} else if rl != 0 {
			suffix = ".rl"
		}
		if in.format == fmtAtomicLR {
			return fmt.Sprintf("%s%s %s, (%s)", in.name, suffix, xreg(rd), xreg(rs1)), 4, nil
		}
		return fmt.Sprintf("%s%s %s, %s, (%s)", in.name, suffix, xreg(rd), xreg(rs2), xreg(rs1)), 4, nil

	case 0x07: // FP load
		imm := signExtend(w>>20, 12)
		switch funct3 {
		case 0x2:
			return fmt.Sprintf("flw %s, %d(%s)", freg(rd), imm, xreg(rs1)), 4, nil
		case 0x3:
			return fmt.Sprintf("fld %s, %d(%s)", freg(rd), imm, xreg(rs1)), 4, nil
		}

	case 0x27: // FP store
		imm := signExtend(((w>>25)&0x7F)<<5|((w>>7)&0x1F), 12)
		switch funct3 {
		case 0x2:
			return fmt.Sprintf("fsw %s, %d(%s)", freg(rs2), imm, xreg(rs1)), 4, nil
		case 0x3:
			return fmt.Sprintf("fsd %s, %d(%s)", freg(rs2), imm, xreg(rs1)), 4, nil
		}

	case 0x53: // OP-FP (R-type)
		if text, ok := disasmFPR(funct7, funct3, rs2, rs1, rd); ok {
			return text, 4, nil
		}

	case 0x43, 0x47, 0x4B, 0x4F: // fused multiply-add (R4-type)
		fmtBit := (w >> 25) & 3
		rs3 := (w >> 27) & 0x1F
		if fmtBit < 2 {
			for i := range fpTable {
				f := &fpTable[i]
				if f.form == fpR4 && f.opcode == opcode && (f.funct7&1) == fmtBit {
					return fmt.Sprintf("%s %s, %s, %s, %s%s", f.name,
						freg(rd), freg(rs1), freg(rs2), freg(rs3), rmSuffix(funct3)), 4, nil
				}
			}
		}

	case 0x37: // lui
		return fmt.Sprintf("lui %s, 0x%x", xreg(rd), (w>>12)&0xFFFFF), 4, nil
	case 0x17: // auipc
		return fmt.Sprintf("auipc %s, 0x%x", xreg(rd), (w>>12)&0xFFFFF), 4, nil

	case 0x6F: // jal
		imm := signExtend(
			((w>>31)&1)<<20|((w>>21)&0x3FF)<<1|((w>>20)&1)<<11|((w>>12)&0xFF)<<12, 21)
		return fmt.Sprintf("jal %s, %d", xreg(rd), imm), 4, nil

	case 0x0F: // fence / fence.i
		if funct3 == 0x1 {
			return "fence.i", 4, nil
		}
		imm := (w >> 20) & 0xFFF
		pred, succ := (imm>>4)&0xF, imm&0xF
		if pred == 0xF && succ == 0xF {
			return "fence", 4, nil
		}
		return fmt.Sprintf("fence %s, %s", fenceStr(pred), fenceStr(succ)), 4, nil

	case 0x73: // system + Zicsr
		if funct3 == 0 {
			switch w >> 20 {
			case 0x000:
				return "ecall", 4, nil
			case 0x001:
				return "ebreak", 4, nil
			case 0x302:
				return "mret", 4, nil
			case 0x102:
				return "sret", 4, nil
			case 0x105:
				return "wfi", 4, nil
			}
			break
		}
		csr := (w >> 20) & 0xFFF
		in := find(func(x *insn) bool {
			return x.opcode == 0x73 && (x.format == fmtCSR || x.format == fmtCSRI) && x.funct3 == funct3
		})
		if in == nil {
			break
		}
		if in.format == fmtCSRI {
			return fmt.Sprintf("%s %s, %s, %d", in.name, xreg(rd), csrName(csr), rs1), 4, nil
		}
		return fmt.Sprintf("%s %s, %s, %s", in.name, xreg(rd), csrName(csr), xreg(rs1)), 4, nil
	}
	return "", 0, fmt.Errorf("riscv: cannot decode %#08x", w)
}

// DisassembleAll decodes every 4-byte instruction in code, one text line each.
func DisassembleAll(code []byte) ([]string, error) {
	var out []string
	for off := 0; off+4 <= len(code); off += 4 {
		text, _, err := Disassemble(code[off:])
		if err != nil {
			return out, fmt.Errorf("riscv disasm at offset %d: %w", off, err)
		}
		out = append(out, strings.TrimSpace(text))
	}
	return out, nil
}
