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

// TestRDMSR tests RDMSR for IA32_APIC_BASE returns the APIC mmio base with
// the enable bit clear (we have no APIC).
func TestRDMSR(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	c.SetReg32(EDX, 0xCAFEBABE)
	c.SetReg32(ECX, 0x1B)
	code := []byte{0x0F, 0x32, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xFEE00000 {
		t.Errorf("EAX = 0x%08X, want 0xFEE00000", got)
	}
	if got := c.GetReg32(EDX); got != 0 {
		t.Errorf("EDX = 0x%08X, want 0", got)
	}
}

// roundtripMSR writes EDX:EAX = (hi,lo) to MSR `idx`, then reads it back and
// returns the read value.
func roundtripMSR(t *testing.T, idx uint32, lo, hi uint32) (uint32, uint32) {
	t.Helper()
	c := newTestCPU(t)
	c.SetReg32(ECX, idx)
	c.SetReg32(EAX, lo)
	c.SetReg32(EDX, hi)
	// WRMSR; RDMSR; HLT
	code := []byte{0x0F, 0x30, 0x0F, 0x32, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	return c.GetReg32(EAX), c.GetReg32(EDX)
}

func TestMSR_SysenterCS(t *testing.T) {
	lo, hi := roundtripMSR(t, 0x174, 0x00000010, 0)
	if lo != 0x00000010 || hi != 0 {
		t.Errorf("MSR_SYSENTER_CS = %08X:%08X, want 0:00000010", hi, lo)
	}
}

func TestMSR_SysenterESP(t *testing.T) {
	lo, hi := roundtripMSR(t, 0x175, 0x00FE0000, 0)
	if lo != 0x00FE0000 || hi != 0 {
		t.Errorf("MSR_SYSENTER_ESP = %08X:%08X, want 0:00FE0000", hi, lo)
	}
}

func TestMSR_FSBase(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 0xC0000100)
	c.SetReg32(EAX, 0xC1234000)
	c.SetReg32(EDX, 0)
	code := []byte{0x0F, 0x30, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("WRMSR FS_BASE: %v", err)
	}
	if got := c.GetSegBase(FS); got != 0xC1234000 {
		t.Errorf("FS base = 0x%08X, want 0xC1234000", got)
	}
}

func TestMSR_MTRRPhysBaseRoundtrip(t *testing.T) {
	lo, hi := roundtripMSR(t, 0x200, 0xDEADBEEF, 0xCAFE)
	if lo != 0xDEADBEEF || hi != 0xCAFE {
		t.Errorf("MTRR base = %08X:%08X, want 0000CAFE:DEADBEEF", hi, lo)
	}
}

func TestMSR_MiscEnableRoundtrip(t *testing.T) {
	lo, hi := roundtripMSR(t, msrIA32_MISC_ENABLE, 0x12345678, 0x9ABCDEF0)
	if lo != 0x12345678 || hi != 0x9ABCDEF0 {
		t.Errorf("MISC_ENABLE = %08X:%08X, want 9ABCDEF0:12345678", hi, lo)
	}
}

func TestMSR_SysenterEIP(t *testing.T) {
	lo, hi := roundtripMSR(t, msrIA32_SYSENTER_EIP, 0xC1234567, 0)
	if lo != 0xC1234567 || hi != 0 {
		t.Errorf("SYSENTER_EIP = %08X:%08X, want 0:C1234567", hi, lo)
	}
}

func TestMSR_GSBase(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, msrIA32_GS_BASE)
	c.SetReg32(EAX, 0xC2222000)
	c.SetReg32(EDX, 0)
	code := []byte{0x0F, 0x30, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("WRMSR GS_BASE: %v", err)
	}
	if got := c.GetSegBase(GS); got != 0xC2222000 {
		t.Errorf("GS base = 0x%08X, want 0xC2222000", got)
	}
}

func TestMSR_KGSBaseRoundtrip(t *testing.T) {
	lo, hi := roundtripMSR(t, msrIA32_KGS_BASE, 0xDEADBEEF, 0)
	if lo != 0xDEADBEEF || hi != 0 {
		t.Errorf("KGS_BASE = %08X:%08X, want 0:DEADBEEF", hi, lo)
	}
}

