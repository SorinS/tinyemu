package x86

// Shift and rotate primitives.
//
// Per Intel SDM (Vol 2A, SAL/SAR/SHL/SHR, ROL/ROR/RCL/RCR):
//   - The count operand is masked to 5 bits (low 5 bits, i.e. count & 0x1F)
//     BEFORE the operation, on all IA-32 processors after the 8086.
//   - If the masked count is zero, no flags are modified and the destination
//     is unchanged.
//   - For shifts of count=1, OF is well-defined (different per op); for
//     count>1 the SDM says OF is undefined — we set it to a stable value.
//   - For RCL/RCR, the count is masked to 5 bits and then reduced modulo
//     (operand_size + 1) — i.e. (count & 31) % 9 for 8-bit, etc.
//
// Prior impl masked count inconsistently (sometimes to 3/4/5 bits depending
// on operand size for rotates, never for shifts), so e.g. `SHL EAX, 32`
// produced 0 instead of leaving EAX unchanged. That matters for Linux's
// occasional `shl reg, cl` where CL came from arithmetic that wraps past 31.

// shl8 performs 8-bit shift left.
func (c *CPU) shl8(v uint8, count uint8) uint8 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	// Use a wider type to avoid Go's "shift count >= type width yields 0"
	// silently dropping the bits we want for CF.
	wide := uint32(v) << count
	r := uint8(wide)
	cf := uint32(0)
	if count <= 8 {
		cf = (uint32(v) >> (8 - count)) & 1
	}
	c.setCF(cf != 0)
	c.setOF(((r >> 7) & 1) != uint8(cf))
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	return r
}

// shl16 performs 16-bit shift left.
func (c *CPU) shl16(v uint16, count uint8) uint16 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	wide := uint32(v) << count
	r := uint16(wide)
	cf := uint32(0)
	if count <= 16 {
		cf = (uint32(v) >> (16 - count)) & 1
	}
	c.setCF(cf != 0)
	c.setOF(((r >> 15) & 1) != uint16(cf))
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// shl32 performs 32-bit shift left.
func (c *CPU) shl32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	wide := uint64(v) << count
	r := uint32(wide)
	cf := uint64(0)
	// count is 1..31, so (32-count) is 1..31 — safe.
	cf = (uint64(v) >> (32 - count)) & 1
	c.setCF(cf != 0)
	c.setOF(((r >> 31) & 1) != uint32(cf))
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// shr8 performs 8-bit logical shift right.
func (c *CPU) shr8(v uint8, count uint8) uint8 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	r := uint8(0)
	if count < 8 {
		r = v >> count
	}
	cf := uint32(0)
	if count <= 8 {
		cf = (uint32(v) >> (count - 1)) & 1
	}
	c.setCF(cf != 0)
	// OF: for SHR, defined for count=1 as MSB of original.
	c.setOF((v & 0x80) != 0)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	return r
}

// shr16 performs 16-bit logical shift right.
func (c *CPU) shr16(v uint16, count uint8) uint16 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	r := uint16(0)
	if count < 16 {
		r = v >> count
	}
	cf := uint32(0)
	if count <= 16 {
		cf = (uint32(v) >> (count - 1)) & 1
	}
	c.setCF(cf != 0)
	c.setOF((v & 0x8000) != 0)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// shr32 performs 32-bit logical shift right.
func (c *CPU) shr32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	r := v >> count // count is 1..31 — safe.
	cf := (v >> (count - 1)) & 1
	c.setCF(cf != 0)
	c.setOF((v & 0x80000000) != 0)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// sar8 performs 8-bit arithmetic shift right.
func (c *CPU) sar8(v uint8, count uint8) uint8 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	// For SAR, shifting by >= width fills with sign bit.
	effectiveCount := count
	if effectiveCount >= 8 {
		effectiveCount = 7
	}
	r := uint8(int8(v) >> effectiveCount)
	if count >= 8 {
		// All bits become sign — saturate.
		if v&0x80 != 0 {
			r = 0xFF
		} else {
			r = 0
		}
	}
	cf := uint32(0)
	if count <= 8 {
		cf = (uint32(v) >> (count - 1)) & 1
	} else if v&0x80 != 0 {
		cf = 1
	}
	c.setCF(cf != 0)
	c.setOF(false)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	return r
}

// sar16 performs 16-bit arithmetic shift right.
func (c *CPU) sar16(v uint16, count uint8) uint16 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	effectiveCount := count
	if effectiveCount >= 16 {
		effectiveCount = 15
	}
	r := uint16(int16(v) >> effectiveCount)
	if count >= 16 {
		if v&0x8000 != 0 {
			r = 0xFFFF
		} else {
			r = 0
		}
	}
	cf := uint32(0)
	if count <= 16 {
		cf = (uint32(v) >> (count - 1)) & 1
	} else if v&0x8000 != 0 {
		cf = 1
	}
	c.setCF(cf != 0)
	c.setOF(false)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// sar32 performs 32-bit arithmetic shift right.
func (c *CPU) sar32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	r := uint32(int32(v) >> count) // count is 1..31 — safe.
	cf := (v >> (count - 1)) & 1
	c.setCF(cf != 0)
	c.setOF(false)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// rol8 performs 8-bit rotate left. SDM: count masked to 5 bits; if masked
// count is 0, flags unchanged. Effective rotation is `count % 8` but flags
// are still set whenever the masked count is non-zero.
func (c *CPU) rol8(v uint8, count uint8) uint8 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	rot := count & 7
	r := v
	if rot != 0 {
		r = (v << rot) | (v >> (8 - rot))
	}
	c.setCF((r & 1) != 0)
	c.setOF(((r >> 7) & 1) != (r & 1))
	return r
}

