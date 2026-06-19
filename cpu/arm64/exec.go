package arm64

import (
	"fmt"
	"math/bits"
	"os"
)

// Step fetches, decodes, and executes one instruction.
func (c *CPU) Step() error {
	word, err := c.fetch()
	if err != nil {
		if ab, ok := err.(*abort); ok {
			return c.handleAbort(ab, false) // instruction abort
		}
		return err
	}
	next := c.PC + 4
	if err := c.exec(word, &next); err != nil {
		if ab, ok := err.(*abort); ok {
			return c.handleAbort(ab, true) // data abort
		}
		return err
	}
	c.PC = next
	return nil
}

// handleAbort delivers an MMU abort to EL1 when a vector table is installed,
// else surfaces it as a fault (the bare run-and-check behaviour). For an abort
// the return address (ELR) is the faulting instruction itself, so the handler
// can fix the mapping and eret to retry.
func (c *CPU) handleAbort(ab *abort, data bool) error {
	if c.VBAR == 0 {
		return ab
	}
	if faultDebug && ab.far < 0x10000 {
		w, _ := c.fetch()
		fmt.Fprintf(os.Stderr, "[arm64-fault] far=%#x pc=%#x insn=%08x kind=%s\n", ab.far, c.PC, w, ab.kind)
		for j := 0; j < 31; j++ {
			fmt.Fprintf(os.Stderr, " x%-2d=%016x", j, c.X[j])
			if j%4 == 3 {
				fmt.Fprintln(os.Stderr)
			}
		}
		fmt.Fprintf(os.Stderr, "\n sp=%016x\n", c.SP)
	}
	c.takeException(excSync, esrAbort(ab, data, c.EL), ab.far, c.PC, true)
	return nil
}

// exec decodes and executes one instruction word. next points at the default
// fall-through address (PC+4); branches overwrite it.
func (c *CPU) exec(w uint32, next *uint64) error {
	switch {
	case (w>>23)&0x3F == 0x22:
		return c.execAddSubImm(w)
	case (w>>23)&0x3F == 0x24:
		return c.execLogicalImm(w)
	case (w>>23)&0x3F == 0x25:
		return c.execMoveWide(w)
	case (w>>23)&0x3F == 0x26:
		return c.execBitfield(w)
	case (w>>23)&0x3F == 0x27:
		return c.execExtr(w)
	case (w>>24)&0x1F == 0x10:
		return c.execAddr(w)
	case (w>>21)&0xFF == 0xD0:
		return c.execAddSubCarry(w)
	case (w>>21)&0xFF == 0xD2:
		return c.execCondCmp(w)
	case (w>>21)&0xFF == 0xD4:
		return c.execCondSel(w)
	case (w>>24)&0x7F == 0x1B:
		return c.execMul(w)
	case (w>>21)&0x3FF == 0x0D6:
		return c.execDataProc2(w)
	case (w>>21)&0x3FF == 0x2D6:
		return c.execDataProc1(w)
	case (w>>24)&0x1F == 0x0B:
		return c.execAddSubReg(w)
	case (w>>24)&0x1F == 0x0A:
		return c.execLogicalReg(w)
	case (w>>24)&0x3F == 0x08: // load/store exclusive + ldar/stlr
		return c.execLoadStoreExclusive(w)
	case (w>>24)&0x3F == 0x18 || (w>>24)&0x3F == 0x1C: // LDR (literal), PC-relative
		return c.execLoadLiteral(w)
	case (w>>24)&0x3F == 0x38 || (w>>24)&0x3F == 0x39:
		return c.execLoadStore(w)
	case (w>>24)&0x3F == 0x3C || (w>>24)&0x3F == 0x3D: // FP load/store (V bit set)
		return c.execFPLoadStore(w)
	case (w>>24)&0x5F == 0x1E: // scalar FP data-processing (excludes Adv-SIMD-scalar 0x5E/0x7E)
		return c.execFPDataProc(w)
	case (w>>24)&0x9F == 0x0E: // Advanced SIMD (vector)
		return c.execSIMD(w)
	case (w>>24)&0xBF == 0x0C: // Advanced SIMD load/store multiple structures
		return c.execSIMDLdSt1(w)
	case (w>>24)&0x9F == 0x0F: // Advanced SIMD: by-element (bit10=0) or shift/movi (bit10=1)
		if (w>>10)&1 == 0 {
			return c.execSIMDByElem(w)
		}
		return c.execSIMDShiftImm(w)
	case (w>>25)&0x1F == 0x14:
		return c.execPair(w)
	case (w>>26)&0x1F == 0x05:
		return c.execBranchImm(w, next)
	case (w>>24)&0xFF == 0x54:
		return c.execBranchCond(w, next)
	case (w>>25)&0x3F == 0x1A:
		return c.execCompareBranch(w, next)
	case (w>>25)&0x3F == 0x1B:
		return c.execTestBranch(w, next)
	case (w>>24)&0xFF == 0xD4:
		return c.execException(w, next)
	case (w>>24)&0xFF == 0xD5:
		return c.execSystem(w)
	case (w>>25)&0x7F == 0x6B:
		return c.execBranchReg(w, next)
	}
	return fmt.Errorf("arm64: unimplemented instruction %08x at %#x", w, c.PC)
}

