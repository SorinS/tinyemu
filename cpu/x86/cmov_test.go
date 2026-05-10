package x86

import "testing"

// TestCMOVcc_True covers each condition that should DO the move when the
// corresponding flag is in the matching state.
func TestCMOVcc_True(t *testing.T) {
	cases := []struct {
		name     string
		opcode2  byte
		setFlags func(c *CPU)
	}{
		{"CMOVO (OF=1)", 0x40, func(c *CPU) { c.setOF(true) }},
		{"CMOVNO (OF=0)", 0x41, func(c *CPU) { c.setOF(false) }},
		{"CMOVB (CF=1)", 0x42, func(c *CPU) { c.setCF(true) }},
		{"CMOVAE (CF=0)", 0x43, func(c *CPU) { c.setCF(false) }},
		{"CMOVE (ZF=1)", 0x44, func(c *CPU) { c.setZF(true) }},
		{"CMOVNE (ZF=0)", 0x45, func(c *CPU) { c.setZF(false) }},
		{"CMOVBE (CF=1)", 0x46, func(c *CPU) { c.setCF(true); c.setZF(false) }},
		{"CMOVA (CF=0,ZF=0)", 0x47, func(c *CPU) { c.setCF(false); c.setZF(false) }},
		{"CMOVS (SF=1)", 0x48, func(c *CPU) { c.setSF(true) }},
		{"CMOVNS (SF=0)", 0x49, func(c *CPU) { c.setSF(false) }},
		{"CMOVP (PF=1)", 0x4A, func(c *CPU) { c.setPF(true) }},
		{"CMOVNP (PF=0)", 0x4B, func(c *CPU) { c.setPF(false) }},
		{"CMOVL (SF!=OF)", 0x4C, func(c *CPU) { c.setSF(true); c.setOF(false) }},
		{"CMOVGE (SF==OF)", 0x4D, func(c *CPU) { c.setSF(false); c.setOF(false) }},
		{"CMOVLE (ZF=1)", 0x4E, func(c *CPU) { c.setZF(true); c.setSF(false); c.setOF(false) }},
		{"CMOVG (ZF=0,SF==OF)", 0x4F, func(c *CPU) { c.setZF(false); c.setSF(false); c.setOF(false) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCPU(t)
			c.SetReg32(EAX, 0x11111111) // dest
			c.SetReg32(EBX, 0x22222222) // source
			tc.setFlags(c)
			// CMOVcc EAX, EBX = 0F 4? C3
			code := []byte{0x0F, tc.opcode2, 0xC3, 0xF4}
			if err := runCode(t, c, code, 0x1000); err != nil {
				t.Fatalf("runCode: %v", err)
			}
			if got := c.GetReg32(EAX); got != 0x22222222 {
				t.Errorf("EAX = 0x%08X, want 0x22222222 (CMOV should have taken)", got)
			}
		})
	}
}

// TestCMOVcc_False covers the inverse: condition not met, dest preserved.
func TestCMOVcc_False(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xAAAAAAAA)
	c.SetReg32(EBX, 0xBBBBBBBB)
	c.setZF(false) // ZF=0
	code := []byte{0x0F, 0x44, 0xC3, 0xF4} // CMOVE EAX, EBX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xAAAAAAAA {
		t.Errorf("EAX = 0x%08X, want 0xAAAAAAAA (CMOVE should NOT have taken with ZF=0)", got)
	}
}

// TestCMOVcc_16bit verifies the 0x66 operand-size-prefixed 16-bit form moves
// only the low 16 bits, leaving the upper half untouched.
func TestCMOVcc_16bit(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xAAAA1111)
	c.SetReg32(EBX, 0xBBBB2222)
	c.setZF(true)
	code := []byte{0x66, 0x0F, 0x44, 0xC3, 0xF4} // CMOVE AX, BX
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xAAAA2222 {
		t.Errorf("EAX = 0x%08X, want 0xAAAA2222 (only low 16 bits moved)", got)
	}
}
