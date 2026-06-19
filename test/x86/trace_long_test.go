package x86_test

import (
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
	"github.com/sorins/tinyemu-go/mem"
)

func TestTest386TraceLong(t *testing.T) {
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

	for i := 0; i < 5000000; i++ {
		if c.IsPowerDown() {
			t.Logf("HALT at step %d", i)
			break
		}
		eip := c.GetEIP()
		// Log every 10000 steps and when EIP changes from the loop area
		if i%50000 == 0 || (eip < 0x380 || eip > 0x4E0) {
			lip := c.GetLIP()
			b0 := c.ReadMem8(lip)
			t.Logf("step %d: EIP=%04X byte=%02X EAX=%08X ECX=%08X EFL=%08X",
				i, eip, b0,
				c.GetReg32(x86.EAX), c.GetReg32(x86.ECX), c.GetEFLAGS())
		}
		if err := c.Step(); err != nil {
			t.Logf("Error at step %d EIP=%04X: %v", i, eip, err)
			break
		}
	}
}
