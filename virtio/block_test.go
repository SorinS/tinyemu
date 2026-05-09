package virtio

import (
	"encoding/binary"
	"testing"

	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/mem"
)

// newTestBlockDevice creates a VirtIO block device for testing
func newTestBlockDevice(t *testing.T, sizeBytes int64) (*BlockDevice, *mem.PhysMemoryMap, *testIRQState) {
	t.Helper()

	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000 for descriptor tables, data buffers, etc.
	// Need at least 1MB to accommodate all test memory regions
	_, err := memMap.RegisterRAM(0x80000000, 0x100000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	// Create in-memory block device
	bs, err := devices.NewMemoryBlockDevice(sizeBytes)
	if err != nil {
		t.Fatalf("failed to create block device: %v", err)
	}

	bd, err := NewBlockDevice(memMap, 0x10000000, irq, bs)
	if err != nil {
		t.Fatalf("failed to create VirtIO block device: %v", err)
	}

	return bd, memMap, irqState
}

func TestBlockDeviceCreate(t *testing.T) {
	bd, _, _ := newTestBlockDevice(t, 4096)

	// Check device ID
	dev := bd.Device()
	id := dev.read(nil, MMIODeviceID, 2)
	if id != DeviceIDBlock {
		t.Errorf("device ID = %d, want %d", id, DeviceIDBlock)
	}

	// Check magic and version
	magic := dev.read(nil, MMIOMagicValue, 2)
	if magic != MMIOMagic {
		t.Errorf("magic = 0x%x, want 0x%x", magic, MMIOMagic)
	}

	version := dev.read(nil, MMIOVersion, 2)
	if version != MMIOVersionVal {
		t.Errorf("version = %d, want %d", version, MMIOVersionVal)
	}
}

func TestBlockDeviceCapacity(t *testing.T) {
	// Create a 4KB block device (8 sectors)
	bd, _, _ := newTestBlockDevice(t, 4096)

	// Check capacity in config space
	dev := bd.Device()
	capacityLow := dev.read(nil, MMIOConfig, 2)
	capacityHigh := dev.read(nil, MMIOConfig+4, 2)
	capacity := uint64(capacityLow) | (uint64(capacityHigh) << 32)

	if capacity != 8 {
		t.Errorf("capacity = %d sectors, want 8", capacity)
	}

	// Also check via GetCapacity
	if bd.GetCapacity() != 8 {
		t.Errorf("GetCapacity() = %d, want 8", bd.GetCapacity())
	}
}

func TestBlockDeviceLargeCapacity(t *testing.T) {
	// Create a 1MB block device (2048 sectors)
	bd, _, _ := newTestBlockDevice(t, 1024*1024)

	// Check capacity
	dev := bd.Device()
	capacityLow := dev.read(nil, MMIOConfig, 2)
	capacityHigh := dev.read(nil, MMIOConfig+4, 2)
	capacity := uint64(capacityLow) | (uint64(capacityHigh) << 32)

	expectedSectors := (1024 * 1024) / devices.SectorSize
	if capacity != uint64(expectedSectors) {
		t.Errorf("capacity = %d sectors, want %d", capacity, expectedSectors)
	}
}

func TestBlockDeviceClose(t *testing.T) {
	bd, _, _ := newTestBlockDevice(t, 4096)

	// Close should not panic or return error
	if err := bd.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

func TestBlockRequestConstants(t *testing.T) {
	// Verify request type constants match VirtIO spec
	if VirtIOBlkTIn != 0 {
		t.Errorf("VirtIOBlkTIn = %d, want 0", VirtIOBlkTIn)
	}
	if VirtIOBlkTOut != 1 {
		t.Errorf("VirtIOBlkTOut = %d, want 1", VirtIOBlkTOut)
	}
	if VirtIOBlkTFlush != 4 {
		t.Errorf("VirtIOBlkTFlush = %d, want 4", VirtIOBlkTFlush)
	}
}

func TestBlockStatusConstants(t *testing.T) {
	// Verify status constants match VirtIO spec
	if VirtIOBlkSOK != 0 {
		t.Errorf("VirtIOBlkSOK = %d, want 0", VirtIOBlkSOK)
	}
	if VirtIOBlkSIOErr != 1 {
		t.Errorf("VirtIOBlkSIOErr = %d, want 1", VirtIOBlkSIOErr)
	}
	if VirtIOBlkSUnsupp != 2 {
		t.Errorf("VirtIOBlkSUnsupp = %d, want 2", VirtIOBlkSUnsupp)
	}
}

// blockTestSetup contains all the state needed for block device I/O tests.
// Reference: virtio.c lines 1063-1115 (virtio_block_recv_request)
type blockTestSetup struct {
	bd       *BlockDevice
	memMap   *mem.PhysMemoryMap
	irqState *testIRQState
	dev      *Device

	// Memory layout:
	// 0x80000000: RAM base
	// 0x80000000: Descriptor table
	// 0x80001000: Available ring
	// 0x80002000: Used ring
	// 0x80003000: Request header
	// 0x80003100: Data buffer
	// 0x80003600: Status byte
	ramBase    uint64
	descAddr   uint64
	availAddr  uint64
	usedAddr   uint64
	headerAddr uint64
	dataAddr   uint64
	statusAddr uint64
}

// newBlockTestSetup creates a fully configured block device test environment.
// Reference: virtio.c lines 1117-1132 (virtio_block_init)
func newBlockTestSetup(t *testing.T, sizeBytes int64) *blockTestSetup {
	t.Helper()

	bd, memMap, irqState := newTestBlockDevice(t, sizeBytes)
	dev := bd.Device()

	s := &blockTestSetup{
		bd:         bd,
		memMap:     memMap,
		irqState:   irqState,
		dev:        dev,
		ramBase:    0x80000000,
		descAddr:   0x80000000,
		availAddr:  0x80001000,
		usedAddr:   0x80002000,
		headerAddr: 0x80003000,
		dataAddr:   0x80003100,
		statusAddr: 0x80003600,
	}

	// Configure queue 0
	dev.write(nil, MMIOQueueSel, 0, 2)
	dev.write(nil, MMIOQueueNum, 16, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(s.descAddr), 2)
	dev.write(nil, MMIOQueueDescHigh, uint32(s.descAddr>>32), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(s.availAddr), 2)
	dev.write(nil, MMIOQueueAvailHigh, uint32(s.availAddr>>32), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(s.usedAddr), 2)
	dev.write(nil, MMIOQueueUsedHigh, uint32(s.usedAddr>>32), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	// Set device status to ready
	dev.write(nil, MMIOStatus, 0xF, 2) // ACKNOWLEDGE | DRIVER | DRIVER_OK | FEATURES_OK

	// Initialize used ring index to 0
	usedRAM := memMap.GetRAMPtr(s.usedAddr, true)
	binary.LittleEndian.PutUint16(usedRAM[0:], 0) // flags
	binary.LittleEndian.PutUint16(usedRAM[2:], 0) // idx

	return s
}

// writeDescriptor writes a VirtIO descriptor at the specified index.
// Reference: virtio.c lines 112-117 (VIRTIODesc structure)
func (s *blockTestSetup) writeDescriptor(idx int, addr uint64, length uint32, flags uint16, next uint16) {
	descRAM := s.memMap.GetRAMPtr(s.descAddr+uint64(idx)*16, true)
	binary.LittleEndian.PutUint64(descRAM[0:], addr)
	binary.LittleEndian.PutUint32(descRAM[8:], length)
	binary.LittleEndian.PutUint16(descRAM[12:], flags)
	binary.LittleEndian.PutUint16(descRAM[14:], next)
}

// writeBlockHeader writes a block request header to memory.
// Reference: virtio.c lines 999-1003 (BlockRequestHeader)
func (s *blockTestSetup) writeBlockHeader(reqType uint32, sectorNum uint64) {
	headerRAM := s.memMap.GetRAMPtr(s.headerAddr, true)
	binary.LittleEndian.PutUint32(headerRAM[0:], reqType)
	binary.LittleEndian.PutUint32(headerRAM[4:], 0) // ioprio = 0
	binary.LittleEndian.PutUint64(headerRAM[8:], sectorNum)
}

// setAvailableRingEntry adds an entry to the available ring.
// Reference: virtio.c lines 518-544 (queue_notify)
func (s *blockTestSetup) setAvailableRingEntry(idx uint16, descIdx uint16) {
	availRAM := s.memMap.GetRAMPtr(s.availAddr, true)
	binary.LittleEndian.PutUint16(availRAM[0:], 0)   // flags
	binary.LittleEndian.PutUint16(availRAM[2:], idx) // next available index
	// Ring entries start at offset 4
	binary.LittleEndian.PutUint16(availRAM[4+uint64(idx-1)*2:], descIdx)
}

// notify triggers queue notification.
func (s *blockTestSetup) notify() {
	s.dev.write(nil, MMIOQueueNotify, 0, 2)
}

// getUsedRingIdx returns the current used ring index.
func (s *blockTestSetup) getUsedRingIdx() uint16 {
	usedRAM := s.memMap.GetRAMPtr(s.usedAddr, false)
	return binary.LittleEndian.Uint16(usedRAM[2:])
}

// getStatus returns the status byte from the status address.
func (s *blockTestSetup) getStatus() byte {
	statusRAM := s.memMap.GetRAMPtr(s.statusAddr, false)
	return statusRAM[0]
}

// getData returns the data buffer contents.
func (s *blockTestSetup) getData(length int) []byte {
	dataRAM := s.memMap.GetRAMPtr(s.dataAddr, false)
	return dataRAM[:length]
}

// setData writes data to the data buffer.
func (s *blockTestSetup) setData(data []byte) {
	dataRAM := s.memMap.GetRAMPtr(s.dataAddr, true)
	copy(dataRAM, data)
}

// writeToUnderlyingDevice writes directly to the underlying block device.
func (s *blockTestSetup) writeToUnderlyingDevice(sectorNum uint64, data []byte) {
	// Access the underlying block device through the test helper
	bs := s.bd.bs.(*devices.MemoryBlockDevice)
	deviceData := bs.Data()
	copy(deviceData[sectorNum*devices.SectorSize:], data)
}

// readFromUnderlyingDevice reads directly from the underlying block device.
func (s *blockTestSetup) readFromUnderlyingDevice(sectorNum uint64, length int) []byte {
	bs := s.bd.bs.(*devices.MemoryBlockDevice)
	deviceData := bs.Data()
	result := make([]byte, length)
	copy(result, deviceData[sectorNum*devices.SectorSize:])
	return result
}

// TestBlockCallbackInvocation verifies that the device callback is invoked on queue notify.
// Reference: virtio.c lines 518-544 (queue_notify)
func TestBlockCallbackInvocation(t *testing.T) {
	s := newBlockTestSetup(t, 4096)

	// Track callback invocations
	callbackCalled := false
	origRecv := s.dev.DeviceRecv
	s.dev.DeviceRecv = func(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
		callbackCalled = true
		return origRecv(dev, queueIdx, descIdx, readSize, writeSize)
	}

	// Set up a minimal descriptor chain
	s.writeBlockHeader(VirtIOBlkTIn, 0)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.dataAddr, 513, VRingDescFWrite, 0)
	s.setAvailableRingEntry(1, 0)

	s.notify()

	if !callbackCalled {
		t.Error("DeviceRecv callback was not called")
	}
}

// TestBlockReadSingleSector tests reading a single sector through VirtIO.
// Reference: virtio.c lines 1083-1095 (VIRTIO_BLK_T_IN handling)
func TestBlockReadSingleSector(t *testing.T) {
	s := newBlockTestSetup(t, 4096) // 8 sectors

	// Write known data to underlying device at sector 0
	testData := make([]byte, devices.SectorSize)
	for i := range testData {
		testData[i] = byte(i)
	}
	s.writeToUnderlyingDevice(0, testData)

	// Set up read request:
	// Descriptor 0: Header (read by device) - 16 bytes
	// Descriptor 1: Data + Status (write by device) - 513 bytes (512 data + 1 status)
	s.writeBlockHeader(VirtIOBlkTIn, 0) // Read from sector 0

	// Desc 0: Header (read descriptor, chains to 1)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)

	// Desc 1: Data + Status buffer (write descriptor)
	dataStatusLen := uint32(devices.SectorSize + 1)
	s.writeDescriptor(1, s.dataAddr, dataStatusLen, VRingDescFWrite, 0)

	// Add to available ring and notify
	s.setAvailableRingEntry(1, 0)
	s.notify()

	// Check status
	status := s.getData(devices.SectorSize + 1)[devices.SectorSize]
	if status != VirtIOBlkSOK {
		t.Errorf("read status = %d, want %d (OK)", status, VirtIOBlkSOK)
	}

	// Verify data matches what we wrote
	readData := s.getData(devices.SectorSize)
	for i := 0; i < devices.SectorSize; i++ {
		if readData[i] != byte(i) {
			t.Errorf("data[%d] = %d, want %d", i, readData[i], byte(i))
			break
		}
	}

	// Check used ring was updated
	if s.getUsedRingIdx() != 1 {
		t.Errorf("used index = %d, want 1", s.getUsedRingIdx())
	}

	// Check IRQ was raised
	if s.irqState.level != 1 {
		t.Error("IRQ should be raised after read completion")
	}
}

// TestBlockWriteSingleSector tests writing a single sector through VirtIO.
// Reference: virtio.c lines 1096-1110 (VIRTIO_BLK_T_OUT handling)
func TestBlockWriteSingleSector(t *testing.T) {
	s := newBlockTestSetup(t, 4096) // 8 sectors

	// Prepare data to write
	writeData := make([]byte, devices.SectorSize)
	for i := range writeData {
		writeData[i] = byte(0xFF - i)
	}
	s.setData(writeData)

	// Set up write request:
	// Descriptor 0: Header (read by device) - 16 bytes
	// Descriptor 1: Data (read by device) - 512 bytes
	// Descriptor 2: Status (write by device) - 1 byte
	s.writeBlockHeader(VirtIOBlkTOut, 0) // Write to sector 0

	// Desc 0: Header (read descriptor, chains to 1)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)

	// Desc 1: Data buffer (read descriptor, chains to 2)
	s.writeDescriptor(1, s.dataAddr, devices.SectorSize, VRingDescFNext, 2)

	// Desc 2: Status buffer (write descriptor)
	s.writeDescriptor(2, s.statusAddr, 1, VRingDescFWrite, 0)

	// Add to available ring and notify
	s.setAvailableRingEntry(1, 0)
	s.notify()

	// Check status
	if s.getStatus() != VirtIOBlkSOK {
		t.Errorf("write status = %d, want %d (OK)", s.getStatus(), VirtIOBlkSOK)
	}

	// Verify data was written to underlying device
	deviceData := s.readFromUnderlyingDevice(0, devices.SectorSize)
	for i := 0; i < devices.SectorSize; i++ {
		if deviceData[i] != byte(0xFF-i) {
			t.Errorf("device data[%d] = %d, want %d", i, deviceData[i], byte(0xFF-i))
			break
		}
	}

	// Check used ring was updated
	if s.getUsedRingIdx() != 1 {
		t.Errorf("used index = %d, want 1", s.getUsedRingIdx())
	}
}

// TestBlockMultiSectorRead tests reading multiple sectors in one request.
// Reference: virtio.c lines 1083-1095 (VIRTIO_BLK_T_IN handling)
func TestBlockMultiSectorRead(t *testing.T) {
	// Create a 4KB device (8 sectors)
	s := newBlockTestSetup(t, 4096)

	// Write distinct data to sectors 2, 3, and 4
	for sector := uint64(2); sector <= 4; sector++ {
		testData := make([]byte, devices.SectorSize)
		for i := range testData {
			testData[i] = byte(sector*10 + uint64(i%10))
		}
		s.writeToUnderlyingDevice(sector, testData)
	}

	// Request: read 3 sectors (1536 bytes) starting at sector 2
	numSectors := 3
	dataLen := numSectors * devices.SectorSize
	s.writeBlockHeader(VirtIOBlkTIn, 2)

	// Desc 0: Header (read), Desc 1: Data+Status (write)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.dataAddr, uint32(dataLen+1), VRingDescFWrite, 0)

	s.setAvailableRingEntry(1, 0)
	s.notify()

	// Check status (at end of data buffer)
	statusRAM := s.memMap.GetRAMPtr(s.dataAddr+uint64(dataLen), false)
	if statusRAM[0] != VirtIOBlkSOK {
		t.Errorf("multi-sector read status = %d, want %d (OK)", statusRAM[0], VirtIOBlkSOK)
	}

	// Verify data for each sector
	dataRAM := s.memMap.GetRAMPtr(s.dataAddr, false)
	for sector := 0; sector < numSectors; sector++ {
		offset := sector * devices.SectorSize
		expectedFirstByte := byte((sector+2)*10 + 0)
		if dataRAM[offset] != expectedFirstByte {
			t.Errorf("sector %d data[0] = %d, want %d", sector+2, dataRAM[offset], expectedFirstByte)
		}
	}

	if s.getUsedRingIdx() != 1 {
		t.Errorf("used index = %d, want 1", s.getUsedRingIdx())
	}
}

// TestBlockMultiSectorWrite tests writing multiple sectors in one request.
// Reference: virtio.c lines 1096-1110 (VIRTIO_BLK_T_OUT handling)
func TestBlockMultiSectorWrite(t *testing.T) {
	s := newBlockTestSetup(t, 4096) // 8 sectors

	// Prepare data for 2 sectors - each sector has unique pattern
	numSectors := 2
	dataLen := numSectors * devices.SectorSize
	writeData := make([]byte, dataLen)
	for i := range writeData {
		sector := i / devices.SectorSize
		offsetInSector := i % devices.SectorSize
		// Each sector starts with its sector number * 100
		writeData[i] = byte(sector*100 + offsetInSector%100)
	}
	s.setData(writeData)

	// Write to sectors 3 and 4
	s.writeBlockHeader(VirtIOBlkTOut, 3)

	// Desc 0: Header, Desc 1: Data, Desc 2: Status
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.dataAddr, uint32(dataLen), VRingDescFNext, 2)
	s.writeDescriptor(2, s.statusAddr, 1, VRingDescFWrite, 0)

	s.setAvailableRingEntry(1, 0)
	s.notify()

	if s.getStatus() != VirtIOBlkSOK {
		t.Errorf("multi-sector write status = %d, want %d (OK)", s.getStatus(), VirtIOBlkSOK)
	}

	// Verify data was written correctly
	for sector := 0; sector < numSectors; sector++ {
		deviceData := s.readFromUnderlyingDevice(uint64(3+sector), devices.SectorSize)
		expectedFirstByte := byte(sector * 100) // First byte of each sector
		if deviceData[0] != expectedFirstByte {
			t.Errorf("sector %d device data[0] = %d, want %d", 3+sector, deviceData[0], expectedFirstByte)
		}
	}
}

// TestBlockFlush tests the flush request.
// Reference: virtio.c lines 1005-1008 (VIRTIO_BLK_T_FLUSH)
func TestBlockFlush(t *testing.T) {
	s := newBlockTestSetup(t, 4096)

	// Set up flush request (type=FLUSH, sector=0)
	s.writeBlockHeader(VirtIOBlkTFlush, 0)

	// Flush only needs header (read) and status (write)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.statusAddr, 1, VRingDescFWrite, 0)

	s.setAvailableRingEntry(1, 0)
	s.notify()

	if s.getStatus() != VirtIOBlkSOK {
		t.Errorf("flush status = %d, want %d (OK)", s.getStatus(), VirtIOBlkSOK)
	}

	if s.getUsedRingIdx() != 1 {
		t.Errorf("used index = %d, want 1", s.getUsedRingIdx())
	}
}

// TestBlockUnsupportedOperation tests handling of unsupported request types.
// Reference: virtio.c lines 1111-1113 (default case)
func TestBlockUnsupportedOperation(t *testing.T) {
	s := newBlockTestSetup(t, 4096)

	// Set up request with unsupported type (0xFF)
	headerRAM := s.memMap.GetRAMPtr(s.headerAddr, true)
	binary.LittleEndian.PutUint32(headerRAM[0:], 0xFF) // Invalid type
	binary.LittleEndian.PutUint32(headerRAM[4:], 0)    // ioprio
	binary.LittleEndian.PutUint64(headerRAM[8:], 0)    // sector

	// Desc 0: Header, Desc 1: Status
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.statusAddr, 1, VRingDescFWrite, 0)

	s.setAvailableRingEntry(1, 0)
	s.notify()

	// Should return UNSUPP status
	if s.getStatus() != VirtIOBlkSUnsupp {
		t.Errorf("unsupported op status = %d, want %d (UNSUPP)", s.getStatus(), VirtIOBlkSUnsupp)
	}
}

