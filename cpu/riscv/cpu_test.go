package riscv

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func TestNewCPU(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	cpu := NewCPU(m, XLEN64)
	if cpu == nil {
		t.Fatal("NewCPU returned nil")
	}

	if cpu.MaxXLEN != XLEN64 {
		t.Errorf("expected MaxXLEN 64, got %d", cpu.MaxXLEN)
	}
	if cpu.CurXLEN != XLEN64 {
		t.Errorf("expected CurXLEN 64, got %d", cpu.CurXLEN)
	}
	if cpu.Priv != PrivMachine {
		t.Errorf("expected Priv Machine, got %d", cpu.Priv)
	}
	if cpu.PendingException != -1 {
		t.Errorf("expected no pending exception, got %d", cpu.PendingException)
	}
	// Reference: riscv_cpu.c:1300 - s->pc = 0x1000
	if cpu.PC != 0x1000 {
		t.Errorf("expected PC 0x1000, got 0x%x", cpu.PC)
	}
}

func TestNewCPU32(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	cpu := NewCPU(m, XLEN32)
	if cpu.MXL != MxlRV32 {
		t.Errorf("expected MXL RV32 (%d), got %d", MxlRV32, cpu.MXL)
	}
	// Check MISA has correct MXL field
	misaMXL := (cpu.Misa >> 30) & 3
	if misaMXL != MxlRV32 {
		t.Errorf("expected MISA MXL %d, got %d", MxlRV32, misaMXL)
	}
}

func TestCPUReset(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	cpu := NewCPU(m, XLEN64)

	// Modify state
	cpu.PC = 0x12345678
	cpu.Reg[1] = 0xDEADBEEF
	cpu.Priv = PrivSupervisor
	cpu.InsnCounter = 1000
	cpu.PowerDownFlag = true
	cpu.Mstatus = 0xFFFFFFFF

	// Reset
	cpu.Reset()

	if cpu.PC != 0 {
		t.Errorf("expected PC 0, got 0x%x", cpu.PC)
	}
	if cpu.Reg[1] != 0 {
		t.Errorf("expected Reg[1] 0, got 0x%x", cpu.Reg[1])
	}
	if cpu.Priv != PrivMachine {
		t.Errorf("expected Priv Machine, got %d", cpu.Priv)
	}
	if cpu.InsnCounter != 0 {
		t.Errorf("expected InsnCounter 0, got %d", cpu.InsnCounter)
	}
	if cpu.PowerDownFlag {
		t.Error("expected PowerDownFlag false")
	}
}

func TestRegisterX0(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// x0 should always read as 0
	if cpu.GetReg(0) != 0 {
		t.Errorf("x0 should be 0, got 0x%x", cpu.GetReg(0))
	}

	// Writes to x0 should be ignored
	cpu.SetReg(0, 0xDEADBEEF)
	if cpu.GetReg(0) != 0 {
		t.Errorf("x0 should still be 0 after write, got 0x%x", cpu.GetReg(0))
	}
}

func TestRegisters(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Test all registers
	for i := 1; i < 32; i++ {
		val := uint64(i * 0x11111111)
		cpu.SetReg(i, val)
		got := cpu.GetReg(i)
		if got != val {
			t.Errorf("Reg[%d]: expected 0x%x, got 0x%x", i, val, got)
		}
	}
}

func TestSignExtension32(t *testing.T) {
	cpu := NewCPU(nil, XLEN32)

	// Positive value
	cpu.SetReg(1, 0x7FFFFFFF)
	if cpu.GetReg(1) != 0x7FFFFFFF {
		t.Errorf("expected 0x7FFFFFFF, got 0x%x", cpu.GetReg(1))
	}

	// Negative value (should be sign-extended)
	cpu.SetReg(1, 0x80000000)
	expected := uint64(0xFFFFFFFF80000000)
	if cpu.GetReg(1) != expected {
		t.Errorf("expected 0x%x, got 0x%x", expected, cpu.GetReg(1))
	}
}

