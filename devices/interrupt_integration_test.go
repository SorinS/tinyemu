package devices_test

import (
	"encoding/binary"
	"testing"

	"github.com/sorins/tinyemu-go/devices"
	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/virtio"
)

// mockInterruptController tracks MIP set/reset calls for testing.
// Implements the devices.InterruptController interface.
type mockInterruptController struct {
	mip    uint32
	cycles uint64
}

func (m *mockInterruptController) SetMIP(mask uint32) {
	m.mip |= mask
}

func (m *mockInterruptController) ResetMIP(mask uint32) {
	m.mip &^= mask
}

func (m *mockInterruptController) GetMIP() uint32 {
	return m.mip
}

func (m *mockInterruptController) GetCycles() uint64 {
	return m.cycles
}

// Helper to write to VirtIO MMIO registers via memory map
func writeMMIO(memMap *mem.PhysMemoryMap, devAddr uint64, offset uint64, val uint32) {
	memMap.Write32(devAddr+offset, val)
}

// TestVirtIOPLICCPUInterruptPath verifies the complete interrupt delivery chain:
// VirtIO Device -> IRQSignal -> PLIC -> CPU MIP
//
// This test exercises the full interrupt path that occurs when a VirtIO device
// completes processing a descriptor and signals the CPU.
//
// Reference: riscv_machine.c (virtio_mmio_set_irq -> plic_set_irq -> cpu MIP)
func TestVirtIOPLICCPUInterruptPath(t *testing.T) {
	// 1. Create a mock interrupt controller (represents the CPU)
	intCtrl := &mockInterruptController{}

	// 2. Create PLIC and connect it to the mock CPU
	plic := devices.NewPLIC(intCtrl)

	// 3. Create IRQ signals from the PLIC
	irqs := plic.CreateIRQs()

	// Use IRQ 1 for our VirtIO device (IRQ 0 is reserved)
	virtioIRQ := irqs[1]

	// 4. Set up memory for VirtIO
	memMap := mem.NewPhysMemoryMap()
	ramBase := uint64(0x80000000)
	_, err := memMap.RegisterRAM(ramBase, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	// 5. Create a VirtIO device with the PLIC IRQ
	virtioAddr := uint64(0x10000000)
	dev, err := virtio.NewDevice(memMap, virtioAddr, virtioIRQ, virtio.DeviceIDBlock, 0, nil)
	if err != nil {
		t.Fatalf("failed to create VirtIO device: %v", err)
	}
	_ = dev // We'll use the MMIO interface instead

	// 6. Set up a virtqueue via MMIO writes
	descAddr := ramBase
	availAddr := ramBase + 0x1000
	usedAddr := ramBase + 0x2000

	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueSel, 0)
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueNum, 4)
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueDescLow, uint32(descAddr))
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueDescHigh, uint32(descAddr>>32))
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueAvailLow, uint32(availAddr))
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueAvailHigh, uint32(availAddr>>32))
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueUsedLow, uint32(usedAddr))
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueUsedHigh, uint32(usedAddr>>32))
	writeMMIO(memMap, virtioAddr, virtio.MMIOQueueReady, 1)

	// 7. Set up a simple descriptor in memory
	ram := memMap.GetRAMPtr(ramBase, true)
	// Descriptor 0: read buffer at 0x80003000, 64 bytes
	binary.LittleEndian.PutUint64(ram[0:], 0x80003000) // addr
	binary.LittleEndian.PutUint32(ram[8:], 64)         // len
	binary.LittleEndian.PutUint16(ram[12:], 0)         // flags (no chain)
	binary.LittleEndian.PutUint16(ram[14:], 0)         // next

	// Verify initial state: no pending interrupts, no MIP
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("initial PLIC pending = 0x%x, want 0", plic.GetPendingIRQ())
	}
	if intCtrl.GetMIP() != 0 {
		t.Errorf("initial MIP = 0x%x, want 0", intCtrl.GetMIP())
	}

	// 8. Trigger ConsumeDesc - this should raise the interrupt through the chain
	dev.ConsumeDesc(0, 0, 64)

	// 9. Verify the interrupt propagated through the chain

	// Check PLIC pending bit is set (IRQ 1 maps to bit 0 in pendingIRQ)
	expectedPending := uint32(1) << 0 // IRQ 1 -> bit 0
	if plic.GetPendingIRQ() != expectedPending {
		t.Errorf("PLIC pending = 0x%x, want 0x%x", plic.GetPendingIRQ(), expectedPending)
	}

	// Check CPU MIP has external interrupt pending bits set
	expectedMIP := uint32(devices.MipMEIP | devices.MipSEIP)
	if intCtrl.GetMIP() != expectedMIP {
		t.Errorf("CPU MIP = 0x%x, want 0x%x (MEIP|SEIP)", intCtrl.GetMIP(), expectedMIP)
	}

	// 10. Test the claim/complete cycle

	// Claim the interrupt (simulates CPU reading PLIC claim register)
	claimedIRQ := plic.Read(nil, devices.PLICHartBase+devices.PLICClaimOffset, 2)
	if claimedIRQ != 1 {
		t.Errorf("claimed IRQ = %d, want 1", claimedIRQ)
	}

	// After claim, PLIC should still show pending but MIP should be cleared
	// because the interrupt is now being served
	if plic.GetPendingIRQ() != expectedPending {
		t.Errorf("PLIC pending after claim = 0x%x, want 0x%x", plic.GetPendingIRQ(), expectedPending)
	}
	if plic.GetServedIRQ() != expectedPending {
		t.Errorf("PLIC served after claim = 0x%x, want 0x%x", plic.GetServedIRQ(), expectedPending)
	}

	// MIP should be cleared since no unserved interrupts remain
	if intCtrl.GetMIP() != 0 {
		t.Errorf("MIP after claim = 0x%x, want 0 (no unserved interrupts)", intCtrl.GetMIP())
	}

	// Complete the interrupt (simulates CPU writing to PLIC complete register)
	plic.Write(nil, devices.PLICHartBase+devices.PLICClaimOffset, 1, 2)

	// After complete, served should be cleared
	if plic.GetServedIRQ() != 0 {
		t.Errorf("PLIC served after complete = 0x%x, want 0", plic.GetServedIRQ())
	}

	// Pending is still set (interrupt source hasn't lowered yet)
	// MIP should be set again since there's a pending unserved interrupt
	if intCtrl.GetMIP() != expectedMIP {
		t.Errorf("MIP after complete = 0x%x, want 0x%x (pending unserved)", intCtrl.GetMIP(), expectedMIP)
	}

	// 11. Clear the interrupt at the source (acknowledge via VirtIO MMIO)
	// This would normally happen when the guest acknowledges the interrupt
	writeMMIO(memMap, virtioAddr, virtio.MMIOInterruptAck, 1)
	virtioIRQ.Lower()

	// Now pending should be cleared
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("PLIC pending after lower = 0x%x, want 0", plic.GetPendingIRQ())
	}
	if intCtrl.GetMIP() != 0 {
		t.Errorf("MIP after lower = 0x%x, want 0", intCtrl.GetMIP())
	}
}

