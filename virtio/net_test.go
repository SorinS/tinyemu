package virtio

import (
	"encoding/binary"
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// newTestNet creates a VirtIO network device for testing.
func newTestNet(t *testing.T) (*Net, *mem.PhysMemoryMap, *testIRQState, *[][]byte, *[][]byte) {
	t.Helper()

	memMap := mem.NewPhysMemoryMap()

	// Allocate RAM at 0x80000000 for descriptor tables, etc.
	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, irqState := newTestIRQ()

	// Create ethernet device with slices for tracking packets
	txPackets := &[][]byte{}
	rxPackets := &[][]byte{}

	es := &EthernetDevice{
		MACAddr: [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56},
		WritePacket: func(buf []byte) {
			// Copy the packet to avoid reference issues
			pkt := make([]byte, len(buf))
			copy(pkt, buf)
			*txPackets = append(*txPackets, pkt)
		},
	}

	net, err := NewNet(memMap, 0x10000000, irq, es)
	if err != nil {
		t.Fatalf("failed to create net device: %v", err)
	}

	return net, memMap, irqState, txPackets, rxPackets
}

// setupNetQueues sets up the RX and TX queues for testing.
func setupNetQueues(t *testing.T, net *Net, memMap *mem.PhysMemoryMap) {
	t.Helper()

	ramBase := uint64(0x80000000)
	dev := net.Device()

	// Negotiate MRG_RXBUF (the Linux virtio_net path) so the header is the
	// 12-byte NetHeaderSize these tests assume. The OVMF/UEFI path (no
	// MRG_RXBUF → 10-byte header) is covered by TestNetHeaderSizeByFeature.
	dev.GuestFeatures = NetFeatureMAC | NetFeatureMRG_RXBUF

	// Set up RX queue (queue 0)
	rxDescAddr := ramBase
	rxAvailAddr := ramBase + 0x1000
	rxUsedAddr := ramBase + 0x2000

	dev.write(nil, MMIOQueueSel, NetQueueRX, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(rxDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(rxAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(rxUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)

	// Set up TX queue (queue 1)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txUsedAddr := ramBase + 0x6000

	dev.write(nil, MMIOQueueSel, NetQueueTX, 2)
	dev.write(nil, MMIOQueueNum, 4, 2)
	dev.write(nil, MMIOQueueDescLow, uint32(txDescAddr), 2)
	dev.write(nil, MMIOQueueAvailLow, uint32(txAvailAddr), 2)
	dev.write(nil, MMIOQueueUsedLow, uint32(txUsedAddr), 2)
	dev.write(nil, MMIOQueueReady, 1, 2)
}

// TestNetHeaderSizeByFeature: the virtio-net header size must follow the
// NEGOTIATED MRG_RXBUF feature, not what the device offered. Linux acks
// MRG_RXBUF → 12-byte header; OVMF's VirtioNetDxe does not → legacy 10-byte
// header. Using a fixed 12 mis-framed every UEFI-networking packet by 2
// bytes (the reason a Go web server on go-boot couldn't be reached).
func TestNetHeaderSizeByFeature(t *testing.T) {
	net, _, _, _, _ := newTestNet(t)

	net.dev.GuestFeatures = NetFeatureMAC // no MRG_RXBUF (OVMF path)
	if got := net.hdrSize(); got != 10 {
		t.Errorf("hdrSize without MRG_RXBUF = %d, want 10", got)
	}

	net.dev.GuestFeatures = NetFeatureMAC | NetFeatureMRG_RXBUF // Linux path
	if got := net.hdrSize(); got != NetHeaderSize {
		t.Errorf("hdrSize with MRG_RXBUF = %d, want %d", got, NetHeaderSize)
	}
}

// TestNetCreation tests basic network device creation.
// Reference: tinyemu-2019-12-21/virtio.c:1235-1258 (virtio_net_init)
func TestNetCreation(t *testing.T) {
	net, _, _, _, _ := newTestNet(t)

	dev := net.Device()

	// Check device ID
	id := dev.read(nil, MMIODeviceID, 2)
	if id != DeviceIDNet {
		t.Errorf("device ID = %d, want %d", id, DeviceIDNet)
	}

	// Check features (should include NET_F_MAC)
	dev.write(nil, MMIODeviceFeaturesSel, 0, 2)
	features := dev.read(nil, MMIODeviceFeatures, 2)
	if features&NetFeatureMAC == 0 {
		t.Error("NET_F_MAC feature not set")
	}

	// Check that RX queue has manual_recv set
	// Reference: tinyemu-2019-12-21/virtio.c:1244
	if !net.dev.Queues[NetQueueRX].ManualRecv {
		t.Error("RX queue ManualRecv should be true")
	}

	// Check that TX queue does not have manual_recv set
	if net.dev.Queues[NetQueueTX].ManualRecv {
		t.Error("TX queue ManualRecv should be false")
	}
}

// TestNetConfigSpace tests that MAC address is in config space.
// Reference: tinyemu-2019-12-21/virtio.c:1246-1249
func TestNetConfigSpace(t *testing.T) {
	net, _, _, _, _ := newTestNet(t)
	dev := net.Device()

	// Read MAC address from config space
	mac := make([]byte, 6)
	for i := 0; i < 6; i++ {
		mac[i] = byte(dev.read(nil, MMIOConfig+uint32(i), 1))
	}

	expected := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	for i := 0; i < 6; i++ {
		if mac[i] != expected[i] {
			t.Errorf("MAC[%d] = %02x, want %02x", i, mac[i], expected[i])
		}
	}

	// Check status bytes are 0
	status0 := dev.read(nil, MMIOConfig+6, 1)
	status1 := dev.read(nil, MMIOConfig+7, 1)
	if status0 != 0 || status1 != 0 {
		t.Errorf("status = [%d, %d], want [0, 0]", status0, status1)
	}
}

// TestNetTX tests transmitting a packet from guest to network.
// Reference: tinyemu-2019-12-21/virtio.c:1153-1175 (virtio_net_recv_request)
func TestNetTX(t *testing.T) {
	net, memMap, irqState, txPackets, _ := newTestNet(t)
	setupNetQueues(t, net, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Total size = header (12 bytes) + packet data
	headerSize := NetHeaderSize
	packetData := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	totalSize := headerSize + len(packetData)

	// Set up TX descriptor (read descriptor - guest sends to host)
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], uint32(totalSize))
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0) // No flags (read descriptor)

	// Write header (zeros) and packet data to buffer
	dataRAM := memMap.GetRAMPtr(txDataAddr, true)
	// Header is zeroed by default
	copy(dataRAM[headerSize:], packetData)

	// Set up available ring with one entry
	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset:], 0)   // flags
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx (1 entry available)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	// Notify the TX queue
	net.dev.write(nil, MMIOQueueNotify, NetQueueTX, 2)

	// Check that packet was transmitted (without header)
	if len(*txPackets) != 1 {
		t.Fatalf("expected 1 TX packet, got %d", len(*txPackets))
	}

	pkt := (*txPackets)[0]
	if len(pkt) != len(packetData) {
		t.Errorf("TX packet length = %d, want %d", len(pkt), len(packetData))
	}

	for i, b := range packetData {
		if pkt[i] != b {
			t.Errorf("TX packet[%d] = %02x, want %02x", i, pkt[i], b)
		}
	}

	// Check that interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after TX completion")
	}
}