func is64bit(w uint32) bool { return (w>>31)&1 == 1 }

func (c *CPU) execAddSubImm(w uint32) error {
	sf := is64bit(w)
	op := (w >> 30) & 1
	s := (w >> 29) & 1
	sh := (w >> 22) & 1
	imm := uint64((w >> 10) & 0xFFF)
	if sh == 1 {
		imm <<= 12
	}
	rn, rd := (w>>5)&0x1F, w&0x1F
	a := c.readX(rn, sf, true) // Rn may be SP
	b := imm
	carry := uint64(0)
	if op == 1 { // sub
		b = ^b
		carry = 1
	}
	res, n, z, cf, v := addWithCarry(a, b, carry, sf)
	c.writeX(rd, sf, s == 0, res) // Rd is SP for add/sub, ZR for adds/subs
	if s == 1 {
		c.setFlags(n, z, cf, v)
	}
	return nil
}

func (c *CPU) execAddSubReg(w uint32) error {
	sf := is64bit(w)
	op := (w >> 30) & 1
	s := (w >> 29) & 1
	rm := (w >> 16) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	width := widthOf(sf)
	var a, b uint64
	rdSP := false
	if (w>>21)&1 == 1 { // extended register (SP-capable)
		option := (w >> 13) & 7
		imm3 := (w >> 10) & 7
		a = c.readX(rn, sf, true)
		b = extendReg(c.readX(rm, true, false), option, imm3)
		if !sf {
			b &= 0xFFFFFFFF
		}
		rdSP = s == 0
	} else { // shifted register
		shift := (w >> 22) & 3
		imm6 := (w >> 10) & 0x3F
		a = c.readX(rn, sf, false)
		b = shiftReg(c.readX(rm, sf, false), shift, imm6, width)
	}
	carry := uint64(0)
	if op == 1 {
		b = ^b
		carry = 1
	}
	res, n, z, cf, v := addWithCarry(a, b, carry, sf)
	if s == 1 {
		rdSP = false // adds/subs write ZR
		c.setFlags(n, z, cf, v)
	}
	c.writeX(rd, sf, rdSP, res)
	return nil
}

func (c *CPU) execLogicalImm(w uint32) error {
	sf := is64bit(w)
	opc := (w >> 29) & 3
	nbit := (w >> 22) & 1
	immr := (w >> 16) & 0x3F
	imms := (w >> 10) & 0x3F
	rn, rd := (w>>5)&0x1F, w&0x1F
	regSize := int(widthOf(sf))
	imm, ok := decodeBitmask(nbit, imms, immr, regSize)
	if !ok {
		return fmt.Errorf("arm64: bad logical immediate %08x", w)
	}
	a := c.readX(rn, sf, false)
	res := c.logicalOp(opc, a, imm, sf)
	c.writeX(rd, sf, opc != 3, res) // and/orr/eor may target SP; ands targets ZR
	return nil
}

func (c *CPU) execLogicalReg(w uint32) error {
	sf := is64bit(w)
	opc := (w >> 29) & 3
	nbit := (w >> 21) & 1
	shift := (w >> 22) & 3
	rm := (w >> 16) & 0x1F
	imm6 := (w >> 10) & 0x3F
	rn, rd := (w>>5)&0x1F, w&0x1F
	b := shiftReg(c.readX(rm, sf, false), shift, imm6, widthOf(sf))
	if nbit == 1 {
		b = ^b // bic/orn/eon/bics
	}
	a := c.readX(rn, sf, false)
	res := c.logicalOp(opc, a, b, sf)
	c.writeX(rd, sf, false, res)
	return nil
}

