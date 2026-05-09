package slirp

import (
	"bytes"
	"net"
	"testing"
)

// TestEthernetConstants verifies the Ethernet constants match the C definitions.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:537-544
func TestEthernetConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      int
		expected int
	}{
		{"EthALen", EthALen, 6},
		{"EthHLen", EthHLen, 14},
		{"EthPIP", EthPIP, 0x0800},
		{"EthPARP", EthPARP, 0x0806},
		{"ARPOpRequest", ARPOpRequest, 1},
		{"ARPOpReply", ARPOpReply, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %d (0x%x), want %d (0x%x)", tt.name, tt.got, tt.got, tt.expected, tt.expected)
			}
		})
	}
}

// TestSpecialEthAddr verifies the special Ethernet address.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:30-31
func TestSpecialEthAddr(t *testing.T) {
	expected := [6]byte{0x52, 0x55, 0x00, 0x00, 0x00, 0x00}
	if SpecialEthAddr != expected {
		t.Errorf("SpecialEthAddr = %v, want %v", SpecialEthAddr, expected)
	}
}

// TestZeroEthAddr verifies the zero Ethernet address.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:34
func TestZeroEthAddr(t *testing.T) {
	expected := [6]byte{0, 0, 0, 0, 0, 0}
	if ZeroEthAddr != expected {
		t.Errorf("ZeroEthAddr = %v, want %v", ZeroEthAddr, expected)
	}
}

// TestNewSlirpInitialization verifies NewSlirp initializes all fields correctly.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:189-226 (slirp_init)
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:51-56 (tcp_init)
// Reference: tinyemu-2019-12-21/slirp/udp.c:47-51 (udp_init)
func TestNewSlirpInitialization(t *testing.T) {
	s := NewSlirp()

	// Verify default network configuration
	if !s.VNetworkAddr.Equal(net.IPv4(10, 0, 2, 0)) {
		t.Errorf("VNetworkAddr = %v, want 10.0.2.0", s.VNetworkAddr)
	}
	if !s.VNetworkMask.Equal(net.IPv4(255, 255, 255, 0)) {
		t.Errorf("VNetworkMask = %v, want 255.255.255.0", s.VNetworkMask)
	}
	if !s.VHostAddr.Equal(net.IPv4(10, 0, 2, 2)) {
		t.Errorf("VHostAddr = %v, want 10.0.2.2", s.VHostAddr)
	}
	if !s.VDHCPStartAddr.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Errorf("VDHCPStartAddr = %v, want 10.0.2.15", s.VDHCPStartAddr)
	}
	if !s.VNameserverAddr.Equal(net.IPv4(10, 0, 2, 3)) {
		t.Errorf("VNameserverAddr = %v, want 10.0.2.3", s.VNameserverAddr)
	}

	// Verify TCP initialization matches C tcp_init
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:53
	if s.TCPIss != 1 {
		t.Errorf("TCPIss = %d, want 1", s.TCPIss)
	}
	if s.TCB.Next != &s.TCB {
		t.Error("TCB.Next not pointing to self")
	}
	if s.TCB.Prev != &s.TCB {
		t.Error("TCB.Prev not pointing to self")
	}
	if s.TCPLastSo != &s.TCB {
		t.Error("TCPLastSo not pointing to TCB")
	}

	// Verify UDP initialization matches C udp_init
	if s.UDB.Next != &s.UDB {
		t.Error("UDB.Next not pointing to self")
	}
	if s.UDB.Prev != &s.UDB {
		t.Error("UDB.Prev not pointing to self")
	}
	if s.UDPLastSo != &s.UDB {
		t.Error("UDPLastSo not pointing to UDB")
	}
}

// TestIfEncapKnownMAC tests IfEncap when the client MAC is known.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:702-709
func TestIfEncapKnownMAC(t *testing.T) {
	s := NewSlirp()
	s.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	var output []byte
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		output = make([]byte, len(pkt))
		copy(output, pkt)
	}

	// Create a minimal IP packet (20 bytes header)
	ipPacket := make([]byte, 40)
	// Version 4, IHL 5
	ipPacket[0] = 0x45
	// Total length
	ipPacket[2] = 0
	ipPacket[3] = 40
	// TTL
	ipPacket[8] = 64
	// Protocol (TCP)
	ipPacket[9] = 6
	// Source IP: 10.0.2.2 (vhost)
	ipPacket[12] = 10
	ipPacket[13] = 0
	ipPacket[14] = 2
	ipPacket[15] = 2
	// Dest IP: 10.0.2.15
	ipPacket[16] = 10
	ipPacket[17] = 0
	ipPacket[18] = 2
	ipPacket[19] = 15

	s.IfEncap(ipPacket)

	if output == nil {
		t.Fatal("Output callback was not called")
	}

	if len(output) != len(ipPacket)+EthHLen {
		t.Fatalf("Output length = %d, want %d", len(output), len(ipPacket)+EthHLen)
	}

	// Check destination MAC
	if !bytes.Equal(output[0:6], s.ClientEthAddr[:]) {
		t.Errorf("Destination MAC = %x, want %x", output[0:6], s.ClientEthAddr)
	}

	// Check source MAC (special_ethaddr with vhost embedded)
	expectedSrcMAC := []byte{0x52, 0x55, 10, 0, 2, 2}
	if !bytes.Equal(output[6:12], expectedSrcMAC) {
		t.Errorf("Source MAC = %x, want %x", output[6:12], expectedSrcMAC)
	}

	// Check EtherType (IP)
	if output[12] != 0x08 || output[13] != 0x00 {
		t.Errorf("EtherType = %02x%02x, want 0800", output[12], output[13])
	}

	// Check IP packet is preserved
	if !bytes.Equal(output[14:], ipPacket) {
		t.Error("IP packet was not preserved correctly")
	}
}

