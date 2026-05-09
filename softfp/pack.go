package softfp

// Pack32 assembles a 32-bit float from sign, exponent, and mantissa.
// sign: 0 or 1
// exp: biased exponent (0-255)
// mant: mantissa without implicit bit (0 to 0x7FFFFF)
// Reference: tinyemu-2019-12-21/softfp_template.h:78-83 (pack_sf with F_SIZE=32)
func Pack32(sign uint32, exp uint32, mant uint32) uint32 {
	return (sign << (Float32Bits - 1)) |
		(exp << Float32MantBits) |
		(mant & Float32MantMask)
}

// Unpack32 extracts sign, exponent, and mantissa from a 32-bit float.
// Returns: sign (0 or 1), biased exponent, and mantissa without implicit bit.
// Reference: tinyemu-2019-12-21/softfp_template.h:85-91 (unpack_sf with F_SIZE=32)
func Unpack32(a uint32) (sign uint32, exp int32, mant uint32) {
	sign = a >> (Float32Bits - 1)
	exp = int32((a >> Float32MantBits) & Float32ExpMask)
	mant = a & Float32MantMask
	return
}

// Pack64 assembles a 64-bit float from sign, exponent, and mantissa.
// sign: 0 or 1
// exp: biased exponent (0-2047)
// mant: mantissa without implicit bit (0 to 0xFFFFFFFFFFFFF)
// Reference: tinyemu-2019-12-21/softfp_template.h:78-83 (pack_sf with F_SIZE=64)
func Pack64(sign uint32, exp uint32, mant uint64) uint64 {
	return (uint64(sign) << (Float64Bits - 1)) |
		(uint64(exp) << Float64MantBits) |
		(mant & Float64MantMask)
}

// Unpack64 extracts sign, exponent, and mantissa from a 64-bit float.
// Returns: sign (0 or 1), biased exponent, and mantissa without implicit bit.
// Reference: tinyemu-2019-12-21/softfp_template.h:85-91 (unpack_sf with F_SIZE=64)
func Unpack64(a uint64) (sign uint32, exp int32, mant uint64) {
	sign = uint32(a >> (Float64Bits - 1))
	exp = int32((a >> Float64MantBits) & Float64ExpMask)
	mant = a & Float64MantMask
	return
}

// IsNaN32 returns true if the 32-bit float is a NaN (quiet or signaling).
// Reference: tinyemu-2019-12-21/softfp_template.h:224-231 (isnan_sf with F_SIZE=32)
func IsNaN32(a uint32) bool {
	exp := (a >> Float32MantBits) & Float32ExpMask
	mant := a & Float32MantMask
	return exp == Float32ExpMask && mant != 0
}

// IsSigNaN32 returns true if the 32-bit float is a signaling NaN.
// A signaling NaN has the quiet bit (MSB of mantissa) clear.
// Reference: tinyemu-2019-12-21/softfp_template.h:215-222 (issignan_sf with F_SIZE=32)
func IsSigNaN32(a uint32) bool {
	// Check if exponent is all 1s and quiet bit is 0 (signaling)
	// The exp1 field spans exp and top bit of mantissa
	exp1 := (a >> (Float32MantBits - 1)) & ((1 << (Float32ExpBits + 1)) - 1)
	mant := a & Float32MantMask
	// exp1 == 2*ExpMask means exp is all 1s and quiet bit is 0
	return exp1 == (2*Float32ExpMask) && mant != 0
}

// IsNaN64 returns true if the 64-bit float is a NaN (quiet or signaling).
// Reference: tinyemu-2019-12-21/softfp_template.h:224-231 (isnan_sf with F_SIZE=64)
func IsNaN64(a uint64) bool {
	exp := (a >> Float64MantBits) & Float64ExpMask
	mant := a & Float64MantMask
	return exp == Float64ExpMask && mant != 0
}

// IsSigNaN64 returns true if the 64-bit float is a signaling NaN.
// A signaling NaN has the quiet bit (MSB of mantissa) clear.
// Reference: tinyemu-2019-12-21/softfp_template.h:215-222 (issignan_sf with F_SIZE=64)
func IsSigNaN64(a uint64) bool {
	// Check if exponent is all 1s and quiet bit is 0 (signaling)
	exp1 := (a >> (Float64MantBits - 1)) & ((1 << (Float64ExpBits + 1)) - 1)
	mant := a & Float64MantMask
	return exp1 == (2*Float64ExpMask) && mant != 0
}

// IsInf32 returns true if the 32-bit float is infinity (positive or negative).
func IsInf32(a uint32) bool {
	exp := (a >> Float32MantBits) & Float32ExpMask
	mant := a & Float32MantMask
	return exp == Float32ExpMask && mant == 0
}

// IsInf64 returns true if the 64-bit float is infinity (positive or negative).
func IsInf64(a uint64) bool {
	exp := (a >> Float64MantBits) & Float64ExpMask
	mant := a & Float64MantMask
	return exp == Float64ExpMask && mant == 0
}

// IsZero32 returns true if the 32-bit float is zero (positive or negative).
func IsZero32(a uint32) bool {
	return (a & ^Float32SignMask) == 0
}

// IsZero64 returns true if the 64-bit float is zero (positive or negative).
func IsZero64(a uint64) bool {
	return (a & ^Float64SignMask) == 0
}

// IsSubnormal32 returns true if the 32-bit float is a subnormal number.
func IsSubnormal32(a uint32) bool {
	exp := (a >> Float32MantBits) & Float32ExpMask
	mant := a & Float32MantMask
	return exp == 0 && mant != 0
}

// IsSubnormal64 returns true if the 64-bit float is a subnormal number.
func IsSubnormal64(a uint64) bool {
	exp := (a >> Float64MantBits) & Float64ExpMask
	mant := a & Float64MantMask
	return exp == 0 && mant != 0
}

// FClass32 classifies a 32-bit float according to the RISC-V FCLASS instruction.
// Reference: tinyemu-2019-12-21/softfp_template.h:898-939 (fclass_sf)
func FClass32(a uint32) FClass {
	sign := a >> (Float32Bits - 1)
	exp := int32((a >> Float32MantBits) & Float32ExpMask)
	mant := a & Float32MantMask

	if exp == Float32ExpMask {
		if mant != 0 {
			// NaN
			if (mant & Float32QNaNMask) != 0 {
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
		if mant == 0 {
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

// FClass64 classifies a 64-bit float according to the RISC-V FCLASS instruction.
// Reference: tinyemu-2019-12-21/softfp_template.h:898-939 (fclass_sf)
func FClass64(a uint64) FClass {
	sign := uint32(a >> (Float64Bits - 1))
	exp := int32((a >> Float64MantBits) & Float64ExpMask)
	mant := a & Float64MantMask

	if exp == Float64ExpMask {
		if mant != 0 {
			// NaN
			if (mant & Float64QNaNMask) != 0 {
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
		if mant == 0 {
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
