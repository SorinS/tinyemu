// Package riscv implements RISC-V floating-point instructions.
// Reference: riscv_cpu_template.h and riscv_cpu_fp_template.h from TinyEMU 2019-12-21

package riscv

import (
	"github.com/jtolio/tinyemu-go/softfp"
)

// F32High is the NaN-boxing mask for F32 values in 64-bit FP registers.
// When storing a 32-bit float in a 64-bit register, the upper bits must be all 1s.
const F32High = uint64(0xFFFFFFFF00000000)

// F32SignMask is the sign bit mask for 32-bit floats.
const F32SignMask = uint32(0x80000000)

// getInsnRM returns the effective rounding mode for an instruction.
// If rm is 7 (DYN), use the FRM register. Otherwise use rm.
// Returns -1 for invalid rounding mode.
func (c *CPU) getInsnRM(rm uint32) int {
	if rm == RoundDynamic {
		rm = uint32(c.FRM)
	}
	if rm >= 5 {
		return -1 // Invalid rounding mode
	}
	return int(rm)
}

// convertRM converts CPU rounding mode to softfp rounding mode.
func convertRM(rm int) softfp.RoundingMode {
	switch rm {
	case RoundNearestEven:
		return softfp.RNE
	case RoundToZero:
		return softfp.RTZ
	case RoundDown:
		return softfp.RDN
	case RoundUp:
		return softfp.RUP
	case RoundNearestMax:
		return softfp.RMM
	default:
		return softfp.RNE
	}
}

// updateFFlags adds the given exception flags to the CPU's fflags.
func (c *CPU) updateFFlags(flags softfp.ExceptionFlags) {
	// Map softfp flags to RISC-V fflags:
	// softfp: FlagInexact=1, FlagUnderflow=2, FlagOverflow=4, FlagDivideZero=8, FlagInvalidOp=16
	// RISC-V: NX=1, UF=2, OF=4, DZ=8, NV=16
	// The mapping is the same, so we can use direct OR
	c.FFlags |= uint32(flags)
	c.FS = FSDirty
}

