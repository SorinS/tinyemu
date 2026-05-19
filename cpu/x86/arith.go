package x86

// add16 performs 16-bit addition with EFLAGS update.
func (c *CPU) add16(a, b uint16) uint16 {
	res := uint32(a) + uint32(b)
	r := uint16(res)
	c.updateArithFlags16(r, a, b, true)
	return r
}

// add8 performs 8-bit addition with EFLAGS update.
func (c *CPU) add8(a, b uint8) uint8 {
	res := uint16(a) + uint16(b)
	r := uint8(res)
	c.updateArithFlags8(r, a, b, true)
	return r
}

// add32 performs 32-bit addition with EFLAGS update.
func (c *CPU) add32(a, b uint32) uint32 {
	res := uint64(a) + uint64(b)
	r := uint32(res)
	c.updateArithFlags32(r, a, b, true)
	return r
}

// sub16 performs 16-bit subtraction with EFLAGS update.
func (c *CPU) sub16(a, b uint16) uint16 {
	res := uint32(a) - uint32(b)
	r := uint16(res)
	c.updateArithFlags16(r, a, b, false)
	return r
}

// sub8 performs 8-bit subtraction with EFLAGS update.
func (c *CPU) sub8(a, b uint8) uint8 {
	res := uint16(a) - uint16(b)
	r := uint8(res)
	c.updateArithFlags8(r, a, b, false)
	return r
}

// sub32 performs 32-bit subtraction with EFLAGS update.
func (c *CPU) sub32(a, b uint32) uint32 {
	res := uint64(a) - uint64(b)
	r := uint32(res)
	c.updateArithFlags32(r, a, b, false)
	return r
}

// inc8 increments an 8-bit value with EFLAGS update (does not affect CF).
// inc8 increments an 8-bit value with EFLAGS update (does not affect CF).
// Signed overflow happens iff v transitions from the largest positive value
// (0x7F → 0x80, i.e. +127 → -128), so OF = (MSB was 0) && (MSB is now 1).
// Prior to this fix the condition was inverted (matching DEC), causing
// `INC EAX` of 0x7FFFFFFF to leave OF=0 and `INC EAX` of 0xFFFFFFFF to
// leave OF=1 — exactly backwards.
func (c *CPU) inc8(v uint8) uint8 {
	r := v + 1
	of := (v&0x80) == 0 && (r&0x80) != 0
	c.setOF(of)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setAF((v & 0x0F) == 0x0F)
	c.setPF(parity8(r))
	return r
}

// dec8 decrements an 8-bit value with EFLAGS update (does not affect CF).
// Signed overflow when v = 0x80 (-128) → r = 0x7F (+127): MSB 1→0.
func (c *CPU) dec8(v uint8) uint8 {
	r := v - 1
	of := (v&0x80) != 0 && (r&0x80) == 0
	c.setOF(of)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setAF((v & 0x0F) == 0)
	c.setPF(parity8(r))
	return r
}

// inc32 increments a 32-bit value with EFLAGS update (does not affect CF).
func (c *CPU) inc32(v uint32) uint32 {
	r := v + 1
	of := (v&0x80000000) == 0 && (r&0x80000000) != 0
	c.setOF(of)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setAF((v & 0x0F) == 0x0F)
	c.setPF(parity8(uint8(r)))
	return r
}