func TestMIP(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Initially 0
	if cpu.GetMIP() != 0 {
		t.Errorf("expected MIP 0, got 0x%x", cpu.GetMIP())
	}

	// Set timer interrupt
	cpu.SetMIP(MipMTIP)
	if cpu.GetMIP() != MipMTIP {
		t.Errorf("expected MIP 0x%x, got 0x%x", MipMTIP, cpu.GetMIP())
	}

	// Set another interrupt
	cpu.SetMIP(MipMSIP)
	expected := uint32(MipMTIP | MipMSIP)
	if cpu.GetMIP() != expected {
		t.Errorf("expected MIP 0x%x, got 0x%x", expected, cpu.GetMIP())
	}

	// Reset timer interrupt
	cpu.ResetMIP(MipMTIP)
	if cpu.GetMIP() != MipMSIP {
		t.Errorf("expected MIP 0x%x, got 0x%x", MipMSIP, cpu.GetMIP())
	}
}

func TestPrivilege(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	cpu := NewCPU(m, XLEN64)

	if cpu.GetPriv() != PrivMachine {
		t.Errorf("expected Machine mode, got %d", cpu.GetPriv())
	}

	cpu.SetPriv(PrivSupervisor)
	if cpu.GetPriv() != PrivSupervisor {
		t.Errorf("expected Supervisor mode, got %d", cpu.GetPriv())
	}

	cpu.SetPriv(PrivUser)
	if cpu.GetPriv() != PrivUser {
		t.Errorf("expected User mode, got %d", cpu.GetPriv())
	}
}

func TestPowerDown(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	if cpu.IsPowerDown() {
		t.Error("expected PowerDown false initially")
	}

	cpu.SetPowerDown(true)
	if !cpu.IsPowerDown() {
		t.Error("expected PowerDown true after SetPowerDown(true)")
	}

	cpu.SetPowerDown(false)
	if cpu.IsPowerDown() {
		t.Error("expected PowerDown false after SetPowerDown(false)")
	}
}

func TestFPRegNaNBoxing(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Set F32 value with NaN-boxing
	cpu.SetFPRegF32(1, 0x3F800000) // 1.0 in F32
	val := cpu.FPReg[1]
	if val != 0xFFFFFFFF3F800000 {
		t.Errorf("expected NaN-boxed value 0xFFFFFFFF3F800000, got 0x%x", val)
	}

	// Read back as F32
	f32val := cpu.GetFPRegF32(1)
	if f32val != 0x3F800000 {
		t.Errorf("expected F32 0x3F800000, got 0x%x", f32val)
	}

	// Set F64 value directly (not properly NaN-boxed for F32)
	cpu.FPReg[2] = 0x1234567890ABCDEF
	f32val = cpu.GetFPRegF32(2)
	// Should return canonical NaN since not properly boxed
	if f32val != 0x7FC00000 {
		t.Errorf("expected canonical NaN 0x7FC00000 for unboxed value, got 0x%x", f32val)
	}
}

func TestFPRegDirty(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	cpu.FS = FSClean
	cpu.SetFPReg(1, 0x12345678)
	if cpu.FS != FSDirty {
		t.Errorf("expected FS Dirty after SetFPReg, got %d", cpu.FS)
	}

	cpu.FS = FSClean
	cpu.SetFPRegF32(2, 0x12345678)
	if cpu.FS != FSDirty {
		t.Errorf("expected FS Dirty after SetFPRegF32, got %d", cpu.FS)
	}
}

func TestTLBFlush(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Set some TLB entries
	cpu.TLBRead[0].VAddr = 0x1000
	cpu.TLBWrite[0].VAddr = 0x2000
	cpu.TLBCode[0].VAddr = 0x3000

	// Flush all
	cpu.FlushTLB()

	// All entries should be invalid (VAddr = ^0)
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Errorf("TLBRead not flushed, VAddr = 0x%x", cpu.TLBRead[0].VAddr)
	}
	if cpu.TLBWrite[0].VAddr != ^uint64(0) {
		t.Errorf("TLBWrite not flushed, VAddr = 0x%x", cpu.TLBWrite[0].VAddr)
	}
	if cpu.TLBCode[0].VAddr != ^uint64(0) {
		t.Errorf("TLBCode not flushed, VAddr = 0x%x", cpu.TLBCode[0].VAddr)
	}
}