// TestIfEncapUnknownMAC tests IfEncap when the client MAC is unknown.
// Should send an ARP request instead.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:672-701
func TestIfEncapUnknownMAC(t *testing.T) {
	s := NewSlirp()
	// ClientEthAddr is zero by default

	var output []byte
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		output = make([]byte, len(pkt))
		copy(output, pkt)
	}

	// Create a minimal IP packet
	ipPacket := make([]byte, 40)
	ipPacket[0] = 0x45
	ipPacket[2] = 0
	ipPacket[3] = 40
	ipPacket[8] = 64
	ipPacket[9] = 6
	// Source IP
	ipPacket[12] = 10
	ipPacket[13] = 0
	ipPacket[14] = 2
	ipPacket[15] = 2
	// Dest IP (this is what we're ARP'ing for)
	ipPacket[16] = 10
	ipPacket[17] = 0
	ipPacket[18] = 2
	ipPacket[19] = 15

	s.IfEncap(ipPacket)

	if output == nil {
		t.Fatal("Output callback was not called")
	}

	// ARP packet should be 14 (eth) + 28 (arp) = 42 bytes
	if len(output) != EthHLen+28 {
		t.Fatalf("ARP packet length = %d, want %d", len(output), EthHLen+28)
	}

	// Check destination MAC (broadcast)
	broadcast := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if !bytes.Equal(output[0:6], broadcast) {
		t.Errorf("Destination MAC = %x, want %x", output[0:6], broadcast)
	}

	// Check EtherType (ARP)
	if output[12] != 0x08 || output[13] != 0x06 {
		t.Errorf("EtherType = %02x%02x, want 0806", output[12], output[13])
	}

	// Check ARP operation (request)
	arp := output[EthHLen:]
	if arp[6] != 0x00 || arp[7] != 0x01 {
		t.Errorf("ARP operation = %02x%02x, want 0001", arp[6], arp[7])
	}

	// Check target IP (should be the IP destination from the IP packet)
	if !bytes.Equal(arp[24:28], ipPacket[16:20]) {
		t.Errorf("Target IP = %v, want %v", arp[24:28], ipPacket[16:20])
	}
}

// TestIfEncapTooLarge tests that IfEncap silently drops oversized packets.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:668-669
func TestIfEncapTooLarge(t *testing.T) {
	s := NewSlirp()
	s.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	var called bool
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		called = true
	}

	// Create an oversized packet (> 1600 - 14)
	ipPacket := make([]byte, 1600)
	s.IfEncap(ipPacket)

	if called {
		t.Error("OutputFunc should not be called for oversized packets")
	}
}

// TestIfEncapNoOutputFunc tests that IfEncap handles nil OutputFunc.
func TestIfEncapNoOutputFunc(t *testing.T) {
	s := NewSlirp()
	s.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	s.OutputFunc = nil

	// Should not panic
	ipPacket := make([]byte, 40)
	s.IfEncap(ipPacket)
}

// TestAddExec tests adding exec entries.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:756-771
func TestAddExec(t *testing.T) {
	s := NewSlirp()

	// Test with nil address (should use default)
	result := s.AddExec(0, "/bin/sh", nil, 22)
	if result != 0 {
		t.Errorf("AddExec with nil address returned %d, want 0", result)
	}

	if s.ExecList == nil {
		t.Fatal("ExecList is nil after AddExec")
	}

	// Check the exec entry
	ex := s.ExecList
	if ex.ExFPort != 22 {
		t.Errorf("ExFPort = %d, want 22", ex.ExFPort)
	}
	if ex.ExExec != "/bin/sh" {
		t.Errorf("ExExec = %s, want /bin/sh", ex.ExExec)
	}

	// Test with valid address in network range
	addr := net.IPv4(10, 0, 2, 20)
	result = s.AddExec(1, "/bin/bash", addr, 23)
	if result != 0 {
		t.Errorf("AddExec with valid address returned %d, want 0", result)
	}

	// New entry should be at head
	if s.ExecList.ExFPort != 23 {
		t.Errorf("New entry ExFPort = %d, want 23", s.ExecList.ExFPort)
	}
}

