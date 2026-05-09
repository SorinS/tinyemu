package isatest

import (
	"os"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/riscv"
	"github.com/jtolio/tinyemu-go/test/isatest/elfloader"
)

// TestFormatSignature tests the signature formatting function.
func TestFormatSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "single word",
			input:    []byte{0x78, 0x56, 0x34, 0x12},
			expected: "12345678",
		},
		{
			name:     "two words",
			input:    []byte{0xef, 0xbe, 0xad, 0xde, 0x78, 0x56, 0x34, 0x12},
			expected: "deadbeef\n12345678",
		},
		{
			name:     "zeros",
			input:    []byte{0x00, 0x00, 0x00, 0x00},
			expected: "00000000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatSignature(tc.input)
			if result != tc.expected {
				t.Errorf("FormatSignature(%v) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// TestFormatSignature64 tests the 64-bit signature formatting.
func TestFormatSignature64(t *testing.T) {
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	expected := "0807060504030201"
	result := FormatSignature64(input)
	if result != expected {
		t.Errorf("FormatSignature64(%v) = %q, want %q", input, result, expected)
	}
}

// TestDefaultConfig tests the default configuration.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxCycles != 100000 {
		t.Errorf("MaxCycles = %d, want 100000", cfg.MaxCycles)
	}

	if cfg.XLEN != riscv.XLEN64 {
		t.Errorf("XLEN = %v, want XLEN64", cfg.XLEN)
	}

	if cfg.RAMBase != 0x80000000 {
		t.Errorf("RAMBase = 0x%x, want 0x80000000", cfg.RAMBase)
	}

	if cfg.RAMSize != 64*1024*1024 {
		t.Errorf("RAMSize = %d, want %d", cfg.RAMSize, 64*1024*1024)
	}
}

// TestRunnerWithoutELF tests that running without loading an ELF returns an error.
func TestRunnerWithoutELF(t *testing.T) {
	runner := NewRunner(DefaultConfig())
	_, err := runner.Run()
	if err == nil {
		t.Error("expected error when running without ELF loaded")
	}
}

// TestCompareSignature tests signature comparison.
func TestCompareSignature(t *testing.T) {
	// Create a temporary reference file
	tmpDir := t.TempDir()
	refPath := tmpDir + "/test.reference_output"

	// Reference format: big-endian hex lines
	// For little-endian memory [0xef, 0xbe, 0xad, 0xde], the reference is "deadbeef"
	refContent := "deadbeef\n12345678\n"
	if err := os.WriteFile(refPath, []byte(refContent), 0644); err != nil {
		t.Fatalf("failed to write reference file: %v", err)
	}

	// The computed signature is in little-endian memory order
	computed := []byte{0xef, 0xbe, 0xad, 0xde, 0x78, 0x56, 0x34, 0x12}

	err := CompareSignature(computed, refPath)
	if err != nil {
		t.Errorf("CompareSignature failed: %v", err)
	}
}

// TestCompareSignatureMismatch tests that mismatched signatures fail.
func TestCompareSignatureMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	refPath := tmpDir + "/test.reference_output"

	refContent := "deadbeef\n"
	if err := os.WriteFile(refPath, []byte(refContent), 0644); err != nil {
		t.Fatalf("failed to write reference file: %v", err)
	}

	// Wrong signature
	computed := []byte{0x00, 0x00, 0x00, 0x00}

	err := CompareSignature(computed, refPath)
	if err == nil {
		t.Error("expected error for mismatched signature")
	}
}

// TestELFInfoString tests the String method of elfloader.Info.
func TestELFInfoString(t *testing.T) {
	entry := uint64(0x80000000)
	begin := uint64(0x80001000)
	end := uint64(0x80001100)
	tohost := uint64(0x80002000)

	info := &elfloader.Info{
		EntryPoint:     entry,
		BeginSignature: &begin,
		EndSignature:   &end,
		ToHostAddr:     &tohost,
		Segments: []elfloader.Segment{
			{VAddr: 0x80000000, Data: make([]byte, 0x1000)},
		},
	}

	s := info.String()

	// Check that all relevant info is present
	if s == "" {
		t.Error("String() returned empty string")
	}

	t.Logf("elfloader.Info.String():\n%s", s)
}