// TestMultipleVirtIOInterrupts verifies handling of multiple VirtIO devices
// raising interrupts through the same PLIC.
func TestMultipleVirtIOInterrupts(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := devices.NewPLIC(intCtrl)
	irqs := plic.CreateIRQs()

	memMap := mem.NewPhysMemoryMap()
	ramBase := uint64(0x80000000)
	_, err := memMap.RegisterRAM(ramBase, 0x20000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	// Create two VirtIO devices with different IRQ numbers
	virtioAddr1 := uint64(0x10000000)
	virtioAddr2 := uint64(0x10001000)

	dev1, err := virtio.NewDevice(memMap, virtioAddr1, irqs[1], virtio.DeviceIDBlock, 0, nil)
	if err != nil {
		t.Fatalf("failed to create VirtIO device 1: %v", err)
	}

	dev2, err := virtio.NewDevice(memMap, virtioAddr2, irqs[2], virtio.DeviceIDConsole, 0, nil)
	if err != nil {
		t.Fatalf("failed to create VirtIO device 2: %v", err)
	}

	// Set up queues for both devices
	setupQueue := func(devAddr uint64, baseOffset uint64) {
		descAddr := ramBase + baseOffset
		availAddr := ramBase + baseOffset + 0x1000
		usedAddr := ramBase + baseOffset + 0x2000

		writeMMIO(memMap, devAddr, virtio.MMIOQueueSel, 0)
		writeMMIO(memMap, devAddr, virtio.MMIOQueueNum, 4)
		writeMMIO(memMap, devAddr, virtio.MMIOQueueDescLow, uint32(descAddr))
		writeMMIO(memMap, devAddr, virtio.MMIOQueueDescHigh, uint32(descAddr>>32))
		writeMMIO(memMap, devAddr, virtio.MMIOQueueAvailLow, uint32(availAddr))
		writeMMIO(memMap, devAddr, virtio.MMIOQueueAvailHigh, uint32(availAddr>>32))
		writeMMIO(memMap, devAddr, virtio.MMIOQueueUsedLow, uint32(usedAddr))
		writeMMIO(memMap, devAddr, virtio.MMIOQueueUsedHigh, uint32(usedAddr>>32))
		writeMMIO(memMap, devAddr, virtio.MMIOQueueReady, 1)

		// Set up a descriptor
		ram := memMap.GetRAMPtr(descAddr, true)
		binary.LittleEndian.PutUint64(ram[0:], descAddr+0x3000)
		binary.LittleEndian.PutUint32(ram[8:], 64)
	}

	setupQueue(virtioAddr1, 0)
	setupQueue(virtioAddr2, 0x8000)

	// Both devices raise interrupts
	dev1.ConsumeDesc(0, 0, 64)
	dev2.ConsumeDesc(0, 0, 64)

	// PLIC should show both IRQs pending
	expectedPending := uint32(0x3) // IRQ 1 and 2 -> bits 0 and 1
	if plic.GetPendingIRQ() != expectedPending {
		t.Errorf("PLIC pending = 0x%x, want 0x%x", plic.GetPendingIRQ(), expectedPending)
	}

	// MIP should be set
	if intCtrl.GetMIP() != (devices.MipMEIP | devices.MipSEIP) {
		t.Errorf("MIP = 0x%x, want 0x%x", intCtrl.GetMIP(), devices.MipMEIP|devices.MipSEIP)
	}

	// Claim first interrupt (should get lowest numbered = IRQ 1)
	claimed := plic.Read(nil, devices.PLICHartBase+devices.PLICClaimOffset, 2)
	if claimed != 1 {
		t.Errorf("first claim = %d, want 1", claimed)
	}

	// MIP should still be set (IRQ 2 still pending)
	if intCtrl.GetMIP() != (devices.MipMEIP | devices.MipSEIP) {
		t.Errorf("MIP after first claim = 0x%x, want 0x%x", intCtrl.GetMIP(), devices.MipMEIP|devices.MipSEIP)
	}

	// Complete IRQ 1
	plic.Write(nil, devices.PLICHartBase+devices.PLICClaimOffset, 1, 2)

	// Lower IRQ 1 at source
	irqs[1].Lower()

	// Claim second interrupt (should get IRQ 2)
	claimed = plic.Read(nil, devices.PLICHartBase+devices.PLICClaimOffset, 2)
	if claimed != 2 {
		t.Errorf("second claim = %d, want 2", claimed)
	}

	// MIP should be cleared (no unserved interrupts)
	if intCtrl.GetMIP() != 0 {
		t.Errorf("MIP after second claim = 0x%x, want 0", intCtrl.GetMIP())
	}

	// Complete IRQ 2 and lower source
	plic.Write(nil, devices.PLICHartBase+devices.PLICClaimOffset, 2, 2)
	irqs[2].Lower()

	// All clear
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("final pending = 0x%x, want 0", plic.GetPendingIRQ())
	}
	if intCtrl.GetMIP() != 0 {
		t.Errorf("final MIP = 0x%x, want 0", intCtrl.GetMIP())
	}
}

