// Package softfp F128 operations
// This file implements 128-bit (quad-precision) floating-point operations.
// Reference: softfp_template.h from TinyEMU 2019-12-21 with F_SIZE=128

package softfp

import (
	"math/bits"

	"lukechampine.com/uint128"
)

// Uint128 is an alias for the uint128 type used for 128-bit floats.
type Uint128 = uint128.Uint128

// Float128 masks and constants
var (
	// Float128MantMask is the mask for mantissa bits (112 bits)
	Float128MantMask = uint128.New(0xFFFFFFFFFFFFFFFF, 0x0000FFFFFFFFFFFF)

	// Float128SignMask is the mask for the sign bit
	Float128SignMask = uint128.New(0, 0x8000000000000000)

	// Float128QNaNMask is the quiet NaN bit in mantissa
	Float128QNaNMask = uint128.New(0, 0x0000800000000000)

	// Float128QNaN is the canonical quiet NaN value
	Float128QNaN = uint128.New(0, 0x7FFF800000000000)

	// Float128Zero is zero
	Float128Zero = uint128.Zero

	// Float128One is used for comparisons
	Float128One = uint128.From64(1)

	// Float128Max is all 1s (used for NOT operations)
	Float128Max = uint128.Max
)

// not128 performs bitwise NOT on a 128-bit value.
func not128(a Uint128) Uint128 {
	return a.Xor(Float128Max)
}

// Pack128 assembles a 128-bit float from sign, exponent, and mantissa.
// Reference: tinyemu-2019-12-21/softfp_template.h:78-83 (pack_sf with F_SIZE=128)
func Pack128(sign uint32, exp uint32, mant Uint128) Uint128 {
	// Sign is in bit 127, exponent in bits 126:112, mantissa in bits 111:0
	// Mantissa: Lo (64 bits) + lower 48 bits of Hi = 112 bits
	// Exponent: Hi bits 62:48 (relative to Hi)
	result := mant.And(Float128MantMask)
	expShifted := uint128.New(0, uint64(exp)<<48)
	result = result.Or(expShifted)
	if sign != 0 {
		result = result.Or(Float128SignMask)
	}
	return result
}

// Unpack128 extracts sign, exponent, and mantissa from a 128-bit float.
// Reference: tinyemu-2019-12-21/softfp_template.h:85-91 (unpack_sf with F_SIZE=128)
func Unpack128(a Uint128) (sign uint32, exp int32, mant Uint128) {
	sign = uint32(a.Hi >> 63)
	exp = int32((a.Hi >> 48) & uint64(Float128ExpMask))
	mant = a.And(Float128MantMask)
	return
}

// IsNaN128 returns true if the 128-bit float is a NaN (quiet or signaling).
// Reference: tinyemu-2019-12-21/softfp_template.h:224-231 (isnan_sf with F_SIZE=128)
func IsNaN128(a Uint128) bool {
	exp := (a.Hi >> 48) & uint64(Float128ExpMask)
	mant := a.And(Float128MantMask)
	return exp == uint64(Float128ExpMask) && !mant.IsZero()
}

// IsSigNaN128 returns true if the 128-bit float is a signaling NaN.
// Reference: tinyemu-2019-12-21/softfp_template.h:215-222 (issignan_sf with F_SIZE=128)
func IsSigNaN128(a Uint128) bool {
	// Check if exponent is all 1s and quiet bit is 0 (signaling)
	// exp1 spans exp and top bit of mantissa
	exp1 := (a.Hi >> 47) & ((1 << (Float128ExpBits + 1)) - 1)
	mant := a.And(Float128MantMask)
	return exp1 == uint64(2*Float128ExpMask) && !mant.IsZero()
}

// IsInf128 returns true if the 128-bit float is infinity (positive or negative).
func IsInf128(a Uint128) bool {
	exp := (a.Hi >> 48) & uint64(Float128ExpMask)
	mant := a.And(Float128MantMask)
	return exp == uint64(Float128ExpMask) && mant.IsZero()
}

