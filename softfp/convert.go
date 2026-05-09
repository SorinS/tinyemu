// Package softfp conversion operations
// This file implements conversions between floats and integers, and between float sizes.
// Reference: softfp_template_icvt.h and softfp_template.h from TinyEMU 2019-12-21

package softfp

import (
	"math/bits"

	"lukechampine.com/uint128"
)

// ============================================================================
// Float32 to Integer Conversions
// ============================================================================

// cvtF32ToInternal converts F32 to integer with configurable signedness.
func cvtF32ToInternal(a uint32, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool, intSize int) uint64 {
	aSign := a >> (Float32Bits - 1)
	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	aMant := uint64(a & Float32MantMask)

	// NaN is treated like +infinity
	if aExp == Float32ExpMask && aMant != 0 {
		aSign = 0
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant |= 1 << Float32MantBits
	}

	aMant <<= float32RndSize
	aExp = aExp - (Float32ExpMask / 2) - Float32MantBits

	// Determine maximum value based on signedness
	var rMax uint64
	if isUnsigned {
		if intSize == 32 {
			if aSign != 0 {
				rMax = 0
			} else {
				rMax = 0xFFFFFFFF
			}
		} else {
			if aSign != 0 {
				rMax = 0
			} else {
				rMax = 0xFFFFFFFFFFFFFFFF
			}
		}
	} else {
		if intSize == 32 {
			if aSign != 0 {
				rMax = 0x80000000 // -2^31
			} else {
				rMax = 0x7FFFFFFF // 2^31 - 1
			}
		} else {
			if aSign != 0 {
				rMax = 0x8000000000000000 // -2^63
			} else {
				rMax = 0x7FFFFFFFFFFFFFFF // 2^63 - 1
			}
		}
	}

	var r uint64
	if aExp >= 0 {
		maxShift := intSize - 1 - Float32MantBits
		if aExp <= int32(maxShift) {
			r = (aMant >> float32RndSize) << aExp
			if r > rMax {
				*flags |= FlagInvalidOp
				return rMax
			}
		} else {
			// Overflow
			*flags |= FlagInvalidOp
			return rMax
		}
	} else {
		// Shift right and round
		aMant = rshiftRnd64(aMant, int(-aExp))

		var addend uint64
		switch rm {
		case RNE, RMM:
			addend = 1 << (float32RndSize - 1)
		case RTZ:
			addend = 0
		case RDN, RUP:
			if (aSign != 0) != (rm == RUP) {
				addend = (1 << float32RndSize) - 1
			} else {
				addend = 0
			}
		}

		rndBits := aMant & ((1 << float32RndSize) - 1)
		aMant = (aMant + addend) >> float32RndSize

		// Half way: select even result
		if rm == RNE && rndBits == (1<<(float32RndSize-1)) {
			aMant &= ^uint64(1)
		}

		if aMant > rMax {
			*flags |= FlagInvalidOp
			return rMax
		}

		r = aMant
		if rndBits != 0 {
			*flags |= FlagInexact
		}
	}

	if aSign != 0 {
		r = -r
	}
	return r
}

// CvtF32ToI32 converts F32 to signed 32-bit integer (FCVT.W.S).
func CvtF32ToI32(a uint32, rm RoundingMode, flags *ExceptionFlags) int32 {
	return int32(cvtF32ToInternal(a, rm, flags, false, 32))
}

// CvtF32ToU32 converts F32 to unsigned 32-bit integer (FCVT.WU.S).
func CvtF32ToU32(a uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return uint32(cvtF32ToInternal(a, rm, flags, true, 32))
}

// CvtF32ToI64 converts F32 to signed 64-bit integer (FCVT.L.S).
func CvtF32ToI64(a uint32, rm RoundingMode, flags *ExceptionFlags) int64 {
	return int64(cvtF32ToInternal(a, rm, flags, false, 64))
}

// CvtF32ToU64 converts F32 to unsigned 64-bit integer (FCVT.LU.S).
func CvtF32ToU64(a uint32, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtF32ToInternal(a, rm, flags, true, 64)
}

// ============================================================================
// Float64 to Integer Conversions
// ============================================================================

