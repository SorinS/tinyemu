package x86

import "fmt"

// reg16FromModRM converts a 3-bit ModR/M register field to a 16-bit register index.
func reg16FromModRM(r int) int { return r << 1 }

// reg8FromModRM converts a 3-bit ModR/M register field to an 8-bit register index.
func reg8FromModRM(r int) int { return (r&3)<<2 | (r >> 2) }

// modRMResult holds the result of parsing a ModR/M byte.
type modRMResult struct {
	mod   uint8
	reg   uint8
	rm    uint8
	disp  uint32
	isReg bool
	ea    uint32 // effective address for memory operands
	// defaultSeg is the segment register implied by the encoding when no
	// override prefix is present. For most encodings this is DS, but for
	// addressing modes that use EBP/ESP as a base it is SS. Callers should
	// use c.segBaseForModRM(mr) so that segment-override prefixes (CS/SS/
	// DS/ES/FS/GS) are honored.
	defaultSeg int
}

// parseModRM32 parses a ModR/M byte for 32-bit addressing.
// It reads the ModR/M byte, optional SIB, and displacement from the code stream.
func (c *CPU) parseModRM32() modRMResult {
	modrm := c.fetch8()
	mod := (modrm >> 6) & 3
	reg := (modrm >> 3) & 7
	rm := modrm & 7

	res := modRMResult{
		mod:        mod,
		reg:        reg,
		rm:         rm,
		isReg:      mod == 3,
		defaultSeg: DS,
	}

	if mod == 3 {
		// Register operand
		return res
	}

	// Memory operand
	var disp uint32
	if rm == 4 {
		// SIB byte present
		sib := c.fetch8()
		scale := uint32(1 << ((sib >> 6) & 3))
		index := (sib >> 3) & 7
		base := sib & 7

		var ea uint32
		if base == 5 && mod == 0 {
			ea = c.fetch32()
		} else {
			ea = c.GetReg32(int(base))
			// Per Intel SDM: when SIB base is ESP (4) or EBP (5, mod != 0),
			// the default segment is SS, not DS.
			if base == 4 || base == 5 {
				res.defaultSeg = SS
			}
		}
		if index != 4 {
			ea += c.GetReg32(int(index)) * scale
		}
		disp = ea
	} else if rm == 5 && mod == 0 {
		// disp32 only
		disp = c.fetch32()
	} else {
		disp = c.GetReg32(int(rm))
		// rm == 5 with mod != 0 means [EBP+disp]: default segment is SS.
		if rm == 5 {
			res.defaultSeg = SS
		}
	}

	if mod == 1 {
		disp += uint32(int32(int8(c.fetch8())))
	} else if mod == 2 {
		disp += c.fetch32()
	}

	res.disp = disp
	res.ea = disp
	return res
}

// parseModRM16 parses a ModR/M byte for 16-bit addressing.
func (c *CPU) parseModRM16() modRMResult {
	modrm := c.fetch8()
	mod := (modrm >> 6) & 3
	reg := (modrm >> 3) & 7
	rm := modrm & 7

	res := modRMResult{
		mod:        mod,
		reg:        reg,
		rm:         rm,
		isReg:      mod == 3,
		defaultSeg: DS,
	}

	if mod == 3 {
		return res
	}

	// 16-bit effective address calculation
	var ea uint32
	switch rm {
	case 0:
		ea = uint32(c.GetReg16(BX) + c.GetReg16(SI))
	case 1:
		ea = uint32(c.GetReg16(BX) + c.GetReg16(DI))
	case 2:
		ea = uint32(c.GetReg16(BP) + c.GetReg16(SI))
		res.defaultSeg = SS
	case 3:
		ea = uint32(c.GetReg16(BP) + c.GetReg16(DI))
		res.defaultSeg = SS
	case 4:
		ea = uint32(c.GetReg16(SI))
	case 5:
		ea = uint32(c.GetReg16(DI))
	case 6:
		if mod == 0 {
			ea = uint32(c.fetch16())
		} else {
			ea = uint32(c.GetReg16(BP))
			res.defaultSeg = SS
		}
	case 7:
		ea = uint32(c.GetReg16(BX))
	}

	if mod == 1 {
		ea += uint32(int32(int8(c.fetch8())))
	} else if mod == 2 {
		ea += uint32(c.fetch16())
	}

	res.ea = ea
	res.disp = ea
	return res
}

