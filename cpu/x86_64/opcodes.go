package x86_64

import "math/bits"

// executeOpcode dispatches a decoded primary opcode after the prefix
// loop in Step has consumed the leading prefix bytes. operandSize is
// 2, 4, or 8 (bytes); addressSize is 4 or 8 in long mode.
//
// The surface covers the M1 vertical slice (MOV, LEA, ADD, SUB, PUSH/
// POP, CALL/RET, JMP, NOP, HLT) plus the M2 expansion: conditional
// jumps Jcc (rel8 and rel32 via 0x0F escape), the full Group 1 family
// (ADD/OR/ADC/SBB/AND/SUB/XOR/CMP), bitwise ops in their primary "rm
// vs r" forms (0x09 OR, 0x21 AND, 0x29 SUB, 0x31 XOR, 0x39 CMP), TEST
// r/m,r (0x85), and Group 5 INC/DEC (0xFF /0, /1).
func (c *CPU) executeOpcode(op, rex, operandSize, addressSize uint8, segOverride int) error {
	_ = segOverride // segment-override handling lands with explicit memory operands beyond the initial slice
	_ = addressSize // 32-bit addressing not yet wired

	switch {
	// ===== Single-byte primary ops =====

	case op == 0x90:
		// NOP. (0x90 with REX.B is XCHG R8,RAX; not exercised in M1/M2.)
		return nil

	case op == 0xF4:
		c.powerDown = true
		return nil

	// ===== ALU (Ev,Gv = 0x_1, Gv,Ev = 0x_3) =====

	case op == 0x01: // ADD r/m, r
		return c.opALURM(rex, operandSize, aluADD)
	case op == 0x03: // ADD r, r/m
		return c.opALURfromM(rex, operandSize, aluADD)

	case op == 0x09: // OR r/m, r
		return c.opALURM(rex, operandSize, aluOR)
	case op == 0x0B: // OR r, r/m
		return c.opALURfromM(rex, operandSize, aluOR)

	case op == 0x21: // AND r/m, r
		return c.opALURM(rex, operandSize, aluAND)
	case op == 0x23: // AND r, r/m
		return c.opALURfromM(rex, operandSize, aluAND)

	case op == 0x29: // SUB r/m, r
		return c.opALURM(rex, operandSize, aluSUB)
	case op == 0x2B: // SUB r, r/m
		return c.opALURfromM(rex, operandSize, aluSUB)

	case op == 0x31: // XOR r/m, r
		return c.opALURM(rex, operandSize, aluXOR)
	case op == 0x33: // XOR r, r/m
		return c.opALURfromM(rex, operandSize, aluXOR)

	case op == 0x39: // CMP r/m, r
		return c.opALURM(rex, operandSize, aluCMP)
	case op == 0x3B: // CMP r, r/m
		return c.opALURfromM(rex, operandSize, aluCMP)

	case op == 0x85: // TEST r/m, r
		return c.opTEST(rex, operandSize)

	// ALU rAX, imm forms (single-byte primary opcode + imm). The imm is
	// imm16 in operandSize=2 mode and imm32 sign-extended-to-64
	// otherwise. AL,imm8 byte forms (0x04/0x0C/...) are not implemented
	// — none of M2's tests need them.
	case op == 0x05:
		return c.opALUImmRAX(operandSize, aluADD)
	case op == 0x0D:
		return c.opALUImmRAX(operandSize, aluOR)
	case op == 0x25:
		return c.opALUImmRAX(operandSize, aluAND)
	case op == 0x2D:
		return c.opALUImmRAX(operandSize, aluSUB)
	case op == 0x35:
		return c.opALUImmRAX(operandSize, aluXOR)
	case op == 0x3D:
		return c.opALUImmRAX(operandSize, aluCMP)

	// ===== MOV family =====

	case op == 0x89:
		return c.opMOVRM(rex, operandSize)
	case op == 0x8B:
		return c.opMOVRfromM(rex, operandSize)
	case op == 0x8D:
		return c.opLEA(rex, operandSize)

	case op == 0xB8, op == 0xB9, op == 0xBA, op == 0xBB,
		op == 0xBC, op == 0xBD, op == 0xBE, op == 0xBF:
		return c.opMOVImmToReg(op-0xB8, rex, operandSize)

	case op == 0xC7:
		return c.opMOVImm(rex, operandSize)

	// ===== Group 1 (immediate) =====

	case op == 0x81:
		// Group 1 r/m, imm16/imm32 (sign-extended to 64).
		return c.opGroup1(rex, operandSize, false)
	case op == 0x83:
		// Group 1 r/m, imm8 (sign-extended).
		return c.opGroup1(rex, operandSize, true)

	// ===== Stack =====

	case op >= 0x50 && op <= 0x57:
		return c.opPUSHReg(op-0x50, rex)
	case op >= 0x58 && op <= 0x5F:
		return c.opPOPReg(op-0x58, rex)

	// ===== Control flow =====

	case op == 0xC3:
		c.rip = c.pop64()
		return nil

	case op == 0xE8:
		disp := int64(int32(c.fetch32()))
		c.push64(c.rip)
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xE9:
		disp := int64(int32(c.fetch32()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xEB:
		disp := int64(int8(c.fetch8()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op >= 0x70 && op <= 0x7F:
		// Conditional jump rel8.
		disp := int64(int8(c.fetch8()))
		if c.evalCond(op & 0xF) {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	// ===== Group 3 (TEST/NOT/NEG/MUL/IMUL/DIV/IDIV) =====

	case op == 0xF7:
		return c.opGroup3(rex, operandSize)

	// ===== IMUL signed-integer forms =====

	case op == 0x69:
		// IMUL r, r/m, imm32 (sign-extended to operand size).
		return c.opIMULImm(rex, operandSize, false)
	case op == 0x6B:
		// IMUL r, r/m, imm8 (sign-extended).
		return c.opIMULImm(rex, operandSize, true)

	// ===== Group 2 (shifts and rotates) =====

	case op == 0xD1:
		// SHL/SHR/SAR r/m, 1 — count is implicit.
		return c.opGroup2(rex, operandSize, 1)
	case op == 0xD3:
		// SHL/SHR/SAR r/m, CL — count comes from CL register.
		return c.opGroup2(rex, operandSize, uint64(c.GetReg8(CL)))
	case op == 0xC1:
		// SHL/SHR/SAR r/m, imm8 — count is an 8-bit immediate.
		return c.opGroup2(rex, operandSize, 0) // count read inside opGroup2

	// ===== Sign-extending integer move (MOVSXD r64, r/m32) =====

	case op == 0x63:
		// In long mode 0x63 is MOVSXD (in 32-bit mode it was ARPL).
		// Destination is the reg field, full 64 bits; source is a
		// 32-bit r/m that gets sign-extended.
		return c.opMOVSXD(rex)

	// ===== Group 5 (Inc/Dec/Call/Jmp/Push) =====

	case op == 0xFF:
		return c.opGroup5(rex, operandSize)

	// ===== Flag manipulation =====

	case op == 0xF5: // CMC — complement CF
		c.rflags ^= RFLAGS_CF
		return nil
	case op == 0xF8: // CLC — clear CF
		c.rflags &^= RFLAGS_CF
		return nil
	case op == 0xF9: // STC — set CF
		c.rflags |= RFLAGS_CF
		return nil
	case op == 0xFA: // CLI — clear IF
		c.rflags &^= RFLAGS_IF
		return nil
	case op == 0xFB: // STI — set IF
		c.rflags |= RFLAGS_IF
		return nil
	case op == 0xFC: // CLD — clear DF
		c.rflags &^= RFLAGS_DF
		return nil
	case op == 0xFD: // STD — set DF
		c.rflags |= RFLAGS_DF
		return nil

	// ===== Two-byte escape =====

	case op == 0x0F:
		return c.opTwoByte(rex, operandSize, segOverride)
	}

	return unimplemented("opcode %#02x rex=%#x", op, rex)
}

// opTwoByte dispatches the 0x0F escape opcode family.
func (c *CPU) opTwoByte(rex, operandSize uint8, segOverride int) error {
	_ = segOverride
	op2 := c.fetch8()
	switch {
	case op2 == 0x00:
		// Group 6 — SLDT/STR/LLDT/LTR/VERR/VERW. Only LTR and LLDT
		// are wired for now (and just stash the selector).
		return c.opGroup6(rex)

	case op2 == 0x01:
		// Group 7 — SGDT/SIDT/LGDT/LIDT/SMSW/LMSW/INVLPG.
		return c.opGroup7(rex)

	case op2 == 0x0B:
		// UD2 — guaranteed-invalid-opcode instruction. Used by the
		// kernel as a fault-on-purpose marker (BUG_ON, WARN_ON).
		// Phase 5c delivers as #UD through the IDT.
		return unimplemented("UD2 (#UD delivery pending)")

	case op2 == 0x20:
		// MOV r64, CRn — reads control register into a GPR. The ModR/M
		// is the unusual "register-register" form where mod is treated
		// as 11 regardless of its actual value (the operand size is
		// always 64 bits in long mode).
		return c.opMovFromCR(rex)
	case op2 == 0x22:
		return c.opMovToCR(rex)
	case op2 == 0x21:
		return c.opMovFromDR(rex)
	case op2 == 0x23:
		return c.opMovToDR(rex)

	case op2 == 0x30:
		return c.opWRMSR()
	case op2 == 0x32:
		return c.opRDMSR()
	case op2 >= 0x40 && op2 <= 0x4F:
		// CMOVcc r, r/m — conditional move. Operand size follows the
		// usual rules (REX.W → 64, 0x66 → 16, else 32). Destination
		// reg always gets updated *with the source* if the condition
		// holds; otherwise no change to the destination — and crucially
		// no zero-extension of the upper bits in the fall-through case.
		m := c.parseModRM64(rex)
		src := c.readOperand(m, operandSize)
		if c.evalCond(op2 & 0xF) {
			c.writeReg(m.reg, src, operandSize)
		}
		return nil

	case op2 >= 0x80 && op2 <= 0x8F:
		// Jcc rel32 — even in 64-bit mode the displacement is 32 bits
		// sign-extended (operand-size override could shrink to 16 but
		// modern code never uses that).
		disp := int64(int32(c.fetch32()))
		if c.evalCond(op2 & 0xF) {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil

	case op2 >= 0x90 && op2 <= 0x9F:
		// SETcc r/m8 — store 1 if condition holds, 0 otherwise. The
		// destination is always 8 bits regardless of operand-size prefix.
		m := c.parseModRM64(rex)
		var v uint8
		if c.evalCond(op2 & 0xF) {
			v = 1
		}
		if m.isReg {
			c.write8FromModRM(m, v)
		} else {
			c.writeMem8(m.ea, v)
		}
		return nil

	case op2 == 0xAF:
		// IMUL r, r/m — two-operand signed multiply, destination is reg.
		return c.opIMUL2Op(rex, operandSize)

	case op2 >= 0xC8 && op2 <= 0xCF:
		// BSWAP r{32,64}: byte-swap the destination register. Only
		// defined for 32- and 64-bit operand sizes; the 16-bit form
		// is "undefined" per Intel SDM (we still permit it for
		// completeness).
		idx := uint8(op2 - 0xC8)
		if rex&rexB != 0 {
			idx |= 0x8
		}
		c.reg64[idx] = bswap(c.reg64[idx], operandSize)
		return nil

	case op2 == 0xB6:
		// MOVZX r, r/m8.
		return c.opMOVZX(rex, operandSize, 1)
	case op2 == 0xB7:
		// MOVZX r, r/m16.
		return c.opMOVZX(rex, operandSize, 2)
	case op2 == 0xBE:
		// MOVSX r, r/m8.
		return c.opMOVSX(rex, operandSize, 1)
	case op2 == 0xBF:
		// MOVSX r, r/m16.
		return c.opMOVSX(rex, operandSize, 2)
	}
	return unimplemented("0F %#02x rex=%#x", op2, rex)
}

// opGroup6 dispatches 0x0F 0x00 — SLDT/STR/LLDT/LTR/VERR/VERW. M5b
// only needs LLDT and LTR; the others are deferred. LLDT/LTR just
// stash the selector in seg[LDTR]/seg[TR]; the full descriptor walk
// happens lazily when the segment is consulted.
func (c *CPU) opGroup6(rex uint8) error {
	m := c.parseModRM64(rex)
	switch m.reg {
	case 0: // SLDT — store LDTR selector
		c.writeOperand(m, uint64(c.seg[LDTR]), 2)
		return nil
	case 1: // STR — store TR selector
		c.writeOperand(m, uint64(c.seg[TR]), 2)
		return nil
	case 2: // LLDT
		sel := uint16(c.readOperand(m, 2))
		c.seg[LDTR] = sel
		return nil
	case 3: // LTR
		sel := uint16(c.readOperand(m, 2))
		c.seg[TR] = sel
		return nil
	}
	return unimplemented("Group 6 /%d", m.reg)
}

// opGroup7 dispatches 0x0F 0x01. Sub-ops 0..3 use a memory operand
// holding a pseudo-descriptor; 4/6 use a 16-bit operand; 7 is per-
// page TLB invalidation. mod=11 forms (XGETBV, MONITOR, etc.) are
// not yet wired.
func (c *CPU) opGroup7(rex uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		// Many mod=11 forms exist (XGETBV at reg=2,rm=0; SWAPGS at
		// reg=7,rm=0 — that one is M6); not yet routed.
		switch {
		case m.reg == 7 && m.rm == 0:
			// SWAPGS — atomic swap of GS.base and KernelGSBase. Lives
			// at 0x0F 0x01 0xF8.
			c.msrGSBase, c.msrKernelGSBase = c.msrKernelGSBase, c.msrGSBase
			c.segBase[GS] = c.msrGSBase
			return nil
		}
		return unimplemented("Group 7 mod=11 reg=%d rm=%d", m.reg, m.rm)
	}
	switch m.reg {
	case 0: // SGDT
		c.writeMem16(m.ea, uint16(c.segLimit[GDTR]))
		c.writeMem64(m.ea+2, c.segBase[GDTR])
		return nil
	case 1: // SIDT
		c.writeMem16(m.ea, uint16(c.segLimit[IDTR]))
		c.writeMem64(m.ea+2, c.segBase[IDTR])
		return nil
	case 2: // LGDT — load GDT base+limit from memory
		c.segLimit[GDTR] = uint32(c.readMem16(m.ea))
		c.segBase[GDTR] = c.readMem64(m.ea + 2)
		return nil
	case 3: // LIDT
		c.segLimit[IDTR] = uint32(c.readMem16(m.ea))
		c.segBase[IDTR] = c.readMem64(m.ea + 2)
		return nil
	case 4: // SMSW — store low 16 of CR0
		c.writeOperand(m, c.cr[0]&0xFFFF, 2)
		return nil
	case 6: // LMSW — set low 4 bits of CR0 (PE/MP/EM/TS). Cannot clear PE.
		v := uint16(c.readOperand(m, 2))
		c.cr[0] = (c.cr[0] &^ 0xF) | uint64(v&0xF)
		c.recomputeMode()
		return nil
	case 7: // INVLPG — invalidate single TLB entry. No TLB yet, so no-op.
		return nil
	}
	return unimplemented("Group 7 /%d", m.reg)
}

// opMovFromCR / opMovToCR — 0x0F 0x20 / 0x22. The ModR/M byte: reg is
// the CRn index (0..7, REX.R doesn't normally extend in long mode but
// some implementations honor it for CR8 access on AMD). rm is the
// GPR. mod is ignored (treated as 11).
func (c *CPU) opMovFromCR(rex uint8) error {
	mb := c.fetch8()
	cr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexR != 0 {
		cr |= 0x8
	}
	if rex&rexB != 0 {
		rm |= 0x8
	}
	c.reg64[rm&0xF] = c.cr[cr&0x7]
	return nil
}

func (c *CPU) opMovToCR(rex uint8) error {
	mb := c.fetch8()
	cr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexR != 0 {
		cr |= 0x8
	}
	if rex&rexB != 0 {
		rm |= 0x8
	}
	v := c.reg64[rm&0xF]
	c.writeCR(int(cr&0x7), v)
	return nil
}

// writeCR centralises the CR update + side-effects. Writes to CR0
// (PE/PG bits) and CR4 (PAE) can flip the long-mode-active latch
// (LMA), which gates the mode field. The recomputeMode call here
// keeps c.mode coherent without the rest of the decoder needing to
// know which CR was written.
func (c *CPU) writeCR(n int, v uint64) {
	if n == 0 {
		oldPG := c.cr[0]&CR0_PG != 0
		newPG := v&CR0_PG != 0
		c.cr[0] = v
		// LMA latches when paging is enabled with LME set; clears when
		// paging turns off.
		if !oldPG && newPG && c.efer&EFER_LME != 0 {
			c.efer |= EFER_LMA
		} else if oldPG && !newPG {
			c.efer &^= EFER_LMA
		}
		c.recomputeMode()
		return
	}
	c.cr[n] = v
	if n == 4 {
		c.recomputeMode()
	}
}

func (c *CPU) opMovFromDR(rex uint8) error {
	mb := c.fetch8()
	dr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexB != 0 {
		rm |= 0x8
	}
	c.reg64[rm&0xF] = c.dr[dr&0x7]
	return nil
}

func (c *CPU) opMovToDR(rex uint8) error {
	mb := c.fetch8()
	dr := (mb >> 3) & 7
	rm := mb & 7
	if rex&rexB != 0 {
		rm |= 0x8
	}
	c.dr[dr&0x7] = c.reg64[rm&0xF]
	return nil
}

// MSR numbers we route. Unrecognized MSRs return zero on RDMSR and
// silently drop writes on WRMSR — real hardware raises #GP, but the
// boot path passes through several MSRs we don't model.
const (
	msrEFER          = 0xC0000080
	msrSTAR          = 0xC0000081
	msrLSTAR         = 0xC0000082
	msrCSTAR         = 0xC0000083
	msrSFMASK        = 0xC0000084
	msrFSBaseMSR     = 0xC0000100
	msrGSBaseMSR     = 0xC0000101
	msrKernelGSBase  = 0xC0000102
)

func (c *CPU) opRDMSR() error {
	v := c.readMSR(c.GetReg32(ECX))
	c.SetReg32(EAX, uint32(v))
	c.SetReg32(EDX, uint32(v>>32))
	return nil
}

func (c *CPU) opWRMSR() error {
	num := c.GetReg32(ECX)
	v := uint64(c.GetReg32(EAX)) | (uint64(c.GetReg32(EDX)) << 32)
	return c.writeMSR(num, v)
}

func (c *CPU) readMSR(num uint32) uint64 {
	switch num {
	case msrEFER:
		return c.efer
	case msrSTAR:
		return c.msrStar
	case msrLSTAR:
		return c.msrLstar
	case msrCSTAR:
		return c.msrCstar
	case msrSFMASK:
		return c.msrSFMask
	case msrFSBaseMSR:
		return c.msrFSBase
	case msrGSBaseMSR:
		return c.msrGSBase
	case msrKernelGSBase:
		return c.msrKernelGSBase
	}
	return 0
}

func (c *CPU) writeMSR(num uint32, v uint64) error {
	switch num {
	case msrEFER:
		// Setting LME may not flip LMA until paging is enabled — but
		// if paging is already on, LMA latches now.
		c.efer = v
		if c.cr[0]&CR0_PG != 0 && c.efer&EFER_LME != 0 {
			c.efer |= EFER_LMA
		}
		c.recomputeMode()
	case msrSTAR:
		c.msrStar = v
	case msrLSTAR:
		c.msrLstar = v
	case msrCSTAR:
		c.msrCstar = v
	case msrSFMASK:
		c.msrSFMask = v
	case msrFSBaseMSR:
		c.msrFSBase = v
		c.segBase[FS] = v
	case msrGSBaseMSR:
		c.msrGSBase = v
		c.segBase[GS] = v
	case msrKernelGSBase:
		c.msrKernelGSBase = v
	}
	return nil
}

// opMOVZX implements MOVZX r, r/m{8,16}. srcSize is 1 or 2; the
// destination width follows operandSize and writes through writeReg,
// which already handles the 32-bit-write-zero-extends-to-64 rule.
func (c *CPU) opMOVZX(rex, operandSize, srcSize uint8) error {
	m := c.parseModRM64(rex)
	var src uint64
	switch {
	case m.isReg && srcSize == 1:
		src = uint64(c.read8FromModRM(m))
	case m.isReg:
		src = c.readReg(m.rm, srcSize)
	case srcSize == 1:
		src = uint64(c.readMem8(m.ea))
	default:
		src = uint64(c.readMem16(m.ea))
	}
	c.writeReg(m.reg, src, operandSize)
	return nil
}

// opMOVSX implements MOVSX r, r/m{8,16}.
func (c *CPU) opMOVSX(rex, operandSize, srcSize uint8) error {
	m := c.parseModRM64(rex)
	var src uint64
	switch {
	case m.isReg && srcSize == 1:
		src = uint64(c.read8FromModRM(m))
	case m.isReg:
		src = c.readReg(m.rm, srcSize)
	case srcSize == 1:
		src = uint64(c.readMem8(m.ea))
	default:
		src = uint64(c.readMem16(m.ea))
	}
	if srcSize == 1 {
		src = uint64(int64(int8(src)))
	} else {
		src = uint64(int64(int16(src)))
	}
	c.writeReg(m.reg, src, operandSize)
	return nil
}

// opMOVSXD implements 0x63 — MOVSXD r64, r/m32. The 32-bit source is
// sign-extended into the full 64-bit destination. (In 32-bit mode this
// opcode was ARPL; long mode reuses the encoding.)
func (c *CPU) opMOVSXD(rex uint8) error {
	m := c.parseModRM64(rex)
	var src uint32
	if m.isReg {
		src = uint32(c.readReg(m.rm, 4))
	} else {
		src = c.readMem32(m.ea)
	}
	c.writeReg(m.reg, uint64(int64(int32(src))), 8)
	return nil
}

// evalCond evaluates the four-bit condition code (the low nibble of
// the conditional opcode). Order matches Intel SDM Vol 1 Appendix B:
//
//	0 O    1 NO   2 B/C   3 NB/NC   4 Z/E   5 NZ/NE   6 BE/NA   7 NBE/A
//	8 S    9 NS   A P/PE  B NP/PO   C L     D NL/GE   E LE/NG   F NLE/G
func (c *CPU) evalCond(cc uint8) bool {
	fl := c.rflags
	cf := fl&RFLAGS_CF != 0
	zf := fl&RFLAGS_ZF != 0
	sf := fl&RFLAGS_SF != 0
	of := fl&RFLAGS_OF != 0
	pf := fl&RFLAGS_PF != 0
	switch cc {
	case 0x0:
		return of
	case 0x1:
		return !of
	case 0x2:
		return cf
	case 0x3:
		return !cf
	case 0x4:
		return zf
	case 0x5:
		return !zf
	case 0x6:
		return cf || zf
	case 0x7:
		return !cf && !zf
	case 0x8:
		return sf
	case 0x9:
		return !sf
	case 0xA:
		return pf
	case 0xB:
		return !pf
	case 0xC:
		return sf != of
	case 0xD:
		return sf == of
	case 0xE:
		return zf || sf != of
	case 0xF:
		return !zf && sf == of
	}
	return false
}

// ===== ALU dispatch =====

type aluOp int

const (
	aluADD aluOp = iota
	aluOR
	aluADC
	aluSBB
	aluAND
	aluSUB
	aluXOR
	aluCMP
)

// aluApply runs op over (dst, src) at the given operand size, returning
// (result, flags). For ADC/SBB the caller is responsible for folding
// in CF; M2 doesn't implement those yet (the ALU helpers below skip
// them). For the bitwise ops (AND, OR, XOR) CF and OF are cleared.
func aluApply(op aluOp, dst, src uint64, size uint8) (uint64, flagBits) {
	switch op {
	case aluADD:
		return add(dst, src, size)
	case aluSUB, aluCMP:
		return sub(dst, src, size)
	case aluAND:
		r := (dst & src) & mask(size)
		return r, logicalFlags(r, size)
	case aluOR:
		r := (dst | src) & mask(size)
		return r, logicalFlags(r, size)
	case aluXOR:
		r := (dst ^ src) & mask(size)
		return r, logicalFlags(r, size)
	}
	return 0, flagBits{}
}

// opALURM handles the "r/m, r" form (e.g. 0x01 ADD, 0x29 SUB, 0x39 CMP).
func (c *CPU) opALURM(rex, operandSize uint8, op aluOp) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	dst := c.readOperand(m, operandSize)
	res, fl := aluApply(op, dst, src, operandSize)
	if op != aluCMP {
		c.writeOperand(m, res, operandSize)
	}
	c.setArithFlags(fl)
	return nil
}

// opALURfromM handles the "r, r/m" form (e.g. 0x03 ADD, 0x2B SUB,
// 0x3B CMP). Destination is the reg field.
func (c *CPU) opALURfromM(rex, operandSize uint8, op aluOp) error {
	m := c.parseModRM64(rex)
	src := c.readOperand(m, operandSize)
	dst := c.readReg(m.reg, operandSize)
	res, fl := aluApply(op, dst, src, operandSize)
	if op != aluCMP {
		c.writeReg(m.reg, res, operandSize)
	}
	c.setArithFlags(fl)
	return nil
}

// opALUImmRAX handles the 0x05/0x0D/0x25/0x2D/0x35/0x3D family —
// "op rAX, imm". imm is operandSize bytes (max 32, then sign-extended
// to 64 in the 8-byte case).
func (c *CPU) opALUImmRAX(operandSize uint8, op aluOp) error {
	var imm uint64
	switch operandSize {
	case 2:
		imm = uint64(c.fetch16())
	default:
		imm = uint64(int64(int32(c.fetch32())))
	}
	dst := c.readReg(RAX, operandSize)
	res, fl := aluApply(op, dst, imm, operandSize)
	if op != aluCMP {
		c.writeReg(RAX, res, operandSize)
	}
	c.setArithFlags(fl)
	return nil
}

// opGroup3 dispatches 0xF7 — Group 3 with non-byte operand size.
// Sub-ops: 0=TEST r/m,imm, 1=reserved, 2=NOT, 3=NEG, 4=MUL, 5=IMUL,
// 6=DIV, 7=IDIV. The 0xF6 byte-operand variant is not yet wired.
func (c *CPU) opGroup3(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	switch m.reg {
	case 0, 1: // TEST r/m, imm
		var imm uint64
		switch operandSize {
		case 2:
			imm = uint64(c.fetch16())
		default:
			imm = uint64(int64(int32(c.fetch32())))
		}
		dst := c.readOperand(m, operandSize)
		r := (dst & imm) & mask(operandSize)
		c.setArithFlags(logicalFlags(r, operandSize))
		return nil
	case 2: // NOT r/m — does not affect flags
		dst := c.readOperand(m, operandSize)
		c.writeOperand(m, ^dst&mask(operandSize), operandSize)
		return nil
	case 3: // NEG r/m
		dst := c.readOperand(m, operandSize)
		res, fl := sub(0, dst, operandSize)
		fl.cf = dst != 0 // CF = "source was non-zero" per SDM (overrides sub's CF rule)
		c.writeOperand(m, res, operandSize)
		c.setArithFlags(fl)
		return nil
	case 4: // MUL r/m — unsigned: rDX:rAX = rAX × r/m
		dst := c.readOperand(m, operandSize)
		return c.opMUL(dst, operandSize)
	case 5: // IMUL r/m (one-operand) — signed: rDX:rAX = rAX × r/m
		dst := c.readOperand(m, operandSize)
		return c.opIMUL1Op(dst, operandSize)
	case 6: // DIV r/m — unsigned
		dst := c.readOperand(m, operandSize)
		return c.opDIV(dst, operandSize)
	case 7: // IDIV r/m — signed
		dst := c.readOperand(m, operandSize)
		return c.opIDIV(dst, operandSize)
	}
	return unimplemented("Group 3 /%d", m.reg)
}

// opMUL: unsigned multiply of rAX (operandSize wide) by src; the
// product fills rDX:rAX. CF and OF are set if the upper half is
// nonzero — i.e. the product didn't fit in the low operand-width.
func (c *CPU) opMUL(src uint64, operandSize uint8) error {
	a := c.readReg(RAX, operandSize) & mask(operandSize)
	src &= mask(operandSize)
	var hi, lo uint64
	switch operandSize {
	case 8:
		hi, lo = bits.Mul64(a, src)
	default:
		prod := a * src
		hi = prod >> (uint(operandSize) * 8)
		lo = prod & mask(operandSize)
	}
	// Write back. For 8-bit MUL the entire product lands in AX (no
	// RDX); we don't model the 8-bit form yet so 16/32/64 only.
	c.writeReg(RAX, lo, operandSize)
	c.writeReg(RDX, hi, operandSize)
	var fl flagBits
	fl.cf = hi != 0
	fl.of = hi != 0
	// SF/ZF/PF/AF are "undefined" per SDM; setArithFlags clears them
	// (which is a common but not architecturally guaranteed result).
	c.setArithFlags(fl)
	return nil
}

// opIMUL1Op: one-operand signed multiply rDX:rAX = rAX × src. CF/OF
// set if the result doesn't fit in operandSize bits (the high half
// is not just the sign extension of the low half).
func (c *CPU) opIMUL1Op(src uint64, operandSize uint8) error {
	a := c.readReg(RAX, operandSize) & mask(operandSize)
	src &= mask(operandSize)
	// Sign-extend both to 64 bits, multiply, then split.
	as := signExtend(a, operandSize)
	ss := signExtend(src, operandSize)
	prod128hi, prod128lo := mul128(uint64(as), uint64(ss))
	// Low half goes to rAX, high half to rDX, at operandSize width.
	c.writeReg(RAX, prod128lo, operandSize)
	c.writeReg(RDX, prod128hi, operandSize)
	// CF/OF: set if the sign-extended low half does NOT equal the full
	// 128-bit product. Equivalent: hi part not just sign-fill of lo.
	expectedHi := uint64(0)
	if prod128lo&signBit(operandSize) != 0 {
		expectedHi = mask(operandSize) // all-ones in the operand-width
	}
	// For operandSize < 8 we only compare within the operand width.
	var fl flagBits
	fl.cf = prod128hi&mask(operandSize) != expectedHi
	fl.of = fl.cf
	c.setArithFlags(fl)
	return nil
}

// opIMUL2Op: 0x0F 0xAF — IMUL r, r/m. Destination = reg = reg × r/m.
// Only the low operandSize bits of the product are kept; flags
// indicate whether the truncation lost meaningful information.
func (c *CPU) opIMUL2Op(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	a := signExtend(c.readReg(m.reg, operandSize), operandSize)
	b := signExtend(c.readOperand(m, operandSize), operandSize)
	prod128hi, prod128lo := mul128(uint64(a), uint64(b))
	c.writeReg(m.reg, prod128lo, operandSize)
	expectedHi := uint64(0)
	if prod128lo&signBit(operandSize) != 0 {
		expectedHi = mask(operandSize)
	}
	var fl flagBits
	fl.cf = prod128hi&mask(operandSize) != expectedHi
	fl.of = fl.cf
	c.setArithFlags(fl)
	return nil
}

// opIMULImm: 0x69 (imm32 sign-extended) or 0x6B (imm8 sign-extended)
// — three-operand IMUL: destination = r/m × imm. ModR/M.reg is the
// destination; ModR/M.rm is the source.
func (c *CPU) opIMULImm(rex, operandSize uint8, imm8 bool) error {
	m := c.parseModRM64(rex)
	var imm int64
	if imm8 {
		imm = int64(int8(c.fetch8()))
	} else {
		switch operandSize {
		case 2:
			imm = int64(int16(c.fetch16()))
		default:
			imm = int64(int32(c.fetch32()))
		}
	}
	src := signExtend(c.readOperand(m, operandSize), operandSize)
	prod128hi, prod128lo := mul128(uint64(src), uint64(imm))
	c.writeReg(m.reg, prod128lo, operandSize)
	expectedHi := uint64(0)
	if prod128lo&signBit(operandSize) != 0 {
		expectedHi = mask(operandSize)
	}
	var fl flagBits
	fl.cf = prod128hi&mask(operandSize) != expectedHi
	fl.of = fl.cf
	c.setArithFlags(fl)
	return nil
}

// opDIV: unsigned divide of rDX:rAX by src, quotient to rAX,
// remainder to rDX. Divide-by-zero or quotient overflow raises #DE on
// real hardware; we surface as an error for now (Phase 7 wires up
// #DE delivery through the IDT).
func (c *CPU) opDIV(src uint64, operandSize uint8) error {
	src &= mask(operandSize)
	if src == 0 {
		return unimplemented("#DE on division by zero — IDT delivery pending")
	}
	switch operandSize {
	case 8:
		hi := c.GetReg64(RDX)
		lo := c.GetReg64(RAX)
		if hi >= src {
			return unimplemented("#DE on quotient overflow — IDT delivery pending")
		}
		q, r := bits.Div64(hi, lo, src)
		c.SetReg64(RAX, q)
		c.SetReg64(RDX, r)
	default:
		// 32-bit: dividend = EDX:EAX (assembled into a 64-bit value).
		hi := uint64(c.readReg(RDX, operandSize))
		lo := uint64(c.readReg(RAX, operandSize))
		dividend := (hi << (uint(operandSize) * 8)) | lo
		q := dividend / src
		r := dividend % src
		if q > mask(operandSize) {
			return unimplemented("#DE on quotient overflow")
		}
		c.writeReg(RAX, q, operandSize)
		c.writeReg(RDX, r, operandSize)
	}
	// DIV leaves SF/ZF/PF/AF/CF/OF undefined; we clear them.
	c.setArithFlags(flagBits{})
	return nil
}

// opIDIV: signed counterpart.
func (c *CPU) opIDIV(src uint64, operandSize uint8) error {
	srcS := signExtend(src&mask(operandSize), operandSize)
	if srcS == 0 {
		return unimplemented("#DE on signed division by zero")
	}
	switch operandSize {
	case 8:
		hi := int64(c.GetReg64(RDX))
		lo := c.GetReg64(RAX)
		// Compose a signed 128-bit dividend and divide. Most cases the
		// dividend fits in 64 signed bits (RDX is just sign-extension of
		// RAX). M4 handles that common case; the full 128-bit IDIV
		// arrives if/when we see real code emit it.
		if hi != int64(-1) && hi != 0 {
			return unimplemented("IDIV with non-trivial RDX hi half")
		}
		dividend := int64(lo)
		q := dividend / srcS
		r := dividend % srcS
		c.SetReg64(RAX, uint64(q))
		c.SetReg64(RDX, uint64(r))
	default:
		hi := signExtend(c.readReg(RDX, operandSize), operandSize)
		lo := uint64(c.readReg(RAX, operandSize))
		// Compose signed dividend at 2× width.
		dividend := (hi << (uint(operandSize) * 8)) | int64(lo)
		q := dividend / srcS
		r := dividend % srcS
		c.writeReg(RAX, uint64(q), operandSize)
		c.writeReg(RDX, uint64(r), operandSize)
	}
	c.setArithFlags(flagBits{})
	return nil
}

// signExtend interprets v (operandSize bits) as a signed value and
// sign-extends it into a Go int64.
func signExtend(v uint64, operandSize uint8) int64 {
	switch operandSize {
	case 1:
		return int64(int8(v))
	case 2:
		return int64(int16(v))
	case 4:
		return int64(int32(v))
	default:
		return int64(v)
	}
}

// mul128 returns the high and low 64 bits of a signed 64x64
// multiplication. Goes through unsigned bits.Mul64 and corrects the
// high half using the "subtract-on-negative" identity.
func mul128(a, b uint64) (uint64, uint64) {
	hi, lo := bits.Mul64(a, b)
	if int64(a) < 0 {
		hi -= b
	}
	if int64(b) < 0 {
		hi -= a
	}
	return hi, lo
}

// bswap byte-swaps the low operandSize bytes of v (BSWAP). The 16-bit
// form is "undefined" per Intel SDM (older silicon zeroed the result);
// we mirror what most modern CPUs do: leave upper bits unchanged and
// swap the low pair.
func bswap(v uint64, operandSize uint8) uint64 {
	switch operandSize {
	case 8:
		return bits.ReverseBytes64(v)
	case 4:
		return uint64(bits.ReverseBytes32(uint32(v)))
	case 2:
		return (v & ^uint64(0xFFFF)) | uint64(bits.ReverseBytes16(uint16(v)))
	}
	return v
}

// opGroup2 dispatches the shift/rotate family — 0xD1 (count=1),
// 0xD3 (count=CL), 0xC1 (count=imm8). Sub-op /4=SHL, /5=SHR, /7=SAR.
// /0..3 (ROL/ROR/RCL/RCR) are deferred until needed.
//
// If implicitCount is 0 (the 0xC1 case), the immediate count is read
// from the instruction stream; otherwise the caller passes 1 or CL.
func (c *CPU) opGroup2(rex, operandSize uint8, implicitCount uint64) error {
	m := c.parseModRM64(rex)
	var count uint64
	if implicitCount == 0 {
		count = uint64(c.fetch8())
	} else {
		count = implicitCount
	}
	// Mask count per Intel SDM: 5 bits for 8/16/32 ops, 6 bits for 64.
	if operandSize == 8 {
		count &= 0x3F
	} else {
		count &= 0x1F
	}
	if count == 0 {
		return nil // flags unchanged
	}
	dst := c.readOperand(m, operandSize)
	var res uint64
	var fl flagBits
	switch m.reg {
	case 4, 6: // SHL / SAL (alias)
		// CF = bit shifted out of the high end on the last shift.
		// Pre-pad dst to 64 bits, shift, then mask. The shifted-out
		// bit is bit (size*8 - 1) of (dst << (count-1)).
		preShift := dst << (count - 1)
		fl.cf = preShift&signBit(operandSize) != 0
		res = (dst << count) & mask(operandSize)
		// OF (count==1): set if sign bit changed.
		if count == 1 {
			origSign := dst & signBit(operandSize)
			newSign := res & signBit(operandSize)
			fl.of = origSign != newSign
		}
	case 5: // SHR
		res = (dst & mask(operandSize)) >> count
		fl.cf = ((dst & mask(operandSize)) >> (count - 1) & 1) != 0
		if count == 1 {
			// OF for SHR-1 = high bit of original.
			fl.of = dst&signBit(operandSize) != 0
		}
	case 7: // SAR
		// Arithmetic right shift: sign-extend.
		signed := int64(dst & mask(operandSize))
		// Re-sign-extend from operandSize to 64 first.
		switch operandSize {
		case 4:
			signed = int64(int32(uint32(dst)))
		case 2:
			signed = int64(int16(uint16(dst)))
		case 1:
			signed = int64(int8(uint8(dst)))
		}
		fl.cf = uint64(signed>>(count-1))&1 != 0
		res = uint64(signed>>count) & mask(operandSize)
		// OF for SAR-1 = 0 (sign bit can't change).
	default:
		return unimplemented("Group 2 /%d", m.reg)
	}
	fl.zf = res == 0
	fl.sf = res&signBit(operandSize) != 0
	fl.pf = parity8(uint8(res))
	c.writeOperand(m, res, operandSize)
	c.setArithFlags(fl)
	return nil
}

// opTEST implements 0x85 — TEST r/m, r. Like AND but no writeback.
func (c *CPU) opTEST(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	dst := c.readOperand(m, operandSize)
	r := (dst & src) & mask(operandSize)
	c.setArithFlags(logicalFlags(r, operandSize))
	return nil
}

// opGroup1 dispatches 0x80/0x81/0x83. imm8 (true) reads a signed-8-bit
// immediate and sign-extends to operandSize; otherwise reads imm16 (for
// operandSize=2) or imm32 (for 4/8, sign-extending to 8). The sub-op
// (0..7) lives in ModR/M.reg.
func (c *CPU) opGroup1(rex, operandSize uint8, imm8 bool) error {
	m := c.parseModRM64(rex)
	var imm uint64
	if imm8 {
		imm = uint64(int64(int8(c.fetch8())))
	} else {
		switch operandSize {
		case 2:
			imm = uint64(int64(int16(c.fetch16())))
		default: // 4 or 8 — imm32, sign-extend to 64 in the 8 case
			imm = uint64(int64(int32(c.fetch32())))
		}
	}
	dst := c.readOperand(m, operandSize)
	var op aluOp
	switch m.reg {
	case 0:
		op = aluADD
	case 1:
		op = aluOR
	case 4:
		op = aluAND
	case 5:
		op = aluSUB
	case 6:
		op = aluXOR
	case 7:
		op = aluCMP
	default:
		return unimplemented("Group 1 /%d (ADC/SBB not implemented)", m.reg)
	}
	res, fl := aluApply(op, dst, imm, operandSize)
	if op != aluCMP {
		c.writeOperand(m, res, operandSize)
	}
	c.setArithFlags(fl)
	return nil
}

// opGroup5 dispatches 0xFF. Sub-ops: 0=INC, 1=DEC, 2=CALL, 3=CALLF,
// 4=JMP, 5=JMPF, 6=PUSH, 7=reserved. M2 wires the data ops; CALL/JMP
// indirect arrive when control flow gets more interesting.
func (c *CPU) opGroup5(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	dst := c.readOperand(m, operandSize)
	switch m.reg {
	case 0: // INC r/m
		res, fl := add(dst, 1, operandSize)
		c.writeOperand(m, res, operandSize)
		// INC/DEC preserve CF (Intel SDM); only OF/SF/ZF/AF/PF are
		// touched. Read the current CF and re-OR it after setArithFlags.
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	case 1: // DEC r/m
		res, fl := sub(dst, 1, operandSize)
		c.writeOperand(m, res, operandSize)
		oldCF := c.rflags & RFLAGS_CF
		c.setArithFlags(fl)
		c.rflags = (c.rflags &^ RFLAGS_CF) | oldCF
		return nil
	case 4: // JMP r/m (near, absolute indirect)
		// In long mode the operand is always 64-bit regardless of
		// operandSize — JMP r64. Use a 64-bit read.
		target := c.readOperand(m, 8)
		c.rip = target
		return nil
	case 6: // PUSH r/m
		c.push64(c.readOperand(m, 8))
		return nil
	}
	return unimplemented("Group 5 /%d", m.reg)
}

// opMOVImm implements 0xC7 /0 — MOV r/m, imm. In 64-bit operand mode
// the immediate is 32 bits, sign-extended to 64.
func (c *CPU) opMOVImm(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	if m.reg != 0 {
		return unimplemented("0xC7 with /%d (not MOV)", m.reg)
	}
	var v uint64
	switch operandSize {
	case 8:
		v = uint64(int64(int32(c.fetch32())))
	case 4:
		v = uint64(c.fetch32())
	case 2:
		v = uint64(c.fetch16())
	}
	c.writeOperand(m, v, operandSize)
	return nil
}

// opMOVImmToReg implements 0xB8+rd. REX.W → 64-bit imm; else 32-bit
// imm zero-extends to 64 (the long-mode rule); 16-bit form preserves
// the upper 48 bits.
func (c *CPU) opMOVImmToReg(rd, rex, operandSize uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	switch operandSize {
	case 8:
		c.reg64[idx] = c.fetch64()
	case 4:
		c.reg64[idx] = uint64(c.fetch32())
	case 2:
		c.reg64[idx] = (c.reg64[idx] & ^uint64(0xFFFF)) | uint64(c.fetch16())
	}
	return nil
}

func (c *CPU) opMOVRM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	c.writeOperand(m, src, operandSize)
	return nil
}

func (c *CPU) opMOVRfromM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	val := c.readOperand(m, operandSize)
	c.writeReg(m.reg, val, operandSize)
	return nil
}

func (c *CPU) opLEA(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		return unimplemented("LEA with register source")
	}
	c.writeReg(m.reg, m.ea, operandSize)
	return nil
}

func (c *CPU) opPUSHReg(rd, rex uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	c.push64(c.reg64[idx])
	return nil
}

func (c *CPU) opPOPReg(rd, rex uint8) error {
	idx := uint8(rd)
	if rex&rexB != 0 {
		idx |= 0x8
	}
	c.reg64[idx] = c.pop64()
	return nil
}

// ===== Operand helpers =====

func (c *CPU) readReg(idx, size uint8) uint64 {
	v := c.reg64[idx&0xF]
	switch size {
	case 8:
		return v
	case 4:
		return v & 0xFFFFFFFF
	case 2:
		return v & 0xFFFF
	case 1:
		// Width-1 access via this path is only legal when the caller
		// has already resolved AH/CH/DH/BH vs SPL/.../R15B for itself —
		// i.e. when REX was present on the instruction. Otherwise the
		// caller must use read8FromModRM with the rex byte. We do the
		// "REX-only low-byte" case here.
		return v & 0xFF
	}
	return v
}

func (c *CPU) writeReg(idx uint8, v uint64, size uint8) {
	i := idx & 0xF
	switch size {
	case 8:
		c.reg64[i] = v
	case 4:
		c.reg64[i] = v & 0xFFFFFFFF
	case 2:
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFFFF)) | (v & 0xFFFF)
	case 1:
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFF)) | (v & 0xFF)
	}
}

