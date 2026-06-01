package x86_64

// String-operation primitives (MOVS / STOS / LODS / SCAS / CMPS) and
// the REP / REPE / REPNE wrappers.
//
// Per Intel SDM Vol 2 (operand encoding for string instructions):
//   - the source index reads from the segment selected by the active
//     segment-override prefix, defaulting to DS for LODS/MOVS/CMPS,
//   - the destination index always writes through ES; the destination
//     segment is NOT overridable per the architecture (the segment-
//     override prefix only affects the *source* of MOVS / CMPS),
//   - the index width follows the current address size (16-bit in
//     real / pm16, 32-bit in pm32, 64-bit in long mode), and
//   - the index register reads back through the segment base — in real
//     and pm modes a non-zero base must be added.
//
// The previous implementation used the raw RSI/RDI value as the linear
// address, which only happens to be correct in long mode where every
// segment is architecturally forced to base zero. In real mode SeaBIOS
// runs `rep movsb` to copy ACPI / e820 entries from CS:0x5660 — with
// CS=0xF000 (base 0xF0000) the source is 0xF5660; reading from 0x5660
// gave us conventional-RAM zeros instead, which manifested as MBR-issued
// INT 15h E820 returning all-zero entries to its caller and Pure64
// then underflowing its mem_amount counter.

// stringSrcBase returns the linear-address base for the *source* index
// (RSI). DS is the default; a CS/SS/ES/FS/GS segment-override prefix
// substitutes one of those instead.
func (c *CPU) stringSrcBase() uint64 {
	if c.currentSegOverride >= 0 {
		return c.segBase[c.currentSegOverride]
	}
	return c.segBase[DS]
}

// stringDstBase returns the linear-address base for the *destination*
// index (RDI). Always ES; the segment-override prefix has no effect on
// the destination for MOVS / STOS / SCAS / CMPS (per SDM Vol 2 §3.7.5).
func (c *CPU) stringDstBase() uint64 {
	return c.segBase[ES]
}

// indexMask returns the bit-mask the address size truncates the index
// register to: 0xFFFF in 16-bit address mode, 0xFFFFFFFF in 32-bit,
// all-ones in 64-bit. This is what lets a 16-bit address-size MOVS
// wrap at the segment boundary instead of running off into adjacent
// memory.
func (c *CPU) indexMask() uint64 {
	switch c.currentAddressSize {
	case 2:
		return 0xFFFF
	case 4:
		return 0xFFFFFFFF
	default:
		return ^uint64(0)
	}
}

func (c *CPU) stringDelta(size uint8) int64 {
	d := int64(size)
	if c.rflags&RFLAGS_DF != 0 {
		return -d
	}
	return d
}

// advanceIndex applies the address-size-mask to the new index value so
// that 16-bit addressing wraps SI/DI inside their 64-KiB segment, and
// 32-bit addressing wraps ESI/EDI inside 4 GiB. The high bits of RSI/
// RDI are preserved (a 32-bit form zero-extends; 16-bit leaves the high
// 48 bits alone per real silicon).
func (c *CPU) advanceIndex(reg int, oldIdx uint64, delta int64) {
	mask := c.indexMask()
	newLow := uint64(int64(oldIdx&mask)+delta) & mask
	c.SetReg64(reg, (oldIdx &^ mask) | newLow)
}

// indexValue returns the *offset* portion of the index register the
// string op should use this iteration, after the address-size mask.
func (c *CPU) indexValue(reg int) uint64 {
	return c.GetReg64(reg) & c.indexMask()
}