// TestAddExecInvalidAddress tests AddExec with invalid addresses.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:763-768
func TestAddExecInvalidAddress(t *testing.T) {
	s := NewSlirp()

	// Test with address outside virtual network
	addr := net.IPv4(192, 168, 1, 1)
	result := s.AddExec(0, "/bin/sh", addr, 22)
	if result != -1 {
		t.Errorf("AddExec with address outside network returned %d, want -1", result)
	}

	// Test with host address (should be rejected)
	result = s.AddExec(0, "/bin/sh", s.VHostAddr, 22)
	if result != -1 {
		t.Errorf("AddExec with host address returned %d, want -1", result)
	}

	// Test with nameserver address (should be rejected)
	result = s.AddExec(0, "/bin/sh", s.VNameserverAddr, 22)
	if result != -1 {
		t.Errorf("AddExec with nameserver address returned %d, want -1", result)
	}
}

// TestAddExecDuplicate tests that duplicate port/address combinations are rejected.
// Reference: tinyemu-2019-12-21/slirp/misc.c:46-50
func TestAddExecDuplicate(t *testing.T) {
	s := NewSlirp()

	addr := net.IPv4(10, 0, 2, 20)

	// First add should succeed
	result := s.AddExec(0, "/bin/sh", addr, 22)
	if result != 0 {
		t.Errorf("First AddExec returned %d, want 0", result)
	}

	// Duplicate add with same port/address should fail
	result = s.AddExec(0, "/bin/bash", addr, 22)
	if result != -1 {
		t.Errorf("Duplicate AddExec returned %d, want -1", result)
	}

	// Same port but different address should succeed
	addr2 := net.IPv4(10, 0, 2, 21)
	result = s.AddExec(0, "/bin/bash", addr2, 22)
	if result != 0 {
		t.Errorf("AddExec with different address returned %d, want 0", result)
	}

	// Same address but different port should succeed
	result = s.AddExec(0, "/bin/bash", addr, 23)
	if result != 0 {
		t.Errorf("AddExec with different port returned %d, want 0", result)
	}
}

// TestFindCtlSocket tests finding TCP sockets by address and port.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:785-796
func TestFindCtlSocket(t *testing.T) {
	s := NewSlirp()

	// Add a test socket to the TCB list
	so := &Socket{
		SoFAddr: net.IPv4(10, 0, 2, 15),
		SoFPort: 22,
		Next:    &s.TCB,
		Prev:    &s.TCB,
	}
	s.TCB.Next = so
	s.TCB.Prev = so

	// Find the socket
	found := s.FindCtlSocket(net.IPv4(10, 0, 2, 15), 22)
	if found == nil {
		t.Fatal("FindCtlSocket returned nil for existing socket")
	}
	if found != so {
		t.Error("FindCtlSocket returned wrong socket")
	}

	// Search for non-existent socket
	found = s.FindCtlSocket(net.IPv4(10, 0, 2, 16), 22)
	if found != nil {
		t.Error("FindCtlSocket should return nil for non-existent socket")
	}

	found = s.FindCtlSocket(net.IPv4(10, 0, 2, 15), 23)
	if found != nil {
		t.Error("FindCtlSocket should return nil for wrong port")
	}
}

// TestSocketCanRecv tests the socket receive capability check.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:798-813
func TestSocketCanRecv(t *testing.T) {
	s := NewSlirp()

	// Test with no socket
	result := s.SocketCanRecv(net.IPv4(10, 0, 2, 15), 22)
	if result != 0 {
		t.Errorf("SocketCanRecv with no socket = %d, want 0", result)
	}

	// Add a connected socket
	var sndBuf SBuf
	sndBuf.SbReserve(8192)
	so := &Socket{
		SoFAddr: net.IPv4(10, 0, 2, 15),
		SoFPort: 22,
		SoState: SSIsFConnected,
		SoSnd:   sndBuf,
		Next:    &s.TCB,
		Prev:    &s.TCB,
	}
	s.TCB.Next = so
	s.TCB.Prev = so

	// Should return available space
	result = s.SocketCanRecv(net.IPv4(10, 0, 2, 15), 22)
	if result == 0 {
		t.Error("SocketCanRecv should return non-zero for connected socket with space")
	}

	// Test with SS_NOFDREF flag
	so.SoState |= SSNoFDRef
	result = s.SocketCanRecv(net.IPv4(10, 0, 2, 15), 22)
	if result != 0 {
		t.Errorf("SocketCanRecv with SS_NOFDREF = %d, want 0", result)
	}
	so.SoState &^= SSNoFDRef

	// Test with half-full buffer
	so.SoSnd.SbReserve(8192)
	so.SoSnd.SbCC = 5000 // over half capacity
	result = s.SocketCanRecv(net.IPv4(10, 0, 2, 15), 22)
	if result != 0 {
		t.Errorf("SocketCanRecv with half-full buffer = %d, want 0", result)
	}
}