func TestTLBFlushEntry(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Set TLB entry at index 1 (vaddr 0x1000 with PageShift=12 gives index 1)
	vaddr := uint64(0x1000)
	idx := (vaddr >> PageShift) & (TLBSize - 1)

	cpu.TLBRead[idx].VAddr = vaddr
	cpu.TLBWrite[idx].VAddr = vaddr
	cpu.TLBCode[idx].VAddr = vaddr

	// Flush just that entry
	cpu.FlushTLBEntry(vaddr)

	if cpu.TLBRead[idx].VAddr != ^uint64(0) {
		t.Errorf("TLBRead entry not flushed")
	}
	if cpu.TLBWrite[idx].VAddr != ^uint64(0) {
		t.Errorf("TLBWrite entry not flushed")
	}
	if cpu.TLBCode[idx].VAddr != ^uint64(0) {
		t.Errorf("TLBCode entry not flushed")
	}
}

func TestPendingException(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	if cpu.HasPendingException() {
		t.Error("should not have pending exception initially")
	}

	cpu.SetPendingException(CauseIllegalInsn, 0x12345678)

	if !cpu.HasPendingException() {
		t.Error("should have pending exception after SetPendingException")
	}
	if cpu.PendingException != CauseIllegalInsn {
		t.Errorf("expected cause %d, got %d", CauseIllegalInsn, cpu.PendingException)
	}
	if cpu.PendingTval != 0x12345678 {
		t.Errorf("expected tval 0x12345678, got 0x%x", cpu.PendingTval)
	}

	cpu.ClearPendingException()
	if cpu.HasPendingException() {
		t.Error("should not have pending exception after clear")
	}
}

func TestLoadReservation(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// No reservation initially
	if cpu.CheckLoadReservation(0x1000) {
		t.Error("should not have reservation initially")
	}

	// Set reservation
	cpu.SetLoadReservation(0x1000)
	if !cpu.CheckLoadReservation(0x1000) {
		t.Error("should have reservation at 0x1000")
	}
	if cpu.CheckLoadReservation(0x2000) {
		t.Error("should not have reservation at 0x2000")
	}

	// Clear reservation
	cpu.ClearLoadReservation()
	if cpu.CheckLoadReservation(0x1000) {
		t.Error("should not have reservation after clear")
	}
}

func TestExtensions(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Default extensions
	if !cpu.HasExtension(MisaI) {
		t.Error("should have I extension")
	}
	if !cpu.HasExtension(MisaM) {
		t.Error("should have M extension")
	}
	if !cpu.HasExtension(MisaA) {
		t.Error("should have A extension")
	}
	if !cpu.HasExtension(MisaF) {
		t.Error("should have F extension")
	}
	if !cpu.HasExtension(MisaD) {
		t.Error("should have D extension")
	}
	if !cpu.HasExtension(MisaC) {
		t.Error("should have C extension")
	}
	if !cpu.HasExtension(MisaS) {
		t.Error("should have S extension")
	}
	if !cpu.HasExtension(MisaU) {
		t.Error("should have U extension")
	}

	// Helper functions
	if !cpu.SupportsFloat() {
		t.Error("should support float")
	}
	if !cpu.SupportsDouble() {
		t.Error("should support double")
	}
	if !cpu.SupportsCompressed() {
		t.Error("should support compressed")
	}
	if !cpu.SupportsAtomic() {
		t.Error("should support atomic")
	}
	if !cpu.SupportsMulDiv() {
		t.Error("should support mul/div")
	}

	// Disable F extension
	cpu.Misa &^= MisaF
	if cpu.SupportsFloat() {
		t.Error("should not support float after disabling")
	}
}

func TestUpdateMstatus(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Update FS field
	cpu.UpdateMstatus(MstatusFS, uint64(FSDirty)<<MstatusFSShift)
	if cpu.FS != FSDirty {
		t.Errorf("expected FS Dirty, got %d", cpu.FS)
	}

	// Check actual mstatus value
	fsVal := (cpu.Mstatus >> MstatusFSShift) & 3
	if fsVal != FSDirty {
		t.Errorf("expected mstatus FS %d, got %d", FSDirty, fsVal)
	}
}

