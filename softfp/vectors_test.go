package softfp

import (
	"embed"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

//go:embed testdata/*.json
var testVectors embed.FS

// TestVector represents a single test case from the JSON vectors.
type TestVector struct {
	Op      string `json:"op"`
	Comment string `json:"comment"`
	A       string `json:"a"`
	B       string `json:"b,omitempty"`
	C       string `json:"c,omitempty"`
	RM      int    `json:"rm"`
	Result  string `json:"result"`
	Flags   uint32 `json:"flags"`
}

// TestVectorFile represents a JSON test vector file.
type TestVectorFile struct {
	Description string       `json:"description"`
	Precision   string       `json:"precision,omitempty"`
	Vectors     []TestVector `json:"vectors"`
}

// parseHex parses a hex string like "0x3f800000" to uint64.
func parseHex(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	return strconv.ParseUint(s, 16, 64)
}

// TestF32VectorsBasic tests F32 operations using test vectors.
func TestF32VectorsBasic(t *testing.T) {
	data, err := testVectors.ReadFile("testdata/f32_basic.json")
	if err != nil {
		t.Skipf("test vectors not found: %v", err)
	}

	var file TestVectorFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("failed to parse test vectors: %v", err)
	}

	for _, v := range file.Vectors {
		t.Run(v.Comment, func(t *testing.T) {
			runF32Vector(t, v)
		})
	}
}

// TestF64VectorsBasic tests F64 operations using test vectors.
func TestF64VectorsBasic(t *testing.T) {
	data, err := testVectors.ReadFile("testdata/f64_basic.json")
	if err != nil {
		t.Skipf("test vectors not found: %v", err)
	}

	var file TestVectorFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("failed to parse test vectors: %v", err)
	}

	for _, v := range file.Vectors {
		t.Run(v.Comment, func(t *testing.T) {
			runF64Vector(t, v)
		})
	}
}

// TestConversionVectors tests conversion operations using test vectors.
func TestConversionVectors(t *testing.T) {
	data, err := testVectors.ReadFile("testdata/conversions.json")
	if err != nil {
		t.Skipf("test vectors not found: %v", err)
	}

	var file TestVectorFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("failed to parse test vectors: %v", err)
	}

	for _, v := range file.Vectors {
		t.Run(v.Comment, func(t *testing.T) {
			runConversionVector(t, v)
		})
	}
}

func runF32Vector(t *testing.T, v TestVector) {
	t.Helper()

	a, err := parseHex(v.A)
	if err != nil {
		t.Fatalf("invalid a: %v", err)
	}

	var b uint64
	if v.B != "" {
		b, err = parseHex(v.B)
		if err != nil {
			t.Fatalf("invalid b: %v", err)
		}
	}

	expected, err := parseHex(v.Result)
	if err != nil {
		t.Fatalf("invalid result: %v", err)
	}

	rm := RoundingMode(v.RM)
	var result uint32
	var flags ExceptionFlags

	switch v.Op {
	case "fadd":
		result = AddF32(uint32(a), uint32(b), rm, &flags)
	case "fsub":
		result = SubF32(uint32(a), uint32(b), rm, &flags)
	case "fmul":
		result = MulF32(uint32(a), uint32(b), rm, &flags)
	case "fdiv":
		result = DivF32(uint32(a), uint32(b), rm, &flags)
	case "fsqrt":
		result = SqrtF32(uint32(a), rm, &flags)
	case "fmin":
		result = MinF32(uint32(a), uint32(b), &flags, MinMaxIEEE754_2008)
	case "fmax":
		result = MaxF32(uint32(a), uint32(b), &flags, MinMaxIEEE754_2008)
	case "feq":
		eq := EqQuietF32(uint32(a), uint32(b), &flags)
		if eq {
			result = 1
		}
	case "flt":
		lt := LtF32(uint32(a), uint32(b), &flags)
		if lt {
			result = 1
		}
	case "fle":
		le := LeF32(uint32(a), uint32(b), &flags)
		if le {
			result = 1
		}
	default:
		t.Skipf("unsupported operation: %s", v.Op)
	}

	if result != uint32(expected) {
		t.Errorf("%s: result = 0x%08x, want 0x%08x", v.Op, result, uint32(expected))
	}
	if uint32(flags) != v.Flags {
		t.Errorf("%s: flags = %d, want %d", v.Op, flags, v.Flags)
	}
}

