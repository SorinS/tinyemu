package riscv

// RISC-V C Extension (Compressed Instructions) Decoder
// Expands 16-bit compressed instructions to their 32-bit equivalents.
//
// Reference: riscv_cpu_template.h from TinyEMU

import "errors"

// ErrIllegalCompressedInsn indicates an invalid compressed instruction
var ErrIllegalCompressedInsn = errors.New("illegal compressed instruction")

// ExpandCompressed expands a 16-bit compressed instruction to its 32-bit equivalent.
// Returns the expanded instruction or an error if the instruction is invalid.
// The xlen parameter controls XLEN-dependent encodings (RV32C vs RV64C).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:462-525
func ExpandCompressed(insn uint16, xlen XLEN) (uint32, error) {
	quadrant := insn & 0x3
	funct3 := (insn >> 13) & 0x7

	switch quadrant {
	case 0:
		return expandC0(insn, funct3, xlen)
	case 1:
		return expandC1(insn, funct3, xlen)
	case 2:
		return expandC2(insn, funct3, xlen)
	default:
		// quadrant == 3 means this is not a compressed instruction
		return 0, ErrIllegalCompressedInsn
	}
}

// expandC0 decodes quadrant 0 compressed instructions (bits 1:0 = 00)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:306-451
// C implementation:
//   - funct3=0: C.ADDI4SPN (lines 311-319)
//   - funct3=1: C.FLD (RV32/64, lines 331-347) / C.LQ (RV128)
//   - funct3=2: C.LW (lines 348-360)
//   - funct3=3: C.LD (RV64, lines 361-373) / C.FLW (RV32, lines 374-391)
//   - funct3=5: C.FSD (RV32/64, lines 403-414) / C.SQ (RV128)
//   - funct3=6: C.SW (lines 415-424)
//   - funct3=7: C.SD (RV64, lines 425-434) / C.FSW (RV32, lines 435-447)
//
// Note: FP instructions (C.FLD, C.FSD, C.FLW, C.FSW) check FS during execution,
// not during expansion. This matches C behavior where check happens at execution.
func expandC0(insn uint16, funct3 uint16, xlen XLEN) (uint32, error) {
	// rd' is in bits 4:2, maps to registers x8-x15
	rdPrime := ((insn >> 2) & 0x7) + 8
	// rs1' is in bits 9:7, maps to registers x8-x15
	rs1Prime := ((insn >> 7) & 0x7) + 8

	switch funct3 {
	case 0: // C.ADDI4SPN - rd' = sp + imm
		// imm[5:4|9:6|2|3] = insn[12:11|10:7|6|5]
		imm := getField1(insn, 11, 4, 5) |
			getField1(insn, 7, 6, 9) |
			getField1(insn, 6, 2, 2) |
			getField1(insn, 5, 3, 3)
		if imm == 0 {
			return 0, ErrIllegalCompressedInsn
		}
		// Expand to: ADDI rd', x2, imm
		return encodeIType(OpcodeOpImm, uint32(rdPrime), Funct3ADDI, 2, int32(imm)), nil

	case 1: // C.FLD (RV32/64) or C.LQ (RV128) - floating point load double
		// imm[5:3|7:6] = insn[12:10|6:5]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		// Expand to: FLD rd', imm(rs1')
		return encodeIType(OpcodeFPLoad, uint32(rdPrime), 3, uint32(rs1Prime), int32(imm)), nil

	case 2: // C.LW - load word
		// imm[5:3|2|6] = insn[12:10|6|5]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 6, 2, 2) |
			getField1(insn, 5, 6, 6)
		// Expand to: LW rd', imm(rs1')
		return encodeIType(OpcodeLoad, uint32(rdPrime), Funct3LW, uint32(rs1Prime), int32(imm)), nil

	case 3: // C.LD (RV64/128) or C.FLW (RV32)
		// imm[5:3|7:6] = insn[12:10|6:5]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		if xlen == XLEN32 {
			// C.FLW - floating point load word (RV32 only)
			// imm[5:3|2|6] = insn[12:10|6|5]
			imm = getField1(insn, 10, 3, 5) |
				getField1(insn, 6, 2, 2) |
				getField1(insn, 5, 6, 6)
			// Expand to: FLW rd', imm(rs1')
			return encodeIType(OpcodeFPLoad, uint32(rdPrime), 2, uint32(rs1Prime), int32(imm)), nil
		}
		// Expand to: LD rd', imm(rs1')
		return encodeIType(OpcodeLoad, uint32(rdPrime), Funct3LD, uint32(rs1Prime), int32(imm)), nil

	case 5: // C.FSD (RV32/64) - floating point store double
		// imm[5:3|7:6] = insn[12:10|6:5]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		// rs2' is in bits 4:2
		rs2Prime := ((insn >> 2) & 0x7) + 8
		// Expand to: FSD rs2', imm(rs1')
		return encodeSType(OpcodeFPStore, 3, uint32(rs1Prime), uint32(rs2Prime), int32(imm)), nil

	case 6: // C.SW - store word
		// imm[5:3|2|6] = insn[12:10|6|5]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 6, 2, 2) |
			getField1(insn, 5, 6, 6)
		// rs2' is in bits 4:2
		rs2Prime := ((insn >> 2) & 0x7) + 8
		// Expand to: SW rs2', imm(rs1')
		return encodeSType(OpcodeStore, 2, uint32(rs1Prime), uint32(rs2Prime), int32(imm)), nil

	case 7: // C.SD (RV64/128) or C.FSW (RV32)
		// rs2' is in bits 4:2
		rs2Prime := ((insn >> 2) & 0x7) + 8
		if xlen == XLEN32 {
			// C.FSW - floating point store word (RV32 only)
			// imm[5:3|2|6] = insn[12:10|6|5]
			imm := getField1(insn, 10, 3, 5) |
				getField1(insn, 6, 2, 2) |
				getField1(insn, 5, 6, 6)
			// Expand to: FSW rs2', imm(rs1')
			return encodeSType(OpcodeFPStore, 2, uint32(rs1Prime), uint32(rs2Prime), int32(imm)), nil
		}
		// imm[5:3|7:6] = insn[12:10|6:5]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 5, 6, 7)
		// Expand to: SD rs2', imm(rs1')
		return encodeSType(OpcodeStore, 3, uint32(rs1Prime), uint32(rs2Prime), int32(imm)), nil

	default:
		return 0, ErrIllegalCompressedInsn
	}
}

