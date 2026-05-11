package x86

import "testing"

// Intel SDM: BT/BTS/BTR/BTC set CF (carry flag) based on the original bit
// value. ZF, SF, OF, AF, PF are undefined. Older revisions of this code
// incorrectly set ZF; that bug would silently break Linux's bitmap-based
// subsystems (test_bit, set_bit, clear_bit) and was the root cause of an
// early-boot kernel crash. These tests pin down the CF semantics.

// 0F A3 BT r/m32, r32
func TestBTRegReg32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000008) // bit 3 set
	c.SetReg32(ECX, 3)
	code := []byte{0x0F, 0xA3, 0xCB, 0xF4} // BT EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 for BT of set bit")
	}

	c = newTestCPU(t)
	c.SetReg32(EBX, 0x00000008) // bit 3 set
	c.SetReg32(ECX, 4)
	code = []byte{0x0F, 0xA3, 0xCB, 0xF4} // BT EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 for BT of clear bit")
	}
}

// 0F AB BTS r/m32, r32
func TestBTSRegReg32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0)
	c.SetReg32(ECX, 5)
	code := []byte{0x0F, 0xAB, 0xCB, 0xF4} // BTS EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 (bit was clear before BTS)")
	}
	if c.GetReg32(EBX) != 0x20 {
		t.Errorf("expected EBX=0x20, got %08X", c.GetReg32(EBX))
	}
}

// BTS of already-set bit
func TestBTSAlreadySet32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x20)
	c.SetReg32(ECX, 5)
	code := []byte{0x0F, 0xAB, 0xCB, 0xF4} // BTS EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set before BTS)")
	}
	if c.GetReg32(EBX) != 0x20 {
		t.Errorf("expected EBX=0x20, got %08X", c.GetReg32(EBX))
	}
}

// 0F B3 BTR r/m32, r32
func TestBTRRegReg32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0xFFFFFFFF)
	c.SetReg32(ECX, 7)
	code := []byte{0x0F, 0xB3, 0xCB, 0xF4} // BTR EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set before BTR)")
	}
	if c.GetReg32(EBX) != 0xFFFFFF7F {
		t.Errorf("expected EBX=0xFFFFFF7F, got %08X", c.GetReg32(EBX))
	}
}

// 0F BB BTC r/m32, r32
func TestBTCRegReg32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000001)
	c.SetReg32(ECX, 0)
	code := []byte{0x0F, 0xBB, 0xCB, 0xF4} // BTC EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set before BTC)")
	}
	if c.GetReg32(EBX) != 0 {
		t.Errorf("expected EBX=0, got %08X", c.GetReg32(EBX))
	}
}

// BTC toggle back
func TestBTCToggleBack32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0)
	c.SetReg32(ECX, 0)
	code := []byte{0x0F, 0xBB, 0xCB, 0xF4} // BTC EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 (bit was clear before BTC)")
	}
	if c.GetReg32(EBX) != 1 {
		t.Errorf("expected EBX=1, got %08X", c.GetReg32(EBX))
	}
}

// 0F BA /4 BT r/m32, imm8
func TestBTRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000100)
	code := []byte{0x0F, 0xBA, 0xE3, 0x08, 0xF4} // BT EBX, 8; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 8 is set)")
	}
}

// 0F BA /5 BTS r/m32, imm8
func TestBTSRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0)
	code := []byte{0x0F, 0xBA, 0xEB, 0x03, 0xF4} // BTS EBX, 3; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 (bit was clear)")
	}
	if c.GetReg32(EBX) != 8 {
		t.Errorf("expected EBX=8, got %08X", c.GetReg32(EBX))
	}
}

// 0F BA /6 BTR r/m32, imm8
func TestBTRRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0xFFFFFFFF)
	code := []byte{0x0F, 0xBA, 0xF3, 0x0A, 0xF4} // BTR EBX, 10; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg32(EBX) != 0xFFFFFBFF {
		t.Errorf("expected EBX=0xFFFFFBFF, got %08X", c.GetReg32(EBX))
	}
}

// 0F BA /7 BTC r/m32, imm8
func TestBTCRegImm32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000400)
	code := []byte{0x0F, 0xBA, 0xFB, 0x0A, 0xF4} // BTC EBX, 10; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set)")
	}
	if c.GetReg32(EBX) != 0 {
		t.Errorf("expected EBX=0, got %08X", c.GetReg32(EBX))
	}
}

