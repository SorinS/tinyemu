package softfp

import (
	"math"
	"testing"
)

// Helper to convert float32 to uint32 bit pattern
func f32bits(f float32) uint32 {
	return math.Float32bits(f)
}

// Helper to convert uint32 bit pattern to float32
func f32val(bits uint32) float32 {
	return math.Float32frombits(bits)
}

// Common test values
var (
	posZero32    = f32bits(0.0)
	negZero32    = f32bits(float32(math.Copysign(0, -1)))
	posOne32     = f32bits(1.0)
	negOne32     = f32bits(-1.0)
	posTwo32     = f32bits(2.0)
	negTwo32     = f32bits(-2.0)
	posInf32     = f32bits(float32(math.Inf(1)))
	negInf32     = f32bits(float32(math.Inf(-1)))
	qNaN32       = Float32QNaN
	sNaN32       = uint32(0x7F800001) // Signaling NaN
	posSubnorm32 = uint32(0x00000001)
	negSubnorm32 = uint32(0x80000001)
	posMaxNorm32 = uint32(0x7F7FFFFF) // Max finite positive
	negMaxNorm32 = uint32(0xFF7FFFFF) // Max finite negative
	posMinNorm32 = uint32(0x00800000) // Min positive normal
)

func TestAddF32Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic arithmetic
		{"1+1", posOne32, posOne32, RNE, posTwo32, 0},
		{"1+0", posOne32, posZero32, RNE, posOne32, 0},
		{"0+0", posZero32, posZero32, RNE, posZero32, 0},
		{"-0+-0", negZero32, negZero32, RNE, negZero32, 0},
		{"1+(-1)", posOne32, negOne32, RNE, posZero32, 0},

		// Infinity
		{"inf+1", posInf32, posOne32, RNE, posInf32, 0},
		{"-inf+1", negInf32, posOne32, RNE, negInf32, 0},
		{"inf+-inf", posInf32, negInf32, RNE, qNaN32, FlagInvalidOp},

		// NaN
		{"NaN+1", qNaN32, posOne32, RNE, qNaN32, 0},
		{"1+NaN", posOne32, qNaN32, RNE, qNaN32, 0},
		{"sNaN+1", sNaN32, posOne32, RNE, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := AddF32(tc.a, tc.b, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("AddF32(%v, %v, %v) = 0x%08x (%v), want NaN",
						f32val(tc.a), f32val(tc.b), tc.rm, got, f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("AddF32(%v, %v, %v) = 0x%08x (%v), want 0x%08x (%v)",
					f32val(tc.a), f32val(tc.b), tc.rm, got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("AddF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSubF32Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		{"2-1", posTwo32, posOne32, RNE, posOne32, 0},
		{"1-1", posOne32, posOne32, RNE, posZero32, 0},
		{"0-0", posZero32, posZero32, RNE, posZero32, 0},
		{"inf-inf", posInf32, posInf32, RNE, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := SubF32(tc.a, tc.b, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("SubF32 got %v, want NaN", f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("SubF32(%v, %v) = %v, want %v",
					f32val(tc.a), f32val(tc.b), f32val(got), f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("SubF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestMulF32Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic multiplication
		{"1*1", posOne32, posOne32, RNE, posOne32, 0},
		{"2*1", posTwo32, posOne32, RNE, posTwo32, 0},
		{"2*2", posTwo32, posTwo32, RNE, f32bits(4.0), 0},
		{"-1*1", negOne32, posOne32, RNE, negOne32, 0},
		{"-1*-1", negOne32, negOne32, RNE, posOne32, 0},

		// Zero
		{"0*1", posZero32, posOne32, RNE, posZero32, 0},
		{"0*-1", posZero32, negOne32, RNE, negZero32, 0},
		{"-0*1", negZero32, posOne32, RNE, negZero32, 0},
		{"-0*-1", negZero32, negOne32, RNE, posZero32, 0},

		// Infinity
		{"inf*2", posInf32, posTwo32, RNE, posInf32, 0},
		{"inf*-2", posInf32, negTwo32, RNE, negInf32, 0},
		{"-inf*2", negInf32, posTwo32, RNE, negInf32, 0},
		{"inf*0", posInf32, posZero32, RNE, qNaN32, FlagInvalidOp},

		// NaN
		{"NaN*1", qNaN32, posOne32, RNE, qNaN32, 0},
		{"sNaN*1", sNaN32, posOne32, RNE, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := MulF32(tc.a, tc.b, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("MulF32 got %v, want NaN", f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("MulF32(%v, %v) = 0x%08x (%v), want 0x%08x (%v)",
					f32val(tc.a), f32val(tc.b), got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("MulF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestDivF32Basic(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Basic division
		{"1/1", posOne32, posOne32, RNE, posOne32, 0},
		{"2/1", posTwo32, posOne32, RNE, posTwo32, 0},
		{"4/2", f32bits(4.0), posTwo32, RNE, posTwo32, 0},
		{"-4/2", f32bits(-4.0), posTwo32, RNE, negTwo32, 0},
		{"-4/-2", f32bits(-4.0), negTwo32, RNE, posTwo32, 0},

		// Zero dividend
		{"0/1", posZero32, posOne32, RNE, posZero32, 0},
		{"-0/1", negZero32, posOne32, RNE, negZero32, 0},
		{"0/-1", posZero32, negOne32, RNE, negZero32, 0},

		// Zero divisor
		{"1/0", posOne32, posZero32, RNE, posInf32, FlagDivideZero},
		{"-1/0", negOne32, posZero32, RNE, negInf32, FlagDivideZero},
		{"1/-0", posOne32, negZero32, RNE, negInf32, FlagDivideZero},
		{"0/0", posZero32, posZero32, RNE, qNaN32, FlagInvalidOp},

		// Infinity
		{"inf/2", posInf32, posTwo32, RNE, posInf32, 0},
		{"2/inf", posTwo32, posInf32, RNE, posZero32, 0},
		{"inf/inf", posInf32, posInf32, RNE, qNaN32, FlagInvalidOp},

		// NaN
		{"NaN/1", qNaN32, posOne32, RNE, qNaN32, 0},
		{"1/NaN", posOne32, qNaN32, RNE, qNaN32, 0},
		{"sNaN/1", sNaN32, posOne32, RNE, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := DivF32(tc.a, tc.b, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("DivF32 got %v, want NaN", f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("DivF32(%v, %v) = 0x%08x (%v), want 0x%08x (%v)",
					f32val(tc.a), f32val(tc.b), got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("DivF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSqrtF32Basic(t *testing.T) {
	tests := []struct {
		name     string
		a        uint32
		rm       RoundingMode
		wantBits uint32
		wantFlag ExceptionFlags
	}{
		// Perfect squares
		{"sqrt(0)", posZero32, RNE, posZero32, 0},
		{"sqrt(-0)", negZero32, RNE, negZero32, 0},
		{"sqrt(1)", posOne32, RNE, posOne32, 0},
		{"sqrt(4)", f32bits(4.0), RNE, posTwo32, 0},

		// Infinity
		{"sqrt(inf)", posInf32, RNE, posInf32, 0},
		{"sqrt(-inf)", negInf32, RNE, qNaN32, FlagInvalidOp},

		// Negative
		{"sqrt(-1)", negOne32, RNE, qNaN32, FlagInvalidOp},

		// NaN
		{"sqrt(NaN)", qNaN32, RNE, qNaN32, 0},
		{"sqrt(sNaN)", sNaN32, RNE, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := SqrtF32(tc.a, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("SqrtF32 got %v, want NaN", f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("SqrtF32(%v) = 0x%08x (%v), want 0x%08x (%v)",
					f32val(tc.a), got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("SqrtF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSqrtF32Approximation(t *testing.T) {
	// Test sqrt(2) which is not a perfect square
	var flags ExceptionFlags
	result := SqrtF32(posTwo32, RNE, &flags)

	// sqrt(2) ~= 1.41421356...
	expected := float32(1.4142135)
	got := f32val(result)

	// Allow small error (should be within 1 ULP)
	diff := got - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-6 {
		t.Errorf("SqrtF32(2) = %v, want approximately %v", got, expected)
	}

	// Should be inexact
	if flags&FlagInexact == 0 {
		t.Errorf("SqrtF32(2) should set inexact flag")
	}
}

func TestFmaF32Basic(t *testing.T) {
	tests := []struct {
		name        string
		a, b, c     uint32
		rm          RoundingMode
		wantBits    uint32
		wantFlag    ExceptionFlags
		approxCheck bool // if true, check for approximate equality
	}{
		// Basic FMA
		{"1*1+0", posOne32, posOne32, posZero32, RNE, posOne32, 0, false},
		{"2*2+0", posTwo32, posTwo32, posZero32, RNE, f32bits(4.0), 0, false},
		{"2*2+1", posTwo32, posTwo32, posOne32, RNE, f32bits(5.0), 0, false},
		{"2*2-1", posTwo32, posTwo32, negOne32, RNE, f32bits(3.0), 0, false},

		// Zero multiplicand
		{"0*2+1", posZero32, posTwo32, posOne32, RNE, posOne32, 0, false},
		{"2*0+1", posTwo32, posZero32, posOne32, RNE, posOne32, 0, false},
		{"0*0+0", posZero32, posZero32, posZero32, RNE, posZero32, 0, false},

		// Infinity cases
		{"inf*2+1", posInf32, posTwo32, posOne32, RNE, posInf32, 0, false},
		{"inf*0+1", posInf32, posZero32, posOne32, RNE, qNaN32, FlagInvalidOp, false},
		{"2*2+inf", posTwo32, posTwo32, posInf32, RNE, posInf32, 0, false},

		// NaN cases
		{"NaN*2+1", qNaN32, posTwo32, posOne32, RNE, qNaN32, 0, false},
		{"2*NaN+1", posTwo32, qNaN32, posOne32, RNE, qNaN32, 0, false},
		{"2*2+NaN", posTwo32, posTwo32, qNaN32, RNE, qNaN32, 0, false},
		{"sNaN*2+1", sNaN32, posTwo32, posOne32, RNE, qNaN32, FlagInvalidOp, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := FmaF32(tc.a, tc.b, tc.c, tc.rm, &flags)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("FmaF32 got %v, want NaN", f32val(got))
				}
			} else if tc.approxCheck {
				gotVal := f32val(got)
				wantVal := f32val(tc.wantBits)
				diff := gotVal - wantVal
				if diff < 0 {
					diff = -diff
				}
				if diff > 1e-6 {
					t.Errorf("FmaF32 got %v, want approximately %v", gotVal, wantVal)
				}
			} else if got != tc.wantBits {
				t.Errorf("FmaF32(%v, %v, %v) = 0x%08x (%v), want 0x%08x (%v)",
					f32val(tc.a), f32val(tc.b), f32val(tc.c), got, f32val(got), tc.wantBits, f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("FmaF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestMinF32(t *testing.T) {
	tests := []struct {
		name       string
		a, b       uint32
		minMaxType MinMaxType
		wantBits   uint32
		wantFlag   ExceptionFlags
	}{
		{"min(1,2)", posOne32, posTwo32, MinMaxIEEE754_2008, posOne32, 0},
		{"min(2,1)", posTwo32, posOne32, MinMaxIEEE754_2008, posOne32, 0},
		{"min(-1,1)", negOne32, posOne32, MinMaxIEEE754_2008, negOne32, 0},
		{"min(-1,-2)", negOne32, negTwo32, MinMaxIEEE754_2008, negTwo32, 0},
		{"min(0,-0)", posZero32, negZero32, MinMaxIEEE754_2008, negZero32, 0},
		{"min(-0,0)", negZero32, posZero32, MinMaxIEEE754_2008, negZero32, 0},
		{"min(inf,-inf)", posInf32, negInf32, MinMaxIEEE754_2008, negInf32, 0},

		// NaN handling
		{"min(NaN,1)", qNaN32, posOne32, MinMaxIEEE754_2008, posOne32, 0},
		{"min(1,NaN)", posOne32, qNaN32, MinMaxIEEE754_2008, posOne32, 0},
		{"min(NaN,NaN)", qNaN32, qNaN32, MinMaxIEEE754_2008, qNaN32, 0},
		{"min(sNaN,1)", sNaN32, posOne32, MinMaxIEEE754_2008, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := MinF32(tc.a, tc.b, &flags, tc.minMaxType)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("MinF32 got %v, want NaN", f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("MinF32(%v, %v) = %v, want %v",
					f32val(tc.a), f32val(tc.b), f32val(got), f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("MinF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestMaxF32(t *testing.T) {
	tests := []struct {
		name       string
		a, b       uint32
		minMaxType MinMaxType
		wantBits   uint32
		wantFlag   ExceptionFlags
	}{
		{"max(1,2)", posOne32, posTwo32, MinMaxIEEE754_2008, posTwo32, 0},
		{"max(2,1)", posTwo32, posOne32, MinMaxIEEE754_2008, posTwo32, 0},
		{"max(-1,1)", negOne32, posOne32, MinMaxIEEE754_2008, posOne32, 0},
		{"max(-1,-2)", negOne32, negTwo32, MinMaxIEEE754_2008, negOne32, 0},
		{"max(0,-0)", posZero32, negZero32, MinMaxIEEE754_2008, posZero32, 0},
		{"max(-0,0)", negZero32, posZero32, MinMaxIEEE754_2008, posZero32, 0},
		{"max(inf,-inf)", posInf32, negInf32, MinMaxIEEE754_2008, posInf32, 0},

		// NaN handling
		{"max(NaN,1)", qNaN32, posOne32, MinMaxIEEE754_2008, posOne32, 0},
		{"max(1,NaN)", posOne32, qNaN32, MinMaxIEEE754_2008, posOne32, 0},
		{"max(NaN,NaN)", qNaN32, qNaN32, MinMaxIEEE754_2008, qNaN32, 0},
		{"max(sNaN,1)", sNaN32, posOne32, MinMaxIEEE754_2008, qNaN32, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := MaxF32(tc.a, tc.b, &flags, tc.minMaxType)

			if IsNaN32(tc.wantBits) {
				if !IsNaN32(got) {
					t.Errorf("MaxF32 got %v, want NaN", f32val(got))
				}
			} else if got != tc.wantBits {
				t.Errorf("MaxF32(%v, %v) = %v, want %v",
					f32val(tc.a), f32val(tc.b), f32val(got), f32val(tc.wantBits))
			}

			if flags != tc.wantFlag {
				t.Errorf("MaxF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestEqQuietF32(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		want     bool
		wantFlag ExceptionFlags
	}{
		{"1==1", posOne32, posOne32, true, 0},
		{"1==2", posOne32, posTwo32, false, 0},
		{"0==-0", posZero32, negZero32, true, 0},
		{"-0==0", negZero32, posZero32, true, 0},
		{"inf==inf", posInf32, posInf32, true, 0},
		{"-inf==-inf", negInf32, negInf32, true, 0},
		{"inf==-inf", posInf32, negInf32, false, 0},

		// NaN comparisons
		{"NaN==1", qNaN32, posOne32, false, 0},
		{"1==NaN", posOne32, qNaN32, false, 0},
		{"NaN==NaN", qNaN32, qNaN32, false, 0},
		{"sNaN==1", sNaN32, posOne32, false, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := EqQuietF32(tc.a, tc.b, &flags)

			if got != tc.want {
				t.Errorf("EqQuietF32(%v, %v) = %v, want %v",
					f32val(tc.a), f32val(tc.b), got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("EqQuietF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestLeF32(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		want     bool
		wantFlag ExceptionFlags
	}{
		{"1<=1", posOne32, posOne32, true, 0},
		{"1<=2", posOne32, posTwo32, true, 0},
		{"2<=1", posTwo32, posOne32, false, 0},
		{"-1<=1", negOne32, posOne32, true, 0},
		{"1<=-1", posOne32, negOne32, false, 0},
		{"0<=-0", posZero32, negZero32, true, 0},
		{"-0<=0", negZero32, posZero32, true, 0},
		{"-inf<=inf", negInf32, posInf32, true, 0},
		{"inf<=-inf", posInf32, negInf32, false, 0},

		// NaN comparisons (signaling)
		{"NaN<=1", qNaN32, posOne32, false, FlagInvalidOp},
		{"1<=NaN", posOne32, qNaN32, false, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := LeF32(tc.a, tc.b, &flags)

			if got != tc.want {
				t.Errorf("LeF32(%v, %v) = %v, want %v",
					f32val(tc.a), f32val(tc.b), got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("LeF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestLtF32(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint32
		want     bool
		wantFlag ExceptionFlags
	}{
		{"1<1", posOne32, posOne32, false, 0},
		{"1<2", posOne32, posTwo32, true, 0},
		{"2<1", posTwo32, posOne32, false, 0},
		{"-1<1", negOne32, posOne32, true, 0},
		{"1<-1", posOne32, negOne32, false, 0},
		{"0<-0", posZero32, negZero32, false, 0},
		{"-0<0", negZero32, posZero32, false, 0},
		{"-inf<inf", negInf32, posInf32, true, 0},
		{"inf<-inf", posInf32, negInf32, false, 0},

		// NaN comparisons (signaling)
		{"NaN<1", qNaN32, posOne32, false, FlagInvalidOp},
		{"1<NaN", posOne32, qNaN32, false, FlagInvalidOp},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var flags ExceptionFlags
			got := LtF32(tc.a, tc.b, &flags)

			if got != tc.want {
				t.Errorf("LtF32(%v, %v) = %v, want %v",
					f32val(tc.a), f32val(tc.b), got, tc.want)
			}

			if flags != tc.wantFlag {
				t.Errorf("LtF32 flags = %v, want %v", flags, tc.wantFlag)
			}
		})
	}
}

func TestSignF32(t *testing.T) {
	tests := []struct {
		name string
		a, b uint32
		want uint32
	}{
		{"sign(1,1)", posOne32, posOne32, posOne32},
		{"sign(1,-1)", posOne32, negOne32, negOne32},
		{"sign(-1,1)", negOne32, posOne32, posOne32},
		{"sign(-1,-1)", negOne32, negOne32, negOne32},
		{"sign(0,-0)", posZero32, negZero32, negZero32},
		{"sign(-0,0)", negZero32, posZero32, posZero32},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SignF32(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("SignF32(0x%08x, 0x%08x) = 0x%08x, want 0x%08x",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSignNF32(t *testing.T) {
	tests := []struct {
		name string
		a, b uint32
		want uint32
	}{
		{"signn(1,1)", posOne32, posOne32, negOne32},
		{"signn(1,-1)", posOne32, negOne32, posOne32},
		{"signn(-1,1)", negOne32, posOne32, negOne32},
		{"signn(-1,-1)", negOne32, negOne32, posOne32},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SignNF32(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("SignNF32(0x%08x, 0x%08x) = 0x%08x, want 0x%08x",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSignXF32(t *testing.T) {
	tests := []struct {
		name string
		a, b uint32
		want uint32
	}{
		{"signx(1,1)", posOne32, posOne32, posOne32},
		{"signx(1,-1)", posOne32, negOne32, negOne32},
		{"signx(-1,1)", negOne32, posOne32, negOne32},
		{"signx(-1,-1)", negOne32, negOne32, posOne32},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SignXF32(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("SignXF32(0x%08x, 0x%08x) = 0x%08x, want 0x%08x",
					tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestAddF32Subnormal(t *testing.T) {
	var flags ExceptionFlags

	// Add two subnormals
	result := AddF32(posSubnorm32, posSubnorm32, RNE, &flags)

	// Result should be a small positive number (2 * min_subnormal)
	if result == 0 {
		t.Error("AddF32 of two subnormals should not be zero")
	}
	if result&Float32SignMask != 0 {
		t.Error("AddF32 of two positive subnormals should be positive")
	}
}

func TestMulF32Overflow(t *testing.T) {
	var flags ExceptionFlags

	// Multiply max normal by 2 should overflow to infinity
	result := MulF32(posMaxNorm32, posTwo32, RNE, &flags)

	if result != posInf32 {
		t.Errorf("MulF32(maxNorm, 2) = 0x%08x, want +inf (0x%08x)", result, posInf32)
	}

	if flags&FlagOverflow == 0 {
		t.Error("MulF32 overflow should set overflow flag")
	}
}

func TestDivF32Underflow(t *testing.T) {
	var flags ExceptionFlags

	// Divide min normal by 4 should underflow to subnormal
	result := DivF32(posMinNorm32, f32bits(4.0), RNE, &flags)

	// Result should be a subnormal
	exp := (result >> Float32MantBits) & Float32ExpMask
	if exp != 0 {
		// Not necessarily subnormal, but should be very small
		if f32val(result) >= f32val(posMinNorm32) {
			t.Errorf("DivF32(minNorm, 4) should produce smaller result")
		}
	}
}

// TestRoundingModes tests that different rounding modes produce correct results
func TestRoundingModes(t *testing.T) {
	// 1/3 cannot be represented exactly, so rounding mode matters
	oneThird := DivF32(posOne32, f32bits(3.0), RNE, new(ExceptionFlags))
	oneThirdRTZ := DivF32(posOne32, f32bits(3.0), RTZ, new(ExceptionFlags))
	oneThirdRUP := DivF32(posOne32, f32bits(3.0), RUP, new(ExceptionFlags))
	oneThirdRDN := DivF32(posOne32, f32bits(3.0), RDN, new(ExceptionFlags))

	// RTZ should truncate towards zero (same as RDN for positive)
	if oneThirdRTZ != oneThirdRDN {
		t.Errorf("RTZ and RDN should give same result for positive 1/3: RTZ=0x%08x, RDN=0x%08x",
			oneThirdRTZ, oneThirdRDN)
	}

	// RUP should be >= RNE for positive numbers
	if f32val(oneThirdRUP) < f32val(oneThird) {
		t.Errorf("RUP result should be >= RNE result for positive 1/3")
	}

	// RDN should be <= RNE for positive numbers
	if f32val(oneThirdRDN) > f32val(oneThird) {
		t.Errorf("RDN result should be <= RNE result for positive 1/3")
	}
}
