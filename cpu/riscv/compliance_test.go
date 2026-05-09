package riscv

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
	"github.com/jtolio/tinyemu-go/test/isatest/elfloader"
)

const (
	// Default RAM base address for RISC-V (same as in riscv-tests)
	ramBaseAddr = 0x80000000

	// Default RAM size (16 MB should be plenty for tests)
	ramSize = 16 * 1024 * 1024

	// Maximum instructions to execute before timeout
	maxInstructions = 1000000

	// tohost magic values
	tohostPass = 1
)

// TestRISCVCompliance runs the RISC-V compliance tests from testdata/riscv-tests/isa/.
// Tests are organized by extension:
//   - rv64ui-p-*: RV64I base integer (no virtual memory)
//   - rv64um-p-*: M extension (multiply/divide)
//   - rv64ua-p-*: A extension (atomics)
//   - rv64uc-p-*: C extension (compressed)
//   - rv64uf-p-*: F extension (single-precision float)
//   - rv64ud-p-*: D extension (double-precision float)
//
// The "-p-" indicates physical addressing (no MMU), which is what we test first.
//
// Run with: go test -v -run TestRISCVCompliance ./cpu/
func TestRISCVCompliance(t *testing.T) {
	testDir := filepath.Join("..", "testdata", "riscv-tests", "isa")

	// Check if test directory exists
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skip("riscv-tests not found at", testDir)
	}

	// Define test suites in priority order
	suites := []struct {
		prefix string
		name   string
	}{
		{"rv64ui-p-", "rv64ui"}, // Base integer
		{"rv64um-p-", "rv64um"}, // Multiply/divide
		{"rv64ua-p-", "rv64ua"}, // Atomics
		{"rv64uc-p-", "rv64uc"}, // Compressed
		{"rv64uf-p-", "rv64uf"}, // Single-precision float
		{"rv64ud-p-", "rv64ud"}, // Double-precision float
	}

	for _, suite := range suites {
		t.Run(suite.name, func(t *testing.T) {
			tests := listComplianceTests(t, testDir, suite.prefix)
			if len(tests) == 0 {
				t.Skipf("no %s tests found", suite.prefix)
			}

			for _, testPath := range tests {
				testName := filepath.Base(testPath)
				// Extract just the instruction name (e.g., "add" from "rv64ui-p-add")
				shortName := strings.TrimPrefix(testName, suite.prefix)

				t.Run(shortName, func(t *testing.T) {
					runComplianceTest(t, testPath)
				})
			}
		})
	}
}

// listComplianceTests returns paths to all compliance test binaries matching the prefix.
func listComplianceTests(t *testing.T, dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read test directory: %v", err)
	}

	var tests []string
	for _, entry := range entries {
		name := entry.Name()
		// Skip dump files and only include files with the right prefix
		if strings.HasPrefix(name, prefix) && !strings.HasSuffix(name, ".dump") {
			tests = append(tests, filepath.Join(dir, name))
		}
	}
	return tests
}

// runComplianceTest runs a single compliance test and checks the result.
func runComplianceTest(t *testing.T, testPath string) {
	// Load ELF file
	elfInfo, err := elfloader.Load(testPath)
	if err != nil {
		t.Fatalf("failed to load ELF: %v", err)
	}

	// Verify we have a tohost address
	if elfInfo.ToHostAddr == nil {
		t.Fatal("ELF missing tohost symbol")
	}
	tohostAddr := *elfInfo.ToHostAddr

	// Create memory map and register RAM
	memMap := mem.NewPhysMemoryMap()
	defer memMap.Close()

	ram, err := memMap.RegisterRAM(ramBaseAddr, ramSize, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	// Load ELF segments into memory
	for _, seg := range elfInfo.Segments {
		if seg.VAddr < ramBaseAddr || seg.VAddr >= ramBaseAddr+ramSize {
			t.Fatalf("segment VAddr 0x%x outside RAM range", seg.VAddr)
		}
		offset := seg.VAddr - ramBaseAddr
		if offset+uint64(len(seg.Data)) > ramSize {
			t.Fatalf("segment extends beyond RAM")
		}
		copy(ram.PhysMem[offset:], seg.Data)
	}

	// Create CPU and set entry point
	cpu := NewCPU(memMap, XLEN64)
	cpu.PC = elfInfo.EntryPoint

	// Run until tohost is written or timeout
	var lastTohostVal uint64
	for i := 0; i < maxInstructions; i++ {
		if err := cpu.Step(); err != nil {
			// Exceptions are normal during test execution (e.g., illegal instruction tests)
			// The test harness handles them via trap vectors
		}

		// Check tohost
		tohostVal := readTohost(ram.PhysMem, tohostAddr, ramBaseAddr)
		if tohostVal != 0 && tohostVal != lastTohostVal {
			lastTohostVal = tohostVal

			// Check if this is the final result
			// Tests write the result and then spin, so we check once and exit
			if tohostVal == tohostPass {
				// Test passed
				return
			}

			// Test failed - extract test case number
			// Format: (test_num << 1) | 1
			if tohostVal&1 != 0 {
				testNum := tohostVal >> 1
				t.Fatalf("test case %d failed (tohost=0x%x)", testNum, tohostVal)
			}

			// Some tests might write other values; just log and continue
			t.Logf("tohost=0x%x at instruction %d", tohostVal, i)
		}
	}

	t.Fatalf("test timed out after %d instructions (PC=0x%x, tohost=0x%x)",
		maxInstructions, cpu.PC, lastTohostVal)
}

// readTohost reads the tohost value from RAM.
// tohost is a 64-bit value at the specified address.
func readTohost(physMem []byte, addr, ramBase uint64) uint64 {
	if addr < ramBase || addr+8 > ramBase+uint64(len(physMem)) {
		return 0
	}
	offset := addr - ramBase
	return binary.LittleEndian.Uint64(physMem[offset:])
}
