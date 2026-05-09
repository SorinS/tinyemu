// Package softfp F64 operations
// This file implements 64-bit floating-point operations.
// Reference: softfp_template.h from TinyEMU 2019-12-21

package softfp

import "math/bits"

// AddF64 adds two 64-bit floats with the given rounding mode.
// Reference: add_sf in softfp_template.h
func AddF64(a, b uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	// Swap so that abs(a) >= abs(b)
	if (a & ^Float64SignMask) < (b & ^Float64SignMask) {
		a, b = b, a
	}

	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))
	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	bExp := int32((b >> Float64MantBits) & Float64ExpMask)
	aMant := (a & Float64MantMask) << 3
	bMant := (b & Float64MantMask) << 3

	// Handle special cases for a (which has larger absolute value)
	if aExp == Float64ExpMask {
		if aMant != 0 {
			// a is NaN - result is NaN
			if (aMant&(Float64QNaNMask<<3)) == 0 || IsSigNaN64(b) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		} else if bExp == Float64ExpMask && aSign != bSign {
			// inf + (-inf) = NaN
			*flags |= FlagInvalidOp
			return Float64QNaN
		} else {
			// a is infinity, result is infinity with same sign
			return a
		}
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant |= 1 << (Float64MantBits + 3)
	}

	if bExp == 0 {
		bExp = 1
	} else {
		bMant |= 1 << (Float64MantBits + 3)
	}

	// Align mantissas by shifting b right
	bMant = rshiftRnd64(bMant, int(aExp-bExp))

	if aSign == bSign {
		// Same signs: add absolute values
		aMant += bMant
	} else {
		// Different signs: subtract absolute values
		aMant -= bMant
		if aMant == 0 {
			// Zero result: sign depends on rounding mode
			if rm == RDN {
				aSign = 1
			} else {
				aSign = 0
			}
		}
	}

	aExp += int32(float64RndSize - 3)
	return normalizeFloat64(aSign, aExp, aMant, rm, flags)
}

// SubF64 subtracts two 64-bit floats with the given rounding mode.
// Reference: sub_sf in softfp_template.h
func SubF64(a, b uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return AddF64(a, b^Float64SignMask, rm, flags)
}

// mulU64 multiplies two uint64 values and returns high and low parts.
// Uses 32-bit half-word multiplication (like C version without F_ULONG).
// Reference: tinyemu-2019-12-21/softfp_template.h:320-343 (mul_u without F_ULONG)
func mulU64(a, b uint64) (high, low uint64) {
	a0 := uint64(uint32(a))
	a1 := a >> 32
	b0 := uint64(uint32(b))
	b1 := b >> 32

	r00 := a0 * b0
	r01 := a0 * b1
	r10 := a1 * b0
	r11 := a1 * b1

	r0 := uint32(r00)
	c := (r00 >> 32) + uint64(uint32(r01)) + uint64(uint32(r10))
	r1 := uint32(c)
	c = (c >> 32) + (r01 >> 32) + (r10 >> 32) + uint64(uint32(r11))
	r2 := uint32(c)
	r3 := uint32(c>>32) + uint32(r11>>32)

	low = (uint64(r1) << 32) | uint64(r0)
	high = (uint64(r3) << 32) | uint64(r2)
	return
}

