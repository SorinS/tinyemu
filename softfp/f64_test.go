package softfp

import (
	"math"
	"testing"
)

// Helper to convert float64 to uint64 bit pattern
func f64bits(f float64) uint64 {
	return math.Float64bits(f)
}

// Helper to convert uint64 bit pattern to float64
func f64val(bits uint64) float64 {
	return math.Float64frombits(bits)
}

// Common test values for F64
var (
	posZero64    = f64bits(0.0)
	negZero64    = f64bits(math.Copysign(0, -1))
	posOne64     = f64bits(1.0)
	negOne64     = f64bits(-1.0)
	posTwo64     = f64bits(2.0)
	negTwo64     = f64bits(-2.0)
	posInf64     = f64bits(math.Inf(1))
	negInf64     = f64bits(math.Inf(-1))
	qNaN64       = Float64QNaN
	sNaN64       = uint64(0x7FF0000000000001) // Signaling NaN
	posSubnorm64 = uint64(0x0000000000000001)
	negSubnorm64 = uint64(0x8000000000000001)
	posMaxNorm64 = uint64(0x7FEFFFFFFFFFFFFF) // Max finite positive
	negMaxNorm64 = uint64(0xFFEFFFFFFFFFFFFF) // Max finite negative
	posMinNorm64 = uint64(0x0010000000000000) // Min positive normal
)

