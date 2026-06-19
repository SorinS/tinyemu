package arm64

import "fmt"

// execLoadStore executes a single-register load/store across all addressing
// modes (unsigned offset, unscaled/pre/post imm9, register offset).
func (c *CPU) execLoadStore(w uint32) error {
	size := (w >> 30) & 3
	opc := (w >> 22) & 3
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	scale := int64(1) << size
	base := c.readX(rn, true, true) // base register is SP-capable

	var addr uint64
	wback := false
	var wbVal uint64

	switch {
	case (w>>24)&1 == 1: // unsigned offset (scaled)
		imm12 := int64((w >> 10) & 0xFFF)
		addr = base + uint64(imm12*scale)
	case (w>>21)&1 == 1 && (w>>10)&3 == 0b10: // register offset
		rm := (w >> 16) & 0x1F
		option := (w >> 13) & 7
		s := (w >> 12) & 1
		var sh uint32
		if s == 1 {
			sh = uint32(size)
		}
		addr = base + extendReg(c.readX(rm, true, false), option, sh)
	default: // imm9: unscaled / pre / post
		imm9 := signExtend(uint64((w>>12)&0x1FF), 9)
		switch (w >> 10) & 3 {
		case 0b00: // unscaled (STUR)
			addr = base + uint64(imm9)
		case 0b01: // post-index
			addr = base
			wback, wbVal = true, base+uint64(imm9)
		case 0b11: // pre-index
			addr = base + uint64(imm9)
			wback, wbVal = true, addr
		default:
			return fmt.Errorf("arm64: bad load/store form %08x", w)
		}
	}

	nbits := uint32(scale * 8)
	if opc == 0 { // store
		if err := c.writeMem(addr, c.readX(rt, size == 3, false), int(scale)); err != nil {
			return err
		}
	} else { // load
		loaded, err := c.readMem(addr, int(scale))
		if err != nil {
			return err
		}
		switch opc {
		case 1: // zero-extend
			c.writeX(rt, size == 3, false, loaded)
		case 2: // sign-extend to 64-bit Xt
			c.writeX(rt, true, false, uint64(signExtend(loaded, nbits)))
		case 3: // sign-extend to 32-bit Wt
			c.writeX(rt, false, false, uint64(signExtend(loaded, nbits))&0xFFFFFFFF)
		}
	}
	if wback {
		c.writeX(rn, true, true, wbVal)
	}
	return nil
}

// execLoadLiteral executes LDR (literal): load a register from PC + a signed
// 19-bit word offset. opc picks the width/sign; V selects the FP register file.
func (c *CPU) execLoadLiteral(w uint32) error {
	opc := (w >> 30) & 3
	v := (w >> 26) & 1
	rt := w & 0x1F
	off := signExtend(uint64((w>>5)&0x7FFFF), 19) << 2
	addr := c.PC + uint64(off)

	if v == 1 { // FP/SIMD literal: S (opc 00), D (01), Q (10)
		switch opc {
		case 0:
			val, err := c.readMem(addr, 4)
			if err != nil {
				return err
			}
			c.writeVS(rt, uint32(val))
		case 1:
			val, err := c.readMem(addr, 8)
			if err != nil {
				return err
			}
			c.writeVD(rt, val)
		case 2:
			lo, err := c.readMem(addr, 8)
			if err != nil {
				return err
			}
			hi, err := c.readMem(addr+8, 8)
			if err != nil {
				return err
			}
			c.Vreg[rt] = [2]uint64{lo, hi}
		}
		return nil
	}
	switch opc {
	case 0: // LDR Wt
		val, err := c.readMem(addr, 4)
		if err != nil {
			return err
		}
		c.writeX(rt, false, false, val)
	case 1: // LDR Xt
		val, err := c.readMem(addr, 8)
		if err != nil {
			return err
		}
		c.writeX(rt, true, false, val)
	case 2: // LDRSW Xt
		val, err := c.readMem(addr, 4)
		if err != nil {
			return err
		}
		c.writeX(rt, true, false, uint64(signExtend(val, 32)))
	case 3: // PRFM (literal prefetch): no-op
	}
	return nil
}

// execPair executes ldp/stp/ldpsw.
func (c *CPU) execPair(w uint32) error {
	opc := (w >> 30) & 3
	l := (w >> 22) & 1
	imm7 := signExtend(uint64((w>>15)&0x7F), 7)
	rt2 := (w >> 10) & 0x1F
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F

	var scale int64
	var is64, ldpsw bool
	switch opc {
	case 0:
		scale, is64 = 4, false
	case 1:
		scale, is64, ldpsw = 4, true, true
	case 2:
		scale, is64 = 8, true
	default:
		return fmt.Errorf("arm64: bad pair opc %08x", w)
	}

	base := c.readX(rn, true, true)
	off := imm7 * scale
	var addr uint64
	wback := false
	var wbVal uint64
	switch (w >> 23) & 3 {
	case 0b010: // signed offset
		addr = base + uint64(off)
	case 0b011: // pre-index
		addr = base + uint64(off)
		wback, wbVal = true, addr
	case 0b001: // post-index
		addr = base
		wback, wbVal = true, base+uint64(off)
	default:
		return fmt.Errorf("arm64: unsupported pair form %08x", w)
	}

	if l == 0 { // store
		if err := c.writeMem(addr, c.readX(rt, is64, false), int(scale)); err != nil {
			return err
		}
		if err := c.writeMem(addr+uint64(scale), c.readX(rt2, is64, false), int(scale)); err != nil {
			return err
		}
	} else { // load
		v1, err := c.readMem(addr, int(scale))
		if err != nil {
			return err
		}
		v2, err := c.readMem(addr+uint64(scale), int(scale))
		if err != nil {
			return err
		}
		if ldpsw {
			c.writeX(rt, true, false, uint64(signExtend(v1, 32)))
			c.writeX(rt2, true, false, uint64(signExtend(v2, 32)))
		} else {
			c.writeX(rt, is64, false, v1)
			c.writeX(rt2, is64, false, v2)
		}
	}
	if wback {
		c.writeX(rn, true, true, wbVal)
	}
	return nil
}
