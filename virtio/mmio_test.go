package virtio

import (
	"encoding/binary"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// testIRQState tracks IRQ state for testing
type testIRQState struct {
	level int
	count int // Number of times IRQ was changed
}

func newTestIRQ() (*mem.IRQSignal, *testIRQState) {
	state := &testIRQState{}
	return mem.NewIRQSignal(func(opaque any, irqNum int, level int) {
		s := opaque.(*testIRQState)
		s.level = level
		s.count++
	}, state, 1), state
}

// newTestDevice creates a VirtIO device for testing
func newTestDevice(t *testing.T, deviceID uint32, configSize uint32) (*Device, *mem.PhysMemoryMap, *testIRQState) {
	t.Helper()

	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000 for descriptor tables, etc.
	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	dev, err := NewDevice(memMap, 0x10000000, irq, deviceID, configSize, nil)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	return dev, memMap, irqState
}

func TestMMIOMagicAndVersion(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Read magic value
	magic := dev.read(nil, MMIOMagicValue, 2)
	if magic != MMIOMagic {
		t.Errorf("magic value = 0x%x, want 0x%x", magic, MMIOMagic)
	}

	// Read version
	version := dev.read(nil, MMIOVersion, 2)
	if version != MMIOVersionVal {
		t.Errorf("version = %d, want %d", version, MMIOVersionVal)
	}
}

func TestMMIODeviceID(t *testing.T) {
	tests := []struct {
		name     string
		deviceID uint32
	}{
		{"net", DeviceIDNet},
		{"block", DeviceIDBlock},
		{"console", DeviceIDConsole},
		{"9p", DeviceID9P},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev, _, _ := newTestDevice(t, tt.deviceID, 0)

			id := dev.read(nil, MMIODeviceID, 2)
			if id != tt.deviceID {
				t.Errorf("device ID = %d, want %d", id, tt.deviceID)
			}
		})
	}
}

func TestMMIOVendorID(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	vendorID := dev.read(nil, MMIOVendorID, 2)
	if vendorID != 0xffff {
		t.Errorf("vendor ID = 0x%x, want 0xffff", vendorID)
	}
}

func TestMMIOFeatures(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Set device features
	dev.SetFeatures(0xDEADBEEF)

	// Read features with selector = 0
	dev.write(nil, MMIODeviceFeaturesSel, 0, 2)
	features := dev.read(nil, MMIODeviceFeatures, 2)
	if features != 0xDEADBEEF {
		t.Errorf("features[0] = 0x%x, want 0xDEADBEEF", features)
	}

	// Read features with selector = 1 (should return 1 for VirtIO version)
	dev.write(nil, MMIODeviceFeaturesSel, 1, 2)
	features = dev.read(nil, MMIODeviceFeatures, 2)
	if features != 1 {
		t.Errorf("features[1] = %d, want 1", features)
	}

	// Read features with selector = 2 (should return 0)
	dev.write(nil, MMIODeviceFeaturesSel, 2, 2)
	features = dev.read(nil, MMIODeviceFeatures, 2)
	if features != 0 {
		t.Errorf("features[2] = %d, want 0", features)
	}
}

func TestMMIOQueueSelection(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Select queue 0
	dev.write(nil, MMIOQueueSel, 0, 2)
	sel := dev.read(nil, MMIOQueueSel, 2)
	if sel != 0 {
		t.Errorf("queue sel = %d, want 0", sel)
	}

	// Select queue 3
	dev.write(nil, MMIOQueueSel, 3, 2)
	sel = dev.read(nil, MMIOQueueSel, 2)
	if sel != 3 {
		t.Errorf("queue sel = %d, want 3", sel)
	}

	// Try to select invalid queue (should not change)
	dev.write(nil, MMIOQueueSel, MaxQueue+1, 2)
	sel = dev.read(nil, MMIOQueueSel, 2)
	if sel != 3 {
		t.Errorf("queue sel = %d, want 3 (unchanged)", sel)
	}
}

func TestMMIOQueueNumMax(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	numMax := dev.read(nil, MMIOQueueNumMax, 2)
	if numMax != MaxQueueNum {
		t.Errorf("queue num max = %d, want %d", numMax, MaxQueueNum)
	}
}