// logicalOp applies and/orr/eor/ands; ands (opc 3) also sets N/Z (C=V=0).
func (c *CPU) logicalOp(opc uint32, a, b uint64, sf bool) uint64 {
	var res uint64
	switch opc {
	case 0: // and / bic
		res = a & b
	case 1: // orr / orn
		res = a | b
	case 2: // eor / eon
		res = a ^ b
	case 3: // ands / bics
		res = a & b
	}
	if !sf {
		res &= 0xFFFFFFFF
	}
	if opc == 3 {
		n := res>>(widthOf(sf)-1)&1 != 0
		c.setFlags(n, res == 0, false, false)
	}
	return res
}

func (c *CPU) execMoveWide(w uint32) error {
	sf := is64bit(w)
	opc := (w >> 29) & 3
	hw := (w >> 21) & 3
	imm16 := uint64((w >> 5) & 0xFFFF)
	rd := w & 0x1F
	shift := hw * 16
	switch opc {
	case 0: // movn
		c.writeX(rd, sf, false, ^(imm16 << shift))
	case 2: // movz
		c.writeX(rd, sf, false, imm16<<shift)
	case 3: // movk — keep the other 16-bit fields
		old := c.readX(rd, sf, false)
		old &^= 0xFFFF << shift
		c.writeX(rd, sf, false, old|imm16<<shift)
	default:
		return fmt.Errorf("arm64: bad move-wide %08x", w)
	}
	return nil
}

func (c *CPU) execBitfield(w uint32) error {
	sf := is64bit(w)
	opc := (w >> 29) & 3
	immr := (w >> 16) & 0x3F
	imms := (w >> 10) & 0x3F
	rn, rd := (w>>5)&0x1F, w&0x1F
	width := widthOf(sf)
	src := c.readX(rn, sf, false)
	wmask, tmask := bfmMasks(width, imms, immr)
	rotated := rorWidth(src, immr&(width-1), width)
	var bot, top uint64
	switch opc {
	case 0: // sbfm
		bot = rotated & wmask
		if (src>>imms)&1 == 1 {
			top = ^uint64(0)
		}
	case 1: // bfm
		dst := c.readX(rd, sf, false)
		bot = (dst &^ wmask) | (rotated & wmask)
		top = dst
	case 2: // ubfm
		bot = rotated & wmask
	default:
		return fmt.Errorf("arm64: bad bitfield %08x", w)
	}
	res := (top &^ tmask) | (bot & tmask)
	c.writeX(rd, sf, false, res)
	return nil
}

func (c *CPU) execExtr(w uint32) error {
	sf := is64bit(w)
	rm := (w >> 16) & 0x1F
	lsb := (w >> 10) & 0x3F
	rn, rd := (w>>5)&0x1F, w&0x1F
	width := widthOf(sf)
	hi := c.readX(rn, sf, false)
	lo := c.readX(rm, sf, false)
	var res uint64
	if lsb == 0 {
		res = lo
	} else {
		res = (lo >> lsb) | (hi << (width - lsb))
	}
	c.writeX(rd, sf, false, res)
	return nil
}

func (c *CPU) execMul(w uint32) error {
	sf := is64bit(w)
	op31 := (w >> 21) & 7
	o0 := (w >> 15) & 1
	rm := (w >> 16) & 0x1F
	ra := (w >> 10) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	switch op31<<1 | o0 {
	case 0b0000, 0b0001: // madd / msub
		n := c.readX(rn, sf, false)
		m := c.readX(rm, sf, false)
		a := c.readX(ra, sf, false)
		prod := n * m
		if o0 == 0 {
			c.writeX(rd, sf, false, a+prod)
		} else {
			c.writeX(rd, sf, false, a-prod)
		}
	case 0b0010, 0b0011: // smaddl / smsubl
		n := int64(int32(c.readX(rn, false, false)))
		m := int64(int32(c.readX(rm, false, false)))
		a := int64(c.readX(ra, true, false))
		prod := n * m
		if o0 == 0 {
			c.writeX(rd, true, false, uint64(a+prod))
		} else {
			c.writeX(rd, true, false, uint64(a-prod))
		}
	case 0b1010, 0b1011: // umaddl / umsubl
		n := uint64(uint32(c.readX(rn, false, false)))
		m := uint64(uint32(c.readX(rm, false, false)))
		a := c.readX(ra, true, false)
		prod := n * m
		if o0 == 0 {
			c.writeX(rd, true, false, a+prod)
		} else {
			c.writeX(rd, true, false, a-prod)
		}
	case 0b0100: // smulh
		hi, _ := bits.Mul64(c.readX(rn, true, false), c.readX(rm, true, false))
		// signed high: adjust the unsigned high product for the sign terms.
		a := c.readX(rn, true, false)
		b := c.readX(rm, true, false)
		shi := int64(hi)
		if a>>63 != 0 {
			shi -= int64(b)
		}
		if b>>63 != 0 {
			shi -= int64(a)
		}
		c.writeX(rd, true, false, uint64(shi))
	case 0b1100: // umulh
		hi, _ := bits.Mul64(c.readX(rn, true, false), c.readX(rm, true, false))
		c.writeX(rd, true, false, hi)
	default:
		return fmt.Errorf("arm64: bad multiply %08x", w)
	}
	return nil
}

