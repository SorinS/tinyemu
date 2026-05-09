package x86

import (
	"testing"
)

// TestWRMSR tests WRMSR as a no-op stub
func TestWRMSR(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	c.SetReg32(EDX, 0x9ABCDEF0)
	c.SetReg32(ECX, 0x1B) // IA32_APIC_BASE
	code := []byte{0x0F, 0x30, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// Registers should be unchanged
	if got := c.GetReg32(EAX); got != 0x12345678 {
		t.Errorf("EAX = 0x%08X, want 0x12345678", got)
	}
	if got := c.GetReg32(EDX); got != 0x9ABCDEF0 {
		t.Errorf("EDX = 0x%08X, want 0x9ABCDEF0", got)
	}
}

// TestRDMSR tests RDMSR returns zero
func TestRDMSR(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	c.SetReg32(EDX, 0xCAFEBABE)
	c.SetReg32(ECX, 0x1B)
	code := []byte{0x0F, 0x32, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = 0x%08X, want 0", got)
	}
	if got := c.GetReg32(EDX); got != 0 {
		t.Errorf("EDX = 0x%08X, want 0", got)
	}
}