// Memory forms
func TestBTMemReg32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0x00000010)
	c.SetReg32(ECX, 4)
	// BT [0x2000], ECX
	// modrm: mod=00, reg=ECX(1), rm=[disp32](5) => 0x0D
	code := []byte{0x0F, 0xA3, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set)")
	}
}

func TestBTSMemReg32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0)
	c.SetReg32(ECX, 5)
	code := []byte{0x0F, 0xAB, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0x20 {
		t.Errorf("mem[0x2000] = %08X, want 0x20", v)
	}
}

func TestBTRMemReg32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0xFFFFFFFF)
	c.SetReg32(ECX, 7)
	code := []byte{0x0F, 0xB3, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0xFFFFFF7F {
		t.Errorf("mem[0x2000] = %08X, want 0xFFFFFF7F", v)
	}
}

func TestBTCMemReg32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0x00000001)
	c.SetReg32(ECX, 0)
	code := []byte{0x0F, 0xBB, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0 {
		t.Errorf("mem[0x2000] = %08X, want 0", v)
	}
}

// 16-bit forms
func TestBTRegReg16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0x0008)
	c.SetReg16(CX, 3)
	code := []byte{0x0F, 0xA3, 0xCB, 0xF4} // BT BX, CX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit set)")
	}
}

func TestBTSRegReg16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0)
	c.SetReg16(CX, 5)
	code := []byte{0x0F, 0xAB, 0xCB, 0xF4} // BTS BX, CX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 (bit was clear)")
	}
	if c.GetReg16(BX) != 0x20 {
		t.Errorf("expected BX=0x20, got %04X", c.GetReg16(BX))
	}
}

func TestBTRRegReg16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0xFFFF)
	c.SetReg16(CX, 7)
	code := []byte{0x0F, 0xB3, 0xCB, 0xF4} // BTR BX, CX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg16(BX) != 0xFF7F {
		t.Errorf("expected BX=0xFF7F, got %04X", c.GetReg16(BX))
	}
}

func TestBTCRegReg16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0x0001)
	c.SetReg16(CX, 0)
	code := []byte{0x0F, 0xBB, 0xCB, 0xF4} // BTC BX, CX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg16(BX) != 0 {
		t.Errorf("expected BX=0, got %04X", c.GetReg16(BX))
	}
}

// 16-bit immediate forms
func TestBTSRegImm16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0)
	code := []byte{0x0F, 0xBA, 0xEB, 0x03, 0xF4} // BTS BX, 3; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg16(BX) != 8 {
		t.Errorf("expected BX=8, got %04X", c.GetReg16(BX))
	}
}

func TestBTRRegImm16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0xFFFF)
	code := []byte{0x0F, 0xBA, 0xF3, 0x0A, 0xF4} // BTR BX, 10; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg16(BX) != 0xFBFF {
		t.Errorf("expected BX=0xFBFF, got %04X", c.GetReg16(BX))
	}
}

func TestBTCRegImm16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0x0400)
	code := []byte{0x0F, 0xBA, 0xFB, 0x0A, 0xF4} // BTC BX, 10; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.GetReg16(BX) != 0 {
		t.Errorf("expected BX=0, got %04X", c.GetReg16(BX))
	}
}

// Test bit indexing mask (bit index modulo operand size — register form).
// Per Intel SDM, for REGISTER destination the bit offset is taken modulo the
// operand size. (For MEMORY destination the offset extends the address; not
// covered here.)
func TestBTBitIndexWrap32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x00000001)
	c.SetReg32(ECX, 33) // bit 33 mod 32 = bit 1
	code := []byte{0x0F, 0xA3, 0xCB, 0xF4} // BT EBX, ECX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 (bit 1 is clear after wrap)")
	}
}

func TestBTBitIndexWrap16(t *testing.T) {
	c := newTestCPURealMode(t)
	c.SetReg16(BX, 0x0001)
	c.SetReg16(CX, 17) // bit 17 mod 16 = bit 1
	code := []byte{0x0F, 0xA3, 0xCB, 0xF4} // BT BX, CX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Error("expected CF=0 (bit 1 is clear after wrap)")
	}
}
