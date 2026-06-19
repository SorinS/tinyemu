package x86_64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// Byte-form DIV/IDIV (Group 3, F6 /6 and /7) are special: the dividend is
// the 16-bit AX register (quotient → AL, remainder → AH), NOT the DX:AX
// shape of the 16/32/64-bit forms. opDIV/opIDIV originally fell through to
// the DX:AX path for the byte case, using DL as the high half of the
// dividend — so a perfectly valid `DIV DL` raised a bogus #DE.
//
// This was the first wall booting OVMF: at RIP 0xFFFD1115 OVMF runs
// `F6 F2` (DIV DL) with AX=0x005D, DL=0x09 (93 / 9 = 10 r 3). The buggy
// path computed (DL<<8)|AL = 0x095D, 0x095D/9 = 0x10A > 0xFF, and faulted.

func TestDIV_Byte_OVMFCase(t *testing.T) {
	// DIV DL with AX=0x005D, DL=0x09 → AL=0x0A (10), AH=0x03 (3).
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x005D)
		c.SetReg64(RDX, 0x09)
	}, []byte{0xF6, 0xF2, 0xF4}) // div dl ; hlt
	if al := c.GetReg8(AL); al != 0x0A {
		t.Errorf("AL (quotient) = %#x, want 0x0A", al)
	}
	if ah := c.GetReg8(AH); ah != 0x03 {
		t.Errorf("AH (remainder) = %#x, want 0x03", ah)
	}
}

func TestDIV_Byte_UsesAXNotDX(t *testing.T) {
	// AX=0x0102 (258), DL=0x10 (16) → 258/16 = 16 r 2 → AL=0x10, AH=0x02.
	// If the dividend high half were taken from DL (here 0x10) instead of
	// AH, the result would be (0x10<<8|0x02)/0x10 = 0x100 → bogus #DE.
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x0102)
		c.SetReg64(RDX, 0x10)
	}, []byte{0xF6, 0xF2, 0xF4}) // div dl ; hlt
	if al := c.GetReg8(AL); al != 0x10 {
		t.Errorf("AL (quotient) = %#x, want 0x10", al)
	}
	if ah := c.GetReg8(AH); ah != 0x02 {
		t.Errorf("AH (remainder) = %#x, want 0x02", ah)
	}
}

func TestIDIV_Byte_Signed(t *testing.T) {
	// IDIV DL with AX = -100 (0xFF9C), DL = 7 → -100/7 = -14 r -2.
	// AL = -14 = 0xF2, AH = -2 = 0xFE.
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0xFF9C)
		c.SetReg64(RDX, 0x07)
	}, []byte{0xF6, 0xFA, 0xF4}) // idiv dl ; hlt
	if al := c.GetReg8(AL); al != 0xF2 {
		t.Errorf("AL (quotient) = %#x, want 0xF2 (-14)", al)
	}
	if ah := c.GetReg8(AH); ah != 0xFE {
		t.Errorf("AH (remainder) = %#x, want 0xFE (-2)", ah)
	}
}