// TestBlockReadWriteRoundTrip tests writing and then reading back data.
// This verifies the complete I/O path.
func TestBlockReadWriteRoundTrip(t *testing.T) {
	s := newBlockTestSetup(t, 4096)

	// Write data to sector 1
	writeData := make([]byte, devices.SectorSize)
	for i := range writeData {
		writeData[i] = byte(i ^ 0xAA)
	}
	s.setData(writeData)

	s.writeBlockHeader(VirtIOBlkTOut, 1)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.dataAddr, devices.SectorSize, VRingDescFNext, 2)
	s.writeDescriptor(2, s.statusAddr, 1, VRingDescFWrite, 0)
	s.setAvailableRingEntry(1, 0)
	s.notify()

	if s.getStatus() != VirtIOBlkSOK {
		t.Fatalf("write status = %d, want %d (OK)", s.getStatus(), VirtIOBlkSOK)
	}

	// Clear data buffer
	dataRAM := s.memMap.GetRAMPtr(s.dataAddr, true)
	for i := 0; i < devices.SectorSize+1; i++ {
		dataRAM[i] = 0
	}

	// Read back from sector 1
	s.writeBlockHeader(VirtIOBlkTIn, 1)
	s.writeDescriptor(3, s.headerAddr, 16, VRingDescFNext, 4)
	s.writeDescriptor(4, s.dataAddr, uint32(devices.SectorSize+1), VRingDescFWrite, 0)
	s.setAvailableRingEntry(2, 3)
	s.notify()

	// Check status
	status := dataRAM[devices.SectorSize]
	if status != VirtIOBlkSOK {
		t.Errorf("read status = %d, want %d (OK)", status, VirtIOBlkSOK)
	}

	// Verify data matches
	for i := 0; i < devices.SectorSize; i++ {
		expected := byte(i ^ 0xAA)
		if dataRAM[i] != expected {
			t.Errorf("roundtrip data[%d] = %d, want %d", i, dataRAM[i], expected)
			break
		}
	}
}

