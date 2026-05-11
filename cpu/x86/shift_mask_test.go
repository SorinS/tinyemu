package x86

import "testing"

// Intel SDM (Vol 2A): the shift/rotate count operand is masked to 5 bits
// BEFORE the operation. If the masked count is 0, NO flags are modified and
// the destination is unchanged. Prior to this fix our impl skipped the
// masking entirely, so `SHL EAX, 32` produced 0 (clobbering bits + flags)
// when it should have been a no-op.

func TestShl32CountModulo32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	c.SetReg32(ECX, 32)
	// Pre-set CF so we can verify it isn't modified.
	c.setCF(true)
	preCF := c.getCF()
	// SHL EAX, CL — `D3 /4` with modrm 0xE0 = mod=11 reg=4 (SHL) rm=0 (EAX)
	code := []byte{0xD3, 0xE0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF (SHL by count masked to 0 should be no-op)", v)
	}
	if c.getCF() != preCF {
		t.Errorf("CF was modified by SHL with masked-count=0")
	}
}

func TestShr32CountModulo32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	c.SetReg32(ECX, 32) // masked = 0
	c.setCF(true)
	preCF := c.getCF()
	code := []byte{0xD3, 0xE8, 0xF4} // SHR EAX, CL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", v)
	}
	if c.getCF() != preCF {
		t.Errorf("CF was modified by SHR with masked-count=0")
	}
}

func TestSar32CountModulo32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x80000000) // negative
	c.SetReg32(ECX, 33)         // masked = 1
	code := []byte{0xD3, 0xF8, 0xF4} // SAR EAX, CL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xC0000000 {
		t.Errorf("EAX = 0x%08X, want 0xC0000000 (SAR by 1 of negative)", v)
	}
}

func TestRol32CountModulo32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	c.SetReg32(ECX, 32) // masked = 0
	code := []byte{0xD3, 0xC0, 0xF4} // ROL EAX, CL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x12345678 {
		t.Errorf("EAX = 0x%08X, want 0x12345678 (ROL by 32 is no-op)", v)
	}
}

func TestRor32CountModulo32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	c.SetReg32(ECX, 33) // masked = 1
	code := []byte{0xD3, 0xC8, 0xF4} // ROR EAX, CL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// 0x12345678 ROR 1 = 0x091A2B3C
	if v := c.GetReg32(EAX); v != 0x091A2B3C {
		t.Errorf("EAX = 0x%08X, want 0x091A2B3C", v)
	}
}

// SHL/SHR by 1: OF is well-defined (XOR of MSB before and after for SHL;
// MSB of original for SHR).
func TestShl32By1OF(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x40000000) // bit 30 set; after SHL by 1, MSB flips
	// D1 /4: SHL r/m32, 1
	code := []byte{0xD1, 0xE0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x80000000 {
		t.Errorf("EAX = 0x%08X, want 0x80000000", v)
	}
	if !c.getOF() {
		t.Error("expected OF=1 (MSB flipped to 1)")
	}
}

// CL=1 path: SHL EAX, 1 should produce the same flags as `D1 /4`.
func TestShl32By1WithCL(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x80000000)
	c.SetReg32(ECX, 1)
	code := []byte{0xD3, 0xE0, 0xF4} // SHL EAX, CL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0 {
		t.Errorf("EAX = 0x%08X, want 0", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 31 shifted out)")
	}
	if !c.getZF() {
		t.Error("expected ZF=1 (result is zero)")
	}
}

// SHR by 1: CF = bit 0 of original; OF = MSB of original.
func TestShr32By1Flags(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x80000001)
	code := []byte{0xD1, 0xE8, 0xF4} // SHR EAX, 1
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x40000000 {
		t.Errorf("EAX = 0x%08X, want 0x40000000", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 0 of original)")
	}
	if !c.getOF() {
		t.Error("expected OF=1 (MSB of original was 1)")
	}
}

// SAR by 1: CF = bit 0 of original; OF = 0.
func TestSar32By1Flags(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xFFFFFFF7)
	c.setOF(true) // poison; SAR should clear OF for count=1
	code := []byte{0xD1, 0xF8, 0xF4} // SAR EAX, 1
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xFFFFFFFB {
		t.Errorf("EAX = 0x%08X, want 0xFFFFFFFB", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 0 was 1)")
	}
	if c.getOF() {
		t.Error("expected OF=0 for SAR by 1")
	}
}

// Edge: SHL r8 by a count that exceeds the 8-bit width but is within the
// 5-bit mask. Pre-fix our impl crashed on shift-by-negative; now it must
// produce a stable 0 result with the right CF.
func TestShl8By10(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x000000FF) // AL = 0xFF
	c.SetReg32(ECX, 10)
	code := []byte{0xD2, 0xE0, 0xF4} // SHL AL, CL
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if al := c.GetReg8(AL); al != 0 {
		t.Errorf("AL = 0x%02X, want 0", al)
	}
	if !c.getZF() {
		t.Error("expected ZF=1 (all bits shifted out)")
	}
}
