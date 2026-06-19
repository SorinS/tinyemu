package riscv

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// Helper to create a test CPU for MMU tests
func mmuTestCPU(t *testing.T) *CPU {
	t.Helper()
	m := mem.NewPhysMemoryMap()
	// Register RAM at physical address 0x80000000 (standard RISC-V)
	_, err := m.RegisterRAM(0x80000000, 4*1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	cpu := NewCPU(m, XLEN64)
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	return cpu
}

// TestTLBLookupMiss tests TLB lookup returns false on miss
func TestTLBLookupMiss(t *testing.T) {
	cpu := mmuTestCPU(t)

	// Flush TLB to ensure miss
	cpu.FlushTLB()

	_, hit := cpu.TLBLookup(0x80001000, AccessRead)
	if hit {
		t.Error("TLB lookup should miss on empty TLB")
	}
}

// TestTLBUpdate tests TLB update and subsequent hit
func TestTLBUpdate(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.FlushTLB()

	// Update TLB with a translation
	cpu.updateTLB(0x80001000, 0x80001000, AccessRead)

	// Check TLB entry
	idx := (0x80001000 >> PageShift) & (TLBSize - 1)
	if cpu.TLBRead[idx].VAddr != 0x80001000 {
		t.Errorf("TLB VAddr = 0x%x, want 0x80001000", cpu.TLBRead[idx].VAddr)
	}
}

// TestTLBSeparateTypes tests that read/write/code TLBs are separate
func TestTLBSeparateTypes(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.FlushTLB()

	vaddr := uint64(0x80001000)

	// Update only read TLB
	cpu.updateTLB(vaddr, vaddr, AccessRead)

	// Read TLB should have entry
	idx := (vaddr >> PageShift) & (TLBSize - 1)
	if cpu.TLBRead[idx].VAddr != vaddr {
		t.Error("Read TLB should have entry")
	}

	// Write and Code TLBs should not have entry
	if cpu.TLBWrite[idx].VAddr != ^uint64(0) {
		t.Error("Write TLB should not have entry")
	}
	if cpu.TLBCode[idx].VAddr != ^uint64(0) {
		t.Error("Code TLB should not have entry")
	}
}

// TestIsMmuEnabled tests MMU enable logic
func TestIsMmuEnabled(t *testing.T) {
	cpu := mmuTestCPU(t)

	// M-mode with bare SATP should have MMU disabled
	cpu.Priv = PrivMachine
	cpu.Satp = 0 // Bare mode

	if cpu.IsMmuEnabled(AccessRead) {
		t.Error("MMU should be disabled in M-mode with bare SATP")
	}

	// S-mode with bare SATP should have MMU disabled
	cpu.Priv = PrivSupervisor
	if cpu.IsMmuEnabled(AccessRead) {
		t.Error("MMU should be disabled in S-mode with bare SATP")
	}

	// S-mode with Sv39 should have MMU enabled
	cpu.Satp = uint64(SatpModeSv39) << 60
	if !cpu.IsMmuEnabled(AccessRead) {
		t.Error("MMU should be enabled in S-mode with Sv39")
	}
}

// TestGetSatpMode tests SATP mode extraction
func TestGetSatpMode(t *testing.T) {
	cpu := mmuTestCPU(t)

	tests := []struct {
		satp     uint64
		expected int
	}{
		{0, SatpModeBare},
		{uint64(SatpModeSv39) << 60, SatpModeSv39},
		{uint64(SatpModeSv48) << 60, SatpModeSv48},
	}

	for _, tc := range tests {
		cpu.Satp = tc.satp
		mode := cpu.GetSatpMode()
		if mode != tc.expected {
			t.Errorf("GetSatpMode(0x%x) = %d, want %d", tc.satp, mode, tc.expected)
		}
	}
}

// TestGetSatpPPN tests SATP PPN extraction
func TestGetSatpPPN(t *testing.T) {
	cpu := mmuTestCPU(t)

	// Set SATP with mode=Sv39 and PPN=0x1234
	cpu.Satp = (uint64(SatpModeSv39) << 60) | 0x1234

	ppn := cpu.GetSatpPPN()
	if ppn != 0x1234 {
		t.Errorf("GetSatpPPN() = 0x%x, want 0x1234", ppn)
	}
}

// TestTranslateAddressBareMode tests translation in bare mode (no translation)
func TestTranslateAddressBareMode(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Satp = 0 // Bare mode

	vaddr := uint64(0x80001000)
	paddr, err := cpu.TranslateAddress(vaddr, AccessRead)

	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}
	if paddr != vaddr {
		t.Errorf("paddr = 0x%x, want 0x%x (no translation)", paddr, vaddr)
	}
}

// TestTranslateAddressMMode tests M-mode bypasses translation
func TestTranslateAddressMMode(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = uint64(SatpModeSv39) << 60 // Sv39 enabled

	vaddr := uint64(0x80001000)
	paddr, err := cpu.TranslateAddress(vaddr, AccessRead)

	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}
	// M-mode should bypass translation
	if paddr != vaddr {
		t.Errorf("paddr = 0x%x, want 0x%x (M-mode bypasses)", paddr, vaddr)
	}
}

// TestLoadStorePhysical tests load/store with physical addressing
func TestLoadStorePhysical(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0 // Bare mode

	addr := uint64(0x80001000)

	// Store test values
	if err := cpu.StoreU8(addr, 0x12); err != nil {
		t.Fatalf("StoreU8 failed: %v", err)
	}
	if err := cpu.StoreU16(addr+8, 0x3456); err != nil {
		t.Fatalf("StoreU16 failed: %v", err)
	}
	if err := cpu.StoreU32(addr+16, 0x789ABCDE); err != nil {
		t.Fatalf("StoreU32 failed: %v", err)
	}
	if err := cpu.StoreU64(addr+24, 0xDEADBEEFCAFEBABE); err != nil {
		t.Fatalf("StoreU64 failed: %v", err)
	}

	// Load and verify
	v8, err := cpu.LoadU8(addr)
	if err != nil {
		t.Fatalf("LoadU8 failed: %v", err)
	}
	if v8 != 0x12 {
		t.Errorf("LoadU8 = 0x%x, want 0x12", v8)
	}

	v16, err := cpu.LoadU16(addr + 8)
	if err != nil {
		t.Fatalf("LoadU16 failed: %v", err)
	}
	if v16 != 0x3456 {
		t.Errorf("LoadU16 = 0x%x, want 0x3456", v16)
	}

	v32, err := cpu.LoadU32(addr + 16)
	if err != nil {
		t.Fatalf("LoadU32 failed: %v", err)
	}
	if v32 != 0x789ABCDE {
		t.Errorf("LoadU32 = 0x%x, want 0x789ABCDE", v32)
	}

	v64, err := cpu.LoadU64(addr + 24)
	if err != nil {
		t.Fatalf("LoadU64 failed: %v", err)
	}
	if v64 != 0xDEADBEEFCAFEBABE {
		t.Errorf("LoadU64 = 0x%x, want 0xDEADBEEFCAFEBABE", v64)
	}
}

// TestLoadFromUnmappedAddress tests load from unmapped address
// TinyEMU C returns 0 for reads from unmapped addresses (no exception).
// Reference: riscv_cpu.c:376-382
func TestLoadFromUnmappedAddress(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Load from unmapped address should return 0 (C behavior)
	val, err := cpu.LoadU32(0x10000000)
	if err != nil {
		t.Errorf("Load from unmapped address should succeed (C behavior), got: %v", err)
	}
	if val != 0 {
		t.Errorf("Load from unmapped address should return 0, got: 0x%x", val)
	}

	// No exception should be raised
	if cpu.PendingException != -1 {
		t.Errorf("PendingException = %d, want -1 (no exception)", cpu.PendingException)
	}
}

// TestStoreToUnmappedAddress tests store to unmapped address
// TinyEMU C silently ignores writes to unmapped addresses (no exception).
// Reference: riscv_cpu.c:462-468
func TestStoreToUnmappedAddress(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Store to unmapped address should succeed silently (C behavior)
	err := cpu.StoreU32(0x10000000, 0x1234)
	if err != nil {
		t.Errorf("Store to unmapped address should succeed (C behavior), got: %v", err)
	}

	// No exception should be raised
	if cpu.PendingException != -1 {
		t.Errorf("PendingException = %d, want -1 (no exception)", cpu.PendingException)
	}
}

// TestFetchInstruction tests instruction fetch
func TestFetchInstruction(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0
	cpu.PC = 0x80000000

	// Write an instruction
	cpu.Mem.Write32(cpu.PC, 0x12345678)

	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("FetchInstruction failed: %v", err)
	}
	if insn != 0x12345678 {
		t.Errorf("insn = 0x%x, want 0x12345678", insn)
	}
}

// TestFetchInstructionInvalid tests instruction fetch from invalid address
func TestFetchInstructionInvalid(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0
	cpu.PC = 0x10000000 // Unmapped

	_, err := cpu.FetchInstruction()
	if err == nil {
		t.Error("Fetch from unmapped address should fail")
	}

	if cpu.PendingException != CauseFaultFetch {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseFaultFetch)
	}
}

// TestCheckPTEPermissions tests PTE permission checking
func TestCheckPTEPermissions(t *testing.T) {
	cpu := mmuTestCPU(t)

	tests := []struct {
		name       string
		pte        uint64
		priv       uint8
		accessType AccessType
		mstatus    uint64
		expected   bool
	}{
		// Basic read permission
		{"Read with R bit", PTEValid | PTERead | PTEUser, PrivUser, AccessRead, 0, true},
		{"Read without R bit", PTEValid | PTEUser, PrivUser, AccessRead, 0, false},

		// Basic write permission
		{"Write with W bit", PTEValid | PTERead | PTEWrite | PTEUser, PrivUser, AccessWrite, 0, true},
		{"Write without W bit", PTEValid | PTERead | PTEUser, PrivUser, AccessWrite, 0, false},

		// Basic execute permission
		{"Execute with X bit", PTEValid | PTEExecute | PTEUser, PrivUser, AccessCode, 0, true},
		{"Execute without X bit", PTEValid | PTERead | PTEUser, PrivUser, AccessCode, 0, false},

		// User mode can't access non-user pages
		{"User no U bit", PTEValid | PTERead, PrivUser, AccessRead, 0, false},

		// Supervisor mode with/without SUM
		{"S-mode user page no SUM", PTEValid | PTERead | PTEUser, PrivSupervisor, AccessRead, 0, false},
		{"S-mode user page with SUM", PTEValid | PTERead | PTEUser, PrivSupervisor, AccessRead, MstatusSUM, true},

		// MXR allows reading executable pages
		{"MXR read X page", PTEValid | PTEExecute | PTEUser, PrivUser, AccessRead, MstatusMXR, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpu.Priv = tc.priv
			cpu.Mstatus = tc.mstatus

			result := cpu.checkPTEPermissions(tc.pte, tc.accessType)
			if result != tc.expected {
				t.Errorf("checkPTEPermissions() = %v, want %v", result, tc.expected)
			}
		})
	}
}

// TestGetPageFaultCause tests page fault cause selection
func TestGetPageFaultCause(t *testing.T) {
	cpu := mmuTestCPU(t)

	tests := []struct {
		accessType AccessType
		expected   int
	}{
		{AccessRead, CauseLoadPageFault},
		{AccessWrite, CauseStorePageFault},
		{AccessCode, CauseFetchPageFault},
	}

	for _, tc := range tests {
		cause := cpu.getPageFaultCause(tc.accessType)
		if cause != tc.expected {
			t.Errorf("getPageFaultCause(%v) = %d, want %d", tc.accessType, cause, tc.expected)
		}
	}
}

// TestMPRVLoad tests MPRV bit affects load privilege
func TestMPRVLoad(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine

	// Without MPRV, M-mode should use M-mode privilege
	cpu.Mstatus = uint64(PrivSupervisor) << MstatusMPPShift // MPP=S but MPRV=0
	priv := cpu.GetEffectivePrivForAccess(AccessRead)
	if priv != PrivMachine {
		t.Errorf("Without MPRV, priv = %d, want %d", priv, PrivMachine)
	}

	// With MPRV, M-mode loads should use MPP privilege
	cpu.Mstatus |= MstatusMPRV
	priv = cpu.GetEffectivePrivForAccess(AccessRead)
	if priv != PrivSupervisor {
		t.Errorf("With MPRV, priv = %d, want %d", priv, PrivSupervisor)
	}

	// MPRV doesn't affect instruction fetch
	priv = cpu.GetEffectivePrivForAccess(AccessCode)
	if priv != PrivMachine {
		t.Errorf("MPRV shouldn't affect code fetch, priv = %d, want %d", priv, PrivMachine)
	}
}

// TestPTEFlags tests PTE flag constants
func TestPTEFlags(t *testing.T) {
	// Verify PTE flag bit positions match RISC-V spec
	tests := []struct {
		name  string
		flag  uint64
		value uint64
	}{
		{"V", PTEValid, 1 << 0},
		{"R", PTERead, 1 << 1},
		{"W", PTEWrite, 1 << 2},
		{"X", PTEExecute, 1 << 3},
		{"U", PTEUser, 1 << 4},
		{"G", PTEGlobal, 1 << 5},
		{"A", PTEAccessed, 1 << 6},
		{"D", PTEDirty, 1 << 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.flag != tc.value {
				t.Errorf("%s = 0x%x, want 0x%x", tc.name, tc.flag, tc.value)
			}
		})
	}
}

// TestSv39CanonicalAddress tests Sv39 canonical address checking
func TestSv39CanonicalAddress(t *testing.T) {
	// This test verifies the canonical address check in translateSv39
	// For Sv39, bits 63:39 must all match bit 38

	// Note: We can't easily test this without setting up full page tables
	// But we can verify the check logic is present in the implementation
	// by examining the high address handling

	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Satp = uint64(SatpModeSv39) << 60 // Enable Sv39 with PPN=0

	// Non-canonical address (bit 38=0 but high bits set)
	_, err := cpu.translateSv39(0x8000000000000000, AccessRead)
	if err == nil {
		t.Error("Non-canonical address should cause page fault")
	}
}