// read8FromModRM reads an 8-bit register value where the encoding may
// be the "no-REX" form (rm 4..7 are AH/CH/DH/BH) or the "REX present"
// form (rm 4..7 are SPL/BPL/SIL/DIL). modRMResult.hasREX picks.
//
// rmRaw is the ModRM.rm field with REX.B applied — i.e. m.rm.
func (c *CPU) read8FromModRM(m modRMResult) uint8 {
	if m.hasREX || m.rm < 4 {
		return uint8(c.reg64[m.rm&0xF])
	}
	// No-REX, rm in 4..7: AH/CH/DH/BH = high byte of reg64[rm-4].
	return uint8(c.reg64[m.rm-4] >> 8)
}

func (c *CPU) write8FromModRM(m modRMResult, v uint8) {
	if m.hasREX || m.rm < 4 {
		i := m.rm & 0xF
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFF)) | uint64(v)
		return
	}
	i := m.rm - 4
	c.reg64[i] = (c.reg64[i] & ^uint64(0xFF00)) | (uint64(v) << 8)
}

func (c *CPU) readOperand(m modRMResult, size uint8) uint64 {
	if m.isReg {
		return c.readReg(m.rm, size)
	}
	switch size {
	case 8:
		return c.readMem64(m.ea)
	case 4:
		return uint64(c.readMem32(m.ea))
	case 2:
		return uint64(c.readMem16(m.ea))
	}
	return uint64(c.readMem8(m.ea))
}

