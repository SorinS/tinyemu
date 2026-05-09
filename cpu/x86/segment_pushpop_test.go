package x86

import (
	"testing"
)

// TestPushFS tests PUSH FS (0F A0)
func TestPushFS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.seg[FS] = 0x0030
	code := []byte{0x0F, 0xA0, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0030 {
		t.Errorf("stack top = 0x%08X, want 0x0030", got)
	}
}

// TestPopFS tests POP FS (0F A1)
func TestPopFS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x0FFC)
	c.writeMem32(0x0FFC, 0x0040)
	code := []byte{0x0F, 0xA1, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x1000 {
		t.Errorf("ESP = 0x%08X, want 0x1000", got)
	}
	if got := c.seg[FS]; got != 0x0040 {
		t.Errorf("FS = 0x%04X, want 0x0040", got)
	}
}

// TestPushGS tests PUSH GS (0F A8)
func TestPushGS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.seg[GS] = 0x0050
	code := []byte{0x0F, 0xA8, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0050 {
		t.Errorf("stack top = 0x%08X, want 0x0050", got)
	}
}

// TestPopGS tests POP GS (0F A9)
func TestPopGS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x0FFC)
	c.writeMem32(0x0FFC, 0x0060)
	code := []byte{0x0F, 0xA9, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x1000 {
		t.Errorf("ESP = 0x%08X, want 0x1000", got)
	}
	if got := c.seg[GS]; got != 0x0060 {
		t.Errorf("GS = 0x%04X, want 0x0060", got)
	}
}

// TestPushES tests PUSH ES (06)
func TestPushES(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.seg[ES] = 0x0010
	code := []byte{0x06, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0010 {
		t.Errorf("stack top = 0x%08X, want 0x0010", got)
	}
}

// TestPopES tests POP ES (07)
func TestPopES(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x0FFC)
	c.writeMem32(0x0FFC, 0x0020)
	code := []byte{0x07, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x1000 {
		t.Errorf("ESP = 0x%08X, want 0x1000", got)
	}
	if got := c.seg[ES]; got != 0x0020 {
		t.Errorf("ES = 0x%04X, want 0x0020", got)
	}
}

// TestPushCS tests PUSH CS (0E)
func TestPushCS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.seg[CS] = 0x0008
	code := []byte{0x0E, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0008 {
		t.Errorf("stack top = 0x%08X, want 0x0008", got)
	}
}

// TestPushSS tests PUSH SS (16)
func TestPushSS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.seg[SS] = 0x0018
	code := []byte{0x16, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0018 {
		t.Errorf("stack top = 0x%08X, want 0x0018", got)
	}
}

// TestPopSS tests POP SS (17)
func TestPopSS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x0FFC)
	c.writeMem32(0x0FFC, 0x0018)
	code := []byte{0x17, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x1000 {
		t.Errorf("ESP = 0x%08X, want 0x1000", got)
	}
	if got := c.seg[SS]; got != 0x0018 {
		t.Errorf("SS = 0x%04X, want 0x0018", got)
	}
}

// TestPushDS tests PUSH DS (1E)
func TestPushDS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.seg[DS] = 0x0028
	code := []byte{0x1E, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0028 {
		t.Errorf("stack top = 0x%08X, want 0x0028", got)
	}
}

// TestPopDS tests POP DS (1F)
func TestPopDS(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x0FFC)
	c.writeMem32(0x0FFC, 0x0030)
	code := []byte{0x1F, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x1000 {
		t.Errorf("ESP = 0x%08X, want 0x1000", got)
	}
	if got := c.seg[DS]; got != 0x0030 {
		t.Errorf("DS = 0x%04X, want 0x0030", got)
	}
}
