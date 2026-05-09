package softfp

import (
	"testing"

	"lukechampine.com/uint128"
)

func TestMulU128(t *testing.T) {
	tests := []struct {
		name           string
		a, b           Uint128
		wantHi, wantLo Uint128
	}{
		{
			name:   "zero times zero",
			a:      uint128.Zero,
			b:      uint128.Zero,
			wantHi: uint128.Zero,
			wantLo: uint128.Zero,
		},
		{
			name:   "one times one",
			a:      uint128.From64(1),
			b:      uint128.From64(1),
			wantHi: uint128.Zero,
			wantLo: uint128.From64(1),
		},
		{
			name:   "small multiply",
			a:      uint128.From64(6),
			b:      uint128.From64(7),
			wantHi: uint128.Zero,
			wantLo: uint128.From64(42),
		},
		{
			name:   "2^64 times 2",
			a:      uint128.New(0, 1), // 2^64
			b:      uint128.From64(2),
			wantHi: uint128.Zero,
			wantLo: uint128.New(0, 2), // 2^65
		},
		{
			name:   "max_u64 times max_u64",
			a:      uint128.From64(^uint64(0)), // 2^64 - 1
			b:      uint128.From64(^uint64(0)), // 2^64 - 1
			wantHi: uint128.Zero,
			// (2^64-1)^2 = 2^128 - 2*2^64 + 1 = (2^64-2)*2^64 + 1
			wantLo: uint128.New(1, ^uint64(0)-1),
		},
		{
			name:   "2^127 times 2",
			a:      uint128.New(0, 1<<63), // 2^127
			b:      uint128.From64(2),
			wantHi: uint128.From64(1), // 2^128
			wantLo: uint128.Zero,
		},
		{
			name:   "max_u128 times 1",
			a:      uint128.Max, // 2^128 - 1
			b:      uint128.From64(1),
			wantHi: uint128.Zero,
			wantLo: uint128.Max,
		},
		{
			name:   "max_u128 times 2",
			a:      uint128.Max, // 2^128 - 1
			b:      uint128.From64(2),
			wantHi: uint128.From64(1),
			wantLo: uint128.New(^uint64(0)-1, ^uint64(0)), // 2^129 - 2
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotHi, gotLo := mulU128(tc.a, tc.b)
			if !gotHi.Equals(tc.wantHi) || !gotLo.Equals(tc.wantLo) {
				t.Errorf("mulU128(%v, %v) = (%v, %v), want (%v, %v)",
					tc.a, tc.b, gotHi, gotLo, tc.wantHi, tc.wantLo)
			}
			// Verify commutativity
			gotHi2, gotLo2 := mulU128(tc.b, tc.a)
			if !gotHi2.Equals(tc.wantHi) || !gotLo2.Equals(tc.wantLo) {
				t.Errorf("mulU128(%v, %v) = (%v, %v), want (%v, %v) (commutativity)",
					tc.b, tc.a, gotHi2, gotLo2, tc.wantHi, tc.wantLo)
			}
		})
	}
}

func TestClz128(t *testing.T) {
	tests := []struct {
		name string
		a    Uint128
		want int
	}{
		{"zero", uint128.Zero, 128},
		{"one", uint128.From64(1), 127},
		{"max_u64", uint128.From64(^uint64(0)), 64},
		{"2^64", uint128.New(0, 1), 63},
		{"2^127", uint128.New(0, 1<<63), 0},
		{"max", uint128.Max, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clz128(tc.a)
			if got != tc.want {
				t.Errorf("clz128(%v) = %d, want %d", tc.a, got, tc.want)
			}
		})
	}
}