func TestAddF64Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic arithmetic
		{"1+1", posOne64, posOne64, RNE, posTwo64, 0},
		{"1+0", posOne64, posZero64, RNE, posOne64, 0},
		{"0+0", posZero64, posZero64, RNE, posZero64, 0},
		{"-0+-0", negZero64, negZero64, RNE, negZero64, 0},
		{"1+(-1)", posOne64, negOne64, RNE, posZero64, 0},

		// Infinity
		{"inf+1", posInf64, posOne64, RNE, posInf64, 0},
		{"-inf+1", negInf64, posOne64, RNE, negInf64, 0},
		{"inf+-inf", posInf64, negInf64, RNE, qNaN64, FlagInvalidOp},

		// NaN
		{"NaN+1", qNaN64, posOne64, RNE, qNaN64, 0},
		{"1+NaN", posOne64, qNaN64, RNE, qNaN64, 0},
		{"sNaN+1", sNaN64, posOne64, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := AddF64(tc.a, tc.b, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("AddF64(%v, %v, %v) = 0x%016x (%v), want NaN",
						f64val(tc.a), f64val(tc.b), tc.rm, got, f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("AddF64(%v, %v, %v) = 0x%016x (%v), want 0x%016x (%v)",
					f64val(tc.a), f64val(tc.b), tc.rm, got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("AddF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSubF64Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		{"2-1", posTwo64, posOne64, RNE, posOne64, 0},
		{"1-1", posOne64, posOne64, RNE, posZero64, 0},
		{"0-0", posZero64, posZero64, RNE, posZero64, 0},
		{"inf-inf", posInf64, posInf64, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := SubF64(tc.a, tc.b, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("SubF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("SubF64(%v, %v) = %v, want %v",
					f64val(tc.a), f64val(tc.b), f64val(got), f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("SubF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestMulF64Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic multiplication
		{"1*1", posOne64, posOne64, RNE, posOne64, 0},
		{"2*1", posTwo64, posOne64, RNE, posTwo64, 0},
		{"2*2", posTwo64, posTwo64, RNE, f64bits(4.0), 0},
		{"-1*1", negOne64, posOne64, RNE, negOne64, 0},
		{"-1*-1", negOne64, negOne64, RNE, posOne64, 0},

		// Zero
		{"0*1", posZero64, posOne64, RNE, posZero64, 0},
		{"0*-1", posZero64, negOne64, RNE, negZero64, 0},
		{"-0*1", negZero64, posOne64, RNE, negZero64, 0},
		{"-0*-1", negZero64, negOne64, RNE, posZero64, 0},

		// Infinity
		{"inf*2", posInf64, posTwo64, RNE, posInf64, 0},
		{"inf*-2", posInf64, negTwo64, RNE, negInf64, 0},
		{"-inf*2", negInf64, posTwo64, RNE, negInf64, 0},
		{"inf*0", posInf64, posZero64, RNE, qNaN64, FlagInvalidOp},

		// NaN
		{"NaN*1", qNaN64, posOne64, RNE, qNaN64, 0},
		{"sNaN*1", sNaN64, posOne64, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := MulF64(tc.a, tc.b, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("MulF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("MulF64(%v, %v) = 0x%016x (%v), want 0x%016x (%v)",
					f64val(tc.a), f64val(tc.b), got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("MulF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestDivF64Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic division
		{"1/1", posOne64, posOne64, RNE, posOne64, 0},
		{"2/1", posTwo64, posOne64, RNE, posTwo64, 0},
		{"4/2", f64bits(4.0), posTwo64, RNE, posTwo64, 0},
		{"-4/2", f64bits(-4.0), posTwo64, RNE, negTwo64, 0},
		{"-4/-2", f64bits(-4.0), negTwo64, RNE, posTwo64, 0},

		// Zero dividend
		{"0/1", posZero64, posOne64, RNE, posZero64, 0},
		{"-0/1", negZero64, posOne64, RNE, negZero64, 0},
		{"0/-1", posZero64, negOne64, RNE, negZero64, 0},

		// Zero divisor
		{"1/0", posOne64, posZero64, RNE, posInf64, FlagDivideZero},
		{"-1/0", negOne64, posZero64, RNE, negInf64, FlagDivideZero},
		{"1/-0", posOne64, negZero64, RNE, negInf64, FlagDivideZero},
		{"0/0", posZero64, posZero64, RNE, qNaN64, FlagInvalidOp},

		// Infinity
		{"inf/2", posInf64, posTwo64, RNE, posInf64, 0},
		{"2/inf", posTwo64, posInf64, RNE, posZero64, 0},
		{"inf/inf", posInf64, posInf64, RNE, qNaN64, FlagInvalidOp},

		// NaN
		{"NaN/1", qNaN64, posOne64, RNE, qNaN64, 0},
		{"1/NaN", posOne64, qNaN64, RNE, qNaN64, 0},
		{"sNaN/1", sNaN64, posOne64, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := DivF64(tc.a, tc.b, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("DivF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("DivF64(%v, %v) = 0x%016x (%v), want 0x%016x (%v)",
					f64val(tc.a), f64val(tc.b), got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("DivF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSqrtF64Basic(t *testing.T) {
	tests := []struct {
		name     string
		a        uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Perfect squares
		{"sqrt(0)", posZero64, RNE, posZero64, 0},
		{"sqrt(-0)", negZero64, RNE, negZero64, 0},
		{"sqrt(1)", posOne64, RNE, posOne64, 0},
		{"sqrt(4)", f64bits(4.0), RNE, posTwo64, 0},

		// Infinity
		{"sqrt(inf)", posInf64, RNE, posInf64, 0},
		{"sqrt(-inf)", negInf64, RNE, qNaN64, FlagInvalidOp},

		// Negative
		{"sqrt(-1)", negOne64, RNE, qNaN64, FlagInvalidOp},

		// NaN
		{"sqrt(NaN)", qNaN64, RNE, qNaN64, 0},
		{"sqrt(sNaN)", sNaN64, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := SqrtF64(tc.a, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("SqrtF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("SqrtF64(%v) = 0x%016x (%v), want 0x%016x (%v)",
					f64val(tc.a), got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("SqrtF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSqrtF64Approximation(t *testing.T) {
	// Test sqrt(2) which is not a perfect square
	var flags ExceptionFlags
	result := SqrtF64(posTwo64, RNE, &flags)

	// sqrt(2) ~= 1.41421356237...
	expected := 1.4142135623730951
	got := f64val(result)

	// Allow small error (should be within 1 ULP)
	diff := got - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-15 {
		t.Errorf("SqrtF64(2) = %v, want approximately %v", got, expected)
	}

	// Should be inexact
	if flags&FlagInexact == 0 {
		t.Errorf("SqrtF64(2) should set inexact flag")
	}
}

func TestFmaF64Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b, c  uint64
		rm       RoundingMode
		wantBits uint64
		wantFlag ExceptionFlags
	}{
		// Basic FMA
		{"1*1+0", posOne64, posOne64, posZero64, RNE, posOne64, 0},
		{"2*2+0", posTwo64, posTwo64, posZero64, RNE, f64bits(4.0), 0},
		{"2*2+1", posTwo64, posTwo64, posOne64, RNE, f64bits(5.0), 0},
		{"2*2-1", posTwo64, posTwo64, negOne64, RNE, f64bits(3.0), 0},

		// Zero multiplicand
		{"0*2+1", posZero64, posTwo64, posOne64, RNE, posOne64, 0},
		{"2*0+1", posTwo64, posZero64, posOne64, RNE, posOne64, 0},
		{"0*0+0", posZero64, posZero64, posZero64, RNE, posZero64, 0},

		// Infinity cases
		{"inf*2+1", posInf64, posTwo64, posOne64, RNE, posInf64, 0},
		{"inf*0+1", posInf64, posZero64, posOne64, RNE, qNaN64, FlagInvalidOp},
		{"2*2+inf", posTwo64, posTwo64, posInf64, RNE, posInf64, 0},

		// NaN cases
		{"NaN*2+1", qNaN64, posTwo64, posOne64, RNE, qNaN64, 0},
		{"2*NaN+1", posTwo64, qNaN64, posOne64, RNE, qNaN64, 0},
		{"2*2+NaN", posTwo64, posTwo64, qNaN64, RNE, qNaN64, 0},
		{"sNaN*2+1", sNaN64, posTwo64, posOne64, RNE, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := FmaF64(tc.a, tc.b, tc.c, tc.rm, &flags)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("FmaF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("FmaF64(%v, %v, %v) = 0x%016x (%v), want 0x%016x (%v)",
					f64val(tc.a), f64val(tc.b), f64val(tc.c), got, f64val(got), tc.wantBits, f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("FmaF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestMinF64(t *testing.T) {
	tests := []struct {
		name       string
		a, b       uint64
		minMaxType MinMaxType
		wantBits   uint64
		wantFlag   ExceptionFlags
	}{
		{"min(1,2)", posOne64, posTwo64, MinMaxIEEE754_2008, posOne64, 0},
		{"min(2,1)", posTwo64, posOne64, MinMaxIEEE754_2008, posOne64, 0},
		{"min(-1,1)", negOne64, posOne64, MinMaxIEEE754_2008, negOne64, 0},
		{"min(-1,-2)", negOne64, negTwo64, MinMaxIEEE754_2008, negTwo64, 0},
		{"min(0,-0)", posZero64, negZero64, MinMaxIEEE754_2008, negZero64, 0},
		{"min(-0,0)", negZero64, posZero64, MinMaxIEEE754_2008, negZero64, 0},
		{"min(inf,-inf)", posInf64, negInf64, MinMaxIEEE754_2008, negInf64, 0},

		// NaN handling
		{"min(NaN,1)", qNaN64, posOne64, MinMaxIEEE754_2008, posOne64, 0},
		{"min(1,NaN)", posOne64, qNaN64, MinMaxIEEE754_2008, posOne64, 0},
		{"min(NaN,NaN)", qNaN64, qNaN64, MinMaxIEEE754_2008, qNaN64, 0},
		{"min(sNaN,1)", sNaN64, posOne64, MinMaxIEEE754_2008, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := MinF64(tc.a, tc.b, &flags, tc.minMaxType)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("MinF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("MinF64(%v, %v) = %v, want %v",
					f64val(tc.a), f64val(tc.b), f64val(got), f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("MinF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestMaxF64(t *testing.T) {
	tests := []struct {
		name       string
		a, b       uint64
		minMaxType MinMaxType
		wantBits   uint64
		wantFlag   ExceptionFlags
	}{
		{"max(1,2)", posOne64, posTwo64, MinMaxIEEE754_2008, posTwo64, 0},
		{"max(2,1)", posTwo64, posOne64, MinMaxIEEE754_2008, posTwo64, 0},
		{"max(-1,1)", negOne64, posOne64, MinMaxIEEE754_2008, posOne64, 0},
		{"max(-1,-2)", negOne64, negTwo64, MinMaxIEEE754_2008, negOne64, 0},
		{"max(0,-0)", posZero64, negZero64, MinMaxIEEE754_2008, posZero64, 0},
		{"max(-0,0)", negZero64, posZero64, MinMaxIEEE754_2008, posZero64, 0},
		{"max(inf,-inf)", posInf64, negInf64, MinMaxIEEE754_2008, posInf64, 0},

		// NaN handling
		{"max(NaN,1)", qNaN64, posOne64, MinMaxIEEE754_2008, posOne64, 0},
		{"max(1,NaN)", posOne64, qNaN64, MinMaxIEEE754_2008, posOne64, 0},
		{"max(NaN,NaN)", qNaN64, qNaN64, MinMaxIEEE754_2008, qNaN64, 0},
		{"max(sNaN,1)", sNaN64, posOne64, MinMaxIEEE754_2008, qNaN64, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := MaxF64(tc.a, tc.b, &flags, tc.minMaxType)

			if IsNaN64(tc.wantBits) {
				if !IsNaN64(got) {
					t.Errorf("MaxF64 got %v, want NaN", f64val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("MaxF64(%v, %v) = %v, want %v",
					f64val(tc.a), f64val(tc.b), f64val(got), f64val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("MaxF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestEqQuietF64(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		want     bool
		wantFlag ExceptionFlags
	}{
		{"1==1", posOne64, posOne64, true, 0},
		{"1==2", posOne64, posTwo64, false, 0},
		{"0==-0", posZero64, negZero64, true, 0},
		{"-0==0", negZero64, posZero64, true, 0},
		{"inf==inf", posInf64, posInf64, true, 0},
		{"-inf==-inf", negInf64, negInf64, true, 0},
		{"inf==-inf", posInf64, negInf64, false, 0},

		// NaN comparisons
		{"NaN==1", qNaN64, posOne64, false, 0},
		{"1==NaN", posOne64, qNaN64, false, 0},
		{"NaN==NaN", qNaN64, qNaN64, false, 0},
		{"sNaN==1", sNaN64, posOne64, false, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := EqQuietF64(tc.a, tc.b, &flags)

			if got != tc.want {
				t.Errorf("EqQuietF64(%v, %v) = %v, want %v",
					f64val(tc.a), f64val(tc.b), got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("EqQuietF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestLeF64(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		want     bool
		wantFlag ExceptionFlags
	}{
		{"1<=1", posOne64, posOne64, true, 0},
		{"1<=2", posOne64, posTwo64, true, 0},
		{"2<=1", posTwo64, posOne64, false, 0},
		{"-1<=1", negOne64, posOne64, true, 0},
		{"1<=-1", posOne64, negOne64, false, 0},
		{"0<=-0", posZero64, negZero64, true, 0},
		{"-0<=0", negZero64, posZero64, true, 0},
		{"-inf<=inf", negInf64, posInf64, true, 0},
		{"inf<=-inf", posInf64, negInf64, false, 0},

		// NaN comparisons (signaling)
		{"NaN<=1", qNaN64, posOne64, false, FlagInvalidOp},
		{"1<=NaN", posOne64, qNaN64, false, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := LeF64(tc.a, tc.b, &flags)

			if got != tc.want {
				t.Errorf("LeF64(%v, %v) = %v, want %v",
					f64val(tc.a), f64val(tc.b), got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("LeF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestLtF64(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		want     bool
		wantFlag ExceptionFlags
	}{
		{"1<1", posOne64, posOne64, false, 0},
		{"1<2", posOne64, posTwo64, true, 0},
		{"2<1", posTwo64, posOne64, false, 0},
		{"-1<1", negOne64, posOne64, true, 0},
		{"1<-1", posOne64, negOne64, false, 0},
		{"0<-0", posZero64, negZero64, false, 0},
		{"-0<0", negZero64, posZero64, false, 0},
		{"-inf<inf", negInf64, posInf64, true, 0},
		{"inf<-inf", posInf64, negInf64, false, 0},

		// NaN comparisons (signaling)
		{"NaN<1", qNaN64, posOne64, false, FlagInvalidOp},
		{"1<NaN", posOne64, qNaN64, false, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := LtF64(tc.a, tc.b, &flags)

			if got != tc.want {
				t.Errorf("LtF64(%v, %v) = %v, want %v",
					f64val(tc.a), f64val(tc.b), got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("LtF64 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSignF64(t *testing.T) {
	tests := []struct {
		name string
		a, b uint64
		want uint64
	}{
		{"sign(1,1)", posOne64, posOne64, posOne64},
		{"sign(1,-1)", posOne64, negOne64, negOne64},
		{"sign(-1,1)", negOne64, posOne64, posOne64},
		{"sign(-1,-1)", negOne64, negOne64, negOne64},
		{"sign(0,-0)", posZero64, negZero64, negZero64},
		{"sign(-0,0)", negZero64, posZero64, posZero64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SignF64(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("SignF64(0x%016x, 0x%016x) = 0x%016x, want 0x%016x",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSignNF64(t *testing.T) {
	tests := []struct {
		name string
		a, b uint64
		want uint64
	}{
		{"signn(1,1)", posOne64, posOne64, negOne64},
		{"signn(1,-1)", posOne64, negOne64, posOne64},
		{"signn(-1,1)", negOne64, posOne64, negOne64},
		{"signn(-1,-1)", negOne64, negOne64, posOne64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SignNF64(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("SignNF64(0x%016x, 0x%016x) = 0x%016x, want 0x%016x",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSignXF64(t *testing.T) {
	tests := []struct {
		name string
		a, b uint64
		want uint64
	}{
		{"signx(1,1)", posOne64, posOne64, posOne64},
		{"signx(1,-1)", posOne64, negOne64, negOne64},
		{"signx(-1,1)", negOne64, posOne64, negOne64},
		{"signx(-1,-1)", negOne64, negOne64, posOne64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SignXF64(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("SignXF64(0x%016x, 0x%016x) = 0x%016x, want 0x%016x",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestMulU64(t *testing.T) {
	tests := []struct {
		name           string
		a, b           uint64
		wantHi, wantLo uint64
	}{
		{"0*0", 0, 0, 0, 0},
		{"1*1", 1, 1, 0, 1},
		{"max*1", 0xFFFFFFFFFFFFFFFF, 1, 0, 0xFFFFFFFFFFFFFFFF},
		{"max*2", 0xFFFFFFFFFFFFFFFF, 2, 1, 0xFFFFFFFFFFFFFFFE},
		// (2^64-1)^2 = 2^128 - 2*2^64 + 1 = (2^64-2) * 2^64 + 1
		{"max*max", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotHi, gotLo := mulU64(tc.a, tc.b)
			if gotHi != tc.wantHi || gotLo != tc.wantLo {
				t.Errorf("mulU64(%d, %d) = (%d, %d), want (%d, %d)",
					tc.a, tc.b, gotHi, gotLo, tc.wantHi, tc.wantLo)
			}
		})
	}
}

func TestAddF64Subnormal(t *testing.T) {
	var flags ExceptionFlags

	// Add two subnormals
	result := AddF64(posSubnorm64, posSubnorm64, RNE, &flags)

	// Result should be a small positive number (2 * min_subnormal)
	if result == 0 {
		t.Error("AddF64 of two subnormals should not be zero")
	}
	if result&Float64SignMask != 0 {
		t.Error("AddF64 of two positive subnormals should be positive")
	}
}

func TestMulF64Overflow(t *testing.T) {
	var flags ExceptionFlags

	// Multiply max normal by 2 should overflow to infinity
	result := MulF64(posMaxNorm64, posTwo64, RNE, &flags)

	if result != posInf64 {
		t.Errorf("MulF64(maxNorm, 2) = 0x%016x, want +inf (0x%016x)", result, posInf64)
	}

	if flags&FlagOverflow == 0 {
		t.Error("MulF64 overflow should set overflow flag")
	}
}

// TestRoundingModes64 tests that different rounding modes produce correct results
func TestRoundingModes64(t *testing.T) {
	// 1/3 cannot be represented exactly, so rounding mode matters
	oneThird := DivF64(posOne64, f64bits(3.0), RNE, new(ExceptionFlags))
	oneThirdRTZ := DivF64(posOne64, f64bits(3.0), RTZ, new(ExceptionFlags))
	oneThirdRUP := DivF64(posOne64, f64bits(3.0), RUP, new(ExceptionFlags))
	oneThirdRDN := DivF64(posOne64, f64bits(3.0), RDN, new(ExceptionFlags))

	// RTZ should truncate towards zero (same as RDN for positive)
	if oneThirdRTZ != oneThirdRDN {
		t.Errorf("RTZ and RDN should give same result for positive 1/3: RTZ=0x%016x, RDN=0x%016x",
			oneThirdRTZ, oneThirdRDN)
	}

	// RUP should be >= RNE for positive numbers
	if f64val(oneThirdRUP) < f64val(oneThird) {
		t.Errorf("RUP result should be >= RNE result for positive 1/3")
	}

	// RDN should be <= RNE for positive numbers
	if f64val(oneThirdRDN) > f64val(oneThird) {
		t.Errorf("RDN result should be <= RNE result for positive 1/3")
	}
}