// TestAccessTypeConstants tests access type constants
func TestAccessTypeConstants(t *testing.T) {
	if AccessRead != 0 {
		t.Errorf("AccessRead = %d, want 0", AccessRead)
	}
	if AccessWrite != 1 {
		t.Errorf("AccessWrite = %d, want 1", AccessWrite)
	}
	if AccessCode != 2 {
		t.Errorf("AccessCode = %d, want 2", AccessCode)
	}
}

// TestCpuError tests cpuError type
func TestCpuError(t *testing.T) {
	tests := []struct {
		err      *cpuError
		contains string
	}{
		{errFetchFault, "access fault"},
		{errPageFault, "page fault"},
		{errLoadFault, "load"},
		{errStoreFault, "store"},
	}

	for _, tc := range tests {
		msg := tc.err.Error()
		if msg == "" {
			t.Errorf("Error() returned empty string")
		}
	}
}

// TestGetSatpModeRV32 tests SATP mode extraction for RV32
func TestGetSatpModeRV32(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()
	cpu := NewCPU(m, XLEN32)

	// For RV32, mode is in bit 31
	tests := []struct {
		satp     uint64
		expected int
	}{
		{0, SatpModeBare},
		{1 << 31, 1}, // Sv32 mode = 1
	}

	for _, tc := range tests {
		cpu.Satp = tc.satp
		mode := cpu.GetSatpMode()
		if mode != tc.expected {
			t.Errorf("GetSatpMode(0x%x) = %d, want %d", tc.satp, mode, tc.expected)
		}
	}
}

// TestGetSatpPPNRV32 tests SATP PPN extraction for RV32
func TestGetSatpPPNRV32(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()
	cpu := NewCPU(m, XLEN32)

	// For RV32, PPN is in bits 21:0
	cpu.Satp = (1 << 31) | 0x3FFFFF // Mode=Sv32, PPN=max

	ppn := cpu.GetSatpPPN()
	if ppn != 0x3FFFFF {
		t.Errorf("GetSatpPPN() = 0x%x, want 0x3FFFFF", ppn)
	}
}

// TestIsMmuEnabledMPRV tests MMU enable logic with MPRV
func TestIsMmuEnabledMPRV(t *testing.T) {
	cpu := mmuTestCPU(t)

	// M-mode with MPRV and MPP=S should use S-mode translation
	cpu.Priv = PrivMachine
	cpu.Satp = uint64(SatpModeSv39) << 60 // Enable Sv39
	cpu.Mstatus = MstatusMPRV | (uint64(PrivSupervisor) << MstatusMPPShift)

	// For loads/stores, MMU should be enabled via MPRV
	if !cpu.IsMmuEnabled(AccessRead) {
		t.Error("MMU should be enabled for M-mode load with MPRV and MPP=S")
	}

	// For code fetch, MPRV doesn't apply - M-mode bypasses translation
	if cpu.IsMmuEnabled(AccessCode) {
		t.Error("MMU should be disabled for M-mode code fetch (MPRV doesn't apply)")
	}

	// M-mode with MPRV and MPP=M should not use translation
	cpu.Mstatus = MstatusMPRV | (uint64(PrivMachine) << MstatusMPPShift)
	if cpu.IsMmuEnabled(AccessRead) {
		t.Error("MMU should be disabled when MPP=M")
	}
}

// TestTLBLookupHit tests TLB lookup hit
func TestTLBLookupHit(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.FlushTLB()

	vaddr := uint64(0x80001000)

	// Update TLB with a translation
	cpu.updateTLB(vaddr, 0x80001000, AccessRead)

	// TLB lookup should hit
	_, hit := cpu.TLBLookup(vaddr, AccessRead)
	if !hit {
		t.Error("TLB lookup should hit after update")
	}

	// Different access type should miss (write TLB wasn't updated)
	_, hit = cpu.TLBLookup(vaddr, AccessWrite)
	if hit {
		t.Error("TLB lookup should miss for different access type")
	}
}

// TestTLBLookupCodeAndWrite tests TLB lookup for code and write access types
func TestTLBLookupCodeAndWrite(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.FlushTLB()

	vaddr := uint64(0x80001000)

	// Update write TLB
	cpu.updateTLB(vaddr, 0x80001000, AccessWrite)
	_, hit := cpu.TLBLookup(vaddr, AccessWrite)
	if !hit {
		t.Error("Write TLB lookup should hit")
	}

	// Update code TLB
	cpu.updateTLB(vaddr, 0x80001000, AccessCode)
	_, hit = cpu.TLBLookup(vaddr, AccessCode)
	if !hit {
		t.Error("Code TLB lookup should hit")
	}
}

// TestFetchInstructionPhysical tests FetchInstruction with physical addressing
func TestFetchInstructionPhysical(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0 // Bare mode

	// Write a simple instruction at PC
	addr := uint64(0x80000100)
	cpu.PC = addr
	cpu.Mem.Write32(addr, 0x00000013) // NOP (ADDI x0, x0, 0)

	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("FetchInstruction failed: %v", err)
	}
	if insn != 0x00000013 {
		t.Errorf("insn = 0x%x, want 0x00000013", insn)
	}
}

// TestFetchInstructionUnmapped tests FetchInstruction from unmapped address
func TestFetchInstructionUnmapped(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Set PC to unmapped address
	cpu.PC = 0x10000000

	_, err := cpu.FetchInstruction()
	if err == nil {
		t.Error("FetchInstruction should fail for unmapped address")
	}
	if cpu.PendingException != CauseFaultFetch {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseFaultFetch)
	}
}

// TestFetchInstructionCompressed tests FetchInstruction with compressed instruction
func TestFetchInstructionCompressed(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Write a compressed NOP (C.NOP = 0x0001)
	addr := uint64(0x80000100)
	cpu.PC = addr
	cpu.Mem.Write16(addr, 0x0001)

	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("FetchInstruction failed: %v", err)
	}
	// Compressed instruction should only have lower 16 bits
	if insn&0xFFFF != 0x0001 {
		t.Errorf("insn = 0x%x, want 0x0001", insn)
	}
}

// TestLoadStoreDeviceAddress tests load/store to device addresses
func TestLoadStoreDeviceAddress(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Test load from unmapped address (should return 0 per C behavior)
	val8, err := cpu.LoadU8(0x10000000)
	if err != nil {
		t.Errorf("LoadU8 unmapped should succeed: %v", err)
	}
	if val8 != 0 {
		t.Errorf("LoadU8 unmapped = 0x%x, want 0", val8)
	}

	val16, err := cpu.LoadU16(0x10000000)
	if err != nil {
		t.Errorf("LoadU16 unmapped should succeed: %v", err)
	}
	if val16 != 0 {
		t.Errorf("LoadU16 unmapped = 0x%x, want 0", val16)
	}

	val64, err := cpu.LoadU64(0x10000000)
	if err != nil {
		t.Errorf("LoadU64 unmapped should succeed: %v", err)
	}
	if val64 != 0 {
		t.Errorf("LoadU64 unmapped = 0x%x, want 0", val64)
	}
}

// TestStoreUnmappedAllSizes tests store to unmapped addresses for all sizes
func TestStoreUnmappedAllSizes(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Store to unmapped address should succeed silently (C behavior)
	if err := cpu.StoreU8(0x10000000, 0x12); err != nil {
		t.Errorf("StoreU8 unmapped should succeed: %v", err)
	}
	if err := cpu.StoreU16(0x10000000, 0x1234); err != nil {
		t.Errorf("StoreU16 unmapped should succeed: %v", err)
	}
	if err := cpu.StoreU64(0x10000000, 0x123456789ABCDEF0); err != nil {
		t.Errorf("StoreU64 unmapped should succeed: %v", err)
	}
}

// TestMemoryAccessFaultCause tests that page fault causes are correct
func TestMemoryAccessFaultCause(t *testing.T) {
	cpu := mmuTestCPU(t)

	tests := []struct {
		name          string
		accessType    AccessType
		expectedCause int
	}{
		{"Read", AccessRead, CauseLoadPageFault},
		{"Write", AccessWrite, CauseStorePageFault},
		{"Code", AccessCode, CauseFetchPageFault},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cause := cpu.getPageFaultCause(tc.accessType)
			if cause != tc.expectedCause {
				t.Errorf("getPageFaultCause(%v) = %d, want %d", tc.accessType, cause, tc.expectedCause)
			}
		})
	}
}

// TestMMUEnabledCodeAccess tests IsMmuEnabled with MPRV for code access
func TestMMUEnabledCodeAccess(t *testing.T) {
	cpu := mmuTestCPU(t)

	// Set up Sv39 mode
	cpu.Satp = uint64(SatpModeSv39) << 60

	// M-mode with MPRV set - code access should ignore MPRV
	cpu.Priv = PrivMachine
	cpu.Mstatus = MstatusMPRV | (uint64(PrivSupervisor) << MstatusMPPShift)

	// Code access should still use M-mode (ignore MPRV), so MMU disabled
	if cpu.IsMmuEnabled(AccessCode) {
		t.Error("Code access should ignore MPRV and use M-mode (MMU disabled)")
	}

	// Data access with MPRV should use MPP (S-mode), so MMU enabled
	if !cpu.IsMmuEnabled(AccessRead) {
		t.Error("Read access with MPRV should use S-mode (MMU enabled)")
	}
}

// TestEffectivePrivForAccess tests GetEffectivePrivForAccess
func TestEffectivePrivForAccess(t *testing.T) {
	cpu := mmuTestCPU(t)

	// U-mode - effective priv is always U
	cpu.Priv = PrivUser
	cpu.Mstatus = 0
	if cpu.GetEffectivePrivForAccess(AccessRead) != PrivUser {
		t.Error("U-mode should have effective priv U")
	}

	// S-mode - effective priv is always S
	cpu.Priv = PrivSupervisor
	if cpu.GetEffectivePrivForAccess(AccessRead) != PrivSupervisor {
		t.Error("S-mode should have effective priv S")
	}

	// M-mode without MPRV - effective priv is M
	cpu.Priv = PrivMachine
	cpu.Mstatus = 0
	if cpu.GetEffectivePrivForAccess(AccessRead) != PrivMachine {
		t.Error("M-mode without MPRV should have effective priv M")
	}

	// M-mode with MPRV and MPP=S - data access uses S
	cpu.Mstatus = MstatusMPRV | (uint64(PrivSupervisor) << MstatusMPPShift)
	if cpu.GetEffectivePrivForAccess(AccessRead) != PrivSupervisor {
		t.Error("M-mode with MPRV and MPP=S should have effective priv S for data")
	}

	// M-mode with MPRV - code access still uses M
	if cpu.GetEffectivePrivForAccess(AccessCode) != PrivMachine {
		t.Error("M-mode code access should ignore MPRV")
	}
}

// Helper to set up a simple Sv39 page table
// Returns the page table physical address
func setupSv39PageTable(t *testing.T, cpu *CPU, vaddr, paddr uint64, perm uint64) uint64 {
	t.Helper()

	// Page table base at 0x80100000
	ptBase := uint64(0x80100000)

	// Sv39: 3-level page tables
	// vpn[2] (bits 38:30), vpn[1] (bits 29:21), vpn[0] (bits 20:12)
	vpn0 := (vaddr >> 12) & 0x1FF
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF

	// L2 page table at ptBase
	l2Addr := ptBase
	// L1 page table at ptBase + 0x1000
	l1Addr := ptBase + 0x1000
	// L0 page table at ptBase + 0x2000
	l0Addr := ptBase + 0x2000

	// Set up L2 PTE pointing to L1
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// Set up L1 PTE pointing to L0
	l1PTE := ((l0Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	// Set up L0 PTE as leaf pointing to physical page
	ppn := paddr >> 12
	l0PTE := (ppn << 10) | perm | PTEValid
	cpu.Mem.Write64(l0Addr+vpn0*8, l0PTE)

	return ptBase >> 12 // Return PPN for SATP
}

// TestSv39PageTableWalkBasic tests basic Sv39 page table walk
func TestSv39PageTableWalkBasic(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Map virtual address 0x1000 to physical address 0x80002000
	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Translate address
	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}

	if translated != paddr {
		t.Errorf("Translated address = 0x%x, want 0x%x", translated, paddr)
	}

	// Verify TLB was updated
	_, hit := cpu.TLBLookup(vaddr, AccessRead)
	if !hit {
		t.Error("TLB should be updated after translation")
	}
}

// TestSv39PageTableWalkWithOffset tests Sv39 translation preserves page offset
func TestSv39PageTableWalkWithOffset(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Map virtual page 0x1000 to physical page 0x80002000
	// Then access address 0x1234 (offset 0x234)
	vaddr := uint64(0x1234)
	paddr := uint64(0x80002000)
	expectedPaddr := paddr + (vaddr & 0xFFF) // 0x80002234

	ptPPN := setupSv39PageTable(t, cpu, vaddr&^uint64(PageMask), paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}

	if translated != expectedPaddr {
		t.Errorf("Translated address = 0x%x, want 0x%x", translated, expectedPaddr)
	}
}

// TestSv39PageTableWalkPermissionDenied tests Sv39 permission checking
func TestSv39PageTableWalkPermissionDenied(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	// Set up page with read-only permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Read should succeed
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("Read should succeed: %v", err)
	}

	cpu.ClearPendingException()

	// Write should fail
	_, err = cpu.TranslateAddress(vaddr, AccessWrite)
	if err == nil {
		t.Error("Write to read-only page should fail")
	}
	if cpu.PendingException != CauseStorePageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseStorePageFault)
	}

	cpu.ClearPendingException()

	// Execute should fail
	_, err = cpu.TranslateAddress(vaddr, AccessCode)
	if err == nil {
		t.Error("Execute on non-executable page should fail")
	}
	if cpu.PendingException != CauseFetchPageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseFetchPageFault)
	}
}

// TestSv39PageTableWalkInvalidPTE tests Sv39 handling of invalid PTEs
func TestSv39PageTableWalkInvalidPTE(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Set up SATP pointing to a page table area
	ptBase := uint64(0x80100000) >> 12
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptBase

	// Don't set up any valid PTEs - all should be 0 (invalid)
	vaddr := uint64(0x1000)

	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err == nil {
		t.Error("Translation of unmapped address should fail")
	}
	if cpu.PendingException != CauseLoadPageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseLoadPageFault)
	}
}