func (c *CPU) writeOperand(m modRMResult, v uint64, size uint8) {
	if m.isReg {
		c.writeReg(m.rm, v, size)
		return
	}
	switch size {
	case 8:
		c.writeMem64(m.ea, v)
	case 4:
		c.writeMem32(m.ea, uint32(v))
	case 2:
		c.writeMem16(m.ea, uint16(v))
	case 1:
		c.writeMem8(m.ea, uint8(v))
	}
}

func (c *CPU) push64(v uint64) {
	c.reg64[RSP] -= 8
	c.writeMem64(c.reg64[RSP], v)
}

func (c *CPU) pop64() uint64 {
	v := c.readMem64(c.reg64[RSP])
	c.reg64[RSP] += 8
	return v
}

// ===== ALU helpers =====

type flagBits struct {
	cf, pf, af, zf, sf, of bool
}

func mask(size uint8) uint64 {
	switch size {
	case 8:
		return 0xFFFFFFFF_FFFFFFFF
	case 4:
		return 0xFFFFFFFF
	case 2:
		return 0xFFFF
	}
	return 0xFF
}

func signBit(size uint8) uint64 {
	switch size {
	case 8:
		return 1 << 63
	case 4:
		return 1 << 31
	case 2:
		return 1 << 15
	}
	return 1 << 7
}

