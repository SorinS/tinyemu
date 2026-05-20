package x86_64

// modRMResult holds the parsed ModR/M (+ optional SIB + displacement)
// for a single operand. For register operands isReg is true and rm
// holds the 0..15 register index (already extended by REX.B). For
// memory operands ea is the effective address and defaultSeg is the
// segment register implied by the addressing encoding (DS in most
// cases; SS when the encoding uses RSP/RBP as a base).
//
// hasREX records whether any REX prefix preceded the opcode. For
// 8-bit operands this changes how rm field bits 0..2 map to the
// register file: without REX, rm=4..7 means AH/CH/DH/BH (the high
// byte of the first four GPRs); with REX, rm=4..7 means SPL/BPL/SIL/
// DIL (low byte of RSP/RBP/RSI/RDI).
type modRMResult struct {
	mod         uint8
	reg         uint8 // 0..15, REX.R applied
	rm          uint8 // 0..15, REX.B applied for the register-operand case
	isReg       bool
	hasREX      bool
	ea          uint64
	defaultSeg  int
	ripRelative bool
}

// parseModRM64 parses ModR/M for 64-bit address-size mode (the default
// in long mode; the only address size supported for now).
//
// Equivalent to parseModRM64WithImm(rex, 0); use this form only when
// the instruction has no trailing immediate operand after the ModR/M
// (+ SIB + disp). When there IS a trailing immediate, you MUST use
// parseModRM64WithImm and pass its size — otherwise RIP-relative
// addressing computes the wrong effective address.
func (c *CPU) parseModRM64(rex uint8) modRMResult {
	return c.parseModRM64WithImm(rex, 0)
}

// parseModRM64WithImm parses ModR/M like parseModRM64 but accepts the
// size (in bytes) of the immediate operand that follows the ModR/M
// (+ SIB + disp) bytes. The immediate hasn't been fetched yet at this
// point — the caller fetches it after parseModRM64WithImm returns —
// so for RIP-relative addressing we need to add `immBytes` to the
// computed effective address. Per Intel SDM Vol 2 §2.2.1.6 the
// RIP-relative offset is "from the address of the instruction
// following the current one", which means past the immediate. Without
// the adjustment, `mov qword [rip+disp32], imm32` wrote to EA-4
// instead of EA — caught while debugging the Linux 6.18 boot.
func (c *CPU) parseModRM64WithImm(rex uint8, immBytes uint8) modRMResult {
	mb := c.fetch8()
	mod := (mb >> 6) & 3
	reg := (mb >> 3) & 7
	rm := mb & 7

	if rex&rexR != 0 {
		reg |= 0x8
	}

	r := modRMResult{
		mod:        mod,
		reg:        reg,
		defaultSeg: DS,
		hasREX:     rex != 0,
	}

	if mod == 3 {
		if rex&rexB != 0 {
			rm |= 0x8
		}
		r.rm = rm
		r.isReg = true
		return r
	}

	var ea uint64

	switch {
	case rm == 4:
		// SIB byte follows.
		sib := c.fetch8()
		scale := uint64(1) << ((sib >> 6) & 3)
		idx := (sib >> 3) & 7
		base := sib & 7

		var indexContrib uint64
		// SIB.index == 4 with REX.X=0 encodes "no index"; RSP cannot be
		// used as an index register. With REX.X=1, index=12 is R12 and
		// is a valid index.
		if !(idx == 4 && rex&rexX == 0) {
			idxReg := idx
			if rex&rexX != 0 {
				idxReg |= 0x8
			}
			indexContrib = c.reg64[idxReg] * scale
		}

		var baseContrib uint64
		if base == 5 && mod == 0 {
			// SIB with base==5, mod==00: disp32 instead of base register.
			// REX.B does not change this special case.
			baseContrib = uint64(int64(int32(c.fetch32())))
		} else {
			baseReg := base
			if rex&rexB != 0 {
				baseReg |= 0x8
			}
			baseContrib = c.reg64[baseReg]
			if base == 4 || base == 5 {
				// base==RSP(4) or RBP(5) selects SS as default segment.
				// (REX.B would map to R12/R13 but the SS default still
				// applies — the segment choice keys off the bottom 3
				// bits of the base field, not the extended register.)
				r.defaultSeg = SS
			}
		}

		ea = baseContrib + indexContrib

	case mod == 0 && rm == 5:
		// RIP-relative + disp32 in long mode. (In legacy 32-bit mode
		// this is "absolute disp32"; not supported by parseModRM64.)
		// The reference RIP is the start of the *next* instruction,
		// which lives `immBytes` past the end of the disp32 we just
		// fetched. See header comment.
		disp := int64(int32(c.fetch32()))
		ea = c.rip + uint64(disp) + uint64(immBytes)
		r.ripRelative = true

	default:
		// Register-indirect. The bottom 3 bits of rm key the default
		// segment (DS except when rm == RBP).
		rmReg := rm
		if rex&rexB != 0 {
			rmReg |= 0x8
		}
		ea = c.reg64[rmReg]
		if rm == 5 {
			r.defaultSeg = SS
		}
	}

	if mod == 1 {
		ea += uint64(int64(int8(c.fetch8())))
	} else if mod == 2 {
		ea += uint64(int64(int32(c.fetch32())))
	}

	r.ea = ea
	if rex&rexB != 0 {
		rm |= 0x8
	}
	r.rm = rm
	return r
}

// shiftEAForImm adds extraImmBytes to the RIP-relative effective
// address when the caller could not pass the immediate size up-front
// (Group 3 is the only such case: the imm size depends on which
// sub-op the ModR/M.reg field selects). Returns m unchanged for
// non-RIP-relative operands. Calling this on a non-RIP-relative
// operand is a no-op; calling it more than once on a RIP-relative
// operand is a bug — it would double-adjust.
func (m *modRMResult) shiftEAForImm(extraImmBytes uint8) {
	if m.ripRelative {
		m.ea += uint64(extraImmBytes)
	}
}