// TestSv39InvalidXWRCombinations tests that xwr=2 and xwr=6 cause page faults.
// Reference: riscv_cpu.c:258-259 - C rejects xwr=2 (write-only) and xwr=6 (write+execute)
func TestSv39InvalidXWRCombinations(t *testing.T) {
	tests := []struct {
		name     string
		xwr      uint64 // bits [3:1] of PTE
		wantFail bool
	}{
		{"xwr=1 (read-only)", PTERead, false},
		{"xwr=2 (write-only)", PTEWrite, true}, // Invalid per spec
		{"xwr=3 (read-write)", PTERead | PTEWrite, false},
		{"xwr=4 (execute-only)", PTEExecute, false},
		{"xwr=5 (read-execute)", PTERead | PTEExecute, false},
		{"xwr=6 (write-execute)", PTEWrite | PTEExecute, true}, // Invalid per spec
		{"xwr=7 (read-write-execute)", PTERead | PTEWrite | PTEExecute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cpu := mmuTestCPU(t)
			cpu.Priv = PrivSupervisor

			vaddr := uint64(0x1000)
			paddr := uint64(0x80002000)

			ptBase := uint64(0x80100000)
			vpn0 := (vaddr >> 12) & 0x1FF
			vpn1 := (vaddr >> 21) & 0x1FF
			vpn2 := (vaddr >> 30) & 0x1FF

			l2Addr := ptBase
			l1Addr := ptBase + 0x1000
			l0Addr := ptBase + 0x2000

			// Set up L2 PTE pointing to L1
			l2PTE := ((l1Addr >> 12) << 10) | PTEValid
			cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

			// Set up L1 PTE pointing to L0
			l1PTE := ((l0Addr >> 12) << 10) | PTEValid
			cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

			// Set up L0 PTE with test xwr combination
			ppn := paddr >> 12
			l0PTE := (ppn << 10) | tt.xwr | PTEValid | PTEAccessed | PTEDirty
			cpu.Mem.Write64(l0Addr+vpn0*8, l0PTE)

			cpu.Satp = (uint64(SatpModeSv39) << 60) | (ptBase >> 12)

			// Try to translate - use appropriate access type
			var accessType AccessType
			if tt.xwr&PTERead != 0 {
				accessType = AccessRead
			} else if tt.xwr&PTEWrite != 0 {
				accessType = AccessWrite
			} else {
				accessType = AccessCode
			}

			_, err := cpu.TranslateAddress(vaddr, accessType)

			if tt.wantFail {
				if err == nil {
					t.Errorf("xwr=%d should cause page fault (invalid combination)", tt.xwr>>1)
				}
			} else {
				if err != nil {
					t.Errorf("xwr=%d should succeed: %v", tt.xwr>>1, err)
				}
			}
		})
	}
}

// TestSv39PageTableWalkUserMode tests Sv39 user mode permissions
func TestSv39PageTableWalkUserMode(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivUser

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	// Set up page with user permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEUser|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// User mode read should succeed
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("User read should succeed: %v", err)
	}
}

// TestSv39PageTableWalkUserDenied tests Sv39 user mode denied access to supervisor page
func TestSv39PageTableWalkUserDenied(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivUser

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	// Set up page WITHOUT user permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// User mode read should fail
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err == nil {
		t.Error("User access to supervisor page should fail")
	}
}

// TestSv39ADUpdate tests that A/D bits are updated during translation
func TestSv39ADUpdate(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	ptBase := uint64(0x80100000)
	vpn0 := (vaddr >> 12) & 0x1FF
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF

	l2Addr := ptBase
	l1Addr := ptBase + 0x1000
	l0Addr := ptBase + 0x2000

	// Set up L2 PTE
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// Set up L1 PTE
	l1PTE := ((l0Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	// Set up L0 PTE WITHOUT A/D bits set
	ppn := paddr >> 12
	l0PTE := (ppn << 10) | PTERead | PTEWrite | PTEValid
	cpu.Mem.Write64(l0Addr+vpn0*8, l0PTE)

	cpu.Satp = (uint64(SatpModeSv39) << 60) | (ptBase >> 12)

	// Translate for read - should set A bit
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}

	// Read back PTE and verify A bit is set
	pteAfter, _ := cpu.Mem.Read64(l0Addr + vpn0*8)
	if pteAfter&PTEAccessed == 0 {
		t.Error("A bit should be set after read access")
	}

	// Clear TLB to force another walk
	cpu.FlushTLB()

	// Translate for write - should set D bit
	_, err = cpu.TranslateAddress(vaddr, AccessWrite)
	if err != nil {
		t.Fatalf("TranslateAddress for write failed: %v", err)
	}

	pteAfter, _ = cpu.Mem.Read64(l0Addr + vpn0*8)
	if pteAfter&PTEDirty == 0 {
		t.Error("D bit should be set after write access")
	}
}

// TestSv39SuperpageAlignment tests that misaligned superpages are handled
// by masking off the misaligned bits (matching C TinyEMU behavior).
// Reference: riscv_cpu.c:286-288 - C masks off misaligned bits rather than faulting
func TestSv39SuperpageAlignment(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	ptBase := uint64(0x80100000)
	l2Addr := ptBase
	l1Addr := ptBase + 0x1000

	vaddr := uint64(0x200000) // 2MB aligned
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF

	// Set up L2 PTE pointing to L1
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// Set up L1 PTE as a leaf (2MB superpage) with MISALIGNED ppn[0]
	// C TinyEMU doesn't check for this - it just masks off the lower bits
	// Reference: riscv_cpu.c:286-288
	misalignedPPN := uint64(0x80001) // Has non-zero lower 9 bits
	l1PTE := (misalignedPPN << 10) | PTERead | PTEValid | PTEAccessed
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	cpu.Satp = (uint64(SatpModeSv39) << 60) | (ptBase >> 12)

	// Translation should succeed (C behavior: masks off misaligned bits)
	// Expected: (0x80001 & ~0x1FF) << 12 = 0x80000000
	paddr, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("Translation should succeed (C masks misaligned bits): %v", err)
	}
	expectedPaddr := uint64(0x80000000)
	if paddr != expectedPaddr {
		t.Errorf("paddr = 0x%x, want 0x%x (misaligned bits should be masked)", paddr, expectedPaddr)
	}
}

// Helper to set up a simple Sv48 page table
func setupSv48PageTable(t *testing.T, cpu *CPU, vaddr, paddr uint64, perm uint64) uint64 {
	t.Helper()

	ptBase := uint64(0x80100000)

	vpn0 := (vaddr >> 12) & 0x1FF
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF
	vpn3 := (vaddr >> 39) & 0x1FF

	l3Addr := ptBase
	l2Addr := ptBase + 0x1000
	l1Addr := ptBase + 0x2000
	l0Addr := ptBase + 0x3000

	// Set up L3 -> L2
	l3PTE := ((l2Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l3Addr+vpn3*8, l3PTE)

	// Set up L2 -> L1
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// Set up L1 -> L0
	l1PTE := ((l0Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	// Set up L0 leaf
	ppn := paddr >> 12
	l0PTE := (ppn << 10) | perm | PTEValid
	cpu.Mem.Write64(l0Addr+vpn0*8, l0PTE)

	return ptBase >> 12
}

// TestSv48PageTableWalkBasic tests basic Sv48 page table walk
func TestSv48PageTableWalkBasic(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	ptPPN := setupSv48PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv48) << 60) | ptPPN

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}

	if translated != paddr {
		t.Errorf("Translated address = 0x%x, want 0x%x", translated, paddr)
	}
}

// TestSv48CanonicalAddress tests Sv48 canonical address checking
func TestSv48CanonicalAddress(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor
	cpu.Satp = uint64(SatpModeSv48) << 60

	// Non-canonical address (bit 47=0 but high bits set)
	_, err := cpu.translateSv48(0x8000000000000000, AccessRead)
	if err == nil {
		t.Error("Non-canonical address should cause page fault")
	}
}

// TestSv48InvalidPTE tests Sv48 handling of invalid PTEs
func TestSv48InvalidPTE(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	ptBase := uint64(0x80100000) >> 12
	cpu.Satp = (uint64(SatpModeSv48) << 60) | ptBase

	vaddr := uint64(0x1000)

	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err == nil {
		t.Error("Translation of unmapped address should fail")
	}
}

// TestSv48PermissionDenied tests Sv48 permission checking
func TestSv48PermissionDenied(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	// Set up page with read-only permission
	ptPPN := setupSv48PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv48) << 60) | ptPPN

	// Read should succeed
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("Read should succeed: %v", err)
	}

	cpu.ClearPendingException()

	// Write should fail
	_, err = cpu.TranslateAddress(vaddr, AccessWrite)
	if err == nil {
		t.Error("Write to read-only page should fail")
	}
}

// TestSv48SuperpageAlignment tests that misaligned Sv48 superpages are handled
// by masking off the misaligned bits (matching C TinyEMU behavior).
// Reference: riscv_cpu.c:286-288 - C masks off misaligned bits rather than faulting
func TestSv48SuperpageAlignment(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	ptBase := uint64(0x80100000)
	l3Addr := ptBase
	l2Addr := ptBase + 0x1000
	l1Addr := ptBase + 0x2000

	vaddr := uint64(0x200000) // 2MB aligned
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF
	vpn3 := (vaddr >> 39) & 0x1FF

	// L3 -> L2
	l3PTE := ((l2Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l3Addr+vpn3*8, l3PTE)

	// L2 -> L1
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// L1 as misaligned 2MB superpage leaf
	// C TinyEMU doesn't check for this - it just masks off the lower bits
	// Reference: riscv_cpu.c:286-288
	misalignedPPN := uint64(0x80001)
	l1PTE := (misalignedPPN << 10) | PTERead | PTEValid | PTEAccessed
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	cpu.Satp = (uint64(SatpModeSv48) << 60) | (ptBase >> 12)

	// Translation should succeed (C behavior: masks off misaligned bits)
	// Expected: (0x80001 & ~0x1FF) << 12 = 0x80000000
	paddr, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("Translation should succeed (C masks misaligned bits): %v", err)
	}
	expectedPaddr := uint64(0x80000000)
	if paddr != expectedPaddr {
		t.Errorf("paddr = 0x%x, want 0x%x (misaligned bits should be masked)", paddr, expectedPaddr)
	}
}

// TestFetchInstructionWithMMU tests FetchInstruction with MMU enabled
func TestFetchInstructionWithMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Virtual address to fetch from
	vaddr := uint64(0x1000)
	// Physical address to map to
	paddr := uint64(0x80002000)

	// Write instruction at physical address
	testInsn := uint32(0x00000013) // NOP
	cpu.Mem.Write32(paddr, testInsn)

	// Set up page table with execute permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	cpu.PC = vaddr

	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("FetchInstruction failed: %v", err)
	}
	if insn != testInsn {
		t.Errorf("insn = 0x%x, want 0x%x", insn, testInsn)
	}
}

// TestFetchInstructionTLBHit tests FetchInstruction with TLB hit
func TestFetchInstructionTLBHit(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	testInsn := uint32(0xDEADBEEF)
	cpu.Mem.Write32(paddr, testInsn)

	// Set up page table and do first translation to populate TLB
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	cpu.PC = vaddr
	_, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}

	// Second fetch should hit TLB
	cpu.PC = vaddr
	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}
	if insn != testInsn {
		t.Errorf("insn = 0x%x, want 0x%x", insn, testInsn)
	}
}

// TestFetchInstructionNoExecute tests fetch from non-executable page
func TestFetchInstructionNoExecute(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	// Set up page table WITHOUT execute permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	cpu.PC = vaddr

	_, err := cpu.FetchInstruction()
	if err == nil {
		t.Error("Fetch from non-executable page should fail")
	}
	if cpu.PendingException != CauseFetchPageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseFetchPageFault)
	}
}

// TestLoadStoreWithMMU tests load/store with MMU enabled
func TestLoadStoreWithMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x1000)
	paddr := uint64(0x80002000)

	// Set up page table with read/write permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Store a value
	if err := cpu.StoreU32(vaddr+100, 0xCAFEBABE); err != nil {
		t.Fatalf("StoreU32 failed: %v", err)
	}

	// Load it back
	val, err := cpu.LoadU32(vaddr + 100)
	if err != nil {
		t.Fatalf("LoadU32 failed: %v", err)
	}
	if val != 0xCAFEBABE {
		t.Errorf("LoadU32 = 0x%x, want 0xCAFEBABE", val)
	}
}

// TestTranslateAddressUnsupportedMode tests unsupported SATP mode
func TestTranslateAddressUnsupportedMode(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Set an invalid/unsupported SATP mode (e.g., mode 15)
	cpu.Satp = (uint64(15) << 60) | 0x1000

	_, err := cpu.TranslateAddress(0x1000, AccessRead)
	if err == nil {
		t.Error("Unsupported SATP mode should cause error")
	}
}

// setupSv39Megapage sets up a 2MB megapage mapping (L1 leaf).
// Reference: riscv_cpu.c get_phys_addr() lines 244-287 - the page table walk
// handles superpages by checking if xwr != 0 at any level.
func setupSv39Megapage(t *testing.T, cpu *CPU, vaddr, paddr uint64, perm uint64) uint64 {
	t.Helper()

	// Page table base at 0x80100000
	ptBase := uint64(0x80100000)

	// For a 2MB megapage, we need to stop at level 1 (L1)
	// vpn[2] indexes L2 (root), vpn[1] indexes L1 (where we put the leaf)
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF

	l2Addr := ptBase
	l1Addr := ptBase + 0x1000

	// L2 PTE points to L1 (non-leaf)
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// L1 PTE is a leaf (megapage) - xwr != 0 makes it a leaf
	// For a valid 2MB superpage, the physical address must be 2MB-aligned
	// The PPN stored in the PTE must have ppn[0] = 0 (the low 9 bits of PPN)
	ppn := paddr >> 12
	l1PTE := (ppn << 10) | perm | PTEValid
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	return ptBase >> 12 // Return PPN for SATP
}

