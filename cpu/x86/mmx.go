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

// packedUnpackLow takes the LOW half of each operand (4 bytes for
// elemSize 1, 2 words for elemSize 2, 1 dword for elemSize 4) and
// interleaves them: src element 0, dst element 0, src element 1,
// dst element 1, …
//
// PUNPCKLBW (elemSize 1): low 4 bytes of dst + low 4 bytes of src
// → 8 bytes interleaved.
// PUNPCKLWD (elemSize 2): low 2 words of dst + src → 4 words.
// PUNPCKLDQ (elemSize 4): low dword of dst + src → 2 dwords.
//
// `dst` is the original destination contents (the one in the MMX reg
// encoded by `reg`); `src` is the r/m operand.
func packedUnpackLow(dst, src uint64, elemSize int) uint64 {
	switch elemSize {
	case 1:
		var out uint64
		for i := 0; i < 4; i++ {
			d := byte(dst >> (uint(i) * 8))
			s := byte(src >> (uint(i) * 8))
			out |= uint64(d) << (uint(i*2) * 8)
			out |= uint64(s) << (uint(i*2+1) * 8)
		}
		return out
	case 2:
		var out uint64
		for i := 0; i < 2; i++ {
			d := uint16(dst >> (uint(i) * 16))
			s := uint16(src >> (uint(i) * 16))
			out |= uint64(d) << (uint(i*2) * 16)
			out |= uint64(s) << (uint(i*2+1) * 16)
		}
		return out
	case 4:
		return (dst & 0xFFFFFFFF) | (src&0xFFFFFFFF)<<32
	}
	return 0
}

// packedUnpackHigh takes the HIGH half of each operand and interleaves
// them, analogous to packedUnpackLow.
func packedUnpackHigh(dst, src uint64, elemSize int) uint64 {
	switch elemSize {
	case 1:
		var out uint64
		for i := 0; i < 4; i++ {
			d := byte(dst >> (uint(4+i) * 8))
			s := byte(src >> (uint(4+i) * 8))
			out |= uint64(d) << (uint(i*2) * 8)
			out |= uint64(s) << (uint(i*2+1) * 8)
		}
		return out
	case 2:
		var out uint64
		for i := 0; i < 2; i++ {
			d := uint16(dst >> (uint(2+i) * 16))
			s := uint16(src >> (uint(2+i) * 16))
			out |= uint64(d) << (uint(i*2) * 16)
			out |= uint64(s) << (uint(i*2+1) * 16)
		}
		return out
	case 4:
		return (dst >> 32) | ((src >> 32) << 32)
	}
	return 0
}

// packSignedSat narrows N-byte signed elements to (N/2)-byte signed
// elements with saturation. Lanes from `a` go in the low half; lanes
// from `b` go in the high half.
//
// PACKSSWB (4 words → 8 bytes per operand, total 8 bytes out): each
// signed-16 is clamped to int8 range.
// PACKSSDW (2 dwords → 4 words): signed-32 clamped to int16 range.
func packSignedSat(a, b uint64, srcSize int) uint64 {
	var out uint64
	switch srcSize {
	case 2: // PACKSSWB
		for i, src := 0, a; i < 8; i++ {
			if i == 4 {
				src = b
			}
			v := int16(src >> (uint(i&3) * 16))
			var clamped int8
			switch {
			case v > 127:
				clamped = 127
			case v < -128:
				clamped = -128
			default:
				clamped = int8(v)
			}
			out |= uint64(uint8(clamped)) << (uint(i) * 8)
		}
		return out
	case 4: // PACKSSDW
		for i, src := 0, a; i < 4; i++ {
			if i == 2 {
				src = b
			}
			v := int32(src >> (uint(i&1) * 32))
			var clamped int16
			switch {
			case v > 32767:
				clamped = 32767
			case v < -32768:
				clamped = -32768
			default:
				clamped = int16(v)
			}
			out |= uint64(uint16(clamped)) << (uint(i) * 16)
		}
		return out
	}
	return 0
}

// packUnsignedSat narrows signed-16 elements to unsigned-8 with
// saturation (PACKUSWB only — there is no PACKUSDW in MMX).
func packUnsignedSat(a, b uint64) uint64 {
	var out uint64
	for i, src := 0, a; i < 8; i++ {
		if i == 4 {
			src = b
		}
		v := int16(src >> (uint(i&3) * 16))
		var clamped uint8
		switch {
		case v > 255:
			clamped = 255
		case v < 0:
			clamped = 0
		default:
			clamped = uint8(v)
		}
		out |= uint64(clamped) << (uint(i) * 8)
	}
	return out
}

// packedShiftLeft logically shifts each element of `a` left by `count`
// bits (zero-fill). If `count` exceeds the element width, the result
// is zero (per MMX semantics).
func packedShiftLeft(a uint64, count int, elemSize int) uint64 {
	if count >= elemSize*8 {
		return 0
	}
	switch elemSize {
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			v := uint16(a>>(uint(i)*16)) << uint(count)
			out |= uint64(v) << (uint(i) * 16)
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			v := uint32(a>>(uint(i)*32)) << uint(count)
			out |= uint64(v) << (uint(i) * 32)
		}
		return out
	case 8:
		return a << uint(count)
	}
	return 0
}

