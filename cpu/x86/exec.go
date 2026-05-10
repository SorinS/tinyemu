package x86

import (
	"fmt"
)

// readPhys8 reads a byte from the given physical address (no paging).
func (c *CPU) readPhys8(addr uint32) uint8 {
	v, _ := c.memMap.Read8(uint64(addr & c.a20Mask))
	return v
}

// readPhys16 reads a word from the given physical address (no paging).
func (c *CPU) readPhys16(addr uint32) uint16 {
	v, _ := c.memMap.Read16(uint64(addr & c.a20Mask))
	return v
}

// readPhys32 reads a dword from the given physical address (no paging).
func (c *CPU) readPhys32(addr uint32) uint32 {
	v, _ := c.memMap.Read32(uint64(addr & c.a20Mask))
	return v
}

// writePhys8 writes a byte to the given physical address (no paging).
func (c *CPU) writePhys8(addr uint32, val uint8) {
	c.memMap.Write8(uint64(addr&c.a20Mask), val)
}

// writePhys16 writes a word to the given physical address (no paging).
func (c *CPU) writePhys16(addr uint32, val uint16) {
	c.memMap.Write16(uint64(addr&c.a20Mask), val)
}

// writePhys32 writes a dword to the given physical address (no paging).
func (c *CPU) writePhys32(addr uint32, val uint32) {
	c.memMap.Write32(uint64(addr&c.a20Mask), val)
}

// readMem8 reads a byte from the given linear address (with paging).
func (c *CPU) readMem8(addr uint32) uint8 {
	return c.readPhys8(c.translateAddress(addr, false, false))
}

// readMem16 reads a word from the given linear address (with paging).
func (c *CPU) readMem16(addr uint32) uint16 {
	return c.readPhys16(c.translateAddress(addr, false, false))
}

// readMem32 reads a dword from the given linear address (with paging).
func (c *CPU) readMem32(addr uint32) uint32 {
	return c.readPhys32(c.translateAddress(addr, false, false))
}

// writeMem8 writes a byte to the given linear address (with paging).
func (c *CPU) writeMem8(addr uint32, val uint8) {
	c.writePhys8(c.translateAddress(addr, true, false), val)
}

// writeMem16 writes a word to the given linear address (with paging).
func (c *CPU) writeMem16(addr uint32, val uint16) {
	c.writePhys16(c.translateAddress(addr, true, false), val)
}

// writeMem32 writes a dword to the given linear address (with paging).
func (c *CPU) writeMem32(addr uint32, val uint32) {
	c.writePhys32(c.translateAddress(addr, true, false), val)
}

// Exported memory accessors for tests and loaders.
func (c *CPU) ReadMem8(addr uint32) uint8   { return c.readMem8(addr) }
func (c *CPU) ReadMem16(addr uint32) uint16 { return c.readMem16(addr) }
func (c *CPU) ReadMem32(addr uint32) uint32 { return c.readMem32(addr) }
func (c *CPU) WriteMem8(addr uint32, val uint8)   { c.writeMem8(addr, val) }
func (c *CPU) WriteMem16(addr uint32, val uint16) { c.writeMem16(addr, val) }
func (c *CPU) WriteMem32(addr uint32, val uint32) { c.writeMem32(addr, val) }

func (c *CPU) maskEIP() {
	if c.DefaultOperandSize() == 2 {
		c.eip &= 0xFFFF
	}
}

// fetch8 reads the next byte from the code stream and advances EIP.
func (c *CPU) fetch8() uint8 {
	lip := c.GetLIP()
	v := c.readMem8(lip)
	c.eip++
	c.maskEIP()
	return v
}

// fetch16 reads the next word from the code stream and advances EIP.
func (c *CPU) fetch16() uint16 {
	lip := c.GetLIP()
	v := c.readMem16(lip)
	c.eip += 2
	c.maskEIP()
	return v
}

// fetch32 reads the next dword from the code stream and advances EIP.
func (c *CPU) fetch32() uint32 {
	lip := c.GetLIP()
	v := c.readMem32(lip)
	c.eip += 4
	c.maskEIP()
	return v
}

// fetchS8 reads a signed byte from the code stream.
func (c *CPU) fetchS8() int8 {
	return int8(c.fetch8())
}

// fetchS16 reads a signed word from the code stream.
func (c *CPU) fetchS16() int16 {
	return int16(c.fetch16())
}

// fetchS32 reads a signed dword from the code stream.
func (c *CPU) fetchS32() int32 {
	return int32(c.fetch32())
}

// Step executes a single x86 instruction.
func (c *CPU) Step() (err error) {
	origEIP := c.eip
	defer func() {
		if r := recover(); r != nil {
			switch ex := r.(type) {
			case pageFaultError:
				c.eip = origEIP
				c.cr[2] = ex.addr
				err = c.handleInterrupt(0x0E, true, ex.errorCode)
			case stackFaultError:
				c.eip = origEIP
				err = c.handleInterrupt(0x0C, true, ex.errorCode)
			case generalProtectionFaultError:
				c.eip = origEIP
				err = c.handleInterrupt(0x0D, true, ex.errorCode)
			default:
				panic(r)
			}
		}
	}()

	// Check for pending hardware interrupts before fetching the next instruction.
	if c.intrLineState != 0 && (c.eflags&EFLAGS_IF) != 0 && !c.interruptsBlocked {
		if c.ackInterruptFunc != nil {
			if vec, ok := c.ackInterruptFunc(); ok {
				return c.handleInterrupt(vec, true)
			}
		}
	}
	c.interruptsBlocked = false

	// Scan prefixes
	var repPrefix uint8 // 0 = none, 1 = REP/REPE/REPZ, 2 = REPNE/REPNZ
	var segOverride int = -1
	operandSizeToggle := false
	addressSizeToggle := false
	for {
		opcode := c.fetch8()
		switch opcode {
		case 0xF0:
			// LOCK prefix - ignored for now (we don't implement SMP)
		case 0xF3:
			repPrefix = 1
		case 0xF2:
			repPrefix = 2
		case 0x66:
			operandSizeToggle = !operandSizeToggle
		case 0x67:
			addressSizeToggle = !addressSizeToggle
		case 0x2E:
			segOverride = CS
		case 0x36:
			segOverride = SS
		case 0x3E:
			segOverride = DS
		case 0x26:
			segOverride = ES
		case 0x64:
			segOverride = FS
		case 0x65:
			segOverride = GS
		default:
			operandSize := c.DefaultOperandSize()
			if operandSizeToggle {
				if operandSize == 2 {
					operandSize = 4
				} else {
					operandSize = 2
				}
			}
			addressSize := c.DefaultAddressSize()
			if addressSizeToggle {
				if addressSize == 2 {
					addressSize = 4
				} else {
					addressSize = 2
				}
			}
			err := c.executeOpcode(opcode, repPrefix, segOverride, operandSize, addressSize)
			c.maskEIP()
			return err
		}
	}
}

