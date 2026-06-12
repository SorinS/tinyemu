package riscv

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// Helper to create a test CPU for CSR tests
func csrTestCPU(t *testing.T) *CPU {
	t.Helper()
	m := mem.NewPhysMemoryMap()
	_, err := m.RegisterRAM(0x80000000, 1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN64)
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine // Start in M-mode for full CSR access
	return cpu
}

// encodeCSRRW encodes CSRRW rd, csr, rs1
func encodeCSRRW(rd, rs1 int, csr uint32) uint32 {
	// CSRRW: funct3=001, opcode=1110011
	return (csr << 20) | (uint32(rs1) << 15) | (1 << 12) | (uint32(rd) << 7) | 0x73
}

// encodeCSRRS encodes CSRRS rd, csr, rs1
func encodeCSRRS(rd, rs1 int, csr uint32) uint32 {
	// CSRRS: funct3=010, opcode=1110011
	return (csr << 20) | (uint32(rs1) << 15) | (2 << 12) | (uint32(rd) << 7) | 0x73
}

// encodeCSRRC encodes CSRRC rd, csr, rs1
func encodeCSRRC(rd, rs1 int, csr uint32) uint32 {
	// CSRRC: funct3=011, opcode=1110011
	return (csr << 20) | (uint32(rs1) << 15) | (3 << 12) | (uint32(rd) << 7) | 0x73
}

// encodeCSRRWI encodes CSRRWI rd, csr, zimm
func encodeCSRRWI(rd int, csr uint32, zimm uint32) uint32 {
	// CSRRWI: funct3=101, opcode=1110011
	return (csr << 20) | (zimm << 15) | (5 << 12) | (uint32(rd) << 7) | 0x73
}

// encodeCSRRSI encodes CSRRSI rd, csr, zimm
func encodeCSRRSI(rd int, csr uint32, zimm uint32) uint32 {
	// CSRRSI: funct3=110, opcode=1110011
	return (csr << 20) | (zimm << 15) | (6 << 12) | (uint32(rd) << 7) | 0x73
}

// encodeCSRRCI encodes CSRRCI rd, csr, zimm
func encodeCSRRCI(rd int, csr uint32, zimm uint32) uint32 {
	// CSRRCI: funct3=111, opcode=1110011
	return (csr << 20) | (zimm << 15) | (7 << 12) | (uint32(rd) << 7) | 0x73
}

// TestCSRRW tests CSRRW instruction (atomic swap)
func TestCSRRW(t *testing.T) {
	cpu := csrTestCPU(t)

	tests := []struct {
		name       string
		csr        uint32
		initCSR    uint64
		rs1Val     uint64
		rd         int
		expectedRd uint64
	}{
		{"CSRRW mscratch", CSRMscratch, 0x1234, 0x5678, 1, 0x1234},
		{"CSRRW mtvec", CSRMtvec, 0xABCD0000, 0x12340000, 2, 0xABCD0000},
		{"CSRRW mepc", CSRMepc, 0x80001000, 0x80002000, 3, 0x80001000},
		{"CSRRW to x0", CSRMscratch, 0xFFFF, 0x1111, 0, 0}, // x0 stays 0
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.Priv = PrivMachine

			// Set initial CSR value
			if err := cpu.WriteCSR(tc.csr, tc.initCSR); err != nil {
				t.Fatalf("failed to set initial CSR: %v", err)
			}

			// Set rs1 value
			cpu.SetReg(4, tc.rs1Val)

			// CSRRW rd, csr, x4
			insn := encodeCSRRW(tc.rd, 4, tc.csr)
			cpu.Mem.Write32(cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd got old CSR value
			if cpu.GetReg(tc.rd) != tc.expectedRd {
				t.Errorf("rd = 0x%x, want 0x%x", cpu.GetReg(tc.rd), tc.expectedRd)
			}

			// Check CSR has new value
			csrVal, _ := cpu.ReadCSR(tc.csr)
			if tc.rd != 0 && csrVal != tc.rs1Val {
				// Note: some CSRs have masks, so check against masked value
				t.Errorf("CSR = 0x%x, want 0x%x", csrVal, tc.rs1Val)
			}
		})
	}
}

// TestCSRRS tests CSRRS instruction (atomic set bits)
func TestCSRRS(t *testing.T) {
	cpu := csrTestCPU(t)

	tests := []struct {
		name        string
		csr         uint32
		initCSR     uint64
		rs1Val      uint64
		rd          int
		expectedRd  uint64
		expectedCSR uint64
	}{
		{"CSRRS set bits", CSRMscratch, 0x00F0, 0x0F00, 1, 0x00F0, 0x0FF0},
		{"CSRRS all bits", CSRMscratch, 0x0000, 0xFFFF, 2, 0x0000, 0xFFFF},
		{"CSRRS no change (x0)", CSRMscratch, 0x1234, 0x0000, 1, 0x1234, 0x1234},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.Priv = PrivMachine

			// Set initial CSR value
			if err := cpu.WriteCSR(tc.csr, tc.initCSR); err != nil {
				t.Fatalf("failed to set initial CSR: %v", err)
			}

			// Set rs1 value
			cpu.SetReg(4, tc.rs1Val)

			// CSRRS rd, csr, x4
			insn := encodeCSRRS(tc.rd, 4, tc.csr)
			cpu.Mem.Write32(cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd got old CSR value
			if cpu.GetReg(tc.rd) != tc.expectedRd {
				t.Errorf("rd = 0x%x, want 0x%x", cpu.GetReg(tc.rd), tc.expectedRd)
			}

			// Check CSR has correct value
			csrVal, _ := cpu.ReadCSR(tc.csr)
			if csrVal != tc.expectedCSR {
				t.Errorf("CSR = 0x%x, want 0x%x", csrVal, tc.expectedCSR)
			}
		})
	}
}