// IsZero128 returns true if the 128-bit float is zero (positive or negative).
func IsZero128(a Uint128) bool {
	// Clear sign bit and check if zero
	return a.And(not128(Float128SignMask)).IsZero()
}

// normalizeSubnormal128 normalizes a subnormal 128-bit float mantissa.
// Reference: tinyemu-2019-12-21/softfp_template.h:298-304 (normalize_subnormal_sf with F_SIZE=128)
func normalizeSubnormal128(mant Uint128) (exp int32, normMant Uint128) {
	shift := Float128MantBits - (Float128Bits - 1 - clz128(mant))
	exp = int32(1 - shift)
	normMant = lshift128(mant, shift)
	return
}

// lshift128 performs a left shift on a 128-bit value.
func lshift128(a Uint128, shift int) Uint128 {
	if shift == 0 {
		return a
	}
	if shift >= 128 {
		return uint128.Zero
	}
	if shift >= 64 {
		// All bits move from Lo to Hi
		return uint128.New(0, a.Lo<<(shift-64))
	}
	// New Lo = old Lo << shift
	// New Hi = (old Hi << shift) | (old Lo >> (64 - shift))
	return uint128.New(a.Lo<<shift, (a.Hi<<shift)|(a.Lo>>(64-shift)))
}

// boolToU128 converts a bool to Uint128.
func boolToU128(b bool) Uint128 {
	if b {
		return Float128One
	}
	return Float128Zero
}

// roundPackFloat128 rounds and packs a 128-bit float result.
// Reference: tinyemu-2019-12-21/softfp_template.h:108-175 (round_pack_sf with F_SIZE=128)
func roundPackFloat128(sign uint32, exp int32, mant Uint128, rm RoundingMode, flags *ExceptionFlags) Uint128 {
	var addend Uint128
	var rndBits Uint128
	rndMask := lshift128(Float128One, float128RndSize).Sub(Float128One) // (1 << RND_SIZE) - 1

	// Determine rounding addend based on rounding mode
	switch rm {
	case RNE, RMM:
		addend = lshift128(Float128One, float128RndSize-1)
	case RTZ:
		addend = Float128Zero
	case RDN, RUP:
		// Round towards signed infinity
		if (sign != 0) != (rm == RUP) {
			addend = rndMask
		} else {
			addend = Float128Zero
		}
	}

	// Handle potential subnormal result
	if exp <= 0 {
		var isSubnormal bool

		// Check if result will be subnormal after rounding
		mantPlusAddend := mant.Add(addend)
		threshold := lshift128(Float128One, Float128Bits-1)
		if exp < 0 {
			isSubnormal = true
		} else {
			isSubnormal = mantPlusAddend.Cmp(threshold) < 0
		}

		// Shift right to denormalize
		diff := 1 - exp
		mant = rshiftRnd128(mant, int(diff))
		rndBits = mant.And(rndMask)

		// Set underflow flag if subnormal and inexact
		if isSubnormal && !rndBits.IsZero() {
			*flags |= FlagUnderflow
		}
		exp = 1
	} else {
		rndBits = mant.And(rndMask)
	}

	// Set inexact flag if rounding bits are non-zero
	if !rndBits.IsZero() {
		*flags |= FlagInexact
	}

	// Add rounding and shift to final mantissa position
	mant = rshift128(mant.Add(addend), float128RndSize)

	// For RNE, if exactly halfway, round to even
	halfWay := lshift128(Float128One, float128RndSize-1)
	if rm == RNE && rndBits.Equals(halfWay) {
		mant = mant.And(not128(Float128One)) // mant &= ~1
	}

	// Adjust exponent for carry out of mantissa
	mantCheck := rshift128(mant, Float128MantBits+1)
	if !mantCheck.IsZero() {
		exp++
	}

	if mant.Cmp(Float128MantMask) <= 0 {
		// Denormalized or zero
		exp = 0
	} else if exp >= int32(Float128ExpMask) {
		// Overflow
		if addend.IsZero() {
			// Round to max finite
			exp = int32(Float128ExpMask) - 1
			mant = Float128MantMask
		} else {
			// Round to infinity
			exp = int32(Float128ExpMask)
			mant = Float128Zero
		}
		*flags |= FlagOverflow | FlagInexact
	}

	return Pack128(sign, uint32(exp), mant)
}