// TestSocketRecv tests receiving data on a socket.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:815-828
func TestSocketRecv(t *testing.T) {
	s := NewSlirp()

	// Add a socket
	var sndBuf SBuf
	sndBuf.SbReserve(8192)
	so := &Socket{
		Slirp:   s,
		SoFAddr: net.IPv4(10, 0, 2, 15),
		SoFPort: 22,
		SoState: SSIsFConnected,
		SoSnd:   sndBuf,
		Next:    &s.TCB,
		Prev:    &s.TCB,
	}
	s.TCB.Next = so
	s.TCB.Prev = so

	// Receive data
	data := []byte("Hello, World!")
	s.SocketRecv(net.IPv4(10, 0, 2, 15), 22, data)

	// Check data was appended
	if !bytes.Equal(so.SoSnd.SbBytes(), data) {
		t.Errorf("SoSnd = %v, want %v", so.SoSnd.SbBytes(), data)
	}

	// Receive more data
	moreData := []byte(" More data.")
	s.SocketRecv(net.IPv4(10, 0, 2, 15), 22, moreData)

	expected := append(data, moreData...)
	if !bytes.Equal(so.SoSnd.SbBytes(), expected) {
		t.Errorf("SoSnd after second recv = %v, want %v", so.SoSnd.SbBytes(), expected)
	}
}

// TestTCPListen tests creating a TCP listening socket.
// Reference: tinyemu-2019-12-21/slirp/socket.c:579-649
func TestTCPListen(t *testing.T) {
	s := NewSlirp()

	// Use port 0 to let the OS assign a port
	so := s.TCPListen(net.IPv4zero, 0, net.IPv4(10, 0, 2, 15), 22, SSHostFwd)
	if so == nil {
		t.Fatal("TCPListen returned nil")
	}

	// Clean up
	if listener, ok := so.Extra.(interface{ Close() error }); ok {
		listener.Close()
	}

	// Check socket is in TCB list
	found := false
	for cur := s.TCB.Next; cur != &s.TCB; cur = cur.Next {
		if cur == so {
			found = true
			break
		}
	}
	if !found {
		t.Error("Socket not found in TCB list")
	}

	// Check state flags
	if (so.SoState & SSFAcceptConn) == 0 {
		t.Error("SS_FACCEPTCONN flag not set")
	}
	if (so.SoState & SSHostFwd) == 0 {
		t.Error("SS_HOSTFWD flag not set")
	}

	// Check local address/port
	if so.SoLPort != 22 {
		t.Errorf("SoLPort = %d, want 22", so.SoLPort)
	}
	if !so.SoLAddr.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Errorf("SoLAddr = %v, want 10.0.2.15", so.SoLAddr)
	}

	// Check TCPCB was created
	if so.SoTCPCB == nil {
		t.Error("SoTCPCB is nil")
	}
}

// TestAddRemoveHostfwd tests adding and removing host forwarding rules.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:714-754
func TestAddRemoveHostfwd(t *testing.T) {
	s := NewSlirp()

	// Add TCP forwarding rule (use port 0 for auto-assign)
	result := s.AddHostfwd(false, net.IPv4zero, 0, net.IPv4(10, 0, 2, 15), 22)
	if result != 0 {
		t.Fatalf("AddHostfwd TCP returned %d, want 0", result)
	}

	// Verify socket exists in TCB
	var tcpSocket *Socket
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		if (so.SoState & SSHostFwd) != 0 {
			tcpSocket = so
			break
		}
	}
	if tcpSocket == nil {
		t.Fatal("TCP forwarding socket not found in TCB")
	}

	// Clean up
	if listener, ok := tcpSocket.Extra.(interface{ Close() error }); ok {
		listener.Close()
	}
}

// TestSlirpSend tests the SlirpSend function.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:773-782
func TestSlirpSend(t *testing.T) {
	// Test with nil socket
	result := SlirpSend(nil, []byte("test"), 0)
	if result != -1 {
		t.Errorf("SlirpSend with nil socket = %d, want -1", result)
	}

	// Test with invalid socket (S == -1)
	so := &Socket{S: -1}
	result = SlirpSend(so, []byte("test"), 0)
	if result != -1 {
		t.Errorf("SlirpSend with invalid socket = %d, want -1", result)
	}

	// Test with valid socket but no conn
	so2 := &Socket{S: 0}
	result = SlirpSend(so2, []byte("test"), 0)
	if result != -1 {
		t.Errorf("SlirpSend with no conn = %d, want -1", result)
	}
}