// MulF64 multiplies two 64-bit floats with the given rounding mode.
// Reference: tinyemu-2019-12-21/softfp_template.h:349-399 (mul_sf)
func MulF64(a, b uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))
	rSign := aSign ^ bSign

	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	bExp := int32((b >> Float64MantBits) & Float64ExpMask)
	aMant := a & Float64MantMask
	bMant := b & Float64MantMask

	// Handle special cases
	if aExp == Float64ExpMask || bExp == Float64ExpMask {
		if IsNaN64(a) || IsNaN64(b) {
			if IsSigNaN64(a) || IsSigNaN64(b) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		}
		// One or both is infinity
		if (aExp == Float64ExpMask && bExp == 0 && bMant == 0) ||
			(bExp == Float64ExpMask && aExp == 0 && aMant == 0) {
			// inf * 0 = NaN
			*flags |= FlagInvalidOp
			return Float64QNaN
		}
		// inf * finite = inf
		return Pack64(rSign, Float64ExpMask, 0)
	}

	// Handle zero
	if aExp == 0 {
		if aMant == 0 {
			return Pack64(rSign, 0, 0) // zero
		}
		aExp, aMant = normalizeSubnormal64(aMant)
	} else {
		aMant |= 1 << Float64MantBits
	}

	if bExp == 0 {
		if bMant == 0 {
			return Pack64(rSign, 0, 0) // zero
		}
		bExp, bMant = normalizeSubnormal64(bMant)
	} else {
		bMant |= 1 << Float64MantBits
	}

	// Calculate result exponent
	rExp := aExp + bExp - (1 << (Float64ExpBits - 1)) + 2

	// Multiply mantissas
	rMantHigh, rMantLow := mulU64(aMant<<float64RndSize, bMant<<(float64RndSize+1))
	if rMantLow != 0 {
		rMantHigh |= 1
	}

	return normalizeFloat64(rSign, rExp, rMantHigh, rm, flags)
}

// divremU64 divides (ah << 64 | al) by b, returning quotient and remainder.
// Uses bit-by-bit loop (like C version without F_ULONG).
// Reference: tinyemu-2019-12-21/softfp_template.h:567-585 (divrem_u without F_ULONG)
func divremU64(ah, al, b uint64) (quotient, remainder uint64) {
	// Long division algorithm since Go doesn't have native 128-bit division
	if ah == 0 && al < b {
		return 0, al
	}

	if ah >= b {
		panic("divremU64: ah >= b")
	}

	// Process bit by bit
	var q uint64
	r := ah
	for i := 63; i >= 0; i-- {
		// Shift in next bit from al
		rHigh := r >> 63
		r = (r << 1) | ((al >> i) & 1)

		if rHigh != 0 || r >= b {
			r -= b
			q |= 1 << i
		}
	}

	return q, r
}

// DivF64 divides two 64-bit floats with the given rounding mode.
// Reference: tinyemu-2019-12-21/softfp_template.h:589-653 (div_sf)
func DivF64(a, b uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))
	rSign := aSign ^ bSign

	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	bExp := int32((b >> Float64MantBits) & Float64ExpMask)
	aMant := a & Float64MantMask
	bMant := b & Float64MantMask

	// Handle NaN and infinity for a
	if aExp == Float64ExpMask {
		if aMant != 0 || IsNaN64(b) {
			if IsSigNaN64(a) || IsSigNaN64(b) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		} else if bExp == Float64ExpMask {
			// inf / inf = NaN
			*flags |= FlagInvalidOp
			return Float64QNaN
		}
		// inf / finite = inf
		return Pack64(rSign, Float64ExpMask, 0)
	}

	// Handle NaN and infinity for b
	if bExp == Float64ExpMask {
		if bMant != 0 {
			if IsSigNaN64(a) || IsSigNaN64(b) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		}
		// finite / inf = 0
		return Pack64(rSign, 0, 0)
	}

	// Handle division by zero
	if bExp == 0 && bMant == 0 {
		if aExp == 0 && aMant == 0 {
			// 0 / 0 = NaN
			*flags |= FlagInvalidOp
			return Float64QNaN
		}
		// finite / 0 = inf
		*flags |= FlagDivideZero
		return Pack64(rSign, Float64ExpMask, 0)
	}

	// Normalize b
	if bExp == 0 {
		bExp, bMant = normalizeSubnormal64(bMant)
	} else {
		bMant |= 1 << Float64MantBits
	}

	// Handle zero dividend
	if aExp == 0 {
		if aMant == 0 {
			return Pack64(rSign, 0, 0) // zero
		}
		aExp, aMant = normalizeSubnormal64(aMant)
	} else {
		aMant |= 1 << Float64MantBits
	}

	// Calculate result exponent
	rExp := aExp - bExp + (1 << (Float64ExpBits - 1)) - 1

	// Divide mantissas
	rMant, r := divremU64(aMant, 0, bMant<<2)
	if r != 0 {
		rMant |= 1
	}

	return normalizeFloat64(rSign, rExp, rMant, rm, flags)
}

