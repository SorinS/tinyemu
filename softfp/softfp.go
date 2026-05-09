// Package softfp provides software floating-point operations for RISC-V emulation.
// This is a port of TinyEMU's softfp library.
//
// Reference: softfp.h and softfp_template.h from TinyEMU 2019-12-21
package softfp

// RoundingMode represents the IEEE 754 rounding modes used by RISC-V.
type RoundingMode uint8

const (
	// RNE rounds to Nearest, ties to Even (default)
	RNE RoundingMode = iota
	// RTZ rounds towards Zero
	RTZ
	// RDN rounds Down (towards negative infinity)
	RDN
	// RUP rounds Up (towards positive infinity)
	RUP
	// RMM rounds to Nearest, ties to Max Magnitude
	RMM
)

// ExceptionFlags represents the IEEE 754 floating-point exception flags.
// These correspond to the RISC-V fflags CSR bits.
type ExceptionFlags uint32

const (
	// FlagInexact indicates the result was rounded (NX)
	FlagInexact ExceptionFlags = 1 << iota
	// FlagUnderflow indicates the result underflowed (UF)
	FlagUnderflow
	// FlagOverflow indicates the result overflowed (OF)
	FlagOverflow
	// FlagDivideZero indicates division by zero (DZ)
	FlagDivideZero
	// FlagInvalidOp indicates an invalid operation (NV)
	FlagInvalidOp
)

// FClass represents the classification of a floating-point value.
// These correspond to the RISC-V FCLASS instruction output bits.
type FClass uint32

const (
	// FClassNegInf indicates negative infinity
	FClassNegInf FClass = 1 << iota
	// FClassNegNormal indicates negative normal number
	FClassNegNormal
	// FClassNegSubnormal indicates negative subnormal number
	FClassNegSubnormal
	// FClassNegZero indicates negative zero
	FClassNegZero
	// FClassPosZero indicates positive zero
	FClassPosZero
	// FClassPosSubnormal indicates positive subnormal number
	FClassPosSubnormal
	// FClassPosNormal indicates positive normal number
	FClassPosNormal
	// FClassPosInf indicates positive infinity
	FClassPosInf
	// FClassSNaN indicates signaling NaN
	FClassSNaN
	// FClassQNaN indicates quiet NaN
	FClassQNaN
)

// Float32 constants for IEEE 754 single-precision format
const (
	// Float32MantBits is the number of mantissa bits (excluding implicit 1)
	Float32MantBits = 23
	// Float32ExpBits is the number of exponent bits
	Float32ExpBits = 8
	// Float32Bits is the total size in bits
	Float32Bits = 32

	// Float32ExpMask is the mask for exponent bits
	Float32ExpMask = (1 << Float32ExpBits) - 1 // 0xFF
	// Float32MantMask is the mask for mantissa bits
	Float32MantMask = (1 << Float32MantBits) - 1 // 0x7FFFFF
	// Float32SignMask is the mask for the sign bit
	Float32SignMask = uint32(1) << (Float32Bits - 1) // 0x80000000
	// Float32QNaNMask is the quiet NaN bit in mantissa
	Float32QNaNMask = uint32(1) << (Float32MantBits - 1) // 0x400000

	// Float32ExpBias is the exponent bias
	Float32ExpBias = (1 << (Float32ExpBits - 1)) - 1 // 127

	// Float32QNaN is the canonical quiet NaN value
	Float32QNaN = (uint32(Float32ExpMask) << Float32MantBits) | Float32QNaNMask
)

// Float64 constants for IEEE 754 double-precision format
const (
	// Float64MantBits is the number of mantissa bits (excluding implicit 1)
	Float64MantBits = 52
	// Float64ExpBits is the number of exponent bits
	Float64ExpBits = 11
	// Float64Bits is the total size in bits
	Float64Bits = 64

	// Float64ExpMask is the mask for exponent bits
	Float64ExpMask = (1 << Float64ExpBits) - 1 // 0x7FF
	// Float64MantMask is the mask for mantissa bits
	Float64MantMask = (uint64(1) << Float64MantBits) - 1 // 0xFFFFFFFFFFFFF
	// Float64SignMask is the mask for the sign bit
	Float64SignMask = uint64(1) << (Float64Bits - 1) // 0x8000000000000000
	// Float64QNaNMask is the quiet NaN bit in mantissa
	Float64QNaNMask = uint64(1) << (Float64MantBits - 1) // 0x8000000000000

	// Float64ExpBias is the exponent bias
	Float64ExpBias = (1 << (Float64ExpBits - 1)) - 1 // 1023

	// Float64QNaN is the canonical quiet NaN value
	Float64QNaN = (uint64(Float64ExpMask) << Float64MantBits) | Float64QNaNMask
)

// Float128 constants for IEEE 754 quad-precision format
// Reference: tinyemu-2019-12-21/softfp_template.h:37-41
const (
	// Float128MantBits is the number of mantissa bits (excluding implicit 1)
	Float128MantBits = 112
	// Float128ExpBits is the number of exponent bits
	Float128ExpBits = 15
	// Float128Bits is the total size in bits
	Float128Bits = 128

	// Float128ExpMask is the mask for exponent bits
	Float128ExpMask = (1 << Float128ExpBits) - 1 // 0x7FFF

	// Float128ExpBias is the exponent bias
	Float128ExpBias = (1 << (Float128ExpBits - 1)) - 1 // 16383
)

// Internal mantissa size constants for rounding calculations
const (
	// float32IMantSize is the internal mantissa size (F_SIZE - 2)
	float32IMantSize = Float32Bits - 2 // 30
	// float32RndSize is the rounding bits count
	float32RndSize = float32IMantSize - Float32MantBits // 7

	// float64IMantSize is the internal mantissa size (F_SIZE - 2)
	float64IMantSize = Float64Bits - 2 // 62
	// float64RndSize is the rounding bits count
	float64RndSize = float64IMantSize - Float64MantBits // 10

	// float128IMantSize is the internal mantissa size (F_SIZE - 2)
	float128IMantSize = Float128Bits - 2 // 126
	// float128RndSize is the rounding bits count
	float128RndSize = float128IMantSize - Float128MantBits // 14
)