// cvtF64ToInternal converts F64 to integer with configurable signedness.
func cvtF64ToInternal(a uint64, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool, intSize int) uint64 {
	aSign := uint32(a >> (Float64Bits - 1))
	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	aMant := a & Float64MantMask

	// NaN is treated like +infinity
	if aExp == Float64ExpMask && aMant != 0 {
		aSign = 0
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant |= 1 << Float64MantBits
	}

	aMant <<= float64RndSize
	aExp = aExp - (Float64ExpMask / 2) - Float64MantBits

	// Determine maximum value based on signedness
	var rMax uint64
	if isUnsigned {
		if intSize == 32 {
			if aSign != 0 {
				rMax = 0
			} else {
				rMax = 0xFFFFFFFF
			}
		} else {
			if aSign != 0 {
				rMax = 0
			} else {
				rMax = 0xFFFFFFFFFFFFFFFF
			}
		}
	} else {
		if intSize == 32 {
			if aSign != 0 {
				rMax = 0x80000000 // -2^31
			} else {
				rMax = 0x7FFFFFFF // 2^31 - 1
			}
		} else {
			if aSign != 0 {
				rMax = 0x8000000000000000 // -2^63
			} else {
				rMax = 0x7FFFFFFFFFFFFFFF // 2^63 - 1
			}
		}
	}

	var r uint64
	if aExp >= 0 {
		maxShift := intSize - 1 - Float64MantBits
		if aExp <= int32(maxShift) {
			r = (aMant >> float64RndSize) << aExp
			if r > rMax {
				*flags |= FlagInvalidOp
				return rMax
			}
		} else {
			// Overflow
			*flags |= FlagInvalidOp
			return rMax
		}
	} else {
		// Shift right and round
		aMant = rshiftRnd64(aMant, int(-aExp))

		var addend uint64
		switch rm {
		case RNE, RMM:
			addend = 1 << (float64RndSize - 1)
		case RTZ:
			addend = 0
		case RDN, RUP:
			if (aSign != 0) != (rm == RUP) {
				addend = (1 << float64RndSize) - 1
			} else {
				addend = 0
			}
		}

		rndBits := aMant & ((1 << float64RndSize) - 1)
		aMant = (aMant + addend) >> float64RndSize

		// Half way: select even result
		if rm == RNE && rndBits == (1<<(float64RndSize-1)) {
			aMant &= ^uint64(1)
		}

		if aMant > rMax {
			*flags |= FlagInvalidOp
			return rMax
		}

		r = aMant
		if rndBits != 0 {
			*flags |= FlagInexact
		}
	}

	if aSign != 0 {
		r = -r
	}
	return r
}

// CvtF64ToI32 converts F64 to signed 32-bit integer (FCVT.W.D).
func CvtF64ToI32(a uint64, rm RoundingMode, flags *ExceptionFlags) int32 {
	return int32(cvtF64ToInternal(a, rm, flags, false, 32))
}

// CvtF64ToU32 converts F64 to unsigned 32-bit integer (FCVT.WU.D).
func CvtF64ToU32(a uint64, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return uint32(cvtF64ToInternal(a, rm, flags, true, 32))
}

// CvtF64ToI64 converts F64 to signed 64-bit integer (FCVT.L.D).
func CvtF64ToI64(a uint64, rm RoundingMode, flags *ExceptionFlags) int64 {
	return int64(cvtF64ToInternal(a, rm, flags, false, 64))
}

// CvtF64ToU64 converts F64 to unsigned 64-bit integer (FCVT.LU.D).
func CvtF64ToU64(a uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtF64ToInternal(a, rm, flags, true, 64)
}

// ============================================================================
// Integer to Float32 Conversions
// ============================================================================