// TestNetRXCanWritePacket tests checking if guest has RX buffer ready.
// Reference: tinyemu-2019-12-21/virtio.c:1177-1187 (virtio_net_can_write_packet)
func TestNetRXCanWritePacket(t *testing.T) {
	net, memMap, _, _, _ := newTestNet(t)
	setupNetQueues(t, net, memMap)

	// Initially no buffers available
	if net.CanWritePacket() {
		t.Error("CanWritePacket should return false when no buffers available")
	}

	ramBase := uint64(0x80000000)
	rxAvailAddr := ramBase + 0x1000
	rxDataAddr := ramBase + 0x3000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up an RX descriptor (write descriptor - host writes to guest)
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 1500) // MTU-sized buffer
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	// Set up available ring with one entry
	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset:], 0)   // flags
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx (1 entry available)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	// Now buffer should be available
	if !net.CanWritePacket() {
		t.Error("CanWritePacket should return true when buffer available")
	}
}

// TestNetRXWritePacket tests receiving a packet from network to guest.
// Reference: tinyemu-2019-12-21/virtio.c:1189-1217 (virtio_net_write_packet)
func TestNetRXWritePacket(t *testing.T) {
	net, memMap, irqState, _, _ := newTestNet(t)
	setupNetQueues(t, net, memMap)

	ramBase := uint64(0x80000000)
	rxAvailAddr := ramBase + 0x1000
	rxUsedAddr := ramBase + 0x2000
	rxDataAddr := ramBase + 0x3000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up an RX descriptor (write descriptor)
	bufferSize := 1500
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr)
	binary.LittleEndian.PutUint32(ram[8:], uint32(bufferSize))
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	// Set up available ring
	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1) // idx = 1
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0) // ring[0] = desc 0

	// Write packet to guest
	packetData := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22}
	net.WritePacket(packetData)

	// Check that header + packet data was written to guest memory
	dataRAM := memMap.GetRAMPtr(rxDataAddr, false)

	// First 12 bytes are the virtio-net header: all zero except
	// num_buffers (offset 10, little-endian) which must be 1.
	for i := 0; i < NetHeaderSize; i++ {
		want := byte(0)
		if i == 10 {
			want = 1
		}
		if dataRAM[i] != want {
			t.Errorf("header[%d] = %02x, want %02x", i, dataRAM[i], want)
		}
	}

	// After header should be packet data
	for i, b := range packetData {
		if dataRAM[NetHeaderSize+i] != b {
			t.Errorf("packet[%d] = %02x, want %02x", i, dataRAM[NetHeaderSize+i], b)
		}
	}

	// Check used ring
	usedOffset := int(rxUsedAddr - ramBase)
	usedIdx := binary.LittleEndian.Uint16(ram[usedOffset+2:])
	if usedIdx != 1 {
		t.Errorf("used idx = %d, want 1", usedIdx)
	}

	usedLen := binary.LittleEndian.Uint32(ram[usedOffset+8:])
	expectedLen := uint32(NetHeaderSize + len(packetData))
	if usedLen != expectedLen {
		t.Errorf("used len = %d, want %d", usedLen, expectedLen)
	}

	// Check interrupt was raised
	if irqState.level != 1 {
		t.Error("IRQ should be raised after WritePacket")
	}
}