func TestMMIOQueueNum(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Initial queue size should be MaxQueueNum
	num := dev.read(nil, MMIOQueueNum, 2)
	if num != MaxQueueNum {
		t.Errorf("initial queue num = %d, want %d", num, MaxQueueNum)
	}

	// Set queue size to 8 (power of 2)
	dev.write(nil, MMIOQueueNum, 8, 2)
	num = dev.read(nil, MMIOQueueNum, 2)
	if num != 8 {
		t.Errorf("queue num = %d, want 8", num)
	}

	// Try to set queue size to 7 (not power of 2, should be ignored)
	dev.write(nil, MMIOQueueNum, 7, 2)
	num = dev.read(nil, MMIOQueueNum, 2)
	if num != 8 {
		t.Errorf("queue num = %d, want 8 (unchanged)", num)
	}

	// Try to set queue size to 0 (should be ignored)
	dev.write(nil, MMIOQueueNum, 0, 2)
	num = dev.read(nil, MMIOQueueNum, 2)
	if num != 8 {
		t.Errorf("queue num = %d, want 8 (unchanged)", num)
	}
}

func TestMMIOQueueAddresses(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Set queue descriptor address
	dev.write(nil, MMIOQueueDescLow, 0x12345678, 2)
	dev.write(nil, MMIOQueueDescHigh, 0xABCD0000, 2)

	// Read back
	descLow := dev.read(nil, MMIOQueueDescLow, 2)
	descHigh := dev.read(nil, MMIOQueueDescHigh, 2)
	descAddr := uint64(descLow) | (uint64(descHigh) << 32)

	if descAddr != 0xABCD000012345678 {
		t.Errorf("desc addr = 0x%x, want 0xABCD000012345678", descAddr)
	}

	// Set queue available address
	dev.write(nil, MMIOQueueAvailLow, 0x11111111, 2)
	dev.write(nil, MMIOQueueAvailHigh, 0x22222222, 2)

	availLow := dev.read(nil, MMIOQueueAvailLow, 2)
	availHigh := dev.read(nil, MMIOQueueAvailHigh, 2)
	availAddr := uint64(availLow) | (uint64(availHigh) << 32)

	if availAddr != 0x2222222211111111 {
		t.Errorf("avail addr = 0x%x, want 0x2222222211111111", availAddr)
	}

	// Set queue used address
	dev.write(nil, MMIOQueueUsedLow, 0x33333333, 2)
	dev.write(nil, MMIOQueueUsedHigh, 0x44444444, 2)

	usedLow := dev.read(nil, MMIOQueueUsedLow, 2)
	usedHigh := dev.read(nil, MMIOQueueUsedHigh, 2)
	usedAddr := uint64(usedLow) | (uint64(usedHigh) << 32)

	if usedAddr != 0x4444444433333333 {
		t.Errorf("used addr = 0x%x, want 0x4444444433333333", usedAddr)
	}
}

func TestMMIOQueueReady(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Initially not ready
	ready := dev.read(nil, MMIOQueueReady, 2)
	if ready != 0 {
		t.Errorf("initial queue ready = %d, want 0", ready)
	}

	// Set ready
	dev.write(nil, MMIOQueueReady, 1, 2)
	ready = dev.read(nil, MMIOQueueReady, 2)
	if ready != 1 {
		t.Errorf("queue ready = %d, want 1", ready)
	}

	// Clear ready (value should be masked to 1 bit)
	dev.write(nil, MMIOQueueReady, 0xFE, 2)
	ready = dev.read(nil, MMIOQueueReady, 2)
	if ready != 0 {
		t.Errorf("queue ready = %d, want 0 (masked)", ready)
	}
}

func TestMMIOStatus(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Initial status should be 0
	status := dev.read(nil, MMIOStatus, 2)
	if status != 0 {
		t.Errorf("initial status = %d, want 0", status)
	}

	// Set status
	dev.write(nil, MMIOStatus, 0x0F, 2)
	status = dev.read(nil, MMIOStatus, 2)
	if status != 0x0F {
		t.Errorf("status = 0x%x, want 0x0F", status)
	}
}

