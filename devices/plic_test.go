package devices

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func TestPLICNew(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	if plic == nil {
		t.Fatal("NewPLIC returned nil")
	}

	// Initial pending should be 0
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("initial pending = 0x%x, want 0", plic.GetPendingIRQ())
	}

	// Initial served should be 0
	if plic.GetServedIRQ() != 0 {
		t.Errorf("initial served = 0x%x, want 0", plic.GetServedIRQ())
	}
}

func TestPLICSetIRQ(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Set IRQ 1
	plic.SetIRQ(1, 1)
	if plic.GetPendingIRQ() != 1 {
		t.Errorf("pending = 0x%x, want 0x1", plic.GetPendingIRQ())
	}
	if intCtrl.mip&(MipMEIP|MipSEIP) == 0 {
		t.Error("External interrupt pending bits should be set")
	}

	// Set IRQ 5
	plic.SetIRQ(5, 1)
	if plic.GetPendingIRQ() != 0x11 { // bits 0 and 4 (IRQ 1 -> bit 0, IRQ 5 -> bit 4)
		t.Errorf("pending = 0x%x, want 0x11", plic.GetPendingIRQ())
	}

	// Clear IRQ 1
	plic.SetIRQ(1, 0)
	if plic.GetPendingIRQ() != 0x10 { // only bit 4 (IRQ 5)
		t.Errorf("pending = 0x%x, want 0x10", plic.GetPendingIRQ())
	}

	// Clear IRQ 5
	plic.SetIRQ(5, 0)
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("pending = 0x%x, want 0x0", plic.GetPendingIRQ())
	}
	if intCtrl.mip&(MipMEIP|MipSEIP) != 0 {
		t.Error("External interrupt pending bits should be cleared")
	}
}

func TestPLICSetIRQBoundary(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// IRQ 0 is reserved, should be ignored
	plic.SetIRQ(0, 1)
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("IRQ 0 should be ignored, pending = 0x%x", plic.GetPendingIRQ())
	}

	// IRQ 31 should work
	plic.SetIRQ(31, 1)
	if plic.GetPendingIRQ() != 0x40000000 { // bit 30 (IRQ 31 -> bit 30)
		t.Errorf("pending = 0x%x, want 0x40000000", plic.GetPendingIRQ())
	}

	// IRQ 32 is out of range, should be ignored
	plic.SetIRQ(32, 1)
	if plic.GetPendingIRQ() != 0x40000000 {
		t.Errorf("IRQ 32 should be ignored, pending = 0x%x", plic.GetPendingIRQ())
	}

	// Negative IRQ should be ignored
	plic.SetIRQ(-1, 1)
	if plic.GetPendingIRQ() != 0x40000000 {
		t.Errorf("negative IRQ should be ignored, pending = 0x%x", plic.GetPendingIRQ())
	}
}

func TestPLICClaimComplete(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Set multiple IRQs
	plic.SetIRQ(3, 1)
	plic.SetIRQ(7, 1)
	plic.SetIRQ(1, 1)

	// Claim should return lowest IRQ (1)
	claimed := plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 1 {
		t.Errorf("first claim = %d, want 1", claimed)
	}
	if plic.GetServedIRQ() != 1 { // bit 0 for IRQ 1
		t.Errorf("served = 0x%x, want 0x1", plic.GetServedIRQ())
	}

	// Claim again should return next lowest (3)
	claimed = plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 3 {
		t.Errorf("second claim = %d, want 3", claimed)
	}
	if plic.GetServedIRQ() != 0x5 { // bits 0 and 2 for IRQ 1 and 3
		t.Errorf("served = 0x%x, want 0x5", plic.GetServedIRQ())
	}

	// External interrupt should still be pending (IRQ 7 not served yet)
	if intCtrl.mip&(MipMEIP|MipSEIP) == 0 {
		t.Error("External interrupt pending bits should still be set (IRQ 7 not served)")
	}

	// Claim the last one (7)
	claimed = plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 7 {
		t.Errorf("third claim = %d, want 7", claimed)
	}

	// All served, so external interrupt should be cleared
	if intCtrl.mip&(MipMEIP|MipSEIP) != 0 {
		t.Error("External interrupt pending bits should be cleared when all served")
	}

	// Claim with nothing pending should return 0
	claimed = plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 0 {
		t.Errorf("claim with nothing pending = %d, want 0", claimed)
	}

	// Complete IRQ 1
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 1, 2)
	if plic.GetServedIRQ() != 0x44 { // bits 2 and 6 for IRQ 3 and 7
		t.Errorf("served after complete 1 = 0x%x, want 0x44", plic.GetServedIRQ())
	}

	// Since IRQ 1 is still pending and no longer served, external interrupt should be set again
	if intCtrl.mip&(MipMEIP|MipSEIP) == 0 {
		t.Error("External interrupt pending bits should be set after completing IRQ 1")
	}

	// Complete all and clear pending
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 3, 2)
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 7, 2)
	plic.SetIRQ(1, 0)
	plic.SetIRQ(3, 0)
	plic.SetIRQ(7, 0)

	if plic.GetServedIRQ() != 0 {
		t.Errorf("served after all complete = 0x%x, want 0", plic.GetServedIRQ())
	}
	if intCtrl.mip&(MipMEIP|MipSEIP) != 0 {
		t.Error("External interrupt pending bits should be cleared")
	}
}

