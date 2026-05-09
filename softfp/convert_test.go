package softfp

import (
	"math"
	"testing"

	"lukechampine.com/uint128"
)

// ============================================================================
// F32 to Integer Conversion Tests
// ============================================================================

func TestCvtF32ToI32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		want     int32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero32, RNE, 0, 0},
		{"-0.0", negZero32, RNE, 0, 0},
		{"1.0", posOne32, RNE, 1, 0},
		{"-1.0", negOne32, RNE, -1, 0},
		{"2.0", posTwo32, RNE, 2, 0},
		{"-2.0", negTwo32, RNE, -2, 0},

		// Integer values
		{"100.0", f32bits(100.0), RNE, 100, 0},
		{"-100.0", f32bits(-100.0), RNE, -100, 0},
		{"1000000.0", f32bits(1000000.0), RNE, 1000000, 0},

		// Fractional values with rounding
		{"1.5 RNE", f32bits(1.5), RNE, 2, FlagInexact},    // round to even
		{"2.5 RNE", f32bits(2.5), RNE, 2, FlagInexact},    // round to even
		{"1.5 RTZ", f32bits(1.5), RTZ, 1, FlagInexact},    // truncate
		{"-1.5 RTZ", f32bits(-1.5), RTZ, -1, FlagInexact}, // truncate towards zero
		{"1.5 RUP", f32bits(1.5), RUP, 2, FlagInexact},    // round up
		{"-1.5 RUP", f32bits(-1.5), RUP, -1, FlagInexact}, // round towards +inf
		{"1.5 RDN", f32bits(1.5), RDN, 1, FlagInexact},    // round down
		{"-1.5 RDN", f32bits(-1.5), RDN, -2, FlagInexact}, // round towards -inf

		// Edge cases
		{"0.999 RNE", f32bits(0.999), RNE, 1, FlagInexact},
		{"0.001 RTZ", f32bits(0.001), RTZ, 0, FlagInexact},

		// Max representable
		{"2147483520.0", f32bits(2147483520.0), RNE, 2147483520, 0}, // close to INT32_MAX

		// Overflow cases - should return INT32_MAX or INT32_MIN with invalid flag
		{"+inf", posInf32, RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"-inf", negInf32, RNE, -0x80000000, FlagInvalidOp},
		{"NaN", qNaN32, RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"sNaN", sNaN32, RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"too large positive", f32bits(3e9), RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"too large negative", f32bits(-3e9), RNE, -0x80000000, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToI32(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF32ToI32(0x%08x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF32ToI32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF32ToU32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		want     uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero32, RNE, 0, 0},
		{"1.0", posOne32, RNE, 1, 0},
		{"2.0", posTwo32, RNE, 2, 0},
		{"100.0", f32bits(100.0), RNE, 100, 0},

		// Fractional values with rounding
		{"1.5 RNE", f32bits(1.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f32bits(1.5), RTZ, 1, FlagInexact},
		{"1.5 RUP", f32bits(1.5), RUP, 2, FlagInexact},
		{"1.5 RDN", f32bits(1.5), RDN, 1, FlagInexact},

		// Large values
		{"4294967040.0", f32bits(4294967040.0), RNE, 4294967040, 0}, // close to UINT32_MAX

		// Overflow/underflow cases
		{"+inf", posInf32, RNE, 0xFFFFFFFF, FlagInvalidOp},
		{"-1.0", negOne32, RNE, 0, FlagInvalidOp},
		{"-inf", negInf32, RNE, 0, FlagInvalidOp},
		{"NaN", qNaN32, RNE, 0xFFFFFFFF, FlagInvalidOp},
		{"too large", f32bits(5e9), RNE, 0xFFFFFFFF, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToU32(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF32ToU32(0x%08x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF32ToU32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF32ToI64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		want     int64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero32, RNE, 0, 0},
		{"1.0", posOne32, RNE, 1, 0},
		{"-1.0", negOne32, RNE, -1, 0},
		{"1000000.0", f32bits(1000000.0), RNE, 1000000, 0},

		// Large values that fit in int64 but not int32
		{"1e10", f32bits(1e10), RNE, 10000000000, 0},
		{"-1e10", f32bits(-1e10), RNE, -10000000000, 0},

		// Fractional values
		{"1.5 RNE", f32bits(1.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f32bits(1.5), RTZ, 1, FlagInexact},

		// Overflow cases
		{"+inf", posInf32, RNE, 0x7FFFFFFFFFFFFFFF, FlagInvalidOp},
		{"-inf", negInf32, RNE, -0x8000000000000000, FlagInvalidOp},
		{"NaN", qNaN32, RNE, 0x7FFFFFFFFFFFFFFF, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToI64(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF32ToI64(0x%08x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF32ToI64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF32ToU64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		want     uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero32, RNE, 0, 0},
		{"1.0", posOne32, RNE, 1, 0},
		{"1e10", f32bits(1e10), RNE, 10000000000, 0},

		// Fractional values
		{"1.5 RNE", f32bits(1.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f32bits(1.5), RTZ, 1, FlagInexact},

		// Overflow/underflow cases
		{"+inf", posInf32, RNE, 0xFFFFFFFFFFFFFFFF, FlagInvalidOp},
		{"-1.0", negOne32, RNE, 0, FlagInvalidOp},
		{"-inf", negInf32, RNE, 0, FlagInvalidOp},
		{"NaN", qNaN32, RNE, 0xFFFFFFFFFFFFFFFF, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToU64(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF32ToU64(0x%08x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF32ToU64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

// ============================================================================
// F64 to Integer Conversion Tests
// ============================================================================

func TestCvtF64ToI32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		want     int32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero64, RNE, 0, 0},
		{"-0.0", negZero64, RNE, 0, 0},
		{"1.0", posOne64, RNE, 1, 0},
		{"-1.0", negOne64, RNE, -1, 0},
		{"2.0", posTwo64, RNE, 2, 0},

		// Precise integer values (F64 can represent all int32 exactly)
		{"2147483647.0", f64bits(2147483647.0), RNE, 2147483647, 0},
		{"-2147483648.0", f64bits(-2147483648.0), RNE, -2147483648, 0},

		// Fractional values with rounding
		{"1.5 RNE", f64bits(1.5), RNE, 2, FlagInexact},
		{"2.5 RNE", f64bits(2.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f64bits(1.5), RTZ, 1, FlagInexact},
		{"-1.5 RTZ", f64bits(-1.5), RTZ, -1, FlagInexact},
		{"1.5 RUP", f64bits(1.5), RUP, 2, FlagInexact},
		{"-1.5 RUP", f64bits(-1.5), RUP, -1, FlagInexact},
		{"1.5 RDN", f64bits(1.5), RDN, 1, FlagInexact},
		{"-1.5 RDN", f64bits(-1.5), RDN, -2, FlagInexact},

		// Overflow cases
		{"+inf", posInf64, RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"-inf", negInf64, RNE, -0x80000000, FlagInvalidOp},
		{"NaN", qNaN64, RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"too large", f64bits(3e9), RNE, 0x7FFFFFFF, FlagInvalidOp},
		{"too small", f64bits(-3e9), RNE, -0x80000000, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF64ToI32(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF64ToI32(0x%016x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF64ToI32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF64ToU32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		want     uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero64, RNE, 0, 0},
		{"1.0", posOne64, RNE, 1, 0},
		{"4294967295.0", f64bits(4294967295.0), RNE, 4294967295, 0},

		// Fractional values
		{"1.5 RNE", f64bits(1.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f64bits(1.5), RTZ, 1, FlagInexact},

		// Overflow/underflow
		{"+inf", posInf64, RNE, 0xFFFFFFFF, FlagInvalidOp},
		{"-1.0", negOne64, RNE, 0, FlagInvalidOp},
		{"-inf", negInf64, RNE, 0, FlagInvalidOp},
		{"NaN", qNaN64, RNE, 0xFFFFFFFF, FlagInvalidOp},
		{"too large", f64bits(5e9), RNE, 0xFFFFFFFF, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF64ToU32(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF64ToU32(0x%016x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF64ToU32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF64ToI64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		want     int64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero64, RNE, 0, 0},
		{"1.0", posOne64, RNE, 1, 0},
		{"-1.0", negOne64, RNE, -1, 0},
		{"1e15", f64bits(1e15), RNE, 1000000000000000, 0},
		{"-1e15", f64bits(-1e15), RNE, -1000000000000000, 0},

		// Fractional values
		{"1.5 RNE", f64bits(1.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f64bits(1.5), RTZ, 1, FlagInexact},
		{"-1.5 RTZ", f64bits(-1.5), RTZ, -1, FlagInexact},

		// Overflow
		{"+inf", posInf64, RNE, 0x7FFFFFFFFFFFFFFF, FlagInvalidOp},
		{"-inf", negInf64, RNE, -0x8000000000000000, FlagInvalidOp},
		{"NaN", qNaN64, RNE, 0x7FFFFFFFFFFFFFFF, FlagInvalidOp},
		{"too large", f64bits(1e19), RNE, 0x7FFFFFFFFFFFFFFF, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF64ToI64(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF64ToI64(0x%016x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF64ToI64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF64ToU64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		want     uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0.0", posZero64, RNE, 0, 0},
		{"1.0", posOne64, RNE, 1, 0},
		{"1e15", f64bits(1e15), RNE, 1000000000000000, 0},

		// Fractional values
		{"1.5 RNE", f64bits(1.5), RNE, 2, FlagInexact},
		{"1.5 RTZ", f64bits(1.5), RTZ, 1, FlagInexact},

		// Overflow/underflow
		{"+inf", posInf64, RNE, 0xFFFFFFFFFFFFFFFF, FlagInvalidOp},
		{"-1.0", negOne64, RNE, 0, FlagInvalidOp},
		{"-inf", negInf64, RNE, 0, FlagInvalidOp},
		{"NaN", qNaN64, RNE, 0xFFFFFFFFFFFFFFFF, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF64ToU64(tc.a, tc.rm, &flags)

			if got != tc.want {
				t.Errorf("CvtF64ToU64(0x%016x, %v) = %d, want %d",
					tc.a, tc.rm, got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF64ToU64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

// ============================================================================
// Integer to F32 Conversion Tests
// ============================================================================

func TestCvtI32ToF32(t *testing.T) {
	tests := []struct {
		name     string
		a        int32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0", 0, RNE, posZero32, 0},
		{"1", 1, RNE, posOne32, 0},
		{"-1", -1, RNE, negOne32, 0},
		{"2", 2, RNE, posTwo32, 0},
		{"-2", -2, RNE, negTwo32, 0},
		{"100", 100, RNE, f32bits(100.0), 0},
		{"-100", -100, RNE, f32bits(-100.0), 0},

		// Exact representations (powers of 2)
		{"1024", 1024, RNE, f32bits(1024.0), 0},
		{"-1024", -1024, RNE, f32bits(-1024.0), 0},
		{"16777216", 16777216, RNE, f32bits(16777216.0), 0}, // 2^24, exact

		// Values that can't be represented exactly (need rounding)
		// 16777217 = 2^24 + 1, cannot be represented exactly in float32
		{"16777217", 16777217, RNE, f32bits(16777216.0), FlagInexact}, // rounds to even

		// INT32_MAX and INT32_MIN
		{"INT32_MAX", 0x7FFFFFFF, RNE, f32bits(2147483648.0), FlagInexact},
		{"INT32_MIN", -0x80000000, RNE, f32bits(-2147483648.0), 0}, // exact (power of 2)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtI32ToF32(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtI32ToF32(%d, %v) = 0x%08x (%v), want 0x%08x (%v)",
					tc.a, tc.rm, got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtI32ToF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtU32ToF32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0", 0, RNE, posZero32, 0},
		{"1", 1, RNE, posOne32, 0},
		{"2", 2, RNE, posTwo32, 0},
		{"100", 100, RNE, f32bits(100.0), 0},

		// Exact representations
		{"16777216", 16777216, RNE, f32bits(16777216.0), 0},

		// Values requiring rounding
		{"16777217", 16777217, RNE, f32bits(16777216.0), FlagInexact},

		// UINT32_MAX
		{"UINT32_MAX", 0xFFFFFFFF, RNE, f32bits(4294967296.0), FlagInexact},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtU32ToF32(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtU32ToF32(%d, %v) = 0x%08x (%v), want 0x%08x (%v)",
					tc.a, tc.rm, got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtU32ToF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtI64ToF32(t *testing.T) {
	tests := []struct {
		name     string
		a        int64
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0", 0, RNE, posZero32, 0},
		{"1", 1, RNE, posOne32, 0},
		{"-1", -1, RNE, negOne32, 0},
		{"1000000", 1000000, RNE, f32bits(1000000.0), 0},

		// Large values
		{"1e15", 1000000000000000, RNE, f32bits(1e15), FlagInexact},
		{"-1e15", -1000000000000000, RNE, f32bits(-1e15), FlagInexact},

		// INT64_MIN (power of 2, should be exact in terms of magnitude)
		{"INT64_MIN", -0x8000000000000000, RNE, f32bits(-9223372036854775808.0), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtI64ToF32(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtI64ToF32(%d, %v) = 0x%08x (%v), want 0x%08x (%v)",
					tc.a, tc.rm, got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtI64ToF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtU64ToF32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0", 0, RNE, posZero32, 0},
		{"1", 1, RNE, posOne32, 0},
		{"1000000", 1000000, RNE, f32bits(1000000.0), 0},

		// Large values
		{"1e15", 1000000000000000, RNE, f32bits(1e15), FlagInexact},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtU64ToF32(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtU64ToF32(%d, %v) = 0x%08x (%v), want 0x%08x (%v)",
					tc.a, tc.rm, got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtU64ToF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

// ============================================================================
// Integer to F64 Conversion Tests
// ============================================================================

func TestCvtI32ToF64(t *testing.T) {
	tests := []struct {
		name     string
		a        int32
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions - all int32 values can be exactly represented in F64
		{"0", 0, RNE, posZero64, 0},
		{"1", 1, RNE, posOne64, 0},
		{"-1", -1, RNE, negOne64, 0},
		{"2", 2, RNE, posTwo64, 0},
		{"100", 100, RNE, f64bits(100.0), 0},
		{"-100", -100, RNE, f64bits(-100.0), 0},

		// Edge cases
		{"INT32_MAX", 0x7FFFFFFF, RNE, f64bits(2147483647.0), 0},
		{"INT32_MIN", -0x80000000, RNE, f64bits(-2147483648.0), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtI32ToF64(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtI32ToF64(%d, %v) = 0x%016x (%v), want 0x%016x (%v)",
					tc.a, tc.rm, got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtI32ToF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtU32ToF64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions - all uint32 values can be exactly represented in F64
		{"0", 0, RNE, posZero64, 0},
		{"1", 1, RNE, posOne64, 0},
		{"100", 100, RNE, f64bits(100.0), 0},
		{"UINT32_MAX", 0xFFFFFFFF, RNE, f64bits(4294967295.0), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtU32ToF64(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtU32ToF64(%d, %v) = 0x%016x (%v), want 0x%016x (%v)",
					tc.a, tc.rm, got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtU32ToF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtI64ToF64(t *testing.T) {
	tests := []struct {
		name     string
		a        int64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0", 0, RNE, posZero64, 0},
		{"1", 1, RNE, posOne64, 0},
		{"-1", -1, RNE, negOne64, 0},

		// Exact representations
		{"1e15", 1000000000000000, RNE, f64bits(1e15), 0},
		{"-1e15", -1000000000000000, RNE, f64bits(-1e15), 0},

		// Values requiring rounding (> 2^53)
		// 9007199254740993 = 2^53 + 1, cannot be exactly represented
		{"2^53+1", 9007199254740993, RNE, f64bits(9007199254740992.0), FlagInexact},

		// INT64_MIN (power of 2)
		{"INT64_MIN", -0x8000000000000000, RNE, f64bits(-9223372036854775808.0), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtI64ToF64(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtI64ToF64(%d, %v) = 0x%016x (%v), want 0x%016x (%v)",
					tc.a, tc.rm, got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtI64ToF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtU64ToF64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"0", 0, RNE, posZero64, 0},
		{"1", 1, RNE, posOne64, 0},
		{"1e15", 1000000000000000, RNE, f64bits(1e15), 0},

		// Values requiring rounding
		{"2^53+1", 9007199254740993, RNE, f64bits(9007199254740992.0), FlagInexact},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtU64ToF64(tc.a, tc.rm, &flags)

			if got != tc.wantBits {
				t.Errorf("CvtU64ToF64(%d, %v) = 0x%016x (%v), want 0x%016x (%v)",
					tc.a, tc.rm, got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtU64ToF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

// ============================================================================
// Float to Float Conversion Tests
// ============================================================================

func TestCvtF32ToF64(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"+0", posZero32, posZero64, 0},
		{"-0", negZero32, negZero64, 0},
		{"1.0", posOne32, posOne64, 0},
		{"-1.0", negOne32, negOne64, 0},
		{"2.0", posTwo32, posTwo64, 0},

		// Infinity
		{"+inf", posInf32, posInf64, 0},
		{"-inf", negInf32, negInf64, 0},

		// NaN
		{"qNaN", qNaN32, qNaN64, 0},
		{"sNaN", sNaN32, qNaN64, FlagInvalidOp},

		// Subnormals become normalized
		{"pos subnorm", posSubnorm32, f64bits(float64(math.Float32frombits(posSubnorm32))), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToF64(tc.a, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("CvtF32ToF64(0x%08x) = 0x%016x, want NaN",
						tc.a, got)
				}
			} else if got != tc.wantBits {
				t.Errorf("CvtF32ToF64(0x%08x) = 0x%016x (%v), want 0x%016x (%v)",
					tc.a, got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF32ToF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF64ToF32(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"+0", posZero64, RNE, posZero32, 0},
		{"-0", negZero64, RNE, negZero32, 0},
		{"1.0", posOne64, RNE, posOne32, 0},
		{"-1.0", negOne64, RNE, negOne32, 0},
		{"2.0", posTwo64, RNE, posTwo32, 0},

		// Infinity
		{"+inf", posInf64, RNE, posInf32, 0},
		{"-inf", negInf64, RNE, negInf32, 0},

		// NaN
		{"qNaN", qNaN64, RNE, qNaN32, 0},
		{"sNaN", sNaN64, RNE, qNaN32, FlagInvalidOp},

		// Values that lose precision
		{"1.0000001", f64bits(1.0000001), RNE, f32bits(1.0000001), FlagInexact},

		// Overflow to infinity
		{"too large", f64bits(1e39), RNE, posInf32, FlagOverflow | FlagInexact},
		{"too small", f64bits(-1e39), RNE, negInf32, FlagOverflow | FlagInexact},

		// Underflow to subnormal (1e-45 rounds to min subnormal)
		{"tiny positive", f64bits(1e-46), RNE, posZero32, FlagUnderflow | FlagInexact},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF64ToF32(tc.a, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("CvtF64ToF32(0x%016x) = 0x%08x, want NaN",
						tc.a, got)
				}
			} else if got != tc.wantBits {
				t.Errorf("CvtF64ToF32(0x%016x, %v) = 0x%08x (%v), want 0x%08x (%v)",
					tc.a, tc.rm, got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF64ToF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF64ToF128(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		wantHi   uint64 // High 64 bits of result
		wantLo   uint64 // Low 64 bits of result
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"+0", posZero64, 0x0000000000000000, 0, 0},
		{"-0", negZero64, 0x8000000000000000, 0, 0},
		{"1.0", posOne64, 0x3FFF000000000000, 0, 0},
		{"-1.0", negOne64, 0xBFFF000000000000, 0, 0},
		{"2.0", posTwo64, 0x4000000000000000, 0, 0},

		// Infinity
		{"+inf", posInf64, 0x7FFF000000000000, 0, 0},
		{"-inf", negInf64, 0xFFFF000000000000, 0, 0},

		// NaN
		{"qNaN", qNaN64, Float128QNaN.Hi, Float128QNaN.Lo, 0},
		{"sNaN", sNaN64, Float128QNaN.Hi, Float128QNaN.Lo, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF64ToF128(tc.a, &flags)
			want := uint128.New(tc.wantLo, tc.wantHi)

			if IsNaN128(want) {
				if !IsNaN128(got) {
					t.Errorf("CvtF64ToF128(0x%016x) = %v, want NaN",
						tc.a, got)
				}
			} else if got != want {
				t.Errorf("CvtF64ToF128(0x%016x) = %v, want %v",
					tc.a, got, want)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF64ToF128 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestCvtF128ToF64(t *testing.T) {
	tests := []struct {
		name     string
		aHi      uint64
		aLo      uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic conversions
		{"+0", 0x0000000000000000, 0, RNE, posZero64, 0},
		{"-0", 0x8000000000000000, 0, RNE, negZero64, 0},
		{"1.0", 0x3FFF000000000000, 0, RNE, posOne64, 0},
		{"-1.0", 0xBFFF000000000000, 0, RNE, negOne64, 0},
		{"2.0", 0x4000000000000000, 0, RNE, posTwo64, 0},

		// Infinity
		{"+inf", 0x7FFF000000000000, 0, RNE, posInf64, 0},
		{"-inf", 0xFFFF000000000000, 0, RNE, negInf64, 0},

		// NaN
		{"qNaN", Float128QNaN.Hi, Float128QNaN.Lo, RNE, qNaN64, 0},
		{"sNaN", 0x7FFF000000000000, 1, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			a := uint128.New(tc.aLo, tc.aHi)
			got := CvtF128ToF64(a, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("CvtF128ToF64(%v) = 0x%016x, want NaN",
						a, got)
				}
			} else if got != tc.wantBits {
				t.Errorf("CvtF128ToF64(%v, %v) = 0x%016x, want 0x%016x",
					a, tc.rm, got, tc.wantBits)
			}

			if flags != tc.wantFlag {
				t.Errorf("CvtF128ToF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestRoundTripF64F128(t *testing.T) {
	// All F64 values should round-trip through F128 exactly
	testValues := []uint64{posZero64, negZero64, posOne64, negOne64, posTwo64, posInf64, negInf64}

	for _, v := range testValues {
		var flags1, flags2 ExceptionFlags
		f128 := CvtF64ToF128(v, &flags1)
		back := CvtF128ToF64(f128, RNE, &flags2)

		if back != v {
			t.Errorf("Round-trip F64->F128->F64 failed: 0x%016x -> %v -> 0x%016x", v, f128, back)
		}
		if flags1 != 0 || flags2 != 0 {
			t.Errorf("Round-trip should be exact for 0x%016x, got flags1=%v flags2=%v", v, flags1, flags2)
		}
	}
}

// ============================================================================
// Round-trip Tests
// ============================================================================

func TestRoundTripI32F32(t *testing.T) {
	// Values that can be exactly represented in F32
	exactValues := []int32{0, 1, -1, 100, -100, 1024, -1024, 16777216, -16777216}

	for _, v := range exactValues {
		var flags1, flags2 ExceptionFlags
		f := CvtI32ToF32(v, RNE, &flags1)
		back := CvtF32ToI32(f, RNE, &flags2)

		if back != v {
			t.Errorf("Round-trip I32->F32->I32 failed: %d -> 0x%08x -> %d", v, f, back)
		}
		if flags1 != 0 || flags2 != 0 {
			t.Errorf("Round-trip should be exact for %d, got flags1=%v flags2=%v", v, flags1, flags2)
		}
	}
}

func TestRoundTripI32F64(t *testing.T) {
	// All int32 values can be exactly represented in F64
	testValues := []int32{0, 1, -1, 100, -100, 0x7FFFFFFF, -0x80000000, 12345678}

	for _, v := range testValues {
		var flags1, flags2 ExceptionFlags
		f := CvtI32ToF64(v, RNE, &flags1)
		back := CvtF64ToI32(f, RNE, &flags2)

		if back != v {
			t.Errorf("Round-trip I32->F64->I32 failed: %d -> 0x%016x -> %d", v, f, back)
		}
		if flags1 != 0 || flags2 != 0 {
			t.Errorf("Round-trip should be exact for %d, got flags1=%v flags2=%v", v, flags1, flags2)
		}
	}
}

func TestRoundTripF32F64(t *testing.T) {
	// Normal F32 values should round-trip exactly through F64
	testValues := []uint32{posZero32, negZero32, posOne32, negOne32, posTwo32, f32bits(3.14159)}

	for _, v := range testValues {
		var flags1, flags2 ExceptionFlags
		f64 := CvtF32ToF64(v, &flags1)
		back := CvtF64ToF32(f64, RNE, &flags2)

		if back != v {
			t.Errorf("Round-trip F32->F64->F32 failed: 0x%08x -> 0x%016x -> 0x%08x", v, f64, back)
		}
	}
}

// ============================================================================
// Rounding Mode Tests for Conversions
// ============================================================================

func TestCvtF32ToI32RoundingModes(t *testing.T) {
	// Test value: 1.6 (between 1 and 2)
	val := f32bits(1.6)

	tests := []struct {
		rm   RoundingMode
		want int32
	}{
		{RNE, 2}, // Round to nearest even -> 2
		{RTZ, 1}, // Truncate -> 1
		{RDN, 1}, // Round down -> 1
		{RUP, 2}, // Round up -> 2
		{RMM, 2}, // Round to magnitude -> 2
	}

	rmNames := []string{"RNE", "RTZ", "RDN", "RUP", "RMM"}
	for i, tc := range tests {
		t.Run(rmNames[i], func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToI32(val, tc.rm, &flags)
			if got != tc.want {
				t.Errorf("CvtF32ToI32(1.6, %v) = %d, want %d", tc.rm, got, tc.want)
			}
		})
	}
}

func TestCvtF32ToI32NegativeRoundingModes(t *testing.T) {
	// Test value: -1.6 (between -2 and -1)
	val := f32bits(-1.6)

	tests := []struct {
		rm   RoundingMode
		want int32
	}{
		{RNE, -2}, // Round to nearest even -> -2
		{RTZ, -1}, // Truncate toward zero -> -1
		{RDN, -2}, // Round down (toward -inf) -> -2
		{RUP, -1}, // Round up (toward +inf) -> -1
		{RMM, -2}, // Round to magnitude -> -2
	}

	rmNames := []string{"RNE", "RTZ", "RDN", "RUP", "RMM"}
	for i, tc := range tests {
		t.Run(rmNames[i], func(t *testing.T) {
			var flags ExceptionFlags
			got := CvtF32ToI32(val, tc.rm, &flags)
			if got != tc.want {
				t.Errorf("CvtF32ToI32(-1.6, %v) = %d, want %d", tc.rm, got, tc.want)
			}
		})
	}
}
