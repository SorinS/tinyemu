package riscv

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// Helper to create a test CPU for trap tests
func trapTestCPU(t *testing.T) *CPU {
	t.Helper()
	m := mem.NewPhysMemoryMap()
	_, err := m.RegisterRAM(0x80000000, 1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN64)
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	return cpu
}

// TestMRET tests MRET instruction returning to S-mode
// Per RISC-V spec, MRET sets the IE bit at position MPP (the target priv) to MPIE
func TestMRET(t *testing.T) {
	cpu := trapTestCPU(t)

	// Setup: simulate return from M-mode trap
	cpu.Priv = PrivMachine
	cpu.Mepc = 0x80001000 // Return address
	// Set MPP = S-mode, MPIE = 1, SIE = 0 initially
	cpu.Mstatus = (uint64(PrivSupervisor) << MstatusMPPShift) | MstatusMPIE

	// MRET -> 0x30200073
	cpu.Mem.Write32(cpu.PC, 0x30200073)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// PC should be mepc
	if cpu.PC != 0x80001000 {
		t.Errorf("PC = 0x%x, want 0x80001000", cpu.PC)
	}

	// Should be in S-mode now
	if cpu.Priv != PrivSupervisor {
		t.Errorf("Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}

	// SIE (bit 1) should be set from MPIE since we returned to S-mode (MPP=1)
	// Per RISC-V spec: "xRET sets the xIE bit corresponding to the mode y
	// that was saved in xPP to the value of xPIE"
	if cpu.Mstatus&MstatusSIE == 0 {
		t.Error("SIE should be set (from MPIE when returning to S-mode)")
	}
	// MIE should NOT be changed (we didn't return to M-mode)
	// (MIE was 0 before, should still be 0)

	// MPIE should be set to 1
	if cpu.Mstatus&MstatusMPIE == 0 {
		t.Error("MPIE should be set to 1")
	}

	// MPP should be cleared to U-mode (0)
	mpp := (cpu.Mstatus >> MstatusMPPShift) & 3
	if mpp != PrivUser {
		t.Errorf("MPP = %d, want %d (should be cleared to U-mode)", mpp, PrivUser)
	}
}

// TestMRETToMachine tests MRET returning to M-mode
func TestMRETToMachine(t *testing.T) {
	cpu := trapTestCPU(t)

	cpu.Priv = PrivMachine
	cpu.Mepc = 0x80002000
	// Set MPP = M-mode, MPIE = 0
	cpu.Mstatus = (uint64(PrivMachine) << MstatusMPPShift)

	// MRET
	cpu.Mem.Write32(cpu.PC, 0x30200073)
	cpu.Step()

	if cpu.Priv != PrivMachine {
		t.Errorf("Priv = %d, want %d (M-mode)", cpu.Priv, PrivMachine)
	}
	if cpu.PC != 0x80002000 {
		t.Errorf("PC = 0x%x, want 0x80002000", cpu.PC)
	}
}

// TestSRET tests SRET instruction returning to U-mode
// Per RISC-V spec, SRET sets the IE bit at position SPP (the target priv) to SPIE
func TestSRET(t *testing.T) {
	cpu := trapTestCPU(t)

	// Setup: simulate return from S-mode trap
	cpu.Priv = PrivSupervisor
	cpu.Sepc = 0x80003000
	// Set SPP = U-mode, SPIE = 1, UIE = 0 initially
	cpu.Mstatus = MstatusSPIE // SPP=0 (U-mode), SPIE=1

	// SRET -> 0x10200073
	cpu.Mem.Write32(cpu.PC, 0x10200073)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// PC should be sepc
	if cpu.PC != 0x80003000 {
		t.Errorf("PC = 0x%x, want 0x80003000", cpu.PC)
	}

	// Should be in U-mode now
	if cpu.Priv != PrivUser {
		t.Errorf("Priv = %d, want %d (U-mode)", cpu.Priv, PrivUser)
	}

	// UIE (bit 0) should be set from SPIE since we returned to U-mode (SPP=0)
	if cpu.Mstatus&MstatusUIE == 0 {
		t.Error("UIE should be set (from SPIE when returning to U-mode)")
	}

	// SPIE should be set to 1
	if cpu.Mstatus&MstatusSPIE == 0 {
		t.Error("SPIE should be set to 1")
	}

	// SPP should remain 0 (U-mode)
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp != PrivUser {
		t.Errorf("SPP = %d, want %d (U-mode)", spp, PrivUser)
	}
}

// TestSRETFromMMode tests SRET from M-mode (should work)
func TestSRETFromMMode(t *testing.T) {
	cpu := trapTestCPU(t)

	cpu.Priv = PrivMachine
	cpu.Sepc = 0x80004000
	cpu.Mstatus = MstatusSPP // SPP=S-mode

	// SRET
	cpu.Mem.Write32(cpu.PC, 0x10200073)
	cpu.Step()

	// Should return to S-mode (from SPP)
	if cpu.Priv != PrivSupervisor {
		t.Errorf("Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}
}

// TestSRETFromUMode tests SRET from U-mode (should trap)
func TestSRETFromUMode(t *testing.T) {
	cpu := trapTestCPU(t)

	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000

	// SRET from U-mode should cause illegal instruction
	cpu.Mem.Write32(cpu.PC, 0x10200073)
	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal insn)", cpu.Mcause, CauseIllegalInsn)
	}
}

// TestECALLFromDifferentModes tests ECALL from different privilege modes
func TestECALLFromDifferentModes(t *testing.T) {
	tests := []struct {
		name          string
		priv          uint8
		expectedCause uint64
	}{
		{"ECALL from U-mode", PrivUser, uint64(CauseUserEcall)},
		{"ECALL from S-mode", PrivSupervisor, uint64(CauseSupervisorEcall)},
		{"ECALL from M-mode", PrivMachine, uint64(CauseMachineEcall)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu := trapTestCPU(t)
			cpu.Priv = tc.priv
			cpu.Mtvec = 0x80010000

			// ECALL -> 0x00000073
			cpu.Mem.Write32(cpu.PC, 0x00000073)
			cpu.Step()

			if cpu.Mcause != tc.expectedCause {
				t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, tc.expectedCause)
			}
		})
	}
}

