package x86_test

import (
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
	"github.com/sorins/tinyemu-go/mem"
)

func TestTest386DebugLoop(t *testing.T) {
	binData, err := os.ReadFile(test386BinPath)
	if err != nil {
		t.Skipf("test386.bin not found: %v", err)
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

	for i := 0; i < 400000; i++ {
		if c.IsPowerDown() {
			t.Logf("HALT at step %d", i)
			break
		}
		eip := c.GetEIP()
		if i >= 394000 {
			lip := c.GetLIP()
			b0 := c.ReadMem8(lip)
			b1 := c.ReadMem8(lip + 1)
			b2 := c.ReadMem8(lip + 2)
			t.Logf("step %d: EIP=%04X bytes=%02X %02X %02X EAX=%08X ECX=%08X EFL=%08X",
				i, eip, b0, b1, b2,
				c.GetReg32(x86.EAX), c.GetReg32(x86.ECX), c.GetEFLAGS())
		}
		if err := c.Step(); err != nil {
			t.Logf("Error at step %d EIP=%04X: %v", i, eip, err)
			break
		}
	}
}