// TestNetRXWritePacketNoBuffer tests WritePacket when no buffer available.
// Reference: tinyemu-2019-12-21/virtio.c:1200-1204
func TestNetRXWritePacketNoBuffer(t *testing.T) {
	net, memMap, irqState, _, _ := newTestNet(t)
	setupNetQueues(t, net, memMap)

	// Try to write without any available buffers
	// Should not panic or cause issues
	net.WritePacket([]byte{0x01, 0x02, 0x03})

	// IRQ should not be raised
	if irqState.level != 0 {
		t.Error("IRQ should not be raised when no buffer available")
	}
}

// TestNetRXWritePacketTooLarge tests WritePacket with packet larger than buffer.
// Reference: tinyemu-2019-12-21/virtio.c:1210-1211
func TestNetRXWritePacketTooLarge(t *testing.T) {
	net, memMap, irqState, _, _ := newTestNet(t)
	setupNetQueues(t, net, memMap)

	ramBase := uint64(0x80000000)
	rxAvailAddr := ramBase + 0x1000
	rxDataAddr := ramBase + 0x3000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up an RX descriptor with small buffer (20 bytes)
	// Header is 12 bytes, so only 8 bytes for packet data
	binary.LittleEndian.PutUint64(ram[0:], rxDataAddr)
	binary.LittleEndian.PutUint32(ram[8:], 20) // Small buffer
	binary.LittleEndian.PutUint16(ram[12:], VRingDescFWrite)

	availOffset := int(rxAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	// Try to write a packet larger than buffer
	largePacket := make([]byte, 100)
	net.WritePacket(largePacket)

	// Packet should be dropped, no IRQ
	if irqState.level != 0 {
		t.Error("IRQ should not be raised when packet too large")
	}
}

// TestNetTXHeaderOnly tests TX with only header (no packet data).
// Reference: tinyemu-2019-12-21/virtio.c:1165-1172
func TestNetTXHeaderOnly(t *testing.T) {
	net, memMap, irqState, txPackets, _ := newTestNet(t)
	setupNetQueues(t, net, memMap)

	ramBase := uint64(0x80000000)
	txDescAddr := ramBase + 0x4000
	txAvailAddr := ramBase + 0x5000
	txDataAddr := ramBase + 0x7000

	ram := memMap.GetRAMPtr(ramBase, true)

	// Set up TX descriptor with only header size
	descOffset := int(txDescAddr - ramBase)
	binary.LittleEndian.PutUint64(ram[descOffset:], txDataAddr)
	binary.LittleEndian.PutUint32(ram[descOffset+8:], uint32(NetHeaderSize)) // Only header
	binary.LittleEndian.PutUint16(ram[descOffset+12:], 0)

	availOffset := int(txAvailAddr - ramBase)
	binary.LittleEndian.PutUint16(ram[availOffset+2:], 1)
	binary.LittleEndian.PutUint16(ram[availOffset+4:], 0)

	net.dev.write(nil, MMIOQueueNotify, NetQueueTX, 2)

	// Should consume descriptor but not call WritePacket (no data)
	if len(*txPackets) != 0 {
		t.Errorf("expected 0 TX packets for header-only, got %d", len(*txPackets))
	}

	// Descriptor should still be consumed
	if irqState.level != 1 {
		t.Error("IRQ should be raised even for header-only TX")
	}
}

// TestNetEthernetDeviceCallbacks tests that callbacks are set on EthernetDevice.
// Reference: tinyemu-2019-12-21/virtio.c:1253-1256
func TestNetEthernetDeviceCallbacks(t *testing.T) {
	memMap := mem.NewPhysMemoryMap()
	_, err := memMap.RegisterRAM(0x80000000, 0x10000, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}

	irq, _ := newTestIRQ()

	es := &EthernetDevice{
		MACAddr: [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56},
	}

	net, err := NewNet(memMap, 0x10000000, irq, es)
	if err != nil {
		t.Fatalf("failed to create net device: %v", err)
	}

	// Check that callbacks were set
	if es.DeviceOpaque != net {
		t.Error("DeviceOpaque should be set to Net device")
	}
	if es.DeviceCanWritePacket == nil {
		t.Error("DeviceCanWritePacket callback should be set")
	}
	if es.DeviceWritePacket == nil {
		t.Error("DeviceWritePacket callback should be set")
	}
	if es.DeviceSetCarrier == nil {
		t.Error("DeviceSetCarrier callback should be set")
	}
}

// TestNetSetCarrier tests the SetCarrier function (currently no-op).
// Reference: tinyemu-2019-12-21/virtio.c:1219-1233 (disabled with #if 0)
func TestNetSetCarrier(t *testing.T) {
	net, _, _, _, _ := newTestNet(t)

	// Should not panic
	net.SetCarrier(true)
	net.SetCarrier(false)
}

// TestNetSetDebug tests the SetDebug function.
func TestNetSetDebug(t *testing.T) {
	net, _, _, _, _ := newTestNet(t)

	// Should not panic
	net.SetDebug(DebugIO)
	net.SetDebug(0)
}

// TestNetQueueNotReady tests operations when queues are not ready.
func TestNetQueueNotReady(t *testing.T) {
	net, memMap, _, _, _ := newTestNet(t)

	// Don't set up queues
	_ = memMap

	// CanWritePacket should return false
	if net.CanWritePacket() {
		t.Error("CanWritePacket should return false when queue not ready")
	}

	// WritePacket should do nothing
	net.WritePacket([]byte{0x01, 0x02, 0x03})
	// No panic is success
}

// TestNetHeaderSize tests that header size constant matches C code.
// Reference: tinyemu-2019-12-21/virtio.c:1143-1151
func TestNetHeaderSize(t *testing.T) {
	// VIRTIONetHeader in C is:
	// uint8_t flags (1) + uint8_t gso_type (1) + uint16_t hdr_len (2) +
	// uint16_t gso_size (2) + uint16_t csum_start (2) + uint16_t csum_offset (2) +
	// uint16_t num_buffers (2) = 12 bytes
	if NetHeaderSize != 12 {
		t.Errorf("NetHeaderSize = %d, want 12", NetHeaderSize)
	}
}

// TestMaxQueueNumLinuxNetThreshold asserts that the ring size we advertise
// is large enough to satisfy Linux's virtio_net flow-control threshold.
// virtnet_poll_tx (drivers/net/virtio_net.c) only re-wakes the TX subqueue
// when num_free >= 2 + MAX_SKB_FRAGS. MAX_SKB_FRAGS is 17 on x86_64 (and
// higher on some arches), so a ring smaller than 19 freezes after one TX:
// num_free can never reach the wake threshold. We pin this at >= 32 so
// future MAX_SKB_FRAGS bumps don't silently regress the boot.
func TestMaxQueueNumLinuxNetThreshold(t *testing.T) {
	const linuxMaxSkbFragsCeiling = 32 // safe upper bound across arches
	const wakeMargin = 2                // virtio_net adds 2 (descriptors for hdr + skb head)
	required := linuxMaxSkbFragsCeiling + wakeMargin
	if MaxQueueNum < required {
		t.Errorf("MaxQueueNum=%d too small for Linux virtio_net: needs >= %d "+
			"(2 + MAX_SKB_FRAGS) or TX subqueue stops after first packet",
			MaxQueueNum, required)
	}
	if MaxQueueNum&(MaxQueueNum-1) != 0 {
		t.Errorf("MaxQueueNum=%d must be a power of two (used as ring-wrap mask)",
			MaxQueueNum)
	}
}