// TestMSR_TSCRead verifies RDMSR IA32_TSC returns the cycles counter.
func TestMSR_TSCRead(t *testing.T) {
	c := newTestCPU(t)
	c.cycles = 0x123456789AB
	c.SetReg32(ECX, msrIA32_TSC)
	code := []byte{0x0F, 0x32, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("RDMSR TSC: %v", err)
	}
	// runCode itself executes some Steps; cycles is updated by Run() not
	// by Step(), so cycles should be close to our preset value modulo
	// per-instruction bumping.
	got := uint64(c.GetReg32(EAX)) | (uint64(c.GetReg32(EDX)) << 32)
	if got < 0x123456789AB {
		t.Errorf("TSC = 0x%X, want >= 0x123456789AB", got)
	}
}

// TestMSR_EFER_LMERejected: writing EFER with LME (bit 8) set must raise #GP
// because we don't support long mode.
func TestMSR_EFER_LMERejected(t *testing.T) {
	c := setupGPCatchCPU(t)
	c.SetReg32(ECX, msrIA32_EFER)
	c.SetReg32(EAX, 1<<8) // LME
	c.SetReg32(EDX, 0)
	code := []byte{0x0F, 0x30, 0xF4} // WRMSR; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xDEAD {
		t.Errorf("EAX = 0x%X, want 0xDEAD (i.e. #GP handler ran)", got)
	}
}

// TestMSR_EFER_NXEAllowed: setting NXE (bit 11) should succeed.
func TestMSR_EFER_NXEAllowed(t *testing.T) {
	lo, hi := roundtripMSR(t, msrIA32_EFER, 1<<11, 0)
	if lo != (1<<11) || hi != 0 {
		t.Errorf("EFER = %08X:%08X, want 0:00000800 (NXE only)", hi, lo)
	}
}

// setupGPCatchCPU returns a CPU configured so that a #GP fault diverts to a
// handler at linear address 0x4000 which sets EAX = 0xDEAD and halts. Useful
// to assert that some instruction raises #GP without leaking the panic.
func setupGPCatchCPU(t *testing.T) *CPU {
	t.Helper()
	c := newTestCPU(t)
	idtBase := uint32(0x4000)
	for i := uint32(0); i < 8*16; i++ {
		c.writeMem8(idtBase+i, 0)
	}
	gate := idtBase + 0x0D*8
	c.writeMem8(gate+0, 0x00)
	c.writeMem8(gate+1, 0x40)
	c.writeMem8(gate+2, 0x08)
	c.writeMem8(gate+3, 0x00)
	c.writeMem8(gate+4, 0x00)
	c.writeMem8(gate+5, 0x8E)
	c.writeMem8(gate+6, 0x00)
	c.writeMem8(gate+7, 0x00)
	c.SetSegBase(IDTR, idtBase)
	c.SetSegLimit(IDTR, 0x0D*8+7)

	gdtBase := uint32(0x5000)
	for i := uint32(0); i < 16; i++ {
		c.writeMem8(gdtBase+i, 0)
	}
	c.writeMem8(gdtBase+8, 0xFF)
	c.writeMem8(gdtBase+9, 0xFF)
	c.writeMem8(gdtBase+10, 0x00)
	c.writeMem8(gdtBase+11, 0x00)
	c.writeMem8(gdtBase+12, 0x00)
	c.writeMem8(gdtBase+13, 0x9A)
	c.writeMem8(gdtBase+14, 0xCF)
	c.writeMem8(gdtBase+15, 0x00)
	c.SetSegBase(GDTR, gdtBase)
	c.SetSegLimit(GDTR, 15)

	c.writeMem8(0x4000, 0xB8)
	c.writeMem8(0x4001, 0xAD)
	c.writeMem8(0x4002, 0xDE)
	c.writeMem8(0x4003, 0x00)
	c.writeMem8(0x4004, 0x00)
	c.writeMem8(0x4005, 0xF4)

	c.SetReg32(ESP, 0x6000)
	return c
}

// TestMSR_UnknownRaisesGP verifies that reading or writing an unrecognized
// MSR raises #GP(0). The kernel uses this with fixup tables for probing.
func TestMSR_UnknownRaisesGP(t *testing.T) {
	c := setupGPCatchCPU(t)
	c.SetReg32(ECX, 0xDEADBEEF)
	code := []byte{0x0F, 0x32, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xDEAD {
		t.Errorf("EAX = 0x%X, want handler to have set 0xDEAD (i.e. #GP fired)", got)
	}
}
