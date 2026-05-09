package softfp

import (
	"math"
	"testing"
)

func TestRoundingModeValues(t *testing.T) {
	// Verify rounding mode enum values match expected order
	tests := []struct {
		rm   RoundingMode
		want int
	}{
		{RNE, 0},
		{RTZ, 1},
		{RDN, 2},
		{RUP, 3},
		{RMM, 4},
	}

	for _, tc := range tests {
		if int(tc.rm) != tc.want {
			t.Errorf("RoundingMode %d = %d, want %d", tc.rm, int(tc.rm), tc.want)
		}
	}
}

func TestExceptionFlagValues(t *testing.T) {
	// Verify exception flag bit positions match RISC-V fflags
	tests := []struct {
		flag ExceptionFlags
		want uint32
	}{
		{FlagInexact, 0x01},
		{FlagUnderflow, 0x02},
		{FlagOverflow, 0x04},
		{FlagDivideZero, 0x08},
		{FlagInvalidOp, 0x10},
	}

	for _, tc := range tests {
		if uint32(tc.flag) != tc.want {
			t.Errorf("ExceptionFlag 0x%x, want 0x%x", uint32(tc.flag), tc.want)
		}
	}
}

func TestFClassValues(t *testing.T) {
	// Verify FClass bit positions match RISC-V FCLASS output
	tests := []struct {
		class FClass
		want  uint32
	}{
		{FClassNegInf, 0x001},
		{FClassNegNormal, 0x002},
		{FClassNegSubnormal, 0x004},
		{FClassNegZero, 0x008},
		{FClassPosZero, 0x010},
		{FClassPosSubnormal, 0x020},
		{FClassPosNormal, 0x040},
		{FClassPosInf, 0x080},
		{FClassSNaN, 0x100},
		{FClassQNaN, 0x200},
	}

	for _, tc := range tests {
		if uint32(tc.class) != tc.want {
			t.Errorf("FClass 0x%x, want 0x%x", uint32(tc.class), tc.want)
		}
	}
}

func TestFloat32Constants(t *testing.T) {
	if Float32MantBits != 23 {
		t.Errorf("Float32MantBits = %d, want 23", Float32MantBits)
	}
	if Float32ExpBits != 8 {
		t.Errorf("Float32ExpBits = %d, want 8", Float32ExpBits)
	}
	if Float32ExpMask != 0xFF {
		t.Errorf("Float32ExpMask = 0x%x, want 0xFF", Float32ExpMask)
	}
	if Float32MantMask != 0x7FFFFF {
		t.Errorf("Float32MantMask = 0x%x, want 0x7FFFFF", Float32MantMask)
	}
	if Float32SignMask != 0x80000000 {
		t.Errorf("Float32SignMask = 0x%x, want 0x80000000", Float32SignMask)
	}
	if Float32ExpBias != 127 {
		t.Errorf("Float32ExpBias = %d, want 127", Float32ExpBias)
	}
}

func TestFloat64Constants(t *testing.T) {
	if Float64MantBits != 52 {
		t.Errorf("Float64MantBits = %d, want 52", Float64MantBits)
	}
	if Float64ExpBits != 11 {
		t.Errorf("Float64ExpBits = %d, want 11", Float64ExpBits)
	}
	if Float64ExpMask != 0x7FF {
		t.Errorf("Float64ExpMask = 0x%x, want 0x7FF", Float64ExpMask)
	}
	if Float64MantMask != 0xFFFFFFFFFFFFF {
		t.Errorf("Float64MantMask = 0x%x, want 0xFFFFFFFFFFFFF", Float64MantMask)
	}
	if Float64SignMask != 0x8000000000000000 {
		t.Errorf("Float64SignMask = 0x%x, want 0x8000000000000000", Float64SignMask)
	}
	if Float64ExpBias != 1023 {
		t.Errorf("Float64ExpBias = %d, want 1023", Float64ExpBias)
	}
}

