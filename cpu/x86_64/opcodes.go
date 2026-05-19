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

	// ===== Group 5 (Inc/Dec/Call/Jmp/Push) =====

	case op == 0xFF:
		return c.opGroup5(rex, operandSize)

	// ===== Two-byte escape =====

	case op == 0x0F:
		return c.opTwoByte(rex, operandSize, segOverride)
	}

	return unimplemented("opcode %#02x rex=%#x", op, rex)
}

// opTwoByte dispatches the 0x0F escape opcode family. M2 covers the
// near-form Jcc set (0x80..0x8F rel32). MOVZX/MOVSX and the SSE/AVX
// space come later.
func (c *CPU) opTwoByte(rex, operandSize uint8, segOverride int) error {
	_ = segOverride
	_ = operandSize
	op2 := c.fetch8()
	switch {
	case op2 >= 0x80 && op2 <= 0x8F:
		// Jcc rel32 — even in 64-bit mode the displacement is 32 bits
		// sign-extended (operand-size override would shrink to 16 but
		// modern code never uses that).
		disp := int64(int32(c.fetch32()))
		if c.evalCond(op2 & 0xF) {
			c.rip = uint64(int64(c.rip) + disp)
		}
		return nil
	}
	return unimplemented("0F %#02x rex=%#x", op2, rex)
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