// rshift128 performs a simple right shift (no sticky bit).
func rshift128(a Uint128, shift int) Uint128 {
	if shift == 0 {
		return a
	}
	if shift >= 128 {
		return uint128.Zero
	}
	if shift >= 64 {
		return uint128.From64(a.Hi >> (shift - 64))
	}
	return uint128.New(a.Hi>>shift, (a.Lo>>shift)|(a.Hi<<(64-shift)))
}

// normalizeFloat128 normalizes and packs a 128-bit float.
// Reference: tinyemu-2019-12-21/softfp_template.h:178-187 (normalize_sf with F_SIZE=128)
func normalizeFloat128(sign uint32, exp int32, mant Uint128, rm RoundingMode, flags *ExceptionFlags) Uint128 {
	shift := clz128(mant) - (Float128Bits - 1 - float128IMantSize)
	if shift < 0 {
		shift = 0
	}
	exp -= int32(shift)
	mant = lshift128(mant, shift)
	return roundPackFloat128(sign, exp, mant, rm, flags)
}

// normalize2Float128 normalizes and packs a 128-bit float from a double-word mantissa.
// Reference: tinyemu-2019-12-21/softfp_template.h:191-213 (normalize2_sf with F_SIZE=128)
func normalize2Float128(sign uint32, exp int32, mant1, mant0 Uint128, rm RoundingMode, flags *ExceptionFlags) Uint128 {
	var l, shift int

	if mant1.IsZero() {
		l = Float128Bits + clz128(mant0)
	} else {
		l = clz128(mant1)
	}

	shift = l - (Float128Bits - 1 - float128IMantSize)
	if shift < 0 {
		shift = 0
	}
	exp -= int32(shift)

	if shift == 0 {
		mant1 = mant1.Or(boolToU128(!mant0.IsZero()))
	} else if shift < Float128Bits {
		mant1 = lshift128(mant1, shift).Or(rshift128(mant0, Float128Bits-shift))
		mant0 = lshift128(mant0, shift)
		mant1 = mant1.Or(boolToU128(!mant0.IsZero()))
	} else {
		mant1 = lshift128(mant0, shift-Float128Bits)
	}

	return roundPackFloat128(sign, exp, mant1, rm, flags)
}