// TestCSRRSReadOnly tests CSRRS with rs1=x0 (read-only operation)
func TestCSRRSReadOnly(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mscratch = 0x12345678

	// CSRRS x1, mscratch, x0 - should only read, not modify
	insn := encodeCSRRS(1, 0, CSRMscratch)
	cpu.Mem.Write32(cpu.PC, insn)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// rd should have CSR value
	if cpu.GetReg(1) != 0x12345678 {
		t.Errorf("x1 = 0x%x, want 0x12345678", cpu.GetReg(1))
	}

	// CSR should be unchanged (no write when rs1=x0)
	if cpu.Mscratch != 0x12345678 {
		t.Errorf("mscratch = 0x%x, want 0x12345678", cpu.Mscratch)
	}
}

// TestCSRRC tests CSRRC instruction (atomic clear bits)
func TestCSRRC(t *testing.T) {
	cpu := csrTestCPU(t)

	tests := []struct {
		name        string
		csr         uint32
		initCSR     uint64
		rs1Val      uint64
		rd          int
		expectedRd  uint64
		expectedCSR uint64
	}{
		{"CSRRC clear bits", CSRMscratch, 0xFFFF, 0x0FF0, 1, 0xFFFF, 0xF00F},
		{"CSRRC clear all", CSRMscratch, 0xFFFF, 0xFFFF, 2, 0xFFFF, 0x0000},
		{"CSRRC clear none", CSRMscratch, 0x1234, 0x0000, 1, 0x1234, 0x1234},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.Priv = PrivMachine

			// Set initial CSR value
			if err := cpu.WriteCSR(tc.csr, tc.initCSR); err != nil {
				t.Fatalf("failed to set initial CSR: %v", err)
			}

			// Set rs1 value
			cpu.SetReg(4, tc.rs1Val)

			// CSRRC rd, csr, x4
			insn := encodeCSRRC(tc.rd, 4, tc.csr)
			cpu.Mem.Write32(cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd got old CSR value
			if cpu.GetReg(tc.rd) != tc.expectedRd {
				t.Errorf("rd = 0x%x, want 0x%x", cpu.GetReg(tc.rd), tc.expectedRd)
			}

			// Check CSR has correct value
			csrVal, _ := cpu.ReadCSR(tc.csr)
			if csrVal != tc.expectedCSR {
				t.Errorf("CSR = 0x%x, want 0x%x", csrVal, tc.expectedCSR)
			}
		})
	}
}

// TestCSRRWI tests CSRRWI instruction (immediate swap)
func TestCSRRWI(t *testing.T) {
	cpu := csrTestCPU(t)

	tests := []struct {
		name       string
		csr        uint32
		initCSR    uint64
		zimm       uint32
		rd         int
		expectedRd uint64
	}{
		{"CSRRWI basic", CSRMscratch, 0x1234, 0x1F, 1, 0x1234},
		{"CSRRWI zero", CSRMscratch, 0xFFFF, 0x00, 2, 0xFFFF},
		{"CSRRWI max zimm", CSRMscratch, 0x0000, 0x1F, 3, 0x0000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.Priv = PrivMachine

			// Set initial CSR value
			if err := cpu.WriteCSR(tc.csr, tc.initCSR); err != nil {
				t.Fatalf("failed to set initial CSR: %v", err)
			}

			// CSRRWI rd, csr, zimm
			insn := encodeCSRRWI(tc.rd, tc.csr, tc.zimm)
			cpu.Mem.Write32(cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd got old CSR value
			if cpu.GetReg(tc.rd) != tc.expectedRd {
				t.Errorf("rd = 0x%x, want 0x%x", cpu.GetReg(tc.rd), tc.expectedRd)
			}

			// Check CSR has new value (zimm is zero-extended)
			csrVal, _ := cpu.ReadCSR(tc.csr)
			if csrVal != uint64(tc.zimm) {
				t.Errorf("CSR = 0x%x, want 0x%x", csrVal, tc.zimm)
			}
		})
	}
}

// TestCSRRSI tests CSRRSI instruction (immediate set bits)
func TestCSRRSI(t *testing.T) {
	cpu := csrTestCPU(t)

	tests := []struct {
		name        string
		csr         uint32
		initCSR     uint64
		zimm        uint32
		rd          int
		expectedRd  uint64
		expectedCSR uint64
	}{
		{"CSRRSI set bits", CSRMscratch, 0x00, 0x1F, 1, 0x00, 0x1F},
		{"CSRRSI partial", CSRMscratch, 0xF0, 0x0F, 2, 0xF0, 0xFF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.Priv = PrivMachine

			// Set initial CSR value
			if err := cpu.WriteCSR(tc.csr, tc.initCSR); err != nil {
				t.Fatalf("failed to set initial CSR: %v", err)
			}

			// CSRRSI rd, csr, zimm
			insn := encodeCSRRSI(tc.rd, tc.csr, tc.zimm)
			cpu.Mem.Write32(cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd got old CSR value
			if cpu.GetReg(tc.rd) != tc.expectedRd {
				t.Errorf("rd = 0x%x, want 0x%x", cpu.GetReg(tc.rd), tc.expectedRd)
			}

			// Check CSR has correct value
			csrVal, _ := cpu.ReadCSR(tc.csr)
			if csrVal != tc.expectedCSR {
				t.Errorf("CSR = 0x%x, want 0x%x", csrVal, tc.expectedCSR)
			}
		})
	}
}

// TestCSRRCI tests CSRRCI instruction (immediate clear bits)
func TestCSRRCI(t *testing.T) {
	cpu := csrTestCPU(t)

	tests := []struct {
		name        string
		csr         uint32
		initCSR     uint64
		zimm        uint32
		rd          int
		expectedRd  uint64
		expectedCSR uint64
	}{
		{"CSRRCI clear bits", CSRMscratch, 0xFF, 0x0F, 1, 0xFF, 0xF0},
		{"CSRRCI clear all low", CSRMscratch, 0x1F, 0x1F, 2, 0x1F, 0x00},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.PC = 0x80000000
			cpu.Priv = PrivMachine

			// Set initial CSR value
			if err := cpu.WriteCSR(tc.csr, tc.initCSR); err != nil {
				t.Fatalf("failed to set initial CSR: %v", err)
			}

			// CSRRCI rd, csr, zimm
			insn := encodeCSRRCI(tc.rd, tc.csr, tc.zimm)
			cpu.Mem.Write32(cpu.PC, insn)

			err := cpu.Step()
			if err != nil {
				t.Fatalf("Step failed: %v", err)
			}

			// Check rd got old CSR value
			if cpu.GetReg(tc.rd) != tc.expectedRd {
				t.Errorf("rd = 0x%x, want 0x%x", cpu.GetReg(tc.rd), tc.expectedRd)
			}

			// Check CSR has correct value
			csrVal, _ := cpu.ReadCSR(tc.csr)
			if csrVal != tc.expectedCSR {
				t.Errorf("CSR = 0x%x, want 0x%x", csrVal, tc.expectedCSR)
			}
		})
	}
}

