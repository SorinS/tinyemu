// Package softfp F32 operations
// This file implements 32-bit floating-point operations.
// Reference: softfp_template.h from TinyEMU 2019-12-21

package softfp

import "math/bits"

// AddF32 adds two 32-bit floats with the given rounding mode.
// Reference: add_sf in softfp_template.h
func AddF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	// Swap so that abs(a) >= abs(b)
	if (a & ^Float32SignMask) < (b & ^Float32SignMask) {
		a, b = b, a
	}

	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)
	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	bExp := int32((b >> Float32MantBits) & Float32ExpMask)
	aMant := (a & Float32MantMask) << 3
	bMant := (b & Float32MantMask) << 3

	// Handle special cases for a (which has larger absolute value)
	if aExp == Float32ExpMask {
		if aMant != 0 {
			// a is NaN - result is NaN
			if (aMant&(Float32QNaNMask<<3)) == 0 || IsSigNaN32(b) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		} else if bExp == Float32ExpMask && aSign != bSign {
			// inf + (-inf) = NaN
			*flags |= FlagInvalidOp
			return Float32QNaN
		} else {
			// a is infinity, result is infinity with same sign
			return a
		}
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant |= 1 << (Float32MantBits + 3)
	}

	if bExp == 0 {
		bExp = 1
	} else {
		bMant |= 1 << (Float32MantBits + 3)
	}

	// Align mantissas by shifting b right
	bMant = rshiftRnd32(bMant, int(aExp-bExp))

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

	aExp += int32(float32RndSize - 3)
	return normalizeFloat32(aSign, aExp, aMant, rm, flags)
}

// SubF32 subtracts two 32-bit floats with the given rounding mode.
// Reference: sub_sf in softfp_template.h
func SubF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return AddF32(a, b^Float32SignMask, rm, flags)
}

// mulU32 multiplies two uint32 values and returns high and low parts.
// Uses 64-bit multiplication (like C version with F_ULONG).
// Reference: tinyemu-2019-12-21/softfp_template.h:308-314 (mul_u with F_ULONG)
func mulU32(a, b uint32) (high, low uint32) {
	r := uint64(a) * uint64(b)
	return uint32(r >> 32), uint32(r)
}

// MulF32 multiplies two 32-bit floats with the given rounding mode.
// Reference: tinyemu-2019-12-21/softfp_template.h:349-399 (mul_sf)
func MulF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)
	rSign := aSign ^ bSign

	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	bExp := int32((b >> Float32MantBits) & Float32ExpMask)
	aMant := a & Float32MantMask
	bMant := b & Float32MantMask

	// Handle special cases
	if aExp == Float32ExpMask || bExp == Float32ExpMask {
		if IsNaN32(a) || IsNaN32(b) {
			if IsSigNaN32(a) || IsSigNaN32(b) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		}
		// One or both is infinity
		if (aExp == Float32ExpMask && bExp == 0 && bMant == 0) ||
			(bExp == Float32ExpMask && aExp == 0 && aMant == 0) {
			// inf * 0 = NaN
			*flags |= FlagInvalidOp
			return Float32QNaN
		}
		// inf * finite = inf
		return Pack32(rSign, Float32ExpMask, 0)
	}

	// Handle zero
	if aExp == 0 {
		if aMant == 0 {
			return Pack32(rSign, 0, 0) // zero
		}
		aExp, aMant = normalizeSubnormal32(aMant)
	} else {
		aMant |= 1 << Float32MantBits
	}

	if bExp == 0 {
		if bMant == 0 {
			return Pack32(rSign, 0, 0) // zero
		}
		bExp, bMant = normalizeSubnormal32(bMant)
	} else {
		bMant |= 1 << Float32MantBits
	}

	// Calculate result exponent
	rExp := aExp + bExp - (1 << (Float32ExpBits - 1)) + 2

	// Multiply mantissas
	rMantHigh, rMantLow := mulU32(aMant<<float32RndSize, bMant<<(float32RndSize+1))
	if rMantLow != 0 {
		rMantHigh |= 1
	}

	return normalizeFloat32(rSign, rExp, rMantHigh, rm, flags)
}

// divremU32 divides (ah << 32 | al) by b, returning quotient and remainder.
// Uses 64-bit division (like C version with F_ULONG).
// Reference: tinyemu-2019-12-21/softfp_template.h:556-562 (divrem_u with F_ULONG)
func divremU32(ah, al, b uint32) (quotient, remainder uint32) {
	a := (uint64(ah) << 32) | uint64(al)
	return uint32(a / uint64(b)), uint32(a % uint64(b))
}