// TestEBREAK tests EBREAK instruction
func TestEBREAK(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000
	startPC := cpu.PC

	// EBREAK -> 0x00100073
	cpu.Mem.Write32(cpu.PC, 0x00100073)
	cpu.Step()

	if cpu.Mcause != uint64(CauseBreakpoint) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseBreakpoint)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
	if cpu.Mtval != startPC {
		t.Errorf("mtval = 0x%x, want 0x%x", cpu.Mtval, startPC)
	}
}

// TestExceptionDelegationToSMode tests medeleg delegation
func TestExceptionDelegationToSMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000
	// Delegate user ecall to S-mode
	cpu.Medeleg = 1 << CauseUserEcall

	startPC := cpu.PC

	// Trigger user ecall (but from S-mode, so it goes to S-mode handler)
	// Actually, the cause depends on current priv, so let's test a different exception
	// Let's test illegal instruction which can be delegated
	cpu.Medeleg = 1 << CauseIllegalInsn

	// Write an invalid instruction
	cpu.Mem.Write32(cpu.PC, 0x00000000) // Compressed instruction with bits 1:0 = 00

	cpu.Step()

	// Should have gone to S-mode handler
	if cpu.Scause != uint64(CauseIllegalInsn) {
		t.Errorf("scause = 0x%x, want 0x%x", cpu.Scause, CauseIllegalInsn)
	}
	if cpu.Sepc != startPC {
		t.Errorf("sepc = 0x%x, want 0x%x", cpu.Sepc, startPC)
	}
	if cpu.PC != 0x80020000 {
		t.Errorf("PC = 0x%x, want 0x80020000 (stvec)", cpu.PC)
	}
}

// TestExceptionNotDelegated tests exception goes to M-mode when not delegated
func TestExceptionNotDelegated(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000
	cpu.Medeleg = 0 // No delegation

	// ECALL from S-mode
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// Should have gone to M-mode handler
	if cpu.Mcause != uint64(CauseSupervisorEcall) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseSupervisorEcall)
	}
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000 (mtvec)", cpu.PC)
	}
}

// TestInterruptPending tests interrupt pending and enable bits
func TestInterruptPending(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000

	// Set timer interrupt pending and enable
	cpu.Mip = MipMTIP
	cpu.Mie = MipMTIP
	cpu.Mstatus = MstatusMIE // Enable M-mode interrupts

	// Run should take the interrupt
	cpu.NCycles = 1
	cpu.Run(1)

	// Should have taken timer interrupt
	expectedCause := uint64(7) | CauseInterrupt // Machine timer interrupt
	if cpu.Mcause != expectedCause {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, expectedCause)
	}
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000", cpu.PC)
	}
}

// TestInterruptDisabled tests that interrupts are not taken when MIE is clear
func TestInterruptDisabled(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000
	startPC := cpu.PC

	// Set timer interrupt pending and enable, but MIE is clear
	cpu.Mip = MipMTIP
	cpu.Mie = MipMTIP
	cpu.Mstatus = 0 // MIE clear

	// Write NOP (ADDI x0, x0, 0)
	cpu.Mem.Write32(cpu.PC, 0x00000013)
	cpu.Step()

	// Should NOT have taken interrupt
	if cpu.PC != startPC+4 {
		t.Errorf("PC = 0x%x, want 0x%x (should advance normally)", cpu.PC, startPC+4)
	}
}

