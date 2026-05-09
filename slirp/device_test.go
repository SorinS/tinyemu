package slirp

import (
	"net"
	"testing"

	"github.com/jtolio/tinyemu-go/virtio"
)

// TestNewEthernetDevice tests creating a Slirp-backed EthernetDevice.
// Reference: tinyemu-2019-12-21/temu.c:497-530 (slirp_open)
func TestNewEthernetDevice(t *testing.T) {
	es := NewEthernetDevice()
	if es == nil {
		t.Fatal("NewEthernetDevice returned nil")
	}

	// Check default MAC address
	// Reference: tinyemu-2019-12-21/temu.c:518-523
	expectedMAC := [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	if es.MACAddr != expectedMAC {
		t.Errorf("MACAddr = %v, want %v", es.MACAddr, expectedMAC)
	}

	// Check that WritePacket callback is set
	if es.WritePacket == nil {
		t.Error("WritePacket callback should be set")
	}

	// Check that Opaque is set (contains slirpDeviceState)
	if es.Opaque == nil {
		t.Error("Opaque should be set")
	}

	// Check that we can get the underlying Slirp instance
	slirp := GetSlirp(es)
	if slirp == nil {
		t.Fatal("GetSlirp returned nil")
	}

	// Check default network configuration
	// Reference: tinyemu-2019-12-21/temu.c:500-504
	if !slirp.VNetworkAddr.Equal(net.IPv4(10, 0, 2, 0)) {
		t.Errorf("VNetworkAddr = %v, want 10.0.2.0", slirp.VNetworkAddr)
	}
	if !slirp.VNetworkMask.Equal(net.IPv4(255, 255, 255, 0)) {
		t.Errorf("VNetworkMask = %v, want 255.255.255.0", slirp.VNetworkMask)
	}
	if !slirp.VHostAddr.Equal(net.IPv4(10, 0, 2, 2)) {
		t.Errorf("VHostAddr = %v, want 10.0.2.2", slirp.VHostAddr)
	}
	if !slirp.VDHCPStartAddr.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Errorf("VDHCPStartAddr = %v, want 10.0.2.15", slirp.VDHCPStartAddr)
	}
	if !slirp.VNameserverAddr.Equal(net.IPv4(10, 0, 2, 3)) {
		t.Errorf("VNameserverAddr = %v, want 10.0.2.3", slirp.VNameserverAddr)
	}
}

// TestNewEthernetDeviceWithConfig tests creating with custom configuration.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:189-226 (slirp_init)
func TestNewEthernetDeviceWithConfig(t *testing.T) {
	cfg := SlirpConfig{
		Network:   net.IPv4(192, 168, 100, 0),
		Netmask:   net.IPv4(255, 255, 255, 0),
		Host:      net.IPv4(192, 168, 100, 1),
		DHCPStart: net.IPv4(192, 168, 100, 10),
		DNS:       net.IPv4(192, 168, 100, 1),
	}

	es := NewEthernetDeviceWithConfig(cfg)
	if es == nil {
		t.Fatal("NewEthernetDeviceWithConfig returned nil")
	}

	slirp := GetSlirp(es)
	if slirp == nil {
		t.Fatal("GetSlirp returned nil")
	}

	if !slirp.VNetworkAddr.Equal(net.IPv4(192, 168, 100, 0)) {
		t.Errorf("VNetworkAddr = %v, want 192.168.100.0", slirp.VNetworkAddr)
	}
	if !slirp.VHostAddr.Equal(net.IPv4(192, 168, 100, 1)) {
		t.Errorf("VHostAddr = %v, want 192.168.100.1", slirp.VHostAddr)
	}
}

// TestGetSlirpNil tests GetSlirp with nil and non-slirp devices.
func TestGetSlirpNil(t *testing.T) {
	// Test nil device
	if GetSlirp(nil) != nil {
		t.Error("GetSlirp(nil) should return nil")
	}

	// Test device with nil Opaque
	es := &virtio.EthernetDevice{}
	if GetSlirp(es) != nil {
		t.Error("GetSlirp with nil Opaque should return nil")
	}

	// Test device with wrong Opaque type
	es.Opaque = "wrong type"
	if GetSlirp(es) != nil {
		t.Error("GetSlirp with wrong Opaque type should return nil")
	}
}