// TestCSRPrivilegeCheck tests CSR privilege level checking
func TestCSRPrivilegeCheck(t *testing.T) {
	cpu := csrTestCPU(t)

	// Try to access M-mode CSR from S-mode
	cpu.Reset()
	cpu.PC = 0x80000000
	cpu.Priv = PrivSupervisor

	// CSRRS x1, mstatus, x0 - should raise illegal instruction
	insn := encodeCSRRS(1, 0, CSRMstatus)
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should have raised an exception
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("expected illegal instruction exception, got mcause=0x%x", cpu.Mcause)
	}
}

// TestCSRReadOnlyCheck tests that read-only CSRs cannot be written
func TestCSRReadOnlyCheck(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// mhartid is read-only (address 0xF14, bits 11:10 = 11)
	// CSRRW x1, mhartid, x2 - should raise illegal instruction
	cpu.SetReg(2, 0x1234)
	insn := encodeCSRRW(1, 2, CSRMhartid)
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Should have raised an exception
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("expected illegal instruction exception, got mcause=0x%x", cpu.Mcause)
	}
}

// TestCSRMstatus tests mstatus field handling
func TestCSRMstatus(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Write to mstatus and verify field masking
	cpu.SetReg(4, 0x8|0x80) // MIE and MPIE
	insn := encodeCSRRW(1, 4, CSRMstatus)
	cpu.Mem.Write32(cpu.PC, insn)

	cpu.Step()

	// Read back mstatus
	mstatus, _ := cpu.ReadCSR(CSRMstatus)

	// MIE should be set (bit 3)
	if mstatus&MstatusMIE == 0 {
		t.Error("MIE should be set")
	}

	// MPIE should be set (bit 7)
	if mstatus&MstatusMPIE == 0 {
		t.Error("MPIE should be set")
	}
}

// TestCSRFPStatus tests floating-point status CSRs
func TestCSRFPStatus(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Enable FP by setting FS to Initial (1) or higher
	// Reference: riscv_cpu.c:714-729 - FP CSRs require FS != 0
	cpu.FS = FSInitial
	cpu.Mstatus |= uint64(FSInitial) << MstatusFSShift

	// Write to fflags
	cpu.SetReg(4, 0x1F) // All flags set
	insn := encodeCSRRW(1, 4, CSRFflags)
	cpu.Mem.Write32(cpu.PC, insn)
	cpu.Step()

	// Check fflags (masked to 5 bits)
	if cpu.FFlags != 0x1F {
		t.Errorf("fflags = 0x%x, want 0x1F", cpu.FFlags)
	}

	// Write to frm
	cpu.SetReg(4, 0x4) // RMM rounding mode
	insn = encodeCSRRW(2, 4, CSRFrm)
	cpu.Mem.Write32(cpu.PC, insn)
	cpu.Step()

	// Check frm (masked to 3 bits)
	if cpu.FRM != 0x4 {
		t.Errorf("frm = 0x%x, want 0x4", cpu.FRM)
	}

	// Read fcsr (combines frm and fflags)
	insn = encodeCSRRS(3, 0, CSRFcsr)
	cpu.Mem.Write32(cpu.PC, insn)
	cpu.Step()

	expected := uint64(0x4<<5 | 0x1F)
	if cpu.GetReg(3) != expected {
		t.Errorf("x3 (fcsr) = 0x%x, want 0x%x", cpu.GetReg(3), expected)
	}
}

// TestCSRSupervisorMode tests S-mode CSR access
func TestCSRSupervisorMode(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	// S-mode should be able to access sstatus
	cpu.SetReg(4, MstatusSIE) // Set SIE
	insn := encodeCSRRW(1, 4, CSRSstatus)
	cpu.Mem.Write32(cpu.PC, insn)

	err := cpu.Step()
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	// SIE should be set in mstatus (sstatus is a view of mstatus)
	if cpu.Mstatus&MstatusSIE == 0 {
		t.Error("SIE should be set")
	}
}

// TestCSRCounters tests counter CSRs
func TestCSRCounters(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.InsnCounter = 12345

	// Read cycle counter
	insn := encodeCSRRS(1, 0, CSRCycle)
	cpu.Mem.Write32(cpu.PC, insn)
	cpu.Step()

	if cpu.GetReg(1) != 12345 {
		t.Errorf("cycle counter = %d, want 12345", cpu.GetReg(1))
	}

	// Read instret counter
	insn = encodeCSRRS(2, 0, CSRInstret)
	cpu.Mem.Write32(cpu.PC, insn)
	cpu.Step()

	// After one more instruction, counter should be 12346
	if cpu.GetReg(2) != 12346 {
		t.Errorf("instret counter = %d, want 12346", cpu.GetReg(2))
	}
}

// TestReadCSRDirect tests the ReadCSR function directly
func TestReadCSRDirect(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	tests := []struct {
		name     string
		csr      uint32
		setup    func()
		expected uint64
	}{
		{"mstatus", CSRMstatus, func() { cpu.Mstatus = 0x1234 }, 0x1234},
		{"mtvec", CSRMtvec, func() { cpu.Mtvec = 0x80000000 }, 0x80000000},
		{"mepc", CSRMepc, func() { cpu.Mepc = 0x80001000 }, 0x80001000},
		{"mcause", CSRMcause, func() { cpu.Mcause = 0xB }, 0xB},
		{"mtval", CSRMtval, func() { cpu.Mtval = 0xDEADBEEF }, 0xDEADBEEF},
		{"mscratch", CSRMscratch, func() { cpu.Mscratch = 0xCAFE }, 0xCAFE},
		{"mie", CSRMie, func() { cpu.Mie = 0x888 }, 0x888},
		{"mip", CSRMip, func() { cpu.Mip = 0x080 }, 0x080},
		{"mhartid", CSRMhartid, func() { cpu.Mhartid = 0 }, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.Priv = PrivMachine
			tc.setup()

			val, err := cpu.ReadCSR(tc.csr)
			if err != nil {
				t.Fatalf("ReadCSR failed: %v", err)
			}
			if val != tc.expected {
				t.Errorf("ReadCSR(%s) = 0x%x, want 0x%x", tc.name, val, tc.expected)
			}
		})
	}
}