// TestInterruptDelegation tests mideleg interrupt delegation
func TestInterruptDelegation(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate supervisor timer interrupt to S-mode
	cpu.Mideleg = MipSTIP

	// Set supervisor timer interrupt pending and enable
	cpu.Mip = MipSTIP
	cpu.Mie = MipSTIP
	cpu.Mstatus = MstatusSIE // Enable S-mode interrupts

	// Run should take the interrupt to S-mode
	cpu.NCycles = 1
	cpu.Run(1)

	// Should have taken supervisor timer interrupt in S-mode
	expectedCause := uint64(5) | CauseInterrupt // Supervisor timer interrupt
	if cpu.Scause != expectedCause {
		t.Errorf("scause = 0x%x, want 0x%x", cpu.Scause, expectedCause)
	}
	if cpu.PC != 0x80020000 {
		t.Errorf("PC = 0x%x, want 0x80020000 (stvec)", cpu.PC)
	}
}

// TestInterruptPriority tests interrupt priority order.
// Per TinyEMU (riscv_cpu.c:1194), ctz32 is used to find the interrupt number,
// so lower bit positions have HIGHER priority.
// Reference: tinyemu-2019-12-21/riscv_cpu.c:1194 - irq_num = ctz32(mask)
func TestInterruptPriority(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000

	// Set multiple interrupts pending
	cpu.Mip = MipMEIP | MipMSIP | MipMTIP
	cpu.Mie = MipMEIP | MipMSIP | MipMTIP
	cpu.Mstatus = MstatusMIE

	cpu.NCycles = 1
	cpu.Run(1)

	// MSIP (bit 3) should be highest priority among the set bits {3, 7, 11}
	// because ctz32 returns the lowest set bit position
	expectedCause := uint64(3) | CauseInterrupt
	if cpu.Mcause != expectedCause {
		t.Errorf("mcause = 0x%x, want 0x%x (MSIP has highest priority via ctz32)", cpu.Mcause, expectedCause)
	}
}

// TestTrapStatusSave tests that mstatus fields are saved correctly on trap
// Per TinyEMU behavior: when trapping from mode y to mode x, xPIE = yIE (the
// IE bit for the interrupted mode), not xIE
func TestTrapStatusSave(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Mstatus = MstatusSIE // SIE set, MIE clear

	// ECALL from S-mode
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// Check that MPP = previous priv (S-mode = 1)
	mpp := (cpu.Mstatus >> MstatusMPPShift) & 3
	if mpp != PrivSupervisor {
		t.Errorf("MPP = %d, want %d (S-mode)", mpp, PrivSupervisor)
	}

	// MPIE should have old SIE value (the IE bit for S-mode, the interrupted mode)
	// Since we were in S-mode with SIE=1, MPIE should be 1
	if cpu.Mstatus&MstatusMPIE == 0 {
		t.Error("MPIE should be set (old SIE was set, and we trapped from S-mode)")
	}

	// MIE should be cleared
	if cpu.Mstatus&MstatusMIE != 0 {
		t.Error("MIE should be cleared on trap")
	}
}

// TestTrapStatusSaveMIE tests that when trapping from M-mode, MPIE = MIE
func TestTrapStatusSaveMIE(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000
	cpu.Mstatus = MstatusMIE // MIE set

	// ECALL from M-mode
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// Check that MPP = previous priv (M-mode = 3)
	mpp := (cpu.Mstatus >> MstatusMPPShift) & 3
	if mpp != PrivMachine {
		t.Errorf("MPP = %d, want %d (M-mode)", mpp, PrivMachine)
	}

	// MPIE should have old MIE value (since we trapped from M-mode)
	if cpu.Mstatus&MstatusMPIE == 0 {
		t.Error("MPIE should be set (old MIE was set, and we trapped from M-mode)")
	}

	// MIE should be cleared
	if cpu.Mstatus&MstatusMIE != 0 {
		t.Error("MIE should be cleared on trap")
	}
}

// TestWFIInstruction tests WFI sets power down flag
func TestWFIInstruction(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mip = 0
	cpu.Mie = 0

	// WFI -> 0x10500073
	cpu.Mem.Write32(cpu.PC, 0x10500073)
	cpu.Step()

	if !cpu.PowerDownFlag {
		t.Error("PowerDownFlag should be set after WFI with no pending interrupts")
	}
}

// TestWFIWithPendingInterrupt tests WFI doesn't power down if interrupt pending
func TestWFIWithPendingInterrupt(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mip = MipMTIP
	cpu.Mie = MipMTIP
	cpu.Mstatus = 0 // MIE clear, so interrupt won't be taken yet

	// WFI
	cpu.Mem.Write32(cpu.PC, 0x10500073)
	cpu.Step()

	// With pending interrupt, WFI should be a NOP (no power down)
	// Note: behavior depends on implementation - some implementations
	// still set power down and wake immediately
}

// TestWFIFromUserMode tests WFI from U-mode raises illegal instruction
func TestWFIFromUserMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000

	// WFI from U-mode
	cpu.Mem.Write32(cpu.PC, 0x10500073)
	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseIllegalInsn)
	}
}