// executeOpcode executes a single opcode (after prefixes have been consumed).
// operandSize is 2 or 4 (16-bit or 32-bit).
// addressSize is 2 or 4 (16-bit or 32-bit addressing).
func (c *CPU) executeOpcode(opcode uint8, repPrefix uint8, segOverride int, operandSize, addressSize uint8) error {
	c.currentOpSize = operandSize
	c.currentAddrSize = addressSize
	switch opcode {
	// NOP
	case 0x90:
		// Do nothing

	// PUSHA/PUSHAD
	case 0x60:
		if operandSize == 2 {
			tmpSP := c.GetReg16(SP)
			c.push16(c.GetReg16(AX))
			c.push16(c.GetReg16(CX))
			c.push16(c.GetReg16(DX))
			c.push16(c.GetReg16(BX))
			c.push16(tmpSP)
			c.push16(c.GetReg16(BP))
			c.push16(c.GetReg16(SI))
			c.push16(c.GetReg16(DI))
		} else {
			tmpESP := c.GetReg32(ESP)
			c.push32(c.GetReg32(EAX))
			c.push32(c.GetReg32(ECX))
			c.push32(c.GetReg32(EDX))
			c.push32(c.GetReg32(EBX))
			c.push32(tmpESP)
			c.push32(c.GetReg32(EBP))
			c.push32(c.GetReg32(ESI))
			c.push32(c.GetReg32(EDI))
		}

	// POPA/POPAD
	case 0x61:
		if operandSize == 2 {
			c.SetReg16(DI, c.pop16())
			c.SetReg16(SI, c.pop16())
			c.SetReg16(BP, c.pop16())
			_ = c.pop16() // discard SP
			c.SetReg16(BX, c.pop16())
			c.SetReg16(DX, c.pop16())
			c.SetReg16(CX, c.pop16())
			c.SetReg16(AX, c.pop16())
		} else {
			c.SetReg32(EDI, c.pop32())
			c.SetReg32(ESI, c.pop32())
			c.SetReg32(EBP, c.pop32())
			_ = c.pop32() // discard ESP
			c.SetReg32(EBX, c.pop32())
			c.SetReg32(EDX, c.pop32())
			c.SetReg32(ECX, c.pop32())
			c.SetReg32(EAX, c.pop32())
		}

	// HLT
	case 0xF4:
		c.powerDown = true

	// CMC - Complement Carry Flag
	case 0xF5:
		c.eflags ^= EFLAGS_CF

	// CLC - Clear Carry Flag
	case 0xF8:
		c.eflags &^= EFLAGS_CF

	// STC - Set Carry Flag
	case 0xF9:
		c.eflags |= EFLAGS_CF

	// CLI - Clear Interrupt Flag
	case 0xFA:
		c.checkIOPL()
		c.eflags &^= EFLAGS_IF

	// STI - Set Interrupt Flag
	case 0xFB:
		c.checkIOPL()
		c.eflags |= EFLAGS_IF
		c.interruptsBlocked = true

	// CLD - Clear Direction Flag
	case 0xFC:
		c.eflags &^= EFLAGS_DF

	// STD - Set Direction Flag
	case 0xFD:
		c.eflags |= EFLAGS_DF

	// PUSH r16/r32
	case 0x50:
		c.pushOp(c.GetReg32(EAX), operandSize)
	case 0x51:
		c.pushOp(c.GetReg32(ECX), operandSize)
	case 0x52:
		c.pushOp(c.GetReg32(EDX), operandSize)
	case 0x53:
		c.pushOp(c.GetReg32(EBX), operandSize)
	case 0x54:
		c.pushOp(c.GetReg32(ESP), operandSize)
	case 0x55:
		c.pushOp(c.GetReg32(EBP), operandSize)
	case 0x56:
		c.pushOp(c.GetReg32(ESI), operandSize)
	case 0x57:
		c.pushOp(c.GetReg32(EDI), operandSize)

	// PUSH imm16/imm32
	case 0x68:
		if operandSize == 2 {
			c.pushOp(uint32(c.fetch16()), operandSize)
		} else {
			c.pushOp(c.fetch32(), operandSize)
		}

	// PUSH imm8
	case 0x6A:
		c.pushOp(uint32(int32(int8(c.fetch8()))), operandSize)

	// IMUL r16/32, r/m16/32, imm16/32
	case 0x69:
		mr := c.parseModRM()
		var src int64
		if mr.isReg {
			if operandSize == 2 {
				src = int64(int16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
			} else {
				src = int64(int32(c.GetReg32(int(mr.rm))))
			}
		} else {
			if operandSize == 2 {
				src = int64(int16(c.readMem16(c.segBase[DS] + mr.ea)))
			} else {
				src = int64(int32(c.readMem32(c.segBase[DS] + mr.ea)))
			}
		}
		var imm int64
		if operandSize == 2 {
			imm = int64(int16(c.fetch16()))
		} else {
			imm = int64(int32(c.fetch32()))
		}
		r := src * imm
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(r))
			c.setOF(r != int64(int16(r)))
			c.setCF(r != int64(int16(r)))
		} else {
			c.SetReg32(int(mr.reg), uint32(r))
			c.setOF(r != int64(int32(r)))
			c.setCF(r != int64(int32(r)))
		}

	// INS B (IN SB)
	case 0x6C:
		c.executeString(repPrefix, 1, func() {
			port := c.GetReg16(DX)
			c.writeMem8(c.segBase[ES]+c.GetReg32(EDI), c.ioRead8(port))
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(EDI, c.GetReg32(EDI)-1)
			} else {
				c.SetReg32(EDI, c.GetReg32(EDI)+1)
			}
		}, false, false)

	// INS W/D (INSW/INSD)
	case 0x6D:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() {
			port := c.GetReg16(DX)
			addr := c.segBase[ES] + c.GetReg32(EDI)
			if size == 2 {
				c.writeMem16(addr, c.ioRead16(port))
			} else {
				c.writeMem32(addr, c.ioRead32(port))
			}
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(EDI, c.GetReg32(EDI)-size)
			} else {
				c.SetReg32(EDI, c.GetReg32(EDI)+size)
			}
		}, false, false)

	// OUTS B (OUTSB)
	case 0x6E:
		c.executeString(repPrefix, 1, func() {
			port := c.GetReg16(DX)
			seg := DS
			if segOverride != -1 {
				seg = segOverride
			}
			c.ioWrite8(port, c.readMem8(c.segBase[seg]+c.GetReg32(ESI)))
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(ESI, c.GetReg32(ESI)-1)
			} else {
				c.SetReg32(ESI, c.GetReg32(ESI)+1)
			}
		}, false, false)

	// OUTS W/D (OUTSW/OUTSD)
	case 0x6F:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() {
			port := c.GetReg16(DX)
			seg := DS
			if segOverride != -1 {
				seg = segOverride
			}
			addr := c.segBase[seg] + c.GetReg32(ESI)
			if size == 2 {
				c.ioWrite16(port, c.readMem16(addr))
			} else {
				c.ioWrite32(port, c.readMem32(addr))
			}
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(ESI, c.GetReg32(ESI)-size)
			} else {
				c.SetReg32(ESI, c.GetReg32(ESI)+size)
			}
		}, false, false)

	// IMUL r16/32, r/m16/32, imm8
	case 0x6B:
		mr := c.parseModRM()
		var src int64
		if mr.isReg {
			if operandSize == 2 {
				src = int64(int16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
			} else {
				src = int64(int32(c.GetReg32(int(mr.rm))))
			}
		} else {
			if operandSize == 2 {
				src = int64(int16(c.readMem16(c.segBase[DS] + mr.ea)))
			} else {
				src = int64(int32(c.readMem32(c.segBase[DS] + mr.ea)))
			}
		}
		imm := int64(int32(int8(c.fetch8())))
		r := src * imm
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(r))
			c.setOF(r != int64(int16(r)))
			c.setCF(r != int64(int16(r)))
		} else {
			c.SetReg32(int(mr.reg), uint32(r))
			c.setOF(r != int64(int32(r)))
			c.setCF(r != int64(int32(r)))
		}

	// POP r16/r32
	case 0x58:
		c.SetReg32(EAX, c.popOp(operandSize))
	case 0x59:
		c.SetReg32(ECX, c.popOp(operandSize))
	case 0x5A:
		c.SetReg32(EDX, c.popOp(operandSize))
	case 0x5B:
		c.SetReg32(EBX, c.popOp(operandSize))
	case 0x5C:
		c.SetReg32(ESP, c.popOp(operandSize))
	case 0x5D:
		c.SetReg32(EBP, c.popOp(operandSize))
	case 0x5E:
		c.SetReg32(ESI, c.popOp(operandSize))
	case 0x5F:
		c.SetReg32(EDI, c.popOp(operandSize))

	// MOV r8, imm8
	case 0xB0:
		c.SetReg8(AL, c.fetch8())
	case 0xB1:
		c.SetReg8(CL, c.fetch8())
	case 0xB2:
		c.SetReg8(DL, c.fetch8())
	case 0xB3:
		c.SetReg8(BL, c.fetch8())
	case 0xB4:
		c.SetReg8(AH, c.fetch8())
	case 0xB5:
		c.SetReg8(CH, c.fetch8())
	case 0xB6:
		c.SetReg8(DH, c.fetch8())
	case 0xB7:
		c.SetReg8(BH, c.fetch8())

	// MOV r16/r32, imm16/imm32
	case 0xB8:
		if operandSize == 2 {
			c.SetReg16(AX, c.fetch16())
		} else {
			c.SetReg32(EAX, c.fetch32())
		}
	case 0xB9:
		if operandSize == 2 {
			c.SetReg16(CX, c.fetch16())
		} else {
			c.SetReg32(ECX, c.fetch32())
		}
	case 0xBA:
		if operandSize == 2 {
			c.SetReg16(DX, c.fetch16())
		} else {
			c.SetReg32(EDX, c.fetch32())
		}
	case 0xBB:
		if operandSize == 2 {
			c.SetReg16(BX, c.fetch16())
		} else {
			c.SetReg32(EBX, c.fetch32())
		}
	case 0xBC:
		if operandSize == 2 {
			c.SetReg16(SP, c.fetch16())
		} else {
			c.SetReg32(ESP, c.fetch32())
		}
	case 0xBD:
		if operandSize == 2 {
			c.SetReg16(BP, c.fetch16())
		} else {
			c.SetReg32(EBP, c.fetch32())
		}
	case 0xBE:
		if operandSize == 2 {
			c.SetReg16(SI, c.fetch16())
		} else {
			c.SetReg32(ESI, c.fetch32())
		}
	case 0xBF:
		if operandSize == 2 {
			c.SetReg16(DI, c.fetch16())
		} else {
			c.SetReg32(EDI, c.fetch32())
		}

	// XCHG r/m8, r8
	case 0x86:
		mr := c.parseModRM()
		if mr.isReg {
			tmp := c.GetReg8(reg8FromModRM(int(mr.rm)))
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.GetReg8(reg8FromModRM(int(mr.reg))))
			c.SetReg8(reg8FromModRM(int(mr.reg)), tmp)
		} else {
			addr := c.segBase[DS] + mr.ea
			tmp := c.readMem8(addr)
			c.writeMem8(addr, c.GetReg8(reg8FromModRM(int(mr.reg))))
			c.SetReg8(reg8FromModRM(int(mr.reg)), tmp)
		}

	// XCHG r/m16/32, r16/32
	case 0x87:
		mr := c.parseModRM()
		if operandSize == 2 {
			if mr.isReg {
				tmp := c.GetReg16(reg16FromModRM(int(mr.rm)))
				c.SetReg16(reg16FromModRM(int(mr.rm)), c.GetReg16(reg16FromModRM(int(mr.reg))))
				c.SetReg16(reg16FromModRM(int(mr.reg)), tmp)
			} else {
				addr := c.segBase[DS] + mr.ea
				tmp := c.readMem16(addr)
				c.writeMem16(addr, c.GetReg16(reg16FromModRM(int(mr.reg))))
				c.SetReg16(reg16FromModRM(int(mr.reg)), tmp)
			}
		} else {
			if mr.isReg {
				tmp := c.GetReg32(int(mr.rm))
				c.SetReg32(int(mr.rm), c.GetReg32(int(mr.reg)))
				c.SetReg32(int(mr.reg), tmp)
			} else {
				addr := c.segBase[DS] + mr.ea
				tmp := c.readMem32(addr)
				c.writeMem32(addr, c.GetReg32(int(mr.reg)))
				c.SetReg32(int(mr.reg), tmp)
			}
		}

	// MOV r/m8, r8 and MOV r8, r/m8
	case 0x88, 0x8A:
		return c.handleModRM8(opcode)

	// MOV r/m16/32, r16/32 and MOV r16/32, r/m16/32
	case 0x89, 0x8B:
		if operandSize == 2 {
			return c.handleModRM16(opcode)
		}
		return c.handleModRM32(opcode)

	// MOV AL, moffs8
	case 0xA0:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		c.SetReg8(AL, c.readMem8(c.segBase[seg]+off))

	// MOV AX/EAX, moffs16/moffs32
	case 0xA1:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		if operandSize == 2 {
			c.SetReg16(AX, c.readMem16(c.segBase[seg]+off))
		} else {
			c.SetReg32(EAX, c.readMem32(c.segBase[seg]+off))
		}

	// MOV moffs8, AL
	case 0xA2:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		c.writeMem8(c.segBase[seg]+off, c.GetReg8(AL))

	// MOV moffs16/moffs32, AX/EAX
	case 0xA3:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		if operandSize == 2 {
			c.writeMem16(c.segBase[seg]+off, c.GetReg16(AX))
		} else {
			c.writeMem32(c.segBase[seg]+off, c.GetReg32(EAX))
		}

	// MOV r/m16/32, imm16/imm32 (C7 /0)
	case 0xC7:
		return c.handleMovImm(operandSize)

	// PUSH ES
	case 0x06:
		c.push32(uint32(c.seg[ES]))

	// POP ES
	case 0x07:
		c.seg[ES] = uint16(c.pop32())

	// PUSH CS
	case 0x0E:
		c.push32(uint32(c.seg[CS]))

	// PUSH SS
	case 0x16:
		c.push32(uint32(c.seg[SS]))

	// POP SS
	case 0x17:
		c.seg[SS] = uint16(c.pop32())
		c.interruptsBlocked = true

	// PUSH DS
	case 0x1E:
		c.push32(uint32(c.seg[DS]))

	// POP DS
	case 0x1F:
		c.seg[DS] = uint16(c.pop32())

	// ADD AL, imm8
	case 0x04:
		c.SetReg8(AL, c.add8(c.GetReg8(AL), c.fetch8()))

	// ADD EAX/AX, imm32/imm16
	case 0x05:
		if operandSize == 2 {
			c.SetReg16(AX, c.add16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.add32(c.GetReg32(EAX), c.fetch32()))
		}

	// ADD r/m8, imm8 (80 /0)
	case 0x80:
		return c.handleGroup1_8()

	// ADD r/m16/32, imm16/32 (81 /0)
	case 0x81:
		if operandSize == 2 {
			return c.handleGroup1_16()
		}
		return c.handleGroup1_32()

	// ADD r/m16/32, imm8 (83 /0, sign-extended)
	case 0x83:
		return c.handleGroup1_8x(operandSize)

	// INC r32
	case 0x40:
		c.SetReg32(EAX, c.inc32(c.GetReg32(EAX)))
	case 0x41:
		c.SetReg32(ECX, c.inc32(c.GetReg32(ECX)))
	case 0x42:
		c.SetReg32(EDX, c.inc32(c.GetReg32(EDX)))
	case 0x43:
		c.SetReg32(EBX, c.inc32(c.GetReg32(EBX)))
	case 0x44:
		c.SetReg32(ESP, c.inc32(c.GetReg32(ESP)))
	case 0x45:
		c.SetReg32(EBP, c.inc32(c.GetReg32(EBP)))
	case 0x46:
		c.SetReg32(ESI, c.inc32(c.GetReg32(ESI)))
	case 0x47:
		c.SetReg32(EDI, c.inc32(c.GetReg32(EDI)))

	// DEC r32
	case 0x48:
		c.SetReg32(EAX, c.dec32(c.GetReg32(EAX)))
	case 0x49:
		c.SetReg32(ECX, c.dec32(c.GetReg32(ECX)))
	case 0x4A:
		c.SetReg32(EDX, c.dec32(c.GetReg32(EDX)))
	case 0x4B:
		c.SetReg32(EBX, c.dec32(c.GetReg32(EBX)))
	case 0x4C:
		c.SetReg32(ESP, c.dec32(c.GetReg32(ESP)))
	case 0x4D:
		c.SetReg32(EBP, c.dec32(c.GetReg32(EBP)))
	case 0x4E:
		c.SetReg32(ESI, c.dec32(c.GetReg32(ESI)))
	case 0x4F:
		c.SetReg32(EDI, c.dec32(c.GetReg32(EDI)))

	// JMP rel8
	case 0xEB:
		off := c.fetchS8()
		c.eip = uint32(int32(c.eip) + int32(off))

	// JMP rel16/rel32
	case 0xE9:
		if operandSize == 2 {
			off := c.fetchS16()
			c.eip = uint32(int32(c.eip) + int32(off))
		} else {
			off := c.fetchS32()
			c.eip = uint32(int32(c.eip) + int32(off))
		}

	// Jcc rel8
	case 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
		0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F:
		return c.handleJcc8(opcode)

	// CALL rel16/rel32
	case 0xE8:
		if operandSize == 2 {
			off := c.fetchS16()
			c.push16(uint16(c.eip))
			c.eip = uint32(int32(c.eip) + int32(off))
		} else {
			off := c.fetchS32()
			c.push32(c.eip)
			c.eip = uint32(int32(c.eip) + int32(off))
		}

	// RET near
	case 0xC3:
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
		} else {
			c.eip = c.pop32()
		}

	// RET imm16
	case 0xC2:
		imm := c.fetch16()
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
			c.SetReg16(SP, c.GetReg16(SP)+imm)
		} else {
			c.eip = c.pop32()
			c.SetReg32(ESP, c.GetReg32(ESP)+uint32(imm))
		}

	// INT 3
	case 0xCC:
		return c.handleInterrupt(0x03, false)

	// INT imm8
	case 0xCD:
		vec := c.fetch8()
		return c.handleInterrupt(vec, false)

	// INTO
	case 0xCE:
		if c.eflags&EFLAGS_OF != 0 {
			return c.handleInterrupt(0x04, false)
		}

		// IRET
		case 0xCF:
			if operandSize == 2 {
				oldCPL := c.cpl
				c.eip = uint32(c.pop16())
				cs := c.pop16()
				if c.IsProtectedMode() {
					if err := c.LoadSegmentProtected(CS, cs); err != nil {
						return err
					}
				} else {
					c.seg[CS] = cs
					c.segBase[CS] = uint32(cs) << 4
				}
				c.eflags = uint32(c.pop16())
				c.eflags |= EFLAGS_RF
				if c.IsProtectedMode() && c.cpl > oldCPL {
					newSP := c.pop16()
					newSS := c.pop16()
					if err := c.LoadSegmentProtected(SS, newSS); err != nil {
						return err
					}
					c.SetReg16(SP, newSP)
				}
			} else {
				oldCPL := c.cpl
				c.eip = c.pop32()
				cs := c.pop32()
				if c.IsProtectedMode() {
					if err := c.LoadSegmentProtected(CS, uint16(cs)); err != nil {
						return err
					}
				} else {
					c.seg[CS] = uint16(cs)
					c.segBase[CS] = uint32(cs) << 4
				}
				c.eflags = c.pop32()
				c.eflags |= EFLAGS_RF
				// Restore VM bit if CPL=0 and popped EFLAGS has it.
				if c.IsProtectedMode() && c.cpl == 0 && (c.eflags&EFLAGS_VM) != 0 {
					// TODO: pop ES, DS, FS, GS for full v8086 mode entry
				}
				if c.IsProtectedMode() && c.cpl > oldCPL {
					newESP := c.pop32()
					newSS := c.pop32()
					if err := c.LoadSegmentProtected(SS, uint16(newSS)); err != nil {
						return err
					}
					c.SetReg32(ESP, newESP)
				}
			}

	// PUSHF
	case 0x9C:
		c.pushOp(c.eflags, operandSize)

	// POPF
	case 0x9D:
		c.eflags = c.popOp(operandSize)

	// CBW/CWDE
	case 0x98:
		if operandSize == 2 {
			if int8(c.GetReg8(AL)) < 0 {
				c.SetReg8(AH, 0xFF)
			} else {
				c.SetReg8(AH, 0)
			}
		} else {
			ax := int16(c.GetReg16(AX))
			if ax < 0 {
				c.SetReg32(EAX, 0xFFFF0000|uint32(ax))
			} else {
				c.SetReg32(EAX, uint32(ax))
			}
		}

	// CWD/CDQ
	case 0x99:
		if operandSize == 2 {
			if c.GetReg16(AX)&0x8000 != 0 {
				c.SetReg16(DX, 0xFFFF)
			} else {
				c.SetReg16(DX, 0)
			}
		} else {
			if c.GetReg32(EAX)&0x80000000 != 0 {
				c.SetReg32(EDX, 0xFFFFFFFF)
			} else {
				c.SetReg32(EDX, 0)
			}
		}

	// SAHF
	case 0x9E:
		c.eflags = (c.eflags & ^uint32(EFLAGS_SF|EFLAGS_ZF|EFLAGS_AF|EFLAGS_PF|EFLAGS_CF)) | uint32(c.GetReg8(AH)&(0x80|0x40|0x10|0x04|0x01))

	// LAHF
	case 0x9F:
		c.SetReg8(AH, uint8(c.eflags)&(0x80|0x40|0x10|0x04|0x01)|0x02)

	// SUB AL, imm8
	case 0x2C:
		c.SetReg8(AL, c.sub8(c.GetReg8(AL), c.fetch8()))

	// SUB EAX/AX, imm32/imm16
	case 0x2D:
		if operandSize == 2 {
			c.SetReg16(AX, c.sub16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.sub32(c.GetReg32(EAX), c.fetch32()))
		}

	// XOR AL, imm8
	case 0x34:
		c.SetReg8(AL, c.xor8(c.GetReg8(AL), c.fetch8()))

	// XOR EAX/AX, imm32/imm16
	case 0x35:
		if operandSize == 2 {
			c.SetReg16(AX, c.xor16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.xor32(c.GetReg32(EAX), c.fetch32()))
		}

	// CMP AL, imm8
	case 0x3C:
		c.sub8(c.GetReg8(AL), c.fetch8())

	// CMP EAX/AX, imm32/imm16
	case 0x3D:
		if operandSize == 2 {
			c.sub16(c.GetReg16(AX), c.fetch16())
		} else {
			c.sub32(c.GetReg32(EAX), c.fetch32())
		}

	// TEST AL, imm8
	case 0xA8:
		c.and8(c.GetReg8(AL), c.fetch8())

	// TEST EAX/AX, imm32/imm16
	case 0xA9:
		if operandSize == 2 {
			c.and16(c.GetReg16(AX), c.fetch16())
		} else {
			c.and32(c.GetReg32(EAX), c.fetch32())
		}

	// IN AL, imm8
	case 0xE4:
		c.checkIOPL()
		port := uint16(c.fetch8())
		c.SetReg8(AL, c.ioRead8(port))

	// IN AX/EAX, imm8
	case 0xE5:
		c.checkIOPL()
		port := uint16(c.fetch8())
		if operandSize == 2 {
			c.SetReg16(AX, c.ioRead16(port))
		} else {
			c.SetReg32(EAX, c.ioRead32(port))
		}

	// OUT imm8, AL
	case 0xE6:
		c.checkIOPL()
		port := uint16(c.fetch8())
		c.ioWrite8(port, c.GetReg8(AL))

	// OUT imm8, AX/EAX
	case 0xE7:
		c.checkIOPL()
		port := uint16(c.fetch8())
		if operandSize == 2 {
			c.ioWrite16(port, c.GetReg16(AX))
		} else {
			c.ioWrite32(port, c.GetReg32(EAX))
		}

	// IN AL, DX
	case 0xEC:
		c.checkIOPL()
		c.SetReg8(AL, c.ioRead8(c.GetReg16(DX)))

	// IN AX/EAX, DX
	case 0xED:
		c.checkIOPL()
		if operandSize == 2 {
			c.SetReg16(AX, c.ioRead16(c.GetReg16(DX)))
		} else {
			c.SetReg32(EAX, c.ioRead32(c.GetReg16(DX)))
		}

	// OUT DX, AL
	case 0xEE:
		c.checkIOPL()
		c.ioWrite8(c.GetReg16(DX), c.GetReg8(AL))

	// OUT DX, AX/EAX
	case 0xEF:
		c.checkIOPL()
		if operandSize == 2 {
			c.ioWrite16(c.GetReg16(DX), c.GetReg16(AX))
		} else {
			c.ioWrite32(c.GetReg16(DX), c.GetReg32(EAX))
		}

	// ALU r/m8, r8 and r8, r/m8
	case 0x00, 0x02, 0x08, 0x0A, 0x10, 0x12, 0x18, 0x1A,
		0x20, 0x22, 0x28, 0x2A, 0x30, 0x32, 0x38, 0x3A:
		return c.handleALU_ModRM8(opcode)
	// ALU r/m16/32, r16/32 and r16/32, r/m16/32
	case 0x01, 0x03, 0x09, 0x0B, 0x11, 0x13, 0x19, 0x1B,
		0x21, 0x23, 0x29, 0x2B, 0x31, 0x33, 0x39, 0x3B:
		if operandSize == 2 {
			return c.handleALU_ModRM16(opcode)
		}
		return c.handleALU_ModRM32(opcode)

	// TEST r/m8, r8
	case 0x84:
		return c.handleTEST_ModRM(1)
	// TEST r/m16/32, r16/32
	case 0x85:
		return c.handleTEST_ModRM(int(operandSize))

	// XCHG AX, r16 / XCHG EAX, r32
	case 0x91:
		if operandSize == 2 {
			c.xchg16(EAX, ECX)
		} else {
			c.xchg32(EAX, ECX)
		}
	case 0x92:
		if operandSize == 2 {
			c.xchg16(EAX, EDX)
		} else {
			c.xchg32(EAX, EDX)
		}
	case 0x93:
		if operandSize == 2 {
			c.xchg16(EAX, EBX)
		} else {
			c.xchg32(EAX, EBX)
		}
	case 0x94:
		if operandSize == 2 {
			c.xchg16(EAX, ESP)
		} else {
			c.xchg32(EAX, ESP)
		}
	case 0x95:
		if operandSize == 2 {
			c.xchg16(EAX, EBP)
		} else {
			c.xchg32(EAX, EBP)
		}
	case 0x96:
		if operandSize == 2 {
			c.xchg16(EAX, ESI)
		} else {
			c.xchg32(EAX, ESI)
		}
	case 0x97:
		if operandSize == 2 {
			c.xchg16(EAX, EDI)
		} else {
			c.xchg32(EAX, EDI)
		}

	// POP r/m16/32 (8F /0)
	case 0x8F:
		mr := c.parseModRM()
		if mr.reg != 0 {
			return fmt.Errorf("8F /%d not implemented", mr.reg)
		}
		if operandSize == 2 {
			if mr.isReg {
				c.SetReg16(reg16FromModRM(int(mr.rm)), c.pop16())
			} else {
				c.writeMem16(c.segBase[DS]+mr.ea, c.pop16())
			}
		} else {
			if mr.isReg {
				c.SetReg32(int(mr.rm), c.pop32())
			} else {
				c.writeMem32(c.segBase[DS]+mr.ea, c.pop32())
			}
		}

	// MOV r/m8, imm8 (C6 /0)
	case 0xC6:
		mr := c.parseModRM()
		imm := c.fetch8()
		if mr.reg != 0 {
			return fmt.Errorf("C6 /%d not implemented", mr.reg)
		}
		if mr.isReg {
			c.SetReg8(reg8FromModRM(int(mr.rm)), imm)
		} else {
			c.writeMem8(c.segBase[DS]+mr.ea, imm)
		}

	// LEA r16/32, m
	case 0x8D:
		mr := c.parseModRM()
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(mr.ea))
		} else {
			c.SetReg32(int(mr.reg), mr.ea)
		}

	// LOOP rel8
	case 0xE2:
		off := c.fetchS8()
		if addressSize == 2 {
			c.SetReg16(CX, c.GetReg16(CX)-1)
			if c.GetReg16(CX) != 0 {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		} else {
			c.SetReg32(ECX, c.GetReg32(ECX)-1)
			if c.GetReg32(ECX) != 0 {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		}

	// LOOPE/LOOPZ rel8
	case 0xE1:
		off := c.fetchS8()
		if addressSize == 2 {
			cx := c.GetReg16(CX)
			c.SetReg16(CX, cx-1)
			newCx := c.GetReg16(CX)
			zf := c.getZF()
			if eip := c.GetEIP(); eip >= 0x3F0 && eip <= 0x400 {
				// LOOPZ16 behavior documented
			}
			if newCx != 0 && zf {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		} else {
			ecx := c.GetReg32(ECX)
			c.SetReg32(ECX, ecx-1)
			newEcx := c.GetReg32(ECX)
			zf := c.getZF()
			if eip := c.GetEIP(); eip >= 0x3C0 && eip <= 0x3D0 {
				// LOOPZ32 behavior documented
			}
			if newEcx != 0 && zf {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		}

	// LOOPNE/LOOPNZ rel8
	case 0xE0:
		off := c.fetchS8()
		if addressSize == 2 {
			c.SetReg16(CX, c.GetReg16(CX)-1)
			if c.GetReg16(CX) != 0 && !c.getZF() {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		} else {
			c.SetReg32(ECX, c.GetReg32(ECX)-1)
			if c.GetReg32(ECX) != 0 && !c.getZF() {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		}

	// JCXZ/JECXZ rel8
	case 0xE3:
		off := c.fetchS8()
		if c.GetReg32(ECX) == 0 {
			c.eip = uint32(int32(c.eip) + int32(off))
		}

	// MOVS byte
	case 0xA4:
		c.executeString(repPrefix, 1, func() { c.movs(1, segOverride) }, false, false)

	// MOVS word/dword
	case 0xA5:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.movs(size, segOverride) }, false, false)

	// CMPS byte
	case 0xA6:
		c.executeString(repPrefix, 1, func() { c.cmps(1, segOverride) }, repPrefix != 0, repPrefix == 1)

	// CMPS word/dword
	case 0xA7:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.cmps(size, segOverride) }, repPrefix != 0, repPrefix == 1)

	// STOS byte
	case 0xAA:
		c.executeString(repPrefix, 1, func() { c.stos(1, segOverride) }, false, false)

	// STOS word/dword
	case 0xAB:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.stos(size, segOverride) }, false, false)

	// LODS byte
	case 0xAC:
		c.executeString(repPrefix, 1, func() { c.lods(1, segOverride) }, false, false)

	// LODS word/dword
	case 0xAD:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.lods(size, segOverride) }, false, false)

	// SCAS byte
	case 0xAE:
		c.executeString(repPrefix, 1, func() { c.scas(1, segOverride) }, repPrefix != 0, repPrefix == 1)

	// SCAS word/dword
	case 0xAF:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.scas(size, segOverride) }, repPrefix != 0, repPrefix == 1)

	// Group 2 shifts (C0 = imm8, C1 = imm8)
	case 0xC0:
		return c.handleGroup2_8()
	case 0xC1:
		if operandSize == 2 {
			return c.handleGroup2_16()
		}
		return c.handleGroup2_32()

	// Group 2 shifts (D0 = 1, D1 = 1, D2 = CL, D3 = CL)
	case 0xD0:
		return c.handleGroup2_8Count(1)
	case 0xD1:
		if operandSize == 2 {
			return c.handleGroup2_16Count(1)
		}
		return c.handleGroup2_32Count(1)
	case 0xD2:
		return c.handleGroup2_8Count(c.GetReg8(CL))
	case 0xD3:
		if operandSize == 2 {
			return c.handleGroup2_16Count(c.GetReg8(CL))
		}
		return c.handleGroup2_32Count(c.GetReg8(CL))

	// XLAT
	case 0xD7:
		if addressSize == 2 {
			c.SetReg8(AL, c.readMem8(c.segBase[DS]+uint32(c.GetReg16(BX)+uint16(c.GetReg8(AL)))))
		} else {
			c.SetReg8(AL, c.readMem8(c.segBase[DS]+c.GetReg32(EBX)+uint32(c.GetReg8(AL))))
		}

	// Group 3 (F6/F7)
	case 0xF6:
		return c.handleGroup3_8()
	case 0xF7:
		if operandSize == 2 {
			return c.handleGroup3_16()
		}
		return c.handleGroup3_32()

	// Group 4 (FE)
	case 0xFE:
		return c.handleGroup4_8()

	// Group 5 (FF)
	case 0xFF:
		if operandSize == 2 {
			return c.handleGroup5_16()
		}
		return c.handleGroup5_32()

	// ADC AL, imm8
	case 0x14:
		c.SetReg8(AL, c.adc8(c.GetReg8(AL), c.fetch8()))

	// ADC EAX/AX, imm32/imm16
	case 0x15:
		if operandSize == 2 {
			c.SetReg16(AX, c.adc16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.adc32(c.GetReg32(EAX), c.fetch32()))
		}

	// SBB AL, imm8
	case 0x1C:
		c.SetReg8(AL, c.sbb8(c.GetReg8(AL), c.fetch8()))

	// SBB EAX/AX, imm32/imm16
	case 0x1D:
		if operandSize == 2 {
			c.SetReg16(AX, c.sbb16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.sbb32(c.GetReg32(EAX), c.fetch32()))
		}

	// AND AL, imm8
	case 0x24:
		c.SetReg8(AL, c.and8(c.GetReg8(AL), c.fetch8()))

	// AND EAX/AX, imm32/imm16
	case 0x25:
		if operandSize == 2 {
			c.SetReg16(AX, c.and16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.and32(c.GetReg32(EAX), c.fetch32()))
		}

	// OR AL, imm8
	case 0x0C:
		c.SetReg8(AL, c.or8(c.GetReg8(AL), c.fetch8()))

	// OR EAX/AX, imm32/imm16
	case 0x0D:
		if operandSize == 2 {
			c.SetReg16(AX, c.or16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.or32(c.GetReg32(EAX), c.fetch32()))
		}

	// MOV r/m16, Sreg (8C)
	case 0x8C:
		mr := c.parseModRM()
		segVal := c.seg[mr.reg]
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), segVal)
		} else {
			c.writeMem16(c.segBase[DS]+mr.ea, segVal)
		}

	// MOV Sreg, r/m16 (8E)
	case 0x8E:
		mr := c.parseModRM()
		var segVal uint16
		if mr.isReg {
			segVal = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			segVal = c.readMem16(c.segBase[DS] + mr.ea)
		}
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(int(mr.reg), segVal); err != nil {
				return err
			}
		} else {
			c.seg[mr.reg] = segVal
			c.segBase[mr.reg] = uint32(segVal) << 4
		}
		if mr.reg == SS {
			c.interruptsBlocked = true
		}

	// LES r16/32, m16:16/32
	case 0xC4:
		mr := c.parseModRM()
		if !mr.isReg {
			addr := c.segBase[DS] + mr.ea
			if operandSize == 2 {
				c.SetReg16(reg16FromModRM(int(mr.reg)), c.readMem16(addr))
			} else {
				c.SetReg32(int(mr.reg), c.readMem32(addr))
			}
			segVal := c.readMem16(addr + uint32(operandSize))
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(ES, segVal); err != nil {
					return err
				}
			} else {
				c.seg[ES] = segVal
				c.segBase[ES] = uint32(segVal) << 4
			}
		}

	// LDS r16/32, m16:16/32
	case 0xC5:
		mr := c.parseModRM()
		if !mr.isReg {
			addr := c.segBase[DS] + mr.ea
			if operandSize == 2 {
				c.SetReg16(reg16FromModRM(int(mr.reg)), c.readMem16(addr))
			} else {
				c.SetReg32(int(mr.reg), c.readMem32(addr))
			}
			segVal := c.readMem16(addr + uint32(operandSize))
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(DS, segVal); err != nil {
					return err
				}
			} else {
				c.seg[DS] = segVal
				c.segBase[DS] = uint32(segVal) << 4
			}
		}

	// JMPF ptr16:16 / ptr16:32
	case 0xEA:
		var off uint32
		if operandSize == 2 {
			off = uint32(c.fetch16())
		} else {
			off = c.fetch32()
		}
		cs := c.fetch16()
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(CS, cs); err != nil {
				return err
			}
		} else {
			c.seg[CS] = cs
			c.segBase[CS] = uint32(cs) << 4
		}
		c.eip = off

	// CALLF ptr16:16 / ptr16:32
	case 0x9A:
		var off uint32
		if operandSize == 2 {
			off = uint32(c.fetch16())
		} else {
			off = c.fetch32()
		}
		cs := c.fetch16()
		c.pushOp(uint32(c.seg[CS]), operandSize)
		c.pushOp(c.eip, operandSize)
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(CS, cs); err != nil {
				return err
			}
		} else {
			c.seg[CS] = cs
			c.segBase[CS] = uint32(cs) << 4
		}
		c.eip = off

	// RETF
	case 0xCB:
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
			cs := c.pop16()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, cs); err != nil {
					return err
				}
			} else {
				c.seg[CS] = cs
				c.segBase[CS] = uint32(cs) << 4
			}
		} else {
			c.eip = c.pop32()
			cs := c.pop32()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, uint16(cs)); err != nil {
					return err
				}
			} else {
				c.seg[CS] = uint16(cs)
				c.segBase[CS] = uint32(cs) << 4
			}
		}

	// RETF imm16
	case 0xCA:
		imm := c.fetch16()
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
			cs := c.pop16()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, cs); err != nil {
					return err
				}
			} else {
				c.seg[CS] = cs
				c.segBase[CS] = uint32(cs) << 4
			}
			c.SetReg16(SP, c.GetReg16(SP)+imm)
		} else {
			c.eip = c.pop32()
			cs := c.pop32()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, uint16(cs)); err != nil {
					return err
				}
			} else {
				c.seg[CS] = uint16(cs)
				c.segBase[CS] = uint32(cs) << 4
			}
			c.SetReg32(ESP, c.GetReg32(ESP)+uint32(imm))
		}

	// ENTER imm16, imm8
	case 0xC8:
		size := c.fetch16()
		nesting := c.fetch8()
		c.push32(c.GetReg32(EBP))
		frame := c.GetReg32(ESP)
		if nesting > 0 {
			for i := uint8(1); i < nesting; i++ {
				c.SetReg32(EBP, c.GetReg32(EBP)-4)
				c.push32(c.readMem32(c.segBase[SS] + c.GetReg32(EBP)))
			}
			c.push32(frame)
		}
		c.SetReg32(EBP, frame)
		c.SetReg32(ESP, c.GetReg32(ESP)-uint32(size))

	// LEAVE
	case 0xC9:
		c.SetReg32(ESP, c.GetReg32(EBP))
		c.SetReg32(EBP, c.pop32())

	// BSWAP r32
	case 0x0F:
		opcode2 := c.fetch8()
		switch opcode2 {
		case 0x00:
			return c.handleGroupO_00()
		case 0x01:
			return c.handleGroupO_01()
		case 0x20:
			// MOV r32, CR0-CR7
			return c.handleMovCR(true)
		case 0x22:
			// MOV CR0-CR7, r32
			return c.handleMovCR(false)
		case 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8A, 0x8B, 0x8C, 0x8D, 0x8E, 0x8F:
			return c.handleJccNear(opcode2, operandSize)
		case 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97,
			0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F:
			return c.handleSETcc(opcode2)
		case 0xA0:
			// PUSH FS
			c.push32(uint32(c.seg[FS]))
		case 0xA1:
			// POP FS
			c.seg[FS] = uint16(c.pop32())
		case 0xA8:
			// PUSH GS
			c.push32(uint32(c.seg[GS]))
		case 0xA9:
			// POP GS
			c.seg[GS] = uint16(c.pop32())
		case 0xA2:
			// CPUID
			c.handleCPUID()
		case 0xA4:
			// SHLD r/m16/32, r16/32, imm8
			mr := c.parseModRM()
			count := c.fetch8() & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst << count) | (src >> (16 - count))
					cf := (dst >> (16 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBase[DS]+mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst << count) | (src >> (32 - count))
					cf := (dst >> (32 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBase[DS]+mr.ea, result)
					}
				}
			}
		case 0xA5:
			// SHLD r/m16/32, r16/32, CL
			mr := c.parseModRM()
			count := c.GetReg8(CL) & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst << count) | (src >> (16 - count))
					cf := (dst >> (16 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBase[DS]+mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst << count) | (src >> (32 - count))
					cf := (dst >> (32 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBase[DS]+mr.ea, result)
					}
				}
			}
		case 0xAC:
			// SHRD r/m16/32, r16/32, imm8
			mr := c.parseModRM()
			count := c.fetch8() & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst >> count) | (src << (16 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBase[DS]+mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst >> count) | (src << (32 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBase[DS]+mr.ea, result)
					}
				}
			}
		case 0xAD:
			// SHRD r/m16/32, r16/32, CL
			mr := c.parseModRM()
			count := c.GetReg8(CL) & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst >> count) | (src << (16 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBase[DS]+mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBase[DS] + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst >> count) | (src << (32 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBase[DS]+mr.ea, result)
					}
				}
			}
		case 0x30:
			// WRMSR — write MSR (stub: ignore)
			// ECX = MSR number, EDX:EAX = value
		case 0x31:
			// RDTSC
			c.handleRDTSC()
		case 0x32:
			// RDMSR — read MSR (stub: return 0)
			c.SetReg32(EAX, 0)
			c.SetReg32(EDX, 0)
		case 0xAF:
			// IMUL r32, r/m32
			mr := c.parseModRM()
			var v int32
			if mr.isReg {
				v = int32(c.GetReg32(int(mr.rm)))
			} else {
				v = int32(c.readMem32(c.segBase[DS] + mr.ea))
			}
			r := int64(int32(c.GetReg32(int(mr.reg)))) * int64(v)
			c.SetReg32(int(mr.reg), uint32(r))
			c.setOF(r != int64(int32(r)))
			c.setCF(r != int64(int32(r)))
		case 0xB0:
			// CMPXCHG r/m8, r8
			mr := c.parseModRM()
			var dst uint8
			if mr.isReg {
				dst = c.GetReg8(reg8FromModRM(int(mr.rm)))
			} else {
				dst = c.readMem8(c.segBase[DS] + mr.ea)
			}
			acc := c.GetReg8(AL)
			src := c.GetReg8(reg8FromModRM(int(mr.reg)))
			if acc == dst {
				c.setZF(true)
				if mr.isReg {
					c.SetReg8(reg8FromModRM(int(mr.rm)), src)
				} else {
					c.writeMem8(c.segBase[DS]+mr.ea, src)
				}
			} else {
				c.setZF(false)
				c.SetReg8(AL, dst)
			}
		case 0xB1:
			// CMPXCHG r/m16/32, r16/32
			mr := c.parseModRM()
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBase[DS] + mr.ea)
				}
				acc := c.GetReg16(AX)
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if acc == dst {
					c.setZF(true)
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), src)
					} else {
						c.writeMem16(c.segBase[DS]+mr.ea, src)
					}
				} else {
					c.setZF(false)
					c.SetReg16(AX, dst)
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBase[DS] + mr.ea)
				}
				acc := c.GetReg32(EAX)
				src := c.GetReg32(int(mr.reg))
				if acc == dst {
					c.setZF(true)
					if mr.isReg {
						c.SetReg32(int(mr.rm), src)
					} else {
						c.writeMem32(c.segBase[DS]+mr.ea, src)
					}
				} else {
					c.setZF(false)
					c.SetReg32(EAX, dst)
				}
			}
		case 0xA3:
			// BT r/m16/32, r16/32
			mr := c.parseModRM()
			var bit, mask uint32
			if operandSize == 2 {
				if mr.isReg {
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					c.setZF((c.GetReg16(reg16FromModRM(int(mr.rm))) & uint16(mask)) == 0)
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					c.setZF((c.readMem16(addr) & uint16(mask)) == 0)
				}
			} else {
				if mr.isReg {
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					c.setZF((c.GetReg32(int(mr.rm)) & mask) == 0)
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					c.setZF((c.readMem32(addr) & mask) == 0)
				}
			}
		case 0xAB:
			// BTS r/m16/32, r16/32
			mr := c.parseModRM()
			var bit, mask uint32
			if operandSize == 2 {
				if mr.isReg {
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					v := c.GetReg16(reg16FromModRM(int(mr.rm)))
					c.setZF((v & uint16(mask)) == 0)
					c.SetReg16(reg16FromModRM(int(mr.rm)), v|uint16(mask))
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					v := c.readMem16(addr)
					c.setZF((v & uint16(mask)) == 0)
					c.writeMem16(addr, v|uint16(mask))
				}
			} else {
				if mr.isReg {
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					v := c.GetReg32(int(mr.rm))
					c.setZF((v & mask) == 0)
					c.SetReg32(int(mr.rm), v|mask)
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					v := c.readMem32(addr)
					c.setZF((v & mask) == 0)
					c.writeMem32(addr, v|mask)
				}
			}
		case 0xB3:
			// BTR r/m16/32, r16/32
			mr := c.parseModRM()
			var bit, mask uint32
			if operandSize == 2 {
				if mr.isReg {
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					v := c.GetReg16(reg16FromModRM(int(mr.rm)))
					c.setZF((v & uint16(mask)) == 0)
					c.SetReg16(reg16FromModRM(int(mr.rm)), v&^uint16(mask))
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					v := c.readMem16(addr)
					c.setZF((v & uint16(mask)) == 0)
					c.writeMem16(addr, v&^uint16(mask))
				}
			} else {
				if mr.isReg {
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					v := c.GetReg32(int(mr.rm))
					c.setZF((v & mask) == 0)
					c.SetReg32(int(mr.rm), v&^mask)
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					v := c.readMem32(addr)
					c.setZF((v & mask) == 0)
					c.writeMem32(addr, v&^mask)
				}
			}
		case 0xBA:
			// BT/BTS/BTR/BTC r/m16/32, imm8
			mr := c.parseModRM()
			imm := uint32(c.fetch8())
			switch mr.reg {
			case 4: // BT
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						c.setZF((c.GetReg16(reg16FromModRM(int(mr.rm))) & mask) == 0)
					} else {
						c.setZF((c.readMem16(c.segBase[DS]+mr.ea) & mask) == 0)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						c.setZF((c.GetReg32(int(mr.rm)) & mask) == 0)
					} else {
						c.setZF((c.readMem32(c.segBase[DS]+mr.ea) & mask) == 0)
					}
				}
			case 5: // BTS
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						v := c.GetReg16(reg16FromModRM(int(mr.rm)))
						c.setZF((v & mask) == 0)
						c.SetReg16(reg16FromModRM(int(mr.rm)), v|mask)
					} else {
						addr := c.segBase[DS] + mr.ea
						v := c.readMem16(addr)
						c.setZF((v & mask) == 0)
						c.writeMem16(addr, v|mask)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						v := c.GetReg32(int(mr.rm))
						c.setZF((v & mask) == 0)
						c.SetReg32(int(mr.rm), v|mask)
					} else {
						addr := c.segBase[DS] + mr.ea
						v := c.readMem32(addr)
						c.setZF((v & mask) == 0)
						c.writeMem32(addr, v|mask)
					}
				}
			case 6: // BTR
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						v := c.GetReg16(reg16FromModRM(int(mr.rm)))
						c.setZF((v & mask) == 0)
						c.SetReg16(reg16FromModRM(int(mr.rm)), v&^mask)
					} else {
						addr := c.segBase[DS] + mr.ea
						v := c.readMem16(addr)
						c.setZF((v & mask) == 0)
						c.writeMem16(addr, v&^mask)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						v := c.GetReg32(int(mr.rm))
						c.setZF((v & mask) == 0)
						c.SetReg32(int(mr.rm), v&^mask)
					} else {
						addr := c.segBase[DS] + mr.ea
						v := c.readMem32(addr)
						c.setZF((v & mask) == 0)
						c.writeMem32(addr, v&^mask)
					}
				}
			case 7: // BTC
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						v := c.GetReg16(reg16FromModRM(int(mr.rm)))
						c.setZF((v & mask) == 0)
						c.SetReg16(reg16FromModRM(int(mr.rm)), v^mask)
					} else {
						addr := c.segBase[DS] + mr.ea
						v := c.readMem16(addr)
						c.setZF((v & mask) == 0)
						c.writeMem16(addr, v^mask)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						v := c.GetReg32(int(mr.rm))
						c.setZF((v & mask) == 0)
						c.SetReg32(int(mr.rm), v^mask)
					} else {
						addr := c.segBase[DS] + mr.ea
						v := c.readMem32(addr)
						c.setZF((v & mask) == 0)
						c.writeMem32(addr, v^mask)
					}
				}
			default:
				return fmt.Errorf("0F BA /%d not implemented", mr.reg)
			}
		case 0xBB:
			// BTC r/m16/32, r16/32
			mr := c.parseModRM()
			var bit, mask uint32
			if operandSize == 2 {
				if mr.isReg {
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					v := c.GetReg16(reg16FromModRM(int(mr.rm)))
					c.setZF((v & uint16(mask)) == 0)
					c.SetReg16(reg16FromModRM(int(mr.rm)), v^uint16(mask))
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask = 1 << bit
					v := c.readMem16(addr)
					c.setZF((v & uint16(mask)) == 0)
					c.writeMem16(addr, v^uint16(mask))
				}
			} else {
				if mr.isReg {
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					v := c.GetReg32(int(mr.rm))
					c.setZF((v & mask) == 0)
					c.SetReg32(int(mr.rm), v^mask)
				} else {
					addr := c.segBase[DS] + mr.ea
					bit = uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask = 1 << bit
					v := c.readMem32(addr)
					c.setZF((v & mask) == 0)
					c.writeMem32(addr, v^mask)
				}
			}
		case 0xB6:
			// MOVZX r32, r/m8
			mr := c.parseModRM()
			var v uint32
			if mr.isReg {
				v = uint32(c.GetReg8(reg8FromModRM(int(mr.rm))))
			} else {
				v = uint32(c.readMem8(c.segBase[DS] + mr.ea))
			}
			c.SetReg32(int(mr.reg), v)
		case 0xB7:
			// MOVZX r32, r/m16
			mr := c.parseModRM()
			var v uint32
			if mr.isReg {
				v = uint32(c.GetReg16(reg16FromModRM(int(mr.rm))))
			} else {
				v = uint32(c.readMem16(c.segBase[DS] + mr.ea))
			}
			c.SetReg32(int(mr.reg), v)
		case 0xBE:
			// MOVSX r32, r/m8
			mr := c.parseModRM()
			var v int32
			if mr.isReg {
				v = int32(int8(c.GetReg8(reg8FromModRM(int(mr.rm)))))
			} else {
				v = int32(int8(c.readMem8(c.segBase[DS] + mr.ea)))
			}
			c.SetReg32(int(mr.reg), uint32(v))
		case 0xBF:
			// MOVSX r32, r/m16
			mr := c.parseModRM()
			var v int32
			if mr.isReg {
				v = int32(int16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
			} else {
				v = int32(int16(c.readMem16(c.segBase[DS] + mr.ea)))
			}
			c.SetReg32(int(mr.reg), uint32(v))
		case 0xC8, 0xC9, 0xCA, 0xCB, 0xCC, 0xCD, 0xCE, 0xCF:
			// BSWAP r32
			r := opcode2 & 7
			v := c.GetReg32(int(r))
			c.SetReg32(int(r), (v>>24)|((v>>8)&0xFF00)|((v<<8)&0xFF0000)|(v<<24))
		default:
			return fmt.Errorf("unimplemented 0F opcode: %02X", opcode2)
		}

	default:
		return fmt.Errorf("unimplemented opcode: %02X at EIP=%08X", opcode, c.eip-1)
	}
	return nil
}