func runF64Vector(t *testing.T, v TestVector) {
	t.Helper()

	a, err := parseHex(v.A)
	if err != nil {
		t.Fatalf("invalid a: %v", err)
	}

	var b uint64
	if v.B != "" {
		b, err = parseHex(v.B)
		if err != nil {
			t.Fatalf("invalid b: %v", err)
		}
	}

	expected, err := parseHex(v.Result)
	if err != nil {
		t.Fatalf("invalid result: %v", err)
	}

	rm := RoundingMode(v.RM)
	var result uint64
	var flags ExceptionFlags

	switch v.Op {
	case "fadd":
		result = AddF64(a, b, rm, &flags)
	case "fsub":
		result = SubF64(a, b, rm, &flags)
	case "fmul":
		result = MulF64(a, b, rm, &flags)
	case "fdiv":
		result = DivF64(a, b, rm, &flags)
	case "fsqrt":
		result = SqrtF64(a, rm, &flags)
	case "fmin":
		result = MinF64(a, b, &flags, MinMaxIEEE754_2008)
	case "fmax":
		result = MaxF64(a, b, &flags, MinMaxIEEE754_2008)
	case "feq":
		eq := EqQuietF64(a, b, &flags)
		if eq {
			result = 1
		}
	case "flt":
		lt := LtF64(a, b, &flags)
		if lt {
			result = 1
		}
	case "fle":
		le := LeF64(a, b, &flags)
		if le {
			result = 1
		}
	default:
		t.Skipf("unsupported operation: %s", v.Op)
	}

	if result != expected {
		t.Errorf("%s: result = 0x%016x, want 0x%016x", v.Op, result, expected)
	}
	if uint32(flags) != v.Flags {
		t.Errorf("%s: flags = %d, want %d", v.Op, flags, v.Flags)
	}
}

func runConversionVector(t *testing.T, v TestVector) {
	t.Helper()

	a, err := parseHex(v.A)
	if err != nil {
		t.Fatalf("invalid a: %v", err)
	}

	expected, err := parseHex(v.Result)
	if err != nil {
		t.Fatalf("invalid result: %v", err)
	}

	rm := RoundingMode(v.RM)
	var result64 uint64
	var result32 uint32
	var flags ExceptionFlags
	is32bit := true

	switch v.Op {
	case "f32_to_i32":
		r := CvtF32ToI32(uint32(a), rm, &flags)
		result32 = uint32(r)
	case "f32_to_u32":
		result32 = CvtF32ToU32(uint32(a), rm, &flags)
	case "i32_to_f32":
		result32 = CvtI32ToF32(int32(a), rm, &flags)
	case "u32_to_f32":
		result32 = CvtU32ToF32(uint32(a), rm, &flags)
	case "f64_to_i32":
		r := CvtF64ToI32(a, rm, &flags)
		result32 = uint32(r)
	case "f64_to_i64":
		r := CvtF64ToI64(a, rm, &flags)
		result64 = uint64(r)
		is32bit = false
	case "i64_to_f64":
		result64 = CvtI64ToF64(int64(a), rm, &flags)
		is32bit = false
	case "f32_to_f64":
		result64 = CvtF32ToF64(uint32(a), &flags)
		is32bit = false
	case "f64_to_f32":
		result32 = CvtF64ToF32(a, rm, &flags)
	default:
		t.Skipf("unsupported operation: %s", v.Op)
	}

	if is32bit {
		if result32 != uint32(expected) {
			t.Errorf("%s: result = 0x%08x, want 0x%08x", v.Op, result32, uint32(expected))
		}
	} else {
		if result64 != expected {
			t.Errorf("%s: result = 0x%016x, want 0x%016x", v.Op, result64, expected)
		}
	}
	if uint32(flags) != v.Flags {
		t.Errorf("%s: flags = %d, want %d", v.Op, flags, v.Flags)
	}
}
