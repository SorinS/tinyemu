package x86

// handleALU_ModRM8 handles ALU operations with 8-bit ModR/M operands.
// op: 0=ADD, 1=OR, 2=ADC, 3=SBB, 4=AND, 5=SUB, 6=XOR, 7=CMP
func (c *CPU) handleALU_ModRM8(opcode uint8) error {
	op := (opcode >> 3) & 7
	mr := c.parseModRM()

	var dst, src uint8
	if opcode&2 != 0 {
		// r8, r/m8 direction (0x02, 0x0A, 0x12, 0x1A, 0x22, 0x2A, 0x32, 0x3A)
		if mr.isReg {
			src = c.GetReg8(reg8FromModRM(int(mr.rm)))
		} else {
			src = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
		}
		dst = c.GetReg8(reg8FromModRM(int(mr.reg)))
	} else {
		// r/m8, r8 direction (0x00, 0x08, 0x10, 0x18, 0x20, 0x28, 0x30, 0x38)
		if mr.isReg {
			dst = c.GetReg8(reg8FromModRM(int(mr.rm)))
		} else {
			dst = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
		}
		src = c.GetReg8(reg8FromModRM(int(mr.reg)))
	}

	var res uint8
	switch op {
	case 0:
		res = c.add8(dst, src)
	case 1:
		res = c.or8(dst, src)
	case 2:
		res = c.adc8(dst, src)
	case 3:
		res = c.sbb8(dst, src)
	case 4:
		res = c.and8(dst, src)
	case 5:
		res = c.sub8(dst, src)
	case 6:
		res = c.xor8(dst, src)
	case 7:
		res = c.sub8(dst, src) // CMP
	}

	if op != 7 { // CMP doesn't write back
		if opcode&2 != 0 {
			c.SetReg8(reg8FromModRM(int(mr.reg)), res)
		} else {
			if mr.isReg {
				c.SetReg8(reg8FromModRM(int(mr.rm)), res)
			} else {
				c.writeMem8(c.segBaseForModRM(mr) + mr.ea, res)
			}
		}
	}
	return nil
}

// handleALU_ModRM32 handles ALU operations with 32-bit ModR/M operands.
// op: 0=ADD, 1=OR, 2=ADC, 3=SBB, 4=AND, 5=SUB, 6=XOR, 7=CMP
func (c *CPU) handleALU_ModRM32(opcode uint8) error {
	op := (opcode >> 3) & 7
	mr := c.parseModRM()

	var dst, src uint32
	if opcode&2 != 0 {
		// r32, r/m32 direction
		if mr.isReg {
			src = c.GetReg32(int(mr.rm))
		} else {
			src = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
		}
		dst = c.GetReg32(int(mr.reg))
	} else {
		// r/m32, r32 direction
		if mr.isReg {
			dst = c.GetReg32(int(mr.rm))
		} else {
			dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
		}
		src = c.GetReg32(int(mr.reg))
	}

	var res uint32
	switch op {
	case 0:
		res = c.add32(dst, src)
	case 1:
		res = c.or32(dst, src)
	case 2:
		res = c.adc32(dst, src)
	case 3:
		res = c.sbb32(dst, src)
	case 4:
		res = c.and32(dst, src)
	case 5:
		res = c.sub32(dst, src)
	case 6:
		res = c.xor32(dst, src)
	case 7:
		res = c.sub32(dst, src) // CMP
	}

	if op != 7 { // CMP doesn't write back
		if opcode&2 != 0 {
			c.SetReg32(int(mr.reg), res)
		} else {
			if mr.isReg {
				c.SetReg32(int(mr.rm), res)
			} else {
				c.writeMem32(c.segBaseForModRM(mr) + mr.ea, res)
			}
		}
	}
	return nil
}

// handleALU_ModRM16 handles ALU operations with 16-bit ModR/M operands.
// op: 0=ADD, 1=OR, 2=ADC, 3=SBB, 4=AND, 5=SUB, 6=XOR, 7=CMP
func (c *CPU) handleALU_ModRM16(opcode uint8) error {
	op := (opcode >> 3) & 7
	mr := c.parseModRM()

	var dst, src uint16
	if opcode&2 != 0 {
		// r16, r/m16 direction
		if mr.isReg {
			src = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			src = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		dst = c.GetReg16(reg16FromModRM(int(mr.reg)))
	} else {
		// r/m16, r16 direction
		if mr.isReg {
			dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		src = c.GetReg16(reg16FromModRM(int(mr.reg)))
	}

	var res uint16
	switch op {
	case 0:
		res = c.add16(dst, src)
	case 1:
		res = c.or16(dst, src)
	case 2:
		res = c.adc16(dst, src)
	case 3:
		res = c.sbb16(dst, src)
	case 4:
		res = c.and16(dst, src)
	case 5:
		res = c.sub16(dst, src)
	case 6:
		res = c.xor16(dst, src)
	case 7:
		res = c.sub16(dst, src) // CMP
	}

	if op != 7 { // CMP doesn't write back
		if opcode&2 != 0 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), res)
		} else {
			if mr.isReg {
				c.SetReg16(reg16FromModRM(int(mr.rm)), res)
			} else {
				c.writeMem16(c.segBaseForModRM(mr) + mr.ea, res)
			}
		}
	}
	return nil
}

// handleTEST_ModRM handles TEST r/m, r at the given operand size (1, 2, or 4
// bytes). Previously this routine treated any non-byte size as 32-bit, which
// meant that `66 85 c0` (TEST AX,AX, with the 0x66 16-bit operand prefix)
// silently tested EAX instead — so e.g. ZF was wrong whenever the upper 16
// bits of EAX were non-zero. That broke busybox's wide-string loop, which
// uses `mov ax, [esi+edx*2]` then `test ax,ax` to find a NUL terminator; the
// loop never terminated and walked off the end of the buffer.
func (c *CPU) handleTEST_ModRM(size int) error {
	mr := c.parseModRM()
	switch size {
	case 1:
		var dst uint8
		if mr.isReg {
			dst = c.GetReg8(reg8FromModRM(int(mr.rm)))
		} else {
			dst = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
		}
		src := c.GetReg8(reg8FromModRM(int(mr.reg)))
		c.and8(dst, src)
	case 2:
		var dst uint16
		if mr.isReg {
			dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		src := c.GetReg16(reg16FromModRM(int(mr.reg)))
		c.and16(dst, src)
	default:
		var dst uint32
		if mr.isReg {
			dst = c.GetReg32(int(mr.rm))
		} else {
			dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
		}
		src := c.GetReg32(int(mr.reg))
		c.and32(dst, src)
	}
	return nil
}