// TestWriteCSRDirect tests the WriteCSR function directly
func TestWriteCSRDirect(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	tests := []struct {
		name   string
		csr    uint32
		value  uint64
		verify func() uint64
	}{
		{"mtvec", CSRMtvec, 0x80000000, func() uint64 { return cpu.Mtvec }},
		{"mepc", CSRMepc, 0x80001000, func() uint64 { return cpu.Mepc }},
		{"mscratch", CSRMscratch, 0xCAFEBABE, func() uint64 { return cpu.Mscratch }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Reset()
			cpu.Priv = PrivMachine

			err := cpu.WriteCSR(tc.csr, tc.value)
			if err != nil {
				t.Fatalf("WriteCSR failed: %v", err)
			}

			got := tc.verify()
			if got != tc.value {
				t.Errorf("WriteCSR(%s, 0x%x) resulted in 0x%x", tc.name, tc.value, got)
			}
		})
	}
}

// TestCSRUnknown tests access to unknown CSR
func TestCSRUnknown(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Try to read unknown CSR (0x350 is not defined but is M-mode writable)
	_, err := cpu.ReadCSR(0x350)
	if err != ErrCSRNotFound {
		t.Errorf("expected ErrCSRNotFound, got %v", err)
	}

	// Try to write unknown CSR
	err = cpu.WriteCSR(0x350, 0)
	if err != ErrCSRNotFound {
		t.Errorf("expected ErrCSRNotFound, got %v", err)
	}
}

// TestWriteSatp tests SATP register write with TLB flush
func TestWriteSatp(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Set TLB entries
	cpu.TLBRead[0].VAddr = 0x1000
	cpu.TLBWrite[0].VAddr = 0x2000
	cpu.TLBCode[0].VAddr = 0x3000

	// Write to SATP with mode change (from Bare to Sv39)
	// Sv39 mode = 8 in bits 63:60
	satpVal := uint64(8)<<60 | 0x12345
	cpu.WriteCSR(CSRSatp, satpVal)

	// TLB should be flushed due to mode change
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Error("TLB should be flushed on SATP mode change")
	}

	// Verify SATP was written
	val, _ := cpu.ReadCSR(CSRSatp)
	if val != satpVal {
		t.Errorf("SATP = 0x%x, want 0x%x", val, satpVal)
	}
}

// TestWriteSatpSameMode tests SATP write without mode change doesn't flush TLB
func TestWriteSatpSameValueNoFlush(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Set initial SATP with Sv39 mode
	satpVal := uint64(8)<<60 | 0x12345
	cpu.WriteCSR(CSRSatp, satpVal)

	// Set a live TLB entry after the initial write
	cpu.TLBRead[0].VAddr = 0x1000

	// Rewrite the *identical* SATP value: no translation change, no flush.
	// (A PPN or mode change does flush — see TestSatpPPNChangeFlushesTLB.)
	cpu.WriteCSR(CSRSatp, satpVal)

	if cpu.TLBRead[0].VAddr == ^uint64(0) {
		t.Error("TLB should NOT be flushed when SATP is rewritten with the same value")
	}
}

// TestWriteCSRSupervisor tests S-mode CSR writes
func TestWriteCSRSupervisor(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Write to stvec
	cpu.WriteCSR(CSRStvec, 0x80010000)
	if cpu.Stvec != 0x80010000 {
		t.Errorf("stvec = 0x%x, want 0x80010000", cpu.Stvec)
	}

	// Write to sscratch
	cpu.WriteCSR(CSRSscratch, 0xDEADBEEF)
	if cpu.Sscratch != 0xDEADBEEF {
		t.Errorf("sscratch = 0x%x, want 0xDEADBEEF", cpu.Sscratch)
	}

	// Write to sepc (must be aligned, low bit cleared)
	cpu.WriteCSR(CSRSepc, 0x80001001)
	if cpu.Sepc != 0x80001000 {
		t.Errorf("sepc = 0x%x, want 0x80001000 (aligned)", cpu.Sepc)
	}

	// Write to scause
	cpu.WriteCSR(CSRScause, 0xB)
	if cpu.Scause != 0xB {
		t.Errorf("scause = 0x%x, want 0xB", cpu.Scause)
	}

	// Write to stval
	cpu.WriteCSR(CSRStval, 0x12345678)
	if cpu.Stval != 0x12345678 {
		t.Errorf("stval = 0x%x, want 0x12345678", cpu.Stval)
	}
}