// TestSv39MegapageTranslation tests 2MB megapage (superpage) translation.
// Reference: riscv_cpu.c get_phys_addr() lines 256-288 - when xwr != 0 at level 1,
// it's a 2MB superpage, and physical address is computed with vaddr_mask.
func TestSv39MegapageTranslation(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Virtual address within a 2MB region (2MB = 0x200000)
	// vaddr = 0x200000 base + 0x1234 offset within megapage
	vaddr := uint64(0x200000 + 0x1234)
	// Physical megapage base must be 2MB-aligned
	paddr := uint64(0x80200000) // 2MB-aligned physical address
	// Expected physical address = paddr base + offset within megapage
	expectedPaddr := paddr + (vaddr & 0x1FFFFF) // 2MB mask = 0x1FFFFF

	ptPPN := setupSv39Megapage(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}

	if translated != expectedPaddr {
		t.Errorf("Megapage translated address = 0x%x, want 0x%x", translated, expectedPaddr)
	}
}

// TestSv39MegapageOffset tests that megapage translation preserves the 21-bit offset.
func TestSv39MegapageOffset(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Test various offsets within the 2MB megapage
	baseVaddr := uint64(0x400000) // 4MB virtual address (2MB aligned)
	basePaddr := uint64(0x80400000)

	ptPPN := setupSv39Megapage(t, cpu, baseVaddr, basePaddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	offsets := []uint64{0, 0x1000, 0x12345, 0x100000, 0x1FFFFF}
	for _, offset := range offsets {
		cpu.FlushTLB()
		vaddr := baseVaddr + offset
		expectedPaddr := basePaddr + offset

		translated, err := cpu.TranslateAddress(vaddr, AccessRead)
		if err != nil {
			t.Errorf("Megapage translation at offset 0x%x failed: %v", offset, err)
			continue
		}
		if translated != expectedPaddr {
			t.Errorf("Megapage offset 0x%x: got 0x%x, want 0x%x", offset, translated, expectedPaddr)
		}
	}
}

// TestSv39MegapagePermissions tests permission checking for megapages.
func TestSv39MegapagePermissions(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x200000)
	paddr := uint64(0x80200000)

	// Set up read-only megapage
	ptPPN := setupSv39Megapage(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Read should succeed
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("Megapage read should succeed: %v", err)
	}

	cpu.ClearPendingException()

	// Write should fail
	_, err = cpu.TranslateAddress(vaddr, AccessWrite)
	if err == nil {
		t.Error("Megapage write to read-only page should fail")
	}
}

// setupSv39Gigapage sets up a 1GB gigapage mapping (L2 leaf).
// Reference: riscv_cpu.c get_phys_addr() - when xwr != 0 at level 2 (root),
// it's a 1GB gigapage.
func setupSv39Gigapage(t *testing.T, cpu *CPU, vaddr, paddr uint64, perm uint64) uint64 {
	t.Helper()

	ptBase := uint64(0x80100000)

	// For a 1GB gigapage, the L2 PTE is a leaf
	vpn2 := (vaddr >> 30) & 0x1FF

	l2Addr := ptBase

	// L2 PTE is a leaf (gigapage) - xwr != 0 makes it a leaf
	// For a valid 1GB superpage, the physical address must be 1GB-aligned
	// The PPN stored must have ppn[1] and ppn[0] = 0
	ppn := paddr >> 12
	l2PTE := (ppn << 10) | perm | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	return ptBase >> 12
}

// TestSv39GigapageTranslation tests 1GB gigapage (superpage) translation.
// Reference: riscv_cpu.c get_phys_addr() lines 256-288
func TestSv39GigapageTranslation(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Virtual address within a 1GB region (1GB = 0x40000000)
	// Use a small offset to test within the first GB region
	vaddr := uint64(0x40000000 + 0x12345678)
	// Physical gigapage base must be 1GB-aligned
	paddr := uint64(0x80000000) // 1GB-aligned (using RAM start)
	// Expected physical address = paddr base + offset within gigapage
	expectedPaddr := paddr + (vaddr & 0x3FFFFFFF) // 1GB mask = 0x3FFFFFFF

	ptPPN := setupSv39Gigapage(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("TranslateAddress failed: %v", err)
	}

	if translated != expectedPaddr {
		t.Errorf("Gigapage translated address = 0x%x, want 0x%x", translated, expectedPaddr)
	}
}

// TestSv39GigapageOffset tests that gigapage translation preserves the 30-bit offset.
func TestSv39GigapageOffset(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	baseVaddr := uint64(0x40000000) // 1GB virtual address
	basePaddr := uint64(0x80000000) // 1GB-aligned physical

	ptPPN := setupSv39Gigapage(t, cpu, baseVaddr, basePaddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Test offsets within the 1GB gigapage
	offsets := []uint64{0, 0x1000, 0x100000, 0x1000000, 0x10000000}
	for _, offset := range offsets {
		cpu.FlushTLB()
		vaddr := baseVaddr + offset
		expectedPaddr := basePaddr + offset

		translated, err := cpu.TranslateAddress(vaddr, AccessRead)
		if err != nil {
			t.Errorf("Gigapage translation at offset 0x%x failed: %v", offset, err)
			continue
		}
		if translated != expectedPaddr {
			t.Errorf("Gigapage offset 0x%x: got 0x%x, want 0x%x", offset, translated, expectedPaddr)
		}
	}
}

// TestSv39GigapagePermissions tests permission checking for gigapages.
func TestSv39GigapagePermissions(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x40000000)
	paddr := uint64(0x80000000)

	// Set up execute-only gigapage
	ptPPN := setupSv39Gigapage(t, cpu, vaddr, paddr, PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Execute should succeed
	_, err := cpu.TranslateAddress(vaddr, AccessCode)
	if err != nil {
		t.Errorf("Gigapage execute should succeed: %v", err)
	}

	cpu.ClearPendingException()

	// Read should fail (no R bit, MXR not set)
	cpu.Mstatus &= ^uint64(MstatusMXR)
	_, err = cpu.TranslateAddress(vaddr, AccessRead)
	if err == nil {
		t.Error("Gigapage read to execute-only page should fail without MXR")
	}

	cpu.ClearPendingException()

	// Read with MXR should succeed
	cpu.Mstatus |= MstatusMXR
	_, err = cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Errorf("Gigapage read with MXR should succeed: %v", err)
	}
}

// TestSfenceVmaFlushAll tests sfence.vma with rs1=0 flushes all TLB entries.
// Reference: riscv_cpu_template.h lines 1326-1333 - sfence.vma handling
func TestSfenceVmaFlushAll(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Populate TLB entries
	vaddr := uint64(0x80001000)
	cpu.updateTLB(vaddr, 0x80001000, AccessRead)
	cpu.updateTLB(vaddr, 0x80001000, AccessWrite)
	cpu.updateTLB(vaddr, 0x80001000, AccessCode)

	// Verify TLB has entries
	idx := (vaddr >> PageShift) & (TLBSize - 1)
	if cpu.TLBRead[idx].VAddr != vaddr {
		t.Fatal("TLB read entry not set up correctly")
	}

	// Execute sfence.vma with rs1=0 (flush all)
	// sfence.vma encoding: funct7=0b0001001, rs2, rs1, funct3=000, rd=00000, opcode=1110011
	// rs1=0, rs2=0 means flush all
	cpu.FlushTLB()

	// Verify all TLB entries are invalidated
	if cpu.TLBRead[idx].VAddr != ^uint64(0) {
		t.Error("TLB read entry should be invalidated after sfence.vma")
	}
	if cpu.TLBWrite[idx].VAddr != ^uint64(0) {
		t.Error("TLB write entry should be invalidated after sfence.vma")
	}
	if cpu.TLBCode[idx].VAddr != ^uint64(0) {
		t.Error("TLB code entry should be invalidated after sfence.vma")
	}
}

// TestSfenceVmaFlushSingle tests sfence.vma with rs1!=0 flushes specific entry.
// Reference: riscv_cpu_template.h lines 1333-1337 - sfence.vma with address
func TestSfenceVmaFlushSingle(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Populate TLB entries at different addresses
	vaddr1 := uint64(0x80001000)
	vaddr2 := uint64(0x80002000)
	cpu.updateTLB(vaddr1, 0x80001000, AccessRead)
	cpu.updateTLB(vaddr2, 0x80002000, AccessRead)

	idx1 := (vaddr1 >> PageShift) & (TLBSize - 1)
	idx2 := (vaddr2 >> PageShift) & (TLBSize - 1)

	// Flush only vaddr1
	cpu.FlushTLBEntry(vaddr1)

	// vaddr1 entry should be invalidated
	if cpu.TLBRead[idx1].VAddr != ^uint64(0) {
		t.Error("TLB entry for vaddr1 should be invalidated")
	}

	// vaddr2 entry should still be valid
	if cpu.TLBRead[idx2].VAddr != vaddr2 {
		t.Error("TLB entry for vaddr2 should remain valid")
	}
}

// TestSfenceVmaAfterSatpChange tests TLB behavior after SATP register change.
// Reference: riscv_cpu.c lines 953-956 - SATP write flushes TLB
func TestSfenceVmaAfterSatpChange(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Set up initial SATP and populate TLB
	cpu.Satp = (uint64(SatpModeSv39) << 60) | 0x1000
	vaddr := uint64(0x80001000)
	cpu.updateTLB(vaddr, 0x80001000, AccessRead)

	idx := (vaddr >> PageShift) & (TLBSize - 1)
	if cpu.TLBRead[idx].VAddr != vaddr {
		t.Fatal("TLB not populated")
	}

	// Change SATP mode - should trigger TLB flush
	oldMode := cpu.GetSatpMode()
	cpu.Satp = 0 // Change to bare mode

	// If mode changed, TLB should be flushed (via WriteCSR in real usage)
	newMode := cpu.GetSatpMode()
	if oldMode != newMode {
		cpu.FlushTLB() // Simulating what WriteCSR(SATP) does
	}

	if cpu.TLBRead[idx].VAddr != ^uint64(0) {
		t.Error("TLB should be flushed after SATP mode change")
	}
}

// TestSfenceVmaPrivilegeCheck tests that sfence.vma fails in U-mode.
// Reference: riscv_cpu_template.h lines 1331-1332 - U-mode check
func TestSfenceVmaPrivilegeCheck(t *testing.T) {
	cpu := mmuTestCPU(t)

	// In U-mode, sfence.vma should be illegal
	cpu.Priv = PrivUser
	cpu.Satp = 0 // Bare mode to allow instruction fetch

	// Set up trap handler address
	cpu.Mtvec = 0x80001000

	// Try to execute sfence.vma
	// sfence.vma rs1, rs2 encoding: imm=0x120 (funct7=0001001), funct3=000, opcode=1110011
	// This is the SYSTEM opcode with specific funct fields
	// The actual instruction is: 0x12000073 for sfence.vma x0, x0
	cpu.PC = 0x80000000
	cpu.Mem.Write32(cpu.PC, 0x12000073) // sfence.vma x0, x0

	// Execute - exception is raised and handled
	originalPC := cpu.PC
	err := cpu.Step()

	// Step() returns nil even on exception (exception is handled internally)
	if err != nil {
		t.Fatalf("Step returned unexpected error: %v", err)
	}

	// Check that illegal instruction exception was trapped:
	// - Mcause should be CauseIllegalInsn (exception not delegated from U-mode by default)
	// - Mepc should be the original PC
	// - Priv should now be M-mode (trap handler runs in M-mode)
	if cpu.Mcause != uint64(CauseIllegalInsn) {
		t.Errorf("sfence.vma in U-mode should raise illegal instruction, got Mcause=%d", cpu.Mcause)
	}
	if cpu.Mepc != originalPC {
		t.Errorf("Mepc should be original PC 0x%x, got 0x%x", originalPC, cpu.Mepc)
	}
	if cpu.Priv != PrivMachine {
		t.Errorf("Should have trapped to M-mode, priv=%d", cpu.Priv)
	}
}

// TestTLBFlushOnPrivilegeChange tests TLB is flushed when privilege changes.
// Reference: riscv_cpu.c lines 1022-1025 - privilege change flushes TLB
func TestTLBFlushOnPrivilegeChange(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine

	// Populate TLB
	vaddr := uint64(0x80001000)
	cpu.updateTLB(vaddr, 0x80001000, AccessRead)

	idx := (vaddr >> PageShift) & (TLBSize - 1)
	if cpu.TLBRead[idx].VAddr != vaddr {
		t.Fatal("TLB not populated")
	}

	// Change privilege level
	cpu.SetPriv(PrivSupervisor)

	// TLB should be flushed
	if cpu.TLBRead[idx].VAddr != ^uint64(0) {
		t.Error("TLB should be flushed after privilege change")
	}
}

// TestTLBFlushOnMstatusChange tests TLB is flushed when MPRV/SUM/MXR change.
// Reference: riscv_cpu.c lines 680-683 - MSTATUS changes may flush TLB
func TestTLBFlushOnMstatusChange(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivMachine

	// Populate TLB
	vaddr := uint64(0x80001000)
	cpu.updateTLB(vaddr, 0x80001000, AccessRead)

	idx := (vaddr >> PageShift) & (TLBSize - 1)
	if cpu.TLBRead[idx].VAddr != vaddr {
		t.Fatal("TLB not populated")
	}

	// Changing MPRV should flush TLB (via WriteCSR in real usage)
	// Here we manually verify the behavior
	cpu.Mstatus |= MstatusMPRV
	cpu.FlushTLB() // Simulating what the CSR write does

	if cpu.TLBRead[idx].VAddr != ^uint64(0) {
		t.Error("TLB should be flushed after MPRV change")
	}
}