// cvtIntToF32Internal converts integer to F32 with configurable signedness.
func cvtIntToF32Internal(a uint64, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool, intSize int) uint32 {
	var aSign uint32
	var r uint64

	if !isUnsigned {
		// Handle signed conversion
		if intSize == 32 {
			signedA := int32(a)
			if signedA < 0 {
				aSign = 1
				r = uint64(uint32(-signedA))
			} else {
				aSign = 0
				r = uint64(uint32(signedA))
			}
		} else {
			signedA := int64(a)
			if signedA < 0 {
				aSign = 1
				r = uint64(-signedA)
			} else {
				aSign = 0
				r = uint64(signedA)
			}
		}
	} else {
		aSign = 0
		if intSize == 32 {
			r = uint64(uint32(a))
		} else {
			r = a
		}
	}

	if r == 0 {
		return Pack32(aSign, 0, 0)
	}

	aExp := int32((Float32ExpMask / 2) + Float32Bits - 2)

	// Need to reduce range before generic float normalization
	// Use appropriate leading zero count based on intSize
	var clz int
	if intSize == 32 {
		clz = bits.LeadingZeros32(uint32(r))
	} else {
		clz = bits.LeadingZeros64(r)
	}
	l := intSize - clz - (Float32Bits - 1)
	if l > 0 {
		mask := (uint64(1) << l) - 1
		sticky := r & mask
		r = (r >> l)
		if sticky != 0 {
			r |= 1
		}
		aExp += int32(l)
	}

	aMant := uint32(r)
	return normalizeFloat32(aSign, aExp, aMant, rm, flags)
}

// CvtI32ToF32 converts signed 32-bit integer to F32 (FCVT.S.W).
func CvtI32ToF32(a int32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return cvtIntToF32Internal(uint64(a), rm, flags, false, 32)
}

// CvtU32ToF32 converts unsigned 32-bit integer to F32 (FCVT.S.WU).
func CvtU32ToF32(a uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return cvtIntToF32Internal(uint64(a), rm, flags, true, 32)
}

// CvtI64ToF32 converts signed 64-bit integer to F32 (FCVT.S.L).
func CvtI64ToF32(a int64, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return cvtIntToF32Internal(uint64(a), rm, flags, false, 64)
}

// CvtU64ToF32 converts unsigned 64-bit integer to F32 (FCVT.S.LU).
func CvtU64ToF32(a uint64, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return cvtIntToF32Internal(a, rm, flags, true, 64)
}

// ============================================================================
// Integer to Float64 Conversions
// ============================================================================

// cvtIntToF64Internal converts integer to F64 with configurable signedness.
func cvtIntToF64Internal(a uint64, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool, intSize int) uint64 {
	var aSign uint32
	var r uint64

	if !isUnsigned {
		// Handle signed conversion
		if intSize == 32 {
			signedA := int32(a)
			if signedA < 0 {
				aSign = 1
				r = uint64(uint32(-signedA))
			} else {
				aSign = 0
				r = uint64(uint32(signedA))
			}
		} else {
			signedA := int64(a)
			if signedA < 0 {
				aSign = 1
				r = uint64(-signedA)
			} else {
				aSign = 0
				r = uint64(signedA)
			}
		}
	} else {
		aSign = 0
		if intSize == 32 {
			r = uint64(uint32(a))
		} else {
			r = a
		}
	}

	if r == 0 {
		return Pack64(aSign, 0, 0)
	}

	aExp := int32((Float64ExpMask / 2) + Float64Bits - 2)

	// Need to reduce range before generic float normalization
	// Use appropriate leading zero count based on intSize
	var clz int
	if intSize == 32 {
		clz = bits.LeadingZeros32(uint32(r))
	} else {
		clz = bits.LeadingZeros64(r)
	}
	l := intSize - clz - (Float64Bits - 1)
	if l > 0 {
		mask := (uint64(1) << l) - 1
		sticky := r & mask
		r = (r >> l)
		if sticky != 0 {
			r |= 1
		}
		aExp += int32(l)
	}

	aMant := r
	return normalizeFloat64(aSign, aExp, aMant, rm, flags)
}

// CvtI32ToF64 converts signed 32-bit integer to F64 (FCVT.D.W).
func CvtI32ToF64(a int32, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtIntToF64Internal(uint64(a), rm, flags, false, 32)
}

// CvtU32ToF64 converts unsigned 32-bit integer to F64 (FCVT.D.WU).
func CvtU32ToF64(a uint32, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtIntToF64Internal(uint64(a), rm, flags, true, 32)
}

// CvtI64ToF64 converts signed 64-bit integer to F64 (FCVT.D.L).
func CvtI64ToF64(a int64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtIntToF64Internal(uint64(a), rm, flags, false, 64)
}