// parseModRM parses a ModR/M byte using the current address size.
func (c *CPU) parseModRM() modRMResult {
	if c.currentAddrSize == 4 {
		return c.parseModRM32()
	}
	return c.parseModRM16()
}

// segBaseForModRM returns the segment base that should be used for the memory
// operand described by mr. If a segment-override prefix is active for the
// current instruction (currentSegOverride != -1) that wins; otherwise the
// instruction's encoded default segment (DS for most cases, SS for EBP/ESP-
// based addressing modes) is used.
func (c *CPU) segBaseForModRM(mr modRMResult) uint32 {
	if c.currentSegOverride >= 0 {
		return c.segBase[c.currentSegOverride]
	}
	return c.segBase[mr.defaultSeg]
}

// handleModRM32 handles MOV r/m32, r32 (0x89) and MOV r32, r/m32 (0x8B).
func (c *CPU) handleModRM32(opcode uint8) error {
	mr := c.parseModRM()
	if mr.isReg {
		if opcode == 0x89 {
			c.SetReg32(int(mr.rm), c.GetReg32(int(mr.reg)))
		} else {
			c.SetReg32(int(mr.reg), c.GetReg32(int(mr.rm)))
		}
	} else {
		addr := c.segBaseForModRM(mr) + mr.ea
		if opcode == 0x89 {
			c.writeMem32(addr, c.GetReg32(int(mr.reg)))
		} else {
			c.SetReg32(int(mr.reg), c.readMem32(addr))
		}
	}
	return nil
}

// handleModRM16 handles MOV r/m16, r16 (0x89) and MOV r16, r/m16 (0x8B).
func (c *CPU) handleModRM16(opcode uint8) error {
	mr := c.parseModRM()
	if mr.isReg {
		if opcode == 0x89 {
			c.SetReg16(reg16FromModRM(int(mr.rm)), c.GetReg16(reg16FromModRM(int(mr.reg))))
		} else {
			c.SetReg16(reg16FromModRM(int(mr.reg)), c.GetReg16(reg16FromModRM(int(mr.rm))))
		}
	} else {
		addr := c.segBaseForModRM(mr) + mr.ea
		if opcode == 0x89 {
			c.writeMem16(addr, c.GetReg16(reg16FromModRM(int(mr.reg))))
		} else {
			c.SetReg16(reg16FromModRM(int(mr.reg)), c.readMem16(addr))
		}
	}
	return nil
}

// handleModRM8 handles MOV r/m8, r8 (0x88) and MOV r8, r/m8 (0x8A).
func (c *CPU) handleModRM8(opcode uint8) error {
	mr := c.parseModRM()
	if mr.isReg {
		if opcode == 0x88 {
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.GetReg8(reg8FromModRM(int(mr.reg))))
		} else {
			c.SetReg8(reg8FromModRM(int(mr.reg)), c.GetReg8(reg8FromModRM(int(mr.rm))))
		}
	} else {
		addr := c.segBaseForModRM(mr) + mr.ea
		if opcode == 0x88 {
			c.writeMem8(addr, c.GetReg8(reg8FromModRM(int(mr.reg))))
		} else {
			c.SetReg8(reg8FromModRM(int(mr.reg)), c.readMem8(addr))
		}
	}
	return nil
}

// handleMovImm handles MOV r/m16/32, imm16/imm32 (C7 /0).
// The caller must pass the current operand size (2 or 4).
func (c *CPU) handleMovImm(operandSize uint8) error {
	mr := c.parseModRM()
	if mr.reg != 0 {
		return fmt.Errorf("C7: unsupported reg field %d", mr.reg)
	}
	if operandSize == 2 {
		imm := c.fetch16()
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), imm)
		} else {
			c.writeMem16(c.segBaseForModRM(mr) + mr.ea, imm)
		}
	} else {
		imm := c.fetch32()
		if mr.isReg {
			c.SetReg32(int(mr.rm), imm)
		} else {
			c.writeMem32(c.segBaseForModRM(mr) + mr.ea, imm)
		}
	}
	return nil
}