func TestGetEffectivePriv(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	cpu.Priv = PrivSupervisor

	// Without MPRV, should return current priv
	priv := cpu.GetEffectivePriv(true)
	if priv != PrivSupervisor {
		t.Errorf("expected Supervisor, got %d", priv)
	}

	// Set MPRV and MPP to User
	cpu.Mstatus = MstatusMPRV | (uint64(PrivUser) << MstatusMPPShift)

	// With MPRV, load should use MPP
	priv = cpu.GetEffectivePriv(true)
	if priv != PrivUser {
		t.Errorf("expected User (from MPP) for load, got %d", priv)
	}

	// Store should still use current priv (we don't implement MPRV for stores in this helper)
	priv = cpu.GetEffectivePriv(false)
	if priv != PrivSupervisor {
		t.Errorf("expected Supervisor for store, got %d", priv)
	}
}

func TestPrivConstants(t *testing.T) {
	// Verify privilege constants match RISC-V spec
	if PrivUser != 0 {
		t.Error("PrivUser should be 0")
	}
	if PrivSupervisor != 1 {
		t.Error("PrivSupervisor should be 1")
	}
	if PrivMachine != 3 {
		t.Error("PrivMachine should be 3")
	}
}

func TestCauseConstants(t *testing.T) {
	// Verify cause constants match RISC-V spec
	if CauseMisalignedFetch != 0 {
		t.Error("CauseMisalignedFetch should be 0")
	}
	if CauseIllegalInsn != 2 {
		t.Error("CauseIllegalInsn should be 2")
	}
	if CauseUserEcall != 8 {
		t.Error("CauseUserEcall should be 8")
	}
	if CauseFetchPageFault != 0xc {
		t.Error("CauseFetchPageFault should be 0xc")
	}
}

func TestMISAExtensions(t *testing.T) {
	// Verify MISA extension bit positions
	if MisaA != (1 << 0) {
		t.Error("MisaA should be bit 0")
	}
	if MisaI != (1 << 8) {
		t.Error("MisaI should be bit 8")
	}
	if MisaM != (1 << 12) {
		t.Error("MisaM should be bit 12")
	}
	if MisaS != (1 << 18) {
		t.Error("MisaS should be bit 18")
	}
	if MisaU != (1 << 20) {
		t.Error("MisaU should be bit 20")
	}
}

func TestTLBConstants(t *testing.T) {
	if TLBSize != 256 {
		t.Errorf("TLBSize should be 256, got %d", TLBSize)
	}
	if PageShift != 12 {
		t.Errorf("PageShift should be 12, got %d", PageShift)
	}
	if PageSize != 4096 {
		t.Errorf("PageSize should be 4096, got %d", PageSize)
	}
}

// TestGetCycles tests the GetCycles function
func TestGetCycles(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	if cpu.GetCycles() != 0 {
		t.Errorf("expected 0 cycles initially, got %d", cpu.GetCycles())
	}

	cpu.InsnCounter = 12345
	if cpu.GetCycles() != 12345 {
		t.Errorf("expected 12345 cycles, got %d", cpu.GetCycles())
	}
}

// TestGetMISA tests the GetMISA function
func TestGetMISA(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	misa := cpu.GetMISA()

	// Should have I extension
	if misa&MisaI == 0 {
		t.Error("MISA should have I extension")
	}

	// Should have M extension
	if misa&MisaM == 0 {
		t.Error("MISA should have M extension")
	}

	// Check MXL field (bits 63:62 for RV64)
	mxl := (misa >> 62) & 3
	if mxl != MxlRV64 {
		t.Errorf("expected MXL %d, got %d", MxlRV64, mxl)
	}
}

// TestGetFPReg tests the GetFPReg function
func TestGetFPReg(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Set a value directly
	cpu.FPReg[5] = 0x123456789ABCDEF0

	// Get it back
	val := cpu.GetFPReg(5)
	if val != 0x123456789ABCDEF0 {
		t.Errorf("expected 0x123456789ABCDEF0, got 0x%x", val)
	}
}

