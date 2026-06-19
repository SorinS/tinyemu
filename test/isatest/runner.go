package isatest

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/sorins/tinyemu-go/cpu/riscv"
	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/test/isatest/elfloader"
)

// RunConfig specifies options for running an ISA test.
type RunConfig struct {
	// MaxCycles is the maximum number of cycles to run before timing out.
	// Default: 100000
	MaxCycles int

	// XLEN specifies the register width (32 or 64).
	// Default: 64
	XLEN riscv.XLEN

	// RAMBase is the base address for RAM.
	// Default: 0x80000000
	RAMBase uint64

	// RAMSize is the size of RAM in bytes.
	// Default: 64 MB
	RAMSize uint64

	// Verbose enables debug output.
	Verbose bool
}

// DefaultConfig returns a default RunConfig.
func DefaultConfig() RunConfig {
	return RunConfig{
		MaxCycles: 100000,
		XLEN:      riscv.XLEN64,
		RAMBase:   0x80000000,
		RAMSize:   64 * 1024 * 1024,
	}
}

// RunResult contains the result of running an ISA test.
type RunResult struct {
	// Cycles is the number of cycles executed.
	Cycles uint64

	// ExitCode is the value written to tohost (if any).
	ExitCode uint64

	// Signature is the extracted signature data (if begin_signature/end_signature found).
	Signature []byte

	// HaltReason describes why execution stopped.
	HaltReason string
}

// Runner executes RISC-V ISA compliance tests.
type Runner struct {
	config RunConfig
	cpu    *riscv.CPU
	mem    *mem.PhysMemoryMap
	ram    *mem.PhysMemoryRange

	// ELF information
	elfInfo *elfloader.Info

	// tohost handling
	toHostAddr    uint64
	toHostValid   bool
	toHostWritten uint64
	halted        bool
}

// NewRunner creates a new ISA test runner with the given configuration.
func NewRunner(config RunConfig) *Runner {
	return &Runner{
		config: config,
	}
}

// LoadELF loads an ELF file and prepares the runner for execution.
func (r *Runner) LoadELF(path string) error {
	info, err := elfloader.Load(path)
	if err != nil {
		return err
	}
	return r.loadELFInfo(info)
}

// LoadELFBytes loads an ELF from a byte slice.
func (r *Runner) LoadELFBytes(data []byte) error {
	info, err := elfloader.LoadFromBytes(data)
	if err != nil {
		return err
	}
	return r.loadELFInfo(info)
}

// loadELFInfo initializes the runner with the given ELF information.
func (r *Runner) loadELFInfo(info *elfloader.Info) error {
	r.elfInfo = info

	// Create memory map
	r.mem = mem.NewPhysMemoryMap()

	// Allocate RAM
	var err error
	r.ram, err = r.mem.RegisterRAM(r.config.RAMBase, r.config.RAMSize, 0)
	if err != nil {
		return fmt.Errorf("failed to allocate RAM: %w", err)
	}

	// Load segments into memory
	for _, seg := range info.Segments {
		if err := r.writeMemory(seg.VAddr, seg.Data); err != nil {
			return fmt.Errorf("failed to load segment at 0x%x: %w", seg.VAddr, err)
		}
	}

	// Create CPU
	r.cpu = riscv.NewCPU(r.mem, r.config.XLEN)

	// Set entry point
	r.cpu.PC = info.EntryPoint

	// Enable floating-point if available
	r.cpu.FS = riscv.FSDirty

	// Set up tohost monitoring
	if info.ToHostAddr != nil {
		r.toHostAddr = *info.ToHostAddr
		r.toHostValid = true
	}

	return nil
}

// writeMemory writes data to physical memory.
func (r *Runner) writeMemory(addr uint64, data []byte) error {
	// Check if address is within RAM
	if addr < r.config.RAMBase || addr+uint64(len(data)) > r.config.RAMBase+r.config.RAMSize {
		return fmt.Errorf("address 0x%x-0x%x out of RAM range", addr, addr+uint64(len(data)))
	}

	offset := addr - r.config.RAMBase
	copy(r.ram.PhysMem[offset:], data)
	return nil
}

// readMemory reads data from physical memory.
func (r *Runner) readMemory(addr uint64, size int) ([]byte, error) {
	// Check if address is within RAM
	if addr < r.config.RAMBase || addr+uint64(size) > r.config.RAMBase+r.config.RAMSize {
		return nil, fmt.Errorf("address 0x%x-0x%x out of RAM range", addr, addr+uint64(size))
	}

	offset := addr - r.config.RAMBase
	data := make([]byte, size)
	copy(data, r.ram.PhysMem[offset:offset+uint64(size)])
	return data, nil
}

