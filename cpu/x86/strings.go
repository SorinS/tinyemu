package x86

// movs implements MOVS (move string).
// size is 1, 2, or 4 for byte, word, dword.
func (c *CPU) movs(size uint32) {
	src := c.segBase[DS] + c.GetReg32(ESI)
	dst := c.segBase[ES] + c.GetReg32(EDI)

	switch size {
	case 1:
		c.writeMem8(dst, c.readMem8(src))
	case 2:
		c.writeMem16(dst, c.readMem16(src))
	case 4:
		c.writeMem32(dst, c.readMem32(src))
	}

	if c.eflags&EFLAGS_DF != 0 {
		c.SetReg32(ESI, c.GetReg32(ESI)-size)
		c.SetReg32(EDI, c.GetReg32(EDI)-size)
	} else {
		c.SetReg32(ESI, c.GetReg32(ESI)+size)
		c.SetReg32(EDI, c.GetReg32(EDI)+size)
	}
}

// stos implements STOS (store string).
func (c *CPU) stos(size uint32) {
	dst := c.segBase[ES] + c.GetReg32(EDI)

	switch size {
	case 1:
		c.writeMem8(dst, c.GetReg8(AL))
	case 2:
		c.writeMem16(dst, c.GetReg16(AX))
	case 4:
		c.writeMem32(dst, c.GetReg32(EAX))
	}

	if c.eflags&EFLAGS_DF != 0 {
		c.SetReg32(EDI, c.GetReg32(EDI)-size)
	} else {
		c.SetReg32(EDI, c.GetReg32(EDI)+size)
	}
}

// lods implements LODS (load string).
func (c *CPU) lods(size uint32) {
	src := c.segBase[DS] + c.GetReg32(ESI)

	switch size {
	case 1:
		c.SetReg8(AL, c.readMem8(src))
	case 2:
		c.SetReg16(AX, c.readMem16(src))
	case 4:
		c.SetReg32(EAX, c.readMem32(src))
	}

	if c.eflags&EFLAGS_DF != 0 {
		c.SetReg32(ESI, c.GetReg32(ESI)-size)
	} else {
		c.SetReg32(ESI, c.GetReg32(ESI)+size)
	}
}

// cmps implements CMPS (compare string).
func (c *CPU) cmps(size uint32) {
	src := c.segBase[DS] + c.GetReg32(ESI)
	dst := c.segBase[ES] + c.GetReg32(EDI)

	switch size {
	case 1:
		c.sub8(c.readMem8(src), c.readMem8(dst))
	case 2:
		c.sub16(c.readMem16(src), c.readMem16(dst))
	case 4:
		c.sub32(c.readMem32(src), c.readMem32(dst))
	}

	if c.eflags&EFLAGS_DF != 0 {
		c.SetReg32(ESI, c.GetReg32(ESI)-size)
		c.SetReg32(EDI, c.GetReg32(EDI)-size)
	} else {
		c.SetReg32(ESI, c.GetReg32(ESI)+size)
		c.SetReg32(EDI, c.GetReg32(EDI)+size)
	}
}

// scas implements SCAS (scan string).
func (c *CPU) scas(size uint32) {
	dst := c.segBase[ES] + c.GetReg32(EDI)

	switch size {
	case 1:
		c.sub8(c.GetReg8(AL), c.readMem8(dst))
	case 2:
		c.sub16(c.GetReg16(AX), c.readMem16(dst))
	case 4:
		c.sub32(c.GetReg32(EAX), c.readMem32(dst))
	}

	if c.eflags&EFLAGS_DF != 0 {
		c.SetReg32(EDI, c.GetReg32(EDI)-size)
	} else {
		c.SetReg32(EDI, c.GetReg32(EDI)+size)
	}
}
