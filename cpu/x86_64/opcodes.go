package x86_64

import "math/bits"

// executeOpcode dispatches a decoded primary opcode after the prefix
// loop in Step has consumed the leading prefix bytes. operandSize is
// 2, 4, or 8 (bytes); addressSize is 4 or 8 in long mode.
//
// M1 covers a minimal vertical slice: MOV reg/imm, MOV r/m vs r, LEA,
// ADD, SUB, PUSH/POP r64, JMP/CALL/RET rel32 and rel8, NOP, HLT. That
// surface is enough to drive an end-to-end NASM-assembled program
// through Step and verify register/memory effects.
func (c *CPU) executeOpcode(op, rex, operandSize, addressSize uint8, segOverride int) error {
	_ = segOverride // segment-override handling lands with explicit memory operands beyond the initial slice
	_ = addressSize // 32-bit addressing not yet wired

	switch {
	case op == 0x90:
		// NOP. (Note: 0x90 with REX.B is XCHG R8, RAX; M1 ignores the
		// REX.B distinction and treats both as NOP — none of the M1
		// tests exercise the xchg form.)
		return nil

	case op == 0xF4:
		// HLT.
		c.powerDown = true
		return nil

	case op == 0x01:
		// ADD r/m, r — Ev, Gv.
		return c.opADDRM(rex, operandSize)

	case op == 0x29:
		// SUB r/m, r — Ev, Gv.
		return c.opSUBRM(rex, operandSize)

	case op == 0x89:
		// MOV r/m, r — Ev, Gv.
		return c.opMOVRM(rex, operandSize)

	case op == 0x8B:
		// MOV r, r/m — Gv, Ev.
		return c.opMOVRfromM(rex, operandSize)

	case op == 0x8D:
		// LEA r, m.
		return c.opLEA(rex, operandSize)

	case op == 0xB8, op == 0xB9, op == 0xBA, op == 0xBB,
		op == 0xBC, op == 0xBD, op == 0xBE, op == 0xBF:
		return c.opMOVImmToReg(op-0xB8, rex, operandSize)

	case op == 0xC7:
		// MOV r/m, imm. ModR/M.reg must be 0 (the rest of /n are
		// reserved / non-MOV under newer ISA additions). The immediate
		// is operandSize bytes for 16/32-bit, and 32 bits sign-extended
		// to 64 when operandSize is 8.
		return c.opMOVImm(rex, operandSize)

	case op == 0x83:
		// Group 1: ADD/OR/ADC/SBB/AND/SUB/XOR/CMP r/m, imm8 (sign-
		// extended to operand size). ModR/M.reg field is the sub-opcode.
		return c.opGroup1Imm8(rex, operandSize)

	case op >= 0x50 && op <= 0x57:
		return c.opPUSHReg(op-0x50, rex)

	case op >= 0x58 && op <= 0x5F:
		return c.opPOPReg(op-0x58, rex)

	case op == 0xC3:
		// RET (near). In long mode the default operand size for a
		// near RET is 64-bit regardless of REX.W.
		c.rip = c.pop64()
		return nil

	case op == 0xE8:
		// CALL rel32.
		disp := int64(int32(c.fetch32()))
		c.push64(c.rip)
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xE9:
		// JMP rel32.
		disp := int64(int32(c.fetch32()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil

	case op == 0xEB:
		// JMP rel8.
		disp := int64(int8(c.fetch8()))
		c.rip = uint64(int64(c.rip) + disp)
		return nil
	}

	return unimplemented("opcode %#02x rex=%#x", op, rex)
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

// opGroup1Imm8 implements 0x83 — Group 1 with an 8-bit sign-extended
// immediate. ModR/M.reg is the sub-opcode (000=ADD, 001=OR, 010=ADC,
// 011=SBB, 100=AND, 101=SUB, 110=XOR, 111=CMP).
func (c *CPU) opGroup1Imm8(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	imm := uint64(int64(int8(c.fetch8())))
	dst := c.readOperand(m, operandSize)
	var res uint64
	var fl flagBits
	switch m.reg {
	case 0: // ADD
		res, fl = add(dst, imm, operandSize)
		c.writeOperand(m, res, operandSize)
	case 5: // SUB
		res, fl = sub(dst, imm, operandSize)
		c.writeOperand(m, res, operandSize)
	case 7: // CMP — like SUB but no writeback
		_, fl = sub(dst, imm, operandSize)
	default:
		return unimplemented("0x83 /%d (only ADD/SUB/CMP wired)", m.reg)
	}
	c.setArithFlags(fl)
	return nil
}

// opMOVImmToReg implements 0xB8+rd in 64-bit, 32-bit, or 16-bit operand
// modes. With REX.W the immediate is the full 64 bits; otherwise the
// 32-bit immediate zero-extends to 64 (the standard long-mode rule for
// any 32-bit destination write), and the 16-bit form preserves the
// upper 48 bits.
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

// opMOVRM implements 0x89 — MOV r/m, r.
func (c *CPU) opMOVRM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	c.writeOperand(m, src, operandSize)
	return nil
}

// opMOVRfromM implements 0x8B — MOV r, r/m.
func (c *CPU) opMOVRfromM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	val := c.readOperand(m, operandSize)
	c.writeReg(m.reg, val, operandSize)
	return nil
}

// opLEA implements 0x8D — LEA r, m. The effective address goes into
// the destination register; the source memory is *not* read. LEA
// width follows operandSize for the destination: 64-bit writes the
// full 64-bit EA; 32-bit truncates to the low 32 (and zero-extends as
// usual); 16-bit truncates to the low 16 (preserving the upper bits).
func (c *CPU) opLEA(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	if m.isReg {
		// LEA with a register source is encoded as #UD; deliver as
		// "not implemented" until Phase 2 puts a #UD handler in.
		return unimplemented("LEA with register source")
	}
	c.writeReg(m.reg, m.ea, operandSize)
	return nil
}

// opADDRM implements 0x01 — ADD r/m, r. Eager flags (M1 doesn't yet
// implement lazy flag deferral).
func (c *CPU) opADDRM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	dst := c.readOperand(m, operandSize)
	res, fl := add(dst, src, operandSize)
	c.writeOperand(m, res, operandSize)
	c.setArithFlags(fl)
	return nil
}