func (c *CPU) execDataProc2(w uint32) error {
	sf := is64bit(w)
	rm := (w >> 16) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	width := widthOf(sf)
	a := c.readX(rn, sf, false)
	b := c.readX(rm, sf, false)
	var res uint64
	switch (w >> 10) & 0x3F {
	case 0x02: // udiv
		if b == 0 {
			res = 0
		} else {
			res = a / b
		}
	case 0x03: // sdiv
		res = sdiv(a, b, sf)
	case 0x08: // lslv
		res = shiftReg(a, 0, uint32(b)%width, width)
	case 0x09: // lsrv
		res = shiftReg(a, 1, uint32(b)%width, width)
	case 0x0A: // asrv
		res = shiftReg(a, 2, uint32(b)%width, width)
	case 0x0B: // rorv
		res = shiftReg(a, 3, uint32(b)%width, width)
	default:
		return fmt.Errorf("arm64: bad 2-source op %08x", w)
	}
	c.writeX(rd, sf, false, res)
	return nil
}

func sdiv(a, b uint64, sf bool) uint64 {
	if b == 0 {
		return 0
	}
	if sf {
		x, y := int64(a), int64(b)
		if x == -1<<63 && y == -1 {
			return uint64(x) // INT_MIN / -1 overflows → INT_MIN
		}
		return uint64(x / y)
	}
	x, y := int32(a), int32(b)
	if x == -1<<31 && y == -1 {
		return uint64(uint32(x))
	}
	return uint64(uint32(x / y))
}

func (c *CPU) execDataProc1(w uint32) error {
	sf := is64bit(w)
	rn, rd := (w>>5)&0x1F, w&0x1F
	width := widthOf(sf)
	a := c.readX(rn, sf, false)
	var res uint64
	switch (w >> 10) & 0x3F {
	case 0x00: // rbit
		res = bits.Reverse64(a)
		if !sf {
			res >>= 32
		}
	case 0x01: // rev16
		res = rev16(a, width)
	case 0x02: // rev32 (64-bit) or rev (32-bit)
		if sf {
			res = rev32in64(a)
		} else {
			res = uint64(bits.ReverseBytes32(uint32(a)))
		}
	case 0x03: // rev (64-bit)
		res = bits.ReverseBytes64(a)
	case 0x04: // clz
		if sf {
			res = uint64(bits.LeadingZeros64(a))
		} else {
			res = uint64(bits.LeadingZeros32(uint32(a)))
		}
	case 0x05: // cls
		res = uint64(countLeadingSign(a, width))
	default:
		return fmt.Errorf("arm64: bad 1-source op %08x", w)
	}
	c.writeX(rd, sf, false, res)
	return nil
}