// TestFlushTLBWriteRange tests the FlushTLBWriteRange function
func TestFlushTLBWriteRange(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Set some TLB write entries
	cpu.TLBWrite[0].VAddr = 0x1000
	cpu.TLBWrite[1].VAddr = 0x2000
	cpu.TLBWrite[2].VAddr = 0x3000

	// Also set read/code entries that should NOT be affected
	cpu.TLBRead[0].VAddr = 0x1000
	cpu.TLBCode[0].VAddr = 0x1000

	// Flush write range
	cpu.FlushTLBWriteRange(nil, 0x10000)

	// Write entries should be flushed
	if cpu.TLBWrite[0].VAddr != ^uint64(0) {
		t.Errorf("TLBWrite[0] should be flushed, got 0x%x", cpu.TLBWrite[0].VAddr)
	}
	if cpu.TLBWrite[1].VAddr != ^uint64(0) {
		t.Errorf("TLBWrite[1] should be flushed, got 0x%x", cpu.TLBWrite[1].VAddr)
	}

	// Read/code entries should NOT be flushed
	if cpu.TLBRead[0].VAddr == ^uint64(0) {
		t.Error("TLBRead[0] should NOT be flushed")
	}
	if cpu.TLBCode[0].VAddr == ^uint64(0) {
		t.Error("TLBCode[0] should NOT be flushed")
	}
}

// TestPrivChangeTLBFlush tests that privilege change flushes TLB
func TestPrivChangeTLBFlush(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	cpu := NewCPU(m, XLEN64)

	// Set some TLB entries
	cpu.TLBRead[0].VAddr = 0x1000
	cpu.TLBWrite[0].VAddr = 0x2000
	cpu.TLBCode[0].VAddr = 0x3000

	// Change privilege
	cpu.SetPriv(PrivSupervisor)

	// TLB should be flushed
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Error("TLB should be flushed on privilege change")
	}
}

// TestPrivChangeNoFlushSamePriv tests that setting same priv doesn't flush TLB
func TestPrivChangeNoFlushSamePriv(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	cpu := NewCPU(m, XLEN64)

	// Change to supervisor first
	cpu.SetPriv(PrivSupervisor)

	// Set some TLB entries
	cpu.TLBRead[0].VAddr = 0x1000

	// Set same privilege again
	cpu.SetPriv(PrivSupervisor)

	// TLB should NOT be flushed
	if cpu.TLBRead[0].VAddr == ^uint64(0) {
		t.Error("TLB should NOT be flushed when setting same privilege")
	}
}

// TestSetMIPWakesFromWFI tests that SetMIP wakes the CPU from WFI when an
// interrupt is pending and enabled.
// Reference: riscv_cpu.c lines 1266-1272
func TestSetMIPWakesFromWFI(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Setup: CPU in WFI with timer interrupt enabled
	cpu.Mie = MipMTIP // Enable timer interrupt
	cpu.PowerDownFlag = true

	// Set timer interrupt pending
	cpu.SetMIP(MipMTIP)

	// Should have woken from WFI
	if cpu.PowerDownFlag {
		t.Error("SetMIP should wake CPU from WFI when interrupt is pending and enabled")
	}

	// Verify MIP was still set
	if cpu.Mip&MipMTIP == 0 {
		t.Error("MIP should have MTIP set")
	}
}

// TestSetMIPNoWakeIfNotEnabled tests that SetMIP does NOT wake the CPU from WFI
// if the interrupt is not enabled.
// Reference: riscv_cpu.c lines 1266-1272
func TestSetMIPNoWakeIfNotEnabled(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Setup: CPU in WFI with NO interrupts enabled
	cpu.Mie = 0 // No interrupts enabled
	cpu.PowerDownFlag = true

	// Set timer interrupt pending
	cpu.SetMIP(MipMTIP)

	// Should NOT have woken from WFI (interrupt not enabled)
	if !cpu.PowerDownFlag {
		t.Error("SetMIP should NOT wake CPU from WFI when interrupt is not enabled")
	}
}

// TestSetMIPNoWakeIfNotInWFI tests that SetMIP doesn't change PowerDownFlag
// if not already in WFI.
func TestSetMIPNoWakeIfNotInWFI(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)

	// Setup: CPU NOT in WFI
	cpu.Mie = MipMTIP
	cpu.PowerDownFlag = false

	// Set timer interrupt pending
	cpu.SetMIP(MipMTIP)

	// PowerDownFlag should still be false (unchanged)
	if cpu.PowerDownFlag {
		t.Error("SetMIP should not change PowerDownFlag when not in WFI")
	}
}