func TestRshiftRnd128(t *testing.T) {
	tests := []struct {
		name  string
		a     Uint128
		shift int
		want  Uint128
	}{
		{"shift 0", uint128.From64(42), 0, uint128.From64(42)},
		{"shift 1 no sticky", uint128.From64(4), 1, uint128.From64(2)},
		{"shift 1 with sticky", uint128.From64(5), 1, uint128.From64(3)}, // 5 >> 1 = 2, but low bit was 1 so sticky set
		{"shift >= 128 nonzero", uint128.From64(1), 128, uint128.From64(1)},
		{"shift >= 128 zero", uint128.Zero, 128, uint128.Zero},
		{"shift 64 no sticky", uint128.New(0, 0x100), 64, uint128.From64(0x100)},   // hi becomes lo, lo was 0 so no sticky
		{"shift 64 with sticky", uint128.New(1, 0x100), 64, uint128.From64(0x101)}, // hi becomes lo, lo was nonzero so sticky set
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rshiftRnd128(tc.a, tc.shift)
			if !got.Equals(tc.want) {
				t.Errorf("rshiftRnd128(%v, %d) = %v, want %v", tc.a, tc.shift, got, tc.want)
			}
		})
	}
}

func TestPack128Unpack128(t *testing.T) {
	tests := []struct {
		name string
		sign uint32
		exp  uint32
		mant Uint128
	}{
		{"positive zero", 0, 0, uint128.Zero},
		{"negative zero", 1, 0, uint128.Zero},
		{"positive one", 0, 16383, uint128.Zero},                                // exp bias = 16383, so exp=16383 means actual exp=0
		{"small mantissa", 0, 16384, uint128.From64(0x123456789ABCDEF0)},        // small mantissa value
		{"large mantissa", 0, 16384, uint128.New(0xFFFFFFFFFFFFFFFF, 0x0000FF)}, // larger mantissa
		{"positive infinity", 0, 32767, uint128.Zero},
		{"negative infinity", 1, 32767, uint128.Zero},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			packed := Pack128(tc.sign, tc.exp, tc.mant)
			gotSign, gotExp, gotMant := Unpack128(packed)

			if gotSign != tc.sign {
				t.Errorf("sign: got %d, want %d", gotSign, tc.sign)
			}
			if uint32(gotExp) != tc.exp {
				t.Errorf("exp: got %d, want %d", gotExp, tc.exp)
			}
			// Mantissa should be masked to Float128MantMask
			wantMant := tc.mant.And(Float128MantMask)
			if !gotMant.Equals(wantMant) {
				t.Errorf("mant: got %v, want %v", gotMant, wantMant)
			}
		})
	}
}

func TestIsNaN128(t *testing.T) {
	tests := []struct {
		name string
		a    Uint128
		want bool
	}{
		{"positive zero", uint128.Zero, false},
		{"positive infinity", uint128.New(0, 0x7FFF000000000000), false},
		{"negative infinity", uint128.New(0, 0xFFFF000000000000), false},
		{"quiet NaN", Float128QNaN, true},
		{"signaling NaN", uint128.New(1, 0x7FFF000000000000), true}, // exp all 1s, mant != 0, quiet bit = 0
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNaN128(tc.a)
			if got != tc.want {
				t.Errorf("IsNaN128(%v) = %v, want %v", tc.a, got, tc.want)
			}
		})
	}
}

func TestFClass128(t *testing.T) {
	tests := []struct {
		name string
		a    Uint128
		want FClass
	}{
		{"positive zero", uint128.Zero, FClassPosZero},
		{"negative zero", Float128SignMask, FClassNegZero},
		{"positive infinity", uint128.New(0, 0x7FFF000000000000), FClassPosInf},
		{"negative infinity", uint128.New(0, 0xFFFF000000000000), FClassNegInf},
		{"quiet NaN", Float128QNaN, FClassQNaN},
		{"signaling NaN", uint128.New(1, 0x7FFF000000000000), FClassSNaN},
		{"positive normal", uint128.New(0, 0x3FFF000000000000), FClassPosNormal}, // exp = 16383 (bias), normal 1.0
		{"negative normal", uint128.New(0, 0xBFFF000000000000), FClassNegNormal},
		{"positive subnormal", uint128.From64(1), FClassPosSubnormal},                  // exp = 0, mant != 0
		{"negative subnormal", uint128.New(1, 0x8000000000000000), FClassNegSubnormal}, // sign = 1, exp = 0, mant != 0
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FClass128(tc.a)
			if got != tc.want {
				t.Errorf("FClass128(%v) = %v, want %v", tc.a, got, tc.want)
			}
		})
	}
}