// rol16 performs 16-bit rotate left.
func (c *CPU) rol16(v uint16, count uint8) uint16 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	rot := count & 15
	r := v
	if rot != 0 {
		r = (v << rot) | (v >> (16 - rot))
	}
	c.setCF((r & 1) != 0)
	c.setOF(((r >> 15) & 1) != (r & 1))
	return r
}

// rol32 performs 32-bit rotate left.
func (c *CPU) rol32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	r := (v << count) | (v >> (32 - count))
	c.setCF((r & 1) != 0)
	c.setOF(((r >> 31) & 1) != (r & 1))
	return r
}

// ror8 performs 8-bit rotate right.
func (c *CPU) ror8(v uint8, count uint8) uint8 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	rot := count & 7
	r := v
	if rot != 0 {
		r = (v >> rot) | (v << (8 - rot))
	}
	c.setCF((r & 0x80) != 0)
	c.setOF(((r >> 7) & 1) != ((r >> 6) & 1))
	return r
}

// ror16 performs 16-bit rotate right.
func (c *CPU) ror16(v uint16, count uint8) uint16 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	rot := count & 15
	r := v
	if rot != 0 {
		r = (v >> rot) | (v << (16 - rot))
	}
	c.setCF((r & 0x8000) != 0)
	c.setOF(((r >> 15) & 1) != ((r >> 14) & 1))
	return r
}

// ror32 performs 32-bit rotate right.
func (c *CPU) ror32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	r := (v >> count) | (v << (32 - count))
	c.setCF((r & 0x80000000) != 0)
	c.setOF(((r >> 31) & 1) != ((r >> 30) & 1))
	return r
}

// rcl8 performs 8-bit rotate left through carry. The effective count is
// `(count & 31) % 9` since the rotation cycles through 9 positions (8 data
// bits + CF).
func (c *CPU) rcl8(v uint8, count uint8) uint8 {
	count &= 0x1F
	rot := count % 9
	if rot == 0 {
		// Per SDM: if masked count is 0 → flags unchanged. If masked count
		// is non-zero but rot==0 (count=9,18,27), the SDM says CF/OF ARE
		// updated. CF receives the bit that would be the last shifted out
		// for a full 9-cycle rotate — which is the current CF (unchanged).
		if count != 0 {
			c.setOF(((v >> 7) & 1) != boolToUint8(c.getCF()))
		}
		return v
	}
	cf := uint8(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v << rot) | (cf << (rot - 1))
	if rot >= 2 {
		r |= v >> (9 - rot)
	}
	newCF := (v >> (8 - rot)) & 1
	c.setCF(newCF != 0)
	c.setOF(((r >> 7) & 1) != newCF)
	return r
}

// rcl16 performs 16-bit rotate left through carry.
func (c *CPU) rcl16(v uint16, count uint8) uint16 {
	count &= 0x1F
	rot := count % 17
	if rot == 0 {
		if count != 0 {
			c.setOF(((v >> 15) & 1) != uint16(boolToUint8(c.getCF())))
		}
		return v
	}
	cf := uint16(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v << rot) | (cf << (rot - 1))
	if rot >= 2 {
		r |= v >> (17 - rot)
	}
	newCF := (v >> (16 - rot)) & 1
	c.setCF(newCF != 0)
	c.setOF(((r >> 15) & 1) != newCF)
	return r
}

// rcl32 performs 32-bit rotate left through carry.
func (c *CPU) rcl32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	cf := uint32(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v << count) | (cf << (count - 1))
	if count >= 2 {
		r |= v >> (33 - count)
	}
	newCF := (v >> (32 - count)) & 1
	c.setCF(newCF != 0)
	c.setOF(((r >> 31) & 1) != newCF)
	return r
}

// rcr8 performs 8-bit rotate right through carry.
func (c *CPU) rcr8(v uint8, count uint8) uint8 {
	count &= 0x1F
	rot := count % 9
	if rot == 0 {
		if count != 0 {
			c.setOF(((v >> 7) & 1) != boolToUint8(c.getCF()))
		}
		return v
	}
	cf := uint8(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v >> rot) | (cf << (8 - rot))
	if rot >= 2 {
		r |= v << (9 - rot)
	}
	newCF := (v >> (rot - 1)) & 1
	c.setCF(newCF != 0)
	c.setOF(((v >> 7) & 1) != ((r >> 7) & 1))
	return r
}

// rcr16 performs 16-bit rotate right through carry.
func (c *CPU) rcr16(v uint16, count uint8) uint16 {
	count &= 0x1F
	rot := count % 17
	if rot == 0 {
		if count != 0 {
			c.setOF(((v >> 15) & 1) != uint16(boolToUint8(c.getCF())))
		}
		return v
	}
	cf := uint16(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v >> rot) | (cf << (16 - rot))
	if rot >= 2 {
		r |= v << (17 - rot)
	}
	newCF := (v >> (rot - 1)) & 1
	c.setCF(newCF != 0)
	c.setOF(((v >> 15) & 1) != ((r >> 15) & 1))
	return r
}

// rcr32 performs 32-bit rotate right through carry.
func (c *CPU) rcr32(v uint32, count uint8) uint32 {
	count &= 0x1F
	if count == 0 {
		return v
	}
	cf := uint32(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v >> count) | (cf << (32 - count))
	if count >= 2 {
		r |= v << (33 - count)
	}
	newCF := (v >> (count - 1)) & 1
	c.setCF(newCF != 0)
	c.setOF(((v >> 31) & 1) != ((r >> 31) & 1))
	return r
}

// boolToUint8 — small helper used by the RCL/RCR helpers above to avoid
// repeating the EFLAGS_CF test inline.
func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