// TestSetPrivUpdatesCurXLEN tests that SetPriv updates CurXLEN based on SXL/UXL
// Reference: riscv_cpu.c:1021-1040 (set_priv updates cur_xlen based on privilege)
func TestSetPrivUpdatesCurXLEN(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)
	cpu.MaxXLEN = XLEN64
	cpu.MXL = 2 // RV64
	cpu.CurXLEN = XLEN64

	// Set SXL=2 (RV64) and UXL=2 (RV64) in mstatus
	cpu.Mstatus = (uint64(2) << MstatusSXLShift) | (uint64(2) << MstatusUXLShift)

	// Start in M-mode
	cpu.Priv = PrivMachine
	cpu.CurXLEN = XLEN64

	// Change to S-mode - should use SXL
	cpu.SetPriv(PrivSupervisor)
	if cpu.CurXLEN != XLEN64 {
		t.Errorf("After SetPriv(S): CurXLEN = %d, want %d", cpu.CurXLEN, XLEN64)
	}
	if cpu.Priv != PrivSupervisor {
		t.Errorf("After SetPriv(S): Priv = %d, want %d", cpu.Priv, PrivSupervisor)
	}

	// Change to U-mode - should use UXL
	cpu.SetPriv(PrivUser)
	if cpu.CurXLEN != XLEN64 {
		t.Errorf("After SetPriv(U): CurXLEN = %d, want %d", cpu.CurXLEN, XLEN64)
	}
	if cpu.Priv != PrivUser {
		t.Errorf("After SetPriv(U): Priv = %d, want %d", cpu.Priv, PrivUser)
	}

	// Change back to M-mode - should use MXL
	cpu.SetPriv(PrivMachine)
	if cpu.CurXLEN != XLEN64 {
		t.Errorf("After SetPriv(M): CurXLEN = %d, want %d", cpu.CurXLEN, XLEN64)
	}
	if cpu.Priv != PrivMachine {
		t.Errorf("After SetPriv(M): Priv = %d, want %d", cpu.Priv, PrivMachine)
	}
}

// TestSetPrivUpdatesCurXLENRV32 tests SetPriv with RV32 mode configured in mstatus
// Reference: riscv_cpu.c:1025-1036 (RV32/RV64 mode based on SXL/UXL)
func TestSetPrivUpdatesCurXLENRV32(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)
	cpu.MaxXLEN = XLEN64
	cpu.MXL = 2 // M-mode is RV64
	cpu.CurXLEN = XLEN64
	cpu.Priv = PrivMachine

	// Set SXL=1 (RV32) and UXL=1 (RV32) in mstatus
	cpu.Mstatus = (uint64(1) << MstatusSXLShift) | (uint64(1) << MstatusUXLShift)

	// Change to S-mode - should switch to RV32
	cpu.SetPriv(PrivSupervisor)
	if cpu.CurXLEN != XLEN32 {
		t.Errorf("After SetPriv(S) with SXL=1: CurXLEN = %d, want %d", cpu.CurXLEN, XLEN32)
	}

	// Change to U-mode - should stay RV32
	cpu.SetPriv(PrivUser)
	if cpu.CurXLEN != XLEN32 {
		t.Errorf("After SetPriv(U) with UXL=1: CurXLEN = %d, want %d", cpu.CurXLEN, XLEN32)
	}

	// Change to M-mode - should switch back to RV64
	cpu.SetPriv(PrivMachine)
	if cpu.CurXLEN != XLEN64 {
		t.Errorf("After SetPriv(M) with MXL=2: CurXLEN = %d, want %d", cpu.CurXLEN, XLEN64)
	}
}