// expandC1 decodes quadrant 1 compressed instructions (bits 1:0 = 01)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:452-605
// C implementation:
//   - funct3=0: C.ADDI/C.NOP (lines 455-461)
//   - funct3=1: C.JAL (RV32, lines 462-474) / C.ADDIW (RV64, lines 475-483)
//   - funct3=2: C.LI (lines 484-490)
//   - funct3=3: C.ADDI16SP/C.LUI (lines 491-508)
//   - funct3=4: Arithmetic operations (lines 509-565)
//   - funct3=5: C.J (lines 567-577)
//   - funct3=6: C.BEQZ (lines 578-589)
//   - funct3=7: C.BNEZ (lines 590-601)
func expandC1(insn uint16, funct3 uint16, xlen XLEN) (uint32, error) {
	rd := (insn >> 7) & 0x1F

	switch funct3 {
	case 0: // C.ADDI / C.NOP
		// imm[5|4:0] = insn[12|6:2]
		imm := sextC(getField1(insn, 12, 5, 5)|getField1(insn, 2, 0, 4), 6)
		if rd == 0 {
			// C.NOP
			return encodeIType(OpcodeOpImm, 0, Funct3ADDI, 0, 0), nil
		}
		// Expand to: ADDI rd, rd, imm
		return encodeIType(OpcodeOpImm, uint32(rd), Funct3ADDI, uint32(rd), imm), nil

	case 1: // C.JAL (RV32) or C.ADDIW (RV64/128)
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:517-525
		if xlen == XLEN32 {
			// C.JAL - jump and link (RV32 only)
			// imm[11|4|9:8|10|6|7|3:1|5] = insn[12|11|10:9|8|7|6|5:3|2]
			imm := sextC(getField1(insn, 12, 11, 11)|
				getField1(insn, 11, 4, 4)|
				getField1(insn, 9, 8, 9)|
				getField1(insn, 8, 10, 10)|
				getField1(insn, 7, 6, 6)|
				getField1(insn, 6, 7, 7)|
				getField1(insn, 3, 1, 3)|
				getField1(insn, 2, 5, 5), 12)
			// Expand to: JAL x1, imm
			return encodeJType(OpcodeJAL, 1, imm), nil
		}
		// C.ADDIW (RV64/128) - add immediate word
		// imm[5|4:0] = insn[12|6:2]
		imm := sextC(getField1(insn, 12, 5, 5)|getField1(insn, 2, 0, 4), 6)
		if rd == 0 {
			return 0, ErrIllegalCompressedInsn // Reserved
		}
		// Expand to: ADDIW rd, rd, imm
		return encodeIType(OpcodeOpImm32, uint32(rd), 0, uint32(rd), imm), nil

	case 2: // C.LI - load immediate
		// imm[5|4:0] = insn[12|6:2]
		imm := sextC(getField1(insn, 12, 5, 5)|getField1(insn, 2, 0, 4), 6)
		if rd == 0 {
			// HINT (rd=0)
			return encodeIType(OpcodeOpImm, 0, Funct3ADDI, 0, 0), nil
		}
		// Expand to: ADDI rd, x0, imm
		return encodeIType(OpcodeOpImm, uint32(rd), Funct3ADDI, 0, imm), nil

	case 3: // C.ADDI16SP / C.LUI
		if rd == 2 {
			// C.ADDI16SP - add 16-byte scaled immediate to sp
			// imm[9|4|6|8:7|5] = insn[12|6|5|4:3|2]
			imm := sextC(getField1(insn, 12, 9, 9)|
				getField1(insn, 6, 4, 4)|
				getField1(insn, 5, 6, 6)|
				getField1(insn, 3, 7, 8)|
				getField1(insn, 2, 5, 5), 10)
			if imm == 0 {
				return 0, ErrIllegalCompressedInsn
			}
			// Expand to: ADDI x2, x2, imm
			return encodeIType(OpcodeOpImm, 2, Funct3ADDI, 2, imm), nil
		}
		if rd != 0 {
			// C.LUI - load upper immediate
			// imm[17|16:12] = insn[12|6:2]
			// NOTE: RISC-V spec says nzimm=0 is RESERVED, but C TinyEMU allows it.
			// We match C behavior: no check for imm == 0.
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:502-507 (no imm==0 check)
			imm := sextC(getField1(insn, 12, 17, 17)|getField1(insn, 2, 12, 16), 18)
			// Expand to: LUI rd, imm
			return encodeUType(OpcodeLUI, uint32(rd), imm), nil
		}
		// rd == 0 is HINT
		return encodeIType(OpcodeOpImm, 0, Funct3ADDI, 0, 0), nil

	case 4: // Arithmetic operations
		return expandC1Arith(insn, xlen)

	case 5: // C.J - unconditional jump
		// imm[11|4|9:8|10|6|7|3:1|5] = insn[12|11|10:9|8|7|6|5:3|2]
		imm := sextC(getField1(insn, 12, 11, 11)|
			getField1(insn, 11, 4, 4)|
			getField1(insn, 9, 8, 9)|
			getField1(insn, 8, 10, 10)|
			getField1(insn, 7, 6, 6)|
			getField1(insn, 6, 7, 7)|
			getField1(insn, 3, 1, 3)|
			getField1(insn, 2, 5, 5), 12)
		// Expand to: JAL x0, imm
		return encodeJType(OpcodeJAL, 0, imm), nil

	case 6: // C.BEQZ - branch if equal to zero
		// rs1' is in bits 9:7
		rs1Prime := ((insn >> 7) & 0x7) + 8
		// imm[8|4:3|7:6|2:1|5] = insn[12|11:10|6:5|4:3|2]
		imm := sextC(getField1(insn, 12, 8, 8)|
			getField1(insn, 10, 3, 4)|
			getField1(insn, 5, 6, 7)|
			getField1(insn, 3, 1, 2)|
			getField1(insn, 2, 5, 5), 9)
		// Expand to: BEQ rs1', x0, imm
		return encodeBType(OpcodeBranch, Funct3BEQ, uint32(rs1Prime), 0, imm), nil

	case 7: // C.BNEZ - branch if not equal to zero
		// rs1' is in bits 9:7
		rs1Prime := ((insn >> 7) & 0x7) + 8
		// imm[8|4:3|7:6|2:1|5] = insn[12|11:10|6:5|4:3|2]
		imm := sextC(getField1(insn, 12, 8, 8)|
			getField1(insn, 10, 3, 4)|
			getField1(insn, 5, 6, 7)|
			getField1(insn, 3, 1, 2)|
			getField1(insn, 2, 5, 5), 9)
		// Expand to: BNE rs1', x0, imm
		return encodeBType(OpcodeBranch, Funct3BNE, uint32(rs1Prime), 0, imm), nil

	default:
		return 0, ErrIllegalCompressedInsn
	}
}

