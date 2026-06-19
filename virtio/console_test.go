package virtio

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// newTestConsole creates a VirtIO console device for testing
func newTestConsole(t *testing.T) (*Console, *mem.PhysMemoryMap, *testIRQState, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000 for descriptor tables, etc.
	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	// Create character device with buffers for testing
	txBuf := &bytes.Buffer{}
	rxBuf := &bytes.Buffer{}

	cs := &CharacterDevice{
		Writer: txBuf,
		Reader: rxBuf,
	}

	console, err := NewConsole(memMap, 0x10000000, irq, cs)
	if err != nil {
		t.Fatalf("failed to create console: %v", err)
	}

	return console, memMap, irqState, txBuf, rxBuf
}

// setupConsoleQueues sets up the RX and TX queues for testing
func setupConsoleQueues(t *testing.T, console *Console, memMap *mem.PhysMemoryMap) {
	t.Helper()

	ramBase := uint64(0x80000000)
	dev := console.Device()

	// Set up RX queue (queue 0)
	rxDescAddr := ramBase
	rxAvailAddr := ramBase + 0x1000
	rxUsedAddr := ramBase + 0x2000

	dev.write(nil, MMIOQueueSel, ConsoleQueueRX, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(rxDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(rxAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(rxUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	// Set up TX queue (queue 1)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000

	dev.write(nil, MMIOQueueSel, ConsoleQueueTX, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(txDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(txAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(txUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)
}

func TestConsoleCreation(t *testing.T) {
	console, _, _, _, _ := newTestConsole(t)

	dev := console.Device()

	// Check device ID
	id := dev.read(nil, MMIODeviceID, 2)
	if id != DeviceIDConsole {
		t.Errorf("device ID = %d, want %d", id, DeviceIDConsole)
	}

	// Check features (should include CONSOLE_F_SIZE)
	dev.write(nil, MMIODeviceFeaturesSel, 0, 2)
	features := dev.read(nil, MMIODeviceFeatures, 2)
	if features&ConsoleFeatureSize == 0 {
		t.Error("CONSOLE_F_SIZE feature not set")
	}

	// Check that RX queue has manual_recv set
	if !console.dev.Queues[ConsoleQueueRX].ManualRecv {
		t.Error("RX queue ManualRecv should be true")
	}

	// Check that TX queue does not have manual_recv set
	if console.dev.Queues[ConsoleQueueTX].ManualRecv {
		t.Error("TX queue ManualRecv should be false")
	}
}

func TestConsoleConfigSpace(t *testing.T) {
	console, _, _, _, _ := newTestConsole(t)
	dev := console.Device()

	// Initially config space should be zero
	width := dev.read(nil, MMIOConfig+ConsoleConfigWidth, 1)
	height := dev.read(nil, MMIOConfig+ConsoleConfigHeight, 1)
	if width != 0 || height != 0 {
		t.Errorf("initial config: width=%d height=%d, want 0,0", width, height)
	}

	// Set resize event
	console.ResizeEvent(80, 24)

	// Read back
	width = dev.read(nil, MMIOConfig+ConsoleConfigWidth, 1)
	height = dev.read(nil, MMIOConfig+ConsoleConfigHeight, 1)
	if width != 80 || height != 24 {
		t.Errorf("after resize: width=%d height=%d, want 80,24", width, height)
	}
}

func TestConsoleResizeEventInterrupt(t *testing.T) {
	console, _, irqState, _, _ := newTestConsole(t)

	// Initial state - no interrupt
	if irqState.level != 0 {
		t.Error("IRQ should be low initially")
	}

	// Trigger resize event
	console.ResizeEvent(120, 40)

	// Check that INT_CONFIG interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after resize event")
	}

	// Check interrupt status (bit 1 = INT_CONFIG)
	status := console.dev.read(nil, MMIOInterruptStatus, 2)
	if status&2 == 0 {
		t.Error("INT_CONFIG bit should be set in interrupt status")
	}
}

func TestConsoleTX(t *testing.T) {
	console, memMap, irqState, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up a TX descriptor (read descriptor - guest sends to host)
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 12)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0) // No flags (read descriptor)

	// Write test data to the data buffer
	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	copy(dataRAM, []byte("Hello World!"))

	// Set up available ring with one entry
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset:], 0)   // flags
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx (1 entry available)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	// Notify the TX queue
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Check that data was written to the TX buffer
	if txBuf.String() != "Hello World!" {
		t.Errorf("TX data = %q, want \"Hello World!\"", txBuf.String())
	}

	// Check that interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after TX completion")
	}
}

func TestConsoleRXCanWriteData(t *testing.T) {
	console, memMap, _, _, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	// Initially no buffers available
	if console.CanWriteData() {
		t.Error("CanWriteData should return false when no buffers available")
	}

	ramBase := uint64(0x80000000)
	rxDescAddr := ramBase
	rxAvailAddr := ramBase + 0x1000
	rxDataAddr := ramBase + 0x3000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up an RX descriptor (write descriptor - host writes to guest)
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 64)
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite) // Write flag

	// Set up available ring with one entry
	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset:], 0)   // flags
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx (1 entry available)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	_ = rxDescAddr // used implicitly through queue setup

	// Now buffer should be available
	if !console.CanWriteData() {
		t.Error("CanWriteData should return true when buffer available")
	}
}