// TestCtz32 tests the ctz32 function used for interrupt priority.
// Reference: riscv_cpu.c:1194 uses ctz32 to find the interrupt number from mask
func TestCtz32(t *testing.T) {
	tests := []struct {
		val      uint32
		expected int
	}{
		{0, 32},          // Zero returns 32
		{1, 0},           // Bit 0 set
		{2, 1},           // Bit 1 set
		{3, 0},           // Bits 0,1 set - returns lowest
		{4, 2},           // Bit 2 set
		{0x80, 7},        // Bit 7 set (MTIP position)
		{0x8, 3},         // Bit 3 set (MSIP position)
		{0x800, 11},      // Bit 11 set (MEIP position)
		{0x20, 5},        // Bit 5 set (STIP position)
		{0x200, 9},       // Bit 9 set (SEIP position)
		{0x2, 1},         // Bit 1 set (SSIP position)
		{0x808, 3},       // Bits 3 and 11 set - returns 3 (MSIP, lower priority wins)
		{0x88, 3},        // Bits 3 and 7 set - returns 3 (MSIP)
		{0x820, 5},       // Bits 5 and 11 set - returns 5 (STIP)
		{0xFFFFFFFF, 0},  // All bits set - returns 0
		{0x80000000, 31}, // Only bit 31 set
		{0x00010000, 16}, // Bit 16 set
	}

	for _, tc := range tests {
		got := ctz32(tc.val)
		if got != tc.expected {
			t.Errorf("ctz32(0x%x) = %d, want %d", tc.val, got, tc.expected)
		}
	}
}

// TestInterruptPriorityLowestBitFirst tests that interrupt priority is based on
// lowest bit position (ctz32) per TinyEMU's implementation.
// Reference: riscv_cpu.c:1194 - irq_num = ctz32(mask)
//
// This means SSIP (bit 1) has HIGHER priority than MEIP (bit 11).
func TestInterruptPriorityLowestBitFirst(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)
	cpu.Priv = PrivUser // All interrupts can fire in user mode

	// Set up: both SSIP (bit 1) and MEIP (bit 11) are pending and enabled
	cpu.Mip = MipSSIP | MipMEIP
	cpu.Mie = MipSSIP | MipMEIP
	cpu.Mstatus = 0 // Interrupts enabled in U-mode by default

	// Check interrupts - should take SSIP (bit 1), not MEIP (bit 11)
	taken := cpu.checkInterrupts()
	if !taken {
		t.Fatal("expected interrupt to be taken")
	}

	// The interrupt cause should be SSIP (1), not MEIP (11)
	// mcause has the interrupt bit set in the high bit
	expectedCause := uint64(1) | CauseInterrupt
	if cpu.Mcause != expectedCause {
		t.Errorf("expected mcause 0x%x (SSIP interrupt), got 0x%x", expectedCause, cpu.Mcause)
	}
}

// TestInterruptPriorityMSIPBeforeMEIP tests that MSIP (bit 3) has higher priority
// than MEIP (bit 11) since it has a lower bit position.
func TestInterruptPriorityMSIPBeforeMEIP(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)
	cpu.Priv = PrivMachine
	cpu.Mstatus = MstatusMIE // Enable M-mode interrupts

	// Both MSIP and MEIP pending
	cpu.Mip = MipMSIP | MipMEIP
	cpu.Mie = MipMSIP | MipMEIP
	cpu.Mideleg = 0 // Don't delegate to S-mode

	taken := cpu.checkInterrupts()
	if !taken {
		t.Fatal("expected interrupt to be taken")
	}

	// MSIP (bit 3) should be taken before MEIP (bit 11)
	expectedCause := uint64(3) | CauseInterrupt
	if cpu.Mcause != expectedCause {
		t.Errorf("expected mcause 0x%x (MSIP interrupt), got 0x%x", expectedCause, cpu.Mcause)
	}
}

// TestInterruptPrioritySTIPBeforeSEIP tests that STIP (bit 5) has higher priority
// than SEIP (bit 9) since it has a lower bit position.
func TestInterruptPrioritySTIPBeforeSEIP(t *testing.T) {
	cpu := NewCPU(nil, XLEN64)
	cpu.Priv = PrivSupervisor
	cpu.Mstatus = MstatusSIE // Enable S-mode interrupts

	// Both STIP and SEIP pending
	cpu.Mip = MipSTIP | MipSEIP
	cpu.Mie = MipSTIP | MipSEIP
	cpu.Mideleg = MipSTIP | MipSEIP // Delegate to S-mode

	taken := cpu.checkInterrupts()
	if !taken {
		t.Fatal("expected interrupt to be taken")
	}

	// STIP (bit 5) should be taken before SEIP (bit 9)
	expectedCause := uint64(5) | CauseInterrupt
	if cpu.Scause != expectedCause {
		t.Errorf("expected scause 0x%x (STIP interrupt), got 0x%x", expectedCause, cpu.Scause)
	}
}
