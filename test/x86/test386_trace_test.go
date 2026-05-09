package x86_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/mem"
)

func TestTest386TraceLoop(t *testing.T) {
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

	for i := 0; i < 150000; i++ {
		if c.IsPowerDown() {
			t.Logf("HALT at step %d", i)
			break
		}
		eip := c.GetEIP()
		if i >= 120000 || (eip >= 0x380 && eip <= 0x400) {
			lip := c.GetLIP()
			b0 := c.ReadMem8(lip)
			b1 := c.ReadMem8(lip + 1)
			b2 := c.ReadMem8(lip + 2)
			b3 := c.ReadMem8(lip + 3)
			t.Logf("step %d: EIP=%04X bytes=%02X %02X %02X %02X EAX=%08X ECX=%08X EFL=%08X",
				i, eip, b0, b1, b2, b3,
				c.GetReg32(x86.EAX), c.GetReg32(x86.ECX), c.GetEFLAGS())
		}
		if err := c.Step(); err != nil {
			t.Logf("Error at step %d EIP=%04X: %v", i, eip, err)
			break
		}
	}
}

func TestTest386DumpAround(t *testing.T) {
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

	for i := 0; i < 200000; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Logf("Error at step %d EIP=%04X: %v", i, c.GetEIP(), err)
			break
		}
	}

	// Dump memory around 0xF039B
	base := uint32(0xF0390)
	for row := uint32(0); row < 8; row++ {
		addr := base + row*16
		line := fmt.Sprintf("%05X: ", addr)
		for col := uint32(0); col < 16; col++ {
			line += fmt.Sprintf("%02X ", c.ReadMem8(addr+col))
		}
		line += " |"
		for col := uint32(0); col < 16; col++ {
			b := c.ReadMem8(addr + col)
			if b >= 0x20 && b < 0x7F {
				line += string(rune(b))
			} else {
				line += "."
			}
		}
		line += "|"
		t.Log(line)
	}

	t.Logf("EIP=%04X EAX=%08X ECX=%08X EFL=%08X",
		c.GetEIP(), c.GetReg32(x86.EAX), c.GetReg32(x86.ECX), c.GetEFLAGS())
}