// expandC1Arith handles the C.SRLI/C.SRAI/C.ANDI/C.SUB/C.XOR/C.OR/C.AND/C.SUBW/C.ADDW group
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:509-565 (funct3=4 cases)
// C implementation:
//   - funct2=0: C.SRLI (lines 513-530)
//   - funct2=1: C.SRAI (lines 513-530)
//   - funct2=2: C.ANDI (lines 532-536)
//   - funct2=3: C.SUB/C.XOR/C.OR/C.AND/C.SUBW/C.ADDW (lines 537-563)
func expandC1Arith(insn uint16, xlen XLEN) (uint32, error) {
	funct2 := (insn >> 10) & 0x3
	rdPrime := ((insn >> 7) & 0x7) + 8

	switch funct2 {
	case 0: // C.SRLI
		// shamt[5|4:0] = insn[12|6:2]
		shamt := getField1(insn, 12, 5, 5) | getField1(insn, 2, 0, 4)
		// For RV32: shamt bit 5 (0x20) is illegal
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:499-502
		if xlen == XLEN32 && (shamt&0x20) != 0 {
			return 0, ErrIllegalCompressedInsn
		}
		// Expand to: SRLI rd', rd', shamt
		return encodeIType(OpcodeOpImm, uint32(rdPrime), Funct3SRLI, uint32(rdPrime), int32(shamt)), nil

	case 1: // C.SRAI
		// shamt[5|4:0] = insn[12|6:2]
		shamt := getField1(insn, 12, 5, 5) | getField1(insn, 2, 0, 4)
		// For RV32: shamt bit 5 (0x20) is illegal
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:503-506
		if xlen == XLEN32 && (shamt&0x20) != 0 {
			return 0, ErrIllegalCompressedInsn
		}
		// For SRAI, bit 30 is set in the expanded instruction
		// Expand to: SRAI rd', rd', shamt
		return encodeIType(OpcodeOpImm, uint32(rdPrime), Funct3SRLI, uint32(rdPrime), int32(shamt|0x400)), nil

	case 2: // C.ANDI
		// imm[5|4:0] = insn[12|6:2]
		imm := sextC(getField1(insn, 12, 5, 5)|getField1(insn, 2, 0, 4), 6)
		// Expand to: ANDI rd', rd', imm
		return encodeIType(OpcodeOpImm, uint32(rdPrime), Funct3ANDI, uint32(rdPrime), imm), nil

	case 3: // C.SUB/C.XOR/C.OR/C.AND/C.SUBW/C.ADDW
		rs2Prime := ((insn >> 2) & 0x7) + 8
		funct := ((insn >> 5) & 0x3) | ((insn >> (12 - 2)) & 0x4)

		switch funct {
		case 0: // C.SUB
			// Expand to: SUB rd', rd', rs2'
			return encodeRType(OpcodeOp, uint32(rdPrime), Funct3ADD, uint32(rdPrime), uint32(rs2Prime), 0x20), nil
		case 1: // C.XOR
			// Expand to: XOR rd', rd', rs2'
			return encodeRType(OpcodeOp, uint32(rdPrime), Funct3XOR, uint32(rdPrime), uint32(rs2Prime), 0), nil
		case 2: // C.OR
			// Expand to: OR rd', rd', rs2'
			return encodeRType(OpcodeOp, uint32(rdPrime), Funct3OR, uint32(rdPrime), uint32(rs2Prime), 0), nil
		case 3: // C.AND
			// Expand to: AND rd', rd', rs2'
			return encodeRType(OpcodeOp, uint32(rdPrime), Funct3AND, uint32(rdPrime), uint32(rs2Prime), 0), nil
		case 4: // C.SUBW (RV64/128)
			// Illegal in RV32 (reserved encoding)
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:513-514
			if xlen == XLEN32 {
				return 0, ErrIllegalCompressedInsn
			}
			// Expand to: SUBW rd', rd', rs2'
			return encodeRType(OpcodeOp32, uint32(rdPrime), 0, uint32(rdPrime), uint32(rs2Prime), 0x20), nil
		case 5: // C.ADDW (RV64/128)
			// Illegal in RV32 (reserved encoding)
			// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:515-516
			if xlen == XLEN32 {
				return 0, ErrIllegalCompressedInsn
			}
			// Expand to: ADDW rd', rd', rs2'
			return encodeRType(OpcodeOp32, uint32(rdPrime), 0, uint32(rdPrime), uint32(rs2Prime), 0), nil
		default:
			return 0, ErrIllegalCompressedInsn
		}

	default:
		return 0, ErrIllegalCompressedInsn
	}
}