func TestMMIOReset(t *testing.T) {
	dev, _, irqState := newTestDevice(t, DeviceIDConsole, 0)

	// Set up some state
	dev.write(nil, MMIOQueueSel, 2, 2)
	dev.write(nil, MMIOQueueNum, 8, 2)
	dev.write(nil, MMIOQueueReady, 1, 2)
	dev.write(nil, MMIOQueueDescLow, 0x12345678, 2)
	dev.write(nil, MMIOStatus, 0x0F, 2)
	dev.write(nil, MMIODeviceFeaturesSel, 1, 2)

	// Raise interrupt
	dev.IntStatus = 1
	dev.IRQ.Raise()

	// Reset by writing 0 to status
	dev.write(nil, MMIOStatus, 0, 2)

	// Verify reset
	if dev.read(nil, MMIOStatus, 2) != 0 {
		t.Error("status not reset")
	}
	if dev.read(nil, MMIOQueueSel, 2) != 0 {
		t.Error("queue sel not reset")
	}
	if dev.read(nil, MMIODeviceFeaturesSel, 2) != 0 {
		t.Error("device features sel not reset")
	}

	// Check that queue state was reset
	dev.write(nil, MMIOQueueSel, 2, 2)
	if dev.read(nil, MMIOQueueNum, 2) != MaxQueueNum {
		t.Error("queue num not reset")
	}
	if dev.read(nil, MMIOQueueReady, 2) != 0 {
		t.Error("queue ready not reset")
	}
	if dev.read(nil, MMIOQueueDescLow, 2) != 0 {
		t.Error("queue desc addr not reset")
	}

	// Check IRQ was lowered
	if irqState.level != 0 {
		t.Error("IRQ not lowered on reset")
	}
}

func TestMMIOInterrupt(t *testing.T) {
	dev, _, irqState := newTestDevice(t, DeviceIDConsole, 0)

	// Initially no interrupt
	status := dev.read(nil, MMIOInterruptStatus, 2)
	if status != 0 {
		t.Errorf("initial interrupt status = %d, want 0", status)
	}

	// Simulate interrupt (normally done internally)
	dev.mu.Lock()
	dev.IntStatus = 3
	dev.IRQ.Raise()
	dev.mu.Unlock()

	// Read interrupt status
	status = dev.read(nil, MMIOInterruptStatus, 2)
	if status != 3 {
		t.Errorf("interrupt status = %d, want 3", status)
	}

	// Acknowledge partial interrupt
	dev.write(nil, MMIOInterruptAck, 1, 2)
	status = dev.read(nil, MMIOInterruptStatus, 2)
	if status != 2 {
		t.Errorf("interrupt status after ack = %d, want 2", status)
	}

	// IRQ should still be raised
	if irqState.level != 1 {
		t.Error("IRQ should still be raised")
	}

	// Acknowledge remaining interrupt
	dev.write(nil, MMIOInterruptAck, 2, 2)
	status = dev.read(nil, MMIOInterruptStatus, 2)
	if status != 0 {
		t.Errorf("interrupt status after full ack = %d, want 0", status)
	}

	// IRQ should be lowered
	if irqState.level != 0 {
		t.Error("IRQ should be lowered")
	}
}

func TestMMIOConfigSpace(t *testing.T) {
	configSize := uint32(16)
	dev, _, _ := newTestDevice(t, DeviceIDConsole, configSize)

	// Set configuration space
	dev.ConfigSpace[0] = 0x11
	dev.ConfigSpace[1] = 0x22
	dev.ConfigSpace[2] = 0x33
	dev.ConfigSpace[3] = 0x44

	// Read byte
	val := dev.read(nil, MMIOConfig+0, 0)
	if val != 0x11 {
		t.Errorf("config[0] = 0x%x, want 0x11", val)
	}

	// Read 16-bit
	val = dev.read(nil, MMIOConfig+0, 1)
	if val != 0x2211 {
		t.Errorf("config[0:2] = 0x%x, want 0x2211", val)
	}

	// Read 32-bit
	val = dev.read(nil, MMIOConfig+0, 2)
	if val != 0x44332211 {
		t.Errorf("config[0:4] = 0x%x, want 0x44332211", val)
	}

	// Write byte
	dev.write(nil, MMIOConfig+4, 0xAA, 0)
	if dev.ConfigSpace[4] != 0xAA {
		t.Errorf("config[4] = 0x%x, want 0xAA", dev.ConfigSpace[4])
	}

	// Write 16-bit
	dev.write(nil, MMIOConfig+6, 0xBBCC, 1)
	if dev.ConfigSpace[6] != 0xCC || dev.ConfigSpace[7] != 0xBB {
		t.Errorf("config[6:8] = 0x%x%x, want 0xCCBB", dev.ConfigSpace[7], dev.ConfigSpace[6])
	}

	// Write 32-bit
	dev.write(nil, MMIOConfig+8, 0xDDEEFF00, 2)
	expected := []byte{0x00, 0xFF, 0xEE, 0xDD}
	for i, exp := range expected {
		if dev.ConfigSpace[8+i] != exp {
			t.Errorf("config[%d] = 0x%x, want 0x%x", 8+i, dev.ConfigSpace[8+i], exp)
		}
	}
}