// handleGroup1_8 handles 80 /n r/m8, imm8.
func (c *CPU) handleGroup1_8() error {
	mr := c.parseModRM()
	imm := c.fetch8()
	var res uint8
	if mr.isReg {
		dst := c.GetReg8(reg8FromModRM(int(mr.rm)))
		switch mr.reg {
		case 0: // ADD
			res = c.add8(dst, imm)
		case 1: // OR
			res = c.or8(dst, imm)
		case 2: // ADC
			res = c.adc8(dst, imm)
		case 3: // SBB
			res = c.sbb8(dst, imm)
		case 4: // AND
			res = c.and8(dst, imm)
		case 5: // SUB
			res = c.sub8(dst, imm)
		case 6: // XOR
			res = c.xor8(dst, imm)
		case 7: // CMP
			c.sub8(dst, imm)
			return nil
		}
		c.SetReg8(reg8FromModRM(int(mr.rm)), res)
	} else {
		addr := c.segBaseForModRM(mr) + mr.ea
		dst := c.readMem8(addr)
		switch mr.reg {
		case 0: // ADD
			res = c.add8(dst, imm)
		case 1: // OR
			res = c.or8(dst, imm)
		case 2: // ADC
			res = c.adc8(dst, imm)
		case 3: // SBB
			res = c.sbb8(dst, imm)
		case 4: // AND
			res = c.and8(dst, imm)
		case 5: // SUB
			res = c.sub8(dst, imm)
		case 6: // XOR
			res = c.xor8(dst, imm)
		case 7: // CMP
			c.sub8(dst, imm)
			return nil
		}
		c.writeMem8(addr, res)
	}
	return nil
}

// handleGroup1_16 handles 81 /n r/m16, imm16.
func (c *CPU) handleGroup1_16() error {
	mr := c.parseModRM()
	imm := c.fetch16()
	return c.handleGroup1Op16(mr, imm)
}

// handleGroup1_32 handles 81 /n r/m32, imm32.
func (c *CPU) handleGroup1_32() error {
	mr := c.parseModRM()
	imm := c.fetch32()
	return c.handleGroup1Op32(mr, imm)
}

// handleGroup1_8x handles 83 /n r/m16/32, imm8 (sign-extended).
func (c *CPU) handleGroup1_8x(operandSize uint8) error {
	mr := c.parseModRM()
	imm8 := c.fetchS8()
	if operandSize == 2 {
		return c.handleGroup1Op16(mr, uint16(int16(imm8)))
	}
	return c.handleGroup1Op32(mr, uint32(int32(imm8)))
}