// AddF128 adds two 128-bit floats with the given rounding mode.
// Reference: tinyemu-2019-12-21/softfp_template.h:234-290 (add_sf with F_SIZE=128)
func AddF128(a, b Uint128, rm RoundingMode, flags *ExceptionFlags) Uint128 {
	// Swap so that abs(a) >= abs(b)
	aAbs := a.And(not128(Float128SignMask))
	bAbs := b.And(not128(Float128SignMask))
	if aAbs.Cmp(bAbs) < 0 {
		a, b = b, a
	}

	aSign := uint32(a.Hi >> 63)
	bSign := uint32(b.Hi >> 63)
	aExp := int32((a.Hi >> 48) & uint64(Float128ExpMask))
	bExp := int32((b.Hi >> 48) & uint64(Float128ExpMask))
	aMant := lshift128(a.And(Float128MantMask), 3)
	bMant := lshift128(b.And(Float128MantMask), 3)

	// Handle special cases for a (which has larger absolute value)
	if aExp == int32(Float128ExpMask) {
		if !aMant.IsZero() {
			// a is NaN - result is NaN
			qnanShifted := lshift128(Float128QNaNMask, 3)
			if aMant.And(qnanShifted).IsZero() || IsSigNaN128(b) {
				*flags |= FlagInvalidOp
			}
			return Float128QNaN
		} else if bExp == int32(Float128ExpMask) && aSign != bSign {
			// inf + (-inf) = NaN
			*flags |= FlagInvalidOp
			return Float128QNaN
		} else {
			// a is infinity, result is infinity with same sign
			return a
		}
	}

	// Add implicit bit for normal numbers
	implicitBit := lshift128(Float128One, Float128MantBits+3)
	if aExp == 0 {
		aExp = 1
	} else {
		aMant = aMant.Or(implicitBit)
	}

	if bExp == 0 {
		bExp = 1
	} else {
		bMant = bMant.Or(implicitBit)
	}

	// Align mantissas by shifting b right
	bMant = rshiftRnd128(bMant, int(aExp-bExp))

	if aSign == bSign {
		// Same signs: add absolute values
		aMant = aMant.Add(bMant)
	} else {
		// Different signs: subtract absolute values
		aMant = aMant.Sub(bMant)
		if aMant.IsZero() {
			// Zero result: sign depends on rounding mode
			if rm == RDN {
				aSign = 1
			} else {
				aSign = 0
			}
		}
	}

	aExp += int32(float128RndSize - 3)
	return normalizeFloat128(aSign, aExp, aMant, rm, flags)
}

// SubF128 subtracts two 128-bit floats with the given rounding mode.
// Reference: tinyemu-2019-12-21/softfp_template.h:292-296 (sub_sf with F_SIZE=128)
func SubF128(a, b Uint128, rm RoundingMode, flags *ExceptionFlags) Uint128 {
	return AddF128(a, b.Xor(Float128SignMask), rm, flags)
}

// FClass128 classifies a 128-bit float according to the RISC-V FCLASS instruction.
// Reference: tinyemu-2019-12-21/softfp_template.h:898-939 (fclass_sf)
func FClass128(a Uint128) FClass {
	sign := uint32(a.Hi >> 63)
	exp := int32((a.Hi >> 48) & uint64(Float128ExpMask))
	mant := a.And(Float128MantMask)

	if exp == int32(Float128ExpMask) {
		if !mant.IsZero() {
			// NaN
			if !mant.And(Float128QNaNMask).IsZero() {
				return FClassQNaN
			}
			return FClassSNaN
		}
		// Infinity
		if sign != 0 {
			return FClassNegInf
		}
		return FClassPosInf
	}

	if exp == 0 {
		if mant.IsZero() {
			// Zero
			if sign != 0 {
				return FClassNegZero
			}
			return FClassPosZero
		}
		// Subnormal
		if sign != 0 {
			return FClassNegSubnormal
		}
		return FClassPosSubnormal
	}

	// Normal
	if sign != 0 {
		return FClassNegNormal
	}
	return FClassPosNormal
}

// Sign injection operations for 128-bit floats

// SignF128 returns a value with the magnitude of a and the sign of b (FSGNJ).
func SignF128(a, b Uint128) Uint128 {
	return a.And(not128(Float128SignMask)).Or(b.And(Float128SignMask))
}

// SignNF128 returns a value with the magnitude of a and the negated sign of b (FSGNJN).
func SignNF128(a, b Uint128) Uint128 {
	return a.And(not128(Float128SignMask)).Or(not128(b).And(Float128SignMask))
}

// SignXF128 returns a value with the magnitude of a and sign XOR of a and b (FSGNJX).
func SignXF128(a, b Uint128) Uint128 {
	return a.Xor(b.And(Float128SignMask))
}

// Comparison operations