// DivF32 divides two 32-bit floats with the given rounding mode.
// Reference: tinyemu-2019-12-21/softfp_template.h:589-653 (div_sf)
func DivF32(a, b uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)
	rSign := aSign ^ bSign

	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	bExp := int32((b >> Float32MantBits) & Float32ExpMask)
	aMant := a & Float32MantMask
	bMant := b & Float32MantMask

	// Handle NaN and infinity for a
	if aExp == Float32ExpMask {
		if aMant != 0 || IsNaN32(b) {
			if IsSigNaN32(a) || IsSigNaN32(b) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		} else if bExp == Float32ExpMask {
			// inf / inf = NaN
			*flags |= FlagInvalidOp
			return Float32QNaN
		}
		// inf / finite = inf
		return Pack32(rSign, Float32ExpMask, 0)
	}

	// Handle NaN and infinity for b
	if bExp == Float32ExpMask {
		if bMant != 0 {
			if IsSigNaN32(a) || IsSigNaN32(b) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		}
		// finite / inf = 0
		return Pack32(rSign, 0, 0)
	}

	// Handle division by zero
	if bExp == 0 && bMant == 0 {
		if aExp == 0 && aMant == 0 {
			// 0 / 0 = NaN
			*flags |= FlagInvalidOp
			return Float32QNaN
		}
		// finite / 0 = inf
		*flags |= FlagDivideZero
		return Pack32(rSign, Float32ExpMask, 0)
	}

	// Normalize b
	if bExp == 0 {
		bExp, bMant = normalizeSubnormal32(bMant)
	} else {
		bMant |= 1 << Float32MantBits
	}

	// Handle zero dividend
	if aExp == 0 {
		if aMant == 0 {
			return Pack32(rSign, 0, 0) // zero
		}
		aExp, aMant = normalizeSubnormal32(aMant)
	} else {
		aMant |= 1 << Float32MantBits
	}

	// Calculate result exponent
	rExp := aExp - bExp + (1 << (Float32ExpBits - 1)) - 1

	// Divide mantissas
	rMant, r := divremU32(aMant, 0, bMant<<2)
	if r != 0 {
		rMant |= 1
	}

	return normalizeFloat32(rSign, rExp, rMant, rm, flags)
}

// sqrtremU32 computes sqrt(ah << 32 | al) where the input < 2^30.
// Returns the square root and whether the result was inexact.
// Reference: tinyemu-2019-12-21/softfp_template.h:659-685 (sqrtrem_u with F_ULONG)
func sqrtremU32(ah, al uint32) (result uint32, inexact bool) {
	a := (uint64(ah) << 32) | uint64(al)

	if a == 0 {
		return 0, false
	}

	// 2^l >= a
	var l int
	if ah != 0 {
		l = 2*32 - bits.LeadingZeros32(ah-1)
	} else {
		l = 32 - bits.LeadingZeros32(al-1)
	}

	// Initial guess
	u := uint64(1) << ((l + 1) / 2)

	// Newton-Raphson iteration
	for {
		s := u
		u = (a/s + s) / 2
		if u >= s {
			// Converged
			inexact = (a - s*s) != 0
			return uint32(s), inexact
		}
	}
}

