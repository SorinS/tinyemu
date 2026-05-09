// Package virtio provides VirtIO device emulation.
// This file contains bare-metal tests for VirtIO devices.
//
// Reference: testdata/bare-metal/ (test binaries)
// Reference: test/isatest/runner.go (test runner pattern)

package virtio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/riscv"
	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/mem"
	"github.com/jtolio/tinyemu-go/test/isatest/elfloader"
)

// bareMetalTestDir returns the path to the bare-metal test binaries.
func bareMetalTestDir(t *testing.T) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get caller information")
	}

	// Go from virtio/baremetal_test.go to testdata/bare-metal/
	dir := filepath.Dir(filename)
	testDir := filepath.Join(dir, "..", "testdata", "bare-metal")

	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skipf("bare-metal test directory not found: %s", testDir)
	}

	return testDir
}

// bareMetalTestConfig holds configuration for a bare-metal test.
type bareMetalTestConfig struct {
	Name       string
	BinaryName string
	SetupFunc  func(t *testing.T, memMap *mem.PhysMemoryMap, plic *devices.PLIC, cpu *riscv.CPU) error
	MaxCycles  int
}

// runBareMetalTest runs a single bare-metal test binary.
//
// Reference: test/isatest/runner.go (similar test execution pattern)
func runBareMetalTest(t *testing.T, cfg bareMetalTestConfig) {
	testDir := bareMetalTestDir(t)
	binaryPath := filepath.Join(testDir, cfg.BinaryName)

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skipf("test binary not found: %s (run 'make' in testdata/bare-metal/)", binaryPath)
	}

	// Load ELF
	elfInfo, err := elfloader.Load(binaryPath)
	if err != nil {
		t.Fatalf("failed to load ELF: %v", err)
	}

	// Create memory map
	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000 (standard RISC-V RAM base)
	const ramBase = 0x80000000
	const ramSize = 64 * 1024 * 1024 // 64 MB
	ram, err := memMap.RegisterRAM(ramBase, ramSize, 0)
	if err != nil {
		t.Fatalf("failed to allocate RAM: %v", err)
	}

	// Load ELF segments into RAM
	for _, seg := range elfInfo.Segments {
		if seg.VAddr < ramBase || seg.VAddr+uint64(len(seg.Data)) > ramBase+ramSize {
			t.Fatalf("segment at 0x%x out of RAM range", seg.VAddr)
		}
		offset := seg.VAddr - ramBase
		copy(ram.PhysMem[offset:], seg.Data)
	}

	// Create CPU
	testCPU := riscv.NewCPU(memMap, riscv.XLEN64)
	testCPU.PC = elfInfo.EntryPoint
	testCPU.FS = riscv.FSDirty // Enable floating point

	// Create PLIC and register it
	plic := devices.NewPLIC(testCPU)
	if _, err := plic.Register(memMap); err != nil {
		t.Fatalf("failed to register PLIC: %v", err)
	}

	// Run device-specific setup
	if cfg.SetupFunc != nil {
		if err := cfg.SetupFunc(t, memMap, plic, testCPU); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	// Find tohost address
	var toHostAddr uint64
	if elfInfo.ToHostAddr != nil {
		toHostAddr = *elfInfo.ToHostAddr
	} else {
		toHostAddr = 0x80001000 // Default location
	}

	// Run test
	maxCycles := cfg.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 1000000
	}

	var exitCode uint64
	halted := false

	for i := 0; i < maxCycles && !halted; i++ {
		// Check tohost
		val, err := memMap.Read64(toHostAddr)
		if err == nil && val != 0 {
			if val&1 != 0 {
				exitCode = val >> 1
				halted = true
				break
			}
		}

		// Check for pending interrupts before executing
		// Note: CPU.Step() doesn't check interrupts; CPU.Run() does.
		// We need to manually check interrupts when using Step().
		testCPU.CheckInterrupts()

		// Execute one instruction
		if err := testCPU.Step(); err != nil {
			// Handle exceptions
			if testCPU.HasPendingException() {
				testCPU.ClearPendingException()
			}
		}

		// Check for power-down (WFI)
		if testCPU.IsPowerDown() {
			// Check if there are pending interrupts
			mip := testCPU.GetMIP()
			mie := testCPU.Mie
			if mip&mie != 0 {
				testCPU.SetPowerDown(false)
			}
		}
	}

	if !halted {
		t.Fatalf("test timed out after %d cycles", maxCycles)
	}

	if exitCode != 0 {
		t.Fatalf("test failed with exit code %d (test number %d)", exitCode, exitCode)
	}
}

