package x86

import (
	"fmt"
	"testing"
)

func TestDebugMovRegImm2(t *testing.T) {
	c := newTestCPU(t)
	fmt.Printf("CS=%04X base=%05X EIP=%04X PE=%v segAccess=%04X\n", c.GetSeg(CS), c.GetSegBase(CS), c.GetEIP(), c.IsProtectedMode(), c.GetSegAccess(CS))
	code := []byte{
		0xB8, 0x78, 0x56, 0x34, 0x12,
		0xB9, 0xEF, 0xBE, 0xAD, 0xDE,
		0xF4,
	}
	for i, b := range code {
		c.writeMem8(0x1000+uint32(i), b)
	}
	c.SetEIP(0x1000)
	for i := 0; i < 20; i++ {
		lip := c.GetLIP()
		b, _ := c.memMap.Read8(uint64(lip))
		fmt.Printf("Step %d: EIP=%04X LIP=%05X byte=%02X EAX=%08X ECX=%08X\n", i, c.GetEIP(), lip, b, c.GetReg32(EAX), c.GetReg32(ECX))
		if err := c.Step(); err != nil {
			fmt.Printf("ERROR: %v\n", err)
			break
		}
		if c.IsPowerDown() {
			fmt.Println("HLT")
			break
		}
	}
	fmt.Printf("Final EAX=%08X\n", c.GetReg32(EAX))
}