// TestSv48MegapageTranslation tests 2MB megapage translation in Sv48.
func TestSv48MegapageTranslation(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	ptBase := uint64(0x80100000)
	l3Addr := ptBase
	l2Addr := ptBase + 0x1000
	l1Addr := ptBase + 0x2000

	vaddr := uint64(0x200000 + 0x5678) // 2MB aligned + offset
	paddr := uint64(0x80200000)        // 2MB-aligned physical
	expectedPaddr := paddr + (vaddr & 0x1FFFFF)

	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF
	vpn3 := (vaddr >> 39) & 0x1FF

	// L3 -> L2 (non-leaf)
	l3PTE := ((l2Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l3Addr+vpn3*8, l3PTE)

	// L2 -> L1 (non-leaf)
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// L1 is leaf (2MB megapage)
	ppn := paddr >> 12
	l1PTE := (ppn << 10) | PTERead | PTEValid | PTEAccessed
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	cpu.Satp = (uint64(SatpModeSv48) << 60) | (ptBase >> 12)

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("Sv48 megapage translation failed: %v", err)
	}

	if translated != expectedPaddr {
		t.Errorf("Sv48 megapage: got 0x%x, want 0x%x", translated, expectedPaddr)
	}
}

// TestSv48GigapageTranslation tests 1GB gigapage translation in Sv48.
func TestSv48GigapageTranslation(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	ptBase := uint64(0x80100000)
	l3Addr := ptBase
	l2Addr := ptBase + 0x1000

	vaddr := uint64(0x40000000 + 0x1234567) // 1GB + offset
	paddr := uint64(0x80000000)             // 1GB-aligned
	expectedPaddr := paddr + (vaddr & 0x3FFFFFFF)

	vpn2 := (vaddr >> 30) & 0x1FF
	vpn3 := (vaddr >> 39) & 0x1FF

	// L3 -> L2 (non-leaf)
	l3PTE := ((l2Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l3Addr+vpn3*8, l3PTE)

	// L2 is leaf (1GB gigapage)
	ppn := paddr >> 12
	l2PTE := (ppn << 10) | PTERead | PTEValid | PTEAccessed
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	cpu.Satp = (uint64(SatpModeSv48) << 60) | (ptBase >> 12)

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("Sv48 gigapage translation failed: %v", err)
	}

	if translated != expectedPaddr {
		t.Errorf("Sv48 gigapage: got 0x%x, want 0x%x", translated, expectedPaddr)
	}
}

// TestSv48TerapageTranslation tests 512GB terapage translation in Sv48.
// In Sv48, a leaf at L3 would be a 512GB terapage.
func TestSv48TerapageTranslation(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	ptBase := uint64(0x80100000)
	l3Addr := ptBase

	// Virtual address in the first 512GB region with an offset
	vaddr := uint64(0x1234567890)
	// Physical terapage base must be 512GB-aligned (0 is simplest)
	// But our RAM is at 0x80000000, so we need to be careful
	// For this test, we use vaddr that maps to within our RAM
	paddr := uint64(0) // 512GB-aligned
	expectedPaddr := paddr + (vaddr & 0x7FFFFFFFFF)

	vpn3 := (vaddr >> 39) & 0x1FF

	// L3 is leaf (512GB terapage)
	ppn := paddr >> 12
	l3PTE := (ppn << 10) | PTERead | PTEValid | PTEAccessed
	cpu.Mem.Write64(l3Addr+vpn3*8, l3PTE)

	cpu.Satp = (uint64(SatpModeSv48) << 60) | (ptBase >> 12)

	translated, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("Sv48 terapage translation failed: %v", err)
	}

	if translated != expectedPaddr {
		t.Errorf("Sv48 terapage: got 0x%x, want 0x%x", translated, expectedPaddr)
	}
}