// TestWriteCSRMachine tests M-mode CSR writes
func TestWriteCSRMachine(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Write to mtvec
	cpu.WriteCSR(CSRMtvec, 0x80010000)
	if cpu.Mtvec != 0x80010000 {
		t.Errorf("mtvec = 0x%x, want 0x80010000", cpu.Mtvec)
	}

	// Write to mscratch
	cpu.WriteCSR(CSRMscratch, 0xCAFEBABE)
	if cpu.Mscratch != 0xCAFEBABE {
		t.Errorf("mscratch = 0x%x, want 0xCAFEBABE", cpu.Mscratch)
	}

	// Write to mepc (must be aligned)
	cpu.WriteCSR(CSRMepc, 0x80001003)
	if cpu.Mepc != 0x80001002 {
		t.Errorf("mepc = 0x%x, want 0x80001002 (aligned)", cpu.Mepc)
	}

	// Write to mcause
	cpu.WriteCSR(CSRMcause, 0xB)
	if cpu.Mcause != 0xB {
		t.Errorf("mcause = 0x%x, want 0xB", cpu.Mcause)
	}

	// Write to mtval
	cpu.WriteCSR(CSRMtval, 0xABCDEF01)
	if cpu.Mtval != 0xABCDEF01 {
		t.Errorf("mtval = 0x%x, want 0xABCDEF01", cpu.Mtval)
	}

	// Write to medeleg (masked to 16 bits)
	// Reference: riscv_cpu.c:978-980 - C TinyEMU allows all 16 low bits
	cpu.WriteCSR(CSRMedeleg, 0xFFFFFFFF)
	if cpu.Medeleg != 0xFFFF {
		t.Errorf("medeleg = 0x%x, want 0xFFFF (masked)", cpu.Medeleg)
	}

	// Write to mideleg (masked)
	cpu.WriteCSR(CSRMideleg, 0xFFFFFFFF)
	if cpu.Mideleg != 0x222 {
		t.Errorf("mideleg = 0x%x, want 0x222 (masked)", cpu.Mideleg)
	}

	// Write to mie (masked)
	cpu.WriteCSR(CSRMie, 0xFFFFFFFF)
	if cpu.Mie != 0xAAA {
		t.Errorf("mie = 0x%x, want 0xAAA (masked)", cpu.Mie)
	}

	// Write to mcounteren (masked)
	cpu.WriteCSR(CSRMcounteren, 0xFFFFFFFF)
	if cpu.Mcounteren != 0x7 {
		t.Errorf("mcounteren = 0x%x, want 0x7 (masked)", cpu.Mcounteren)
	}
}

// TestWriteCSRSie tests writing SIE through delegation
func TestWriteCSRSie(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	// First, delegate some interrupts to S-mode
	cpu.Priv = PrivMachine
	cpu.WriteCSR(CSRMideleg, 0x222) // Delegate S-mode interrupts
	cpu.Priv = PrivSupervisor

	// Write to sie (only delegated bits can be written)
	cpu.WriteCSR(CSRSie, 0xFFF)
	expected := uint32(0x222) // Only delegated bits
	if cpu.Mie != expected {
		t.Errorf("mie = 0x%x, want 0x%x", cpu.Mie, expected)
	}
}

// TestWriteCSRSip tests writing SIP
func TestWriteCSRSip(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Delegate SSIP to S-mode
	cpu.Priv = PrivMachine
	cpu.WriteCSR(CSRMideleg, uint64(MipSSIP))
	cpu.Priv = PrivSupervisor

	// Write to sip (only SSIP can be written)
	cpu.WriteCSR(CSRSip, 0xFFF)
	expected := uint32(MipSSIP) // Only SSIP
	if cpu.Mip != expected {
		t.Errorf("mip = 0x%x, want 0x%x", cpu.Mip, expected)
	}
}

// TestWriteCSRMip tests writing MIP
func TestWriteCSRMip(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Write to mip (only SSIP and STIP can be written by M-mode software)
	// MSIP is controlled by CLINT memory-mapped register, not CSR write.
	// Reference: riscv_cpu.c line 1009
	cpu.WriteCSR(CSRMip, 0xFFF)
	expected := uint32(MipSSIP | MipSTIP)
	if cpu.Mip != expected {
		t.Errorf("mip = 0x%x, want 0x%x", cpu.Mip, expected)
	}
}

// TestWriteCSRScounteren tests writing scounteren
func TestWriteCSRScounteren(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	cpu.WriteCSR(CSRScounteren, 0xFFFFFFFF)
	if cpu.Scounteren != 0x7 {
		t.Errorf("scounteren = 0x%x, want 0x7 (masked)", cpu.Scounteren)
	}
}

// TestReadCSRTime tests reading the TIME CSR
func TestReadCSRTime(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.InsnCounter = 160 // 160 / 16 = 10

	val, err := cpu.ReadCSR(CSRTime)
	if err != nil {
		t.Fatalf("ReadCSR(time) failed: %v", err)
	}
	if val != 10 {
		t.Errorf("time = %d, want 10", val)
	}
}

// TestReadCSRMcycle tests reading mcycle
func TestReadCSRMcycle(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.InsnCounter = 12345

	val, err := cpu.ReadCSR(CSRMcycle)
	if err != nil {
		t.Fatalf("ReadCSR(mcycle) failed: %v", err)
	}
	if val != 12345 {
		t.Errorf("mcycle = %d, want 12345", val)
	}
}

// TestReadCSRMinstret tests reading minstret
func TestReadCSRMinstret(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.InsnCounter = 67890

	val, err := cpu.ReadCSR(CSRMinstret)
	if err != nil {
		t.Fatalf("ReadCSR(minstret) failed: %v", err)
	}
	if val != 67890 {
		t.Errorf("minstret = %d, want 67890", val)
	}
}

// TestReadCSRSupervisor tests reading S-mode CSRs
func TestReadCSRSupervisor(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Setup values
	cpu.Mie = 0x222
	cpu.Mideleg = 0x222
	cpu.Mip = 0x222
	cpu.Stvec = 0x80010000
	cpu.Scounteren = 0x5
	cpu.Sscratch = 0xABCD
	cpu.Sepc = 0x80001000
	cpu.Scause = 0xB
	cpu.Stval = 0x1234
	cpu.Satp = 0x8000000012345

	// Test sie (only delegated bits)
	val, _ := cpu.ReadCSR(CSRSie)
	if val != 0x222 {
		t.Errorf("sie = 0x%x, want 0x222", val)
	}

	// Test sip (only delegated bits)
	val, _ = cpu.ReadCSR(CSRSip)
	if val != 0x222 {
		t.Errorf("sip = 0x%x, want 0x222", val)
	}

	// Test stvec
	val, _ = cpu.ReadCSR(CSRStvec)
	if val != 0x80010000 {
		t.Errorf("stvec = 0x%x, want 0x80010000", val)
	}

	// Test scounteren
	val, _ = cpu.ReadCSR(CSRScounteren)
	if val != 0x5 {
		t.Errorf("scounteren = 0x%x, want 0x5", val)
	}

	// Test sscratch
	val, _ = cpu.ReadCSR(CSRSscratch)
	if val != 0xABCD {
		t.Errorf("sscratch = 0x%x, want 0xABCD", val)
	}

	// Test sepc
	val, _ = cpu.ReadCSR(CSRSepc)
	if val != 0x80001000 {
		t.Errorf("sepc = 0x%x, want 0x80001000", val)
	}

	// Test scause
	val, _ = cpu.ReadCSR(CSRScause)
	if val != 0xB {
		t.Errorf("scause = 0x%x, want 0xB", val)
	}

	// Test stval
	val, _ = cpu.ReadCSR(CSRStval)
	if val != 0x1234 {
		t.Errorf("stval = 0x%x, want 0x1234", val)
	}

	// Test satp
	val, _ = cpu.ReadCSR(CSRSatp)
	if val != 0x8000000012345 {
		t.Errorf("satp = 0x%x, want 0x8000000012345", val)
	}
}