// expandC2 decodes quadrant 2 compressed instructions (bits 1:0 = 10)
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:606-776
// C implementation:
//   - funct3=0: C.SLLI (lines 610-621)
//   - funct3=1: C.FLDSP (RV32/64, lines 633-649) / C.LQSP (RV128)
//   - funct3=2: C.LWSP (lines 650-662)
//   - funct3=3: C.LDSP (RV64, lines 663-676) / C.FLWSP (RV32, lines 677-693)
//   - funct3=4: C.JR/C.MV/C.EBREAK/C.JALR/C.ADD (lines 694-726)
//   - funct3=5: C.FSDSP (RV32/64, lines 735-744) / C.SQSP (RV128)
//   - funct3=6: C.SWSP (lines 746-752)
//   - funct3=7: C.SDSP (RV64, lines 753-760) / C.FSWSP (RV32, lines 761-770)
func expandC2(insn uint16, funct3 uint16, xlen XLEN) (uint32, error) {
	rd := (insn >> 7) & 0x1F
	rs2 := uint32((insn >> 2) & 0x1F)

	switch funct3 {
	case 0: // C.SLLI
		// shamt[5|4:0] = insn[12|6:2]
		shamt := getField1(insn, 12, 5, 5) | rs2
		// For RV32: shamt bit 5 (0x20) is illegal
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:527-530
		if xlen == XLEN32 && (shamt&0x20) != 0 {
			return 0, ErrIllegalCompressedInsn
		}
		if rd == 0 {
			// HINT
			return encodeIType(OpcodeOpImm, 0, Funct3ADDI, 0, 0), nil
		}
		// Expand to: SLLI rd, rd, shamt
		return encodeIType(OpcodeOpImm, uint32(rd), Funct3SLLI, uint32(rd), int32(shamt)), nil

	case 1: // C.FLDSP - floating point load double, stack-pointer relative
		// imm[5|4:3|8:6] = insn[12|6:5|4:2]
		imm := getField1(insn, 12, 5, 5) |
			(rs2 & (3 << 3)) |
			getField1(insn, 2, 6, 8)
		// Expand to: FLD rd, imm(x2)
		return encodeIType(OpcodeFPLoad, uint32(rd), 3, 2, int32(imm)), nil

	case 2: // C.LWSP - load word, stack-pointer relative
		// imm[5|4:2|7:6] = insn[12|6:4|3:2]
		imm := getField1(insn, 12, 5, 5) |
			(rs2 & (7 << 2)) |
			getField1(insn, 2, 6, 7)
		// NOTE: RISC-V spec says rd=0 is RESERVED, but C TinyEMU allows it.
		// C performs the load (which can fault) then discards result if rd=0.
		// We match C behavior by expanding to LW x0, ... which performs the load.
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:650-661
		// Expand to: LW rd, imm(x2)
		return encodeIType(OpcodeLoad, uint32(rd), Funct3LW, 2, int32(imm)), nil

	case 3: // C.LDSP (RV64/128) or C.FLWSP (RV32)
		if xlen == XLEN32 {
			// C.FLWSP - floating point load word, stack-pointer relative
			// imm[5|4:2|7:6] = insn[12|6:4|3:2]
			imm := getField1(insn, 12, 5, 5) |
				(rs2 & (7 << 2)) |
				getField1(insn, 2, 6, 7)
			// Expand to: FLW rd, imm(x2)
			return encodeIType(OpcodeFPLoad, uint32(rd), 2, 2, int32(imm)), nil
		}
		// C.LDSP (RV64/128) - load double, stack-pointer relative
		// imm[5|4:3|8:6] = insn[12|6:5|4:2]
		imm := getField1(insn, 12, 5, 5) |
			(rs2 & (3 << 3)) |
			getField1(insn, 2, 6, 8)
		// NOTE: RISC-V spec says rd=0 is RESERVED, but C TinyEMU allows it.
		// C performs the load (which can fault) then discards result if rd=0.
		// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:663-675
		// Expand to: LD rd, imm(x2)
		return encodeIType(OpcodeLoad, uint32(rd), Funct3LD, 2, int32(imm)), nil

	case 4: // C.JR/C.MV/C.EBREAK/C.JALR/C.ADD
		bit12 := (insn >> 12) & 1
		if bit12 == 0 {
			if rs2 == 0 {
				// C.JR - jump register
				if rd == 0 {
					return 0, ErrIllegalCompressedInsn // Reserved
				}
				// Expand to: JALR x0, rs1, 0
				return encodeIType(OpcodeJALR, 0, 0, uint32(rd), 0), nil
			}
			// C.MV - move
			if rd == 0 {
				// HINT
				return encodeIType(OpcodeOpImm, 0, Funct3ADDI, 0, 0), nil
			}
			// Expand to: ADD rd, x0, rs2
			return encodeRType(OpcodeOp, uint32(rd), Funct3ADD, 0, uint32(rs2), 0), nil
		}
		// bit12 == 1
		if rs2 == 0 {
			if rd == 0 {
				// C.EBREAK
				return encodeIType(OpcodeSystem, 0, 0, 0, 1), nil // EBREAK encoding
			}
			// C.JALR - jump and link register
			// Expand to: JALR x1, rs1, 0
			return encodeIType(OpcodeJALR, 1, 0, uint32(rd), 0), nil
		}
		// C.ADD
		if rd == 0 {
			// HINT
			return encodeIType(OpcodeOpImm, 0, Funct3ADDI, 0, 0), nil
		}
		// Expand to: ADD rd, rd, rs2
		return encodeRType(OpcodeOp, uint32(rd), Funct3ADD, uint32(rd), uint32(rs2), 0), nil

	case 5: // C.FSDSP - floating point store double, stack-pointer relative
		// imm[5:3|8:6] = insn[12:10|9:7]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 7, 6, 8)
		// Expand to: FSD rs2, imm(x2)
		return encodeSType(OpcodeFPStore, 3, 2, uint32(rs2), int32(imm)), nil

	case 6: // C.SWSP - store word, stack-pointer relative
		// imm[5:2|7:6] = insn[12:9|8:7]
		imm := getField1(insn, 9, 2, 5) |
			getField1(insn, 7, 6, 7)
		// Expand to: SW rs2, imm(x2)
		return encodeSType(OpcodeStore, 2, 2, uint32(rs2), int32(imm)), nil

	case 7: // C.SDSP (RV64/128) or C.FSWSP (RV32)
		if xlen == XLEN32 {
			// C.FSWSP - floating point store word, stack-pointer relative
			// imm[5:2|7:6] = insn[12:9|8:7]
			imm := getField1(insn, 9, 2, 5) |
				getField1(insn, 7, 6, 7)
			// Expand to: FSW rs2, imm(x2)
			return encodeSType(OpcodeFPStore, 2, 2, uint32(rs2), int32(imm)), nil
		}
		// C.SDSP (RV64/128) - store double, stack-pointer relative
		// imm[5:3|8:6] = insn[12:10|9:7]
		imm := getField1(insn, 10, 3, 5) |
			getField1(insn, 7, 6, 8)
		// Expand to: SD rs2, imm(x2)
		return encodeSType(OpcodeStore, 3, 2, uint32(rs2), int32(imm)), nil

	default:
		return 0, ErrIllegalCompressedInsn
	}
}