func (c *CPU) execSystem(w uint32) error {
	switch {
	case w>>20 == 0xD53: // mrs Xt, sysreg
		c.writeX(w&0x1F, true, false, c.readSysreg(w&0x000FFFE0))
	case w>>20 == 0xD51: // msr sysreg, Xt
		c.writeSysreg(w&0x000FFFE0, c.readX(w&0x1F, true, false))
	case w&0xFFF8F01F == 0xD500401F: // msr (immediate) — PSTATE field
		op1, crm, op2 := (w>>16)&7, (w>>8)&0xF, (w>>5)&7
		switch op1<<3 | op2 {
		case 0<<3 | 5: // SPSel
			c.switchEL(c.EL, uint8(crm&1))
		case 3<<3 | 6: // DAIFSet — mask interrupts (D/A/I/F per crm bits)
			c.DAIF |= uint8(crm)
		case 3<<3 | 7: // DAIFClr — unmask
			c.DAIF &^= uint8(crm)
		}
		// Other PSTATE fields (PAN/UAO/DIT/SSBS/TCO) are not modelled: no-op.
	case w>>12 == 0xD5032:
		// hints. WFE/WFI park the core until an interrupt; the rest (nop/yield/
		// sev/sevl) are no-ops for this single-core, in-order model.
		hint := ((w>>8)&0xF)<<3 | ((w >> 5) & 7)
		if hint == 2 || hint == 3 { // WFE / WFI
			c.powerDown = true
		}
	case w>>12 == 0xD5033:
		// barriers (dmb/dsb/isb): no-ops for flat, in-order memory.
	case w>>19 == 0x1AA1: // SYS (tlbi/dc/ic/at)
		if (w>>12)&0xF == 8 { // CRn==8 → TLBI: conservative full flush
			c.flushTLB()
		}
		// dc/ic/at (CRn 7) are no-ops here (no caches modelled).
	default:
		return fmt.Errorf("arm64: unimplemented system instruction %08x at %#x", w, c.PC)
	}
	return nil
}

func (c *CPU) execException(w uint32, next *uint64) error {
	imm := uint16((w >> 5) & 0xFFFF)
	c.ExcImm = imm
	switch (((w >> 21) & 7) << 2) | (w & 3) {
	case 0b000_01:
		c.ExcType = "svc"
	case 0b000_10:
		c.ExcType = "hvc"
	case 0b000_11:
		c.ExcType = "smc"
	case 0b001_00:
		c.ExcType = "brk"
	case 0b010_00:
		c.ExcType = "hlt"
	default:
		return fmt.Errorf("arm64: bad exception %08x at %#x", w, c.PC)
	}
	// PSCI and other hypercalls: a board can service hvc/smc directly (read x0
	// function-id/args, write the result to x0) instead of vectoring to EL1.
	if (c.ExcType == "hvc" || c.ExcType == "smc") && c.HVCHandler != nil {
		if c.HVCHandler(c) {
			return nil // handled; *next is already PC+4
		}
	}
	if c.VBAR == 0 {
		// No vector table installed: surface as a clean halt so a bare caller
		// (the emu sandbox) can stop the run.
		c.Halted = true
		return nil
	}
	// Vector to EL1. svc returns to the next instruction; brk/hlt to the
	// trapping instruction itself.
	esr, elr := esrSVC(imm), *next
	switch c.ExcType {
	case "brk":
		esr, elr = esrBRK(imm), c.PC
	case "hlt":
		esr, elr = esrBRK(imm), c.PC
	}
	c.takeException(excSync, esr, 0, elr, false)
	*next = c.PC
	return nil
}

func (c *CPU) execAddSubCarry(w uint32) error {
	sf := is64bit(w)
	op := (w >> 30) & 1
	s := (w >> 29) & 1
	rm := (w >> 16) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	a := c.readX(rn, sf, false)
	b := c.readX(rm, sf, false)
	carryIn := uint64(0)
	if c.C {
		carryIn = 1
	}
	if op == 1 { // sbc: Rn + ~Rm + C
		b = ^b
	}
	res, n, z, cf, v := addWithCarry(a, b, carryIn, sf)
	c.writeX(rd, sf, false, res)
	if s == 1 {
		c.setFlags(n, z, cf, v)
	}
	return nil
}

func (c *CPU) execCondCmp(w uint32) error {
	sf := is64bit(w)
	op := (w >> 30) & 1 // 0 = ccmn, 1 = ccmp
	cond := (w >> 12) & 0xF
	rn := (w >> 5) & 0x1F
	nzcv := w & 0xF
	if c.condHolds(cond) {
		var b uint64
		if (w>>11)&1 == 1 { // immediate comparand
			b = uint64((w >> 16) & 0x1F)
		} else {
			b = c.readX((w>>16)&0x1F, sf, false)
		}
		carryIn := uint64(0)
		if op == 1 { // ccmp subtracts
			b = ^b
			carryIn = 1
		}
		_, n, z, cf, v := addWithCarry(c.readX(rn, sf, false), b, carryIn, sf)
		c.setFlags(n, z, cf, v)
	} else { // condition false: take NZCV from the immediate
		c.setFlags(nzcv>>3&1 == 1, nzcv>>2&1 == 1, nzcv>>1&1 == 1, nzcv&1 == 1)
	}
	return nil
}