// packedShiftRightLogical: per-element logical right shift (zero-fill).
func packedShiftRightLogical(a uint64, count int, elemSize int) uint64 {
	if count >= elemSize*8 {
		return 0
	}
	switch elemSize {
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			v := uint16(a>>(uint(i)*16)) >> uint(count)
			out |= uint64(v) << (uint(i) * 16)
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			v := uint32(a>>(uint(i)*32)) >> uint(count)
			out |= uint64(v) << (uint(i) * 32)
		}
		return out
	case 8:
		return a >> uint(count)
	}
	return 0
}

// packedShiftRightArith: per-element arithmetic right shift (sign-fill).
// Note: there is no PSRAQ in MMX; PSRA only operates on words/dwords.
// If count exceeds element width, result is all-1s for negative input,
// all-0s for positive.
func packedShiftRightArith(a uint64, count int, elemSize int) uint64 {
	if count >= elemSize*8 {
		count = elemSize*8 - 1
	}
	switch elemSize {
	case 2:
		var out uint64
		for i := 0; i < 4; i++ {
			v := int16(a>>(uint(i)*16)) >> uint(count)
			out |= uint64(uint16(v)) << (uint(i) * 16)
		}
		return out
	case 4:
		var out uint64
		for i := 0; i < 2; i++ {
			v := int32(a>>(uint(i)*32)) >> uint(count)
			out |= uint64(uint32(v)) << (uint(i) * 32)
		}
		return out
	}
	return 0
}

// packedMulLow: per-element 16x16 multiply, keeping the low 16 bits.
// PMULLW: each lane of `a` × corresponding lane of `b`, result is
// low 16 bits of the 32-bit product. The high half is discarded.
func packedMulLow(a, b uint64) uint64 {
	var out uint64
	for i := 0; i < 4; i++ {
		x := uint16(a >> (uint(i) * 16))
		y := uint16(b >> (uint(i) * 16))
		out |= uint64(x*y) << (uint(i) * 16)
	}
	return out
}

// packedMulHigh: per-element 16x16 multiply, keeping the high 16 bits.
// PMULHW (signed) vs PMULHUW (unsigned).
func packedMulHigh(a, b uint64, signed bool) uint64 {
	var out uint64
	for i := 0; i < 4; i++ {
		if signed {
			x := int32(int16(a >> (uint(i) * 16)))
			y := int32(int16(b >> (uint(i) * 16)))
			hi := uint16(uint32(x*y) >> 16)
			out |= uint64(hi) << (uint(i) * 16)
		} else {
			x := uint32(uint16(a >> (uint(i) * 16)))
			y := uint32(uint16(b >> (uint(i) * 16)))
			hi := uint16((x * y) >> 16)
			out |= uint64(hi) << (uint(i) * 16)
		}
	}
	return out
}

// packedAddSat: per-element add with signed/unsigned saturation.
//   - signed = true:  PADDS{B,W}  clamps to int8/int16 range
//   - signed = false: PADDUS{B,W} clamps to uint8/uint16 range
// Only byte and word element sizes are defined in MMX.
func packedAddSat(a, b uint64, elemSize int, signed bool) uint64 {
	var out uint64
	if elemSize == 1 {
		for i := 0; i < 8; i++ {
			x := byte(a >> (uint(i) * 8))
			y := byte(b >> (uint(i) * 8))
			var r byte
			if signed {
				sum := int16(int8(x)) + int16(int8(y))
				switch {
				case sum > 127:
					r = 127
				case sum < -128:
					r = 0x80 // int8 min
				default:
					r = byte(int8(sum))
				}
			} else {
				sum := uint16(x) + uint16(y)
				if sum > 0xFF {
					r = 0xFF
				} else {
					r = byte(sum)
				}
			}
			out |= uint64(r) << (uint(i) * 8)
		}
		return out
	}
	// elemSize == 2
	for i := 0; i < 4; i++ {
		x := uint16(a >> (uint(i) * 16))
		y := uint16(b >> (uint(i) * 16))
		var r uint16
		if signed {
			sum := int32(int16(x)) + int32(int16(y))
			switch {
			case sum > 32767:
				r = 32767
			case sum < -32768:
				r = 0x8000 // int16 min
			default:
				r = uint16(int16(sum))
			}
		} else {
			sum := uint32(x) + uint32(y)
			if sum > 0xFFFF {
				r = 0xFFFF
			} else {
				r = uint16(sum)
			}
		}
		out |= uint64(r) << (uint(i) * 16)
	}
	return out
}