// Helper functions for instruction encoding

// getField1 extracts a field from a 16-bit instruction and shifts it to target position
// srcBit: source bit position in instruction
// dstLo: lowest bit position in destination
// dstHi: highest bit position in destination
// Returns uint32 to accommodate shifts up to bit positions 20+ (for J-type immediates)
func getField1(insn uint16, srcBit, dstLo, dstHi int) uint32 {
	width := dstHi - dstLo + 1
	mask := uint32((1 << width) - 1)
	return ((uint32(insn) >> srcBit) & mask) << dstLo
}

// sextC sign-extends a value from fromBits bits to int32
func sextC(val uint32, fromBits int) int32 {
	shift := 32 - fromBits
	return int32(int32(val) << shift >> shift)
}

// encodeRType creates an R-type instruction
func encodeRType(opcode uint32, rd, funct3, rs1, rs2, funct7 uint32) uint32 {
	return opcode | (rd << 7) | (funct3 << 12) | (rs1 << 15) | (rs2 << 20) | (funct7 << 25)
}

// encodeIType creates an I-type instruction
func encodeIType(opcode uint32, rd, funct3, rs1 uint32, imm int32) uint32 {
	return opcode | (rd << 7) | (funct3 << 12) | (rs1 << 15) | (uint32(imm) << 20)
}

