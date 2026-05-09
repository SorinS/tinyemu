package x86

import "fmt"

// handleGroup2_8 handles C0 /n r/m8, imm8 (shifts/rotates).
func (c *CPU) handleGroup2_8() error {
	mr := c.parseModRM()
	count := c.fetch8()
	var r uint8
	if mr.isReg {
		r = c.GetReg8(reg8FromModRM(int(mr.rm)))
	} else {
		r = c.readMem8(c.segBase[DS] + mr.ea)
	}
	switch mr.reg {
	case 0:
		r = c.rol8(r, count)
	case 1:
		r = c.ror8(r, count)
	case 4:
		r = c.shl8(r, count)
	case 5:
		r = c.shr8(r, count)
	case 7:
		r = c.sar8(r, count)
	default:
		return fmt.Errorf("C0 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg8(reg8FromModRM(int(mr.rm)), r)
	} else {
		c.writeMem8(c.segBase[DS]+mr.ea, r)
	}
	return nil
}

// handleGroup2_32 handles C1 /n r/m32, imm8.
func (c *CPU) handleGroup2_32() error {
	mr := c.parseModRM()
	count := c.fetch8()
	var r uint32
	if mr.isReg {
		r = c.GetReg32(int(mr.rm))
	} else {
		r = c.readMem32(c.segBase[DS] + mr.ea)
	}
	switch mr.reg {
	case 0:
		r = c.rol32(r, count)
	case 1:
		r = c.ror32(r, count)
	case 4:
		r = c.shl32(r, count)
	case 5:
		r = c.shr32(r, count)
	case 7:
		r = c.sar32(r, count)
	default:
		return fmt.Errorf("C1 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg32(int(mr.rm), r)
	} else {
		c.writeMem32(c.segBase[DS]+mr.ea, r)
	}
	return nil
}

// handleGroup2_8Count handles D0-D2 /n r/m8, count.
func (c *CPU) handleGroup2_8Count(count uint8) error {
	mr := c.parseModRM()
	var r uint8
	if mr.isReg {
		r = c.GetReg8(reg8FromModRM(int(mr.rm)))
	} else {
		r = c.readMem8(c.segBase[DS] + mr.ea)
	}
	switch mr.reg {
	case 0:
		r = c.rol8(r, count)
	case 1:
		r = c.ror8(r, count)
	case 4:
		r = c.shl8(r, count)
	case 5:
		r = c.shr8(r, count)
	case 7:
		r = c.sar8(r, count)
	default:
		return fmt.Errorf("D0-D2 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg8(reg8FromModRM(int(mr.rm)), r)
	} else {
		c.writeMem8(c.segBase[DS]+mr.ea, r)
	}
	return nil
}

// handleGroup2_32Count handles D1-D3 /n r/m32, count.
func (c *CPU) handleGroup2_32Count(count uint8) error {
	mr := c.parseModRM()
	var r uint32
	if mr.isReg {
		r = c.GetReg32(int(mr.rm))
	} else {
		r = c.readMem32(c.segBase[DS] + mr.ea)
	}
	switch mr.reg {
	case 0:
		r = c.rol32(r, count)
	case 1:
		r = c.ror32(r, count)
	case 4:
		r = c.shl32(r, count)
	case 5:
		r = c.shr32(r, count)
	case 7:
		r = c.sar32(r, count)
	default:
		return fmt.Errorf("D1-D3 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg32(int(mr.rm), r)
	} else {
		c.writeMem32(c.segBase[DS]+mr.ea, r)
	}
	return nil
}

// handleGroup2_16 handles C1 /n r/m16, imm8.
func (c *CPU) handleGroup2_16() error {
	mr := c.parseModRM()
	count := c.fetch8()
	var r uint16
	if mr.isReg {
		r = c.GetReg16(reg16FromModRM(int(mr.rm)))
	} else {
		r = c.readMem16(c.segBase[DS] + mr.ea)
	}
	switch mr.reg {
	case 0:
		r = c.rol16(r, count)
	case 1:
		r = c.ror16(r, count)
	case 4:
		r = c.shl16(r, count)
	case 5:
		r = c.shr16(r, count)
	case 7:
		r = c.sar16(r, count)
	default:
		return fmt.Errorf("C1 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg16(reg16FromModRM(int(mr.rm)), r)
	} else {
		c.writeMem16(c.segBase[DS]+mr.ea, r)
	}
	return nil
}

// handleGroup2_16Count handles D1-D3 /n r/m16, count.
func (c *CPU) handleGroup2_16Count(count uint8) error {
	mr := c.parseModRM()
	var r uint16
	if mr.isReg {
		r = c.GetReg16(reg16FromModRM(int(mr.rm)))
	} else {
		r = c.readMem16(c.segBase[DS] + mr.ea)
	}
	switch mr.reg {
	case 0:
		r = c.rol16(r, count)
	case 1:
		r = c.ror16(r, count)
	case 4:
		r = c.shl16(r, count)
	case 5:
		r = c.shr16(r, count)
	case 7:
		r = c.sar16(r, count)
	default:
		return fmt.Errorf("D1-D3 /%d not implemented", mr.reg)
	}
	if mr.isReg {
		c.SetReg16(reg16FromModRM(int(mr.rm)), r)
	} else {
		c.writeMem16(c.segBase[DS]+mr.ea, r)
	}
	return nil
}

// handleGroup3_16 handles F7 /n r/m16.
func (c *CPU) handleGroup3_16() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // TEST r/m16, imm16
		imm := c.fetch16()
		if mr.isReg {
			c.and16(c.GetReg16(reg16FromModRM(int(mr.rm))), imm)
		} else {
			c.and16(c.readMem16(c.segBase[DS]+mr.ea), imm)
		}
	case 2: // NOT r/m16
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), c.not16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem16(addr, c.not16(c.readMem16(addr)))
		}
	case 3: // NEG r/m16
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), c.neg16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem16(addr, c.neg16(c.readMem16(addr)))
		}
	case 4: // MUL AX, r/m16
		var v uint16
		if mr.isReg {
			v = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			v = c.readMem16(c.segBase[DS] + mr.ea)
		}
		res := uint32(c.GetReg16(AX)) * uint32(v)
		c.SetReg16(AX, uint16(res))
		c.SetReg16(DX, uint16(res>>16))
		c.setOF(res > 0xFFFF)
		c.setCF(res > 0xFFFF)
	case 5: // IMUL AX, r/m16
		var v int16
		if mr.isReg {
			v = int16(c.GetReg16(reg16FromModRM(int(mr.rm))))
		} else {
			v = int16(c.readMem16(c.segBase[DS] + mr.ea))
		}
		c.imul16(v)
	case 6: // DIV AX, r/m16
		var v uint16
		if mr.isReg {
			v = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			v = c.readMem16(c.segBase[DS] + mr.ea)
		}
		c.div16(v)
	case 7: // IDIV AX, r/m16
		var v int16
		if mr.isReg {
			v = int16(c.GetReg16(reg16FromModRM(int(mr.rm))))
		} else {
			v = int16(c.readMem16(c.segBase[DS] + mr.ea))
		}
		c.idiv16(v)
	default:
		return fmt.Errorf("F7 /%d not implemented", mr.reg)
	}
	return nil
}