// sqrtremU64 computes sqrt(ah << 64 | al) where the input < 2^62.
// Returns the square root and whether the result was inexact.
// Reference: tinyemu-2019-12-21/softfp_template.h:689-716 (sqrtrem_u without F_ULONG)
func sqrtremU64(ah, al uint64) (result uint64, inexact bool) {
	if ah == 0 && al == 0 {
		return 0, false
	}

	// 2^l >= a
	var l int
	if ah != 0 {
		l = 2*64 - bits.LeadingZeros64(ah-1)
	} else {
		l = 64 - bits.LeadingZeros64(al-1)
	}

	// Initial guess
	u := uint64(1) << ((l + 1) / 2)

	// Newton-Raphson iteration using 128-bit arithmetic approximation
	for {
		s := u
		// u = ((a / s) + s) / 2
		// We need to compute (ah << 64 | al) / s
		q, _ := divremU64(ah, al, s)
		u = (q + s) / 2
		if u >= s {
			// Converged - check for exact square
			// Compute s * s using mulU64
			sqHi, sqLo := mulU64(s, s)
			if sqHi == ah && sqLo == al {
				return s, false
			}
			return s, true
		}
	}
}

// SqrtF64 computes the square root of a 64-bit float.
// Reference: tinyemu-2019-12-21/softfp_template.h:720-767 (sqrt_sf)
func SqrtF64(a uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	aSign := uint32(a >> (Float64Bits - 1))
	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	aMant := a & Float64MantMask

	// Handle special cases
	if aExp == Float64ExpMask {
		if aMant != 0 {
			// sqrt(NaN) = NaN
			if IsSigNaN64(a) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		} else if aSign != 0 {
			// sqrt(-inf) = NaN
			*flags |= FlagInvalidOp
			return Float64QNaN
		}
		// sqrt(+inf) = +inf
		return a
	}

	// Handle negative numbers
	if aSign != 0 {
		if aExp == 0 && aMant == 0 {
			// sqrt(-0) = -0
			return a
		}
		*flags |= FlagInvalidOp
		return Float64QNaN
	}

	// Handle zero
	if aExp == 0 {
		if aMant == 0 {
			return Pack64(0, 0, 0) // zero
		}
		aExp, aMant = normalizeSubnormal64(aMant)
	} else {
		aMant |= 1 << Float64MantBits
	}

	// Adjust exponent
	aExp -= Float64ExpMask / 2

	// Make exponent even for easier square root
	if (aExp & 1) != 0 {
		aExp--
		aMant <<= 1
	}

	aExp = (aExp >> 1) + Float64ExpMask/2

	// Shift mantissa for precision
	aMant <<= (Float64Bits - 4 - Float64MantBits)

	// Compute square root
	result, inexact := sqrtremU64(aMant, 0)
	if inexact {
		result |= 1
	}

	return normalizeFloat64(aSign, aExp, result, rm, flags)
}

// fmaF64MulZero handles the case when one of the multiplicands is zero in FMA.
func fmaF64MulZero(c uint64, rSign uint32, rm RoundingMode) uint64 {
	cSign := uint32(c >> (Float64Bits - 1))
	cExp := int32((c >> Float64MantBits) & Float64ExpMask)
	cMant := c & Float64MantMask

	if cExp == 0 && cMant == 0 {
		if cSign != rSign {
			if rm == RDN {
				rSign = 1
			} else {
				rSign = 0
			}
		}
		return Pack64(rSign, 0, 0)
	}
	return c
}