func TestConsoleRXGetWriteLen(t *testing.T) {
	console, memMap, _, _, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	rxAvailAddr := ramBase + 0x1000
	rxDataAddr := ramBase + 0x3000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up an RX descriptor (write descriptor) with 128 byte buffer
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 128)
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	// Set up available ring
	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx = 1
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	writeLen := console.GetWriteLen()
	if writeLen != 128 {
		t.Errorf("GetWriteLen = %d, want 128", writeLen)
	}
}

func TestConsoleRXWriteData(t *testing.T) {
	console, memMap, irqState, _, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	rxAvailAddr := ramBase + 0x1000
	rxUsedAddr := ramBase + 0x2000
	rxDataAddr := ramBase + 0x3000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up an RX descriptor (write descriptor)
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 64)
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	// Set up available ring
	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx = 1
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	// Write data to guest
	testData := []byte("Hello from host!")
	written := console.WriteData(testData)

	if written != len(testData) {
		t.Errorf("WriteData returned %d, want %d", written, len(testData))
	}

	// Check that data was written to guest memory
	dataRAM := memMap.GetRAMPtr(rxDataAddr, false)
	if string(dataRAM[:len(testData)]) != "Hello from host!" {
		t.Errorf("RX data = %q, want \"Hello from host!\"", string(dataRAM[:len(testData)]))
	}

	// Check used ring
	usedOffset := int(rxUsedAddr - ramBase)
	usedIdx := binary.LittleEndian.Uint16(ram[usedOffset+2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	usedDescIdx := binary.LittleEndian.Uint32(ram[usedOffset+4:])
	if usedDescIdx != 0 {
		t.Errorf("used desc idx = %d, want 0", usedDescIdx)
	}

	usedLen := binary.LittleEndian.Uint32(ram[usedOffset+8:])
	if usedLen != uint32(len(testData)) {
		t.Errorf("used len = %d, want %d", usedLen, len(testData))
	}

	// Check interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after WriteData")
	}
}

func TestConsoleRXWriteDataNoBuffer(t *testing.T) {
	console, memMap, _, _, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	// Try to write without any available buffers
	testData := []byte("Test data")
	written := console.WriteData(testData)

	if written != 0 {
		t.Errorf("WriteData without buffer returned %d, want 0", written)
	}
}