// encodeSType creates an S-type instruction
func encodeSType(opcode uint32, funct3, rs1, rs2 uint32, imm int32) uint32 {
	immLo := uint32(imm) & 0x1F
	immHi := (uint32(imm) >> 5) & 0x7F
	return opcode | (immLo << 7) | (funct3 << 12) | (rs1 << 15) | (rs2 << 20) | (immHi << 25)
}

// encodeBType creates a B-type instruction
func encodeBType(opcode uint32, funct3, rs1, rs2 uint32, imm int32) uint32 {
	// imm[12|10:5|4:1|11]
	imm11 := (uint32(imm) >> 11) & 1
	imm4_1 := (uint32(imm) >> 1) & 0xF
	imm10_5 := (uint32(imm) >> 5) & 0x3F
	imm12 := (uint32(imm) >> 12) & 1
	return opcode | (imm11 << 7) | (imm4_1 << 8) | (funct3 << 12) | (rs1 << 15) | (rs2 << 20) | (imm10_5 << 25) | (imm12 << 31)
}

// encodeUType creates a U-type instruction
func encodeUType(opcode uint32, rd uint32, imm int32) uint32 {
	return opcode | (rd << 7) | (uint32(imm) & 0xFFFFF000)
}

// encodeJType creates a J-type instruction
func encodeJType(opcode uint32, rd uint32, imm int32) uint32 {
	// imm[20|10:1|11|19:12]
	imm19_12 := (uint32(imm) >> 12) & 0xFF
	imm11 := (uint32(imm) >> 11) & 1
	imm10_1 := (uint32(imm) >> 1) & 0x3FF
	imm20 := (uint32(imm) >> 20) & 1
	return opcode | (rd << 7) | (imm19_12 << 12) | (imm11 << 20) | (imm10_1 << 21) | (imm20 << 31)
}