// TestRemoveHostfwd tests removing host forwarding rules.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:714-736
func TestRemoveHostfwd(t *testing.T) {
	s := NewSlirp()

	// Try to remove non-existent rule
	result := s.RemoveHostfwd(false, net.IPv4(10, 0, 2, 2), 8080)
	if result != -1 {
		t.Errorf("RemoveHostfwd non-existent = %d, want -1", result)
	}

	// Add a TCP forwarding rule
	err := s.AddHostfwd(false, net.IPv4zero, 0, net.IPv4(10, 0, 2, 15), 22)
	if err != 0 {
		t.Fatalf("AddHostfwd failed: %d", err)
	}

	// Find the socket and get its port
	var tcpSocket *Socket
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		if (so.SoState & SSHostFwd) != 0 {
			tcpSocket = so
			break
		}
	}
	if tcpSocket == nil {
		t.Fatal("TCP socket not found")
	}

	// Remove the rule using the socket's actual port and address
	result = s.RemoveHostfwd(false, tcpSocket.SoFAddr, int(tcpSocket.SoFPort))
	if result != 0 {
		t.Errorf("RemoveHostfwd existing = %d, want 0", result)
	}

	// Verify socket was removed from list
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		if so == tcpSocket {
			t.Error("Socket was not removed from TCB list")
		}
	}
}

// TestAddHostfwdUDP tests adding UDP host forwarding rules.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:738-754
func TestAddHostfwdUDP(t *testing.T) {
	s := NewSlirp()

	// Add UDP forwarding rule
	result := s.AddHostfwd(true, net.IPv4zero, 0, net.IPv4(10, 0, 2, 15), 53)
	if result != 0 {
		t.Fatalf("AddHostfwd UDP returned %d, want 0", result)
	}

	// Verify socket exists in UDB
	var udpSocket *Socket
	for so := s.UDB.Next; so != &s.UDB; so = so.Next {
		if (so.SoState & SSHostFwd) != 0 {
			udpSocket = so
			break
		}
	}
	if udpSocket == nil {
		t.Fatal("UDP forwarding socket not found in UDB")
	}

	// Clean up
	if conn, ok := udpSocket.Extra.(interface{ Close() error }); ok {
		conn.Close()
	}
}

// TestAddHostfwdDefaultGuestAddr tests that AddHostfwd uses DHCP start address when guest address is nil.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:741-743
func TestAddHostfwdDefaultGuestAddr(t *testing.T) {
	s := NewSlirp()

	// Add rule with nil guest address
	result := s.AddHostfwd(false, net.IPv4zero, 0, nil, 22)
	if result != 0 {
		t.Fatalf("AddHostfwd returned %d, want 0", result)
	}

	// Find the socket and check guest address
	var tcpSocket *Socket
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		if (so.SoState & SSHostFwd) != 0 {
			tcpSocket = so
			break
		}
	}
	if tcpSocket == nil {
		t.Fatal("TCP socket not found")
	}

	// Clean up
	if listener, ok := tcpSocket.Extra.(interface{ Close() error }); ok {
		listener.Close()
	}

	// Guest address should be the DHCP start address
	if !tcpSocket.SoLAddr.Equal(s.VDHCPStartAddr) {
		t.Errorf("SoLAddr = %v, want %v (VDHCPStartAddr)", tcpSocket.SoLAddr, s.VDHCPStartAddr)
	}
}

// TestSocketRecvNoSocket tests SocketRecv with non-existent socket.
func TestSocketRecvNoSocket(t *testing.T) {
	s := NewSlirp()

	// Should not panic
	s.SocketRecv(net.IPv4(10, 0, 2, 15), 22, []byte("test"))
}

// TestTCPOutput tests the TCPOutput function on a minimal TCPCB.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:56-477
func TestTCPOutput(t *testing.T) {
	s := NewSlirp()

	// Create a socket and attach it to slirp
	var sndBuf, rcvBuf SBuf
	sndBuf.SbReserve(8192)
	rcvBuf.SbReserve(8192)
	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   sndBuf,
		SoRcv:   rcvBuf,
	}

	// Create TCPCB with minimal state
	tp := &TCPCB{
		TSocket: so,
		TState:  TCPSEstablished,
		TMaxSeg: 1460,
		SndCwnd: 1460,
		SndWnd:  8192,
		TRxtCur: 1,
		TFlags:  TFAckNow, // Force sending
	}
	so.SoTCPCB = tp

	// Should not panic - will return 0 (nothing to send with current state)
	result := tp.TCPOutput()
	if result != 0 {
		t.Errorf("TCPOutput = %d, want 0", result)
	}
}