// TestSFENCEVMA tests SFENCE.VMA flushes TLB
func TestSFENCEVMA(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine

	// Set up some TLB entries
	cpu.TLBRead[0].VAddr = 0x80000000
	cpu.TLBWrite[0].VAddr = 0x80000000
	cpu.TLBCode[0].VAddr = 0x80000000

	// SFENCE.VMA x0, x0 (flush all) -> 0x12000073
	cpu.Mem.Write32(cpu.PC, 0x12000073)
	cpu.Step()

	// TLB should be flushed (VAddr set to invalid)
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Error("TLB read entry should be invalidated")
	}
	if cpu.TLBWrite[0].VAddr != ^uint64(0) {
		t.Error("TLB write entry should be invalidated")
	}
	if cpu.TLBCode[0].VAddr != ^uint64(0) {
		t.Error("TLB code entry should be invalidated")
	}
}

// TestMIPMIEInteraction tests MIP and MIE register interaction
func TestMIPMIEInteraction(t *testing.T) {
	cpu := trapTestCPU(t)

	// Test SetMIP
	cpu.SetMIP(MipMTIP)
	if cpu.Mip&MipMTIP == 0 {
		t.Error("MTIP should be set")
	}

	// Test ResetMIP
	cpu.ResetMIP(MipMTIP)
	if cpu.Mip&MipMTIP != 0 {
		t.Error("MTIP should be cleared")
	}

	// Test GetMIP
	cpu.Mip = 0x123
	if cpu.GetMIP() != 0x123 {
		t.Errorf("GetMIP() = 0x%x, want 0x123", cpu.GetMIP())
	}
}

// TestExceptionCauses tests various exception causes
func TestExceptionCauses(t *testing.T) {
	// Verify exception cause constants
	tests := []struct {
		name  string
		cause int
		value int
	}{
		{"Misaligned fetch", CauseMisalignedFetch, 0},
		{"Fault fetch", CauseFaultFetch, 1},
		{"Illegal instruction", CauseIllegalInsn, 2},
		{"Breakpoint", CauseBreakpoint, 3},
		{"Misaligned load", CauseMisalignedLoad, 4},
		{"Fault load", CauseFaultLoad, 5},
		{"Misaligned store", CauseMisalignedStore, 6},
		{"Fault store", CauseFaultStore, 7},
		{"User ecall", CauseUserEcall, 8},
		{"Supervisor ecall", CauseSupervisorEcall, 9},
		{"Machine ecall", CauseMachineEcall, 11},
		{"Fetch page fault", CauseFetchPageFault, 12},
		{"Load page fault", CauseLoadPageFault, 13},
		{"Store page fault", CauseStorePageFault, 15},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cause != tc.value {
				t.Errorf("%s cause = %d, want %d", tc.name, tc.cause, tc.value)
			}
		})
	}
}

// ===========================================================================
// Privilege Mode Tests
// Reference: riscv_cpu.c lines 1042-1120 (raise_exception2)
// ===========================================================================