// TestInterruptPathWithoutClaim verifies that interrupts remain pending
// until claimed, matching the edge-triggered behavior in TinyEMU.
func TestInterruptPathWithoutClaim(t *testing.T) {
	intCtrl := &mockInterruptController{}
	plic := devices.NewPLIC(intCtrl)
	irqs := plic.CreateIRQs()

	// Raise and lower an interrupt without claiming
	irqs[1].Raise()

	// Should be pending and MIP set
	if plic.GetPendingIRQ() != 1 {
		t.Errorf("pending after raise = 0x%x, want 0x1", plic.GetPendingIRQ())
	}
	if intCtrl.GetMIP() != (devices.MipMEIP | devices.MipSEIP) {
		t.Errorf("MIP after raise = 0x%x, want 0x%x", intCtrl.GetMIP(), devices.MipMEIP|devices.MipSEIP)
	}

	// Lower without claiming
	irqs[1].Lower()

	// Pending should be cleared, MIP cleared
	if plic.GetPendingIRQ() != 0 {
		t.Errorf("pending after lower = 0x%x, want 0", plic.GetPendingIRQ())
	}
	if intCtrl.GetMIP() != 0 {
		t.Errorf("MIP after lower = 0x%x, want 0", intCtrl.GetMIP())
	}
}