func TestPack32(t *testing.T) {
	tests := []struct {
		name      string
		sign      uint32
		exp       uint32
		mant      uint32
		want      uint32
		wantFloat float32
	}{
		{"positive zero", 0, 0, 0, 0x00000000, 0.0},
		{"negative zero", 1, 0, 0, 0x80000000, float32(math.Copysign(0, -1))},
		{"1.0", 0, 127, 0, 0x3F800000, 1.0},
		{"-1.0", 1, 127, 0, 0xBF800000, -1.0},
		{"2.0", 0, 128, 0, 0x40000000, 2.0},
		{"0.5", 0, 126, 0, 0x3F000000, 0.5},
		{"+inf", 0, 255, 0, 0x7F800000, float32(math.Inf(1))},
		{"-inf", 1, 255, 0, 0xFF800000, float32(math.Inf(-1))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Pack32(tc.sign, tc.exp, tc.mant)
			if got != tc.want {
				t.Errorf("Pack32(%d, %d, 0x%x) = 0x%08x, want 0x%08x",
					tc.sign, tc.exp, tc.mant, got, tc.want)
			}
			// Verify it matches the expected float value
			gotFloat := math.Float32frombits(got)
			if math.IsNaN(float64(tc.wantFloat)) {
				if !math.IsNaN(float64(gotFloat)) {
					t.Errorf("Pack32 result %v, want NaN", gotFloat)
				}
			} else if gotFloat != tc.wantFloat {
				t.Errorf("Pack32 as float = %v, want %v", gotFloat, tc.wantFloat)
			}
		})
	}
}

func TestUnpack32(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		wantSign uint32
		wantExp  int32
		wantMant uint32
	}{
		{"positive zero", 0x00000000, 0, 0, 0},
		{"negative zero", 0x80000000, 1, 0, 0},
		{"1.0", 0x3F800000, 0, 127, 0},
		{"-1.0", 0xBF800000, 1, 127, 0},
		{"1.5", 0x3FC00000, 0, 127, 0x400000},
		{"+inf", 0x7F800000, 0, 255, 0},
		{"-inf", 0xFF800000, 1, 255, 0},
		{"qnan", 0x7FC00000, 0, 255, 0x400000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sign, exp, mant := Unpack32(tc.input)
			if sign != tc.wantSign {
				t.Errorf("Unpack32(0x%08x) sign = %d, want %d", tc.input, sign, tc.wantSign)
			}
			if exp != tc.wantExp {
				t.Errorf("Unpack32(0x%08x) exp = %d, want %d", tc.input, exp, tc.wantExp)
			}
			if mant != tc.wantMant {
				t.Errorf("Unpack32(0x%08x) mant = 0x%x, want 0x%x", tc.input, mant, tc.wantMant)
			}
		})
	}
}

func TestPack64(t *testing.T) {
	tests := []struct {
		name      string
		sign      uint32
		exp       uint32
		mant      uint64
		want      uint64
		wantFloat float64
	}{
		{"positive zero", 0, 0, 0, 0x0000000000000000, 0.0},
		{"negative zero", 1, 0, 0, 0x8000000000000000, math.Copysign(0, -1)},
		{"1.0", 0, 1023, 0, 0x3FF0000000000000, 1.0},
		{"-1.0", 1, 1023, 0, 0xBFF0000000000000, -1.0},
		{"2.0", 0, 1024, 0, 0x4000000000000000, 2.0},
		{"+inf", 0, 2047, 0, 0x7FF0000000000000, math.Inf(1)},
		{"-inf", 1, 2047, 0, 0xFFF0000000000000, math.Inf(-1)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Pack64(tc.sign, tc.exp, tc.mant)
			if got != tc.want {
				t.Errorf("Pack64(%d, %d, 0x%x) = 0x%016x, want 0x%016x",
					tc.sign, tc.exp, tc.mant, got, tc.want)
			}
			gotFloat := math.Float64frombits(got)
			if math.IsNaN(tc.wantFloat) {
				if !math.IsNaN(gotFloat) {
					t.Errorf("Pack64 result %v, want NaN", gotFloat)
				}
			} else if gotFloat != tc.wantFloat {
				t.Errorf("Pack64 as float = %v, want %v", gotFloat, tc.wantFloat)
			}
		})
	}
}

func TestUnpack64(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		wantSign uint32
		wantExp  int32
		wantMant uint64
	}{
		{"positive zero", 0x0000000000000000, 0, 0, 0},
		{"negative zero", 0x8000000000000000, 1, 0, 0},
		{"1.0", 0x3FF0000000000000, 0, 1023, 0},
		{"-1.0", 0xBFF0000000000000, 1, 1023, 0},
		{"+inf", 0x7FF0000000000000, 0, 2047, 0},
		{"-inf", 0xFFF0000000000000, 1, 2047, 0},
		{"qnan", 0x7FF8000000000000, 0, 2047, 0x8000000000000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sign, exp, mant := Unpack64(tc.input)
			if sign != tc.wantSign {
				t.Errorf("Unpack64(0x%016x) sign = %d, want %d", tc.input, sign, tc.wantSign)
			}
			if exp != tc.wantExp {
				t.Errorf("Unpack64(0x%016x) exp = %d, want %d", tc.input, exp, tc.wantExp)
			}
			if mant != tc.wantMant {
				t.Errorf("Unpack64(0x%016x) mant = 0x%x, want 0x%x", tc.input, mant, tc.wantMant)
			}
		})
	}
}

