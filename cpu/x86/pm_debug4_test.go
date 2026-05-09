package x86

import (
	"fmt"
	"testing"
)

func TestPMDebug4(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	gdtAddr := uint32(0x2000)
	for i := 0; i < 32; i++ {
		c.writeMem8(gdtAddr+uint32(i), 0)
	}
	c.writeMem8(gdtAddr+0, 0x17)
	c.writeMem8(gdtAddr+1, 0x00)
	c.writeMem8(gdtAddr+2, 0x00)
	c.writeMem8(gdtAddr+3, 0x20)
	c.writeMem8(gdtAddr+4, 0x00)
	c.writeMem8(gdtAddr+8, 0xFF)
	c.writeMem8(gdtAddr+9, 0xFF)
	c.writeMem8(gdtAddr+10, 0x00)
	c.writeMem8(gdtAddr+11, 0x00)
	c.writeMem8(gdtAddr+12, 0x00)
	c.writeMem8(gdtAddr+13, 0x9A)
	c.writeMem8(gdtAddr+14, 0xCF)
	c.writeMem8(gdtAddr+15, 0x00)
	c.writeMem8(gdtAddr+16, 0xFF)
	c.writeMem8(gdtAddr+17, 0xFF)
	c.writeMem8(gdtAddr+18, 0x00)
	c.writeMem8(gdtAddr+19, 0x00)
	c.writeMem8(gdtAddr+20, 0x00)
	c.writeMem8(gdtAddr+21, 0x92)
	c.writeMem8(gdtAddr+22, 0xCF)
	c.writeMem8(gdtAddr+23, 0x00)

	code := []byte{
		0x0F, 0x01, 0x16, 0x00, 0x20,
		0x0F, 0x20, 0xC0,
		0x83, 0xC8, 0x01,
		0x0F, 0x22, 0xC0,
		0x66, 0xEA,
	}
	pmStart := uint32(0x1000 + len(code) + 6)
	code = append(code, byte(pmStart), byte(pmStart>>8), byte(pmStart>>16), byte(pmStart>>24))
	code = append(code, 0x08, 0x00)
	code = append(code, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE, 0xF4)

	fmt.Printf("pmStart=%08X len=%d\n", pmStart, len(code))
	for i := 0; i < 6; i++ {
		b, _ := c.memMap.Read8(uint64(0x1010 + i))
		fmt.Printf("mem[%04X] = %02X\n", 0x1010+i, b)
	}

	if err := runCode(t, c, code, 0x1000); err != nil {
		fmt.Printf("ERROR: %v\n", err)
		// Continue to print state
	}
	fmt.Printf("Final: EIP=%08X CS=%04X PE=%v EAX=%08X\n", c.GetEIP(), c.GetSeg(CS), c.IsProtectedMode(), c.GetReg32(EAX))
}
