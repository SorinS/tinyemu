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
// advances both. With REP (repPrefix=1) it loops while RCX != 0,
// decrementing RCX per iteration and updating RSI/RDI before
// continuing. The per-iteration decrement is load-bearing for #PF
// resumption: if a write/read inside the loop faults, Step's defer
// rewinds RIP, the kernel handles the PF, IRETQ resumes at the same
// REP instruction — and the architectural RCX must reflect the count
// of bytes actually committed, not the original count. With a single
// post-loop SetReg64(RCX, 0) the rep would restart from the original
// count after each fault and effectively never terminate.
func (c *CPU) opStringMOVS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	if repPrefix == 0 {
		src := c.GetReg64(RSI)
		dst := c.GetReg64(RDI)
		c.stringCopyOne(src, dst, size)
		c.SetReg64(RSI, uint64(int64(src)+delta))
		c.SetReg64(RDI, uint64(int64(dst)+delta))
		return nil
	}
	for c.GetReg64(RCX) != 0 {
		src := c.GetReg64(RSI)
		dst := c.GetReg64(RDI)
		c.stringCopyOne(src, dst, size)
		c.SetReg64(RSI, uint64(int64(src)+delta))
		c.SetReg64(RDI, uint64(int64(dst)+delta))
		c.SetReg64(RCX, c.GetReg64(RCX)-1)
	}
	return nil
}

func (c *CPU) stringCopyOne(src, dst uint64, size uint8) {
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
}

// opStringSTOS — STOSB/STOSW/STOSD/STOSQ. Writes the low operandSize
// bytes of RAX to [RDI] and advances RDI. See opStringMOVS for why the
// per-iteration RCX decrement matters — page-faulting writes to lazily-
// mapped pages (e.g. musl mallocng's just-mmap'd group storage being
// memset'd by busybox's xzalloc) resume on the same instruction and
// must see RCX reflecting bytes already committed.
func (c *CPU) opStringSTOS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	val := c.readReg(RAX, size)
	if repPrefix == 0 {
		dst := c.GetReg64(RDI)
		c.stringStoreOne(dst, val, size)
		c.SetReg64(RDI, uint64(int64(dst)+delta))
		return nil
	}
	for c.GetReg64(RCX) != 0 {
		dst := c.GetReg64(RDI)
		c.stringStoreOne(dst, val, size)
		c.SetReg64(RDI, uint64(int64(dst)+delta))
		c.SetReg64(RCX, c.GetReg64(RCX)-1)
	}
	return nil
}

func (c *CPU) stringStoreOne(dst, val uint64, size uint8) {
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
}

// opStringLODS — load [RSI] into RAX and advance. Same per-iteration
// RCX semantics as the others.
func (c *CPU) opStringLODS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	step := func() {
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
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX) != 0 {
		step()
		c.SetReg64(RCX, c.GetReg64(RCX)-1)
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
	a := c.readReg(RAX, size)
	step := func() bool {
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
		return fl.zf
	}
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX) != 0 {
		zf := step()
		c.SetReg64(RCX, c.GetReg64(RCX)-1)
		if repPrefix == 1 && !zf {
			return nil
		}
		if repPrefix == 2 && zf {
			return nil
		}
	}
	return nil
}