// dec32 decrements a 32-bit value with EFLAGS update (does not affect CF).
func (c *CPU) dec32(v uint32) uint32 {
	r := v - 1
	of := (v&0x80000000) != 0 && (r&0x80000000) == 0
	c.setOF(of)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setAF((v & 0x0F) == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// xor8 performs 8-bit XOR with EFLAGS update.
func (c *CPU) xor8(a, b uint8) uint8 {
	r := a ^ b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	c.setAF(false) // Undefined, but Bochs sets to false
	return r
}

// xor16 performs 16-bit XOR with EFLAGS update.
func (c *CPU) xor16(a, b uint16) uint16 {
	r := a ^ b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	c.setAF(false)
	return r
}

// xor32 performs 32-bit XOR with EFLAGS update.
func (c *CPU) xor32(a, b uint32) uint32 {
	r := a ^ b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	c.setAF(false)
	return r
}

// and8 performs 8-bit AND with EFLAGS update.
func (c *CPU) and8(a, b uint8) uint8 {
	r := a & b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	c.setAF(false)
	return r
}

// and16 performs 16-bit AND with EFLAGS update.
func (c *CPU) and16(a, b uint16) uint16 {
	r := a & b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	c.setAF(false)
	return r
}

// and32 performs 32-bit AND with EFLAGS update.
func (c *CPU) and32(a, b uint32) uint32 {
	r := a & b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	c.setAF(false)
	return r
}

// updateArithFlags16 updates OF, SF, ZF, AF, PF, CF for 16-bit operations.
func (c *CPU) updateArithFlags16(res, op1, op2 uint16, isAdd bool) {
	var cf, of bool
	if isAdd {
		cf = uint32(op1)+uint32(op2) > 0xFFFF
		of = ((op1^op2)&0x8000) == 0 && ((op1^res)&0x8000) != 0
	} else {
		cf = op1 < op2
		of = ((op1^op2)&0x8000) != 0 && ((op1^res)&0x8000) != 0
	}
	c.setCF(cf)
	c.setOF(of)
	c.setSF((res & 0x8000) != 0)
	c.setZF(res == 0)
	c.setAF(((op1 ^ op2 ^ res) & 0x10) != 0)
	c.setPF(parity8(uint8(res)))
}

// updateArithFlags8 updates OF, SF, ZF, AF, PF, CF for 8-bit operations.
func (c *CPU) updateArithFlags8(res, op1, op2 uint8, isAdd bool) {
	var cf, of bool
	if isAdd {
		cf = uint16(op1)+uint16(op2) > 0xFF
		of = ((op1^op2)&0x80) == 0 && ((op1^res)&0x80) != 0
	} else {
		cf = op1 < op2
		of = ((op1^op2)&0x80) != 0 && ((op1^res)&0x80) != 0
	}
	c.setCF(cf)
	c.setOF(of)
	c.setSF((res & 0x80) != 0)
	c.setZF(res == 0)
	c.setAF(((op1 ^ op2 ^ res) & 0x10) != 0)
	c.setPF(parity8(res))
}

// updateArithFlags32 updates OF, SF, ZF, AF, PF, CF for 32-bit operations.
func (c *CPU) updateArithFlags32(res, op1, op2 uint32, isAdd bool) {
	var cf, of bool
	if isAdd {
		cf = uint64(op1)+uint64(op2) > 0xFFFFFFFF
		of = ((op1^op2)&0x80000000) == 0 && ((op1^res)&0x80000000) != 0
	} else {
		cf = op1 < op2
		of = ((op1^op2)&0x80000000) != 0 && ((op1^res)&0x80000000) != 0
	}
	c.setCF(cf)
	c.setOF(of)
	c.setSF((res & 0x80000000) != 0)
	c.setZF(res == 0)
	c.setAF(((op1 ^ op2 ^ res) & 0x10) != 0)
	c.setPF(parity8(uint8(res)))
}

// setCF sets or clears the carry flag.
func (c *CPU) setCF(flag bool) {
	if flag {
		c.eflags |= EFLAGS_CF
	} else {
		c.eflags &^= EFLAGS_CF
	}
}

// setOF sets or clears the overflow flag.
func (c *CPU) setOF(flag bool) {
	if flag {
		c.eflags |= EFLAGS_OF
	} else {
		c.eflags &^= EFLAGS_OF
	}
}

// setSF sets or clears the sign flag.
func (c *CPU) setSF(flag bool) {
	if flag {
		c.eflags |= EFLAGS_SF
	} else {
		c.eflags &^= EFLAGS_SF
	}
}

// setZF sets or clears the zero flag.
func (c *CPU) setZF(flag bool) {
	if flag {
		c.eflags |= EFLAGS_ZF
	} else {
		c.eflags &^= EFLAGS_ZF
	}
}

// setAF sets or clears the auxiliary carry flag.
func (c *CPU) setAF(flag bool) {
	if flag {
		c.eflags |= EFLAGS_AF
	} else {
		c.eflags &^= EFLAGS_AF
	}
}

// setPF sets or clears the parity flag.
func (c *CPU) setPF(flag bool) {
	if flag {
		c.eflags |= EFLAGS_PF
	} else {
		c.eflags &^= EFLAGS_PF
	}
}

// adc8 performs 8-bit addition with carry.
//
// NOTE: We cannot simply call updateArithFlags8(r, a, b+cfIn, true) — when
// b+cfIn overflows (e.g. b=0xFF, cfIn=1), the second operand truncates to 0
// and the helper's `cf = op1+op2 > 0xFF` check returns wrong values.
// Instead we compute CF/OF/AF directly here using the wider-precision sum.
func (c *CPU) adc8(a, b uint8) uint8 {
	cfIn := uint16(0)
	if c.eflags&EFLAGS_CF != 0 {
		cfIn = 1
	}
	sum := uint16(a) + uint16(b) + cfIn
	r := uint8(sum)
	c.setCF(sum > 0xFF)
	c.setOF(((a^b)&0x80) == 0 && ((a^r)&0x80) != 0)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setAF(((a^b^r)&0x10) != 0)
	c.setPF(parity8(r))
	return r
}

// adc16 performs 16-bit addition with carry.
func (c *CPU) adc16(a, b uint16) uint16 {
	cfIn := uint32(0)
	if c.eflags&EFLAGS_CF != 0 {
		cfIn = 1
	}
	sum := uint32(a) + uint32(b) + cfIn
	r := uint16(sum)
	c.setCF(sum > 0xFFFF)
	c.setOF(((a^b)&0x8000) == 0 && ((a^r)&0x8000) != 0)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setAF(((a^b^r)&0x10) != 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// adc32 performs 32-bit addition with carry.
func (c *CPU) adc32(a, b uint32) uint32 {
	cfIn := uint64(0)
	if c.eflags&EFLAGS_CF != 0 {
		cfIn = 1
	}
	sum := uint64(a) + uint64(b) + cfIn
	r := uint32(sum)
	c.setCF(sum > 0xFFFFFFFF)
	c.setOF(((a^b)&0x80000000) == 0 && ((a^r)&0x80000000) != 0)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setAF(((a^b^r)&0x10) != 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// sbb8 performs 8-bit subtraction with borrow.
//
// CF_out is set when (a < b + cf_in) considered in infinite precision,
// equivalently when the 9-bit signed subtraction `a - b - cf_in` borrows
// out of bit 8. The earlier `b + cf_in` shortcut was wrong when b == 0xFF
// and cf_in == 1 (the sum truncated to 0, so the helper said CF=0 even
// though a real CPU sets CF=1 here).
func (c *CPU) sbb8(a, b uint8) uint8 {
	cfIn := uint16(0)
	if c.eflags&EFLAGS_CF != 0 {
		cfIn = 1
	}
	diff := uint16(a) - uint16(b) - cfIn
	r := uint8(diff)
	c.setCF(diff > 0xFF) // borrow bit (when uint16 underflows, high bits become set)
	c.setOF(((a^b)&0x80) != 0 && ((a^r)&0x80) != 0)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setAF(((a^b^r)&0x10) != 0)
	c.setPF(parity8(r))
	return r
}

// sbb16 performs 16-bit subtraction with borrow.
func (c *CPU) sbb16(a, b uint16) uint16 {
	cfIn := uint32(0)
	if c.eflags&EFLAGS_CF != 0 {
		cfIn = 1
	}
	diff := uint32(a) - uint32(b) - cfIn
	r := uint16(diff)
	c.setCF(diff > 0xFFFF)
	c.setOF(((a^b)&0x8000) != 0 && ((a^r)&0x8000) != 0)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setAF(((a^b^r)&0x10) != 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// sbb32 performs 32-bit subtraction with borrow.
func (c *CPU) sbb32(a, b uint32) uint32 {
	cfIn := uint64(0)
	if c.eflags&EFLAGS_CF != 0 {
		cfIn = 1
	}
	diff := uint64(a) - uint64(b) - cfIn
	r := uint32(diff)
	c.setCF(diff > 0xFFFFFFFF)
	c.setOF(((a^b)&0x80000000) != 0 && ((a^r)&0x80000000) != 0)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setAF(((a^b^r)&0x10) != 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// or8 performs 8-bit OR.
func (c *CPU) or8(a, b uint8) uint8 {
	r := a | b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	c.setAF(false)
	return r
}

// or16 performs 16-bit OR.
func (c *CPU) or16(a, b uint16) uint16 {
	r := a | b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	c.setAF(false)
	return r
}

// or32 performs 32-bit OR.
func (c *CPU) or32(a, b uint32) uint32 {
	r := a | b
	c.eflags &^= EFLAGS_OF | EFLAGS_CF
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	c.setAF(false)
	return r
}

// neg8 performs 8-bit negation.
func (c *CPU) neg8(v uint8) uint8 {
	r := -v
	c.updateArithFlags8(r, 0, v, false)
	c.setCF(v != 0)
	return r
}

// neg16 performs 16-bit negation.
func (c *CPU) neg16(v uint16) uint16 {
	r := -v
	c.updateArithFlags16(r, 0, v, false)
	c.setCF(v != 0)
	return r
}

// neg32 performs 32-bit negation.
func (c *CPU) neg32(v uint32) uint32 {
	r := -v
	c.updateArithFlags32(r, 0, v, false)
	c.setCF(v != 0)
	return r
}

// not8 performs 8-bit NOT (does not affect flags).
func (c *CPU) not8(v uint8) uint8 {
	return ^v
}

// not16 performs 16-bit NOT (does not affect flags).
func (c *CPU) not16(v uint16) uint16 {
	return ^v
}

// not32 performs 32-bit NOT (does not affect flags).
func (c *CPU) not32(v uint32) uint32 {
	return ^v
}

// inc16 performs 16-bit increment. INC must NOT modify CF — use the same
// flag pattern as inc8/inc32, computed inline to avoid clobbering CF via
// the generic ADD helper.
func (c *CPU) inc16(v uint16) uint16 {
	r := v + 1
	c.setOF((v&0x8000) == 0 && (r&0x8000) != 0)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setAF((v & 0x0F) == 0x0F)
	c.setPF(parity8(uint8(r)))
	return r
}

// dec16 performs 16-bit decrement. DEC must NOT modify CF.
func (c *CPU) dec16(v uint16) uint16 {
	r := v - 1
	c.setOF((v&0x8000) != 0 && (r&0x8000) == 0)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setAF((v & 0x0F) == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// div8 performs unsigned 8-bit division: AX / divisor.
// div8/16/32 and idiv8/16/32 — per Intel SDM, if the divisor is zero OR the
// quotient overflows the destination's width, the CPU raises #DE (divide-
// error fault). Prior to this fix we silently returned on either condition,
// leaving the destination registers unchanged and bypassing the kernel's
// fault handler.
func (c *CPU) div8(divisor uint8) {
	if divisor == 0 {
		panic(divideError{})
	}
	dividend := c.GetReg16(AX)
	q := dividend / uint16(divisor)
	r := dividend % uint16(divisor)
	if q > 0xFF {
		panic(divideError{})
	}
	c.SetReg8(AL, uint8(q))
	c.SetReg8(AH, uint8(r))
}

// div16 performs unsigned 16-bit division: DX:AX / divisor.
func (c *CPU) div16(divisor uint16) {
	if divisor == 0 {
		panic(divideError{})
	}
	dividend := (uint32(c.GetReg16(DX)) << 16) | uint32(c.GetReg16(AX))
	q := dividend / uint32(divisor)
	if q > 0xFFFF {
		panic(divideError{})
	}
	r := uint16(dividend % uint32(divisor))
	c.SetReg16(AX, uint16(q))
	c.SetReg16(DX, r)
}

// div32 performs unsigned 32-bit division: EDX:EAX / divisor.
func (c *CPU) div32(divisor uint32) {
	if divisor == 0 {
		panic(divideError{})
	}
	dividend := (uint64(c.GetReg32(EDX)) << 32) | uint64(c.GetReg32(EAX))
	q := dividend / uint64(divisor)
	if q > 0xFFFFFFFF {
		panic(divideError{})
	}
	r := uint32(dividend % uint64(divisor))
	c.SetReg32(EAX, uint32(q))
	c.SetReg32(EDX, r)
}

// idiv8 performs signed 8-bit division: AX / divisor.
func (c *CPU) idiv8(divisor int8) {
	if divisor == 0 {
		panic(divideError{})
	}
	dividend := int16(c.GetReg16(AX))
	q := dividend / int16(divisor)
	r := dividend % int16(divisor)
	if q < -128 || q > 127 {
		panic(divideError{})
	}
	c.SetReg8(AL, uint8(q))
	c.SetReg8(AH, uint8(r))
}

// idiv16 performs signed 16-bit division: DX:AX / divisor.
func (c *CPU) idiv16(divisor int16) {
	if divisor == 0 {
		panic(divideError{})
	}
	dividend := (int32(c.GetReg16(DX)) << 16) | int32(c.GetReg16(AX))
	q := dividend / int32(divisor)
	if q < -32768 || q > 32767 {
		panic(divideError{})
	}
	r := int16(dividend % int32(divisor))
	c.SetReg16(AX, uint16(q))
	c.SetReg16(DX, uint16(r))
}

// idiv32 performs signed 32-bit division: EDX:EAX / divisor.
func (c *CPU) idiv32(divisor int32) {
	if divisor == 0 {
		panic(divideError{})
	}
	dividend := (int64(c.GetReg32(EDX)) << 32) | int64(c.GetReg32(EAX))
	q := dividend / int64(divisor)
	if q < -(1<<31) || q > (1<<31)-1 {
		panic(divideError{})
	}
	r := int32(dividend % int64(divisor))
	c.SetReg32(EAX, uint32(q))
	c.SetReg32(EDX, uint32(r))
}

// imul8 performs signed 8-bit multiplication: AL * r/m8.
func (c *CPU) imul8(v int8) {
	res := int16(int8(c.GetReg8(AL))) * int16(v)
	c.SetReg16(AX, uint16(res))
	c.setOF(res != int16(int8(res)))
	c.setCF(res != int16(int8(res)))
}

// imul16 performs signed 16-bit multiplication: AX * r/m16.
func (c *CPU) imul16(v int16) {
	res := int32(int16(c.GetReg16(AX))) * int32(v)
	c.SetReg16(AX, uint16(res))
	c.SetReg16(DX, uint16(res>>16))
	c.setOF(res != int32(int16(res)))
	c.setCF(res != int32(int16(res)))
}

// imul32 performs signed 32-bit multiplication: EAX * r/m32.
func (c *CPU) imul32(v int32) {
	res := int64(int32(c.GetReg32(EAX))) * int64(v)
	c.SetReg32(EAX, uint32(res))
	c.SetReg32(EDX, uint32(res>>32))
	c.setOF(res != int64(int32(res)))
	c.setCF(res != int64(int32(res)))
}

// xchg16 exchanges two 16-bit registers.
func (c *CPU) xchg16(r1, r2 int) {
	tmp := c.GetReg16(r1)
	c.SetReg16(r1, c.GetReg16(r2))
	c.SetReg16(r2, tmp)
}

// xchg32 exchanges two 32-bit registers.
func (c *CPU) xchg32(r1, r2 int) {
	tmp := c.GetReg32(r1)
	c.SetReg32(r1, c.GetReg32(r2))
	c.SetReg32(r2, tmp)
}

// parity8 returns true if the number of set bits in v is even.
func parity8(v uint8) bool {
	v ^= v >> 4
	v ^= v >> 2
	v ^= v >> 1
	return (v & 1) == 0
}