// FmaF64 performs fused multiply-add: (a * b) + c with a single rounding.
// Reference: tinyemu-2019-12-21/softfp_template.h:402-552 (fma_sf)
func FmaF64(a, b, c uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))
	cSign := uint32(c >> (Float64Bits - 1))
	rSign := aSign ^ bSign

	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	bExp := int32((b >> Float64MantBits) & Float64ExpMask)
	cExp := int32((c >> Float64MantBits) & Float64ExpMask)
	aMant := a & Float64MantMask
	bMant := b & Float64MantMask
	cMant := c & Float64MantMask

	// Handle special cases
	if aExp == Float64ExpMask || bExp == Float64ExpMask || cExp == Float64ExpMask {
		if IsNaN64(a) || IsNaN64(b) || IsNaN64(c) {
			if IsSigNaN64(a) || IsSigNaN64(b) || IsSigNaN64(c) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		}
		// Infinities
		if (aExp == Float64ExpMask && bExp == 0 && bMant == 0) ||
			(bExp == Float64ExpMask && aExp == 0 && aMant == 0) ||
			((aExp == Float64ExpMask || bExp == Float64ExpMask) &&
				cExp == Float64ExpMask && rSign != cSign) {
			*flags |= FlagInvalidOp
			return Float64QNaN
		} else if cExp == Float64ExpMask {
			return Pack64(cSign, Float64ExpMask, 0)
		} else {
			return Pack64(rSign, Float64ExpMask, 0)
		}
	}

	// Handle zero multiplicand
	if aExp == 0 && aMant == 0 {
		return fmaF64MulZero(c, rSign, rm)
	}
	if aExp == 0 {
		aExp, aMant = normalizeSubnormal64(aMant)
	} else {
		aMant |= 1 << Float64MantBits
	}

	if bExp == 0 && bMant == 0 {
		return fmaF64MulZero(c, rSign, rm)
	}
	if bExp == 0 {
		bExp, bMant = normalizeSubnormal64(bMant)
	} else {
		bMant |= 1 << Float64MantBits
	}

	// Multiply
	rExp := aExp + bExp - (1 << (Float64ExpBits - 1)) + 3
	rMant1, rMant0 := mulU64(aMant<<float64RndSize, bMant<<float64RndSize)

	// Normalize to F_SIZE - 3
	if rMant1 < (1 << (Float64Bits - 3)) {
		rMant1 = (rMant1 << 1) | (rMant0 >> (Float64Bits - 1))
		rMant0 <<= 1
		rExp--
	}

	// Add c
	if cExp == 0 {
		if cMant == 0 {
			// Add zero
			if rMant0 != 0 {
				rMant1 |= 1
			}
			return normalizeFloat64(rSign, rExp, rMant1, rm, flags)
		}
		cExp, cMant = normalizeSubnormal64(cMant)
	} else {
		cMant |= 1 << Float64MantBits
	}
	cExp++
	cMant1 := cMant << (float64RndSize - 1)
	cMant0 := uint64(0)

	// Ensure abs(r) >= abs(c)
	if !(rExp > cExp || (rExp == cExp && rMant1 >= cMant1)) {
		rMant1, cMant1 = cMant1, rMant1
		rMant0, cMant0 = cMant0, rMant0
		rExp, cExp = cExp, rExp
		rSign, cSign = cSign, rSign
	}

	// Right shift c_mant
	shift := rExp - cExp
	if shift >= 2*Float64Bits {
		if cMant0 != 0 || cMant1 != 0 {
			cMant0 = 1
		} else {
			cMant0 = 0
		}
		cMant1 = 0
	} else if shift >= Float64Bits+1 {
		cMant0 = rshiftRnd64(cMant1, int(shift-Float64Bits))
		cMant1 = 0
	} else if shift == Float64Bits {
		if cMant0 != 0 {
			cMant0 = cMant1 | 1
		} else {
			cMant0 = cMant1
		}
		cMant1 = 0
	} else if shift != 0 {
		mask := (uint64(1) << shift) - 1
		var sticky uint64
		if (cMant0 & mask) != 0 {
			sticky = 1
		}
		cMant0 = (cMant1 << (Float64Bits - shift)) | (cMant0 >> shift) | sticky
		cMant1 = cMant1 >> shift
	}

	// Add or subtract
	if rSign == cSign {
		rMant0Tmp := rMant0
		rMant0 += cMant0
		if rMant0 < rMant0Tmp {
			rMant1++
		}
		rMant1 += cMant1
	} else {
		rMant0Tmp := rMant0
		rMant0 -= cMant0
		if rMant0 > rMant0Tmp {
			rMant1--
		}
		rMant1 -= cMant1
		if rMant0 == 0 && rMant1 == 0 {
			if rm == RDN {
				rSign = 1
			} else {
				rSign = 0
			}
		}
	}

	return normalize2Float64(rSign, rExp, rMant1, rMant0, rm, flags)
}