func TestMMIOConfigWriteCallback(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 16)

	callCount := 0
	dev.ConfigWrite = func(d *Device) {
		callCount++
	}

	// Write byte
	dev.write(nil, MMIOConfig+0, 0x11, 0)
	if callCount != 1 {
		t.Errorf("config write callback count = %d, want 1", callCount)
	}

	// Write 16-bit
	dev.write(nil, MMIOConfig+2, 0x2233, 1)
	if callCount != 2 {
		t.Errorf("config write callback count = %d, want 2", callCount)
	}

	// Write 32-bit
	dev.write(nil, MMIOConfig+4, 0x44556677, 2)
	if callCount != 3 {
		t.Errorf("config write callback count = %d, want 3", callCount)
	}
}

func TestMMIOConfigOutOfBounds(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 8)

	// Read beyond config space should return 0
	val := dev.read(nil, MMIOConfig+8, 0)
	if val != 0 {
		t.Errorf("out of bounds read = %d, want 0", val)
	}

	// 16-bit read at boundary
	val = dev.read(nil, MMIOConfig+7, 1)
	if val != 0 {
		t.Errorf("boundary 16-bit read = %d, want 0", val)
	}

	// 32-bit read at boundary
	val = dev.read(nil, MMIOConfig+5, 2)
	if val != 0 {
		t.Errorf("boundary 32-bit read = %d, want 0", val)
	}
}

func TestMMIONon32BitAccess(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Non-32-bit reads to non-config registers should return 0
	val := dev.read(nil, MMIOMagicValue, 0) // 8-bit
	if val != 0 {
		t.Errorf("8-bit magic read = %d, want 0", val)
	}

	val = dev.read(nil, MMIOMagicValue, 1) // 16-bit
	if val != 0 {
		t.Errorf("16-bit magic read = %d, want 0", val)
	}

	// Non-32-bit writes should be ignored
	dev.write(nil, MMIOStatus, 0xFF, 0) // 8-bit
	status := dev.read(nil, MMIOStatus, 2)
	if status != 0 {
		t.Errorf("status after 8-bit write = %d, want 0", status)
	}
}

func TestMMIOConfigGeneration(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDConsole, 0)

	gen := dev.read(nil, MMIOConfigGeneration, 2)
	if gen != 0 {
		t.Errorf("config generation = %d, want 0", gen)
	}
}