// TestReadCSRMachine tests reading M-mode CSRs
func TestReadCSRMachine(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Setup values
	cpu.Medeleg = 0x100
	cpu.Mideleg = 0x200
	cpu.Mie = 0xAAA
	cpu.Mip = 0x080
	cpu.Mcounteren = 0x7

	// Test medeleg
	val, _ := cpu.ReadCSR(CSRMedeleg)
	if val != 0x100 {
		t.Errorf("medeleg = 0x%x, want 0x100", val)
	}

	// Test mideleg
	val, _ = cpu.ReadCSR(CSRMideleg)
	if val != 0x200 {
		t.Errorf("mideleg = 0x%x, want 0x200", val)
	}

	// Test mie
	val, _ = cpu.ReadCSR(CSRMie)
	if val != 0xAAA {
		t.Errorf("mie = 0x%x, want 0xAAA", val)
	}

	// Test mip
	val, _ = cpu.ReadCSR(CSRMip)
	if val != 0x080 {
		t.Errorf("mip = 0x%x, want 0x080", val)
	}

	// Test mcounteren
	val, _ = cpu.ReadCSR(CSRMcounteren)
	if val != 0x7 {
		t.Errorf("mcounteren = 0x%x, want 0x7", val)
	}
}

// TestWriteCSRFcsr tests writing FCSR (combined FRM and FFLAGS)
func TestWriteCSRFcsr(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Enable FP by setting FS to Initial (1) or higher
	// Reference: riscv_cpu.c:714-729 - FP CSRs require FS != 0
	cpu.FS = FSInitial
	cpu.Mstatus |= uint64(FSInitial) << MstatusFSShift

	// Write FCSR with FRM=4 and FFLAGS=0x1F
	cpu.WriteCSR(CSRFcsr, (4<<5)|0x1F)

	if cpu.FRM != 4 {
		t.Errorf("FRM = %d, want 4", cpu.FRM)
	}
	if cpu.FFlags != 0x1F {
		t.Errorf("FFlags = 0x%x, want 0x1F", cpu.FFlags)
	}
}

// TestWriteCSRFrmClamping tests that FRM values >= 5 are clamped to 0
// Reference: riscv_cpu.c:860-865 (set_frm clamps invalid values)
func TestWriteCSRFrmClamping(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.FS = FSInitial // Enable FP

	// Write FRM with invalid value 5 - should be clamped to 0
	cpu.WriteCSR(CSRFrm, 5)
	if cpu.FRM != 0 {
		t.Errorf("FRM with val=5: got %d, want 0 (clamped)", cpu.FRM)
	}

	// Write FRM with invalid value 6 - should be clamped to 0
	cpu.WriteCSR(CSRFrm, 6)
	if cpu.FRM != 0 {
		t.Errorf("FRM with val=6: got %d, want 0 (clamped)", cpu.FRM)
	}

	// Write FRM with invalid value 7 - should be clamped to 0
	cpu.WriteCSR(CSRFrm, 7)
	if cpu.FRM != 0 {
		t.Errorf("FRM with val=7: got %d, want 0 (clamped)", cpu.FRM)
	}

	// Write FRM with valid value 4 - should stay as 4
	cpu.WriteCSR(CSRFrm, 4)
	if cpu.FRM != 4 {
		t.Errorf("FRM with val=4: got %d, want 4", cpu.FRM)
	}
}

// TestFPCSRDisabledAccess tests that FP CSR access fails when FS==0
// Reference: riscv_cpu.c:714-729 (FP CSRs require FS != 0)
func TestFPCSRDisabledAccess(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.FS = FSOff // Disable FP

	// Read fflags - should fail
	_, err := cpu.ReadCSR(CSRFflags)
	if err == nil {
		t.Error("ReadCSR(fflags) with FS=0 should return error")
	}

	// Read frm - should fail
	_, err = cpu.ReadCSR(CSRFrm)
	if err == nil {
		t.Error("ReadCSR(frm) with FS=0 should return error")
	}

	// Read fcsr - should fail
	_, err = cpu.ReadCSR(CSRFcsr)
	if err == nil {
		t.Error("ReadCSR(fcsr) with FS=0 should return error")
	}

	// Write fflags - should fail
	err = cpu.WriteCSR(CSRFflags, 0x1F)
	if err == nil {
		t.Error("WriteCSR(fflags) with FS=0 should return error")
	}

	// Write frm - should fail
	err = cpu.WriteCSR(CSRFrm, 4)
	if err == nil {
		t.Error("WriteCSR(frm) with FS=0 should return error")
	}

	// Write fcsr - should fail
	err = cpu.WriteCSR(CSRFcsr, 0x9F)
	if err == nil {
		t.Error("WriteCSR(fcsr) with FS=0 should return error")
	}
}