func TestPLICThreshold(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Read threshold at PLIC_HART_BASE (should always return 0 per C code)
	// Reference: riscv_machine.c:263-265
	threshold := plic.Read(nil, PLICHartBase+PLICThresholdOffset, 2)
	if threshold != 0 {
		t.Errorf("threshold read = %d, want 0", threshold)
	}

	// Write threshold (should be ignored per C code)
	// Reference: riscv_machine.c:284-301 (only PLIC_HART_BASE+4 handled)
	plic.Write(nil, PLICHartBase+PLICThresholdOffset, 5, 2)
	threshold = plic.Read(nil, PLICHartBase+PLICThresholdOffset, 2)
	if threshold != 0 {
		t.Errorf("threshold read after write = %d, want 0 (writes ignored)", threshold)
	}
}

func TestPLICCreateIRQs(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	irqs := plic.CreateIRQs()

	// IRQ 0 should be nil (reserved)
	if irqs[0] != nil {
		t.Error("IRQ 0 should be nil")
	}

	// IRQ 1-31 should be valid
	for i := 1; i < PLICMaxIRQ; i++ {
		if irqs[i] == nil {
			t.Errorf("IRQ %d should not be nil", i)
		}
		if irqs[i].IRQNum() != i {
			t.Errorf("IRQ %d has number %d", i, irqs[i].IRQNum())
		}
	}

	// Test raising IRQ via IRQSignal
	irqs[5].Raise()
	if plic.GetPendingIRQ() != 0x10 { // bit 4 for IRQ 5
		t.Errorf("pending after IRQ 5 raise = 0x%x, want 0x10", plic.GetPendingIRQ())
	}

	// Test lowering IRQ via IRQSignal
	irqs[5].Lower()
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("pending after IRQ 5 lower = 0x%x, want 0", plic.GetPendingIRQ())
	}
}

func TestPLICRegister(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)
	memMap := mem.NewPhysMemoryMap()

	pr, err := plic.Register(memMap)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if pr.Addr != PLICBaseAddr {
		t.Errorf("addr = 0x%x, want 0x%x", pr.Addr, PLICBaseAddr)
	}
	if pr.Size != PLICSize {
		t.Errorf("size = 0x%x, want 0x%x", pr.Size, PLICSize)
	}
	if pr.IsRAM {
		t.Error("PLIC should not be RAM")
	}
}

func TestPLICRegisterAt(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)
	memMap := mem.NewPhysMemoryMap()

	customAddr := uint64(0x50000000)
	pr, err := plic.RegisterAt(memMap, customAddr)
	if err != nil {
		t.Fatalf("RegisterAt failed: %v", err)
	}

	if pr.Addr != customAddr {
		t.Errorf("addr = 0x%x, want 0x%x", pr.Addr, customAddr)
	}
}

func TestPLICMemMapAccess(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)
	memMap := mem.NewPhysMemoryMap()

	_, err := plic.Register(memMap)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Set an IRQ
	plic.SetIRQ(2, 1)

	// Claim via memory map
	claimAddr := uint64(PLICBaseAddr + PLICHartBase + PLICClaimOffset)
	val, err := memMap.Read32(claimAddr)
	if err != nil {
		t.Fatalf("Read32 failed: %v", err)
	}
	if val != 2 {
		t.Errorf("claimed IRQ = %d, want 2", val)
	}

	// Complete via memory map
	err = memMap.Write32(claimAddr, 2)
	if err != nil {
		t.Fatalf("Write32 failed: %v", err)
	}
	if plic.GetServedIRQ() != 0 {
		t.Errorf("served = 0x%x, want 0 after complete", plic.GetServedIRQ())
	}
}

func TestPLICNilInterruptController(t *testing.T) {
	// PLIC should work even without an interrupt controller
	plic := NewPLIC(nil)

	// These should not panic
	plic.SetIRQ(1, 1)
	plic.SetIRQ(1, 0)

	// Claim/complete should work
	plic.SetIRQ(5, 1)
	claimed := plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 5 {
		t.Errorf("claim = %d, want 5", claimed)
	}
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 5, 2)
}

func TestPLICUnusedOffset(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Reading from unused offset should return 0
	val := plic.Read(nil, 0x100, 2)
	if val != 0 {
		t.Errorf("unused offset read = %d, want 0", val)
	}

	// Writing to unused offset should not panic
	plic.Write(nil, 0x100, 0x12345678, 2)
}