func add(a, b uint64, size uint8) (uint64, flagBits) {
	m := mask(size)
	a &= m
	b &= m
	r := (a + b) & m
	var fl flagBits
	fl.cf = (a + b) < a
	if size != 8 {
		fl.cf = (a+b)&(m+1) != 0
	}
	fl.af = ((a ^ b ^ r) & 0x10) != 0
	fl.zf = r == 0
	fl.sf = r&signBit(size) != 0
	fl.of = ((^(a ^ b)) & (a ^ r) & signBit(size)) != 0
	fl.pf = parity8(uint8(r))
	return r, fl
}

func sub(a, b uint64, size uint8) (uint64, flagBits) {
	m := mask(size)
	a &= m
	b &= m
	r := (a - b) & m
	var fl flagBits
	fl.cf = a < b
	fl.af = ((a ^ b ^ r) & 0x10) != 0
	fl.zf = r == 0
	fl.sf = r&signBit(size) != 0
	fl.of = ((a ^ b) & (a ^ r) & signBit(size)) != 0
	fl.pf = parity8(uint8(r))
	return r, fl
}

// logicalFlags computes the flag profile for bitwise ops (AND, OR, XOR,
// TEST). Per Intel SDM: CF and OF are cleared; SF, ZF, PF follow the
// result; AF is undefined (we leave it zero).
func logicalFlags(r uint64, size uint8) flagBits {
	return flagBits{
		zf: r == 0,
		sf: r&signBit(size) != 0,
		pf: parity8(uint8(r)),
	}
}

func parity8(v uint8) bool {
	return bits.OnesCount8(v)%2 == 0
}

func (c *CPU) setArithFlags(fl flagBits) {
	f := c.rflags
	f &^= RFLAGS_CF | RFLAGS_PF | RFLAGS_AF | RFLAGS_ZF | RFLAGS_SF | RFLAGS_OF
	if fl.cf {
		f |= RFLAGS_CF
	}
	if fl.pf {
		f |= RFLAGS_PF
	}
	if fl.af {
		f |= RFLAGS_AF
	}
	if fl.zf {
		f |= RFLAGS_ZF
	}
	if fl.sf {
		f |= RFLAGS_SF
	}
	if fl.of {
		f |= RFLAGS_OF
	}
	c.rflags = f | 2
}