// TestARPInputRequest tests processing an ARP request for the host address.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:570-626
func TestARPInputRequest(t *testing.T) {
	s := NewSlirp()

	var output []byte
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		output = make([]byte, len(pkt))
		copy(output, pkt)
	}

	// Build an ARP request packet
	// Target IP: 10.0.2.2 (vhost address)
	pkt := make([]byte, EthHLen+28)

	// Ethernet header
	// Destination: broadcast
	for i := 0; i < EthALen; i++ {
		pkt[i] = 0xff
	}
	// Source MAC: guest's MAC
	guestMAC := []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	copy(pkt[EthALen:EthALen+EthALen], guestMAC)
	// EtherType: ARP
	pkt[12] = 0x08
	pkt[13] = 0x06

	// ARP header
	arp := pkt[EthHLen:]
	// Hardware type: Ethernet (1)
	arp[0] = 0x00
	arp[1] = 0x01
	// Protocol type: IP (0x0800)
	arp[2] = 0x08
	arp[3] = 0x00
	// Hardware address length: 6
	arp[4] = 0x06
	// Protocol address length: 4
	arp[5] = 0x04
	// Operation: ARP request (1)
	arp[6] = 0x00
	arp[7] = 0x01
	// Sender hardware address (guest's MAC)
	copy(arp[8:8+EthALen], guestMAC)
	// Sender protocol address: 10.0.2.15 (guest IP)
	arp[14] = 10
	arp[15] = 0
	arp[16] = 2
	arp[17] = 15
	// Target hardware address: zeros
	for i := 18; i < 24; i++ {
		arp[i] = 0
	}
	// Target protocol address: 10.0.2.2 (vhost)
	arp[24] = 10
	arp[25] = 0
	arp[26] = 2
	arp[27] = 2

	s.ARPInput(pkt)

	if output == nil {
		t.Fatal("No ARP reply was sent")
	}

	// Check ARP reply
	if len(output) < 64 {
		t.Fatalf("ARP reply too short: %d bytes", len(output))
	}

	// Check EtherType
	if output[12] != 0x08 || output[13] != 0x06 {
		t.Errorf("Reply EtherType = %02x%02x, want 0806", output[12], output[13])
	}

	// Check ARP operation (reply = 2)
	replyARP := output[EthHLen:]
	if replyARP[6] != 0x00 || replyARP[7] != 0x02 {
		t.Errorf("Reply ARP operation = %02x%02x, want 0002", replyARP[6], replyARP[7])
	}

	// Check sender IP (should be the target we asked about)
	if replyARP[14] != 10 || replyARP[15] != 0 || replyARP[16] != 2 || replyARP[17] != 2 {
		t.Errorf("Reply sender IP = %d.%d.%d.%d, want 10.0.2.2",
			replyARP[14], replyARP[15], replyARP[16], replyARP[17])
	}

	// Check that client MAC was learned
	if !bytes.Equal(s.ClientEthAddr[:], guestMAC) {
		t.Errorf("ClientEthAddr = %x, want %x", s.ClientEthAddr, guestMAC)
	}
}

// TestARPInputRequestNameserver tests ARP request for nameserver address.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:585-586
func TestARPInputRequestNameserver(t *testing.T) {
	s := NewSlirp()

	var output []byte
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		output = make([]byte, len(pkt))
		copy(output, pkt)
	}

	// Build an ARP request for nameserver (10.0.2.3)
	pkt := make([]byte, EthHLen+28)
	guestMAC := []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	// Ethernet header
	for i := 0; i < EthALen; i++ {
		pkt[i] = 0xff
	}
	copy(pkt[EthALen:EthALen+EthALen], guestMAC)
	pkt[12] = 0x08
	pkt[13] = 0x06

	// ARP header
	arp := pkt[EthHLen:]
	arp[0] = 0x00
	arp[1] = 0x01
	arp[2] = 0x08
	arp[3] = 0x00
	arp[4] = 0x06
	arp[5] = 0x04
	arp[6] = 0x00
	arp[7] = 0x01
	copy(arp[8:14], guestMAC)
	arp[14] = 10
	arp[15] = 0
	arp[16] = 2
	arp[17] = 15
	// Target: nameserver 10.0.2.3
	arp[24] = 10
	arp[25] = 0
	arp[26] = 2
	arp[27] = 3

	s.ARPInput(pkt)

	if output == nil {
		t.Fatal("No ARP reply for nameserver")
	}
}

// TestARPInputRequestOutsideNetwork tests ARP request for IP outside network.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:583-584
func TestARPInputRequestOutsideNetwork(t *testing.T) {
	s := NewSlirp()

	var outputCalled bool
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}

	// Build an ARP request for IP outside virtual network (192.168.1.1)
	pkt := make([]byte, EthHLen+28)
	guestMAC := []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	for i := 0; i < EthALen; i++ {
		pkt[i] = 0xff
	}
	copy(pkt[EthALen:EthALen+EthALen], guestMAC)
	pkt[12] = 0x08
	pkt[13] = 0x06

	arp := pkt[EthHLen:]
	arp[0] = 0x00
	arp[1] = 0x01
	arp[2] = 0x08
	arp[3] = 0x00
	arp[4] = 0x06
	arp[5] = 0x04
	arp[6] = 0x00
	arp[7] = 0x01
	copy(arp[8:14], guestMAC)
	arp[14] = 10
	arp[15] = 0
	arp[16] = 2
	arp[17] = 15
	// Target: outside network (192.168.1.1)
	arp[24] = 192
	arp[25] = 168
	arp[26] = 1
	arp[27] = 1

	s.ARPInput(pkt)

	if outputCalled {
		t.Error("Should not reply to ARP for IP outside network")
	}
}