// minMaxNaN64 handles NaN cases for min/max operations.
// Reference: tinyemu-2019-12-21/softfp_template.h:771-790 (min_max_nan_sf)
func minMaxNaN64(a, b uint64, flags *ExceptionFlags, minMaxType MinMaxType) uint64 {
	if IsSigNaN64(a) || IsSigNaN64(b) {
		*flags |= FlagInvalidOp
		if minMaxType == MinMaxIEEE754_2008 {
			return Float64QNaN
		}
	}
	if minMaxType == MinMaxProp {
		return Float64QNaN
	}
	// Return the non-NaN operand if possible
	if IsNaN64(a) {
		if IsNaN64(b) {
			return Float64QNaN
		}
		return b
	}
	return a
}

// MinF64 returns the minimum of two 64-bit floats.
// Reference: tinyemu-2019-12-21/softfp_template.h:792-814 (min_sf)
func MinF64(a, b uint64, flags *ExceptionFlags, minMaxType MinMaxType) uint64 {
	if IsNaN64(a) || IsNaN64(b) {
		return minMaxNaN64(a, b, flags, minMaxType)
	}

	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))

	if aSign != bSign {
		if aSign != 0 {
			return a
		}
		return b
	}

	// Same sign
	if (a < b) != (aSign != 0) {
		return a
	}
	return b
}

// MaxF64 returns the maximum of two 64-bit floats.
// Reference: tinyemu-2019-12-21/softfp_template.h:816-838 (max_sf)
func MaxF64(a, b uint64, flags *ExceptionFlags, minMaxType MinMaxType) uint64 {
	if IsNaN64(a) || IsNaN64(b) {
		return minMaxNaN64(a, b, flags, minMaxType)
	}

	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))

	if aSign != bSign {
		if aSign != 0 {
			return b
		}
		return a
	}

	// Same sign
	if (a < b) != (aSign != 0) {
		return b
	}
	return a
}

// EqQuietF64 returns true if a == b (quiet comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:840-852 (eq_quiet_sf)
func EqQuietF64(a, b uint64, flags *ExceptionFlags) bool {
	if IsNaN64(a) || IsNaN64(b) {
		if IsSigNaN64(a) || IsSigNaN64(b) {
			*flags |= FlagInvalidOp
		}
		return false
	}

	// Zero case: +0 == -0
	if ((a | b) << 1) == 0 {
		return true
	}
	return a == b
}

// LeF64 returns true if a <= b (signaling comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:854-874 (le_sf)
func LeF64(a, b uint64, flags *ExceptionFlags) bool {
	if IsNaN64(a) || IsNaN64(b) {
		*flags |= FlagInvalidOp
		return false
	}

	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))

	if aSign != bSign {
		// a <= b if a is negative OR both are zero
		return aSign != 0 || ((a|b)<<1) == 0
	}

	// Same sign
	if aSign != 0 {
		return a >= b
	}
	return a <= b
}

// LtF64 returns true if a < b (signaling comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:876-896 (lt_sf)
func LtF64(a, b uint64, flags *ExceptionFlags) bool {
	if IsNaN64(a) || IsNaN64(b) {
		*flags |= FlagInvalidOp
		return false
	}

	aSign := uint32(a >> (Float64Bits - 1))
	bSign := uint32(b >> (Float64Bits - 1))

	if aSign != bSign {
		// a < b if a is negative AND not both zero
		return aSign != 0 && ((a|b)<<1) != 0
	}

	// Same sign
	if aSign != 0 {
		return a > b
	}
	return a < b
}

// Sign injection operations

// SignF64 returns a value with the magnitude of a and the sign of b (FSGNJ.D).
func SignF64(a, b uint64) uint64 {
	return (a & ^Float64SignMask) | (b & Float64SignMask)
}

// SignNF64 returns a value with the magnitude of a and the negated sign of b (FSGNJN.D).
func SignNF64(a, b uint64) uint64 {
	return (a & ^Float64SignMask) | ((^b) & Float64SignMask)
}

// SignXF64 returns a value with the magnitude of a and sign XOR of a and b (FSGNJX.D).
func SignXF64(a, b uint64) uint64 {
	return a ^ (b & Float64SignMask)
}