// handleGroup5_16 handles FF /n r/m16.
func (c *CPU) handleGroup5_16() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // INC r/m16
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), c.inc16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem16(addr, c.inc16(c.readMem16(addr)))
		}
	case 1: // DEC r/m16
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), c.dec16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem16(addr, c.dec16(c.readMem16(addr)))
		}
	case 2: // CALL r/m16
		var target uint16
		if mr.isReg {
			target = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			target = c.readMem16(c.segBase[DS] + mr.ea)
		}
		c.push16(uint16(c.eip))
		c.eip = uint32(target)
	case 4: // JMP r/m16
		if mr.isReg {
			c.eip = uint32(c.GetReg16(reg16FromModRM(int(mr.rm))))
		} else {
			c.eip = uint32(c.readMem16(c.segBase[DS] + mr.ea))
		}
	case 6: // PUSH r/m16
		if mr.isReg {
			c.push16(c.GetReg16(reg16FromModRM(int(mr.rm))))
		} else {
			c.push16(c.readMem16(c.segBase[DS] + mr.ea))
		}
	default:
		return fmt.Errorf("FF /%d not implemented", mr.reg)
	}
	return nil
}

// handleGroup3_8 handles F6 /n r/m8.
func (c *CPU) handleGroup3_8() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // TEST r/m8, imm8
		imm := c.fetch8()
		if mr.isReg {
			c.and8(c.GetReg8(reg8FromModRM(int(mr.rm))), imm)
		} else {
			c.and8(c.readMem8(c.segBase[DS]+mr.ea), imm)
		}
	case 2: // NOT r/m8
		if mr.isReg {
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.not8(c.GetReg8(reg8FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem8(addr, c.not8(c.readMem8(addr)))
		}
	case 3: // NEG r/m8
		if mr.isReg {
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.neg8(c.GetReg8(reg8FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem8(addr, c.neg8(c.readMem8(addr)))
		}
	case 4: // MUL AL, r/m8
		var v uint8
		if mr.isReg {
			v = c.GetReg8(reg8FromModRM(int(mr.rm)))
		} else {
			v = c.readMem8(c.segBase[DS] + mr.ea)
		}
		res := uint16(c.GetReg8(AL)) * uint16(v)
		c.SetReg16(AX, res)
		c.setOF(res > 0xFF)
		c.setCF(res > 0xFF)
	case 5: // IMUL AL, r/m8
		var v int8
		if mr.isReg {
			v = int8(c.GetReg8(reg8FromModRM(int(mr.rm))))
		} else {
			v = int8(c.readMem8(c.segBase[DS] + mr.ea))
		}
		c.imul8(v)
	case 6: // DIV AL, r/m8
		var v uint8
		if mr.isReg {
			v = c.GetReg8(reg8FromModRM(int(mr.rm)))
		} else {
			v = c.readMem8(c.segBase[DS] + mr.ea)
		}
		c.div8(v)
	case 7: // IDIV AL, r/m8
		var v int8
		if mr.isReg {
			v = int8(c.GetReg8(reg8FromModRM(int(mr.rm))))
		} else {
			v = int8(c.readMem8(c.segBase[DS] + mr.ea))
		}
		c.idiv8(v)
	default:
		return fmt.Errorf("F6 /%d not implemented", mr.reg)
	}
	return nil
}

// handleGroup3_32 handles F7 /n r/m32.
func (c *CPU) handleGroup3_32() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // TEST r/m32, imm32
		imm := c.fetch32()
		if mr.isReg {
			c.and32(c.GetReg32(int(mr.rm)), imm)
		} else {
			c.and32(c.readMem32(c.segBase[DS]+mr.ea), imm)
		}
	case 2: // NOT r/m32
		if mr.isReg {
			c.SetReg32(int(mr.rm), c.not32(c.GetReg32(int(mr.rm))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem32(addr, c.not32(c.readMem32(addr)))
		}
	case 3: // NEG r/m32
		if mr.isReg {
			c.SetReg32(int(mr.rm), c.neg32(c.GetReg32(int(mr.rm))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem32(addr, c.neg32(c.readMem32(addr)))
		}
	case 4: // MUL EAX, r/m32
		var v uint32
		if mr.isReg {
			v = c.GetReg32(int(mr.rm))
		} else {
			v = c.readMem32(c.segBase[DS] + mr.ea)
		}
		res := uint64(c.GetReg32(EAX)) * uint64(v)
		c.SetReg32(EAX, uint32(res))
		c.SetReg32(EDX, uint32(res>>32))
		c.setOF(res > 0xFFFFFFFF)
		c.setCF(res > 0xFFFFFFFF)
	case 5: // IMUL EAX, r/m32
		var v int32
		if mr.isReg {
			v = int32(c.GetReg32(int(mr.rm)))
		} else {
			v = int32(c.readMem32(c.segBase[DS] + mr.ea))
		}
		c.imul32(v)
	case 6: // DIV EAX, r/m32
		var v uint32
		if mr.isReg {
			v = c.GetReg32(int(mr.rm))
		} else {
			v = c.readMem32(c.segBase[DS] + mr.ea)
		}
		c.div32(v)
	case 7: // IDIV EAX, r/m32
		var v int32
		if mr.isReg {
			v = int32(c.GetReg32(int(mr.rm)))
		} else {
			v = int32(c.readMem32(c.segBase[DS] + mr.ea))
		}
		c.idiv32(v)
	default:
		return fmt.Errorf("F7 /%d not implemented", mr.reg)
	}
	return nil
}

// handleGroup4_8 handles FE /n r/m8.
func (c *CPU) handleGroup4_8() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // INC r/m8
		if mr.isReg {
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.inc8(c.GetReg8(reg8FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem8(addr, c.inc8(c.readMem8(addr)))
		}
	case 1: // DEC r/m8
		if mr.isReg {
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.dec8(c.GetReg8(reg8FromModRM(int(mr.rm)))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem8(addr, c.dec8(c.readMem8(addr)))
		}
	default:
		return fmt.Errorf("FE /%d not implemented", mr.reg)
	}
	return nil
}

// handleGroup5_32 handles FF /n r/m32.
func (c *CPU) handleGroup5_32() error {
	mr := c.parseModRM()
	switch mr.reg {
	case 0: // INC r/m32
		if mr.isReg {
			c.SetReg32(int(mr.rm), c.inc32(c.GetReg32(int(mr.rm))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem32(addr, c.inc32(c.readMem32(addr)))
		}
	case 1: // DEC r/m32
		if mr.isReg {
			c.SetReg32(int(mr.rm), c.dec32(c.GetReg32(int(mr.rm))))
		} else {
			addr := c.segBase[DS] + mr.ea
			c.writeMem32(addr, c.dec32(c.readMem32(addr)))
		}
	case 2: // CALL r/m32
		var target uint32
		if mr.isReg {
			target = c.GetReg32(int(mr.rm))
		} else {
			target = c.readMem32(c.segBase[DS] + mr.ea)
		}
		c.push32(c.eip)
		c.eip = target
	case 4: // JMP r/m32
		if mr.isReg {
			c.eip = c.GetReg32(int(mr.rm))
		} else {
			c.eip = c.readMem32(c.segBase[DS] + mr.ea)
		}
	case 6: // PUSH r/m32
		if mr.isReg {
			c.push32(c.GetReg32(int(mr.rm)))
		} else {
			c.push32(c.readMem32(c.segBase[DS] + mr.ea))
		}
	default:
		return fmt.Errorf("FF /%d not implemented", mr.reg)
	}
	return nil
}