// TestBlockReadAtDifferentSectors tests reading from different sectors.
func TestBlockReadAtDifferentSectors(t *testing.T) {
	s := newBlockTestSetup(t, 4096) // 8 sectors

	// Write unique data to each sector
	for sector := uint64(0); sector < 8; sector++ {
		data := make([]byte, devices.SectorSize)
		for i := range data {
			data[i] = byte(sector)
		}
		s.writeToUnderlyingDevice(sector, data)
	}

	// Test reading from sectors 0, 3, and 7
	testSectors := []uint64{0, 3, 7}

	for i, sector := range testSectors {
		s.writeBlockHeader(VirtIOBlkTIn, sector)
		descBase := i * 2
		s.writeDescriptor(descBase, s.headerAddr, 16, VRingDescFNext, uint16(descBase+1))
		s.writeDescriptor(descBase+1, s.dataAddr, uint32(devices.SectorSize+1), VRingDescFWrite, 0)
		s.setAvailableRingEntry(uint16(i+1), uint16(descBase))
		s.notify()

		dataRAM := s.memMap.GetRAMPtr(s.dataAddr, false)
		// Status check
		if dataRAM[devices.SectorSize] != VirtIOBlkSOK {
			t.Errorf("sector %d read status = %d, want OK", sector, dataRAM[devices.SectorSize])
			continue
		}
		// Data check
		if dataRAM[0] != byte(sector) {
			t.Errorf("sector %d data = %d, want %d", sector, dataRAM[0], byte(sector))
		}
	}
}

