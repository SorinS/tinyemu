package x86

// shl8 performs 8-bit shift left.
func (c *CPU) shl8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	cf := (v >> (8 - count)) & 1
	r := v << count
	c.setCF(cf != 0)
	c.setOF(((r >> 7) & 1) != cf)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	// AF is undefined for shifts > 1
	return r
}

// shl16 performs 16-bit shift left.
func (c *CPU) shl16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	cf := (v >> (16 - count)) & 1
	r := v << count
	c.setCF(cf != 0)
	c.setOF(((r >> 15) & 1) != cf)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// shl32 performs 32-bit shift left.
func (c *CPU) shl32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	cf := (v >> (32 - count)) & 1
	r := v << count
	c.setCF(cf != 0)
	c.setOF(((r >> 31) & 1) != cf)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// shr8 performs 8-bit logical shift right.
func (c *CPU) shr8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	cf := (v >> (count - 1)) & 1
	r := v >> count
	c.setCF(cf != 0)
	c.setOF((v & 0x80) != 0)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	return r
}

// shr16 performs 16-bit logical shift right.
func (c *CPU) shr16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	cf := (v >> (count - 1)) & 1
	r := v >> count
	c.setCF(cf != 0)
	c.setOF((v & 0x8000) != 0)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// shr32 performs 32-bit logical shift right.
func (c *CPU) shr32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	cf := (v >> (count - 1)) & 1
	r := v >> count
	c.setCF(cf != 0)
	c.setOF((v & 0x80000000) != 0)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// sar8 performs 8-bit arithmetic shift right.
func (c *CPU) sar8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	cf := (v >> (count - 1)) & 1
	r := uint8(int8(v) >> count)
	c.setCF(cf != 0)
	c.setOF(false)
	c.setSF((r & 0x80) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(r))
	return r
}

// sar16 performs 16-bit arithmetic shift right.
func (c *CPU) sar16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	cf := (v >> (count - 1)) & 1
	r := uint16(int16(v) >> count)
	c.setCF(cf != 0)
	c.setOF(false)
	c.setSF((r & 0x8000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// sar32 performs 32-bit arithmetic shift right.
func (c *CPU) sar32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	cf := (v >> (count - 1)) & 1
	r := uint32(int32(v) >> count)
	c.setCF(cf != 0)
	c.setOF(false)
	c.setSF((r & 0x80000000) != 0)
	c.setZF(r == 0)
	c.setPF(parity8(uint8(r)))
	return r
}

// rol8 performs 8-bit rotate left.
func (c *CPU) rol8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	count &= 7
	r := (v << count) | (v >> (8 - count))
	c.setCF((r & 1) != 0)
	c.setOF(((r >> 7) & 1) != (r & 1))
	return r
}

// rol16 performs 16-bit rotate left.
func (c *CPU) rol16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	count &= 15
	r := (v << count) | (v >> (16 - count))
	c.setCF((r & 1) != 0)
	c.setOF(((r >> 15) & 1) != (r & 1))
	return r
}

// rol32 performs 32-bit rotate left.
func (c *CPU) rol32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	count &= 31
	r := (v << count) | (v >> (32 - count))
	c.setCF((r & 1) != 0)
	c.setOF(((r >> 31) & 1) != (r & 1))
	return r
}

// ror8 performs 8-bit rotate right.
func (c *CPU) ror8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	count &= 7
	r := (v >> count) | (v << (8 - count))
	c.setCF((r & 0x80) != 0)
	c.setOF(((r >> 7) & 1) != ((r >> 6) & 1))
	return r
}

// ror16 performs 16-bit rotate right.
func (c *CPU) ror16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	count &= 15
	r := (v >> count) | (v << (16 - count))
	c.setCF((r & 0x8000) != 0)
	c.setOF(((r >> 15) & 1) != ((r >> 14) & 1))
	return r
}

// ror32 performs 32-bit rotate right.
func (c *CPU) ror32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	count &= 31
	r := (v >> count) | (v << (32 - count))
	c.setCF((r & 0x80000000) != 0)
	c.setOF(((r >> 31) & 1) != ((r >> 30) & 1))
	return r
}

// rcl8 performs 8-bit rotate left through carry.
func (c *CPU) rcl8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	count &= 7
	cf := uint8(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v << count) | (cf << (count - 1)) | (v >> (9 - count))
	c.setCF((v>>(8-count))&1 != 0)
	c.setOF(((r >> 7) & 1) != ((r >> 6) & 1))
	return r
}

// rcl16 performs 16-bit rotate left through carry.
func (c *CPU) rcl16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	count &= 15
	cf := uint16(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v << count) | (cf << (count - 1)) | (v >> (17 - count))
	c.setCF((v>>(16-count))&1 != 0)
	c.setOF(((r >> 15) & 1) != ((r >> 14) & 1))
	return r
}

// rcl32 performs 32-bit rotate left through carry.
func (c *CPU) rcl32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	count &= 31
	cf := uint32(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v << count) | (cf << (count - 1)) | (v >> (33 - count))
	c.setCF((v>>(32-count))&1 != 0)
	c.setOF(((r >> 31) & 1) != ((r >> 30) & 1))
	return r
}

// rcr8 performs 8-bit rotate right through carry.
func (c *CPU) rcr8(v uint8, count uint8) uint8 {
	if count == 0 {
		return v
	}
	count &= 7
	cf := uint8(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v >> count) | (cf << (8 - count)) | (v << (9 - count))
	c.setCF((v>>(count-1))&1 != 0)
	c.setOF(((v >> 7) & 1) != (r&0x80)>>7)
	return r
}

// rcr16 performs 16-bit rotate right through carry.
func (c *CPU) rcr16(v uint16, count uint8) uint16 {
	if count == 0 {
		return v
	}
	count &= 15
	cf := uint16(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v >> count) | (cf << (16 - count)) | (v << (17 - count))
	c.setCF((v>>(count-1))&1 != 0)
	c.setOF(((v >> 15) & 1) != (r&0x8000)>>15)
	return r
}

// rcr32 performs 32-bit rotate right through carry.
func (c *CPU) rcr32(v uint32, count uint8) uint32 {
	if count == 0 {
		return v
	}
	count &= 31
	cf := uint32(0)
	if c.eflags&EFLAGS_CF != 0 {
		cf = 1
	}
	r := (v >> count) | (cf << (32 - count)) | (v << (33 - count))
	c.setCF((v>>(count-1))&1 != 0)
	c.setOF(((v >> 31) & 1) != (r&0x80000000)>>31)
	return r
}