// TestARPInputReply tests processing an ARP reply.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:616-621
func TestARPInputReply(t *testing.T) {
	s := NewSlirp()
	s.ClientIPAddr = net.IPv4(10, 0, 2, 15)
	s.ClientEthAddr = ZeroEthAddr // Unknown

	// Build an ARP reply
	pkt := make([]byte, EthHLen+28)
	replyMAC := []byte{0x52, 0x54, 0x00, 0xAB, 0xCD, 0xEF}

	// Ethernet header
	copy(pkt[0:EthALen], []byte{0x52, 0x55, 10, 0, 2, 2}) // our MAC
	copy(pkt[EthALen:EthALen+EthALen], replyMAC)
	pkt[12] = 0x08
	pkt[13] = 0x06

	// ARP header
	arp := pkt[EthHLen:]
	arp[0] = 0x00
	arp[1] = 0x01
	arp[2] = 0x08
	arp[3] = 0x00
	arp[4] = 0x06
	arp[5] = 0x04
	// Operation: ARP reply (2)
	arp[6] = 0x00
	arp[7] = 0x02
	// Sender hardware address
	copy(arp[8:14], replyMAC)
	// Sender IP: 10.0.2.15 (matches ClientIPAddr)
	arp[14] = 10
	arp[15] = 0
	arp[16] = 2
	arp[17] = 15

	s.ARPInput(pkt)

	// Client MAC should be learned
	if !bytes.Equal(s.ClientEthAddr[:], replyMAC) {
		t.Errorf("ClientEthAddr = %x, want %x", s.ClientEthAddr, replyMAC)
	}
}

// TestARPInputTooShort tests that short packets are ignored.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:570-573
func TestARPInputTooShort(t *testing.T) {
	s := NewSlirp()

	var outputCalled bool
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}

	// Packet too short
	pkt := make([]byte, EthHLen+10) // need at least EthHLen+28
	s.ARPInput(pkt)

	if outputCalled {
		t.Error("Should not process packet that is too short")
	}
}

// TestInputTooShort tests that Input ignores packets smaller than ethernet header.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:633-634
func TestInputTooShort(t *testing.T) {
	s := NewSlirp()

	// Should not panic
	s.Input(make([]byte, 5))
	s.Input(make([]byte, 0))
	s.Input(nil)
}

// TestInputARP tests that Input dispatches ARP packets.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:638-639
func TestInputARP(t *testing.T) {
	s := NewSlirp()

	var arpCalled bool
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		arpCalled = true
	}

	// Build a valid ARP request packet
	pkt := make([]byte, EthHLen+28)
	guestMAC := []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	for i := 0; i < EthALen; i++ {
		pkt[i] = 0xff
	}
	copy(pkt[EthALen:EthALen+EthALen], guestMAC)
	// EtherType: ARP
	pkt[12] = 0x08
	pkt[13] = 0x06

	// ARP request for vhost
	arp := pkt[EthHLen:]
	arp[0] = 0x00
	arp[1] = 0x01
	arp[2] = 0x08
	arp[3] = 0x00
	arp[4] = 0x06
	arp[5] = 0x04
	arp[6] = 0x00
	arp[7] = 0x01
	copy(arp[8:14], guestMAC)
	arp[14] = 10
	arp[15] = 0
	arp[16] = 2
	arp[17] = 15
	arp[24] = 10
	arp[25] = 0
	arp[26] = 2
	arp[27] = 2

	s.Input(pkt)

	if !arpCalled {
		t.Error("ARP processing was not triggered")
	}
}

// TestInputIP tests that Input dispatches IP packets.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:641-655
func TestInputIP(t *testing.T) {
	s := NewSlirp()

	// Build an ICMP echo request to vhost
	ipLen := 28 // 20 byte IP header + 8 byte ICMP
	pkt := make([]byte, EthHLen+ipLen)

	// Ethernet header
	copy(pkt[0:EthALen], []byte{0x52, 0x55, 10, 0, 2, 2})              // our MAC
	copy(pkt[EthALen:EthALen+EthALen], []byte{0x52, 0x54, 0, 0, 0, 1}) // guest MAC
	// EtherType: IP
	pkt[12] = 0x08
	pkt[13] = 0x00

	// IP header
	ip := pkt[EthHLen:]
	ip[0] = 0x45        // version 4, header length 5 (20 bytes)
	ip[1] = 0x00        // TOS
	ip[2] = 0x00        // Total length (high byte)
	ip[3] = byte(ipLen) // Total length (low byte)
	ip[8] = 0x40        // TTL
	ip[9] = 0x01        // Protocol: ICMP
	// Source: 10.0.2.15
	ip[12] = 10
	ip[13] = 0
	ip[14] = 2
	ip[15] = 15
	// Dest: 10.0.2.2 (vhost)
	ip[16] = 10
	ip[17] = 0
	ip[18] = 2
	ip[19] = 2

	// ICMP echo request
	icmp := ip[20:]
	icmp[0] = 8 // Type: Echo Request
	icmp[1] = 0 // Code
	icmp[2] = 0 // Checksum (will be computed by ICMPInput)
	icmp[3] = 0
	icmp[4] = 0 // ID
	icmp[5] = 1
	icmp[6] = 0 // Seq
	icmp[7] = 1

	// Should not panic - packet will be processed
	s.Input(pkt)
}

