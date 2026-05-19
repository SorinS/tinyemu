package x86_64

// String-operation primitives (MOVS / STOS / LODS / SCAS) and the
// REP / REPE / REPNE wrappers. Default address size is 64-bit in
// long mode, so the index registers used are RSI/RDI/RCX; a 0x67
// prefix shrinks them to ESI/EDI/ECX (not yet implemented). DF in
// RFLAGS picks increment (DF=0) vs decrement (DF=1). The element
// size comes from operandSize for the word/dword/qword variants;
// the byte variants always use 1.

func (c *CPU) stringDelta(size uint8) int64 {
	d := int64(size)
	if c.rflags&RFLAGS_DF != 0 {
		return -d
	}
	return d
}

// opStringMOVS — MOVSB/MOVSW/MOVSD/MOVSQ. Copies [RSI] → [RDI] and
// advances both. With REP (repPrefix=1) it loops RCX times and zeros
// RCX on exit.
func (c *CPU) opStringMOVS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	count := uint64(1)
	if repPrefix != 0 {
		count = c.GetReg64(RCX)
	}
	for i := uint64(0); i < count; i++ {
		src := c.GetReg64(RSI)
		dst := c.GetReg64(RDI)
		switch size {
		case 8:
			c.writeMem64(dst, c.readMem64(src))
		case 4:
			c.writeMem32(dst, c.readMem32(src))
		case 2:
			c.writeMem16(dst, c.readMem16(src))
		default:
			c.writeMem8(dst, c.readMem8(src))
		}
		c.SetReg64(RSI, uint64(int64(src)+delta))
		c.SetReg64(RDI, uint64(int64(dst)+delta))
	}
	if repPrefix != 0 {
		c.SetReg64(RCX, 0)
	}
	return nil
}

// opStringSTOS — STOSB/STOSW/STOSD/STOSQ. Writes the low operandSize
// bytes of RAX to [RDI] and advances RDI.
func (c *CPU) opStringSTOS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	count := uint64(1)
	if repPrefix != 0 {
		count = c.GetReg64(RCX)
	}
	val := c.readReg(RAX, size)
	for i := uint64(0); i < count; i++ {
		dst := c.GetReg64(RDI)
		switch size {
		case 8:
			c.writeMem64(dst, val)
		case 4:
			c.writeMem32(dst, uint32(val))
		case 2:
			c.writeMem16(dst, uint16(val))
		default:
			c.writeMem8(dst, uint8(val))
		}
		c.SetReg64(RDI, uint64(int64(dst)+delta))
	}
	if repPrefix != 0 {
		c.SetReg64(RCX, 0)
	}
	return nil
}

// opStringLODS — load [RSI] into RAX and advance.
func (c *CPU) opStringLODS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	count := uint64(1)
	if repPrefix != 0 {
		count = c.GetReg64(RCX)
	}
	for i := uint64(0); i < count; i++ {
		src := c.GetReg64(RSI)
		var v uint64
		switch size {
		case 8:
			v = c.readMem64(src)
		case 4:
			v = uint64(c.readMem32(src))
		case 2:
			v = uint64(c.readMem16(src))
		default:
			v = uint64(c.readMem8(src))
		}
		c.writeReg(RAX, v, size)
		c.SetReg64(RSI, uint64(int64(src)+delta))
	}
	if repPrefix != 0 {
		c.SetReg64(RCX, 0)
	}
	return nil
}

// opStringSCAS — compare [RDI] vs RAX and advance. With REPE (=1)
// continues while ZF=1; with REPNE (=2) continues while ZF=0. Each
// iteration sets the arithmetic flags so the early-exit test is
// observable.
func (c *CPU) opStringSCAS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	count := uint64(1)
	if repPrefix != 0 {
		count = c.GetReg64(RCX)
	}
	a := c.readReg(RAX, size)
	for i := uint64(0); i < count; i++ {
		dst := c.GetReg64(RDI)
		var b uint64
		switch size {
		case 8:
			b = c.readMem64(dst)
		case 4:
			b = uint64(c.readMem32(dst))
		case 2:
			b = uint64(c.readMem16(dst))
		default:
			b = uint64(c.readMem8(dst))
		}
		_, fl := sub(a, b, size)
		c.setArithFlags(fl)
		c.SetReg64(RDI, uint64(int64(dst)+delta))
		if repPrefix != 0 {
			c.SetReg64(RCX, c.GetReg64(RCX)-1)
			if repPrefix == 1 && !fl.zf {
				return nil
			}
			if repPrefix == 2 && fl.zf {
				return nil
			}
		}
	}
	return nil
}