func TestConsoleRXMultipleWrites(t *testing.T) {
	console, memMap, _, _, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	rxAvailAddr := ramBase + 0x1000
	rxDataAddr1 := ramBase + 0x3000
	rxDataAddr2 := ramBase + 0x3100

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up two RX descriptors
	// Desc 0
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr1)
	binary.LittleEndian.PutUint32(ram[8:], 32)
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	// Desc 1
	binary.LittleEndian.PutUint64(ram[16:], rxDataAddr2)
	binary.LittleEndian.PutUint32(ram[24:], 32)
	binary.LittleEndian.PutUint16(ram[28:], VRingDescFWrite)

	// Set up available ring with two entries
	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 2) // idx = 2
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0
	binary.LittleEndian.PutUint16(ram[availOffset+6:], 1) // ring[1] = desc 1

	// Write first message
	msg1 := []byte("First message")
	written1 := console.WriteData(msg1)
	if written1 != len(msg1) {
		t.Errorf("first WriteData returned %d, want %d", written1, len(msg1))
	}

	// Write second message
	msg2 := []byte("Second message")
	written2 := console.WriteData(msg2)
	if written2 != len(msg2) {
		t.Errorf("second WriteData returned %d, want %d", written2, len(msg2))
	}

	// Verify both messages were written
	data1RAM := memMap.GetRAMPtr(rxDataAddr1, false)
	if string(data1RAM[:len(msg1)]) != "First message" {
		t.Errorf("first message = %q, want \"First message\"", string(data1RAM[:len(msg1)]))
	}

	data2RAM := memMap.GetRAMPtr(rxDataAddr2, false)
	if string(data2RAM[:len(msg2)]) != "Second message" {
		t.Errorf("second message = %q, want \"Second message\"", string(data2RAM[:len(msg2)]))
	}

	// Third write should fail (no more buffers)
	msg3 := []byte("Third message")
	written3 := console.WriteData(msg3)
	if written3 != 0 {
		t.Errorf("third WriteData returned %d, want 0", written3)
	}
}

func TestConsoleTXMultipleMessages(t *testing.T) {
	console, memMap, _, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr1 := ramBase + 0x7000
	txDataAddr2 := ramBase + 0x7100

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up two TX descriptors
	descOffset := int(txDescAddr - ramBase)
	// Desc 0
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr1)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 6)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0) // Read descriptor

	// Desc 1
	binary.LittleEndian.PutUint64(ram[descOffset+16:], txDataAddr2)
	binary.LittleEndian.PutUint32(ram[descOffset+24:], 7)
	binary.LittleEndian.PutUint16(ram[descOffset+28:], 0) // Read descriptor

	// Write test data
	data1RAM := memMap.GetRAMPtr(txDataAddr1, true)
	copy(data1RAM, []byte("Hello "))
	data2RAM := memMap.GetRAMPtr(txDataAddr2, true)
	copy(data2RAM, []byte("World!\n"))

	// Set up available ring with two entries
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 2) // idx = 2
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0
	binary.LittleEndian.PutUint16(ram[availOffset+6:], 1) // ring[1] = desc 1

	// Notify TX queue
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Check both messages were transmitted
	if txBuf.String() != "Hello World!\n" {
		t.Errorf("TX data = %q, want \"Hello World!\\n\"", txBuf.String())
	}
}

func TestConsoleNilCharacterDevice(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()

	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, _ := newTestIRQ()

	// Create console with nil character device
	console, err := NewConsole(memMap, 0x10000000, irq, nil)
	if err != nil {
		t.Fatalf("failed to create console: %v", err)
	}

	// This should not panic
	ramBase := uint64(0x80000000)
	setupConsoleQueuesRaw(console, memMap, ramBase)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up a TX descriptor
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr := ramBase + 0x7000

	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 5)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	copy(dataRAM, []byte("Test!"))

	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	// Should not panic even with nil character device
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)
}