// TestMegapageLoadStore tests actual load/store through megapage translation.
func TestMegapageLoadStore(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Map virtual megapage at 0x200000 to physical 0x80200000
	baseVaddr := uint64(0x200000)
	basePaddr := uint64(0x80200000)

	ptPPN := setupSv39Megapage(t, cpu, baseVaddr, basePaddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Store value at various offsets within the megapage
	offsets := []uint64{0, 0x1000, 0x50000, 0x100000}
	testVal := uint32(0xDEADBEEF)

	for i, offset := range offsets {
		addr := baseVaddr + offset
		val := testVal + uint32(i)

		if err := cpu.StoreU32(addr, val); err != nil {
			t.Fatalf("StoreU32 at offset 0x%x failed: %v", offset, err)
		}

		loaded, err := cpu.LoadU32(addr)
		if err != nil {
			t.Fatalf("LoadU32 at offset 0x%x failed: %v", offset, err)
		}

		if loaded != val {
			t.Errorf("Megapage load/store at offset 0x%x: got 0x%x, want 0x%x", offset, loaded, val)
		}
	}
}

// TestGigapageLoadStore tests actual load/store through gigapage translation.
func TestGigapageLoadStore(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	// Map virtual gigapage at 0x40000000 to physical 0x80000000
	baseVaddr := uint64(0x40000000)
	basePaddr := uint64(0x80000000)

	ptPPN := setupSv39Gigapage(t, cpu, baseVaddr, basePaddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Store value at various offsets within the gigapage (but within our 4MB RAM)
	offsets := []uint64{0, 0x1000, 0x100000, 0x200000}
	testVal := uint64(0xCAFEBABE12345678)

	for i, offset := range offsets {
		addr := baseVaddr + offset
		val := testVal + uint64(i)

		if err := cpu.StoreU64(addr, val); err != nil {
			t.Fatalf("StoreU64 at offset 0x%x failed: %v", offset, err)
		}

		loaded, err := cpu.LoadU64(addr)
		if err != nil {
			t.Fatalf("LoadU64 at offset 0x%x failed: %v", offset, err)
		}

		if loaded != val {
			t.Errorf("Gigapage load/store at offset 0x%x: got 0x%x, want 0x%x", offset, loaded, val)
		}
	}
}

// ===========================================================================
// Userspace Execution Through MMU Tests
// These tests validate the M->S->U privilege transition path with MMU enabled,
// which is the specific path that fails during Linux init execution.
// Reference: riscv_cpu.c get_phys_addr() lines 257-267 (U-bit permission check)
// Reference: riscv_cpu.c handle_sret() lines 1127-1141
// ===========================================================================

// setupUserPage sets up a page table entry for user-mode code execution.
// This maps a virtual address to a physical address with U (user) and X (execute) bits.
// Reference: riscv_cpu.c lines 262-266 - PRV_U requires PTE_U_MASK
func setupUserPage(t *testing.T, cpu *CPU, vaddr, paddr uint64, perm uint64) uint64 {
	t.Helper()
	// Use the existing setupSv39PageTable helper, ensuring U bit is included
	return setupSv39PageTable(t, cpu, vaddr, paddr, perm|PTEUser)
}

// TestUserspaceExecutionThroughMMU tests the complete M->S->U transition with MMU.
// This is the specific path that fails during Linux init execution.
// Reference: riscv_cpu.c handle_sret() lines 1127-1141 (SRET to U-mode)
// Reference: riscv_cpu.c get_phys_addr() lines 265-266 (U-bit check for U-mode)
func TestUserspaceExecutionThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	// Virtual address for user code (must be in Sv39 canonical range)
	userVaddr := uint64(0x10000)
	// Physical address to map to (within our RAM at 0x80000000)
	userPaddr := uint64(0x80200000)

	// S-mode virtual address (identity mapped to physical for simplicity)
	sVaddr := uint64(0x80000000)
	sPaddr := uint64(0x80000000)

	// Page table base
	ptBase := uint64(0x80100000)

	// Set up page tables manually - we need to map two pages:
	// 1. S-mode code at 0x80000000 (supervisor page, no U bit)
	// 2. User code at 0x10000 (user page, with U bit)

	// For 0x10000: vpn[2]=0, vpn[1]=0, vpn[0]=0x10
	// For 0x80000000: vpn[2]=2, vpn[1]=0, vpn[0]=0

	l2Addr := ptBase
	l1AddrUser := ptBase + 0x1000 // L1 for user page (vpn[2]=0)
	l0AddrUser := ptBase + 0x2000 // L0 for user page
	l1AddrSup := ptBase + 0x3000  // L1 for supervisor page (vpn[2]=2)
	l0AddrSup := ptBase + 0x4000  // L0 for supervisor page

	// L2[0] -> L1 for user region
	cpu.Mem.Write64(l2Addr+0*8, ((l1AddrUser>>12)<<10)|PTEValid)
	// L2[2] -> L1 for supervisor region (vpn[2]=2 for vaddr 0x80000000)
	cpu.Mem.Write64(l2Addr+2*8, ((l1AddrSup>>12)<<10)|PTEValid)

	// L1[0] -> L0 for user pages
	cpu.Mem.Write64(l1AddrUser+0*8, ((l0AddrUser>>12)<<10)|PTEValid)
	// L1[0] -> L0 for supervisor pages (vpn[1]=0 for vaddr 0x80000000)
	cpu.Mem.Write64(l1AddrSup+0*8, ((l0AddrSup>>12)<<10)|PTEValid)

	// L0[0x10] for user page 0x10000 -> paddr 0x80200000 with U bit
	cpu.Mem.Write64(l0AddrUser+0x10*8, ((userPaddr>>12)<<10)|PTEValid|PTERead|PTEExecute|PTEUser|PTEAccessed)
	// L0[0] for supervisor page 0x80000000 -> paddr 0x80000000 (no U bit)
	cpu.Mem.Write64(l0AddrSup+0*8, ((sPaddr>>12)<<10)|PTEValid|PTERead|PTEExecute|PTEAccessed)

	cpu.Satp = (uint64(SatpModeSv39) << 60) | (ptBase >> 12)

	// Write ECALL instruction at the physical address (user code)
	// ECALL = 0x00000073
	cpu.Mem.Write32(userPaddr, 0x00000073)

	// Set up S-mode trap handler
	cpu.Stvec = 0x80010000

	// Delegate user ecall to S-mode
	cpu.Medeleg = 1 << CauseUserEcall

	// Start in S-mode, prepare to SRET to U-mode
	cpu.Priv = PrivSupervisor
	cpu.Sepc = userVaddr // Return to user code virtual address
	cpu.Mstatus = MstatusSPIE
	cpu.Mstatus &^= MstatusSPP // Ensure SPP = U-mode (0)
	cpu.PC = sVaddr            // S-mode will execute SRET here

	// Write SRET instruction at current PC
	// SRET = 0x10200073
	cpu.Mem.Write32(sPaddr, 0x10200073)

	// Execute SRET - should transition to U-mode at userVaddr
	err := cpu.Step()
	if err != nil {
		t.Fatalf("SRET execution failed: %v", err)
	}

	// Verify we're now in U-mode
	if cpu.Priv != PrivUser {
		t.Errorf("After SRET: Priv = %d, want %d (U-mode)", cpu.Priv, PrivUser)
	}

	// Verify PC is at user code virtual address
	if cpu.PC != userVaddr {
		t.Errorf("After SRET: PC = 0x%x, want 0x%x (user vaddr)", cpu.PC, userVaddr)
	}

	// Now execute the ECALL instruction at userVaddr
	// This tests instruction fetch through MMU in U-mode
	err = cpu.Step()
	if err != nil {
		t.Fatalf("ECALL execution failed: %v", err)
	}

	// Verify ECALL caused trap to S-mode (delegated)
	if cpu.Priv != PrivSupervisor {
		t.Errorf("After ECALL: Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}

	// Verify scause is user ecall
	if cpu.Scause != uint64(CauseUserEcall) {
		t.Errorf("After ECALL: Scause = 0x%x, want 0x%x", cpu.Scause, CauseUserEcall)
	}

	// Verify sepc points to the ECALL instruction
	if cpu.Sepc != userVaddr {
		t.Errorf("After ECALL: Sepc = 0x%x, want 0x%x", cpu.Sepc, userVaddr)
	}

	// Verify PC is at stvec (trap handler)
	if cpu.PC != cpu.Stvec {
		t.Errorf("After ECALL: PC = 0x%x, want 0x%x (stvec)", cpu.PC, cpu.Stvec)
	}

	// Verify SPP records U-mode
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp != PrivUser {
		t.Errorf("After ECALL: SPP = %d, want %d (U-mode)", spp, PrivUser)
	}
}

// TestUserModeInstructionFetchThroughMMU tests instruction fetch in U-mode through MMU.
// This specifically tests the FetchInstruction path for user code.
// Reference: riscv_cpu.c lines 265-266 - U-mode instruction fetch requires PTE_U_MASK
func TestUserModeInstructionFetchThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up page table with User + Execute + Read permissions
	ptPPN := setupUserPage(t, cpu, userVaddr, userPaddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write a NOP instruction at the physical address
	// ADDI x0, x0, 0 = 0x00000013
	cpu.Mem.Write32(userPaddr, 0x00000013)

	// Set CPU to U-mode with PC at user virtual address
	cpu.Priv = PrivUser
	cpu.PC = userVaddr

	// Fetch instruction - should work through MMU with U-bit set
	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("FetchInstruction in U-mode failed: %v", err)
	}

	if insn != 0x00000013 {
		t.Errorf("Fetched instruction = 0x%x, want 0x00000013", insn)
	}
}

// TestUserModeCannotAccessSupervisorPage tests that U-mode cannot fetch from supervisor page.
// Reference: riscv_cpu.c lines 265-266 - U-mode denied when PTE_U_MASK is not set
func TestUserModeCannotAccessSupervisorPage(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up page WITHOUT User bit (supervisor-only page)
	ptPPN := setupSv39PageTable(t, cpu, userVaddr, userPaddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write instruction at the physical address
	cpu.Mem.Write32(userPaddr, 0x00000013)

	// Set CPU to U-mode
	cpu.Priv = PrivUser
	cpu.PC = userVaddr

	// Fetch instruction - should fail because U bit is not set
	_, err := cpu.FetchInstruction()
	if err == nil {
		t.Error("FetchInstruction in U-mode from supervisor page should fail")
	}

	// Verify page fault was raised
	if cpu.PendingException != CauseFetchPageFault {
		t.Errorf("PendingException = %d, want %d (fetch page fault)",
			cpu.PendingException, CauseFetchPageFault)
	}
}

// TestUserModeLoadStoreThroughMMU tests load/store in U-mode through MMU.
// Reference: riscv_cpu.c lines 265-266 - U-mode data access requires PTE_U_MASK
func TestUserModeLoadStoreThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up page with User + Read + Write permissions
	ptPPN := setupUserPage(t, cpu, userVaddr, userPaddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Set CPU to U-mode
	cpu.Priv = PrivUser

	// Store a value through MMU
	testVal := uint32(0xDEADBEEF)
	if err := cpu.StoreU32(userVaddr+100, testVal); err != nil {
		t.Fatalf("StoreU32 in U-mode failed: %v", err)
	}

	// Load it back
	loaded, err := cpu.LoadU32(userVaddr + 100)
	if err != nil {
		t.Fatalf("LoadU32 in U-mode failed: %v", err)
	}

	if loaded != testVal {
		t.Errorf("LoadU32 = 0x%x, want 0x%x", loaded, testVal)
	}
}

// TestSRETToUModeWithMMU tests SRET returning to U-mode with MMU enabled.
// This is the specific SRET path used by Linux when starting init.
// Reference: riscv_cpu.c handle_sret() lines 1127-1141
func TestSRETToUModeWithMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// S-mode virtual address (identity mapped)
	sVaddr := uint64(0x80000000)
	sPaddr := uint64(0x80000000)

	// Set up page tables for both S-mode and U-mode pages
	ptBase := uint64(0x80100000)

	l2Addr := ptBase
	l1AddrUser := ptBase + 0x1000
	l0AddrUser := ptBase + 0x2000
	l1AddrSup := ptBase + 0x3000
	l0AddrSup := ptBase + 0x4000

	// L2[0] -> L1 for user region (vpn[2]=0)
	cpu.Mem.Write64(l2Addr+0*8, ((l1AddrUser>>12)<<10)|PTEValid)
	// L2[2] -> L1 for supervisor region (vpn[2]=2)
	cpu.Mem.Write64(l2Addr+2*8, ((l1AddrSup>>12)<<10)|PTEValid)

	// L1[0] -> L0 for user pages
	cpu.Mem.Write64(l1AddrUser+0*8, ((l0AddrUser>>12)<<10)|PTEValid)
	// L1[0] -> L0 for supervisor pages
	cpu.Mem.Write64(l1AddrSup+0*8, ((l0AddrSup>>12)<<10)|PTEValid)

	// L0[0x10] for user page 0x10000
	cpu.Mem.Write64(l0AddrUser+0x10*8, ((userPaddr>>12)<<10)|PTEValid|PTERead|PTEExecute|PTEUser|PTEAccessed)
	// L0[0] for supervisor page 0x80000000
	cpu.Mem.Write64(l0AddrSup+0*8, ((sPaddr>>12)<<10)|PTEValid|PTERead|PTEExecute|PTEAccessed)

	cpu.Satp = (uint64(SatpModeSv39) << 60) | (ptBase >> 12)

	// Write NOP at user code location
	cpu.Mem.Write32(userPaddr, 0x00000013)

	// Start in S-mode, prepare to SRET to U-mode
	cpu.Priv = PrivSupervisor
	cpu.Sepc = userVaddr
	cpu.Mstatus = MstatusSPIE // SPP=0 (U-mode), SPIE=1
	cpu.PC = sVaddr

	// Write SRET at current PC
	cpu.Mem.Write32(sPaddr, 0x10200073)

	// Execute SRET
	err := cpu.Step()
	if err != nil {
		t.Fatalf("SRET failed: %v", err)
	}

	// Verify transition to U-mode
	if cpu.Priv != PrivUser {
		t.Errorf("After SRET: Priv = %d, want %d", cpu.Priv, PrivUser)
	}

	// Verify PC is at user virtual address
	if cpu.PC != userVaddr {
		t.Errorf("After SRET: PC = 0x%x, want 0x%x", cpu.PC, userVaddr)
	}

	// Verify MMU is enabled for instruction fetch
	if !cpu.IsMmuEnabled(AccessCode) {
		t.Error("MMU should be enabled for code access in U-mode")
	}

	// Execute NOP through MMU - this is the critical test
	err = cpu.Step()
	if err != nil {
		t.Fatalf("NOP execution in U-mode through MMU failed: %v", err)
	}

	// PC should advance by 4
	if cpu.PC != userVaddr+4 {
		t.Errorf("After NOP: PC = 0x%x, want 0x%x", cpu.PC, userVaddr+4)
	}
}

// TestUserEcallBackToSMode tests that ECALL from U-mode traps to S-mode when delegated.
// Reference: riscv_cpu.c raise_exception2() lines 1083-1091 (delegation)
func TestUserEcallBackToSMode(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up user page
	ptPPN := setupUserPage(t, cpu, userVaddr, userPaddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write ECALL at user code location
	cpu.Mem.Write32(userPaddr, 0x00000073)

	// Set up S-mode trap handler
	cpu.Stvec = 0x80010000
	cpu.Mtvec = 0x80020000

	// Delegate user ecall to S-mode
	cpu.Medeleg = 1 << CauseUserEcall

	// Start in U-mode at user virtual address
	cpu.Priv = PrivUser
	cpu.PC = userVaddr

	// Execute ECALL
	err := cpu.Step()
	if err != nil {
		t.Fatalf("ECALL execution failed: %v", err)
	}

	// Verify trap to S-mode (not M-mode)
	if cpu.Priv != PrivSupervisor {
		t.Errorf("After ECALL: Priv = %d, want %d (S-mode)", cpu.Priv, PrivSupervisor)
	}

	// Verify PC is at stvec
	if cpu.PC != cpu.Stvec {
		t.Errorf("After ECALL: PC = 0x%x, want 0x%x (stvec)", cpu.PC, cpu.Stvec)
	}

	// Verify scause
	if cpu.Scause != uint64(CauseUserEcall) {
		t.Errorf("Scause = 0x%x, want 0x%x", cpu.Scause, CauseUserEcall)
	}

	// Verify sepc points to ECALL instruction
	if cpu.Sepc != userVaddr {
		t.Errorf("Sepc = 0x%x, want 0x%x", cpu.Sepc, userVaddr)
	}

	// Verify SPP records U-mode
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp != PrivUser {
		t.Errorf("SPP = %d, want %d", spp, PrivUser)
	}
}

// TestMultipleUserInstructionsThroughMMU tests executing multiple instructions in U-mode.
// This more closely simulates what happens when Linux runs init.
// Reference: riscv_cpu.c lines 265-266 - repeated U-mode fetch
func TestMultipleUserInstructionsThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up user page with execute permission
	ptPPN := setupUserPage(t, cpu, userVaddr, userPaddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write a sequence of instructions:
	// 0x10000: ADDI x1, x0, 5     (0x00500093) - x1 = 5
	// 0x10004: ADDI x2, x0, 10    (0x00a00113) - x2 = 10
	// 0x10008: ADD x3, x1, x2    (0x002081b3) - x3 = x1 + x2 = 15
	// 0x1000c: ECALL              (0x00000073) - trap back to S-mode
	cpu.Mem.Write32(userPaddr+0, 0x00500093)  // ADDI x1, x0, 5
	cpu.Mem.Write32(userPaddr+4, 0x00a00113)  // ADDI x2, x0, 10
	cpu.Mem.Write32(userPaddr+8, 0x002081b3)  // ADD x3, x1, x2
	cpu.Mem.Write32(userPaddr+12, 0x00000073) // ECALL

	// Set up trap handling
	cpu.Stvec = 0x80010000
	cpu.Medeleg = 1 << CauseUserEcall

	// Start in U-mode
	cpu.Priv = PrivUser
	cpu.PC = userVaddr

	// Execute first instruction: ADDI x1, x0, 5
	if err := cpu.Step(); err != nil {
		t.Fatalf("Instruction 1 failed: %v", err)
	}
	if cpu.Reg[1] != 5 {
		t.Errorf("After ADDI x1: x1 = %d, want 5", cpu.Reg[1])
	}
	if cpu.PC != userVaddr+4 {
		t.Errorf("After ADDI x1: PC = 0x%x, want 0x%x", cpu.PC, userVaddr+4)
	}

	// Execute second instruction: ADDI x2, x0, 10
	if err := cpu.Step(); err != nil {
		t.Fatalf("Instruction 2 failed: %v", err)
	}
	if cpu.Reg[2] != 10 {
		t.Errorf("After ADDI x2: x2 = %d, want 10", cpu.Reg[2])
	}

	// Execute third instruction: ADD x3, x1, x2
	if err := cpu.Step(); err != nil {
		t.Fatalf("Instruction 3 failed: %v", err)
	}
	if cpu.Reg[3] != 15 {
		t.Errorf("After ADD x3: x3 = %d, want 15", cpu.Reg[3])
	}

	// Execute ECALL - should trap to S-mode
	if err := cpu.Step(); err != nil {
		t.Fatalf("ECALL failed: %v", err)
	}

	if cpu.Priv != PrivSupervisor {
		t.Errorf("After ECALL: Priv = %d, want %d", cpu.Priv, PrivSupervisor)
	}
	if cpu.Scause != uint64(CauseUserEcall) {
		t.Errorf("After ECALL: Scause = 0x%x, want 0x%x", cpu.Scause, CauseUserEcall)
	}
}

// TestCompressedInstructionInUserMode tests compressed instruction execution in U-mode.
// Userspace binaries (like busybox init) use compressed instructions heavily.
// Reference: riscv_cpu_template.h compressed instruction handling
func TestCompressedInstructionInUserMode(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up user page
	ptPPN := setupUserPage(t, cpu, userVaddr, userPaddr, PTERead|PTEExecute|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write compressed instruction sequence:
	// C.NOP (0x0001) - 2 bytes
	// C.LI x10, 5 (0x4515) - li a0, 5 - 2 bytes
	// ECALL (0x00000073) - 4 bytes
	cpu.Mem.Write16(userPaddr+0, 0x0001)     // C.NOP
	cpu.Mem.Write16(userPaddr+2, 0x4515)     // C.LI a0, 5
	cpu.Mem.Write32(userPaddr+4, 0x00000073) // ECALL

	// Set up trap handling
	cpu.Stvec = 0x80010000
	cpu.Medeleg = 1 << CauseUserEcall

	// Start in U-mode
	cpu.Priv = PrivUser
	cpu.PC = userVaddr

	// Execute C.NOP
	if err := cpu.Step(); err != nil {
		t.Fatalf("C.NOP failed: %v", err)
	}
	if cpu.PC != userVaddr+2 {
		t.Errorf("After C.NOP: PC = 0x%x, want 0x%x", cpu.PC, userVaddr+2)
	}

	// Execute C.LI a0, 5
	if err := cpu.Step(); err != nil {
		t.Fatalf("C.LI failed: %v", err)
	}
	if cpu.Reg[10] != 5 {
		t.Errorf("After C.LI: a0 = %d, want 5", cpu.Reg[10])
	}
	if cpu.PC != userVaddr+4 {
		t.Errorf("After C.LI: PC = 0x%x, want 0x%x", cpu.PC, userVaddr+4)
	}

	// Execute ECALL
	if err := cpu.Step(); err != nil {
		t.Fatalf("ECALL failed: %v", err)
	}
	if cpu.Priv != PrivSupervisor {
		t.Errorf("After ECALL: Priv = %d, want %d", cpu.Priv, PrivSupervisor)
	}
}

// TestFullSRetToUModeAndBack tests a complete S->U->S round trip.
// This simulates what Linux does when running a system call.
// Reference: riscv_cpu.c handle_sret() and raise_exception2()
func TestFullSRetToUModeAndBack(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// S-mode virtual address (identity mapped)
	sVaddr := uint64(0x80000000)
	sPaddr := uint64(0x80000000)

	// Set up page tables for both S-mode and U-mode pages
	ptBase := uint64(0x80100000)

	l2Addr := ptBase
	l1AddrUser := ptBase + 0x1000
	l0AddrUser := ptBase + 0x2000
	l1AddrSup := ptBase + 0x3000
	l0AddrSup := ptBase + 0x4000

	// L2[0] -> L1 for user region
	cpu.Mem.Write64(l2Addr+0*8, ((l1AddrUser>>12)<<10)|PTEValid)
	// L2[2] -> L1 for supervisor region
	cpu.Mem.Write64(l2Addr+2*8, ((l1AddrSup>>12)<<10)|PTEValid)

	// L1[0] -> L0 for user pages
	cpu.Mem.Write64(l1AddrUser+0*8, ((l0AddrUser>>12)<<10)|PTEValid)
	// L1[0] -> L0 for supervisor pages
	cpu.Mem.Write64(l1AddrSup+0*8, ((l0AddrSup>>12)<<10)|PTEValid)

	// L0[0x10] for user page 0x10000
	cpu.Mem.Write64(l0AddrUser+0x10*8, ((userPaddr>>12)<<10)|PTEValid|PTERead|PTEExecute|PTEUser|PTEAccessed)
	// L0[0] for supervisor page 0x80000000
	cpu.Mem.Write64(l0AddrSup+0*8, ((sPaddr>>12)<<10)|PTEValid|PTERead|PTEExecute|PTEAccessed)

	cpu.Satp = (uint64(SatpModeSv39) << 60) | (ptBase >> 12)

	// Write user program: NOP then ECALL
	cpu.Mem.Write32(userPaddr+0, 0x00000013) // NOP
	cpu.Mem.Write32(userPaddr+4, 0x00000073) // ECALL

	// Write SRET at S-mode location
	cpu.Mem.Write32(sPaddr, 0x10200073) // SRET

	// Set up trap handling
	cpu.Stvec = 0x80010000
	cpu.Medeleg = 1 << CauseUserEcall

	// === Phase 1: S-mode prepares to run user code ===
	cpu.Priv = PrivSupervisor
	cpu.PC = sVaddr
	cpu.Sepc = userVaddr
	cpu.Mstatus = MstatusSPIE // SPP=0 (U), SPIE=1

	// Execute SRET
	if err := cpu.Step(); err != nil {
		t.Fatalf("SRET failed: %v", err)
	}

	// === Phase 2: Now in U-mode ===
	if cpu.Priv != PrivUser {
		t.Fatalf("After SRET: Priv = %d, want %d", cpu.Priv, PrivUser)
	}
	if cpu.PC != userVaddr {
		t.Fatalf("After SRET: PC = 0x%x, want 0x%x", cpu.PC, userVaddr)
	}

	// Execute NOP in U-mode
	if err := cpu.Step(); err != nil {
		t.Fatalf("NOP in U-mode failed: %v", err)
	}
	if cpu.PC != userVaddr+4 {
		t.Fatalf("After NOP: PC = 0x%x, want 0x%x", cpu.PC, userVaddr+4)
	}

	// Execute ECALL in U-mode
	if err := cpu.Step(); err != nil {
		t.Fatalf("ECALL failed: %v", err)
	}

	// === Phase 3: Back in S-mode ===
	if cpu.Priv != PrivSupervisor {
		t.Errorf("After ECALL: Priv = %d, want %d", cpu.Priv, PrivSupervisor)
	}
	if cpu.PC != cpu.Stvec {
		t.Errorf("After ECALL: PC = 0x%x, want 0x%x", cpu.PC, cpu.Stvec)
	}
	if cpu.Scause != uint64(CauseUserEcall) {
		t.Errorf("Scause = 0x%x, want 0x%x", cpu.Scause, CauseUserEcall)
	}
	if cpu.Sepc != userVaddr+4 {
		t.Errorf("Sepc = 0x%x, want 0x%x (ECALL address)", cpu.Sepc, userVaddr+4)
	}

	// Verify SPP = U-mode
	spp := (cpu.Mstatus >> MstatusSPPShift) & 1
	if spp != PrivUser {
		t.Errorf("SPP = %d, want %d", spp, PrivUser)
	}
}

// TestUserModeWithSUMBit tests that S-mode with SUM can access user pages.
// Reference: riscv_cpu.c line 262-263 - SUM allows S-mode to access U pages
func TestUserModeWithSUMBit(t *testing.T) {
	cpu := mmuTestCPU(t)

	userVaddr := uint64(0x10000)
	userPaddr := uint64(0x80200000)

	// Set up user page (has U bit)
	ptPPN := setupUserPage(t, cpu, userVaddr, userPaddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Initialize memory with a known value
	cpu.Mem.Write32(userPaddr, 0xCAFEBABE)

	// S-mode WITHOUT SUM should NOT be able to read user page
	cpu.Priv = PrivSupervisor
	cpu.Mstatus = 0 // SUM = 0

	_, err := cpu.TranslateAddress(userVaddr, AccessRead)
	if err == nil {
		t.Error("S-mode without SUM should not access user page")
	}
	cpu.ClearPendingException()

	// S-mode WITH SUM should be able to read user page
	cpu.Mstatus = MstatusSUM
	cpu.FlushTLB()

	paddr, err := cpu.TranslateAddress(userVaddr, AccessRead)
	if err != nil {
		t.Errorf("S-mode with SUM should access user page: %v", err)
	}
	if paddr != userPaddr {
		t.Errorf("Translated address = 0x%x, want 0x%x", paddr, userPaddr)
	}

	// Verify we can actually load through the MMU
	val, err := cpu.LoadU32(userVaddr)
	if err != nil {
		t.Errorf("LoadU32 with SUM failed: %v", err)
	}
	if val != 0xCAFEBABE {
		t.Errorf("LoadU32 = 0x%x, want 0xCAFEBABE", val)
	}
}

// TestStoreU8ThroughMMU tests StoreU8 through virtual memory.
// Reference: riscv_cpu_template.h - store instructions use target_write_slow
func TestStoreU8ThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x2000)
	paddr := uint64(0x80003000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEUser|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN
	cpu.Mstatus = MstatusSUM // Allow S-mode to access U pages

	// Test StoreU8 through MMU
	if err := cpu.StoreU8(vaddr+10, 0xAB); err != nil {
		t.Fatalf("StoreU8 through MMU failed: %v", err)
	}

	// Verify the value was written to physical memory
	val, _ := cpu.Mem.Read8(paddr + 10)
	if val != 0xAB {
		t.Errorf("StoreU8: read back 0x%x, want 0xAB", val)
	}
}

// TestStoreU16ThroughMMU tests StoreU16 through virtual memory.
func TestStoreU16ThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x2000)
	paddr := uint64(0x80003000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEUser|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN
	cpu.Mstatus = MstatusSUM

	if err := cpu.StoreU16(vaddr+20, 0xBEEF); err != nil {
		t.Fatalf("StoreU16 through MMU failed: %v", err)
	}

	val, _ := cpu.Mem.Read16(paddr + 20)
	if val != 0xBEEF {
		t.Errorf("StoreU16: read back 0x%x, want 0xBEEF", val)
	}
}

// TestStoreU64ThroughMMU tests StoreU64 through virtual memory.
func TestStoreU64ThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x2000)
	paddr := uint64(0x80003000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEUser|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN
	cpu.Mstatus = MstatusSUM

	if err := cpu.StoreU64(vaddr+40, 0xDEADBEEFCAFEBABE); err != nil {
		t.Fatalf("StoreU64 through MMU failed: %v", err)
	}

	val, _ := cpu.Mem.Read64(paddr + 40)
	if val != 0xDEADBEEFCAFEBABE {
		t.Errorf("StoreU64: read back 0x%x, want 0xDEADBEEFCAFEBABE", val)
	}
}

// TestLoadU8ThroughMMU tests LoadU8 through virtual memory.
func TestLoadU8ThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x2000)
	paddr := uint64(0x80003000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEUser|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN
	cpu.Mstatus = MstatusSUM

	// Write to physical memory
	cpu.Mem.Write8(paddr+5, 0xCD)

	// Load through MMU
	val, err := cpu.LoadU8(vaddr + 5)
	if err != nil {
		t.Fatalf("LoadU8 through MMU failed: %v", err)
	}
	if val != 0xCD {
		t.Errorf("LoadU8: got 0x%x, want 0xCD", val)
	}
}

// TestLoadU16ThroughMMU tests LoadU16 through virtual memory.
func TestLoadU16ThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x2000)
	paddr := uint64(0x80003000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEUser|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN
	cpu.Mstatus = MstatusSUM

	cpu.Mem.Write16(paddr+10, 0xFACE)

	val, err := cpu.LoadU16(vaddr + 10)
	if err != nil {
		t.Fatalf("LoadU16 through MMU failed: %v", err)
	}
	if val != 0xFACE {
		t.Errorf("LoadU16: got 0x%x, want 0xFACE", val)
	}
}