// SqrtF32 computes the square root of a 32-bit float.
// Reference: tinyemu-2019-12-21/softfp_template.h:720-767 (sqrt_sf)
func SqrtF32(a uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	aSign := a >> (Float32Bits - 1)
	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	aMant := a & Float32MantMask

	// Handle special cases
	if aExp == Float32ExpMask {
		if aMant != 0 {
			// sqrt(NaN) = NaN
			if IsSigNaN32(a) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		} else if aSign != 0 {
			// sqrt(-inf) = NaN
			*flags |= FlagInvalidOp
			return Float32QNaN
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
		return Float32QNaN
	}

	// Handle zero
	if aExp == 0 {
		if aMant == 0 {
			return Pack32(0, 0, 0) // zero
		}
		aExp, aMant = normalizeSubnormal32(aMant)
	} else {
		aMant |= 1 << Float32MantBits
	}

	// Adjust exponent
	aExp -= Float32ExpMask / 2

	// Make exponent even for easier square root
	if (aExp & 1) != 0 {
		aExp--
		aMant <<= 1
	}

	aExp = (aExp >> 1) + Float32ExpMask/2

	// Shift mantissa for precision
	aMant <<= (Float32Bits - 4 - Float32MantBits)

	// Compute square root
	result, inexact := sqrtremU32(aMant, 0)
	if inexact {
		result |= 1
	}

	return normalizeFloat32(aSign, aExp, result, rm, flags)
}

// fmaF32MulZero handles the case when one of the multiplicands is zero in FMA.
func fmaF32MulZero(c uint32, rSign uint32, rm RoundingMode) uint32 {
	cSign := c >> (Float32Bits - 1)
	cExp := int32((c >> Float32MantBits) & Float32ExpMask)
	cMant := c & Float32MantMask

	if cExp == 0 && cMant == 0 {
		if cSign != rSign {
			if rm == RDN {
				rSign = 1
			} else {
				rSign = 0
			}
		}
		return Pack32(rSign, 0, 0)
	}
	return c
}

// FmaF32 performs fused multiply-add: (a * b) + c with a single rounding.
// Reference: tinyemu-2019-12-21/softfp_template.h:402-552 (fma_sf)
func FmaF32(a, b, c uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)
	cSign := c >> (Float32Bits - 1)
	rSign := aSign ^ bSign

	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	bExp := int32((b >> Float32MantBits) & Float32ExpMask)
	cExp := int32((c >> Float32MantBits) & Float32ExpMask)
	aMant := a & Float32MantMask
	bMant := b & Float32MantMask
	cMant := c & Float32MantMask

	// Handle special cases
	if aExp == Float32ExpMask || bExp == Float32ExpMask || cExp == Float32ExpMask {
		if IsNaN32(a) || IsNaN32(b) || IsNaN32(c) {
			if IsSigNaN32(a) || IsSigNaN32(b) || IsSigNaN32(c) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		}
		// Infinities
		if (aExp == Float32ExpMask && bExp == 0 && bMant == 0) ||
			(bExp == Float32ExpMask && aExp == 0 && aMant == 0) ||
			((aExp == Float32ExpMask || bExp == Float32ExpMask) &&
				cExp == Float32ExpMask && rSign != cSign) {
			*flags |= FlagInvalidOp
			return Float32QNaN
		} else if cExp == Float32ExpMask {
			return Pack32(cSign, Float32ExpMask, 0)
		} else {
			return Pack32(rSign, Float32ExpMask, 0)
		}
	}

	// Handle zero multiplicand
	if aExp == 0 && aMant == 0 {
		return fmaF32MulZero(c, rSign, rm)
	}
	if aExp == 0 {
		aExp, aMant = normalizeSubnormal32(aMant)
	} else {
		aMant |= 1 << Float32MantBits
	}

	if bExp == 0 && bMant == 0 {
		return fmaF32MulZero(c, rSign, rm)
	}
	if bExp == 0 {
		bExp, bMant = normalizeSubnormal32(bMant)
	} else {
		bMant |= 1 << Float32MantBits
	}

	// Multiply
	rExp := aExp + bExp - (1 << (Float32ExpBits - 1)) + 3
	rMant1, rMant0 := mulU32(aMant<<float32RndSize, bMant<<float32RndSize)

	// Normalize to F_SIZE - 3
	if rMant1 < (1 << (Float32Bits - 3)) {
		rMant1 = (rMant1 << 1) | (rMant0 >> (Float32Bits - 1))
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
			return normalizeFloat32(rSign, rExp, rMant1, rm, flags)
		}
		cExp, cMant = normalizeSubnormal32(cMant)
	} else {
		cMant |= 1 << Float32MantBits
	}
	cExp++
	cMant1 := cMant << (float32RndSize - 1)
	cMant0 := uint32(0)

	// Ensure abs(r) >= abs(c)
	if !(rExp > cExp || (rExp == cExp && rMant1 >= cMant1)) {
		rMant1, cMant1 = cMant1, rMant1
		rMant0, cMant0 = cMant0, rMant0
		rExp, cExp = cExp, rExp
		rSign, cSign = cSign, rSign
	}

	// Right shift c_mant
	shift := rExp - cExp
	if shift >= 2*Float32Bits {
		if cMant0 != 0 || cMant1 != 0 {
			cMant0 = 1
		} else {
			cMant0 = 0
		}
		cMant1 = 0
	} else if shift >= Float32Bits+1 {
		cMant0 = rshiftRnd32(cMant1, int(shift-Float32Bits))
		cMant1 = 0
	} else if shift == Float32Bits {
		if cMant0 != 0 {
			cMant0 = cMant1 | 1
		} else {
			cMant0 = cMant1
		}
		cMant1 = 0
	} else if shift != 0 {
		mask := (uint32(1) << shift) - 1
		var sticky uint32
		if (cMant0 & mask) != 0 {
			sticky = 1
		}
		cMant0 = (cMant1 << (Float32Bits - shift)) | (cMant0 >> shift) | sticky
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

	return normalize2Float32(rSign, rExp, rMant1, rMant0, rm, flags)
}