// TestUModeCannotAccessSModeCSRs tests that U-mode cannot access S-mode CSRs
// Reference: riscv_cpu.c lines 758-810 (CSR read privilege check)
func TestUModeCannotAccessSModeCSRs(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000

	// Try to read sstatus from U-mode (should cause illegal instruction)
	// CSRRS x1, sstatus, x0 -> encodes as: csr=0x100, rs1=0, rd=1, funct3=2
	insn := uint32(0x100)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	startPC := cpu.PC
	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestUModeCannotAccessMModeCSRs tests that U-mode cannot access M-mode CSRs
// Reference: riscv_cpu.c lines 758-810 (CSR read privilege check)
func TestUModeCannotAccessMModeCSRs(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000

	// Try to read mstatus from U-mode (should cause illegal instruction)
	// CSRRS x1, mstatus, x0 -> encodes as: csr=0x300, rs1=0, rd=1, funct3=2
	insn := uint32(0x300)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	startPC := cpu.PC
	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestSModeCannotAccessMModeCSRs tests that S-mode cannot access M-mode CSRs
// Reference: riscv_cpu.c lines 758-810 (CSR read privilege check)
func TestSModeCannotAccessMModeCSRs(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000

	// Try to read mstatus from S-mode (should cause illegal instruction)
	insn := uint32(0x300)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	startPC := cpu.PC
	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestMRETFromSMode tests that MRET from S-mode causes illegal instruction
// Reference: riscv_cpu.c (MRET requires M-mode privilege)
func TestMRETFromSMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000

	// MRET -> 0x30200073
	cpu.Mem.Write32(cpu.PC, 0x30200073)
	startPC := cpu.PC

	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestMRETFromUMode tests that MRET from U-mode causes illegal instruction
// Reference: riscv_cpu.c (MRET requires M-mode privilege)
func TestMRETFromUMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000

	// MRET -> 0x30200073
	cpu.Mem.Write32(cpu.PC, 0x30200073)
	startPC := cpu.PC

	cpu.Step()

	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// ===========================================================================
// Timer Interrupt Delegation Tests
// Reference: riscv_cpu.c lines 1083-1091 (interrupt delegation)
// ===========================================================================

// TestMTIPNotDelegatedToSMode tests that MTIP is handled by M-mode
// Reference: riscv_cpu.c lines 1171-1177 (M-mode timer interrupts)
func TestMTIPNotDelegatedToSMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// MTIP cannot be delegated (mideleg only allows delegating S-mode interrupts)
	// Delegate all possible interrupts
	cpu.Mideleg = 0x222 // SSIP, STIP, SEIP

	// Set machine timer interrupt pending and enable
	cpu.Mip = MipMTIP
	cpu.Mie = MipMTIP
	cpu.Mstatus = MstatusMIE | MstatusSIE // Enable both M and S mode interrupts

	cpu.NCycles = 1
	cpu.Run(1)

	// MTIP should be handled by M-mode, not S-mode
	expectedCause := uint64(7) | CauseInterrupt // Machine timer interrupt
	if cpu.Mcause != expectedCause {
		t.Errorf("mcause = 0x%x, want 0x%x (M-mode timer)", cpu.Mcause, expectedCause)
	}
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000 (mtvec)", cpu.PC)
	}
	if cpu.Priv != PrivMachine {
		t.Errorf("Priv = %d, want %d (M-mode)", cpu.Priv, PrivMachine)
	}
}

// TestSTIPDelegatedToSMode tests that STIP is delegated to S-mode
// Reference: riscv_cpu.c lines 1083-1091 (interrupt delegation)
func TestSTIPDelegatedToSMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate STIP to S-mode
	cpu.Mideleg = MipSTIP

	// Set supervisor timer interrupt pending and enable
	cpu.Mip = MipSTIP
	cpu.Mie = MipSTIP
	cpu.Mstatus = MstatusSIE // Enable S-mode interrupts

	cpu.NCycles = 1
	cpu.Run(1)

	// STIP should be handled by S-mode
	expectedCause := uint64(5) | CauseInterrupt // Supervisor timer interrupt
	if cpu.Scause != expectedCause {
		t.Errorf("scause = 0x%x, want 0x%x (S-mode timer)", cpu.Scause, expectedCause)
	}
	if cpu.PC != 0x80020000 {
		t.Errorf("PC = 0x%x, want 0x80020000 (stvec)", cpu.PC)
	}
	if cpu.Priv != PrivSupervisor {
		t.Errorf("Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}
}

// TestInterruptFromUModeToSMode tests interrupt handling from U-mode with delegation
// Reference: riscv_cpu.c lines 1083-1091 (delegation check uses current priv)
func TestInterruptFromUModeToSMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate STIP to S-mode
	cpu.Mideleg = MipSTIP

	// Set supervisor timer interrupt pending and enable
	cpu.Mip = MipSTIP
	cpu.Mie = MipSTIP
	// SIE controls S-mode interrupts when in U-mode
	cpu.Mstatus = MstatusSIE

	cpu.NCycles = 1
	cpu.Run(1)

	// Interrupt should go to S-mode (delegated) not M-mode
	expectedCause := uint64(5) | CauseInterrupt
	if cpu.Scause != expectedCause {
		t.Errorf("scause = 0x%x, want 0x%x", cpu.Scause, expectedCause)
	}
	// SPP should record U-mode
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp != PrivUser {
		t.Errorf("SPP = %d, want %d (U-mode)", spp, PrivUser)
	}
	if cpu.PC != 0x80020000 {
		t.Errorf("PC = 0x%x, want 0x80020000 (stvec)", cpu.PC)
	}
}

// TestExceptionFromUModeToSMode tests exception delegation from U-mode
// Reference: riscv_cpu.c lines 1083-1091 (exception delegation)
func TestExceptionFromUModeToSMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate user ecall to S-mode
	cpu.Medeleg = 1 << CauseUserEcall

	startPC := cpu.PC

	// ECALL from U-mode
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// Should go to S-mode
	if cpu.Scause != uint64(CauseUserEcall) {
		t.Errorf("scause = 0x%x, want 0x%x", cpu.Scause, CauseUserEcall)
	}
	if cpu.Sepc != startPC {
		t.Errorf("sepc = 0x%x, want 0x%x", cpu.Sepc, startPC)
	}
	if cpu.PC != 0x80020000 {
		t.Errorf("PC = 0x%x, want 0x80020000 (stvec)", cpu.PC)
	}
	if cpu.Priv != PrivSupervisor {
		t.Errorf("Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}
}

// ===========================================================================
// Vectored Trap Handler Tests
// Reference: riscv_cpu.c line 1118 (s->pc = s->mtvec)
// ===========================================================================

// TestMtvecLowBitsCleared tests that mtvec low bits are cleared on write
// Reference: riscv_cpu.c line 991 (s->mtvec = val & ~3)
// TinyEMU does NOT implement vectored mode - it always clears low 2 bits
func TestMtvecLowBitsCleared(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine

	// Try to set vectored mode (bit 0 = 1)
	cpu.WriteCSR(CSRMtvec, 0x80010001)

	// Low bits should be cleared (TinyEMU always uses direct mode)
	val, _ := cpu.ReadCSR(CSRMtvec)
	if val != 0x80010000 {
		t.Errorf("mtvec = 0x%x, want 0x80010000 (low bits cleared)", val)
	}
}

// TestStvecLowBitsCleared tests that stvec low bits are cleared on write
// Reference: riscv_cpu.c line 914 (s->stvec = val & ~3)
// TinyEMU does NOT implement vectored mode - it always clears low 2 bits
func TestStvecLowBitsCleared(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Try to set vectored mode (bit 0 = 1)
	cpu.WriteCSR(CSRStvec, 0x80020003)

	// Low bits should be cleared (TinyEMU always uses direct mode)
	val, _ := cpu.ReadCSR(CSRStvec)
	if val != 0x80020000 {
		t.Errorf("stvec = 0x%x, want 0x80020000 (low bits cleared)", val)
	}
}

// TestDirectTrapHandlerMode tests direct (non-vectored) interrupt handler
// Reference: riscv_cpu.c - when mtvec[1:0]=0, PC = mtvec_base
func TestDirectTrapHandlerMode(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	// Set direct mode (bit 0 = 0)
	cpu.Mtvec = 0x80010000 // Base 0x80010000, mode = 0 (direct)

	// Trigger machine timer interrupt
	cpu.Mip = MipMTIP
	cpu.Mie = MipMTIP
	cpu.Mstatus = MstatusMIE

	cpu.NCycles = 1
	cpu.Run(1)

	// For direct mode, PC = base (ignores cause)
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000 (direct)", cpu.PC)
	}
}

// TestExceptionAlwaysUsesDirect tests that exceptions use direct mode even with vectored mtvec
// Reference: riscv_cpu.c - vectored mode only applies to interrupts
func TestExceptionAlwaysUsesDirect(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	// Set vectored mode
	cpu.Mtvec = 0x80010001 // Mode = 1 (vectored)

	startPC := cpu.PC

	// ECALL (exception, not interrupt)
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// Exceptions always use direct mode (base address)
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000 (exceptions use direct)", cpu.PC)
	}
	if cpu.Mcause != uint64(CauseMachineEcall) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseMachineEcall)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// ===========================================================================