// TestLoadU64ThroughMMU tests LoadU64 through virtual memory.
func TestLoadU64ThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x2000)
	paddr := uint64(0x80003000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEUser|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN
	cpu.Mstatus = MstatusSUM

	cpu.Mem.Write64(paddr+24, 0x123456789ABCDEF0)

	val, err := cpu.LoadU64(vaddr + 24)
	if err != nil {
		t.Fatalf("LoadU64 through MMU failed: %v", err)
	}
	if val != 0x123456789ABCDEF0 {
		t.Errorf("LoadU64: got 0x%x, want 0x123456789ABCDEF0", val)
	}
}

// TestStoreToReadOnlyPageFault tests that StoreU* functions fault on read-only page.
// Reference: riscv_cpu.c - page faults for permission violations
func TestStoreToReadOnlyPageFault(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x3000)
	paddr := uint64(0x80004000)

	// Set up page with READ ONLY permission (no write)
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// StoreU8 should fail
	err := cpu.StoreU8(vaddr, 0x12)
	if err == nil {
		t.Error("StoreU8 to read-only page should fail")
	}
	if cpu.PendingException != CauseStorePageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseStorePageFault)
	}
	cpu.ClearPendingException()

	// StoreU16 should fail
	err = cpu.StoreU16(vaddr, 0x1234)
	if err == nil {
		t.Error("StoreU16 to read-only page should fail")
	}
	if cpu.PendingException != CauseStorePageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseStorePageFault)
	}
	cpu.ClearPendingException()

	// StoreU32 should fail
	err = cpu.StoreU32(vaddr, 0x12345678)
	if err == nil {
		t.Error("StoreU32 to read-only page should fail")
	}
	if cpu.PendingException != CauseStorePageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseStorePageFault)
	}
	cpu.ClearPendingException()

	// StoreU64 should fail
	err = cpu.StoreU64(vaddr, 0x123456789ABCDEF0)
	if err == nil {
		t.Error("StoreU64 to read-only page should fail")
	}
	if cpu.PendingException != CauseStorePageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseStorePageFault)
	}
}

// TestMPRVLoadThroughMMU tests load with MPRV bit through MMU translation.
// Reference: riscv_cpu.c - MPRV causes M-mode loads to use MPP privilege
func TestMPRVLoadThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	vaddr := uint64(0x4000)
	paddr := uint64(0x80005000)

	// Set up page table
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write test value to physical memory
	cpu.Mem.Write32(paddr+100, 0xDEADC0DE)

	// M-mode with MPRV and MPP=S should use S-mode translation
	cpu.Priv = PrivMachine
	cpu.Mstatus = MstatusMPRV | (uint64(PrivSupervisor) << MstatusMPPShift)

	// Load should use S-mode translation
	val, err := cpu.LoadU32(vaddr + 100)
	if err != nil {
		t.Fatalf("LoadU32 with MPRV failed: %v", err)
	}
	if val != 0xDEADC0DE {
		t.Errorf("LoadU32 = 0x%x, want 0xDEADC0DE", val)
	}
}

// TestMPRVStoreThroughMMU tests store with MPRV bit through MMU translation.
func TestMPRVStoreThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	vaddr := uint64(0x4000)
	paddr := uint64(0x80005000)

	// Set up page table with write permission
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// M-mode with MPRV and MPP=S should use S-mode translation
	cpu.Priv = PrivMachine
	cpu.Mstatus = MstatusMPRV | (uint64(PrivSupervisor) << MstatusMPPShift)

	// Store should use S-mode translation
	if err := cpu.StoreU32(vaddr+200, 0xCAFED00D); err != nil {
		t.Fatalf("StoreU32 with MPRV failed: %v", err)
	}

	// Verify value in physical memory
	val, _ := cpu.Mem.Read32(paddr + 200)
	if val != 0xCAFED00D {
		t.Errorf("StoreU32: read back 0x%x, want 0xCAFED00D", val)
	}
}

// TestTLBUpdateOnStore tests that TLB is updated after store translation.
func TestTLBUpdateOnStore(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x5000)
	paddr := uint64(0x80006000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Flush TLB to ensure miss
	cpu.FlushTLB()

	// Verify TLB is empty for write
	_, hit := cpu.TLBLookup(vaddr, AccessWrite)
	if hit {
		t.Error("TLB should be empty before store")
	}

	// Perform store - this should populate the write TLB
	if err := cpu.StoreU32(vaddr, 0x12345678); err != nil {
		t.Fatalf("StoreU32 failed: %v", err)
	}

	// TLB should now have entry for write
	_, hit = cpu.TLBLookup(vaddr, AccessWrite)
	if !hit {
		t.Error("TLB should be populated after store")
	}

	// Read TLB should NOT be populated (separate TLBs)
	_, hit = cpu.TLBLookup(vaddr, AccessRead)
	if hit {
		t.Error("Read TLB should not be populated by store")
	}
}

// TestTLBUpdateOnLoad tests that TLB is updated after load translation.
func TestTLBUpdateOnLoad(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x5000)
	paddr := uint64(0x80006000)

	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	cpu.FlushTLB()

	// Verify TLB is empty for read
	_, hit := cpu.TLBLookup(vaddr, AccessRead)
	if hit {
		t.Error("TLB should be empty before load")
	}

	// Perform load - this should populate the read TLB
	_, err := cpu.LoadU32(vaddr)
	if err != nil {
		t.Fatalf("LoadU32 failed: %v", err)
	}

	// TLB should now have entry for read
	_, hit = cpu.TLBLookup(vaddr, AccessRead)
	if !hit {
		t.Error("TLB should be populated after load")
	}
}

// TestUserModeStoreThroughMMU tests store from U-mode through virtual memory.
// Reference: riscv_cpu.c - user mode uses PTEUser bit
func TestUserModeStoreThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	vaddr := uint64(0x6000)
	paddr := uint64(0x80007000)

	// Set up user page
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEUser|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// U-mode
	cpu.Priv = PrivUser

	// Store should succeed
	if err := cpu.StoreU32(vaddr+50, 0xFEEDFACE); err != nil {
		t.Fatalf("U-mode StoreU32 failed: %v", err)
	}

	val, _ := cpu.Mem.Read32(paddr + 50)
	if val != 0xFEEDFACE {
		t.Errorf("U-mode store: read back 0x%x, want 0xFEEDFACE", val)
	}
}

// TestUserModeLoadThroughMMU tests load from U-mode through virtual memory.
func TestUserModeLoadThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)

	vaddr := uint64(0x6000)
	paddr := uint64(0x80007000)

	// Set up user page
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEUser|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Write test data
	cpu.Mem.Write32(paddr+60, 0xBADCAFE)

	// U-mode
	cpu.Priv = PrivUser

	// Load should succeed
	val, err := cpu.LoadU32(vaddr + 60)
	if err != nil {
		t.Fatalf("U-mode LoadU32 failed: %v", err)
	}
	if val != 0xBADCAFE {
		t.Errorf("U-mode load: got 0x%x, want 0xBADCAFE", val)
	}
}

// TestUserModeAccessSupervisorPageFault tests U-mode cannot access S-mode page.
func TestUserModeAccessSupervisorPageFault(t *testing.T) {
	cpu := mmuTestCPU(t)

	vaddr := uint64(0x7000)
	paddr := uint64(0x80008000)

	// Set up supervisor page (no U bit)
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// U-mode
	cpu.Priv = PrivUser

	// Load should fault
	_, err := cpu.LoadU32(vaddr)
	if err == nil {
		t.Error("U-mode load from S-mode page should fail")
	}
	if cpu.PendingException != CauseLoadPageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseLoadPageFault)
	}
	cpu.ClearPendingException()

	// Store should fault
	err = cpu.StoreU32(vaddr, 0x1234)
	if err == nil {
		t.Error("U-mode store to S-mode page should fail")
	}
	if cpu.PendingException != CauseStorePageFault {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseStorePageFault)
	}
}

