package softfp

import (
	"math/bits"

	"lukechampine.com/uint128"
)

// clz32 counts leading zeros in a 32-bit value.
// Reference: tinyemu-2019-12-21/softfp.c:32-41
func clz32(a uint32) int {
	return bits.LeadingZeros32(a)
}

// clz64 counts leading zeros in a 64-bit value.
// Reference: tinyemu-2019-12-21/softfp.c:43-53
func clz64(a uint64) int {
	return bits.LeadingZeros64(a)
}

// clz128 counts leading zeros in a 128-bit value.
// Reference: tinyemu-2019-12-21/softfp.c:56-72
func clz128(a uint128.Uint128) int {
	if a.IsZero() {
		return 128
	}
	if a.Hi != 0 {
		return bits.LeadingZeros64(a.Hi)
	}
	return bits.LeadingZeros64(a.Lo) + 64
}

// rshiftRnd128 performs a right shift with sticky bit for rounding on 128-bit value.
// Reference: tinyemu-2019-12-21/softfp_template.h:93-105 (rshift_rnd with F_SIZE=128)
func rshiftRnd128(a uint128.Uint128, d int) uint128.Uint128 {
	if d == 0 {
		return a
	}
	if d >= 128 {
		if !a.IsZero() {
			return uint128.From64(1)
		}
		return uint128.Zero
	}
	// Get the bits that will be shifted out
	var sticky bool
	if d >= 64 {
		// All low bits will be shifted out, plus some high bits
		sticky = a.Lo != 0 || (a.Hi&((1<<(d-64))-1)) != 0
		result := uint128.From64(a.Hi >> (d - 64))
		if sticky {
			result = result.Or(uint128.From64(1))
		}
		return result
	}
	// d < 64
	mask := uint64((1 << d) - 1)
	sticky = (a.Lo & mask) != 0
	newLo := (a.Lo >> d) | (a.Hi << (64 - d))
	newHi := a.Hi >> d
	result := uint128.Uint128{Hi: newHi, Lo: newLo}
	if sticky {
		result = result.Or(uint128.From64(1))
	}
	return result
}

// rshiftRnd32 performs a right shift with sticky bit for rounding.
// If d >= 32, returns 1 if a was non-zero, 0 otherwise.
// Otherwise, returns (a >> d) | (bottom d bits were non-zero ? 1 : 0)
// Reference: tinyemu-2019-12-21/softfp_template.h:93-105 (rshift_rnd with F_SIZE=32)
func rshiftRnd32(a uint32, d int) uint32 {
	if d == 0 {
		return a
	}
	if d >= 32 {
		if a != 0 {
			return 1
		}
		return 0
	}
	mask := (uint32(1) << d) - 1
	return (a >> d) | boolToU32((a&mask) != 0)
}

// rshiftRnd64 performs a right shift with sticky bit for rounding.
// If d >= 64, returns 1 if a was non-zero, 0 otherwise.
// Otherwise, returns (a >> d) | (bottom d bits were non-zero ? 1 : 0)
// Reference: tinyemu-2019-12-21/softfp_template.h:93-105 (rshift_rnd with F_SIZE=64)
func rshiftRnd64(a uint64, d int) uint64 {
	if d == 0 {
		return a
	}
	if d >= 64 {
		if a != 0 {
			return 1
		}
		return 0
	}
	mask := (uint64(1) << d) - 1
	return (a >> d) | boolToU64((a&mask) != 0)
}