// EqQuietF128 returns true if a == b (quiet comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:840-852 (eq_quiet_sf with F_SIZE=128)
func EqQuietF128(a, b Uint128, flags *ExceptionFlags) bool {
	if IsNaN128(a) || IsNaN128(b) {
		if IsSigNaN128(a) || IsSigNaN128(b) {
			*flags |= FlagInvalidOp
		}
		return false
	}

	// Zero case: +0 == -0
	cleared := a.Or(b).And(not128(Float128SignMask))
	if cleared.IsZero() {
		return true
	}
	return a.Equals(b)
}

// LeF128 returns true if a <= b (signaling comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:854-874 (le_sf with F_SIZE=128)
func LeF128(a, b Uint128, flags *ExceptionFlags) bool {
	if IsNaN128(a) || IsNaN128(b) {
		*flags |= FlagInvalidOp
		return false
	}

	aSign := uint32(a.Hi >> 63)
	bSign := uint32(b.Hi >> 63)

	if aSign != bSign {
		// a <= b if a is negative OR both are zero
		cleared := a.Or(b).And(not128(Float128SignMask))
		return aSign != 0 || cleared.IsZero()
	}

	// Same sign
	if aSign != 0 {
		return a.Cmp(b) >= 0
	}
	return a.Cmp(b) <= 0
}

// LtF128 returns true if a < b (signaling comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:876-896 (lt_sf with F_SIZE=128)
func LtF128(a, b Uint128, flags *ExceptionFlags) bool {
	if IsNaN128(a) || IsNaN128(b) {
		*flags |= FlagInvalidOp
		return false
	}

	aSign := uint32(a.Hi >> 63)
	bSign := uint32(b.Hi >> 63)

	if aSign != bSign {
		// a < b if a is negative AND not both zero
		cleared := a.Or(b).And(not128(Float128SignMask))
		return aSign != 0 && !cleared.IsZero()
	}

	// Same sign
	if aSign != 0 {
		return a.Cmp(b) > 0
	}
	return a.Cmp(b) < 0
}

// mulU128 multiplies two 128-bit values and returns high and low parts.
// Uses 64-bit multiplication to produce a 256-bit result.
// Reference: tinyemu-2019-12-21/softfp_template.h:308-343 (mul_u with F_SIZE=128)
func mulU128(a, b Uint128) (high, low Uint128) {
	// Split into 64-bit halves
	a0 := a.Lo
	a1 := a.Hi
	b0 := b.Lo
	b1 := b.Hi

	// 64x64 -> 128-bit products using bits.Mul64
	r00Hi, r00Lo := bits.Mul64(a0, b0)
	r01Hi, r01Lo := bits.Mul64(a0, b1)
	r10Hi, r10Lo := bits.Mul64(a1, b0)
	r11Hi, r11Lo := bits.Mul64(a1, b1)

	// Combine:
	// Result[0:64] = r00.Lo
	// Result[64:128] = r00.Hi + r01.Lo + r10.Lo (with carry)
	// Result[128:192] = r01.Hi + r10.Hi + r11.Lo (with carry)
	// Result[192:256] = r11.Hi (with carry)

	r0 := r00Lo

	// c = r00.Hi + r01.Lo + r10.Lo
	c := uint128.New(r00Hi, 0)
	c = c.Add(uint128.From64(r01Lo))
	c = c.Add(uint128.From64(r10Lo))
	r1 := c.Lo

	// c = (c >> 64) + r01.Hi + r10.Hi + r11.Lo
	c = uint128.From64(c.Hi)
	c = c.Add(uint128.From64(r01Hi))
	c = c.Add(uint128.From64(r10Hi))
	c = c.Add(uint128.From64(r11Lo))
	r2 := c.Lo

	// r3 = (c >> 64) + r11.Hi
	r3 := c.Hi + r11Hi

	// C: *plow = ((F_UINT)r1 << FH_SIZE) | r0  -> {Lo: r0, Hi: r1}
	// C: return ((F_UINT)r3 << FH_SIZE) | r2   -> {Lo: r2, Hi: r3}
	low = uint128.New(r0, r1)
	high = uint128.New(r2, r3)
	return
}