// TestSlirpOutputCallback tests the output callback wiring.
// Reference: tinyemu-2019-12-21/temu.c:475-479 (slirp_output)
func TestSlirpOutputCallback(t *testing.T) {
	es := NewEthernetDevice()
	slirp := GetSlirp(es)

	// Set up a capture for DeviceWritePacket
	var capturedPacket []byte
	es.DeviceWritePacket = func(pkt []byte) {
		capturedPacket = make([]byte, len(pkt))
		copy(capturedPacket, pkt)
	}

	// Simulate Slirp sending a packet via OutputFunc
	testPacket := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	slirp.OutputFunc(slirp.Opaque, testPacket)

	// Check the packet was forwarded
	if capturedPacket == nil {
		t.Fatal("DeviceWritePacket was not called")
	}
	if len(capturedPacket) != len(testPacket) {
		t.Fatalf("capturedPacket length = %d, want %d", len(capturedPacket), len(testPacket))
	}
	for i, b := range testPacket {
		if capturedPacket[i] != b {
			t.Errorf("capturedPacket[%d] = %02x, want %02x", i, capturedPacket[i], b)
		}
	}
}

// TestSlirpCanOutputCallback tests the can_output callback wiring.
// Reference: tinyemu-2019-12-21/temu.c:469-473 (slirp_can_output)
func TestSlirpCanOutputCallback(t *testing.T) {
	es := NewEthernetDevice()
	slirp := GetSlirp(es)

	// Without DeviceCanWritePacket set, should return false
	if slirp.CanOutput(slirp.Opaque) {
		t.Error("CanOutput should return false when DeviceCanWritePacket is nil")
	}

	// Set DeviceCanWritePacket to return true
	es.DeviceCanWritePacket = func() bool { return true }
	if !slirp.CanOutput(slirp.Opaque) {
		t.Error("CanOutput should return true when DeviceCanWritePacket returns true")
	}

	// Set DeviceCanWritePacket to return false
	es.DeviceCanWritePacket = func() bool { return false }
	if slirp.CanOutput(slirp.Opaque) {
		t.Error("CanOutput should return false when DeviceCanWritePacket returns false")
	}
}

// TestSlirpWritePacketCallback tests the write_packet callback wiring.
// Reference: tinyemu-2019-12-21/temu.c:462-467 (slirp_write_packet)
func TestSlirpWritePacketCallback(t *testing.T) {
	es := NewEthernetDevice()
	slirp := GetSlirp(es)

	// Create a valid ARP packet and send it via WritePacket
	// This tests that WritePacket calls slirp.Input
	pkt := make([]byte, EthHLen+28)
	// Set up ethernet header
	for i := 0; i < EthALen; i++ {
		pkt[i] = 0xff // broadcast destination
	}
	copy(pkt[EthALen:EthALen+EthALen], []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56})
	// EtherType: ARP
	pkt[12] = 0x08
	pkt[13] = 0x06

	// ARP header - request for vhost address
	arp := pkt[EthHLen:]
	arp[0] = 0x00
	arp[1] = 0x01 // hardware type
	arp[2] = 0x08
	arp[3] = 0x00 // protocol type
	arp[4] = 0x06 // hardware size
	arp[5] = 0x04 // protocol size
	arp[6] = 0x00
	arp[7] = 0x01 // opcode: request
	// Sender hardware address
	copy(arp[8:14], []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56})
	// Sender IP
	arp[14] = 10
	arp[15] = 0
	arp[16] = 2
	arp[17] = 15
	// Target IP: vhost (10.0.2.2)
	arp[24] = 10
	arp[25] = 0
	arp[26] = 2
	arp[27] = 2

	// Set up output capture
	var output []byte
	slirp.OutputFunc = func(opaque interface{}, pkt []byte) {
		output = make([]byte, len(pkt))
		copy(output, pkt)
	}

	// Call WritePacket - this should trigger ARP handling
	es.WritePacket(pkt)

	// We should get an ARP reply
	if output == nil {
		t.Error("No ARP reply was generated")
	}
}

// TestPoll tests the Poll function.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:357-534 (slirp_select_poll)
func TestPoll(t *testing.T) {
	es := NewEthernetDevice()

	// Poll should not panic
	Poll(es)
}

// TestPollNilDevice tests Poll with nil device.
func TestPollNilDevice(t *testing.T) {
	// Should not panic
	Poll(nil)
}

// TestPollNonSlirpDevice tests Poll with non-slirp device.
func TestPollNonSlirpDevice(t *testing.T) {
	es := &virtio.EthernetDevice{}

	// Should not panic
	Poll(es)
}

// TestIPSlowtimo tests the IP slowtimo function.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:443-461
func TestIPSlowtimo(t *testing.T) {
	s := NewSlirp()
	// Should not panic with empty queue
	s.IPSlowtimo()
}

// TestProcessTimers tests the timer processing.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:374-382
func TestProcessTimers(t *testing.T) {
	s := NewSlirp()

	// Test with no timers pending
	s.processTimers(0)

	// Test with fast timer pending
	s.timeFastTimo = 1
	s.processTimers(100) // 100ms later
	if s.timeFastTimo != 0 {
		t.Error("timeFastTimo should be cleared after fast timeout")
	}

	// Test with slow timer pending
	s.doSlowTimo = true
	s.lastSlowTimo = 0
	s.processTimers(500) // 500ms later
	if s.lastSlowTimo != 500 {
		t.Errorf("lastSlowTimo = %d, want 500", s.lastSlowTimo)
	}
}