// packedSubSat: same shape as packedAddSat but for subtraction.
func packedSubSat(a, b uint64, elemSize int, signed bool) uint64 {
	var out uint64
	if elemSize == 1 {
		for i := 0; i < 8; i++ {
			x := byte(a >> (uint(i) * 8))
			y := byte(b >> (uint(i) * 8))
			var r byte
			if signed {
				diff := int16(int8(x)) - int16(int8(y))
				switch {
				case diff > 127:
					r = 127
				case diff < -128:
					r = 0x80 // int8 min
				default:
					r = byte(int8(diff))
				}
			} else {
				if x < y {
					r = 0
				} else {
					r = x - y
				}
			}
			out |= uint64(r) << (uint(i) * 8)
		}
		return out
	}
	for i := 0; i < 4; i++ {
		x := uint16(a >> (uint(i) * 16))
		y := uint16(b >> (uint(i) * 16))
		var r uint16
		if signed {
			diff := int32(int16(x)) - int32(int16(y))
			switch {
			case diff > 32767:
				r = 32767
			case diff < -32768:
				r = 0x8000 // int16 min
			default:
				r = uint16(int16(diff))
			}
		} else {
			if x < y {
				r = 0
			} else {
				r = x - y
			}
		}
		out |= uint64(r) << (uint(i) * 16)
	}
	return out
}

// byteShiftLeft128 shifts a 128-bit value left by `count` BYTES (zero fill).
// PSLLDQ semantics: count >= 16 → result is zero.
func byteShiftLeft128(v [2]uint64, count int) [2]uint64 {
	if count >= 16 {
		return [2]uint64{0, 0}
	}
	if count == 0 {
		return v
	}
	if count >= 8 {
		// Whole 64-bit shift, plus residual byte shift in high lane.
		return [2]uint64{0, v[0] << (uint(count-8) * 8)}
	}
	// count in 1..7: bits flow from low qword into high qword.
	bits := uint(count) * 8
	return [2]uint64{
		v[0] << bits,
		(v[1] << bits) | (v[0] >> (64 - bits)),
	}
}

// byteShiftRight128 shifts a 128-bit value right by `count` BYTES (zero fill).
// PSRLDQ semantics: count >= 16 → result is zero.
func byteShiftRight128(v [2]uint64, count int) [2]uint64 {
	if count >= 16 {
		return [2]uint64{0, 0}
	}
	if count == 0 {
		return v
	}
	if count >= 8 {
		return [2]uint64{v[1] >> (uint(count-8) * 8), 0}
	}
	bits := uint(count) * 8
	return [2]uint64{
		(v[0] >> bits) | (v[1] << (64 - bits)),
		v[1] >> bits,
	}
}

// pshufWord reorders 4 words (16-bit each) in a 64-bit value according to
// imm8 (two bits per output lane select source lane).
//
// PSHUFW mm, mm/m64, imm8 — MMX.
// PSHUFLW xmm, xmm/m128, imm8 reuses this on the LOW 64 bits.
// PSHUFHW xmm, xmm/m128, imm8 reuses this on the HIGH 64 bits.
func pshufWord(src uint64, imm uint8) uint64 {
	var out uint64
	for i := 0; i < 4; i++ {
		idx := uint((imm >> uint(i*2)) & 3)
		w := uint16(src >> (idx * 16))
		out |= uint64(w) << (uint(i) * 16)
	}
	return out
}

// pshufDword reorders 4 doublewords (32-bit each) in a 128-bit value
// according to imm8 (two bits per output lane select source lane).
//
// PSHUFD xmm, xmm/m128, imm8 — SSE2.
func pshufDword(src [2]uint64, imm uint8) [2]uint64 {
	srcWords := [4]uint32{
		uint32(src[0]),
		uint32(src[0] >> 32),
		uint32(src[1]),
		uint32(src[1] >> 32),
	}
	var out [4]uint32
	for i := 0; i < 4; i++ {
		idx := (imm >> uint(i*2)) & 3
		out[i] = srcWords[idx]
	}
	return [2]uint64{
		uint64(out[0]) | uint64(out[1])<<32,
		uint64(out[2]) | uint64(out[3])<<32,
	}
}

// packedMulUDQ: PMULUDQ. Takes the LOW 32 bits of each 64-bit lane,
// multiplies them as unsigned, produces a 64-bit result per lane.
//
// MMX form: 1 × 64-bit lane → result is low 32 of `a` × low 32 of `b`.
// SSE2 form (called twice, once per 64-bit lane in a 128-bit XMM).
func packedMulUDQ(a, b uint64) uint64 {
	return uint64(uint32(a)) * uint64(uint32(b))
}

// packedMaddWord: PMADDWD. Treats each operand as 4 signed-16 lanes.
// Pairs of adjacent lanes are multiplied to int32, then each pair is
// summed to produce 2 signed-32 output lanes.
//   out[0] = (int16)a[0] * (int16)b[0] + (int16)a[1] * (int16)b[1]
//   out[1] = (int16)a[2] * (int16)b[2] + (int16)a[3] * (int16)b[3]
func packedMaddWord(a, b uint64) uint64 {
	var out uint64
	for i := 0; i < 2; i++ {
		x0 := int32(int16(a >> (uint(i*2) * 16)))
		y0 := int32(int16(b >> (uint(i*2) * 16)))
		x1 := int32(int16(a >> (uint(i*2+1) * 16)))
		y1 := int32(int16(b >> (uint(i*2+1) * 16)))
		sum := uint32(x0*y0 + x1*y1)
		out |= uint64(sum) << (uint(i) * 32)
	}
	return out
}