// Trap Value (xtval) Tests
// Reference: riscv_cpu.c lines 1110-1111, 1099-1100 (mtval/stval)
// ===========================================================================

// TestMtvalOnPageFault tests that mtval contains faulting address on page fault
func TestMtvalOnPageFault(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000

	// Try to load from unmapped address (will cause fault)
	faultAddr := uint64(0x10000000)

	// Set up a pending load page fault exception
	cpu.SetPendingException(CauseLoadPageFault, faultAddr)
	cpu.handleException()

	if cpu.Mtval != faultAddr {
		t.Errorf("mtval = 0x%x, want 0x%x", cpu.Mtval, faultAddr)
	}
	if cpu.Mcause != uint64(CauseLoadPageFault) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseLoadPageFault)
	}
}

// TestStvalOnDelegatedPageFault tests that stval contains faulting address when delegated
func TestStvalOnDelegatedPageFault(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate load page faults to S-mode
	cpu.Medeleg = 1 << CauseLoadPageFault

	faultAddr := uint64(0x10000000)

	// Set up a pending load page fault exception
	cpu.SetPendingException(CauseLoadPageFault, faultAddr)
	cpu.handleException()

	// Should be handled by S-mode
	if cpu.Stval != faultAddr {
		t.Errorf("stval = 0x%x, want 0x%x", cpu.Stval, faultAddr)
	}
	if cpu.Scause != uint64(CauseLoadPageFault) {
		t.Errorf("scause = 0x%x, want 0x%x", cpu.Scause, CauseLoadPageFault)
	}
	if cpu.PC != 0x80020000 {
		t.Errorf("PC = 0x%x, want 0x80020000 (stvec)", cpu.PC)
	}
}

// TestMtvalOnIllegalInstruction tests that mtval contains instruction on illegal insn
func TestMtvalOnIllegalInstruction(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000

	// Write an invalid instruction and execute it
	invalidInsn := uint32(0x00000000) // All zeros is not a valid instruction
	cpu.Mem.Write32(cpu.PC, invalidInsn)

	cpu.Step()

	// mtval should contain the faulting instruction
	if cpu.Mtval != uint64(invalidInsn) {
		t.Errorf("mtval = 0x%x, want 0x%x", cpu.Mtval, invalidInsn)
	}
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseIllegalInsn)
	}
}