// TestUpdateTimerFlagsNoConnections tests updateTimerFlags with no connections.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:254-272
func TestUpdateTimerFlagsNoConnections(t *testing.T) {
	s := NewSlirp()

	// Initially doSlowTimo should be false
	s.doSlowTimo = true // Set to true first
	s.updateTimerFlags(100)

	// With no connections, doSlowTimo should be reset to false
	if s.doSlowTimo {
		t.Error("doSlowTimo should be false with no connections")
	}
}

// TestUpdateTimerFlagsWithTCPConnection tests updateTimerFlags with active TCP connection.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:261-262
func TestUpdateTimerFlagsWithTCPConnection(t *testing.T) {
	s := NewSlirp()

	// Add a TCP socket to the list
	so := &Socket{
		Next: &s.TCB,
		Prev: &s.TCB,
	}
	s.TCB.Next = so
	s.TCB.Prev = so

	s.updateTimerFlags(100)

	// With TCP connections, doSlowTimo should be true
	if !s.doSlowTimo {
		t.Error("doSlowTimo should be true with TCP connections")
	}
}

// TestUpdateTimerFlagsWithDelayedAck tests updateTimerFlags with delayed ACK flag.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:271-272
func TestUpdateTimerFlagsWithDelayedAck(t *testing.T) {
	s := NewSlirp()

	// Create a TCP socket with TCPCB that has TF_DELACK set
	tp := &TCPCB{
		TFlags: TFDelAck,
	}
	so := &Socket{
		SoTCPCB: tp,
		Next:    &s.TCB,
		Prev:    &s.TCB,
	}
	s.TCB.Next = so
	s.TCB.Prev = so
	s.timeFastTimo = 0

	curtime := uint32(12345)
	s.updateTimerFlags(curtime)

	// timeFastTimo should be set to current time
	if s.timeFastTimo != curtime {
		t.Errorf("timeFastTimo = %d, want %d", s.timeFastTimo, curtime)
	}
}

// TestUpdateTimerFlagsWithUDPExpire tests updateTimerFlags with UDP socket that has expire set.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:329-335
func TestUpdateTimerFlagsWithUDPExpire(t *testing.T) {
	s := NewSlirp()

	// Add a UDP socket with expiration
	so := &Socket{
		SoExpire: 1000, // Will expire at time 1000
		Next:     &s.UDB,
		Prev:     &s.UDB,
	}
	s.UDB.Next = so
	s.UDB.Prev = so

	s.updateTimerFlags(100)

	// With UDP expiration pending, doSlowTimo should be true
	if !s.doSlowTimo {
		t.Error("doSlowTimo should be true with UDP socket expiration pending")
	}
}

// TestUpdateTimerFlagsDetachesExpiredUDP tests that expired UDP sockets are detached.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:329-335
func TestUpdateTimerFlagsDetachesExpiredUDP(t *testing.T) {
	s := NewSlirp()

	// Add a UDP socket that is already expired (SoExpire < curtime)
	so := &Socket{
		Slirp:    s,
		SoExpire: 100, // Expired at time 100
		S:        -1,  // No actual socket fd
		Next:     &s.UDB,
		Prev:     &s.UDB,
	}
	s.UDB.Next = so
	s.UDB.Prev = so

	// Call updateTimerFlags with a time after the expiration
	s.updateTimerFlags(200) // curtime = 200 > 100 = SoExpire

	// Socket should be detached (list should be empty)
	if s.UDB.Next != &s.UDB {
		t.Error("Expired UDP socket should be detached from list")
	}
	if s.UDB.Prev != &s.UDB {
		t.Error("Expired UDP socket should be detached from list")
	}
}

// TestUpdateTimerFlagsKeepsNonExpiredUDP tests that non-expired UDP sockets are kept.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:333-334
func TestUpdateTimerFlagsKeepsNonExpiredUDP(t *testing.T) {
	s := NewSlirp()

	// Add a UDP socket that has NOT yet expired
	so := &Socket{
		Slirp:    s,
		SoExpire: 1000, // Will expire at time 1000
		S:        -1,   // No actual socket fd
		Next:     &s.UDB,
		Prev:     &s.UDB,
	}
	s.UDB.Next = so
	s.UDB.Prev = so

	// Call updateTimerFlags with a time before the expiration
	s.updateTimerFlags(100) // curtime = 100 < 1000 = SoExpire

	// Socket should NOT be detached
	if s.UDB.Next == &s.UDB {
		t.Error("Non-expired UDP socket should not be detached")
	}
	// doSlowTimo should be set
	if !s.doSlowTimo {
		t.Error("doSlowTimo should be true for pending expiration")
	}
}