// opStringMOVS — MOVSB/MOVSW/MOVSD/MOVSQ. Copies [DS:SI] → [ES:DI] and
// advances both. With REP (repPrefix=1) it loops while RCX != 0,
// decrementing RCX per iteration. The per-iteration decrement is load-
// bearing for #PF resumption: if a write/read inside the loop faults,
// Step's defer rewinds RIP, the kernel handles the PF, IRETQ resumes
// at the same REP instruction — and the architectural RCX must reflect
// the count of bytes actually committed, not the original count.
func (c *CPU) opStringMOVS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	srcBase := c.stringSrcBase()
	dstBase := c.stringDstBase()
	step := func() {
		srcIdx := c.indexValue(RSI)
		dstIdx := c.indexValue(RDI)
		c.stringCopyOne(srcBase+srcIdx, dstBase+dstIdx, size)
		c.advanceIndex(RSI, c.GetReg64(RSI), delta)
		c.advanceIndex(RDI, c.GetReg64(RDI), delta)
	}
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX)&c.indexMask() != 0 {
		step()
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
// bytes of RAX to [ES:DI] and advances RDI. ES is non-overridable.
func (c *CPU) opStringSTOS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	val := c.readReg(RAX, size)
	dstBase := c.stringDstBase()
	step := func() {
		dstIdx := c.indexValue(RDI)
		c.stringStoreOne(dstBase+dstIdx, val, size)
		c.advanceIndex(RDI, c.GetReg64(RDI), delta)
	}
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX)&c.indexMask() != 0 {
		step()
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

// opStringLODS — load [DS:SI] into RAX and advance. DS is the default
// source segment, overridable.
func (c *CPU) opStringLODS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	srcBase := c.stringSrcBase()
	step := func() {
		srcIdx := c.indexValue(RSI)
		var v uint64
		switch size {
		case 8:
			v = c.readMem64(srcBase + srcIdx)
		case 4:
			v = uint64(c.readMem32(srcBase + srcIdx))
		case 2:
			v = uint64(c.readMem16(srcBase + srcIdx))
		default:
			v = uint64(c.readMem8(srcBase + srcIdx))
		}
		c.writeReg(RAX, v, size)
		c.advanceIndex(RSI, c.GetReg64(RSI), delta)
	}
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX)&c.indexMask() != 0 {
		step()
		c.SetReg64(RCX, c.GetReg64(RCX)-1)
	}
	return nil
}

// opStringCMPS — CMPSB/CMPSW/CMPSD/CMPSQ. Compares [DS:SI] with [ES:DI],
// setting the arithmetic flags from ([DS:SI] - [ES:DI]), and advances
// both index registers. DS is the default source (overridable), ES is
// the destination. With REPE (repPrefix=1) continues while ZF=1 and
// RCX!=0; with REPNE (=2) continues while ZF=0. MenuetOS's boot sector
// uses `repe cmpsb` (CX=11) to match 8.3 FAT names.
func (c *CPU) opStringCMPS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	srcBase := c.stringSrcBase()
	dstBase := c.stringDstBase()
	readAt := func(addr uint64) uint64 {
		switch size {
		case 8:
			return c.readMem64(addr)
		case 4:
			return uint64(c.readMem32(addr))
		case 2:
			return uint64(c.readMem16(addr))
		default:
			return uint64(c.readMem8(addr))
		}
	}
	step := func() bool {
		srcIdx := c.indexValue(RSI)
		dstIdx := c.indexValue(RDI)
		a := readAt(srcBase + srcIdx)
		b := readAt(dstBase + dstIdx)
		_, fl := sub(a, b, size)
		c.setArithFlags(fl)
		c.advanceIndex(RSI, c.GetReg64(RSI), delta)
		c.advanceIndex(RDI, c.GetReg64(RDI), delta)
		return fl.zf
	}
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX)&c.indexMask() != 0 {
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

// opStringSCAS — compare [ES:DI] vs RAX and advance. With REPE (=1)
// continues while ZF=1 and RCX!=0; with REPNE (=2) continues while
// ZF=0. ES is non-overridable for SCAS.
func (c *CPU) opStringSCAS(rex, size, repPrefix uint8) error {
	_ = rex
	delta := c.stringDelta(size)
	a := c.readReg(RAX, size)
	dstBase := c.stringDstBase()
	step := func() bool {
		dstIdx := c.indexValue(RDI)
		var b uint64
		switch size {
		case 8:
			b = c.readMem64(dstBase + dstIdx)
		case 4:
			b = uint64(c.readMem32(dstBase + dstIdx))
		case 2:
			b = uint64(c.readMem16(dstBase + dstIdx))
		default:
			b = uint64(c.readMem8(dstBase + dstIdx))
		}
		_, fl := sub(a, b, size)
		c.setArithFlags(fl)
		c.advanceIndex(RDI, c.GetReg64(RDI), delta)
		return fl.zf
	}
	if repPrefix == 0 {
		step()
		return nil
	}
	for c.GetReg64(RCX)&c.indexMask() != 0 {
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