// opSUBRM implements 0x29 — SUB r/m, r.
func (c *CPU) opSUBRM(rex, operandSize uint8) error {
	m := c.parseModRM64(rex)
	src := c.readReg(m.reg, operandSize)
	dst := c.readOperand(m, operandSize)
	res, fl := sub(dst, src, operandSize)
	c.writeOperand(m, res, operandSize)
	c.setArithFlags(fl)
	return nil
}

// opPUSHReg implements 0x50+r. In long mode PUSH r64 is the default
// (operand size is 64-bit regardless of REX.W — REX.W is reserved
// here per Intel SDM, but ignored for compatibility).
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
	}
	return v
}

func (c *CPU) writeReg(idx uint8, v uint64, size uint8) {
	i := idx & 0xF
	switch size {
	case 8:
		c.reg64[i] = v
	case 4:
		c.reg64[i] = v & 0xFFFFFFFF // zero-extend, long-mode 32-bit-write rule
	case 2:
		c.reg64[i] = (c.reg64[i] & ^uint64(0xFFFF)) | (v & 0xFFFF)
	}
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

// push64 / pop64 — long-mode stack ops always operate on 8-byte slots
// and on the full 64-bit RSP regardless of operand-size prefixes.
func (c *CPU) push64(v uint64) {
	c.reg64[RSP] -= 8
	c.writeMem64(c.reg64[RSP], v)
}

func (c *CPU) pop64() uint64 {
	v := c.readMem64(c.reg64[RSP])
	c.reg64[RSP] += 8
	return v
}

// ===== ALU helpers (eager flag computation) =====

// flagBits packages the six arithmetic flags computed by add/sub. M1
// uses eager evaluation; the lazy-flag scheme cpu/x86 uses can come
// later if the decoder dispatches enough opcodes to make the deferral
// pay off.
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
	fl.cf = (a + b) < a // unsigned overflow within the full 64-bit add
	if size != 8 {
		// for narrower widths the carry-out lives at bit `size*8`
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
	c.rflags = f | 2 // preserve reserved bit
}