// executeFPLoad executes floating-point load instructions (FLW, FLD).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1479-1518 (case 0x07)
func (c *CPU) executeFPLoad(insn uint32, funct3 uint32, rd, rs1 int) error {
	// Check if FP is enabled
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	imm := ExtractIImm(insn)
	addr := uint64(int64(c.GetReg(rs1)) + imm)

	switch funct3 {
	case 2: // FLW - load single-precision float
		val, err := c.LoadU32(addr)
		if err != nil {
			return c.handleException()
		}
		// NaN-box the 32-bit value
		c.FPReg[rd] = F32High | uint64(val)
		c.FS = FSDirty

	case 3: // FLD - load double-precision float
		val, err := c.LoadU64(addr)
		if err != nil {
			return c.handleException()
		}
		c.FPReg[rd] = val
		c.FS = FSDirty

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.PC += 4
	return nil
}

// executeFPStore executes floating-point store instructions (FSW, FSD).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1519-1546 (case 0x27)
func (c *CPU) executeFPStore(insn uint32, funct3 uint32, rs1, rs2 int) error {
	// Check if FP is enabled
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	imm := ExtractSImm(insn)
	addr := uint64(int64(c.GetReg(rs1)) + imm)

	switch funct3 {
	case 2: // FSW - store single-precision float
		val := uint32(c.FPReg[rs2])
		if err := c.StoreU32(addr, val); err != nil {
			return c.handleException()
		}

	case 3: // FSD - store double-precision float
		if err := c.StoreU64(addr, c.FPReg[rs2]); err != nil {
			return c.handleException()
		}

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.PC += 4
	return nil
}

// executeFMADD executes FMADD.S/D instructions: (rs1 * rs2) + rs3
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1547-1576 (case 0x43)
func (c *CPU) executeFMADD(insn uint32) error {
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	rd := int(ExtractRd(insn))
	rs1 := int(ExtractRs1(insn))
	rs2 := int(ExtractRs2(insn))
	rs3 := int(ExtractRs3(insn))
	fmt := (insn >> 25) & 3 // Float format: 0=S, 1=D
	rmBits := ExtractFunct3(insn)

	rm := c.getInsnRM(rmBits)
	if rm < 0 {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	var flags softfp.ExceptionFlags

	switch fmt {
	case 0: // FMADD.S
		a := c.GetFPRegF32(rs1)
		b := c.GetFPRegF32(rs2)
		cc := c.GetFPRegF32(rs3)
		result := softfp.FmaF32(a, b, cc, convertRM(rm), &flags)
		c.SetFPRegF32(rd, result)

	case 1: // FMADD.D
		a := c.FPReg[rs1]
		b := c.FPReg[rs2]
		cc := c.FPReg[rs3]
		result := softfp.FmaF64(a, b, cc, convertRM(rm), &flags)
		c.FPReg[rd] = result
		c.FS = FSDirty

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.updateFFlags(flags)
	c.PC += 4
	return nil
}

// executeFMSUB executes FMSUB.S/D instructions: (rs1 * rs2) - rs3
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1577-1612 (case 0x47)
func (c *CPU) executeFMSUB(insn uint32) error {
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	rd := int(ExtractRd(insn))
	rs1 := int(ExtractRs1(insn))
	rs2 := int(ExtractRs2(insn))
	rs3 := int(ExtractRs3(insn))
	fmt := (insn >> 25) & 3
	rmBits := ExtractFunct3(insn)

	rm := c.getInsnRM(rmBits)
	if rm < 0 {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	var flags softfp.ExceptionFlags

	switch fmt {
	case 0: // FMSUB.S - negate c (the addend)
		a := c.GetFPRegF32(rs1)
		b := c.GetFPRegF32(rs2)
		cc := c.GetFPRegF32(rs3) ^ F32SignMask // Negate c
		result := softfp.FmaF32(a, b, cc, convertRM(rm), &flags)
		c.SetFPRegF32(rd, result)

	case 1: // FMSUB.D
		a := c.FPReg[rs1]
		b := c.FPReg[rs2]
		cc := c.FPReg[rs3] ^ softfp.Float64SignMask // Negate c
		result := softfp.FmaF64(a, b, cc, convertRM(rm), &flags)
		c.FPReg[rd] = result
		c.FS = FSDirty

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.updateFFlags(flags)
	c.PC += 4
	return nil
}

// executeFNMSUB executes FNMSUB.S/D instructions: -(rs1 * rs2) + rs3
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1613-1648 (case 0x4b)
func (c *CPU) executeFNMSUB(insn uint32) error {
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	rd := int(ExtractRd(insn))
	rs1 := int(ExtractRs1(insn))
	rs2 := int(ExtractRs2(insn))
	rs3 := int(ExtractRs3(insn))
	fmt := (insn >> 25) & 3
	rmBits := ExtractFunct3(insn)

	rm := c.getInsnRM(rmBits)
	if rm < 0 {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	var flags softfp.ExceptionFlags

	switch fmt {
	case 0: // FNMSUB.S - negate a (first multiplicand)
		a := c.GetFPRegF32(rs1) ^ F32SignMask // Negate a
		b := c.GetFPRegF32(rs2)
		cc := c.GetFPRegF32(rs3)
		result := softfp.FmaF32(a, b, cc, convertRM(rm), &flags)
		c.SetFPRegF32(rd, result)

	case 1: // FNMSUB.D
		a := c.FPReg[rs1] ^ softfp.Float64SignMask // Negate a
		b := c.FPReg[rs2]
		cc := c.FPReg[rs3]
		result := softfp.FmaF64(a, b, cc, convertRM(rm), &flags)
		c.FPReg[rd] = result
		c.FS = FSDirty

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.updateFFlags(flags)
	c.PC += 4
	return nil
}

// executeFNMADD executes FNMADD.S/D instructions: -(rs1 * rs2) - rs3
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1649-1684 (case 0x4f)
func (c *CPU) executeFNMADD(insn uint32) error {
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	rd := int(ExtractRd(insn))
	rs1 := int(ExtractRs1(insn))
	rs2 := int(ExtractRs2(insn))
	rs3 := int(ExtractRs3(insn))
	fmt := (insn >> 25) & 3
	rmBits := ExtractFunct3(insn)

	rm := c.getInsnRM(rmBits)
	if rm < 0 {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	var flags softfp.ExceptionFlags

	switch fmt {
	case 0: // FNMADD.S - negate both a and c
		a := c.GetFPRegF32(rs1) ^ F32SignMask // Negate a
		b := c.GetFPRegF32(rs2)
		cc := c.GetFPRegF32(rs3) ^ F32SignMask // Negate c
		result := softfp.FmaF32(a, b, cc, convertRM(rm), &flags)
		c.SetFPRegF32(rd, result)

	case 1: // FNMADD.D
		a := c.FPReg[rs1] ^ softfp.Float64SignMask // Negate a
		b := c.FPReg[rs2]
		cc := c.FPReg[rs3] ^ softfp.Float64SignMask // Negate c
		result := softfp.FmaF64(a, b, cc, convertRM(rm), &flags)
		c.FPReg[rd] = result
		c.FS = FSDirty

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.updateFFlags(flags)
	c.PC += 4
	return nil
}

// executeFPOp executes floating-point computational instructions (opcode 0x53).
// Reference: tinyemu-2019-12-21/riscv_cpu_template.h:1685-1706 (case 0x53)
// Reference: tinyemu-2019-12-21/riscv_cpu_fp_template.h (included for F32/F64 ops)
func (c *CPU) executeFPOp(insn uint32) error {
	if c.FS == FSOff {
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	rd := int(ExtractRd(insn))
	rs1 := int(ExtractRs1(insn))
	rs2 := int(ExtractRs2(insn))
	funct7 := ExtractFunct7(insn)
	rmBits := ExtractFunct3(insn)

	// funct7 encodes operation and size
	// Bits 6:2 = operation, bits 1:0 = format (0=S, 1=D, 3=Q)
	opcode := funct7 >> 2
	fmt := funct7 & 3

	var flags softfp.ExceptionFlags

	switch opcode {
	case 0x00: // FADD
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // FADD.S
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			result := softfp.AddF32(a, b, convertRM(rm), &flags)
			c.SetFPRegF32(rd, result)
		case 1: // FADD.D
			result := softfp.AddF64(c.FPReg[rs1], c.FPReg[rs2], convertRM(rm), &flags)
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x01: // FSUB
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // FSUB.S
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			result := softfp.SubF32(a, b, convertRM(rm), &flags)
			c.SetFPRegF32(rd, result)
		case 1: // FSUB.D
			result := softfp.SubF64(c.FPReg[rs1], c.FPReg[rs2], convertRM(rm), &flags)
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x02: // FMUL
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // FMUL.S
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			result := softfp.MulF32(a, b, convertRM(rm), &flags)
			c.SetFPRegF32(rd, result)
		case 1: // FMUL.D
			result := softfp.MulF64(c.FPReg[rs1], c.FPReg[rs2], convertRM(rm), &flags)
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x03: // FDIV
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // FDIV.S
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			result := softfp.DivF32(a, b, convertRM(rm), &flags)
			c.SetFPRegF32(rd, result)
		case 1: // FDIV.D
			result := softfp.DivF64(c.FPReg[rs1], c.FPReg[rs2], convertRM(rm), &flags)
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x0B: // FSQRT
		rm := c.getInsnRM(rmBits)
		if rm < 0 || rs2 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // FSQRT.S
			a := c.GetFPRegF32(rs1)
			result := softfp.SqrtF32(a, convertRM(rm), &flags)
			c.SetFPRegF32(rd, result)
		case 1: // FSQRT.D
			result := softfp.SqrtF64(c.FPReg[rs1], convertRM(rm), &flags)
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x04: // FSGNJ/FSGNJN/FSGNJX
		switch fmt {
		case 0: // F32
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			var result uint32
			switch rmBits {
			case 0: // FSGNJ.S
				result = softfp.SignF32(a, b)
			case 1: // FSGNJN.S
				result = softfp.SignNF32(a, b)
			case 2: // FSGNJX.S
				result = softfp.SignXF32(a, b)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.SetFPRegF32(rd, result)
		case 1: // F64
			a := c.FPReg[rs1]
			b := c.FPReg[rs2]
			var result uint64
			switch rmBits {
			case 0: // FSGNJ.D
				result = softfp.SignF64(a, b)
			case 1: // FSGNJN.D
				result = softfp.SignNF64(a, b)
			case 2: // FSGNJX.D
				result = softfp.SignXF64(a, b)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x05: // FMIN/FMAX
		switch fmt {
		case 0: // F32
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			var result uint32
			switch rmBits {
			case 0: // FMIN.S
				result = softfp.MinF32(a, b, &flags, softfp.MinMaxIEEE754_201X)
			case 1: // FMAX.S
				result = softfp.MaxF32(a, b, &flags, softfp.MinMaxIEEE754_201X)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.SetFPRegF32(rd, result)
		case 1: // F64
			a := c.FPReg[rs1]
			b := c.FPReg[rs2]
			var result uint64
			switch rmBits {
			case 0: // FMIN.D
				result = softfp.MinF64(a, b, &flags, softfp.MinMaxIEEE754_201X)
			case 1: // FMAX.D
				result = softfp.MaxF64(a, b, &flags, softfp.MinMaxIEEE754_201X)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x08: // FCVT between float formats
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // Convert TO F32
			switch rs2 {
			case 1: // FCVT.S.D
				result := softfp.CvtF64ToF32(c.FPReg[rs1], convertRM(rm), &flags)
				c.SetFPRegF32(rd, result)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		case 1: // Convert TO F64
			switch rs2 {
			case 0: // FCVT.D.S
				result := softfp.CvtF32ToF64(c.GetFPRegF32(rs1), &flags)
				c.FPReg[rd] = result
				c.FS = FSDirty
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x14: // FLE/FLT/FEQ - comparisons (write to integer register)
		var val uint64
		switch fmt {
		case 0: // F32
			a := c.GetFPRegF32(rs1)
			b := c.GetFPRegF32(rs2)
			switch rmBits {
			case 0: // FLE.S
				if softfp.LeF32(a, b, &flags) {
					val = 1
				}
			case 1: // FLT.S
				if softfp.LtF32(a, b, &flags) {
					val = 1
				}
			case 2: // FEQ.S
				if softfp.EqQuietF32(a, b, &flags) {
					val = 1
				}
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		case 1: // F64
			a := c.FPReg[rs1]
			b := c.FPReg[rs2]
			switch rmBits {
			case 0: // FLE.D
				if softfp.LeF64(a, b, &flags) {
					val = 1
				}
			case 1: // FLT.D
				if softfp.LtF64(a, b, &flags) {
					val = 1
				}
			case 2: // FEQ.D
				if softfp.EqQuietF64(a, b, &flags) {
					val = 1
				}
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		c.SetReg(rd, val)

	case 0x18: // FCVT float to integer (write to integer register)
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		// FCVT.L/LU.S/D (rs2 = 2/3) yield a 64-bit integer: RV64 only.
		if (rs2 == 2 || rs2 == 3) && c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		var val uint64
		switch fmt {
		case 0: // From F32
			a := c.GetFPRegF32(rs1)
			switch rs2 {
			case 0: // FCVT.W.S
				val = uint64(int64(int32(softfp.CvtF32ToI32(a, convertRM(rm), &flags))))
			case 1: // FCVT.WU.S
				val = uint64(int64(int32(softfp.CvtF32ToU32(a, convertRM(rm), &flags))))
			case 2: // FCVT.L.S
				val = uint64(softfp.CvtF32ToI64(a, convertRM(rm), &flags))
			case 3: // FCVT.LU.S
				val = softfp.CvtF32ToU64(a, convertRM(rm), &flags)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		case 1: // From F64
			a := c.FPReg[rs1]
			switch rs2 {
			case 0: // FCVT.W.D
				val = uint64(int64(int32(softfp.CvtF64ToI32(a, convertRM(rm), &flags))))
			case 1: // FCVT.WU.D
				val = uint64(int64(int32(softfp.CvtF64ToU32(a, convertRM(rm), &flags))))
			case 2: // FCVT.L.D
				val = uint64(softfp.CvtF64ToI64(a, convertRM(rm), &flags))
			case 3: // FCVT.LU.D
				val = softfp.CvtF64ToU64(a, convertRM(rm), &flags)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		c.SetReg(rd, val)

	case 0x1A: // FCVT integer to float
		rm := c.getInsnRM(rmBits)
		if rm < 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		// FCVT.S/D.L/LU (rs2 = 2/3) source a 64-bit integer: RV64 only.
		if (rs2 == 2 || rs2 == 3) && c.CurXLEN == XLEN32 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // To F32
			var result uint32
			switch rs2 {
			case 0: // FCVT.S.W
				result = softfp.CvtI32ToF32(int32(c.GetReg(rs1)), convertRM(rm), &flags)
			case 1: // FCVT.S.WU
				result = softfp.CvtU32ToF32(uint32(c.GetReg(rs1)), convertRM(rm), &flags)
			case 2: // FCVT.S.L
				result = softfp.CvtI64ToF32(int64(c.GetReg(rs1)), convertRM(rm), &flags)
			case 3: // FCVT.S.LU
				result = softfp.CvtU64ToF32(c.GetReg(rs1), convertRM(rm), &flags)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.SetFPRegF32(rd, result)
		case 1: // To F64
			var result uint64
			switch rs2 {
			case 0: // FCVT.D.W
				result = softfp.CvtI32ToF64(int32(c.GetReg(rs1)), convertRM(rm), &flags)
			case 1: // FCVT.D.WU
				result = softfp.CvtU32ToF64(uint32(c.GetReg(rs1)), convertRM(rm), &flags)
			case 2: // FCVT.D.L
				result = softfp.CvtI64ToF64(int64(c.GetReg(rs1)), convertRM(rm), &flags)
			case 3: // FCVT.D.LU
				result = softfp.CvtU64ToF64(c.GetReg(rs1), convertRM(rm), &flags)
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.FPReg[rd] = result
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x1C: // FMV.X.W/D or FCLASS
		if rs2 != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch rmBits {
		case 0: // FMV.X.W or FMV.X.D (move FP to integer register)
			switch fmt {
			case 0: // FMV.X.W
				// Get raw 32-bit value, sign-extend to 64 bits
				val := int32(c.FPReg[rs1])
				c.SetReg(rd, uint64(int64(val)))
			case 1: // FMV.X.D — RV64 only
				if c.CurXLEN == XLEN32 {
					c.SetPendingException(CauseIllegalInsn, uint64(insn))
					return c.handleException()
				}
				c.SetReg(rd, c.FPReg[rs1])
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
		case 1: // FCLASS
			var val uint64
			switch fmt {
			case 0: // FCLASS.S
				val = uint64(softfp.FClass32(c.GetFPRegF32(rs1)))
			case 1: // FCLASS.D
				val = uint64(softfp.FClass64(c.FPReg[rs1]))
			default:
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.SetReg(rd, val)
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	case 0x1E: // FMV.W.X or FMV.D.X (move integer to FP register)
		if rs2 != 0 || rmBits != 0 {
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}
		switch fmt {
		case 0: // FMV.W.X
			// NaN-box the 32-bit value
			c.FPReg[rd] = F32High | uint64(uint32(c.GetReg(rs1)))
			c.FS = FSDirty
		case 1: // FMV.D.X — RV64 only
			if c.CurXLEN == XLEN32 {
				c.SetPendingException(CauseIllegalInsn, uint64(insn))
				return c.handleException()
			}
			c.FPReg[rd] = c.GetReg(rs1)
			c.FS = FSDirty
		default:
			c.SetPendingException(CauseIllegalInsn, uint64(insn))
			return c.handleException()
		}

	default:
		c.SetPendingException(CauseIllegalInsn, uint64(insn))
		return c.handleException()
	}

	c.updateFFlags(flags)
	c.PC += 4
	return nil
}