// TestBlockQueueNotifyCallbackFlow tests the complete queue notification callback flow.
// Reference: virtio.c lines 518-544 (queue_notify)
func TestBlockQueueNotifyCallbackFlow(t *testing.T) {
	s := newBlockTestSetup(t, 4096)

	// Track callback invocations
	var recvCalls []struct {
		queueIdx  int
		descIdx   int
		readSize  int
		writeSize int
	}

	origRecv := s.dev.DeviceRecv
	s.dev.DeviceRecv = func(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
		recvCalls = append(recvCalls, struct {
			queueIdx  int
			descIdx   int
			readSize  int
			writeSize int
		}{queueIdx, descIdx, readSize, writeSize})
		return origRecv(dev, queueIdx, descIdx, readSize, writeSize)
	}

	// Submit a read request
	s.writeBlockHeader(VirtIOBlkTIn, 0)
	s.writeDescriptor(0, s.headerAddr, 16, VRingDescFNext, 1)
	s.writeDescriptor(1, s.dataAddr, uint32(devices.SectorSize+1), VRingDescFWrite, 0)
	s.setAvailableRingEntry(1, 0)
	s.notify()

	// Verify callback was invoked with correct parameters
	if len(recvCalls) != 1 {
		t.Fatalf("callback called %d times, want 1", len(recvCalls))
	}

	call := recvCalls[0]
	if call.queueIdx != 0 {
		t.Errorf("queueIdx = %d, want 0", call.queueIdx)
	}
	if call.descIdx != 0 {
		t.Errorf("descIdx = %d, want 0", call.descIdx)
	}
	if call.readSize != 16 {
		t.Errorf("readSize = %d, want 16 (header)", call.readSize)
	}
	if call.writeSize != devices.SectorSize+1 {
		t.Errorf("writeSize = %d, want %d (data+status)", call.writeSize, devices.SectorSize+1)
	}

	// Verify request was completed (used ring updated, IRQ raised)
	if s.getUsedRingIdx() != 1 {
		t.Errorf("used ring index = %d, want 1", s.getUsedRingIdx())
	}
	if s.irqState.level != 1 {
		t.Error("IRQ should be raised after request completion")
	}
}