// MinMaxType specifies the behavior for min/max with NaN operands.
type MinMaxType int

const (
	// MinMaxIEEE754_2008 follows IEEE 754-2008 behavior:
	// min(1, qNaN) -> 1, min(1, sNaN) -> qNaN (with Invalid flag)
	MinMaxIEEE754_2008 MinMaxType = iota
	// MinMaxProp propagates NaN: min(1, NaN) -> qNaN
	MinMaxProp
	// MinMaxIEEE754_201X follows IEEE 754-201x behavior:
	// min(1, qNaN) -> 1, min(1, sNaN) -> 1 (with Invalid flag)
	// This is what RISC-V uses.
	MinMaxIEEE754_201X
)

// minMaxNaN32 handles NaN cases for min/max operations.
// Reference: tinyemu-2019-12-21/softfp_template.h:771-790 (min_max_nan_sf)
func minMaxNaN32(a, b uint32, flags *ExceptionFlags, minMaxType MinMaxType) uint32 {
	if IsSigNaN32(a) || IsSigNaN32(b) {
		*flags |= FlagInvalidOp
		if minMaxType == MinMaxIEEE754_2008 {
			return Float32QNaN
		}
	}
	if minMaxType == MinMaxProp {
		return Float32QNaN
	}
	// Return the non-NaN operand if possible
	if IsNaN32(a) {
		if IsNaN32(b) {
			return Float32QNaN
		}
		return b
	}
	return a
}

// MinF32 returns the minimum of two 32-bit floats.
// Reference: tinyemu-2019-12-21/softfp_template.h:792-814 (min_sf)
func MinF32(a, b uint32, flags *ExceptionFlags, minMaxType MinMaxType) uint32 {
	if IsNaN32(a) || IsNaN32(b) {
		return minMaxNaN32(a, b, flags, minMaxType)
	}

	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)

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

// MaxF32 returns the maximum of two 32-bit floats.
// Reference: tinyemu-2019-12-21/softfp_template.h:816-838 (max_sf)
func MaxF32(a, b uint32, flags *ExceptionFlags, minMaxType MinMaxType) uint32 {
	if IsNaN32(a) || IsNaN32(b) {
		return minMaxNaN32(a, b, flags, minMaxType)
	}

	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)

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

// EqQuietF32 returns true if a == b (quiet comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:840-852 (eq_quiet_sf)
func EqQuietF32(a, b uint32, flags *ExceptionFlags) bool {
	if IsNaN32(a) || IsNaN32(b) {
		if IsSigNaN32(a) || IsSigNaN32(b) {
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

// LeF32 returns true if a <= b (signaling comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:854-874 (le_sf)
func LeF32(a, b uint32, flags *ExceptionFlags) bool {
	if IsNaN32(a) || IsNaN32(b) {
		*flags |= FlagInvalidOp
		return false
	}

	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)

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

// LtF32 returns true if a < b (signaling comparison).
// Reference: tinyemu-2019-12-21/softfp_template.h:876-896 (lt_sf)
func LtF32(a, b uint32, flags *ExceptionFlags) bool {
	if IsNaN32(a) || IsNaN32(b) {
		*flags |= FlagInvalidOp
		return false
	}

	aSign := a >> (Float32Bits - 1)
	bSign := b >> (Float32Bits - 1)

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

// SignF32 returns a value with the magnitude of a and the sign of b (FSGNJ.S).
func SignF32(a, b uint32) uint32 {
	return (a & ^Float32SignMask) | (b & Float32SignMask)
}

// SignNF32 returns a value with the magnitude of a and the negated sign of b (FSGNJN.S).
func SignNF32(a, b uint32) uint32 {
	return (a & ^Float32SignMask) | ((^b) & Float32SignMask)
}

// SignXF32 returns a value with the magnitude of a and sign XOR of a and b (FSGNJX.S).
func SignXF32(a, b uint32) uint32 {
	return a ^ (b & Float32SignMask)
}