// executeString executes a string instruction with optional REP prefix.
func (c *CPU) executeString(repPrefix uint8, size uint32,
	op func(), checkZF bool, zfExpected bool) {
	if repPrefix == 0 {
		op()
		return
	}
	for c.GetReg32(ECX) != 0 {
		c.SetReg32(ECX, c.GetReg32(ECX)-1)
		op()
		if checkZF {
			if zfExpected && !c.getZF() {
				break
			}
			if !zfExpected && c.getZF() {
				break
			}
		}
		if c.GetReg32(ECX) == 0 {
			break
		}
	}
}

// fetchMoffs reads a 16/32-bit offset for moffs addressing.
// In real mode / 16-bit address size, this is 16 bits.
// For now, we assume 16-bit real mode.
func (c *CPU) fetchMoffs() uint32 {
	if c.currentAddrSize == 4 {
		return c.fetch32()
	}
	return uint32(c.fetch16())
}

// push16 pushes a 16-bit value onto the stack.
func (c *CPU) push16(v uint16) {
	c.SetReg16(SP, c.GetReg16(SP)-2)
	c.checkStackLimit(uint32(c.GetReg16(SP)), 2)
	c.writeMem16(c.segBase[SS]+uint32(c.GetReg16(SP)), v)
}

// pop16 pops a 16-bit value from the stack.
func (c *CPU) pop16() uint16 {
	v := c.readMem16(c.segBase[SS] + uint32(c.GetReg16(SP)))
	c.SetReg16(SP, c.GetReg16(SP)+2)
	return v
}

