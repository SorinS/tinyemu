package x86

// MMX integer packed-arithmetic helpers. Each operates on a 64-bit
// MMX register treated as N elements of size `elemSize` bytes:
//   1 = byte    (8 elems)
//   2 = word    (4 elems)
//   4 = dword   (2 elems)
//   8 = qword   (1 elem)
//
// The mathematical operation is performed per-element WITHOUT
// propagating carries across element boundaries — this is the whole
// point of SIMD. We do this by masking off the high bit of each
// element when checking overflow, or by extracting and operating on
// each element separately.

// mmxSrc64 reads the 64-bit MMX source operand from a ModR/M operand
// — either an MMX register or a 64-bit memory location.
func (c *CPU) mmxSrc64(mr modRMResult) uint64 {
	if mr.isReg {
		return c.mm[mr.rm]
	}
	return c.readMem64(c.segBaseForModRM(mr) + mr.ea)
}

// packedAdd returns the packed sum of `a` and `b`, treating both as
// arrays of `elemSize`-byte unsigned integers. Carries do NOT
// propagate across element boundaries (PADD wraps within each lane).
func packedAdd(a, b uint64, elemSize int) uint64 {
	switch elemSize {
	case 1:
		var out uint64
		for i := 0; i < 8; i++ {
			s := byte(a>>(uint(i)*8)) + byte(b>>(uint(i)*8))
			out |= uint64(s) << (uint(i) * 8)
		}
		return out
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			s := uint16(a>>(uint(i)*16)) + uint16(b>>(uint(i)*16))
			out |= uint64(s) << (uint(i) * 16)
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			s := uint32(a>>(uint(i)*32)) + uint32(b>>(uint(i)*32))
			out |= uint64(s) << (uint(i) * 32)
		}
		return out
	case 8:
		return a + b
	}
	return 0
}

// packedSub returns the packed difference (a - b) per element.
func packedSub(a, b uint64, elemSize int) uint64 {
	switch elemSize {
	case 1:
		var out uint64
		for i := 0; i < 8; i++ {
			s := byte(a>>(uint(i)*8)) - byte(b>>(uint(i)*8))
			out |= uint64(s) << (uint(i) * 8)
		}
		return out
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			s := uint16(a>>(uint(i)*16)) - uint16(b>>(uint(i)*16))
			out |= uint64(s) << (uint(i) * 16)
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			s := uint32(a>>(uint(i)*32)) - uint32(b>>(uint(i)*32))
			out |= uint64(s) << (uint(i) * 32)
		}
		return out
	case 8:
		return a - b
	}
	return 0
}

// packedCmpEq returns, per element, all-1s if the element of a equals
// the element of b, otherwise all-0s.
func packedCmpEq(a, b uint64, elemSize int) uint64 {
	switch elemSize {
	case 1:
		var out uint64
		for i := 0; i < 8; i++ {
			if byte(a>>(uint(i)*8)) == byte(b>>(uint(i)*8)) {
				out |= 0xFF << (uint(i) * 8)
			}
		}
		return out
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			if uint16(a>>(uint(i)*16)) == uint16(b>>(uint(i)*16)) {
				out |= 0xFFFF << (uint(i) * 16)
			}
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			if uint32(a>>(uint(i)*32)) == uint32(b>>(uint(i)*32)) {
				out |= uint64(0xFFFFFFFF) << (uint(i) * 32)
			}
		}
		return out
	}
	return 0
}

// packedCmpGt returns, per element, all-1s if the signed element of a
// is greater than the signed element of b, otherwise all-0s.
func packedCmpGt(a, b uint64, elemSize int) uint64 {
	switch elemSize {
	case 1:
		var out uint64
		for i := 0; i < 8; i++ {
			if int8(a>>(uint(i)*8)) > int8(b>>(uint(i)*8)) {
				out |= 0xFF << (uint(i) * 8)
			}
		}
		return out
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			if int16(a>>(uint(i)*16)) > int16(b>>(uint(i)*16)) {
				out |= 0xFFFF << (uint(i) * 16)
			}
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			if int32(a>>(uint(i)*32)) > int32(b>>(uint(i)*32)) {
				out |= uint64(0xFFFFFFFF) << (uint(i) * 32)
			}
		}
		return out
	}
	return 0
}