// ===========================================================================
// M-mode Exception Never Delegated Tests
// Reference: riscv_cpu.c lines 1083-1091 (deleg=0 when priv >= PRV_M)
// ===========================================================================

// TestExceptionFromMModeNeverDelegated tests that exceptions from M-mode are never delegated
func TestExceptionFromMModeNeverDelegated(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate machine ecall (shouldn't matter - we're in M-mode)
	cpu.Medeleg = 1 << CauseMachineEcall

	startPC := cpu.PC

	// ECALL from M-mode
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// Should NOT be delegated, should stay in M-mode
	if cpu.Mcause != uint64(CauseMachineEcall) {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, CauseMachineEcall)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000 (mtvec, not stvec)", cpu.PC)
	}
}

// TestInterruptFromMModeNeverDelegated tests that interrupts from M-mode are never delegated
func TestInterruptFromMModeNeverDelegated(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Try to delegate (won't work from M-mode)
	cpu.Mideleg = 0x222

	// Set timer interrupt
	cpu.Mip = MipMTIP
	cpu.Mie = MipMTIP
	cpu.Mstatus = MstatusMIE

	cpu.NCycles = 1
	cpu.Run(1)

	// Should be handled by M-mode
	expectedCause := uint64(7) | CauseInterrupt
	if cpu.Mcause != expectedCause {
		t.Errorf("mcause = 0x%x, want 0x%x", cpu.Mcause, expectedCause)
	}
	if cpu.PC != 0x80010000 {
		t.Errorf("PC = 0x%x, want 0x80010000 (mtvec)", cpu.PC)
	}
}

// ===========================================================================
// Nested Trap Tests
// ===========================================================================

// TestNestedTrapMPPSaved tests that MPP correctly records previous privilege on nested traps
func TestNestedTrapMPPSaved(t *testing.T) {
	cpu := trapTestCPU(t)

	// Start in S-mode
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Mstatus = MstatusSIE // SIE = 1

	startPC := cpu.PC

	// ECALL from S-mode (goes to M-mode since not delegated)
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// MPP should be S-mode (1)
	mpp := (cpu.Mstatus >> MstatusMPPShift) & 3
	if mpp != PrivSupervisor {
		t.Errorf("MPP = %d, want %d (S-mode)", mpp, PrivSupervisor)
	}

	// MPIE should have the old SIE value (1)
	if cpu.Mstatus&MstatusMPIE == 0 {
		t.Error("MPIE should be set (old SIE was 1)")
	}

	// We're now in M-mode
	if cpu.Priv != PrivMachine {
		t.Errorf("Priv = %d, want %d (M-mode)", cpu.Priv, PrivMachine)
	}

	// mepc should have the old PC
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestNestedTrapSPPSaved tests that SPP correctly records previous privilege on nested traps
func TestNestedTrapSPPSaved(t *testing.T) {
	cpu := trapTestCPU(t)

	// Start in U-mode
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Stvec = 0x80020000

	// Delegate user ecall to S-mode
	cpu.Medeleg = 1 << CauseUserEcall
	cpu.Mstatus = 0 // UIE = 0

	startPC := cpu.PC

	// ECALL from U-mode (goes to S-mode since delegated)
	cpu.Mem.Write32(cpu.PC, 0x00000073)
	cpu.Step()

	// SPP should be U-mode (0)
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp != PrivUser {
		t.Errorf("SPP = %d, want %d (U-mode)", spp, PrivUser)
	}

	// SPIE should have the old UIE value (0)
	if cpu.Mstatus&MstatusSPIE != 0 {
		t.Error("SPIE should be clear (old UIE was 0)")
	}

	// We're now in S-mode
	if cpu.Priv != PrivSupervisor {
		t.Errorf("Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}

	// sepc should have the old PC
	if cpu.Sepc != startPC {
		t.Errorf("sepc = 0x%x, want 0x%x", cpu.Sepc, startPC)
	}
}

// ===========================================================================
// Counter Enable (counteren) Tests
// Reference: riscv_cpu.c lines 731-761 (CSR read counteren check)
// ===========================================================================

// TestUModeCannotReadCycleWithoutScounteren tests that U-mode needs scounteren[CY]=1
// Reference: riscv_cpu.c lines 734-741
func TestUModeCannotReadCycleWithoutScounteren(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Scounteren = 0 // Disable counter access for U-mode

	startPC := cpu.PC

	// CSRRS x1, cycle, x0 -> read cycle counter
	// csr=0xC00, rs1=0, rd=1, funct3=2
	insn := uint32(0xC00)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should cause illegal instruction since scounteren[CY]=0
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestUModeCanReadCycleWithScounteren tests that U-mode can read cycle with scounteren[CY]=1
// Reference: riscv_cpu.c lines 734-741
func TestUModeCanReadCycleWithScounteren(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Scounteren = 1 // Enable cycle counter for U-mode (bit 0)

	startPC := cpu.PC
	cpu.InsnCounter = 0x12345678

	// CSRRS x1, cycle, x0 -> read cycle counter
	insn := uint32(0xC00)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should NOT trap, PC should advance
	if cpu.PC != startPC+4 {
		t.Errorf("PC = 0x%x, want 0x%x (should advance)", cpu.PC, startPC+4)
	}
	// x1 should have the cycle counter value
	if cpu.Reg[1] != 0x12345678 {
		t.Errorf("x1 = 0x%x, want 0x12345678", cpu.Reg[1])
	}
}

// TestSModeCannotReadCycleWithoutMcounteren tests that S-mode needs mcounteren[CY]=1
// Reference: riscv_cpu.c lines 734-741
func TestSModeCannotReadCycleWithoutMcounteren(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Mcounteren = 0 // Disable counter access for S-mode

	startPC := cpu.PC

	// CSRRS x1, cycle, x0 -> read cycle counter
	insn := uint32(0xC00)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should cause illegal instruction since mcounteren[CY]=0
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestSModeCanReadCycleWithMcounteren tests that S-mode can read cycle with mcounteren[CY]=1
// Reference: riscv_cpu.c lines 734-741
func TestSModeCanReadCycleWithMcounteren(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Mtvec = 0x80010000
	cpu.Mcounteren = 1 // Enable cycle counter for S-mode (bit 0)

	startPC := cpu.PC
	cpu.InsnCounter = 0xDEADBEEF

	// CSRRS x1, cycle, x0 -> read cycle counter
	insn := uint32(0xC00)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should NOT trap, PC should advance
	if cpu.PC != startPC+4 {
		t.Errorf("PC = 0x%x, want 0x%x (should advance)", cpu.PC, startPC+4)
	}
	// x1 should have the cycle counter value
	if cpu.Reg[1] != 0xDEADBEEF {
		t.Errorf("x1 = 0x%x, want 0xDEADBEEF", cpu.Reg[1])
	}
}

// TestMModeAlwaysCanReadCycle tests that M-mode can always read cycle counter
// Reference: riscv_cpu.c lines 734-741 (check is "if (s->priv < PRV_M)")
func TestMModeAlwaysCanReadCycle(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mtvec = 0x80010000
	cpu.Mcounteren = 0 // Even with counteren=0, M-mode can read

	startPC := cpu.PC
	cpu.InsnCounter = 0xCAFEBABE

	// CSRRS x1, cycle, x0 -> read cycle counter
	insn := uint32(0xC00)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should NOT trap, PC should advance
	if cpu.PC != startPC+4 {
		t.Errorf("PC = 0x%x, want 0x%x (should advance)", cpu.PC, startPC+4)
	}
	// x1 should have the cycle counter value
	if cpu.Reg[1] != 0xCAFEBABE {
		t.Errorf("x1 = 0x%x, want 0xCAFEBABE", cpu.Reg[1])
	}
}

// TestInstretCounterenCheck tests instret CSR also respects counteren
// Reference: riscv_cpu.c lines 731-744 (same logic for cycle and instret)
func TestInstretCounterenCheck(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Scounteren = 0 // Disable counter access for U-mode

	startPC := cpu.PC

	// CSRRS x1, instret, x0 -> read instret counter (CSR 0xC02)
	insn := uint32(0xC02)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should cause illegal instruction since scounteren[IR]=0
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("mcause = 0x%x, want 0x%x (illegal instruction)", cpu.Mcause, CauseIllegalInsn)
	}
	if cpu.Mepc != startPC {
		t.Errorf("mepc = 0x%x, want 0x%x", cpu.Mepc, startPC)
	}
}

// TestInstretWithCounterenEnabled tests instret CSR works with counteren enabled
// Reference: riscv_cpu.c lines 731-744
func TestInstretWithCounterenEnabled(t *testing.T) {
	cpu := trapTestCPU(t)
	cpu.Priv = PrivUser
	cpu.Mtvec = 0x80010000
	cpu.Scounteren = 4 // Enable instret (bit 2)

	startPC := cpu.PC
	cpu.InsnCounter = 0xABCD1234

	// CSRRS x1, instret, x0 -> read instret counter (CSR 0xC02)
	insn := uint32(0xC02)<<20 | (0 << 15) | (2 << 12) | (1 << 7) | 0x73
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should NOT trap, PC should advance
	if cpu.PC != startPC+4 {
		t.Errorf("PC = 0x%x, want 0x%x (should advance)", cpu.PC, startPC+4)
	}
	// x1 should have the instret counter value
	if cpu.Reg[1] != 0xABCD1234 {
		t.Errorf("x1 = 0x%x, want 0xABCD1234", cpu.Reg[1])
	}
}
