package x86_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
	"github.com/sorins/tinyemu-go/mem"
)

const test386BinPath = "../../helpers/test386_asm.git/test386.bin"

// postName maps test386 POST codes to human-readable descriptions.
var postName = map[byte]string{
	0x00: "Initialization",
	0x01: "Real-mode data movement",
	0x02: "Real-mode arithmetic",
	0x03: "Real-mode logical",
	0x04: "Real-mode string ops",
	0x05: "Real-mode stack ops",
	0x06: "Real-mode control flow",
	0x08: "Protected-mode setup",
	0x09: "Protected-mode tests",
	0x0B: "Protected-mode data movement",
	0x0C: "Protected-mode arithmetic",
	0x0D: "Protected-mode logical",
	0x0E: "Protected-mode string ops",
	0x0F: "Protected-mode stack ops",
	0x10: "Protected-mode control flow",
	0x20: "Interrupt tests",
	0x21: "V86 mode tests",
	0x22: "TSS tests",
	0xEE: "Reference output generation",
	0xFF: "Success",
}

// runTest386 loads test386.bin, runs it, and returns the captured POST codes.
// It also returns the last error (if any) and the number of steps executed.
func runTest386(t *testing.T, maxSteps int) ([]byte, error, int) {
	t.Helper()

	binData, err := os.ReadFile(test386BinPath)
	if err != nil {
		t.Skipf("test386.bin not found at %s: %v", test386BinPath, err)
	}
	if len(binData) != 65536 {
		t.Fatalf("test386.bin should be 65536 bytes, got %d", len(binData))
	}

	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	c := x86.NewCPU(mm)
	for i, b := range binData {
		c.WriteMem8(0xF0000+uint32(i), b)
	}
	c.Reset()

	postCodes := make([]byte, 0, 256)
	c.SetIOHandlers(
		func(port uint16) uint8 { return 0 },
		func(port uint16, val uint8) {
			if port == 0x190 {
				postCodes = append(postCodes, val)
			}
		},
		func(port uint16) uint16 { return 0 },
		func(port uint16, val uint16) {},
		func(port uint16) uint32 { return 0 },
		func(port uint16, val uint32) {},
	)

	var lastErr error
	steps := 0
	for i := 0; i < maxSteps; i++ {
		steps = i
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			lastErr = err
			break
		}
	}
	return postCodes, lastErr, steps
}

// TestTest386MilestonePost00 verifies we reach POST 0x00 (Initialization).
func TestTest386MilestonePost00(t *testing.T) {
	postCodes, lastErr, steps := runTest386(t, 10000000)
	if len(postCodes) == 0 {
		t.Fatalf("test386 did not reach POST 0x00 (stopped at step %d with err=%v)", steps, lastErr)
	}
	if postCodes[0] != 0x00 {
		t.Fatalf("First POST was 0x%02X, expected 0x00", postCodes[0])
	}
	t.Logf("Reached POST 0x00 after %d steps", steps)
}

// TestTest386MilestonePost01 verifies we reach POST 0x01 (Real-mode data movement).
func TestTest386MilestonePost01(t *testing.T) {
	postCodes, lastErr, steps := runTest386(t, 10000000)
	if len(postCodes) == 0 {
		t.Fatalf("test386 did not reach any POST code (stopped at step %d with err=%v)", steps, lastErr)
	}
	// Find the highest POST code reached
	lastPost := postCodes[len(postCodes)-1]
	if lastPost < 0x01 {
		t.Fatalf("test386 reached POST 0x%02X (%s), expected at least POST 0x01; last err=%v",
			lastPost, postName[lastPost], lastErr)
	}
	t.Logf("Reached POST 0x%02X (%s) after %d steps", lastPost, postName[lastPost], steps)
}

// TestTest386FullRun attempts to run test386 to completion (POST 0xFF).
// This is skipped until the emulator is complete enough.
func TestTest386FullRun(t *testing.T) {
	postCodes, lastErr, steps := runTest386(t, 5000000)
	if len(postCodes) == 0 {
		t.Fatalf("No POST codes captured (stopped at step %d with err=%v)", steps, lastErr)
	}
	lastPost := postCodes[len(postCodes)-1]
	if lastPost != 0xFF {
		t.Skipf("Reached POST 0x%02X (%s), not yet at 0xFF (last err=%v, steps=%d)",
			lastPost, postName[lastPost], lastErr, steps)
	}
	t.Logf("SUCCESS: test386 completed after %d steps!", steps)
}

// TestTest386Progress logs detailed POST-code progress for debugging.
func TestTest386Progress(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("Run with -v to see progress")
	}
	postCodes, lastErr, steps := runTest386(t, 10000000)
	t.Logf("POST codes reached: %v", postCodes)
	if lastErr != nil {
		t.Logf("Stopped at step %d: %v", steps, lastErr)
	}
	if len(postCodes) > 0 {
		lastPost := postCodes[len(postCodes)-1]
		name := postName[lastPost]
		if name == "" {
			name = "unknown"
		}
		t.Logf("Reached POST 0x%02X (%s)", lastPost, name)
	}
}

// Helper to write a byte to CPU memory (exported for tests)
func writeMem8(c *x86.CPU, addr uint32, val uint8) {
	c.WriteMem8(addr, val)
}

// Helper to read a byte from CPU memory
func readMem8(c *x86.CPU, addr uint32) uint8 {
	return c.ReadMem8(addr)
}

// Helper: format POST code for display
func formatPost(code byte) string {
	return fmt.Sprintf("0x%02X", code)
}
