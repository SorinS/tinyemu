package isatest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/riscv"
)

// TestCase represents a single ISA compliance test.
type TestCase struct {
	// Name is the test name (usually derived from filename).
	Name string

	// ELFPath is the path to the test ELF binary.
	ELFPath string

	// RefPath is the path to the reference signature file (optional).
	RefPath string

	// XLEN specifies the register width (32 or 64).
	XLEN riscv.XLEN
}

// DiscoverTests finds all ISA tests in a directory.
// It looks for .elf files and matches them with .reference_output files.
func DiscoverTests(dir string, xlen riscv.XLEN) ([]TestCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tests []TestCase
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)

		// Look for .elf files
		if ext != ".elf" {
			// Also check for extensionless files (common format)
			if ext != "" {
				continue
			}
		}

		elfPath := filepath.Join(dir, name)

		// Check if it's actually an ELF file by reading magic
		f, err := os.Open(elfPath)
		if err != nil {
			continue
		}
		magic := make([]byte, 4)
		_, err = f.Read(magic)
		f.Close()
		if err != nil || magic[0] != 0x7f || magic[1] != 'E' || magic[2] != 'L' || magic[3] != 'F' {
			continue
		}

		baseName := name
		if ext != "" {
			baseName = name[:len(name)-len(ext)]
		}

		tc := TestCase{
			Name:    baseName,
			ELFPath: elfPath,
			XLEN:    xlen,
		}

		// Look for reference file
		for _, refExt := range []string{".reference_output", ".signature", ".ref"} {
			refPath := filepath.Join(dir, baseName+refExt)
			if _, err := os.Stat(refPath); err == nil {
				tc.RefPath = refPath
				break
			}
		}

		tests = append(tests, tc)
	}

	return tests, nil
}

// RunTests runs all discovered tests using the testing framework.
func RunTests(t *testing.T, tests []TestCase, config RunConfig) {
	for _, tc := range tests {
		tc := tc // capture for closure
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			// Override XLEN from test case
			cfg := config
			if tc.XLEN != 0 {
				cfg.XLEN = tc.XLEN
			}

			result, err := RunTest(tc.ELFPath, cfg)
			if err != nil {
				t.Fatalf("failed to run test: %v", err)
			}

			// Check exit code
			if result.ExitCode != 0 {
				t.Errorf("test failed with exit code %d (halt reason: %s, cycles: %d)",
					result.ExitCode, result.HaltReason, result.Cycles)
				return
			}

			// Check for timeout
			if result.HaltReason == "timeout" {
				t.Errorf("test timed out after %d cycles", result.Cycles)
				return
			}

			// Compare signature if reference available
			if tc.RefPath != "" && result.Signature != nil {
				if err := CompareSignature(result.Signature, tc.RefPath); err != nil {
					t.Errorf("signature mismatch: %v", err)
					return
				}
			}

			t.Logf("passed in %d cycles (halt reason: %s)", result.Cycles, result.HaltReason)
		})
	}
}

// TestHelper provides a convenient way to run ISA tests from a test function.
type TestHelper struct {
	t      *testing.T
	config RunConfig
}

// NewTestHelper creates a new test helper.
func NewTestHelper(t *testing.T) *TestHelper {
	return &TestHelper{
		t:      t,
		config: DefaultConfig(),
	}
}

// WithXLEN sets the XLEN for tests.
func (h *TestHelper) WithXLEN(xlen riscv.XLEN) *TestHelper {
	h.config.XLEN = xlen
	return h
}

// WithMaxCycles sets the maximum cycles for tests.
func (h *TestHelper) WithMaxCycles(cycles int) *TestHelper {
	h.config.MaxCycles = cycles
	return h
}

// WithVerbose enables verbose output.
func (h *TestHelper) WithVerbose() *TestHelper {
	h.config.Verbose = true
	return h
}

// RunDir discovers and runs all tests in a directory.
func (h *TestHelper) RunDir(dir string) {
	tests, err := DiscoverTests(dir, h.config.XLEN)
	if err != nil {
		h.t.Fatalf("failed to discover tests: %v", err)
	}

	if len(tests) == 0 {
		h.t.Skip("no tests found in directory")
	}

	RunTests(h.t, tests, h.config)
}

// RunFile runs a single test file.
func (h *TestHelper) RunFile(elfPath string) *RunResult {
	result, err := RunTest(elfPath, h.config)
	if err != nil {
		h.t.Fatalf("failed to run test: %v", err)
	}

	if result.ExitCode != 0 {
		h.t.Errorf("test failed with exit code %d", result.ExitCode)
	}

	if result.HaltReason == "timeout" {
		h.t.Errorf("test timed out after %d cycles", result.Cycles)
	}

	return result
}

// RunFileWithRef runs a test and compares against a reference.
func (h *TestHelper) RunFileWithRef(elfPath, refPath string) {
	err := RunTestWithReference(elfPath, refPath, h.config)
	if err != nil {
		h.t.Error(err)
	}
}