// boolToU32 converts a bool to uint32 (1 for true, 0 for false).
func boolToU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// boolToU64 converts a bool to uint64 (1 for true, 0 for false).
func boolToU64(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// rshiftRnd128ToU64 performs a right shift on a 128-bit value and returns a 64-bit result
// with sticky bit for rounding. Used when converting F128 to smaller float formats.
// The shift amount should result in a value that fits in 64 bits.
func rshiftRnd128ToU64(a uint128.Uint128, d int) uint64 {
	if d == 0 {
		return a.Lo
	}
	if d >= 128 {
		if !a.IsZero() {
			return 1
		}
		return 0
	}
	// Calculate the shifted result
	var result uint64
	var sticky bool

	if d >= 64 {
		// All low bits shifted out, some high bits become result
		result = a.Hi >> (d - 64)
		// Sticky is set if any shifted-out bits were non-zero
		highMask := uint64((1 << (d - 64)) - 1)
		sticky = a.Lo != 0 || (a.Hi&highMask) != 0
	} else {
		// d < 64
		// Result is combination of shifted high and low
		result = (a.Lo >> d) | (a.Hi << (64 - d))
		// Check if this would overflow uint64
		if a.Hi>>(d) != 0 {
			// Result doesn't fit in 64 bits, saturate
			return 0xFFFFFFFFFFFFFFFF
		}
		mask := uint64((1 << d) - 1)
		sticky = (a.Lo & mask) != 0
	}

	if sticky {
		result |= 1
	}
	return result
}

// normalizeSubnormal32 normalizes a subnormal 32-bit float mantissa.
// Returns the new exponent and normalized mantissa with implicit bit position.
// Reference: tinyemu-2019-12-21/softfp_template.h:298-304 (normalize_subnormal_sf with F_SIZE=32)
func normalizeSubnormal32(mant uint32) (exp int32, normMant uint32) {
	shift := Float32MantBits - (Float32Bits - 1 - clz32(mant))
	exp = int32(1 - shift)
	normMant = mant << shift
	return
}

// normalizeSubnormal64 normalizes a subnormal 64-bit float mantissa.
// Returns the new exponent and normalized mantissa with implicit bit position.
// Reference: tinyemu-2019-12-21/softfp_template.h:298-304 (normalize_subnormal_sf with F_SIZE=64)
func normalizeSubnormal64(mant uint64) (exp int32, normMant uint64) {
	shift := Float64MantBits - (Float64Bits - 1 - clz64(mant))
	exp = int32(1 - shift)
	normMant = mant << shift
	return
}

// roundPackFloat32 rounds and packs a 32-bit float result.
// mant is assumed to have its MSB at bit position (Float32Bits - 2) = 30.
// This function handles rounding, overflow, underflow, and subnormal results.
// Reference: tinyemu-2019-12-21/softfp_template.h:108-175 (round_pack_sf with F_SIZE=32)
func roundPackFloat32(sign uint32, exp int32, mant uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	var addend uint32
	var rndBits uint32

	// Determine rounding addend based on rounding mode
	switch rm {
	case RNE, RMM:
		addend = 1 << (float32RndSize - 1)
	case RTZ:
		addend = 0
	case RDN, RUP:
		// Round towards signed infinity
		if (sign != 0) != (rm == RUP) {
			addend = (1 << float32RndSize) - 1
		} else {
			addend = 0
		}
	}

	// Handle potential subnormal result
	if exp <= 0 {
		var isSubnormal bool

		// Check if result will be subnormal after rounding
		if exp < 0 {
			isSubnormal = true
		} else {
			isSubnormal = (mant + addend) < (1 << (Float32Bits - 1))
		}

		// Shift right to denormalize
		diff := 1 - exp
		mant = rshiftRnd32(mant, int(diff))
		rndBits = mant & ((1 << float32RndSize) - 1)

		// Set underflow flag if subnormal and inexact
		if isSubnormal && rndBits != 0 {
			*flags |= FlagUnderflow
		}
		exp = 1
	} else {
		rndBits = mant & ((1 << float32RndSize) - 1)
	}

	// Set inexact flag if rounding bits are non-zero
	if rndBits != 0 {
		*flags |= FlagInexact
	}

	// Add rounding and shift to final mantissa position
	mant = (mant + addend) >> float32RndSize

	// For RNE, if exactly halfway, round to even
	if rm == RNE && rndBits == (1<<(float32RndSize-1)) {
		mant &= ^uint32(1)
	}

	// Adjust exponent for carry out of mantissa
	exp += int32(mant >> (Float32MantBits + 1))

	if mant <= Float32MantMask {
		// Denormalized or zero
		exp = 0
	} else if exp >= Float32ExpMask {
		// Overflow
		if addend == 0 {
			// Round to max finite
			exp = Float32ExpMask - 1
			mant = Float32MantMask
		} else {
			// Round to infinity
			exp = Float32ExpMask
			mant = 0
		}
		*flags |= FlagOverflow | FlagInexact
	}

	return Pack32(sign, uint32(exp), mant)
}

// roundPackFloat64 rounds and packs a 64-bit float result.
// mant is assumed to have its MSB at bit position (Float64Bits - 2) = 62.
// This function handles rounding, overflow, underflow, and subnormal results.
// Reference: tinyemu-2019-12-21/softfp_template.h:108-175 (round_pack_sf with F_SIZE=64)
func roundPackFloat64(sign uint32, exp int32, mant uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	var addend uint64
	var rndBits uint64

	// Determine rounding addend based on rounding mode
	switch rm {
	case RNE, RMM:
		addend = 1 << (float64RndSize - 1)
	case RTZ:
		addend = 0
	case RDN, RUP:
		// Round towards signed infinity
		if (sign != 0) != (rm == RUP) {
			addend = (1 << float64RndSize) - 1
		} else {
			addend = 0
		}
	}

	// Handle potential subnormal result
	if exp <= 0 {
		var isSubnormal bool

		// Check if result will be subnormal after rounding
		if exp < 0 {
			isSubnormal = true
		} else {
			isSubnormal = (mant + addend) < (1 << (Float64Bits - 1))
		}

		// Shift right to denormalize
		diff := 1 - exp
		mant = rshiftRnd64(mant, int(diff))
		rndBits = mant & ((1 << float64RndSize) - 1)

		// Set underflow flag if subnormal and inexact
		if isSubnormal && rndBits != 0 {
			*flags |= FlagUnderflow
		}
		exp = 1
	} else {
		rndBits = mant & ((1 << float64RndSize) - 1)
	}

	// Set inexact flag if rounding bits are non-zero
	if rndBits != 0 {
		*flags |= FlagInexact
	}

	// Add rounding and shift to final mantissa position
	mant = (mant + addend) >> float64RndSize

	// For RNE, if exactly halfway, round to even
	if rm == RNE && rndBits == (1<<(float64RndSize-1)) {
		mant &= ^uint64(1)
	}

	// Adjust exponent for carry out of mantissa
	exp += int32(mant >> (Float64MantBits + 1))

	if mant <= Float64MantMask {
		// Denormalized or zero
		exp = 0
	} else if exp >= Float64ExpMask {
		// Overflow
		if addend == 0 {
			// Round to max finite
			exp = Float64ExpMask - 1
			mant = Float64MantMask
		} else {
			// Round to infinity
			exp = Float64ExpMask
			mant = 0
		}
		*flags |= FlagOverflow | FlagInexact
	}

	return Pack64(sign, uint32(exp), mant)
}

// normalizeFloat32 normalizes and packs a 32-bit float.
// mant is assumed to have at most (Float32Bits - 1) significant bits.
// Reference: tinyemu-2019-12-21/softfp_template.h:178-187 (normalize_sf with F_SIZE=32)
func normalizeFloat32(sign uint32, exp int32, mant uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	shift := clz32(mant) - (Float32Bits - 1 - float32IMantSize)
	if shift < 0 {
		shift = 0
	}
	exp -= int32(shift)
	mant <<= shift
	return roundPackFloat32(sign, exp, mant, rm, flags)
}

// normalizeFloat64 normalizes and packs a 64-bit float.
// mant is assumed to have at most (Float64Bits - 1) significant bits.
// Reference: tinyemu-2019-12-21/softfp_template.h:178-187 (normalize_sf with F_SIZE=64)
func normalizeFloat64(sign uint32, exp int32, mant uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	shift := clz64(mant) - (Float64Bits - 1 - float64IMantSize)
	if shift < 0 {
		shift = 0
	}
	exp -= int32(shift)
	mant <<= shift
	return roundPackFloat64(sign, exp, mant, rm, flags)
}

// normalize2Float32 normalizes and packs a 32-bit float from a double-word mantissa.
// mant1 is the high word, mant0 is the low word.
// mant1 is assumed to have at most (Float32Bits - 1) significant bits.
// Reference: tinyemu-2019-12-21/softfp_template.h:191-213 (normalize2_sf with F_SIZE=32)
func normalize2Float32(sign uint32, exp int32, mant1, mant0 uint32, rm RoundingMode, flags *ExceptionFlags) uint32 {
	var l, shift int

	if mant1 == 0 {
		l = Float32Bits + clz32(mant0)
	} else {
		l = clz32(mant1)
	}

	shift = l - (Float32Bits - 1 - float32IMantSize)
	if shift < 0 {
		shift = 0
	}
	exp -= int32(shift)

	if shift == 0 {
		mant1 |= boolToU32(mant0 != 0)
	} else if shift < Float32Bits {
		mant1 = (mant1 << shift) | (mant0 >> (Float32Bits - shift))
		mant0 <<= shift
		mant1 |= boolToU32(mant0 != 0)
	} else {
		mant1 = mant0 << (shift - Float32Bits)
	}

	return roundPackFloat32(sign, exp, mant1, rm, flags)
}

// normalize2Float64 normalizes and packs a 64-bit float from a double-word mantissa.
// mant1 is the high word, mant0 is the low word.
// mant1 is assumed to have at most (Float64Bits - 1) significant bits.
// Reference: tinyemu-2019-12-21/softfp_template.h:191-213 (normalize2_sf with F_SIZE=64)
func normalize2Float64(sign uint32, exp int32, mant1, mant0 uint64, rm RoundingMode, flags *ExceptionFlags) uint64 {
	var l, shift int

	if mant1 == 0 {
		l = Float64Bits + clz64(mant0)
	} else {
		l = clz64(mant1)
	}

	shift = l - (Float64Bits - 1 - float64IMantSize)
	if shift < 0 {
		shift = 0
	}
	exp -= int32(shift)

	if shift == 0 {
		mant1 |= boolToU64(mant0 != 0)
	} else if shift < Float64Bits {
		mant1 = (mant1 << shift) | (mant0 >> (Float64Bits - shift))
		mant0 <<= shift
		mant1 |= boolToU64(mant0 != 0)
	} else {
		mant1 = mant0 << (shift - Float64Bits)
	}

	return roundPackFloat64(sign, exp, mant1, rm, flags)
}