// Run executes the loaded test until completion or timeout.
func (r *Runner) Run() (*RunResult, error) {
	if r.cpu == nil {
		return nil, fmt.Errorf("no ELF loaded")
	}

	result := &RunResult{}
	maxCycles := r.config.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 100000
	}

	for i := 0; i < maxCycles && !r.halted; i++ {
		// Check tohost before each instruction
		if r.toHostValid {
			val, err := r.mem.Read64(r.toHostAddr)
			if err == nil && val != 0 && val != r.toHostWritten {
				r.toHostWritten = val
				// tohost protocol: bit 0 = halt, bits 63:1 = exit code
				if val&1 != 0 {
					result.ExitCode = val >> 1
					r.halted = true
					result.HaltReason = "tohost"
					break
				}
			}
		}

		// Execute one instruction
		err := r.cpu.Step()
		if err != nil {
			// Check if this is an ECALL
			if r.cpu.HasPendingException() {
				// Handle exception - for ISA tests, ECALL typically means halt
				r.cpu.ClearPendingException()
				if r.config.Verbose {
					fmt.Printf("Exception at PC=0x%x\n", r.cpu.PC)
				}
			}
		}

		result.Cycles++

		// Check for power-down (WFI)
		if r.cpu.IsPowerDown() {
			result.HaltReason = "wfi"
			break
		}
	}

	if !r.halted && result.HaltReason == "" {
		result.HaltReason = "timeout"
	}

	// Extract signature if available
	if r.elfInfo.BeginSignature != nil && r.elfInfo.EndSignature != nil {
		begin := *r.elfInfo.BeginSignature
		end := *r.elfInfo.EndSignature
		if end > begin {
			sig, err := r.readMemory(begin, int(end-begin))
			if err == nil {
				result.Signature = sig
			}
		}
	}

	return result, nil
}

// FormatSignature formats the signature as hex lines (4 bytes per line, big-endian).
// This matches the RISC-V ISA test reference signature format.
func FormatSignature(sig []byte) string {
	var lines []string
	for i := 0; i < len(sig); i += 4 {
		end := i + 4
		if end > len(sig) {
			end = len(sig)
		}
		// Read as little-endian 32-bit word, output as hex
		word := uint32(0)
		for j := 0; j < end-i; j++ {
			word |= uint32(sig[i+j]) << (j * 8)
		}
		lines = append(lines, fmt.Sprintf("%08x", word))
	}
	return strings.Join(lines, "\n")
}

// FormatSignature64 formats the signature as hex lines (8 bytes per line).
// This is used for 64-bit tests.
func FormatSignature64(sig []byte) string {
	var lines []string
	for i := 0; i < len(sig); i += 8 {
		end := i + 8
		if end > len(sig) {
			end = len(sig)
		}
		// Read as little-endian 64-bit word, output as hex
		word := uint64(0)
		for j := 0; j < end-i; j++ {
			word |= uint64(sig[i+j]) << (j * 8)
		}
		lines = append(lines, fmt.Sprintf("%016x", word))
	}
	return strings.Join(lines, "\n")
}

// CompareSignature compares a computed signature against a reference file.
func CompareSignature(computed []byte, referencePath string) error {
	ref, err := os.ReadFile(referencePath)
	if err != nil {
		return fmt.Errorf("failed to read reference: %w", err)
	}

	// Parse reference file (hex lines)
	refLines := strings.Split(strings.TrimSpace(string(ref)), "\n")
	var refBytes []byte
	for _, line := range refLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b, err := hex.DecodeString(line)
		if err != nil {
			return fmt.Errorf("invalid hex in reference: %w", err)
		}
		// Reference is big-endian hex, reverse for little-endian memory
		for i := len(b) - 1; i >= 0; i-- {
			refBytes = append(refBytes, b[i])
		}
	}

	if !bytes.Equal(computed, refBytes) {
		return fmt.Errorf("signature mismatch:\ncomputed:\n%s\nexpected:\n%s",
			FormatSignature(computed), FormatSignature(refBytes))
	}

	return nil
}

// RunTest is a convenience function that loads and runs a test, returning the result.
func RunTest(elfPath string, config RunConfig) (*RunResult, error) {
	runner := NewRunner(config)
	if err := runner.LoadELF(elfPath); err != nil {
		return nil, err
	}
	return runner.Run()
}

// RunTestWithReference runs a test and compares against a reference signature file.
func RunTestWithReference(elfPath, refPath string, config RunConfig) error {
	result, err := RunTest(elfPath, config)
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("test failed with exit code %d", result.ExitCode)
	}

	if result.HaltReason == "timeout" {
		return fmt.Errorf("test timed out after %d cycles", result.Cycles)
	}

	if result.Signature == nil {
		return fmt.Errorf("no signature found in test")
	}

	return CompareSignature(result.Signature, refPath)
}