func TestQueueDescriptorChain(t *testing.T) {
	dev, memMap, irqState := newTestDevice(t, DeviceIDConsole, 0)

	// Set up queue addresses at RAM base
	ramBase := uint64(0x80000000)
	descAddr := ramBase
	availAddr := ramBase + 0x1000
	usedAddr := ramBase + 0x2000

	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(descAddr), 2)
	dev.write(nil, MMIOQueueDescHigh, uint32(descAddr>>32), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(availAddr), 2)
	dev.write(nil, MMIOQueueAvailHigh, uint32(availAddr>>32), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(usedAddr), 2)
	dev.write(nil, MMIOQueueUsedHigh, uint32(usedAddr>>32), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	// Set up a simple descriptor chain: desc 0 -> desc 1 (read) + desc 2 (write)
	ram := memMap.GetRAMPtr(ramBase, true)

	// Descriptor 0: read buffer at 0x80003000, 100 bytes, chain to 1
	binary.LittleEndian.PutUint64(ram[0:], 0x80003000)      // addr
	binary.LittleEndian.PutUint32(ram[8:], 100)             // len
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFNext) // flags
	binary.LittleEndian.PutUint16(ram[14:], 1)              // next

	// Descriptor 1: read buffer at 0x80003100, 50 bytes, chain to 2
	binary.LittleEndian.PutUint64(ram[16:], 0x80003100)     // addr
	binary.LittleEndian.PutUint32(ram[24:], 50)             // len
	binary.LittleEndian.PutUint16(ram[28:], VRingDescFNext) // flags
	binary.LittleEndian.PutUint16(ram[30:], 2)              // next

	// Descriptor 2: write buffer at 0x80003200, 200 bytes, end of chain
	binary.LittleEndian.PutUint64(ram[32:], 0x80003200)      // addr
	binary.LittleEndian.PutUint32(ram[40:], 200)             // len
	binary.LittleEndian.PutUint16(ram[44:], VRingDescFWrite) // flags
	binary.LittleEndian.PutUint16(ram[46:], 0)               // next

	// Test getDescRWSize
	readSize, writeSize, ok := dev.getDescRWSize(0, 0)
	if !ok {
		t.Fatal("getDescRWSize failed")
	}
	if readSize != 150 {
		t.Errorf("read size = %d, want 150", readSize)
	}
	if writeSize != 200 {
		t.Errorf("write size = %d, want 200", writeSize)
	}

	// Test descriptor reading
	desc, err := dev.getDesc(0, 0)
	if err != nil {
		t.Fatalf("getDesc failed: %v", err)
	}
	if desc.Addr != 0x80003000 {
		t.Errorf("desc addr = 0x%x, want 0x80003000", desc.Addr)
	}
	if desc.Len != 100 {
		t.Errorf("desc len = %d, want 100", desc.Len)
	}
	if desc.Flags != VRingDescFNext {
		t.Errorf("desc flags = %d, want %d", desc.Flags, VRingDescFNext)
	}
	if desc.Next != 1 {
		t.Errorf("desc next = %d, want 1", desc.Next)
	}

	// Test ConsumeDesc (should add to used ring and raise interrupt)
	dev.ConsumeDesc(0, 0, 200)

	// Check used ring
	usedRAM := memMap.GetRAMPtr(usedAddr, false)
	usedIdx := binary.LittleEndian.Uint16(usedRAM[2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	usedDescIdx := binary.LittleEndian.Uint32(usedRAM[4:])
	if usedDescIdx != 0 {
		t.Errorf("used desc idx = %d, want 0", usedDescIdx)
	}

	usedLen := binary.LittleEndian.Uint32(usedRAM[8:])
	if usedLen != 200 {
		t.Errorf("used len = %d, want 200", usedLen)
	}

	// Check interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after ConsumeDesc")
	}
	if dev.IntStatus != 1 {
		t.Errorf("IntStatus = %d, want 1", dev.IntStatus)
	}
}