// push32 pushes a 32-bit value onto the stack.
func (c *CPU) push32(v uint32) {
	c.SetReg32(ESP, c.GetReg32(ESP)-4)
	c.checkStackLimit(c.GetReg32(ESP), 4)
	c.writeMem32(c.segBase[SS]+c.GetReg32(ESP), v)
}

// pop32 pops a 32-bit value from the stack.
func (c *CPU) pop32() uint32 {
	v := c.readMem32(c.segBase[SS] + c.GetReg32(ESP))
	c.SetReg32(ESP, c.GetReg32(ESP)+4)
	return v
}

// pushOp pushes a value of the given operand size (2 or 4).
// checkIOPL raises #GP if the current CPL is greater than the IOPL.
func (c *CPU) checkIOPL() {
	if c.IsProtectedMode() {
		iopl := (c.eflags & EFLAGS_IOPL) >> 12
		if c.cpl > int(iopl) {
			c.raiseGeneralProtectionFault(0)
		}
	}
}

func (c *CPU) pushOp(v uint32, size uint8) {
	if size == 2 {
		c.push16(uint16(v))
	} else {
		c.push32(v)
	}
}

// popOp pops a value of the given operand size (2 or 4).
func (c *CPU) popOp(size uint8) uint32 {
	if size == 2 {
		return uint32(c.pop16())
	}
	return c.pop32()
}