func TestIsNaN32(t *testing.T) {
	tests := []struct {
		name  string
		input uint32
		want  bool
	}{
		{"positive zero", 0x00000000, false},
		{"negative zero", 0x80000000, false},
		{"1.0", 0x3F800000, false},
		{"+inf", 0x7F800000, false},
		{"-inf", 0xFF800000, false},
		{"qnan", 0x7FC00000, true},
		{"snan", 0x7F800001, true},
		{"negative qnan", 0xFFC00000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNaN32(tc.input)
			if got != tc.want {
				t.Errorf("IsNaN32(0x%08x) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsSigNaN32(t *testing.T) {
	tests := []struct {
		name  string
		input uint32
		want  bool
	}{
		{"positive zero", 0x00000000, false},
		{"1.0", 0x3F800000, false},
		{"+inf", 0x7F800000, false},
		{"qnan", 0x7FC00000, false},    // quiet NaN (bit 22 set)
		{"snan", 0x7F800001, true},     // signaling NaN (bit 22 clear, other bits set)
		{"snan max", 0x7FBFFFFF, true}, // signaling NaN with max mantissa (bit 22 clear)
		{"negative snan", 0xFF800001, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSigNaN32(tc.input)
			if got != tc.want {
				t.Errorf("IsSigNaN32(0x%08x) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsNaN64(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  bool
	}{
		{"positive zero", 0x0000000000000000, false},
		{"1.0", 0x3FF0000000000000, false},
		{"+inf", 0x7FF0000000000000, false},
		{"qnan", 0x7FF8000000000000, true},
		{"snan", 0x7FF0000000000001, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNaN64(tc.input)
			if got != tc.want {
				t.Errorf("IsNaN64(0x%016x) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsSigNaN64(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  bool
	}{
		{"positive zero", 0x0000000000000000, false},
		{"1.0", 0x3FF0000000000000, false},
		{"+inf", 0x7FF0000000000000, false},
		{"qnan", 0x7FF8000000000000, false},
		{"snan", 0x7FF0000000000001, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSigNaN64(tc.input)
			if got != tc.want {
				t.Errorf("IsSigNaN64(0x%016x) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestFClass32(t *testing.T) {
	tests := []struct {
		name  string
		input uint32
		want  FClass
	}{
		{"-inf", 0xFF800000, FClassNegInf},
		{"-normal", 0xBF800000, FClassNegNormal}, // -1.0
		{"-subnormal", 0x80000001, FClassNegSubnormal},
		{"-zero", 0x80000000, FClassNegZero},
		{"+zero", 0x00000000, FClassPosZero},
		{"+subnormal", 0x00000001, FClassPosSubnormal},
		{"+normal", 0x3F800000, FClassPosNormal}, // 1.0
		{"+inf", 0x7F800000, FClassPosInf},
		{"snan", 0x7F800001, FClassSNaN},
		{"qnan", 0x7FC00000, FClassQNaN},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FClass32(tc.input)
			if got != tc.want {
				t.Errorf("FClass32(0x%08x) = 0x%x, want 0x%x", tc.input, got, tc.want)
			}
		})
	}
}

func TestFClass64(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  FClass
	}{
		{"-inf", 0xFFF0000000000000, FClassNegInf},
		{"-normal", 0xBFF0000000000000, FClassNegNormal}, // -1.0
		{"-subnormal", 0x8000000000000001, FClassNegSubnormal},
		{"-zero", 0x8000000000000000, FClassNegZero},
		{"+zero", 0x0000000000000000, FClassPosZero},
		{"+subnormal", 0x0000000000000001, FClassPosSubnormal},
		{"+normal", 0x3FF0000000000000, FClassPosNormal}, // 1.0
		{"+inf", 0x7FF0000000000000, FClassPosInf},
		{"snan", 0x7FF0000000000001, FClassSNaN},
		{"qnan", 0x7FF8000000000000, FClassQNaN},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FClass64(tc.input)
			if got != tc.want {
				t.Errorf("FClass64(0x%016x) = 0x%x, want 0x%x", tc.input, got, tc.want)
			}
		})
	}
}

func TestRshiftRnd32(t *testing.T) {
	tests := []struct {
		name string
		a    uint32
		d    int
		want uint32
	}{
		{"shift 0", 0x12345678, 0, 0x12345678},
		{"shift 1, no sticky", 0x12345678, 1, 0x091A2B3C},
		{"shift 1, sticky", 0x12345679, 1, 0x091A2B3D}, // low bit was 1
		{"shift 4", 0x12345670, 4, 0x01234567},
		{"shift 4 sticky", 0x12345678, 4, 0x01234567}, // low 4 bits non-zero, but result already has bit 0 set
		{"shift >= 32, non-zero", 0x00000001, 32, 1},
		{"shift >= 32, zero", 0x00000000, 32, 0},
		{"shift > 32", 0xFFFFFFFF, 64, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rshiftRnd32(tc.a, tc.d)
			if got != tc.want {
				t.Errorf("rshiftRnd32(0x%08x, %d) = 0x%08x, want 0x%08x",
					tc.a, tc.d, got, tc.want)
			}
		})
	}
}

func TestRshiftRnd64(t *testing.T) {
	tests := []struct {
		name string
		a    uint64
		d    int
		want uint64
	}{
		{"shift 0", 0x123456789ABCDEF0, 0, 0x123456789ABCDEF0},
		{"shift 4 no sticky", 0x123456789ABCDEF0, 4, 0x0123456789ABCDEF},
		{"shift 4 sticky", 0x123456789ABCDEF1, 4, 0x0123456789ABCDEF}, // low bits non-zero, but result already has bit 0 set
		{"shift >= 64, non-zero", 0x0000000000000001, 64, 1},
		{"shift >= 64, zero", 0x0000000000000000, 64, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rshiftRnd64(tc.a, tc.d)
			if got != tc.want {
				t.Errorf("rshiftRnd64(0x%016x, %d) = 0x%016x, want 0x%016x",
					tc.a, tc.d, got, tc.want)
			}
		})
	}
}

func TestNormalizeSubnormal32(t *testing.T) {
	tests := []struct {
		name     string
		mant     uint32
		wantExp  int32
		wantMant uint32
	}{
		// Smallest subnormal: mant = 1, shift left by 23 to get MSB at bit 23
		{"smallest", 1, -22, 1 << 23},
		// Largest subnormal: mant = 0x7FFFFF (MSB at bit 22), shift by 1
		{"largest", 0x7FFFFF, 0, 0xFFFFFE},
		// Mid value: MSB at bit 22, shift by 1 to get MSB at bit 23
		{"mid", 0x400000, 0, 0x800000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotExp, gotMant := normalizeSubnormal32(tc.mant)
			if gotExp != tc.wantExp {
				t.Errorf("normalizeSubnormal32(0x%x) exp = %d, want %d",
					tc.mant, gotExp, tc.wantExp)
			}
			if gotMant != tc.wantMant {
				t.Errorf("normalizeSubnormal32(0x%x) mant = 0x%x, want 0x%x",
					tc.mant, gotMant, tc.wantMant)
			}
		})
	}
}

func TestPackUnpackRoundTrip32(t *testing.T) {
	// Test that pack and unpack are inverses
	testCases := []uint32{
		0x00000000, // +0
		0x80000000, // -0
		0x3F800000, // 1.0
		0xBF800000, // -1.0
		0x7F7FFFFF, // max finite
		0x00000001, // min subnormal
		0x7F800000, // +inf
		0xFF800000, // -inf
	}

	for _, orig := range testCases {
		sign, exp, mant := Unpack32(orig)
		repacked := Pack32(sign, uint32(exp), mant)
		if repacked != orig {
			t.Errorf("Round trip failed: 0x%08x -> unpack -> pack = 0x%08x",
				orig, repacked)
		}
	}
}

func TestPackUnpackRoundTrip64(t *testing.T) {
	// Test that pack and unpack are inverses
	testCases := []uint64{
		0x0000000000000000, // +0
		0x8000000000000000, // -0
		0x3FF0000000000000, // 1.0
		0xBFF0000000000000, // -1.0
		0x7FEFFFFFFFFFFFFF, // max finite
		0x0000000000000001, // min subnormal
		0x7FF0000000000000, // +inf
		0xFFF0000000000000, // -inf
	}

	for _, orig := range testCases {
		sign, exp, mant := Unpack64(orig)
		repacked := Pack64(sign, uint32(exp), mant)
		if repacked != orig {
			t.Errorf("Round trip failed: 0x%016x -> unpack -> pack = 0x%016x",
				orig, repacked)
		}
	}
}