func TestQueueNotify(t *testing.T) {
	dev, memMap, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Track device_recv calls
	recvCalls := []struct {
		queueIdx  int
		descIdx   int
		readSize  int
		writeSize int
	}{}

	dev.DeviceRecv = func(d *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
		recvCalls = append(recvCalls, struct {
			queueIdx  int
			descIdx   int
			readSize  int
			writeSize int
		}{queueIdx, descIdx, readSize, writeSize})
		return 0
	}

	// Set up queue
	ramBase := uint64(0x80000000)
	descAddr := ramBase
	availAddr := ramBase + 0x1000
	usedAddr := ramBase + 0x2000

	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(descAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(availAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(usedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up a single read descriptor
	binary.LittleEndian.PutUint64(ram[0:], 0x80003000)
	binary.LittleEndian.PutUint32(ram[8:], 64)
	binary.LittleEndian.PutUint16(ram[12:], 0) // No flags
	binary.LittleEndian.PutUint16(ram[14:], 0)

	// Set up available ring with one entry
	availRAM := memMap.GetRAMPtr(availAddr, true)
	binary.LittleEndian.PutUint16(availRAM[0:], 0) // flags
	binary.LittleEndian.PutUint16(availRAM[2:], 1) // idx (1 entry available)
	binary.LittleEndian.PutUint16(availRAM[4:], 0) // ring[0] = desc 0

	// Notify queue
	dev.write(nil, MMIOQueueNotify, 0, 2)

	// Check that device_recv was called
	if len(recvCalls) != 1 {
		t.Fatalf("device_recv called %d times, want 1", len(recvCalls))
	}
	if recvCalls[0].queueIdx != 0 {
		t.Errorf("queueIdx = %d, want 0", recvCalls[0].queueIdx)
	}
	if recvCalls[0].descIdx != 0 {
		t.Errorf("descIdx = %d, want 0", recvCalls[0].descIdx)
	}
	if recvCalls[0].readSize != 64 {
		t.Errorf("readSize = %d, want 64", recvCalls[0].readSize)
	}
	if recvCalls[0].writeSize != 0 {
		t.Errorf("writeSize = %d, want 0", recvCalls[0].writeSize)
	}

	// Add another entry to available ring
	binary.LittleEndian.PutUint16(availRAM[2:], 2) // idx = 2

	// Second descriptor (write)
	binary.LittleEndian.PutUint64(ram[16:], 0x80003100)
	binary.LittleEndian.PutUint32(ram[24:], 128)
	binary.LittleEndian.PutUint16(ram[28:], VRingDescFWrite)
	binary.LittleEndian.PutUint16(availRAM[6:], 1) // ring[1] = desc 1

	// Notify again
	dev.write(nil, MMIOQueueNotify, 0, 2)

	// Should have called device_recv again
	if len(recvCalls) != 2 {
		t.Fatalf("device_recv called %d times, want 2", len(recvCalls))
	}
	if recvCalls[1].descIdx != 1 {
		t.Errorf("descIdx = %d, want 1", recvCalls[1].descIdx)
	}
	if recvCalls[1].readSize != 0 {
		t.Errorf("readSize = %d, want 0", recvCalls[1].readSize)
	}
	if recvCalls[1].writeSize != 128 {
		t.Errorf("writeSize = %d, want 128", recvCalls[1].writeSize)
	}
}

func TestMemcpyFromQueue(t *testing.T) {
	dev, memMap, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Set up queue
	ramBase := uint64(0x80000000)
	descAddr := ramBase
	dataAddr := ramBase + 0x3000

	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(descAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up descriptor pointing to data
	binary.LittleEndian.PutUint64(ram[0:], dataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 16)
	binary.LittleEndian.PutUint16(ram[12:], 0)

	// Write test data
	dataRAM := memMap.GetRAMPtr(dataAddr, true)
	copy(dataRAM, []byte("Hello, VirtIO!!"))

	// Read data using MemcpyFromQueue
	buf := make([]byte, 15)
	err := dev.MemcpyFromQueue(buf, 0, 0, 0, 15)
	if err != nil {
		t.Fatalf("MemcpyFromQueue failed: %v", err)
	}
	if string(buf) != "Hello, VirtIO!!" {
		t.Errorf("read data = %q, want \"Hello, VirtIO!!\"", string(buf))
	}

	// Read with offset
	buf2 := make([]byte, 7)
	err = dev.MemcpyFromQueue(buf2, 0, 0, 7, 7)
	if err != nil {
		t.Fatalf("MemcpyFromQueue with offset failed: %v", err)
	}
	if string(buf2) != "VirtIO!" {
		t.Errorf("read data with offset = %q, want \"VirtIO!\"", string(buf2))
	}
}

func TestMemcpyToQueue(t *testing.T) {
	dev, memMap, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Set up queue
	ramBase := uint64(0x80000000)
	descAddr := ramBase
	dataAddr := ramBase + 0x3000

	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(descAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up write descriptor
	binary.LittleEndian.PutUint64(ram[0:], dataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 32)
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	// Write data using MemcpyToQueue
	data := []byte("Hello from host!")
	err := dev.MemcpyToQueue(0, 0, 0, data, len(data))
	if err != nil {
		t.Fatalf("MemcpyToQueue failed: %v", err)
	}

	// Verify data was written
	dataRAM := memMap.GetRAMPtr(dataAddr, false)
	if string(dataRAM[:len(data)]) != "Hello from host!" {
		t.Errorf("written data = %q, want \"Hello from host!\"", string(dataRAM[:len(data)]))
	}
}

func TestMemcpyChainedDescriptors(t *testing.T) {
	dev, memMap, _ := newTestDevice(t, DeviceIDConsole, 0)

	// Set up queue
	ramBase := uint64(0x80000000)
	descAddr := ramBase
	data1Addr := ramBase + 0x3000
	data2Addr := ramBase + 0x4000

	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(descAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up chained read descriptors
	// Desc 0: 8 bytes at data1Addr, chain to 1
	binary.LittleEndian.PutUint64(ram[0:], data1Addr)
	binary.LittleEndian.PutUint32(ram[8:], 8)
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFNext)
	binary.LittleEndian.PutUint16(ram[14:], 1)

	// Desc 1: 8 bytes at data2Addr, end of chain
	binary.LittleEndian.PutUint64(ram[16:], data2Addr)
	binary.LittleEndian.PutUint32(ram[24:], 8)
	binary.LittleEndian.PutUint16(ram[28:], 0)

	// Write test data
	data1RAM := memMap.GetRAMPtr(data1Addr, true)
	data2RAM := memMap.GetRAMPtr(data2Addr, true)
	copy(data1RAM, []byte("ABCDEFGH"))
	copy(data2RAM, []byte("IJKLMNOP"))

	// Read across chain boundary
	buf := make([]byte, 16)
	err := dev.MemcpyFromQueue(buf, 0, 0, 0, 16)
	if err != nil {
		t.Fatalf("MemcpyFromQueue across chain failed: %v", err)
	}
	if string(buf) != "ABCDEFGHIJKLMNOP" {
		t.Errorf("read data = %q, want \"ABCDEFGHIJKLMNOP\"", string(buf))
	}

	// Read starting in second descriptor
	buf2 := make([]byte, 4)
	err = dev.MemcpyFromQueue(buf2, 0, 0, 10, 4)
	if err != nil {
		t.Fatalf("MemcpyFromQueue with offset across chain failed: %v", err)
	}
	if string(buf2) != "KLMN" {
		t.Errorf("read data = %q, want \"KLMN\"", string(buf2))
	}
}

func TestManualRecv(t *testing.T) {
	dev, memMap, _ := newTestDevice(t, DeviceIDConsole, 0)

	recvCalled := false
	dev.DeviceRecv = func(d *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
		recvCalled = true
		return 0
	}

	// Set up queue with ManualRecv = true
	ramBase := uint64(0x80000000)
	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(ramBase), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(ramBase+0x1000), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	dev.Queues[0].ManualRecv = true

	// Set up available ring
	availRAM := memMap.GetRAMPtr(ramBase+0x1000, true)
	binary.LittleEndian.PutUint16(availRAM[2:], 1)

	// Notify - should not call DeviceRecv
	dev.write(nil, MMIOQueueNotify, 0, 2)

	if recvCalled {
		t.Error("DeviceRecv should not be called when ManualRecv is true")
	}
}

func TestMultipleQueues(t *testing.T) {
	dev, _, _ := newTestDevice(t, DeviceIDNet, 0)

	// Set up different configurations for queues 0 and 1
	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 8, 2)
	dev.write(nil, MMIOQueueDescLow, 0x1000, 2)

	dev.write(nil, MMIOQueueSel, 1, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, 0x2000, 2)

	// Verify queue 0 settings
	dev.write(nil, MMIOQueueSel, 0, 2)
	if dev.read(nil, MMIOQueueNum, 2) != 8 {
		t.Error("queue 0 num incorrect")
	}
	if dev.read(nil, MMIOQueueDescLow, 2) != 0x1000 {
		t.Error("queue 0 desc addr incorrect")
	}

	// Verify queue 1 settings
	dev.write(nil, MMIOQueueSel, 1, 2)
	if dev.read(nil, MMIOQueueNum, 2) != 4 {
		t.Error("queue 1 num incorrect")
	}
	if dev.read(nil, MMIOQueueDescLow, 2) != 0x2000 {
		t.Error("queue 1 desc addr incorrect")
	}
}

func TestIsPowerOfTwo(t *testing.T) {
	tests := []struct {
		n    uint32
		want bool
	}{
		{0, true}, // Edge case: 0 is considered power of 2 by the formula
		{1, true},
		{2, true},
		{3, false},
		{4, true},
		{5, false},
		{8, true},
		{16, true},
		{17, false},
		{256, true},
		{1024, true},
	}

	for _, tt := range tests {
		if got := isPowerOfTwo(tt.n); got != tt.want {
			t.Errorf("isPowerOfTwo(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

func TestSetLow32(t *testing.T) {
	var addr uint64 = 0xAAAAAAAABBBBBBBB
	setLow32(&addr, 0x12345678)
	if addr != 0xAAAAAAAA12345678 {
		t.Errorf("setLow32 result = 0x%x, want 0xAAAAAAAA12345678", addr)
	}
}

func TestSetHigh32(t *testing.T) {
	var addr uint64 = 0xAAAAAAAABBBBBBBB
	setHigh32(&addr, 0x12345678)
	if addr != 0x12345678BBBBBBBB {
		t.Errorf("setHigh32 result = 0x%x, want 0x12345678BBBBBBBB", addr)
	}
}