// CvtU64ToF64 converts unsigned 64-bit integer to F64 (FCVT.D.LU).
func CvtU64ToF64(a uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtIntToF64Internal(a, rm, flags, true, 64)
}

// ============================================================================
// Float to Float Conversions
// ============================================================================

// CvtF32ToF64 converts F32 to F64 (FCVT.D.S).
// Reference: tinyemu-2019-12-21/softfp_template.h:945-976 (cvt_sf32_sf)
func CvtF32ToF64(a uint32, flags *ExceptionFlags) uint64 {
	aSign, aExp, aMant := Unpack32(a)

	if aExp == Float32ExpMask {
		if aMant != 0 {
			// NaN
			if IsSigNaN32(a) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		}
		// Infinity
		return Pack64(aSign, Float64ExpMask, 0)
	}

	if aExp == 0 {
		if aMant == 0 {
			// Zero
			return Pack64(aSign, 0, 0)
		}
		// Subnormal - normalize it
		aExp, aMant = normalizeSubnormal32(aMant)
	}

	// Convert exponent: bias32=127, bias64=1023
	newExp := uint32(aExp - Float32ExpBias + Float64ExpBias)

	// Shift mantissa: 23 bits -> 52 bits
	newMant := uint64(aMant) << (Float64MantBits - Float32MantBits)

	return Pack64(aSign, newExp, newMant)
}

// CvtF64ToF32 converts F64 to F32 (FCVT.S.D).
// Reference: tinyemu-2019-12-21/softfp_template.h:978-1010 (cvt_sf64_sf32)
func CvtF64ToF32(a uint64, rm RoundingMode, flags *ExceptionFlags) uint32 {
	aSign, aExp, aMant := Unpack64(a)

	if aExp == Float64ExpMask {
		if aMant != 0 {
			// NaN
			if IsSigNaN64(a) {
				*flags |= FlagInvalidOp
			}
			return Float32QNaN
		}
		// Infinity
		return Pack32(aSign, Float32ExpMask, 0)
	}

	if aExp == 0 {
		if aMant == 0 {
			// Zero
			return Pack32(aSign, 0, 0)
		}
		// Subnormal - normalize it
		aExp, aMant = normalizeSubnormal64(aMant)
	} else {
		// Add implicit bit
		aMant |= 1 << Float64MantBits
	}

	// Convert exponent: bias64=1023 -> bias32=127
	newExp := aExp - Float64ExpBias + Float32ExpBias

	// Shift mantissa from 52 bits to internal 32-bit representation
	// We need to shift to position with MSB at bit 30 (Float32Bits - 2)
	// aMant has implicit bit at position 52, so total is 53 significant bits
	// Need to fit into 31 bits (positions 0-30)
	// Shift right by (53 - 31) = 22 bits, but preserve rounding info
	shiftAmount := Float64MantBits - (Float32Bits - 2)
	aMant32 := rshiftRnd64(aMant, int(shiftAmount))

	return roundPackFloat32(aSign, newExp, uint32(aMant32), rm, flags)
}

// CvtF64ToF128 converts F64 to F128 (FCVT.Q.D).
// Reference: tinyemu-2019-12-21/softfp_template.h:1016-1046 (cvt_sf64_sf)
func CvtF64ToF128(a uint64, flags *ExceptionFlags) uint128.Uint128 {
	aSign, aExp, aMant := Unpack64(a)

	if aExp == Float64ExpMask {
		if aMant != 0 {
			// NaN
			if IsSigNaN64(a) {
				*flags |= FlagInvalidOp
			}
			return Float128QNaN
		}
		// Infinity
		return Pack128(aSign, Float128ExpMask, uint128.Zero)
	}

	if aExp == 0 {
		if aMant == 0 {
			// Zero
			return Pack128(aSign, 0, uint128.Zero)
		}
		// Subnormal - normalize it
		aExp, aMant = normalizeSubnormal64(aMant)
	}

	// Convert exponent: bias64=1023 -> bias128=16383
	newExp := uint32(aExp - Float64ExpBias + Float128ExpBias)

	// Shift mantissa: 52 bits -> 112 bits (shift left by 60)
	newMant := lshift128(uint128.From64(aMant), Float128MantBits-Float64MantBits)

	return Pack128(aSign, newExp, newMant)
}