// TestMstatusSDbit tests that the SD bit is computed dynamically
// Reference: riscv_cpu.c:651-662 (get_mstatus computes SD bit)
func TestMstatusSDbit(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.CurXLEN = XLEN64
	cpu.MaxXLEN = XLEN64

	// Clear mstatus and FS
	cpu.Mstatus = 0
	cpu.FS = FSOff

	// Read mstatus with FS=0 - SD bit should be 0
	val, _ := cpu.ReadCSR(CSRMstatus)
	sdBit := val >> 63
	if sdBit != 0 {
		t.Errorf("SD bit with FS=0: got %d, want 0", sdBit)
	}

	// Set FS to Dirty (3)
	cpu.FS = FSDirty

	// Read mstatus - SD bit should be 1
	val, _ = cpu.ReadCSR(CSRMstatus)
	sdBit = val >> 63
	if sdBit != 1 {
		t.Errorf("SD bit with FS=Dirty: got %d, want 1", sdBit)
	}

	// Set FS to Clean (2)
	cpu.FS = FSClean

	// Read mstatus - SD bit should be 0
	val, _ = cpu.ReadCSR(CSRMstatus)
	sdBit = val >> 63
	if sdBit != 0 {
		t.Errorf("SD bit with FS=Clean: got %d, want 0", sdBit)
	}
}

// TestMstatusTLBFlush tests that TLB is flushed on MPRV/SUM/MXR changes
// Reference: riscv_cpu.c:678-683 (set_mstatus flushes TLB on MMU config change)
func TestMstatusTLBFlush(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Set up a TLB entry
	cpu.TLBRead[0].VAddr = 0x1000
	cpu.TLBRead[0].MemAddend = 0x2000

	// Write mstatus with MPRV change - should flush TLB
	cpu.WriteCSR(CSRMstatus, MstatusMPRV)
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Error("TLB not flushed on MPRV change")
	}

	// Set up TLB entry again
	cpu.TLBRead[0].VAddr = 0x1000

	// Write mstatus with SUM change - should flush TLB
	cpu.Mstatus = MstatusMPRV // Keep MPRV
	cpu.WriteCSR(CSRMstatus, MstatusMPRV|MstatusSUM)
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Error("TLB not flushed on SUM change")
	}

	// Set up TLB entry again
	cpu.TLBRead[0].VAddr = 0x1000

	// Write mstatus with MXR change - should flush TLB
	cpu.Mstatus = MstatusMPRV | MstatusSUM
	cpu.WriteCSR(CSRMstatus, MstatusMPRV|MstatusSUM|MstatusMXR)
	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Error("TLB not flushed on MXR change")
	}
}

// TestReadCSRHighCountersRV64 tests that high counter CSRs return error for RV64
// Reference: riscv_cpu.c:746-761, 835-840 - high counters only valid for cur_xlen=32
func TestReadCSRHighCountersRV64(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.CurXLEN = XLEN64
	cpu.InsnCounter = 0x123456789ABCDEF0

	// cycleh should fail for RV64
	_, err := cpu.ReadCSR(CSRCycleh)
	if err != ErrCSRNotFound {
		t.Errorf("ReadCSR(cycleh) on RV64 should fail, got %v", err)
	}

	// instreth should fail for RV64
	_, err = cpu.ReadCSR(CSRInstreth)
	if err != ErrCSRNotFound {
		t.Errorf("ReadCSR(instreth) on RV64 should fail, got %v", err)
	}

	// mcycleh should fail for RV64
	_, err = cpu.ReadCSR(CSRMcycleh)
	if err != ErrCSRNotFound {
		t.Errorf("ReadCSR(mcycleh) on RV64 should fail, got %v", err)
	}

	// minstreth should fail for RV64
	_, err = cpu.ReadCSR(CSRMinstreth)
	if err != ErrCSRNotFound {
		t.Errorf("ReadCSR(minstreth) on RV64 should fail, got %v", err)
	}
}

// TestReadCSRHighCountersRV32 tests that high counter CSRs return high bits for RV32
// Reference: riscv_cpu.c:746-761, 835-840 - returns insn_counter >> 32
func TestReadCSRHighCountersRV32(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	_, err := m.RegisterRAM(0x80000000, 1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN32)
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	cpu.CurXLEN = XLEN32
	cpu.InsnCounter = 0x123456789ABCDEF0

	// cycleh should return high 32 bits
	val, err := cpu.ReadCSR(CSRCycleh)
	if err != nil {
		t.Fatalf("ReadCSR(cycleh) failed: %v", err)
	}
	expected := uint64(0x12345678)
	if val != expected {
		t.Errorf("cycleh = 0x%x, want 0x%x", val, expected)
	}

	// instreth should return high 32 bits
	val, err = cpu.ReadCSR(CSRInstreth)
	if err != nil {
		t.Fatalf("ReadCSR(instreth) failed: %v", err)
	}
	if val != expected {
		t.Errorf("instreth = 0x%x, want 0x%x", val, expected)
	}

	// mcycleh should return high 32 bits
	val, err = cpu.ReadCSR(CSRMcycleh)
	if err != nil {
		t.Fatalf("ReadCSR(mcycleh) failed: %v", err)
	}
	if val != expected {
		t.Errorf("mcycleh = 0x%x, want 0x%x", val, expected)
	}

	// minstreth should return high 32 bits
	val, err = cpu.ReadCSR(CSRMinstreth)
	if err != nil {
		t.Fatalf("ReadCSR(minstreth) failed: %v", err)
	}
	if val != expected {
		t.Errorf("minstreth = 0x%x, want 0x%x", val, expected)
	}
}

// TestReadCSRMisaWithMXL tests that MISA ORs in MXL based on cur_xlen
// Reference: riscv_cpu.c:797-800 - val |= (target_ulong)s->mxl << (s->cur_xlen - 2)
func TestReadCSRMisaWithMXL(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.CurXLEN = XLEN64
	cpu.MXL = MxlRV64

	// Set MISA to extensions only (no MXL)
	extensions := uint64(MisaI | MisaM | MisaA | MisaF | MisaD | MisaC | MisaS | MisaU)
	cpu.Misa = extensions

	// Read MISA - should have MXL OR'd in at position cur_xlen-2
	val, err := cpu.ReadCSR(CSRMisa)
	if err != nil {
		t.Fatalf("ReadCSR(misa) failed: %v", err)
	}

	// For RV64, MXL (=2) should be at bits 63:62, i.e., shifted by 62
	expectedMXL := uint64(MxlRV64) << 62
	if val&(3<<62) != expectedMXL {
		t.Errorf("MISA MXL bits = 0x%x, want 0x%x", val&(3<<62), expectedMXL)
	}

	// Extensions should still be present
	if val&extensions != extensions {
		t.Errorf("MISA extensions lost, got 0x%x, want 0x%x", val&extensions, extensions)
	}
}