// TestInputUnknownProtocol tests that Input ignores unknown ethertype.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:657-658
func TestInputUnknownProtocol(t *testing.T) {
	s := NewSlirp()

	// Build packet with unknown ethertype
	pkt := make([]byte, EthHLen+20)
	pkt[12] = 0x99 // Unknown protocol
	pkt[13] = 0x99

	// Should not panic
	s.Input(pkt)
}

// TestIPInputTooShort tests that IPInput handles packets shorter than IP header.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c
func TestIPInputTooShort(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()
	m.Len = 10 // Less than IPHeaderSize (20)

	// Should not panic - mbuf should be freed
	s.IPInput(m)
}

// TestIPInputBadVersion tests that IPInput rejects non-IPv4 packets.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c
func TestIPInputBadVersion(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()

	// Build packet with wrong version
	m.Data[0] = 0x65 // Version 6, not 4
	m.Data[2] = 0
	m.Data[3] = 20
	m.Len = 20

	// Should not panic
	s.IPInput(m)
}

// TestIPInputDispatchICMP tests that IPInput dispatches ICMP.
func TestIPInputDispatchICMP(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()

	// Build minimal IP + ICMP packet
	m.Data[0] = 0x45 // Version 4, IHL 5
	m.Data[2] = 0
	m.Data[3] = 28 // Total length
	m.Data[8] = 64 // TTL
	m.Data[9] = IPProtoICMP
	m.Data[12] = 10 // Src
	m.Data[13] = 0
	m.Data[14] = 2
	m.Data[15] = 15
	m.Data[16] = 10 // Dst
	m.Data[17] = 0
	m.Data[18] = 2
	m.Data[19] = 2
	m.Len = 28

	// Should not panic
	s.IPInput(m)
}

// TestIPInputDispatchUDP tests that IPInput dispatches to UDP.
// The packet is intentionally too short for full UDP processing
// to avoid triggering actual UDP handling - we just verify dispatch works.
func TestIPInputDispatchUDP(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()

	// Build minimal IP packet with UDP protocol but too short for UDP processing
	// UDPInput will free the mbuf when m.Len < iphlen + UDPHeaderSize (20 + 8 = 28)
	m.Data[0] = 0x45 // Version 4, IHL 5
	m.Data[2] = 0
	m.Data[3] = 24 // Total length = 24 (too short for UDP header)
	m.Data[8] = 64 // TTL
	m.Data[9] = IPProtoUDP
	m.Data[12] = 10 // Src
	m.Data[13] = 0
	m.Data[14] = 2
	m.Data[15] = 15
	m.Data[16] = 10 // Dst
	m.Data[17] = 0
	m.Data[18] = 2
	m.Data[19] = 2
	m.Len = 24 // 20 byte IP + 4 bytes (not enough for 8-byte UDP header)

	// Should not panic - UDPInput will see packet is too short and free it
	s.IPInput(m)
}

// TestIPInputDispatchTCP tests that IPInput dispatches TCP (stub).
func TestIPInputDispatchTCP(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()

	// Build minimal IP + TCP packet
	m.Data[0] = 0x45 // Version 4, IHL 5
	m.Data[2] = 0
	m.Data[3] = 40 // Total length
	m.Data[8] = 64 // TTL
	m.Data[9] = IPProtoTCP
	m.Data[12] = 10 // Src
	m.Data[13] = 0
	m.Data[14] = 2
	m.Data[15] = 15
	m.Data[16] = 10 // Dst
	m.Data[17] = 0
	m.Data[18] = 2
	m.Data[19] = 2
	m.Len = 40

	// Should not panic (TCP stub just frees the mbuf)
	s.IPInput(m)
}

// TestIPInputDispatchUnknown tests that IPInput handles unknown protocols.
func TestIPInputDispatchUnknown(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()

	// Build minimal IP packet with unknown protocol
	m.Data[0] = 0x45 // Version 4, IHL 5
	m.Data[2] = 0
	m.Data[3] = 20 // Total length
	m.Data[8] = 64 // TTL
	m.Data[9] = 99 // Unknown protocol
	m.Data[12] = 10
	m.Data[13] = 0
	m.Data[14] = 2
	m.Data[15] = 15
	m.Data[16] = 10
	m.Data[17] = 0
	m.Data[18] = 2
	m.Data[19] = 2
	m.Len = 20

	// Should not panic - mbuf should be freed
	s.IPInput(m)
}

// TestTCPInputStub tests the TCP input stub.
func TestTCPInputStub(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()
	m.Len = 40

	// Should not panic - just frees the mbuf
	s.TCPInput(m, 20)
}