// CvtF128ToF64 converts F128 to F64 (FCVT.D.Q).
// Reference: tinyemu-2019-12-21/softfp_template.h:1048-1080 (cvt_sf128_sf64)
func CvtF128ToF64(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint64 {
	aSign, aExp, aMant := Unpack128(a)

	if aExp == int32(Float128ExpMask) {
		if !aMant.IsZero() {
			// NaN
			if IsSigNaN128(a) {
				*flags |= FlagInvalidOp
			}
			return Float64QNaN
		}
		// Infinity
		return Pack64(uint32(aSign), Float64ExpMask, 0)
	}

	if aExp == 0 {
		if aMant.IsZero() {
			// Zero
			return Pack64(uint32(aSign), 0, 0)
		}
		// Subnormal - normalize it
		aExp, aMant = normalizeSubnormal128(aMant)
	} else {
		// Add implicit bit
		aMant = aMant.Or(lshift128(uint128.From64(1), Float128MantBits))
	}

	// Convert exponent: bias128=16383 -> bias64=1023
	newExp := aExp - Float128ExpBias + Float64ExpBias

	// Shift mantissa from 112 bits to internal 64-bit representation
	// aMant has implicit bit at position 112, so total is 113 significant bits
	// Need to fit into 63 bits (positions 0-62)
	// Use rshift_rnd to preserve rounding info
	shiftAmount := Float128MantBits - (Float64Bits - 2)
	aMant64 := rshiftRnd128ToU64(aMant, shiftAmount)

	return roundPackFloat64(uint32(aSign), newExp, aMant64, rm, flags)
}

// ============================================================================
// Float32 to 128-bit Integer Conversions
// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:38-108 (with ICVT_SIZE=128)
// ============================================================================

// cvtF32ToI128Internal converts F32 to 128-bit integer with configurable signedness.
func cvtF32ToI128Internal(a uint32, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool) uint128.Uint128 {
	aSign := a >> (Float32Bits - 1)
	aExp := int32((a >> Float32MantBits) & Float32ExpMask)
	aMant := uint128.From64(uint64(a & Float32MantMask))

	// NaN is treated like +infinity
	if aExp == Float32ExpMask && !aMant.IsZero() {
		aSign = 0
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant = aMant.Or(uint128.From64(1 << Float32MantBits))
	}

	aMant = lshift128(aMant, float32RndSize)
	aExp = aExp - (Float32ExpMask / 2) - Float32MantBits

	// Determine maximum value based on signedness
	var rMax uint128.Uint128
	if isUnsigned {
		if aSign != 0 {
			rMax = uint128.Zero
		} else {
			rMax = uint128.Max
		}
	} else {
		if aSign != 0 {
			// -2^127
			rMax = uint128.New(0, 0x8000000000000000)
		} else {
			// 2^127 - 1
			rMax = uint128.New(0xFFFFFFFFFFFFFFFF, 0x7FFFFFFFFFFFFFFF)
		}
	}

	var r uint128.Uint128
	if aExp >= 0 {
		maxShift := 128 - 1 - Float32MantBits
		if aExp <= int32(maxShift) {
			r = lshift128(rshift128(aMant, float32RndSize), int(aExp))
			if r.Cmp(rMax) > 0 {
				*flags |= FlagInvalidOp
				return rMax
			}
		} else {
			// Overflow
			*flags |= FlagInvalidOp
			return rMax
		}
	} else {
		// Shift right and round
		aMant = rshiftRnd128(aMant, int(-aExp))

		var addend uint128.Uint128
		rndMask := lshift128(uint128.From64(1), float32RndSize).Sub(uint128.From64(1))
		switch rm {
		case RNE, RMM:
			addend = lshift128(uint128.From64(1), float32RndSize-1)
		case RTZ:
			addend = uint128.Zero
		case RDN, RUP:
			if (aSign != 0) != (rm == RUP) {
				addend = rndMask
			} else {
				addend = uint128.Zero
			}
		}

		rndBits := aMant.And(rndMask)
		aMant = rshift128(aMant.Add(addend), float32RndSize)

		// Half way: select even result
		halfWay := lshift128(uint128.From64(1), float32RndSize-1)
		if rm == RNE && rndBits.Equals(halfWay) {
			aMant = aMant.And(not128(uint128.From64(1)))
		}

		if aMant.Cmp(rMax) > 0 {
			*flags |= FlagInvalidOp
			return rMax
		}

		r = aMant
		if !rndBits.IsZero() {
			*flags |= FlagInexact
		}
	}

	if aSign != 0 {
		r = uint128.Zero.Sub(r) // negate
	}
	return r
}

// CvtF32ToI128 converts F32 to signed 128-bit integer.
func CvtF32ToI128(a uint32, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtF32ToI128Internal(a, rm, flags, false)
}

// CvtF32ToU128 converts F32 to unsigned 128-bit integer.
func CvtF32ToU128(a uint32, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtF32ToI128Internal(a, rm, flags, true)
}

// ============================================================================
// Float64 to 128-bit Integer Conversions
// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:38-108 (with ICVT_SIZE=128)
// ============================================================================

// cvtF64ToI128Internal converts F64 to 128-bit integer with configurable signedness.
func cvtF64ToI128Internal(a uint64, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool) uint128.Uint128 {
	aSign := uint32(a >> (Float64Bits - 1))
	aExp := int32((a >> Float64MantBits) & Float64ExpMask)
	aMant := uint128.From64(a & Float64MantMask)

	// NaN is treated like +infinity
	if aExp == Float64ExpMask && !aMant.IsZero() {
		aSign = 0
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant = aMant.Or(uint128.From64(1 << Float64MantBits))
	}

	aMant = lshift128(aMant, float64RndSize)
	aExp = aExp - (Float64ExpMask / 2) - Float64MantBits

	// Determine maximum value based on signedness
	var rMax uint128.Uint128
	if isUnsigned {
		if aSign != 0 {
			rMax = uint128.Zero
		} else {
			rMax = uint128.Max
		}
	} else {
		if aSign != 0 {
			// -2^127
			rMax = uint128.New(0, 0x8000000000000000)
		} else {
			// 2^127 - 1
			rMax = uint128.New(0xFFFFFFFFFFFFFFFF, 0x7FFFFFFFFFFFFFFF)
		}
	}

	var r uint128.Uint128
	if aExp >= 0 {
		maxShift := 128 - 1 - Float64MantBits
		if aExp <= int32(maxShift) {
			r = lshift128(rshift128(aMant, float64RndSize), int(aExp))
			if r.Cmp(rMax) > 0 {
				*flags |= FlagInvalidOp
				return rMax
			}
		} else {
			// Overflow
			*flags |= FlagInvalidOp
			return rMax
		}
	} else {
		// Shift right and round
		aMant = rshiftRnd128(aMant, int(-aExp))

		var addend uint128.Uint128
		rndMask := lshift128(uint128.From64(1), float64RndSize).Sub(uint128.From64(1))
		switch rm {
		case RNE, RMM:
			addend = lshift128(uint128.From64(1), float64RndSize-1)
		case RTZ:
			addend = uint128.Zero
		case RDN, RUP:
			if (aSign != 0) != (rm == RUP) {
				addend = rndMask
			} else {
				addend = uint128.Zero
			}
		}

		rndBits := aMant.And(rndMask)
		aMant = rshift128(aMant.Add(addend), float64RndSize)

		// Half way: select even result
		halfWay := lshift128(uint128.From64(1), float64RndSize-1)
		if rm == RNE && rndBits.Equals(halfWay) {
			aMant = aMant.And(not128(uint128.From64(1)))
		}

		if aMant.Cmp(rMax) > 0 {
			*flags |= FlagInvalidOp
			return rMax
		}

		r = aMant
		if !rndBits.IsZero() {
			*flags |= FlagInexact
		}
	}

	if aSign != 0 {
		r = uint128.Zero.Sub(r) // negate
	}
	return r
}

// CvtF64ToI128 converts F64 to signed 128-bit integer.
func CvtF64ToI128(a uint64, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtF64ToI128Internal(a, rm, flags, false)
}

// CvtF64ToU128 converts F64 to unsigned 128-bit integer.
func CvtF64ToU128(a uint64, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtF64ToI128Internal(a, rm, flags, true)
}

// ============================================================================
// Float128 to 128-bit Integer Conversions
// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:38-108 (with ICVT_SIZE=128, F_SIZE=128)
// ============================================================================

// cvtF128ToI128Internal converts F128 to 128-bit integer with configurable signedness.
func cvtF128ToI128Internal(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool) uint128.Uint128 {
	aSign, aExp, aMant := Unpack128(a)

	// NaN is treated like +infinity
	if aExp == Float128ExpMask && !aMant.IsZero() {
		aSign = 0
	}

	// Add implicit bit for normal numbers
	if aExp == 0 {
		aExp = 1
	} else {
		aMant = aMant.Or(lshift128(uint128.From64(1), Float128MantBits))
	}

	aMant = lshift128(aMant, float128RndSize)
	aExp = aExp - (Float128ExpMask / 2) - Float128MantBits

	// Determine maximum value based on signedness
	var rMax uint128.Uint128
	if isUnsigned {
		if aSign != 0 {
			rMax = uint128.Zero
		} else {
			rMax = uint128.Max
		}
	} else {
		if aSign != 0 {
			// -2^127
			rMax = uint128.New(0, 0x8000000000000000)
		} else {
			// 2^127 - 1
			rMax = uint128.New(0xFFFFFFFFFFFFFFFF, 0x7FFFFFFFFFFFFFFF)
		}
	}

	var r uint128.Uint128
	if aExp >= 0 {
		maxShift := 128 - 1 - Float128MantBits
		if aExp <= int32(maxShift) {
			r = lshift128(rshift128(aMant, float128RndSize), int(aExp))
			if r.Cmp(rMax) > 0 {
				*flags |= FlagInvalidOp
				return rMax
			}
		} else {
			// Overflow
			*flags |= FlagInvalidOp
			return rMax
		}
	} else {
		// Shift right and round
		aMant = rshiftRnd128(aMant, int(-aExp))

		var addend uint128.Uint128
		rndMask := lshift128(uint128.From64(1), float128RndSize).Sub(uint128.From64(1))
		switch rm {
		case RNE, RMM:
			addend = lshift128(uint128.From64(1), float128RndSize-1)
		case RTZ:
			addend = uint128.Zero
		case RDN, RUP:
			if (aSign != 0) != (rm == RUP) {
				addend = rndMask
			} else {
				addend = uint128.Zero
			}
		}

		rndBits := aMant.And(rndMask)
		aMant = rshift128(aMant.Add(addend), float128RndSize)

		// Half way: select even result
		halfWay := lshift128(uint128.From64(1), float128RndSize-1)
		if rm == RNE && rndBits.Equals(halfWay) {
			aMant = aMant.And(not128(uint128.From64(1)))
		}

		if aMant.Cmp(rMax) > 0 {
			*flags |= FlagInvalidOp
			return rMax
		}

		r = aMant
		if !rndBits.IsZero() {
			*flags |= FlagInexact
		}
	}

	if aSign != 0 {
		r = uint128.Zero.Sub(r) // negate
	}
	return r
}

// CvtF128ToI128 converts F128 to signed 128-bit integer.
func CvtF128ToI128(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtF128ToI128Internal(a, rm, flags, false)
}

// CvtF128ToU128 converts F128 to unsigned 128-bit integer.
func CvtF128ToU128(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtF128ToI128Internal(a, rm, flags, true)
}

// ============================================================================
// 128-bit Integer to Float32 Conversions
// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:125-153 (with ICVT_SIZE=128)
// ============================================================================

// cvtI128ToF32Internal converts 128-bit integer to F32 with configurable signedness.
func cvtI128ToF32Internal(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool) uint32 {
	var aSign uint32
	var r uint128.Uint128

	if !isUnsigned {
		// Check sign bit (bit 127)
		if a.Hi>>63 != 0 {
			aSign = 1
			r = uint128.Zero.Sub(a) // negate
		} else {
			aSign = 0
			r = a
		}
	} else {
		aSign = 0
		r = a
	}

	if r.IsZero() {
		return Pack32(aSign, 0, 0)
	}

	aExp := int32((Float32ExpMask / 2) + Float32Bits - 2)

	// Need to reduce range before generic float normalization
	clz := clz128(r)
	l := 128 - clz - (Float32Bits - 1)
	if l > 0 {
		mask := lshift128(uint128.From64(1), l).Sub(uint128.From64(1))
		sticky := r.And(mask)
		r = rshift128(r, l)
		if !sticky.IsZero() {
			r = r.Or(uint128.From64(1))
		}
		aExp += int32(l)
	}

	aMant := uint32(r.Lo)
	return normalizeFloat32(aSign, aExp, aMant, rm, flags)
}

// CvtI128ToF32 converts signed 128-bit integer to F32.
func CvtI128ToF32(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return cvtI128ToF32Internal(a, rm, flags, false)
}

// CvtU128ToF32 converts unsigned 128-bit integer to F32.
func CvtU128ToF32(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint32 {
	return cvtI128ToF32Internal(a, rm, flags, true)
}

// ============================================================================
// 128-bit Integer to Float64 Conversions
// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:125-153 (with ICVT_SIZE=128)
// ============================================================================

// cvtI128ToF64Internal converts 128-bit integer to F64 with configurable signedness.
func cvtI128ToF64Internal(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool) uint64 {
	var aSign uint32
	var r uint128.Uint128

	if !isUnsigned {
		// Check sign bit (bit 127)
		if a.Hi>>63 != 0 {
			aSign = 1
			r = uint128.Zero.Sub(a) // negate
		} else {
			aSign = 0
			r = a
		}
	} else {
		aSign = 0
		r = a
	}

	if r.IsZero() {
		return Pack64(aSign, 0, 0)
	}

	aExp := int32((Float64ExpMask / 2) + Float64Bits - 2)

	// Need to reduce range before generic float normalization
	clz := clz128(r)
	l := 128 - clz - (Float64Bits - 1)
	if l > 0 {
		mask := lshift128(uint128.From64(1), l).Sub(uint128.From64(1))
		sticky := r.And(mask)
		r = rshift128(r, l)
		if !sticky.IsZero() {
			r = r.Or(uint128.From64(1))
		}
		aExp += int32(l)
	}

	aMant := r.Lo
	return normalizeFloat64(aSign, aExp, aMant, rm, flags)
}

// CvtI128ToF64 converts signed 128-bit integer to F64.
func CvtI128ToF64(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtI128ToF64Internal(a, rm, flags, false)
}

// CvtU128ToF64 converts unsigned 128-bit integer to F64.
func CvtU128ToF64(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint64 {
	return cvtI128ToF64Internal(a, rm, flags, true)
}

// ============================================================================
// 128-bit Integer to Float128 Conversions
// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:125-153 (with ICVT_SIZE=128, F_SIZE=128)
// ============================================================================

// cvtI128ToF128Internal converts 128-bit integer to F128 with configurable signedness.
func cvtI128ToF128Internal(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags, isUnsigned bool) uint128.Uint128 {
	var aSign uint32
	var r uint128.Uint128

	if !isUnsigned {
		// Check sign bit (bit 127)
		if a.Hi>>63 != 0 {
			aSign = 1
			r = uint128.Zero.Sub(a) // negate
		} else {
			aSign = 0
			r = a
		}
	} else {
		aSign = 0
		r = a
	}

	if r.IsZero() {
		return Pack128(aSign, 0, uint128.Zero)
	}

	aExp := int32((Float128ExpMask / 2) + Float128Bits - 2)

	// Need to reduce range before generic float normalization
	clz := clz128(r)
	l := 128 - clz - (Float128Bits - 1)
	if l > 0 {
		mask := lshift128(uint128.From64(1), l).Sub(uint128.From64(1))
		sticky := r.And(mask)
		r = rshift128(r, l)
		if !sticky.IsZero() {
			r = r.Or(uint128.From64(1))
		}
		aExp += int32(l)
	}

	return normalizeFloat128(aSign, aExp, r, rm, flags)
}

// CvtI128ToF128 converts signed 128-bit integer to F128.
func CvtI128ToF128(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtI128ToF128Internal(a, rm, flags, false)
}

// CvtU128ToF128 converts unsigned 128-bit integer to F128.
func CvtU128ToF128(a uint128.Uint128, rm RoundingMode, flags *ExceptionFlags) uint128.Uint128 {
	return cvtI128ToF128Internal(a, rm, flags, true)
}