func (c *CPU) handleGroup1Op16(mr modRMResult, imm uint16) error {
	var dst uint16
	if mr.isReg {
		dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
	} else {
		dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
	}
	var res uint16
	switch mr.reg {
	case 0: // ADD
		res = c.add16(dst, imm)
	case 1: // OR
		res = c.or16(dst, imm)
	case 2: // ADC
		res = c.adc16(dst, imm)
	case 3: // SBB
		res = c.sbb16(dst, imm)
	case 4: // AND
		res = c.and16(dst, imm)
	case 5: // SUB
		res = c.sub16(dst, imm)
	case 6: // XOR
		res = c.xor16(dst, imm)
	case 7: // CMP
		c.sub16(dst, imm)
		return nil
	default:
		return fmt.Errorf("group1 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg16(reg16FromModRM(int(mr.rm)), res)
	} else {
		c.writeMem16(c.segBaseForModRM(mr) + mr.ea, res)
	}
	return nil
}

func (c *CPU) handleGroup1Op32(mr modRMResult, imm uint32) error {
	var dst uint32
	if mr.isReg {
		dst = c.GetReg32(int(mr.rm))
	} else {
		dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
	}
	var res uint32
	switch mr.reg {
	case 0: // ADD
		res = c.add32(dst, imm)
	case 1: // OR
		res = c.or32(dst, imm)
	case 2: // ADC
		res = c.adc32(dst, imm)
	case 3: // SBB
		res = c.sbb32(dst, imm)
	case 4: // AND
		res = c.and32(dst, imm)
	case 5: // SUB
		res = c.sub32(dst, imm)
	case 6: // XOR
		res = c.xor32(dst, imm)
	case 7: // CMP
		c.sub32(dst, imm)
		return nil
	default:
		return fmt.Errorf("group1 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg32(int(mr.rm), res)
	} else {
		c.writeMem32(c.segBaseForModRM(mr) + mr.ea, res)
	}
	return nil
}

// handleJcc8 handles conditional near jumps (70-7F rel8).
func (c *CPU) handleJcc8(opcode uint8) error {
	off := int32(c.fetchS8())
	cond := false
	switch opcode {
	case 0x70: // JO
		cond = c.getOF()
	case 0x71: // JNO
		cond = !c.getOF()
	case 0x72: // JB/JC/JNAE
		cond = c.getCF()
	case 0x73: // JNB/JNC/JAE
		cond = !c.getCF()
	case 0x74: // JE/JZ
		cond = c.getZF()
	case 0x75: // JNE/JNZ
		cond = !c.getZF()
	case 0x76: // JBE/JNA
		cond = c.getCF() || c.getZF()
	case 0x77: // JNBE/JA
		cond = !c.getCF() && !c.getZF()
	case 0x78: // JS
		cond = c.getSF()
	case 0x79: // JNS
		cond = !c.getSF()
	case 0x7A: // JP/JPE
		cond = c.getPF()
	case 0x7B: // JNP/JPO
		cond = !c.getPF()
	case 0x7C: // JL/JNGE
		cond = c.getSF() != c.getOF()
	case 0x7D: // JNL/JGE
		cond = c.getSF() == c.getOF()
	case 0x7E: // JLE/JNG
		cond = c.getZF() || (c.getSF() != c.getOF())
	case 0x7F: // JNLE/JG
		cond = !c.getZF() && (c.getSF() == c.getOF())
	}
	if cond {
		c.eip = uint32(int32(c.eip) + off)
	}
	return nil
}

// handleJccNear handles conditional near jumps (0F 80-8F rel16/rel32).
func (c *CPU) handleJccNear(opcode2 uint8, operandSize uint8) error {
	var off int32
	if operandSize == 2 {
		off = int32(c.fetchS16())
	} else {
		off = c.fetchS32()
	}
	cond := false
	switch opcode2 {
	case 0x80: // JO
		cond = c.getOF()
	case 0x81: // JNO
		cond = !c.getOF()
	case 0x82: // JB/JC/JNAE
		cond = c.getCF()
	case 0x83: // JNB/JNC/JAE
		cond = !c.getCF()
	case 0x84: // JE/JZ
		cond = c.getZF()
	case 0x85: // JNE/JNZ
		cond = !c.getZF()
	case 0x86: // JBE/JNA
		cond = c.getCF() || c.getZF()
	case 0x87: // JNBE/JA
		cond = !c.getCF() && !c.getZF()
	case 0x88: // JS
		cond = c.getSF()
	case 0x89: // JNS
		cond = !c.getSF()
	case 0x8A: // JP/JPE
		cond = c.getPF()
	case 0x8B: // JNP/JPO
		cond = !c.getPF()
	case 0x8C: // JL/JNGE
		cond = c.getSF() != c.getOF()
	case 0x8D: // JNL/JGE
		cond = c.getSF() == c.getOF()
	case 0x8E: // JLE/JNG
		cond = c.getZF() || (c.getSF() != c.getOF())
	case 0x8F: // JNLE/JG
		cond = !c.getZF() && (c.getSF() == c.getOF())
	}
	if cond {
		c.eip = uint32(int32(c.eip) + off)
	}
	return nil
}

// EFLAGS condition helpers.
func (c *CPU) getCF() bool { return c.eflags&EFLAGS_CF != 0 }
func (c *CPU) getOF() bool { return c.eflags&EFLAGS_OF != 0 }
func (c *CPU) getSF() bool { return c.eflags&EFLAGS_SF != 0 }
func (c *CPU) getZF() bool { return c.eflags&EFLAGS_ZF != 0 }
func (c *CPU) getPF() bool { return c.eflags&EFLAGS_PF != 0 }
func (c *CPU) getAF() bool { return c.eflags&EFLAGS_AF != 0 }

// handleCMOVcc handles conditional 32/16-bit moves (0F 40..0F 4F).
// The destination is updated with the source only if the condition holds.
func (c *CPU) handleCMOVcc(opcode2 uint8, operandSize uint8) error {
	mr := c.parseModRM()
	cond := false
	switch opcode2 {
	case 0x40: // CMOVO
		cond = c.getOF()
	case 0x41: // CMOVNO
		cond = !c.getOF()
	case 0x42: // CMOVB/CMOVC/CMOVNAE
		cond = c.getCF()
	case 0x43: // CMOVNB/CMOVNC/CMOVAE
		cond = !c.getCF()
	case 0x44: // CMOVE/CMOVZ
		cond = c.getZF()
	case 0x45: // CMOVNE/CMOVNZ
		cond = !c.getZF()
	case 0x46: // CMOVBE/CMOVNA
		cond = c.getCF() || c.getZF()
	case 0x47: // CMOVNBE/CMOVA
		cond = !c.getCF() && !c.getZF()
	case 0x48: // CMOVS
		cond = c.getSF()
	case 0x49: // CMOVNS
		cond = !c.getSF()
	case 0x4A: // CMOVP/CMOVPE
		cond = c.getPF()
	case 0x4B: // CMOVNP/CMOVPO
		cond = !c.getPF()
	case 0x4C: // CMOVL/CMOVNGE
		cond = c.getSF() != c.getOF()
	case 0x4D: // CMOVNL/CMOVGE
		cond = c.getSF() == c.getOF()
	case 0x4E: // CMOVLE/CMOVNG
		cond = c.getZF() || (c.getSF() != c.getOF())
	case 0x4F: // CMOVNLE/CMOVG
		cond = !c.getZF() && (c.getSF() == c.getOF())
	}
	if operandSize == 2 {
		var src uint16
		if mr.isReg {
			src = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			src = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		if cond {
			c.SetReg16(reg16FromModRM(int(mr.reg)), src)
		}
	} else {
		var src uint32
		if mr.isReg {
			src = c.GetReg32(int(mr.rm))
		} else {
			src = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
		}
		if cond {
			c.SetReg32(int(mr.reg), src)
		}
	}
	return nil
}

// handleLoadFarPointer implements LSS/LFS/LGS r16/32, m16:32 (and the older
// LES/LDS via direct call). The instruction loads `seg:offset` from memory
// into the named segment register and the destination GPR.
func (c *CPU) handleLoadFarPointer(segReg int, operandSize uint8) error {
	mr := c.parseModRM()
	if mr.isReg {
		// Source must be memory.
		return fmt.Errorf("LSS/LFS/LGS with register source")
	}
	addr := c.segBaseForModRM(mr) + mr.ea
	if operandSize == 2 {
		off := c.readMem16(addr)
		sel := c.readMem16(addr + 2)
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(segReg, sel); err != nil {
				return err
			}
		} else {
			c.seg[segReg] = sel
			c.segBase[segReg] = uint32(sel) << 4
		}
		c.SetReg16(reg16FromModRM(int(mr.reg)), off)
	} else {
		off := c.readMem32(addr)
		sel := c.readMem16(addr + 4)
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(segReg, sel); err != nil {
				return err
			}
		} else {
			c.seg[segReg] = sel
			c.segBase[segReg] = uint32(sel) << 4
		}
		c.SetReg32(int(mr.reg), off)
	}
	if segReg == SS {
		c.interruptsBlocked = true
	}
	return nil
}

// handleSETcc handles SETcc r/m8 (0F 90-0F 9F).
func (c *CPU) handleSETcc(opcode2 uint8) error {
	mr := c.parseModRM()
	cond := false
	switch opcode2 {
	case 0x90: // SETO
		cond = c.getOF()
	case 0x91: // SETNO
		cond = !c.getOF()
	case 0x92: // SETB/SETC/SETNAE
		cond = c.getCF()
	case 0x93: // SETNB/SETNC/SETAE
		cond = !c.getCF()
	case 0x94: // SETE/SETZ
		cond = c.getZF()
	case 0x95: // SETNE/SETNZ
		cond = !c.getZF()
	case 0x96: // SETBE/SETNA
		cond = c.getCF() || c.getZF()
	case 0x97: // SETNBE/SETA
		cond = !c.getCF() && !c.getZF()
	case 0x98: // SETS
		cond = c.getSF()
	case 0x99: // SETNS
		cond = !c.getSF()
	case 0x9A: // SETP/SETPE
		cond = c.getPF()
	case 0x9B: // SETNP/SETPO
		cond = !c.getPF()
	case 0x9C: // SETL/SETNGE
		cond = c.getSF() != c.getOF()
	case 0x9D: // SETNL/SETGE
		cond = c.getSF() == c.getOF()
	case 0x9E: // SETLE/SETNG
		cond = c.getZF() || (c.getSF() != c.getOF())
	case 0x9F: // SETNLE/SETG
		cond = !c.getZF() && (c.getSF() == c.getOF())
	}
	var val uint8
	if cond {
		val = 1
	}
	if mr.isReg {
		c.SetReg8(reg8FromModRM(int(mr.rm)), val)
	} else {
		c.writeMem8(c.segBaseForModRM(mr) + mr.ea, val)
	}
	return nil
}