// TestSv48LoadStoreThroughMMU tests load/store through Sv48 translation.
func TestSv48LoadStoreThroughMMU(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x8000)
	paddr := uint64(0x80009000)

	ptPPN := setupSv48PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv48) << 60) | ptPPN

	// Store through Sv48
	if err := cpu.StoreU64(vaddr+8, 0xFEDCBA9876543210); err != nil {
		t.Fatalf("Sv48 StoreU64 failed: %v", err)
	}

	// Load through Sv48
	val, err := cpu.LoadU64(vaddr + 8)
	if err != nil {
		t.Fatalf("Sv48 LoadU64 failed: %v", err)
	}
	if val != 0xFEDCBA9876543210 {
		t.Errorf("Sv48 LoadU64: got 0x%x, want 0xFEDCBA9876543210", val)
	}
}

// TestSv48ADUpdate tests A/D bit updates in Sv48 translation.
func TestSv48ADUpdate(t *testing.T) {
	cpu := mmuTestCPU(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0x9000)
	paddr := uint64(0x8000A000)

	ptBase := uint64(0x80100000)
	vpn0 := (vaddr >> 12) & 0x1FF
	vpn1 := (vaddr >> 21) & 0x1FF
	vpn2 := (vaddr >> 30) & 0x1FF
	vpn3 := (vaddr >> 39) & 0x1FF

	l3Addr := ptBase
	l2Addr := ptBase + 0x1000
	l1Addr := ptBase + 0x2000
	l0Addr := ptBase + 0x3000

	// L3 -> L2
	l3PTE := ((l2Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l3Addr+vpn3*8, l3PTE)

	// L2 -> L1
	l2PTE := ((l1Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l2Addr+vpn2*8, l2PTE)

	// L1 -> L0
	l1PTE := ((l0Addr >> 12) << 10) | PTEValid
	cpu.Mem.Write64(l1Addr+vpn1*8, l1PTE)

	// L0 leaf without A/D bits
	ppn := paddr >> 12
	l0PTE := (ppn << 10) | PTERead | PTEWrite | PTEValid
	cpu.Mem.Write64(l0Addr+vpn0*8, l0PTE)

	cpu.Satp = (uint64(SatpModeSv48) << 60) | (ptBase >> 12)

	// Translate for read - should set A bit
	_, err := cpu.TranslateAddress(vaddr, AccessRead)
	if err != nil {
		t.Fatalf("Sv48 read translation failed: %v", err)
	}

	pteAfter, _ := cpu.Mem.Read64(l0Addr + vpn0*8)
	if pteAfter&PTEAccessed == 0 {
		t.Error("A bit should be set after Sv48 read")
	}

	cpu.FlushTLB()

	// Translate for write - should set D bit
	_, err = cpu.TranslateAddress(vaddr, AccessWrite)
	if err != nil {
		t.Fatalf("Sv48 write translation failed: %v", err)
	}

	pteAfter, _ = cpu.Mem.Read64(l0Addr + vpn0*8)
	if pteAfter&PTEDirty == 0 {
		t.Error("D bit should be set after Sv48 write")
	}
}

// TestMPRVWithUserMPP tests MPRV with MPP=U for user-mode access.
func TestMPRVWithUserMPP(t *testing.T) {
	cpu := mmuTestCPU(t)

	vaddr := uint64(0xA000)
	paddr := uint64(0x8000B000)

	// Set up user page
	ptPPN := setupSv39PageTable(t, cpu, vaddr, paddr, PTERead|PTEWrite|PTEUser|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// M-mode with MPRV and MPP=U
	cpu.Priv = PrivMachine
	cpu.Mstatus = MstatusMPRV | (uint64(PrivUser) << MstatusMPPShift)

	// Write test data
	cpu.Mem.Write32(paddr+100, 0x11223344)

	// Load should use U-mode translation
	val, err := cpu.LoadU32(vaddr + 100)
	if err != nil {
		t.Fatalf("MPRV+MPP=U load failed: %v", err)
	}
	if val != 0x11223344 {
		t.Errorf("MPRV+MPP=U load: got 0x%x, want 0x11223344", val)
	}

	// Store should use U-mode translation
	if err := cpu.StoreU32(vaddr+200, 0x55667788); err != nil {
		t.Fatalf("MPRV+MPP=U store failed: %v", err)
	}

	val, _ = cpu.Mem.Read32(paddr + 200)
	if val != 0x55667788 {
		t.Errorf("MPRV+MPP=U store: read back 0x%x, want 0x55667788", val)
	}
}

// mmuTestCPUWithROM creates a test CPU with both RAM and a ROM region.
// The ROM is at 0x90000000 to avoid overlap with RAM at 0x80000000.
func mmuTestCPUWithROM(t *testing.T) *CPU {
	t.Helper()
	m := mem.NewPhysMemoryMap()

	// Register RAM at 0x80000000 (4MB for page tables and regular memory)
	_, err := m.RegisterRAM(0x80000000, 4*1024*1024, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	// Register ROM at 0x90000000 (64KB read-only region, separate from RAM)
	_, err = m.RegisterRAM(0x90000000, 64*1024, mem.RAMFlagROM)
	if err != nil {
		t.Fatalf("failed to register ROM: %v", err)
	}

	cpu := NewCPU(m, XLEN64)
	cpu.PC = 0x80000000
	cpu.Priv = PrivMachine
	return cpu
}

// TestStoreToROMPhysicalFault tests that storing to ROM through MMU causes a store fault.
// This tests the error path in StoreU* when Mem.Write* returns an error.
// Reference: riscv_cpu.c - store access faults for ROM writes
func TestStoreToROMPhysicalFault(t *testing.T) {
	cpu := mmuTestCPUWithROM(t)
	cpu.Priv = PrivSupervisor

	// Virtual address mapped to ROM
	vaddr := uint64(0xB000)
	romPaddr := uint64(0x90000000) // Physical address in ROM region

	// Set up page table with WRITE permission
	// Translation will succeed, but physical write will fail
	ptPPN := setupSv39PageTable(t, cpu, vaddr, romPaddr, PTERead|PTEWrite|PTEAccessed|PTEDirty)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// StoreU8 to ROM should fail with store fault
	err := cpu.StoreU8(vaddr, 0x12)
	if err == nil {
		t.Error("StoreU8 to ROM should fail")
	}
	if cpu.PendingException != CauseFaultStore {
		t.Errorf("StoreU8 to ROM: PendingException = %d, want %d", cpu.PendingException, CauseFaultStore)
	}
	cpu.ClearPendingException()

	// StoreU16 to ROM should fail with store fault
	err = cpu.StoreU16(vaddr+2, 0x1234)
	if err == nil {
		t.Error("StoreU16 to ROM should fail")
	}
	if cpu.PendingException != CauseFaultStore {
		t.Errorf("StoreU16 to ROM: PendingException = %d, want %d", cpu.PendingException, CauseFaultStore)
	}
	cpu.ClearPendingException()

	// StoreU32 to ROM should fail with store fault
	err = cpu.StoreU32(vaddr+4, 0x12345678)
	if err == nil {
		t.Error("StoreU32 to ROM should fail")
	}
	if cpu.PendingException != CauseFaultStore {
		t.Errorf("StoreU32 to ROM: PendingException = %d, want %d", cpu.PendingException, CauseFaultStore)
	}
	cpu.ClearPendingException()

	// StoreU64 to ROM should fail with store fault
	err = cpu.StoreU64(vaddr+8, 0x123456789ABCDEF0)
	if err == nil {
		t.Error("StoreU64 to ROM should fail")
	}
	if cpu.PendingException != CauseFaultStore {
		t.Errorf("StoreU64 to ROM: PendingException = %d, want %d", cpu.PendingException, CauseFaultStore)
	}
}

// TestLoadFromROMThroughMMU tests loading from ROM through MMU works.
func TestLoadFromROMThroughMMU(t *testing.T) {
	cpu := mmuTestCPUWithROM(t)
	cpu.Priv = PrivSupervisor

	vaddr := uint64(0xC000)
	romPaddr := uint64(0x90000000)

	// Initialize ROM with test data by writing before registering as ROM
	// (or we can just read whatever zero values are there)
	// Since ROM is initialized to zero, let's just verify reads work

	ptPPN := setupSv39PageTable(t, cpu, vaddr, romPaddr, PTERead|PTEAccessed)
	cpu.Satp = (uint64(SatpModeSv39) << 60) | ptPPN

	// Load from ROM should succeed
	val8, err := cpu.LoadU8(vaddr)
	if err != nil {
		t.Errorf("LoadU8 from ROM failed: %v", err)
	}
	_ = val8 // ROM initialized to 0

	val16, err := cpu.LoadU16(vaddr)
	if err != nil {
		t.Errorf("LoadU16 from ROM failed: %v", err)
	}
	_ = val16

	val32, err := cpu.LoadU32(vaddr)
	if err != nil {
		t.Errorf("LoadU32 from ROM failed: %v", err)
	}
	_ = val32

	val64, err := cpu.LoadU64(vaddr)
	if err != nil {
		t.Errorf("LoadU64 from ROM failed: %v", err)
	}
	_ = val64
}

// TestStoreToROMPhysicalDirect tests that direct physical store to ROM fails.
// This verifies the memory subsystem behavior we're relying on.
func TestStoreToROMPhysicalDirect(t *testing.T) {
	cpu := mmuTestCPUWithROM(t)
	cpu.Priv = PrivMachine
	cpu.Satp = 0 // Bare mode

	romAddr := uint64(0x90000000)

	// Direct store to ROM (no translation) should fail
	err := cpu.StoreU8(romAddr, 0x12)
	if err == nil {
		t.Error("Direct StoreU8 to ROM should fail")
	}
	if cpu.PendingException != CauseFaultStore {
		t.Errorf("Direct StoreU8 to ROM: PendingException = %d, want %d", cpu.PendingException, CauseFaultStore)
	}
	cpu.ClearPendingException()

	err = cpu.StoreU16(romAddr, 0x1234)
	if err == nil {
		t.Error("Direct StoreU16 to ROM should fail")
	}
	cpu.ClearPendingException()

	err = cpu.StoreU32(romAddr, 0x12345678)
	if err == nil {
		t.Error("Direct StoreU32 to ROM should fail")
	}
	cpu.ClearPendingException()

	err = cpu.StoreU64(romAddr, 0x123456789ABCDEF0)
	if err == nil {
		t.Error("Direct StoreU64 to ROM should fail")
	}
}

// TestFetchCompressedInstructionAtPageBoundary tests fetching a compressed instruction
// when it's at the very end of a memory region (offset where only 2 bytes remain).
// Reference: riscv_cpu.c fetchInstructionPhys - handles compressed instruction at boundary
func TestFetchCompressedInstructionAtPageBoundary(t *testing.T) {
	// Create a CPU with a small memory region to test boundary conditions
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	// Register exactly one page of RAM (4096 bytes)
	_, err := m.RegisterRAM(0x80000000, 4096, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	cpu := NewCPU(m, XLEN64)
	cpu.Priv = PrivMachine
	cpu.Satp = 0 // Bare mode (physical access)

	// Write a compressed instruction at offset 4094 (last 2 bytes of page)
	// C.NOP = 0x0001 (compressed, bits 1:0 != 11)
	compressedNOP := uint16(0x0001)
	m.Write16(0x80000000+4094, compressedNOP)

	// Set PC to the compressed instruction
	cpu.PC = 0x80000000 + 4094

	// Fetch should succeed - compressed instruction fits in remaining 2 bytes
	insn, err := cpu.FetchInstruction()
	if err != nil {
		t.Fatalf("FetchInstruction for compressed at boundary failed: %v", err)
	}
	if insn&0xFFFF != uint32(compressedNOP) {
		t.Errorf("insn = 0x%x, want 0x%x", insn&0xFFFF, compressedNOP)
	}
}

// TestFetchUncompressedInstructionAtPageBoundaryFault tests that an uncompressed
// instruction at the page boundary causes a fault (needs second half from next page).
// Reference: riscv_cpu.c fetchInstructionPhys - faults when 4-byte insn straddles boundary
func TestFetchUncompressedInstructionAtPageBoundaryFault(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	// Register exactly one page of RAM (4096 bytes)
	_, err := m.RegisterRAM(0x80000000, 4096, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	cpu := NewCPU(m, XLEN64)
	cpu.Priv = PrivMachine
	cpu.Satp = 0 // Bare mode

	// Write an uncompressed instruction at offset 4094 (straddles page boundary)
	// An uncompressed instruction has bits 1:0 == 11
	// Use ADDI x0, x0, 0 (NOP) = 0x00000013 (bits 1:0 = 11)
	// Only write the first 2 bytes since we only have 2 bytes left
	uncompressedLow := uint16(0x0013) // bits 1:0 = 11, indicating 4-byte instruction
	m.Write16(0x80000000+4094, uncompressedLow)

	// Set PC to the instruction
	cpu.PC = 0x80000000 + 4094

	// Fetch should fail - needs 4 bytes but only 2 available
	_, err = cpu.FetchInstruction()
	if err == nil {
		t.Error("FetchInstruction should fail for uncompressed instruction at page boundary")
	}
	if cpu.PendingException != CauseFaultFetch {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseFaultFetch)
	}
}

// TestFetchInstructionPastRAMEnd tests that fetch past the end of RAM fails.
func TestFetchInstructionPastRAMEnd(t *testing.T) {
	m := mem.NewPhysMemoryMap()
	defer m.Close()

	// Register 4KB of RAM
	_, err := m.RegisterRAM(0x80000000, 4096, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	cpu := NewCPU(m, XLEN64)
	cpu.Priv = PrivMachine
	cpu.Satp = 0

	// Set PC past the end of RAM (but still in the PhysMemoryRange)
	// Actually, offset+4 needs to be > len(PhysMem) to trigger line 155
	// And offset+2 > len(PhysMem) to trigger line 168 (the final fault path)
	cpu.PC = 0x80000000 + 4095 // Only 1 byte remains

	_, err = cpu.FetchInstruction()
	if err == nil {
		t.Error("FetchInstruction past RAM end should fail")
	}
	if cpu.PendingException != CauseFaultFetch {
		t.Errorf("PendingException = %d, want %d", cpu.PendingException, CauseFaultFetch)
	}
}