// handleInterrupt services a software or hardware interrupt.
// In real mode, it reads the IVT at physical address vector*4.
// In protected mode, it reads the IDT gate descriptor.
func (c *CPU) handleInterrupt(vector uint8, isHardware bool, errorCode ...uint32) error {
	// Hardware interrupts wake the CPU from HLT.
	if isHardware {
		c.powerDown = false
	}
	if !c.IsProtectedMode() {
		// Real mode: push FLAGS, CS, IP; clear IF and TF; jump to IVT entry.
		c.push16(uint16(c.eflags))
		c.push16(c.seg[CS])
		c.push16(uint16(c.eip))
		c.eflags &^= EFLAGS_IF | EFLAGS_TF
		ivtAddr := uint32(vector) * 4
		newIP := c.readMem16(ivtAddr)
		newCS := c.readMem16(ivtAddr + 2)
		c.seg[CS] = newCS
		c.segBase[CS] = uint32(newCS) << 4
		c.eip = uint32(newIP)
		c.maskEIP()
		return nil
	}

	// Protected mode: read IDT gate descriptor.
	idtBase := c.segBase[IDTR]
	idtLimit := c.segLimit[IDTR]
	offset := uint32(vector) * 8
	if offset+7 > idtLimit {
		return fmt.Errorf("interrupt vector %02X exceeds IDT limit", vector)
	}

	gateAddr := idtBase + offset
	var gate [8]byte
	for i := 0; i < 8; i++ {
		gate[i] = c.readMem8(gateAddr + uint32(i))
	}

	offsetLow := uint32(gate[0]) | uint32(gate[1])<<8
	selector := uint16(gate[2]) | uint16(gate[3])<<8
	// byte 4 is unused/reserved
	gateType := gate[5] & 0x0F
	dpl := (gate[5] >> 5) & 3
	present := gate[5]&0x80 != 0
	offsetHigh := uint32(gate[6]) | uint32(gate[7])<<8

	if !present {
		return fmt.Errorf("interrupt gate not present for vector %02X", vector)
	}

	// For software interrupts (INT n), check DPL >= CPL.
	if !isHardware && dpl < uint8(c.cpl) {
		return fmt.Errorf("INT %02X: DPL < CPL", vector)
	}

	// Validate and load CS.
	if selector == 0 {
		return fmt.Errorf("interrupt gate null CS selector for vector %02X", vector)
	}
	oldCPL := c.cpl
	oldCS := c.seg[CS]
	oldEIP := c.eip
	oldEFLAGS := c.eflags
	if err := c.LoadSegmentProtected(CS, selector); err != nil {
		return fmt.Errorf("interrupt gate load CS failed for vector %02X: %w", vector, err)
	}

	csAccess := c.segAccess[CS]
	csType := (csAccess >> 0) & 0x0F
	// Code segment types are 0x8-0xF; bit 3 must be set.
	if csType&0x08 == 0 {
		return fmt.Errorf("interrupt gate CS is not code for vector %02X", vector)
	}
	csDPL := uint8((csAccess >> 5) & 3)
	if csDPL > uint8(oldCPL) {
		return fmt.Errorf("interrupt gate CS DPL > CPL for vector %02X", vector)
	}

	// For non-conforming code segments, CPL becomes the DPL of the code segment.
	// For conforming segments, CPL does not change.
	isConforming := (csType&0x04 != 0) // types 0xC-0xF with bit 2 set
	if !isConforming {
		c.cpl = int(csDPL)
	}

	// Determine operand size from gate type.
	// 0xE = 386 interrupt gate, 0xF = 386 trap gate (32-bit)
	// 0x6 = 286 interrupt gate, 0x7 = 286 trap gate (16-bit)
	gateSize := uint8(2)
	if gateType == 0x0E || gateType == 0x0F {
		gateSize = 4
	}

	// Stack switch if going to a more privileged ring (lower DPL number).
	// Conforming segments do not change CPL, so no stack switch occurs.
	if !isConforming && oldCPL > int(csDPL) {
		// Read new SS:ESP from the TSS.
		tssBase := c.segBase[TR]
		tssLimit := c.segLimit[TR]
		// TSS offsets: ESPn at 0x04 + n*8, SSn at 0x08 + n*8
		espOffset := uint32(4 + csDPL*8)
		ssOffset := uint32(8 + csDPL*8)
		if ssOffset+1 > tssLimit {
			return fmt.Errorf("TSS limit too small for stack switch to ring %d", csDPL)
		}
		newESP := c.readMem32(tssBase + espOffset)
		newSS := c.readMem16(tssBase + ssOffset)

		oldSS := c.seg[SS]
		oldESP := c.GetReg32(ESP)

		// Load new SS.
		if err := c.LoadSegmentProtected(SS, newSS); err != nil {
			return fmt.Errorf("stack switch load SS failed: %w", err)
		}
		// Set new ESP.
		c.SetReg32(ESP, newESP)

		// Push old SS and old ESP onto the new stack.
		if gateSize == 2 {
			c.push16(oldSS)
			c.push16(uint16(oldESP))
		} else {
			c.push32(uint32(oldSS))
			c.push32(oldESP)
		}
	}

	// Push FLAGS, CS, EIP (using saved old values).
	c.pushOp(oldEFLAGS, gateSize)
	c.pushOp(uint32(oldCS), gateSize)
	c.pushOp(oldEIP, gateSize)

	// Push error code if provided (for exceptions like #PF).
	// Error codes are always 32-bit in 32-bit protected mode, even for 16-bit gates.
	if len(errorCode) > 0 {
		c.push32(errorCode[0])
	}

	// Clear VM bit when entering handler from v8086 mode.
	c.eflags &^= EFLAGS_VM

	// Interrupt gate (but not trap gate) clears IF and TF.
	if gateType == 0x06 || gateType == 0x0E {
		c.eflags &^= EFLAGS_IF | EFLAGS_TF
	}

	// Set RF to suppress debug exceptions on the first instruction of the handler.
	c.eflags |= EFLAGS_RF

	// Jump to handler.
	c.eip = offsetLow | offsetHigh<<16
	c.maskEIP()
	return nil
}