func TestPLICPriorityRegisters(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Write priority for source 1 (offset 0x4)
	plic.Write(nil, 0x4, 7, 2)
	val := plic.Read(nil, 0x4, 2)
	if val != 7 {
		t.Errorf("priority[1] = %d, want 7", val)
	}

	// Write priority for source 5 (offset 0x14)
	plic.Write(nil, 0x14, 3, 2)
	val = plic.Read(nil, 0x14, 2)
	if val != 3 {
		t.Errorf("priority[5] = %d, want 3", val)
	}
}

func TestPLICEnableRegister(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Write enable bits (offset 0x2000)
	plic.Write(nil, 0x2000, 0xFFFFFFFF, 2)
	val := plic.Read(nil, 0x2000, 2)
	if val != 0xFFFFFFFF {
		t.Errorf("enable = 0x%x, want 0xFFFFFFFF", val)
	}
}

func TestPLICPendingRegister(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Set some IRQs
	plic.SetIRQ(1, 1)
	plic.SetIRQ(5, 1)

	// Read pending bits (offset 0x1000)
	val := plic.Read(nil, 0x1000, 2)
	expected := uint32(0x11) // bits 0 and 4
	if val != expected {
		t.Errorf("pending register = 0x%x, want 0x%x", val, expected)
	}
}

func TestPLICConstants(t *testing.T) {
	// Verify PLIC constants match RISC-V spec
	if PLICBaseAddr != 0x40100000 {
		t.Errorf("PLICBaseAddr = 0x%x, want 0x40100000", PLICBaseAddr)
	}
	if PLICSize != 0x00400000 {
		t.Errorf("PLICSize = 0x%x, want 0x00400000", PLICSize)
	}
	if PLICHartBase != 0x200000 {
		t.Errorf("PLICHartBase = 0x%x, want 0x200000", PLICHartBase)
	}

	// Verify MIP bit positions for external interrupts
	if MipMEIP != 1<<11 {
		t.Errorf("MipMEIP = 0x%x, want 0x%x", MipMEIP, 1<<11)
	}
	if MipSEIP != 1<<9 {
		t.Errorf("MipSEIP = 0x%x, want 0x%x", MipSEIP, 1<<9)
	}
}

func TestPLICCompleteInvalidIRQ(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Set and claim an IRQ
	plic.SetIRQ(5, 1)
	plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)

	// Try to complete an invalid IRQ (too high)
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 100, 2)
	// Should not crash and served should be unchanged
	if plic.GetServedIRQ() != 0x10 { // bit 4 for IRQ 5
		t.Errorf("served = 0x%x, want 0x10 (unchanged)", plic.GetServedIRQ())
	}

	// Try to complete IRQ 0 (should be handled gracefully)
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 0, 2)
}

func TestPLICMultipleClaimsSameIRQ(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Set one IRQ
	plic.SetIRQ(3, 1)

	// Claim it
	claimed := plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 3 {
		t.Errorf("first claim = %d, want 3", claimed)
	}

	// Try to claim again while it's being served (should return 0)
	claimed = plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 0 {
		t.Errorf("second claim = %d, want 0 (already served)", claimed)
	}

	// Complete and claim again
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 3, 2)
	claimed = plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if claimed != 3 {
		t.Errorf("claim after complete = %d, want 3", claimed)
	}
}

func TestPLICInterruptUpdateTiming(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := NewPLIC(intCtrl)

	// Initially no external interrupts
	if intCtrl.mip&(MipMEIP|MipSEIP) != 0 {
		t.Error("Initially should have no external interrupts pending")
	}

	// Set IRQ - should immediately raise external interrupt
	plic.SetIRQ(1, 1)
	if intCtrl.mip&(MipMEIP|MipSEIP) == 0 {
		t.Error("External interrupt should be set immediately when IRQ raised")
	}

	// Claim - should clear external interrupt (all served)
	plic.Read(nil, PLICHartBase+PLICClaimOffset, 2)
	if intCtrl.mip&(MipMEIP|MipSEIP) != 0 {
		t.Error("External interrupt should be cleared when all IRQs served")
	}

	// Complete - should re-raise external interrupt (IRQ still pending)
	plic.Write(nil, PLICHartBase+PLICClaimOffset, 1, 2)
	if intCtrl.mip&(MipMEIP|MipSEIP) == 0 {
		t.Error("External interrupt should be re-raised when IRQ completed but still pending")
	}

	// Clear IRQ - should clear external interrupt
	plic.SetIRQ(1, 0)
	if intCtrl.mip&(MipMEIP|MipSEIP) != 0 {
		t.Error("External interrupt should be cleared when IRQ cleared")
	}
}