// TestBareMetalHelloVirtIOConsole tests VirtIO console TX functionality.
// This test verifies that:
//   - VirtIO console device is correctly detected
//   - TX queue can be set up and used
//   - Data sent through TX queue is received by the backend
//
// Reference: testdata/bare-metal/hello_virtio_console.S
func TestBareMetalHelloVirtIOConsole(t *testing.T) {
	var consoleOutput bytes.Buffer

	runBareMetalTest(t, bareMetalTestConfig{
		Name:       "hello_virtio_console",
		BinaryName: "hello_virtio_console",
		MaxCycles:  500000,
		SetupFunc: func(t *testing.T, memMap *mem.PhysMemoryMap, plic *devices.PLIC, testCPU *riscv.CPU) error {
			// Create VirtIO console at 0x40010000 (first VirtIO slot)
			const virtioAddr = 0x40010000

			// Get IRQ 1 from PLIC
			irqs := plic.CreateIRQs()
			irq := irqs[1]

			// Create console device
			charDev := &CharacterDevice{
				Writer: &consoleOutput,
			}

			_, err := NewConsole(memMap, virtioAddr, irq, charDev)
			if err != nil {
				return fmt.Errorf("failed to create console: %w", err)
			}

			return nil
		},
	})

	// Verify output
	expected := "Hello World\n"
	if consoleOutput.String() != expected {
		t.Errorf("console output mismatch:\n  got: %q\n  want: %q", consoleOutput.String(), expected)
	}
}

// TestBareMetalReadVirtIOBlock tests VirtIO block device read functionality.
// This test verifies that:
//   - VirtIO block device is correctly detected
//   - Block read requests work correctly
//   - Data is read correctly from the block device
//
// Reference: testdata/bare-metal/read_virtio_block.S
func TestBareMetalReadVirtIOBlock(t *testing.T) {
	// Use the existing MemoryBlockDevice from devices package for better compatibility
	sectorSize := 512
	numSectors := 16
	data := make([]byte, sectorSize*numSectors)

	// Write magic value at start of sector 0 (little-endian)
	binary.LittleEndian.PutUint32(data[0:4], 0xDEADBEEF)

	blockDev := devices.NewMemoryBlockDeviceFromData(data)

	runBareMetalTest(t, bareMetalTestConfig{
		Name:       "read_virtio_block",
		BinaryName: "read_virtio_block",
		MaxCycles:  500000,
		SetupFunc: func(t *testing.T, memMap *mem.PhysMemoryMap, plic *devices.PLIC, testCPU *riscv.CPU) error {
			// Create VirtIO block device at 0x40010000 (first VirtIO slot)
			const virtioAddr = 0x40010000

			// Get IRQ 1 from PLIC
			irqs := plic.CreateIRQs()
			irq := irqs[1]

			_, err := NewBlockDevice(memMap, virtioAddr, irq, blockDev)
			if err != nil {
				return fmt.Errorf("failed to create block device: %w", err)
			}

			return nil
		},
	})
}

// TestBareMetalVirtIOInterrupt tests VirtIO device interrupt delivery.
// This test verifies:
//   - VirtIO device can raise interrupts through PLIC
//   - CPU receives and handles external interrupts
//   - Interrupt acknowledge flow works correctly
//
// Reference: testdata/bare-metal/virtio_interrupt_test.S
func TestBareMetalVirtIOInterrupt(t *testing.T) {
	var consoleOutput bytes.Buffer

	runBareMetalTest(t, bareMetalTestConfig{
		Name:       "virtio_interrupt_test",
		BinaryName: "virtio_interrupt_test",
		MaxCycles:  500000,
		SetupFunc: func(t *testing.T, memMap *mem.PhysMemoryMap, plic *devices.PLIC, testCPU *riscv.CPU) error {
			// Create VirtIO console at 0x40010000 (first VirtIO slot)
			const virtioAddr = 0x40010000

			// Get IRQ 1 from PLIC
			irqs := plic.CreateIRQs()
			irq := irqs[1]

			// Create console device
			charDev := &CharacterDevice{
				Writer: &consoleOutput,
			}

			_, err := NewConsole(memMap, virtioAddr, irq, charDev)
			if err != nil {
				return fmt.Errorf("failed to create console: %w", err)
			}

			return nil
		},
	})
}