// ioRead8 reads from an I/O port.
func (c *CPU) ioRead8(port uint16) uint8 {
	if c.ioRead8Func != nil {
		return c.ioRead8Func(port)
	}
	return 0xFF
}

// ioRead16 reads from an I/O port.
func (c *CPU) ioRead16(port uint16) uint16 {
	if c.ioRead16Func != nil {
		return c.ioRead16Func(port)
	}
	return 0xFFFF
}

// ioRead32 reads from an I/O port.
func (c *CPU) ioRead32(port uint16) uint32 {
	if c.ioRead32Func != nil {
		return c.ioRead32Func(port)
	}
	return 0xFFFFFFFF
}

// ioWrite8 writes to an I/O port.
func (c *CPU) ioWrite8(port uint16, val uint8) {
	if c.ioWrite8Func != nil {
		c.ioWrite8Func(port, val)
	}
}

// ioWrite16 writes to an I/O port.
func (c *CPU) ioWrite16(port uint16, val uint16) {
	if c.ioWrite16Func != nil {
		c.ioWrite16Func(port, val)
	}
}

// ioWrite32 writes to an I/O port.
func (c *CPU) ioWrite32(port uint16, val uint32) {
	if c.ioWrite32Func != nil {
		c.ioWrite32Func(port, val)
	}
}