// setupConsoleQueuesRaw sets up console queues using raw addresses
func setupConsoleQueuesRaw(console *Console, memMap *mem.PhysMemoryMap, ramBase uint64) {
	dev := console.Device()

	// Set up RX queue
	rxDescAddr := ramBase
	rxAvailAddr := ramBase + 0x1000
	rxUsedAddr := ramBase + 0x2000

	dev.write(nil, MMIOQueueSel, ConsoleQueueRX, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(rxDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(rxAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(rxUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	// Set up TX queue
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000

	dev.write(nil, MMIOQueueSel, ConsoleQueueTX, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(txDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(txAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(txUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)
}

func TestConsoleQueueNotReady(t *testing.T) {
	console, memMap, _, _, _ := newTestConsole(t)

	// Don't set up queues (leave them not ready)
	_ = memMap

	// CanWriteData should return false
	if console.CanWriteData() {
		t.Error("CanWriteData should return false when queue not ready")
	}

	// GetWriteLen should return 0
	if console.GetWriteLen() != 0 {
		t.Error("GetWriteLen should return 0 when queue not ready")
	}

	// WriteData should return 0
	written := console.WriteData([]byte("test"))
	if written != 0 {
		t.Errorf("WriteData should return 0 when queue not ready, got %d", written)
	}
}

// TestConsoleTXChainedDescriptors tests TX with data spanning multiple chained descriptors.
// This verifies that memcpy_from_queue correctly handles descriptor chains.
// Reference: virtio.c lines 1268-1285 (virtio_console_recv_request)
// Reference: virtio.c lines 380-442 (memcpy_to_from_queue)
func TestConsoleTXChainedDescriptors(t *testing.T) {
	console, memMap, irqState, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000
	txDataAddr1 := ramBase + 0x7000
	txDataAddr2 := ramBase + 0x7100

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up chained TX descriptors (desc 0 -> desc 1)
	// Descriptor 0: first part of data (8 bytes), chains to desc 1
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr1)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 8)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], VRingDescFNext) // Chain to next
	binary.LittleEndian.PutUint16(ram[descOffset+14:], 1)              // Next = desc 1

	// Descriptor 1: second part of data (8 bytes), end of chain
	binary.LittleEndian.PutUint64(ram[descOffset+16:], txDataAddr2)
	binary.LittleEndian.PutUint32(ram[descOffset+24:], 8)
	binary.LittleEndian.PutUint16(ram[descOffset+28:], 0) // No flags, end of chain

	// Write test data to the data buffers
	data1RAM := memMap.GetRAMPtr(txDataAddr1, true)
	copy(data1RAM, []byte("ABCDEFGH"))
	data2RAM := memMap.GetRAMPtr(txDataAddr2, true)
	copy(data2RAM, []byte("IJKLMNOP"))

	// Set up available ring with one entry
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset:], 0)   // flags
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx (1 entry available)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	// Notify the TX queue
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Check that all 16 bytes of chained data were written to TX buffer
	if txBuf.String() != "ABCDEFGHIJKLMNOP" {
		t.Errorf("TX data = %q, want \"ABCDEFGHIJKLMNOP\"", txBuf.String())
	}

	// Check used ring was updated
	usedOffset := int(txUsedAddr - ramBase)
	usedIdx := binary.LittleEndian.Uint16(ram[usedOffset+2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	// Check that interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after TX completion")
	}
}

// TestConsoleTXZeroLength tests TX with zero-length data.
// Reference: virtio.c lines 1268-1285 (virtio_console_recv_request)
func TestConsoleTXZeroLength(t *testing.T) {
	console, memMap, irqState, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up a TX descriptor with zero length
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 0) // Zero length
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	// Set up available ring
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	// Notify the TX queue
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Check that nothing was written to TX buffer (zero-length)
	if txBuf.Len() != 0 {
		t.Errorf("TX buffer length = %d, want 0 for zero-length data", txBuf.Len())
	}

	// Used ring should still be updated
	usedOffset := int(txUsedAddr - ramBase)
	usedIdx := binary.LittleEndian.Uint16(ram[usedOffset+2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	// IRQ should still be raised (descriptor was consumed)
	if irqState.level != 1 {
		t.Error("IRQ should be raised even for zero-length TX")
	}
}

// TestConsoleTXNilWriteDataCallback tests TX when WriteData callback is nil
// but CharacterDevice is not nil.
// Reference: virtio.c lines 1268-1285 (virtio_console_recv_request)
func TestConsoleTXNilWriteDataCallback(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()

	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	// Create character device with nil Writer
	cs := &CharacterDevice{
		Writer: nil, // Explicitly nil
	}

	console, err := NewConsole(memMap, 0x10000000, irq, cs)
	if err != nil {
		t.Fatalf("failed to create console: %v", err)
	}

	ramBase := uint64(0x80000000)
	setupConsoleQueuesRaw(console, memMap, ramBase)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up a TX descriptor
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000
	txDataAddr := ramBase + 0x7000

	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 10)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	copy(dataRAM, []byte("TestData10"))

	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	// Should not panic with nil WriteData callback
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Used ring should still be updated
	usedOffset := int(txUsedAddr - ramBase)
	usedIdx := binary.LittleEndian.Uint16(ram[usedOffset+2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	// IRQ should be raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised even with nil WriteData callback")
	}
}

// TestConsoleTXUsedRingEntry tests the used ring entry details after TX.
// Reference: virtio.c lines 461-478 (virtio_consume_desc)
func TestConsoleTXUsedRingEntry(t *testing.T) {
	console, memMap, _, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up TX descriptor
	descOffset := int(txDescAddr - ramBase)
	testData := "Test TX Used Ring"
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], uint32(len(testData)))
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	copy(dataRAM, []byte(testData))

	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Verify data was transmitted
	if txBuf.String() != testData {
		t.Errorf("TX data = %q, want %q", txBuf.String(), testData)
	}

	// Verify used ring entry
	usedOffset := int(txUsedAddr - ramBase)
	// Used ring: [flags:2][idx:2][entries...]
	// Entry: [id:4][len:4]
	usedIdx := binary.LittleEndian.Uint16(ram[usedOffset+2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	// First used entry at offset 4 (after flags and idx)
	usedDescIdx := binary.LittleEndian.Uint32(ram[usedOffset+4:])
	if usedDescIdx != 0 {
		t.Errorf("used entry desc id = %d, want 0", usedDescIdx)
	}

	// For TX (guest->host), length should be 0 (no data written back to guest)
	// Reference: virtio.c line 1282 - virtio_consume_desc(s, queue_idx, desc_idx, 0)
	usedLen := binary.LittleEndian.Uint32(ram[usedOffset+8:])
	if usedLen != 0 {
		t.Errorf("used entry len = %d, want 0 (TX writes 0 back to guest)", usedLen)
	}
}

// TestConsoleTXCallbackInvocation tests that recvRequest is invoked with correct parameters.
// Reference: virtio.c lines 518-544 (queue_notify)
// Reference: virtio.c lines 1268-1285 (virtio_console_recv_request)
func TestConsoleTXCallbackInvocation(t *testing.T) {
	console, memMap, _, _, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	// Track callback invocations
	var recvCalls []struct {
		queueIdx  int
		descIdx   int
		readSize  int
		writeSize int
	}

	// Wrap the device's DeviceRecv to track calls
	origRecv := console.dev.DeviceRecv
	console.dev.DeviceRecv = func(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
		recvCalls = append(recvCalls, struct {
			queueIdx  int
			descIdx   int
			readSize  int
			writeSize int
		}{queueIdx, descIdx, readSize, writeSize})
		return origRecv(dev, queueIdx, descIdx, readSize, writeSize)
	}

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up TX descriptor with 100 bytes
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 100)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0) // No flags (read descriptor)

	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Verify callback was invoked
	if len(recvCalls) != 1 {
		t.Fatalf("recvRequest called %d times, want 1", len(recvCalls))
	}

	call := recvCalls[0]
	if call.queueIdx != ConsoleQueueTX {
		t.Errorf("queueIdx = %d, want %d (TX)", call.queueIdx, ConsoleQueueTX)
	}
	if call.descIdx != 0 {
		t.Errorf("descIdx = %d, want 0", call.descIdx)
	}
	// TX descriptor is read descriptor (guest sends to host), so readSize = 100
	if call.readSize != 100 {
		t.Errorf("readSize = %d, want 100", call.readSize)
	}
	// No write descriptors in this chain
	if call.writeSize != 0 {
		t.Errorf("writeSize = %d, want 0", call.writeSize)
	}
}

// TestConsoleTXLargeData tests TX with large data buffer.
// This helps verify no issues with larger data transfers.
// Reference: virtio.c lines 1268-1285 (virtio_console_recv_request)
func TestConsoleTXLargeData(t *testing.T) {
	// Need more RAM for large data
	memMap := mem.NewPhysMemoryMap()
	_, err := memMap.RegisterRAM(0x80000000, 0x100000, 0) // 1MB RAM
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	txBuf := &bytes.Buffer{}
	cs := &CharacterDevice{
		Writer: txBuf,
	}

	console, err := NewConsole(memMap, 0x10000000, irq, cs)
	if err != nil {
		t.Fatalf("failed to create console: %v", err)
	}

	ramBase := uint64(0x80000000)
	dev := console.Device()

	// Set up TX queue with larger queue size
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000
	txDataAddr := ramBase + 0x10000

	dev.write(nil, MMIOQueueSel, ConsoleQueueTX, 2)
	dev.write(nil, MMIOQueueNum, 16, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(txDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(txAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(txUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	ram := memMap.GetRAMPtr(ramBase, true)

	// Create large test data (4KB)
	dataSize := 4096
	testData := make([]byte, dataSize)
	for i := 0; i < dataSize; i++ {
		testData[i] = byte(i % 256)
	}

	// Set up TX descriptor with large data
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], uint32(dataSize))
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	copy(dataRAM, testData)

	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Verify all data was transmitted
	if txBuf.Len() != dataSize {
		t.Errorf("TX buffer length = %d, want %d", txBuf.Len(), dataSize)
	}

	// Verify data integrity
	txData := txBuf.Bytes()
	for i := 0; i < dataSize; i++ {
		if txData[i] != byte(i%256) {
			t.Errorf("TX data[%d] = %d, want %d", i, txData[i], byte(i%256))
			break
		}
	}

	// Verify IRQ was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after large TX completion")
	}
}

// TestConsoleTXQueueNotification tests that queue notification properly triggers
// the TX processing path.
// Reference: virtio.c lines 518-544 (queue_notify)
func TestConsoleTXQueueNotification(t *testing.T) {
	console, memMap, irqState, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up TX descriptor
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 5)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	copy(dataRAM, []byte("Test1"))

	// Don't notify yet - buffer should be empty
	if txBuf.Len() != 0 {
		t.Error("TX buffer should be empty before notification")
	}

	// Add to available ring but don't notify
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	// Still should be empty (no notification)
	if txBuf.Len() != 0 {
		t.Error("TX buffer should be empty without notification")
	}
	if irqState.level != 0 {
		t.Error("IRQ should not be raised without notification")
	}

	// Now notify the TX queue
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// Now data should be transmitted
	if txBuf.String() != "Test1" {
		t.Errorf("TX data = %q, want \"Test1\"", txBuf.String())
	}
	if irqState.level != 1 {
		t.Error("IRQ should be raised after notification")
	}
}

// TestConsoleTXMultipleDescriptorsInSingleNotify tests processing multiple
// descriptors in a single queue notification.
// Reference: virtio.c lines 518-544 (queue_notify - processes all available descriptors)
func TestConsoleTXMultipleDescriptorsInSingleNotify(t *testing.T) {
	console, memMap, _, txBuf, _ := newTestConsole(t)
	setupConsoleQueues(t, console, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr1 := ramBase + 0x7000
	txDataAddr2 := ramBase + 0x7100
	txDataAddr3 := ramBase + 0x7200

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up three separate TX descriptors (not chained)
	descOffset := int(txDescAddr - ramBase)

	// Desc 0
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr1)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], 3)
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	// Desc 1
	binary.LittleEndian.PutUint64(ram[descOffset+16:], txDataAddr2)
	binary.LittleEndian.PutUint32(ram[descOffset+24:], 3)
	binary.LittleEndian.PutUint16(ram[descOffset+28:], 0)

	// Desc 2
	binary.LittleEndian.PutUint64(ram[descOffset+32:], txDataAddr3)
	binary.LittleEndian.PutUint32(ram[descOffset+40:], 3)
	binary.LittleEndian.PutUint16(ram[descOffset+44:], 0)

	// Write data
	data1RAM := memMap.GetRAMPtr(txDataAddr1, true)
	copy(data1RAM, []byte("AAA"))
	data2RAM := memMap.GetRAMPtr(txDataAddr2, true)
	copy(data2RAM, []byte("BBB"))
	data3RAM := memMap.GetRAMPtr(txDataAddr3, true)
	copy(data3RAM, []byte("CCC"))

	// Set up available ring with all three entries at once
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 3) // idx = 3 (3 entries available)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0
	binary.LittleEndian.PutUint16(ram[availOffset+6:], 1) // ring[1] = desc 1
	binary.LittleEndian.PutUint16(ram[availOffset+8:], 2) // ring[2] = desc 2

	// Single notification should process all three descriptors
	console.dev.write(nil, MMIOQueueNotify, ConsoleQueueTX, 2)

	// All three messages should be transmitted in order
	expected := "AAABBBCCC"
	if txBuf.String() != expected {
		t.Errorf("TX data = %q, want %q", txBuf.String(), expected)
	}
}

// TestConsoleTXSetDebug tests the SetDebug function for code coverage.
// Reference: virtio.c - debug functionality
func TestConsoleTXSetDebug(t *testing.T) {
	console, _, _, _, _ := newTestConsole(t)

	// SetDebug should not panic
	console.SetDebug(DebugIO)
	console.SetDebug(0)
}