func (c *CPU) execCondSel(w uint32) error {
	sf := is64bit(w)
	op := (w >> 30) & 1
	op2 := (w >> 10) & 1
	cond := (w >> 12) & 0xF
	rm := (w >> 16) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	n := c.readX(rn, sf, false)
	m := c.readX(rm, sf, false)
	var res uint64
	if c.condHolds(cond) {
		res = n
	} else {
		switch op<<1 | op2 {
		case 0b00: // csel
			res = m
		case 0b01: // csinc
			res = m + 1
		case 0b10: // csinv
			res = ^m
		case 0b11: // csneg
			res = -m
		}
	}
	c.writeX(rd, sf, false, res)
	return nil
}

func (c *CPU) execAddr(w uint32) error {
	immlo := (w >> 29) & 3
	immhi := (w >> 5) & 0x7FFFF
	rd := w & 0x1F
	imm := signExtend(uint64(immhi<<2|immlo), 21)
	if (w>>31)&1 == 1 { // adrp
		base := c.PC &^ 0xFFF
		c.writeX(rd, true, false, base+uint64(imm<<12))
	} else {
		c.writeX(rd, true, false, c.PC+uint64(imm))
	}
	return nil
}

func (c *CPU) execBranchImm(w uint32, next *uint64) error {
	off := signExtend(uint64(w&0x3FFFFFF), 26) << 2
	if (w>>31)&1 == 1 { // bl
		c.X[30] = c.PC + 4
	}
	*next = c.PC + uint64(off)
	return nil
}

func (c *CPU) execBranchCond(w uint32, next *uint64) error {
	if c.condHolds(w & 0xF) {
		off := signExtend(uint64((w>>5)&0x7FFFF), 19) << 2
		*next = c.PC + uint64(off)
	}
	return nil
}

// execTestBranch executes TBZ/TBNZ: branch if a single bit of Rt is zero (TBZ,
// op=0) or non-zero (TBNZ, op=1). The bit position is b5:b40 (0..63).
func (c *CPU) execTestBranch(w uint32, next *uint64) error {
	op := (w >> 24) & 1
	bitpos := (w>>31)&1<<5 | (w>>19)&0x1F
	rt := w & 0x1F
	v := c.readX(rt, true, false)
	bitSet := (v>>bitpos)&1 == 1
	if bitSet == (op == 1) { // TBNZ branches when set; TBZ when clear
		off := signExtend(uint64((w>>5)&0x3FFF), 14) << 2
		*next = c.PC + uint64(off)
	}
	return nil
}

func (c *CPU) execCompareBranch(w uint32, next *uint64) error {
	sf := is64bit(w)
	op := (w >> 24) & 1
	rt := w & 0x1F
	v := c.readX(rt, sf, false)
	taken := (v == 0) == (op == 0) // cbz when op==0
	if taken {
		off := signExtend(uint64((w>>5)&0x7FFFF), 19) << 2
		*next = c.PC + uint64(off)
	}
	return nil
}

func (c *CPU) execBranchReg(w uint32, next *uint64) error {
	rn := (w >> 5) & 0x1F
	switch (w >> 21) & 0xF {
	case 0: // br
		*next = c.readX(rn, true, false)
	case 1: // blr
		c.X[30] = c.PC + 4
		*next = c.readX(rn, true, false)
	case 2: // ret
		*next = c.readX(rn, true, false)
	case 4: // eret — restore PSTATE from SPSR_EL1, jump to ELR_EL1
		c.eret()
		*next = c.PC
	default:
		return fmt.Errorf("arm64: bad branch-register %08x", w)
	}
	return nil
}

// rev16 reverses bytes within each 16-bit lane.
func rev16(v uint64, width uint32) uint64 {
	var res uint64
	for i := uint32(0); i < width; i += 16 {
		h := uint16(v >> i)
		h = h>>8 | h<<8
		res |= uint64(h) << i
	}
	return res
}

// rev32in64 reverses bytes within each 32-bit lane of a 64-bit value.
func rev32in64(v uint64) uint64 {
	lo := uint64(bits.ReverseBytes32(uint32(v)))
	hi := uint64(bits.ReverseBytes32(uint32(v >> 32)))
	return lo | hi<<32
}

// countLeadingSign counts the number of bits after the sign bit that equal it.
func countLeadingSign(v uint64, width uint32) int {
	v &= onesMask(width)
	sign := (v >> (width - 1)) & 1
	count := 0
	for i := int(width) - 2; i >= 0; i-- {
		if (v>>uint(i))&1 == sign {
			count++
		} else {
			break
		}
	}
	return count
}