// TestWriteMstatusMatchesC tests that writeMstatus mask matches C TinyEMU
// Reference: riscv_cpu.c:641-645, 686 - MSTATUS_MASK & ~MSTATUS_FS
func TestWriteMstatusMatchesC(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine

	// Try to write bits that C doesn't allow: TVM, TW, TSR, XS, HIE, HPIE, HPP
	// These should NOT be written per C MSTATUS_MASK
	cpu.Mstatus = 0
	cpu.FS = FSOff

	// Write with all bits set
	cpu.WriteCSR(CSRMstatus, 0xFFFFFFFFFFFFFFFF)

	// C MSTATUS_MASK allows: UIE|SIE|MIE|UPIE|SPIE|MPIE|SPP|MPP|FS|MPRV|SUM|MXR
	// Note: FS is stored separately in C, but we can check the mask was applied

	// TVM (bit 20), TW (bit 21), TSR (bit 22) should NOT be set (not in C mask)
	if cpu.Mstatus&MstatusTVM != 0 {
		t.Error("TVM bit should not be writable (not in C MSTATUS_MASK)")
	}
	if cpu.Mstatus&MstatusTW != 0 {
		t.Error("TW bit should not be writable (not in C MSTATUS_MASK)")
	}
	if cpu.Mstatus&MstatusTSR != 0 {
		t.Error("TSR bit should not be writable (not in C MSTATUS_MASK)")
	}

	// These bits SHOULD be set (in C MSTATUS_MASK)
	if cpu.Mstatus&MstatusMIE == 0 {
		t.Error("MIE bit should be writable")
	}
	if cpu.Mstatus&MstatusMPIE == 0 {
		t.Error("MPIE bit should be writable")
	}
	if cpu.Mstatus&MstatusMPRV == 0 {
		t.Error("MPRV bit should be writable")
	}
	if cpu.Mstatus&MstatusSUM == 0 {
		t.Error("SUM bit should be writable")
	}
	if cpu.Mstatus&MstatusMXR == 0 {
		t.Error("MXR bit should be writable")
	}
}

// TestWriteMstatusFSStoredSeparately tests that FS is stored in c.FS
// Reference: riscv_cpu.c:684 - s->fs = (val >> MSTATUS_FS_SHIFT) & 3
func TestWriteMstatusFSStoredSeparately(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Mstatus = 0
	cpu.FS = FSOff

	// Write FS=Dirty (3) to mstatus
	cpu.WriteCSR(CSRMstatus, uint64(FSDirty)<<MstatusFSShift)

	// c.FS should be updated
	if cpu.FS != FSDirty {
		t.Errorf("c.FS = %d, want %d (Dirty)", cpu.FS, FSDirty)
	}

	// Reading mstatus via getMstatus should show FS correctly
	val, _ := cpu.ReadCSR(CSRMstatus)
	fsInMstatus := (val >> MstatusFSShift) & 3
	if fsInMstatus != FSDirty {
		t.Errorf("FS in mstatus read = %d, want %d", fsInMstatus, FSDirty)
	}
}

// TestSatpPPNChangeFlushesTLB: writing satp with a new root page-table PPN
// but the same translation mode (e.g. a context switch between two Sv39
// processes) must flush the TLB, otherwise stale entries from the previous
// address space survive.
func TestSatpPPNChangeFlushesTLB(t *testing.T) {
	cpu := csrTestCPU(t)
	mode := uint64(SatpModeSv39) << 60

	// Establish an initial Sv39 mapping, then seed a live TLB entry.
	cpu.writeSatp(mode | 0x100)
	cpu.TLBRead[0].VAddr = 0x1000

	// Switch to a different root page table — same Sv39 mode, new PPN.
	cpu.writeSatp(mode | 0x200)

	if cpu.TLBRead[0].VAddr != ^uint64(0) {
		t.Errorf("satp PPN change did not flush the TLB (TLBRead[0].VAddr = %#x, want invalid)", cpu.TLBRead[0].VAddr)
	}
}

// TestTimeCSRRespectsCounteren: reading the time CSR below M-mode requires
// the TM bit in the relevant counteren, like cycle/instret (2.3.2).
func TestTimeCSRRespectsCounteren(t *testing.T) {
	cpu := csrTestCPU(t)
	cpu.Priv = PrivSupervisor

	// mcounteren.TM (bit 1) clear -> S-mode time read is denied.
	cpu.Mcounteren = 0
	if _, err := cpu.ReadCSR(CSRTime); err != ErrCSRPrivilege {
		t.Errorf("time read without mcounteren.TM: err = %v, want ErrCSRPrivilege", err)
	}

	// TM set -> allowed.
	cpu.Mcounteren = 1 << 1
	if _, err := cpu.ReadCSR(CSRTime); err != nil {
		t.Errorf("time read with mcounteren.TM set: err = %v, want nil", err)
	}
}

// TestWriteSatpRV32MasksAndExtractsMode: on RV32, satp must be masked to 32
// bits and its mode read from bit 31, so an Sv32 value arriving sign-extended
// from a 32-bit register is accepted (2.3.7).
func TestWriteSatpRV32MasksAndExtractsMode(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	t.Cleanup(m.Close)
	if _, err := m.RegisterRAM(0x80000000, 1024*1024, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	cpu := NewCPU(m, XLEN32)

	// Sv32 satp (MODE bit 31 set) as it would arrive sign-extended from a
	// 32-bit source register.
	cpu.writeSatp(0xFFFFFFFF80000000)

	if cpu.Satp != 0x80000000 {
		t.Errorf("Satp = %#x, want 0x80000000 (masked to 32 bits)", cpu.Satp)
	}
	if cpu.GetSatpMode() != SatpModeSv32 {
		t.Errorf("GetSatpMode = %d, want Sv32 (%d)", cpu.GetSatpMode(), SatpModeSv32)
	}
}
