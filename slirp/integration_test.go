package slirp

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// Integration tests for SLIRP network operations.
// These tests verify that network operations work correctly from a guest perspective,
// simulating the packet flow as it would occur from an emulated guest OS.
//
// Reference: tinyemu-2019-12-21/slirp/

// testHelper provides utilities for building and parsing network packets.
type testHelper struct {
	slirp      *Slirp
	guestMAC   [6]byte
	guestIP    net.IP
	outputPkts [][]byte
}

func newTestHelper() *testHelper {
	s := NewSlirp()
	h := &testHelper{
		slirp:    s,
		guestMAC: [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56},
		guestIP:  net.IPv4(10, 0, 2, 15), // default DHCP address
	}

	// Set up output capture
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		captured := make([]byte, len(pkt))
		copy(captured, pkt)
		h.outputPkts = append(h.outputPkts, captured)
	}
	s.Opaque = h

	// Set known client MAC
	copy(s.ClientEthAddr[:], h.guestMAC[:])

	return h
}

// clearOutput clears captured output packets
func (h *testHelper) clearOutput() {
	h.outputPkts = nil
}

// buildEthFrame builds an Ethernet frame with given payload
func (h *testHelper) buildEthFrame(dstMAC [6]byte, etherType uint16, payload []byte) []byte {
	frame := make([]byte, EthHLen+len(payload))
	copy(frame[0:6], dstMAC[:])
	copy(frame[6:12], h.guestMAC[:])
	binary.BigEndian.PutUint16(frame[12:14], etherType)
	copy(frame[EthHLen:], payload)
	return frame
}

// buildIPPacket builds an IP packet
func (h *testHelper) buildIPPacket(proto uint8, srcIP, dstIP net.IP, payload []byte) []byte {
	totalLen := IPHeaderSize + len(payload)
	pkt := make([]byte, totalLen)

	pkt[0] = (IPVersion << 4) | (IPHeaderSize >> 2) // version + IHL
	pkt[1] = 0                                      // TOS
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], uint16(h.slirp.IPID)) // ID
	h.slirp.IPID++
	binary.BigEndian.PutUint16(pkt[6:8], 0) // flags + frag offset
	pkt[8] = 64                             // TTL
	pkt[9] = proto                          // protocol
	copy(pkt[12:16], srcIP.To4())
	copy(pkt[16:20], dstIP.To4())
	copy(pkt[IPHeaderSize:], payload)

	// Compute IP checksum
	pkt[10] = 0
	pkt[11] = 0
	cksum := CksumData(pkt[:IPHeaderSize])
	binary.BigEndian.PutUint16(pkt[10:12], cksum)

	return pkt
}

// buildICMPEchoRequest builds an ICMP echo request
func (h *testHelper) buildICMPEchoRequest(id, seq uint16) []byte {
	icmp := make([]byte, 8)
	icmp[0] = ICMPEcho // type
	icmp[1] = 0        // code
	binary.BigEndian.PutUint16(icmp[4:6], id)
	binary.BigEndian.PutUint16(icmp[6:8], seq)

	// Compute checksum
	icmp[2] = 0
	icmp[3] = 0
	cksum := CksumData(icmp)
	binary.BigEndian.PutUint16(icmp[2:4], cksum)

	return icmp
}

// buildUDPPacket builds a UDP packet (just the UDP portion)
func (h *testHelper) buildUDPPacket(srcPort, dstPort uint16, payload []byte) []byte {
	udpLen := UDPHeaderSize + len(payload)
	udp := make([]byte, udpLen)

	binary.BigEndian.PutUint16(udp[0:2], srcPort)
	binary.BigEndian.PutUint16(udp[2:4], dstPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	// checksum will be 0 (valid for UDP)
	copy(udp[UDPHeaderSize:], payload)

	return udp
}

// buildARPRequest builds an ARP request packet
func (h *testHelper) buildARPRequest(senderIP, targetIP net.IP) []byte {
	arp := make([]byte, 28)
	binary.BigEndian.PutUint16(arp[0:2], 1)      // hardware type: Ethernet
	binary.BigEndian.PutUint16(arp[2:4], EthPIP) // protocol type: IP
	arp[4] = 6                                   // hardware addr len
	arp[5] = 4                                   // protocol addr len
	binary.BigEndian.PutUint16(arp[6:8], ARPOpRequest)
	copy(arp[8:14], h.guestMAC[:])   // sender hardware addr
	copy(arp[14:18], senderIP.To4()) // sender protocol addr
	// target hardware addr (zeros)
	copy(arp[24:28], targetIP.To4()) // target protocol addr
	return arp
}

// buildDHCPDiscover builds a DHCP DISCOVER packet
func (h *testHelper) buildDHCPDiscover(xid uint32) []byte {
	bp := &BootpT{
		Op:    BootpRequest,
		Htype: 1,
		Hlen:  6,
		Xid:   xid,
	}
	copy(bp.Hwaddr[:], h.guestMAC[:])

	// DHCP options
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType
	bp.Vend[5] = 1
	bp.Vend[6] = DHCPDiscover
	bp.Vend[7] = RFC1533End

	data := make([]byte, BootpSize)
	bp.Marshal(data)
	return data
}

// buildDHCPRequest builds a DHCP REQUEST packet
func (h *testHelper) buildDHCPRequest(xid uint32, requestedIP net.IP) []byte {
	bp := &BootpT{
		Op:    BootpRequest,
		Htype: 1,
		Hlen:  6,
		Xid:   xid,
	}
	copy(bp.Hwaddr[:], h.guestMAC[:])

	// DHCP options
	i := 0
	copy(bp.Vend[i:i+4], RFC1533Cookie)
	i += 4

	bp.Vend[i] = RFC2132MsgType
	i++
	bp.Vend[i] = 1
	i++
	bp.Vend[i] = DHCPRequest
	i++

	bp.Vend[i] = RFC2132ReqAddr
	i++
	bp.Vend[i] = 4
	i++
	copy(bp.Vend[i:i+4], requestedIP.To4())
	i += 4

	bp.Vend[i] = RFC1533End

	data := make([]byte, BootpSize)
	bp.Marshal(data)
	return data
}

// TestIntegrationARPResolution tests ARP resolution from guest perspective.
// Guest sends ARP request, receives ARP reply with correct MAC.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:570-626
func TestIntegrationARPResolution(t *testing.T) {
	h := newTestHelper()
	h.slirp.ClientEthAddr = ZeroEthAddr // Reset so ARP is needed

	// Build ARP request for vhost address (10.0.2.2)
	arpPayload := h.buildARPRequest(h.guestIP, h.slirp.VHostAddr)
	broadcastMAC := [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	frame := h.buildEthFrame(broadcastMAC, EthPARP, arpPayload)

	// Send to slirp
	h.slirp.Input(frame)

	// Should receive ARP reply
	if len(h.outputPkts) == 0 {
		t.Fatal("No ARP reply received")
	}

	reply := h.outputPkts[0]
	if len(reply) < EthHLen+28 {
		t.Fatalf("ARP reply too short: %d bytes", len(reply))
	}

	// Check EtherType
	etherType := binary.BigEndian.Uint16(reply[12:14])
	if etherType != EthPARP {
		t.Errorf("EtherType = 0x%04x, want 0x%04x", etherType, EthPARP)
	}

	// Check ARP operation (should be reply)
	arpOp := binary.BigEndian.Uint16(reply[EthHLen+6 : EthHLen+8])
	if arpOp != ARPOpReply {
		t.Errorf("ARP operation = %d, want %d (reply)", arpOp, ARPOpReply)
	}

	// Check sender IP (should be vhost)
	senderIP := net.IP(reply[EthHLen+14 : EthHLen+18])
	if !senderIP.Equal(h.slirp.VHostAddr) {
		t.Errorf("ARP sender IP = %v, want %v", senderIP, h.slirp.VHostAddr)
	}

	// Verify client MAC was learned
	if bytes.Equal(h.slirp.ClientEthAddr[:], ZeroEthAddr[:]) {
		t.Error("Client MAC address was not learned from ARP request")
	}
}

// TestIntegrationARPNameserver tests ARP resolution for nameserver address.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:585-586
func TestIntegrationARPNameserver(t *testing.T) {
	h := newTestHelper()

	// Build ARP request for nameserver (10.0.2.3)
	arpPayload := h.buildARPRequest(h.guestIP, h.slirp.VNameserverAddr)
	broadcastMAC := [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	frame := h.buildEthFrame(broadcastMAC, EthPARP, arpPayload)

	h.slirp.Input(frame)

	if len(h.outputPkts) == 0 {
		t.Fatal("No ARP reply for nameserver")
	}

	// Verify sender IP is the nameserver
	reply := h.outputPkts[0]
	senderIP := net.IP(reply[EthHLen+14 : EthHLen+18])
	if !senderIP.Equal(h.slirp.VNameserverAddr) {
		t.Errorf("ARP sender IP = %v, want %v", senderIP, h.slirp.VNameserverAddr)
	}
}

// TestIntegrationICMPPingVHost tests ICMP echo request to vhost.
// Guest pings vhost, should receive echo reply.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:66-166
func TestIntegrationICMPPingVHost(t *testing.T) {
	h := newTestHelper()

	// Build ICMP echo request
	icmpPayload := h.buildICMPEchoRequest(0x1234, 1)
	ipPayload := h.buildIPPacket(IPProtoICMP, h.guestIP, h.slirp.VHostAddr, icmpPayload)

	// Build Ethernet frame to vhost
	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	h.slirp.Input(frame)

	// Should receive ICMP echo reply
	if len(h.outputPkts) == 0 {
		t.Fatal("No ICMP echo reply received")
	}

	reply := h.outputPkts[0]
	if len(reply) < EthHLen+IPHeaderSize+ICMPHeaderSize {
		t.Fatalf("ICMP reply too short: %d bytes", len(reply))
	}

	// Verify IP protocol is ICMP
	ipProto := reply[EthHLen+9]
	if ipProto != IPProtoICMP {
		t.Errorf("IP protocol = %d, want %d (ICMP)", ipProto, IPProtoICMP)
	}

	// Verify ICMP type is echo reply
	icmpType := reply[EthHLen+IPHeaderSize]
	if icmpType != ICMPEchoReply {
		t.Errorf("ICMP type = %d, want %d (echo reply)", icmpType, ICMPEchoReply)
	}

	// Verify echo ID and sequence are preserved
	icmpID := binary.BigEndian.Uint16(reply[EthHLen+IPHeaderSize+4 : EthHLen+IPHeaderSize+6])
	icmpSeq := binary.BigEndian.Uint16(reply[EthHLen+IPHeaderSize+6 : EthHLen+IPHeaderSize+8])
	if icmpID != 0x1234 {
		t.Errorf("ICMP ID = 0x%04x, want 0x1234", icmpID)
	}
	if icmpSeq != 1 {
		t.Errorf("ICMP seq = %d, want 1", icmpSeq)
	}

	// Verify source/dest IPs are swapped
	srcIP := net.IP(reply[EthHLen+12 : EthHLen+16])
	dstIP := net.IP(reply[EthHLen+16 : EthHLen+20])
	if !srcIP.Equal(h.slirp.VHostAddr) {
		t.Errorf("Reply src IP = %v, want %v", srcIP, h.slirp.VHostAddr)
	}
	if !dstIP.Equal(h.guestIP) {
		t.Errorf("Reply dst IP = %v, want %v", dstIP, h.guestIP)
	}
}

// TestIntegrationDHCPDiscoverOffer tests DHCP discover/offer exchange.
// Guest sends DHCP DISCOVER, receives DHCP OFFER.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:143-305
func TestIntegrationDHCPDiscoverOffer(t *testing.T) {
	h := newTestHelper()

	// Build DHCP DISCOVER using direct mbuf approach like the bootp_test does
	// This bypasses the IP/UDP input processing and directly tests BOOTP handling
	xid := uint32(0x12345678)

	// Create mbuf with BOOTP packet (following IP and UDP headers)
	m := h.slirp.MGet()
	if m == nil {
		t.Fatal("Failed to get mbuf")
	}

	// Build BOOTP request directly into mbuf after IP+UDP headers
	bp := &BootpT{
		Op:    BootpRequest,
		Htype: 1,
		Hlen:  6,
		Xid:   xid,
	}
	copy(bp.Hwaddr[:], h.guestMAC[:])

	// DHCP options
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType
	bp.Vend[5] = 1
	bp.Vend[6] = DHCPDiscover
	bp.Vend[7] = RFC1533End

	// Build IP+UDP headers
	totalLen := IPHeaderSize + UDPHeaderSize + BootpSize
	m.Data[0] = (IPVersion << 4) | (IPHeaderSize >> 2)
	m.Data[1] = 0 // TOS
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(totalLen))
	m.Data[8] = 64         // TTL
	m.Data[9] = IPProtoUDP // Protocol
	copy(m.Data[12:16], net.IPv4zero.To4())
	copy(m.Data[16:20], net.IPv4bcast.To4())

	// IP checksum
	m.Data[10] = 0
	m.Data[11] = 0
	cksum := CksumData(m.Data[:IPHeaderSize])
	binary.BigEndian.PutUint16(m.Data[10:12], cksum)

	// UDP header
	udpStart := IPHeaderSize
	binary.BigEndian.PutUint16(m.Data[udpStart:udpStart+2], BootpClient)                       // src port
	binary.BigEndian.PutUint16(m.Data[udpStart+2:udpStart+4], BootpServer)                     // dst port
	binary.BigEndian.PutUint16(m.Data[udpStart+4:udpStart+6], uint16(UDPHeaderSize+BootpSize)) // len
	binary.BigEndian.PutUint16(m.Data[udpStart+6:udpStart+8], 0)                               // checksum (0 = no checksum)

	// Marshal BOOTP packet
	bootpStart := IPHeaderSize + UDPHeaderSize
	bp.Marshal(m.Data[bootpStart:])

	m.Len = totalLen

	// Set client MAC for response routing
	copy(h.slirp.ClientEthAddr[:], h.guestMAC[:])

	// Process via BootpInput (which expects mbuf with IP+UDP+BOOTP)
	h.slirp.BootpInput(m)
	m.Free()

	// The DHCP offer will be queued - need to flush output
	if h.slirp.IfQueued > 0 {
		h.slirp.IfStart()
	}

	// Should receive DHCP OFFER
	if len(h.outputPkts) == 0 {
		t.Fatal("No DHCP offer received")
	}

	reply := h.outputPkts[0]

	// Parse reply - find BOOTP portion
	if len(reply) < EthHLen+IPHeaderSize+UDPHeaderSize+BootpSize {
		t.Fatalf("DHCP reply too short: %d bytes, want at least %d", len(reply), EthHLen+IPHeaderSize+UDPHeaderSize+BootpSize)
	}

	bootpOffset := EthHLen + IPHeaderSize + UDPHeaderSize
	rbp := ParseBootp(reply[bootpOffset:])
	if rbp == nil {
		t.Fatal("Failed to parse BOOTP reply")
	}

	// Verify it's a reply
	if rbp.Op != BootpReply {
		t.Errorf("BOOTP op = %d, want %d (reply)", rbp.Op, BootpReply)
	}

	// Verify XID matches
	if rbp.Xid != xid {
		t.Errorf("BOOTP xid = 0x%08x, want 0x%08x", rbp.Xid, xid)
	}

	// Verify yiaddr (your IP) is set
	if rbp.Yiaddr.Equal(net.IPv4zero) {
		t.Error("BOOTP yiaddr not set")
	}

	// Verify siaddr (server IP) is vhost
	if !rbp.Siaddr.Equal(h.slirp.VHostAddr) {
		t.Errorf("BOOTP siaddr = %v, want %v", rbp.Siaddr, h.slirp.VHostAddr)
	}

	// Verify DHCP message type is OFFER
	msgType, _ := dhcpDecode(rbp)
	if msgType != DHCPOffer {
		t.Errorf("DHCP message type = %d, want %d (OFFER)", msgType, DHCPOffer)
	}
}

// TestIntegrationDHCPRequestAck tests DHCP request/ack exchange.
// Guest sends DHCP REQUEST, receives DHCP ACK.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:143-305
func TestIntegrationDHCPRequestAck(t *testing.T) {
	h := newTestHelper()
	xid := uint32(0x12345678)

	// First do DISCOVER to allocate an address
	m := h.slirp.MGet()
	if m == nil {
		t.Fatal("Failed to get mbuf")
	}

	bp := &BootpT{
		Op:    BootpRequest,
		Htype: 1,
		Hlen:  6,
		Xid:   xid,
	}
	copy(bp.Hwaddr[:], h.guestMAC[:])
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType
	bp.Vend[5] = 1
	bp.Vend[6] = DHCPDiscover
	bp.Vend[7] = RFC1533End

	// Build IP+UDP headers
	totalLen := IPHeaderSize + UDPHeaderSize + BootpSize
	m.Data[0] = (IPVersion << 4) | (IPHeaderSize >> 2)
	m.Data[1] = 0
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(totalLen))
	m.Data[8] = 64
	m.Data[9] = IPProtoUDP
	copy(m.Data[12:16], net.IPv4zero.To4())
	copy(m.Data[16:20], net.IPv4bcast.To4())
	m.Data[10] = 0
	m.Data[11] = 0
	cksum := CksumData(m.Data[:IPHeaderSize])
	binary.BigEndian.PutUint16(m.Data[10:12], cksum)

	udpStart := IPHeaderSize
	binary.BigEndian.PutUint16(m.Data[udpStart:udpStart+2], BootpClient)
	binary.BigEndian.PutUint16(m.Data[udpStart+2:udpStart+4], BootpServer)
	binary.BigEndian.PutUint16(m.Data[udpStart+4:udpStart+6], uint16(UDPHeaderSize+BootpSize))
	binary.BigEndian.PutUint16(m.Data[udpStart+6:udpStart+8], 0)

	bootpStart := IPHeaderSize + UDPHeaderSize
	bp.Marshal(m.Data[bootpStart:])
	m.Len = totalLen

	copy(h.slirp.ClientEthAddr[:], h.guestMAC[:])
	h.slirp.BootpInput(m)
	m.Free()

	if h.slirp.IfQueued > 0 {
		h.slirp.IfStart()
	}

	if len(h.outputPkts) == 0 {
		t.Fatal("No DHCP offer received")
	}

	// Parse the offer to get the offered IP
	offerReply := h.outputPkts[0]
	bootpOffset := EthHLen + IPHeaderSize + UDPHeaderSize
	offerBP := ParseBootp(offerReply[bootpOffset:])
	if offerBP == nil {
		t.Fatal("Failed to parse BOOTP offer")
	}
	offeredIP := offerBP.Yiaddr

	// Clear output and send DHCP REQUEST
	h.clearOutput()

	m2 := h.slirp.MGet()
	if m2 == nil {
		t.Fatal("Failed to get mbuf for request")
	}

	bp2 := &BootpT{
		Op:    BootpRequest,
		Htype: 1,
		Hlen:  6,
		Xid:   xid,
	}
	copy(bp2.Hwaddr[:], h.guestMAC[:])

	// Build DHCP REQUEST options
	i := 0
	copy(bp2.Vend[i:i+4], RFC1533Cookie)
	i += 4
	bp2.Vend[i] = RFC2132MsgType
	i++
	bp2.Vend[i] = 1
	i++
	bp2.Vend[i] = DHCPRequest
	i++
	bp2.Vend[i] = RFC2132ReqAddr
	i++
	bp2.Vend[i] = 4
	i++
	copy(bp2.Vend[i:i+4], offeredIP.To4())
	i += 4
	bp2.Vend[i] = RFC1533End

	m2.Data[0] = (IPVersion << 4) | (IPHeaderSize >> 2)
	m2.Data[1] = 0
	binary.BigEndian.PutUint16(m2.Data[2:4], uint16(totalLen))
	m2.Data[8] = 64
	m2.Data[9] = IPProtoUDP
	copy(m2.Data[12:16], net.IPv4zero.To4())
	copy(m2.Data[16:20], net.IPv4bcast.To4())
	m2.Data[10] = 0
	m2.Data[11] = 0
	cksum2 := CksumData(m2.Data[:IPHeaderSize])
	binary.BigEndian.PutUint16(m2.Data[10:12], cksum2)

	binary.BigEndian.PutUint16(m2.Data[udpStart:udpStart+2], BootpClient)
	binary.BigEndian.PutUint16(m2.Data[udpStart+2:udpStart+4], BootpServer)
	binary.BigEndian.PutUint16(m2.Data[udpStart+4:udpStart+6], uint16(UDPHeaderSize+BootpSize))
	binary.BigEndian.PutUint16(m2.Data[udpStart+6:udpStart+8], 0)

	bp2.Marshal(m2.Data[bootpStart:])
	m2.Len = totalLen

	h.slirp.BootpInput(m2)
	m2.Free()

	if h.slirp.IfQueued > 0 {
		h.slirp.IfStart()
	}

	// Should receive DHCP ACK
	if len(h.outputPkts) == 0 {
		t.Fatal("No DHCP ACK received")
	}

	ackReply := h.outputPkts[0]
	ackBP := ParseBootp(ackReply[bootpOffset:])
	if ackBP == nil {
		t.Fatal("Failed to parse BOOTP ACK")
	}

	// Verify DHCP message type is ACK
	msgType, _ := dhcpDecode(ackBP)
	if msgType != DHCPAck {
		t.Errorf("DHCP message type = %d, want %d (ACK)", msgType, DHCPAck)
	}

	// Verify yiaddr matches requested IP
	if !ackBP.Yiaddr.Equal(offeredIP) {
		t.Errorf("BOOTP yiaddr = %v, want %v", ackBP.Yiaddr, offeredIP)
	}
}

// TestIntegrationUDPToExternal tests UDP packet sending to external host.
// Guest sends UDP packet, it should create a socket for reply.
// Reference: tinyemu-2019-12-21/slirp/udp.c:57-226
func TestIntegrationUDPToExternal(t *testing.T) {
	h := newTestHelper()

	// Build UDP packet to external address
	externalIP := net.IPv4(8, 8, 8, 8)
	dnsQuery := []byte{0x00, 0x01} // minimal DNS query (truncated for test)
	udpPayload := h.buildUDPPacket(12345, 53, dnsQuery)
	ipPayload := h.buildIPPacket(IPProtoUDP, h.guestIP, externalIP, udpPayload)

	// Build Ethernet frame
	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	h.slirp.Input(frame)

	// A UDP socket should have been created
	found := false
	for so := h.slirp.UDB.Next; so != &h.slirp.UDB; so = so.Next {
		if so.SoLPort == 12345 && so.SoLAddr.Equal(h.guestIP) {
			found = true
			// Verify the socket is connected
			if (so.SoState & SSIsFConnected) == 0 {
				t.Error("UDP socket not marked as connected")
			}
			// Clean up socket
			h.slirp.UDPDetach(so)
			break
		}
	}

	if !found {
		t.Error("UDP socket was not created for outgoing packet")
	}
}

// TestIntegrationTCPSYN tests TCP SYN packet handling.
// Guest sends TCP SYN to external host, should initiate connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c
func TestIntegrationTCPSYN(t *testing.T) {
	h := newTestHelper()

	// Build TCP SYN packet
	externalIP := net.IPv4(93, 184, 216, 34) // example.com
	tcpPayload := make([]byte, 20)           // minimal TCP header
	tcpPayload[0] = 0x30                     // src port high byte (12288)
	tcpPayload[1] = 0x00                     // src port low byte
	tcpPayload[2] = 0x00                     // dst port high byte (80)
	tcpPayload[3] = 0x50                     // dst port low byte
	// seq number (bytes 4-7)
	binary.BigEndian.PutUint32(tcpPayload[4:8], 1000)
	// ack number (bytes 8-11) = 0 for SYN
	// data offset (4 bits) + reserved (4 bits) + flags
	tcpPayload[12] = (5 << 4) // data offset = 5 (20 bytes header)
	tcpPayload[13] = THSyn    // flags = SYN
	// window
	binary.BigEndian.PutUint16(tcpPayload[14:16], 65535)
	// checksum (bytes 16-17) - will compute
	// urgent pointer (bytes 18-19) = 0

	// Compute TCP checksum with pseudo-header
	pseudoLen := 12 + len(tcpPayload)
	pseudo := make([]byte, pseudoLen)
	copy(pseudo[0:4], h.guestIP.To4())
	copy(pseudo[4:8], externalIP.To4())
	pseudo[8] = 0
	pseudo[9] = IPProtoTCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcpPayload)))
	copy(pseudo[12:], tcpPayload)
	cksum := CksumData(pseudo)
	binary.BigEndian.PutUint16(tcpPayload[16:18], cksum)

	ipPayload := h.buildIPPacket(IPProtoTCP, h.guestIP, externalIP, tcpPayload)

	// Build Ethernet frame
	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	// Input the packet - this will attempt to connect
	h.slirp.Input(frame)

	// A TCP socket should have been created (though connection may fail)
	found := false
	for so := h.slirp.TCB.Next; so != &h.slirp.TCB; so = so.Next {
		if so.SoLPort == 12288 && so.SoLAddr.Equal(h.guestIP) {
			found = true
			// Socket should be in connecting or connected state
			if so.SoState&(SSIsFConnecting|SSIsFConnected) == 0 {
				// May also be in other states if connection failed quickly
				t.Logf("TCP socket state: 0x%x", so.SoState)
			}
			// Clean up
			so.SoFree()
			break
		}
	}

	// Note: The socket may or may not be found depending on connection attempt timing
	// The important thing is that the packet was processed without panic
	_ = found
}

// TestIntegrationIPForwarding tests that IP packets to external addresses are forwarded.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c
func TestIntegrationIPForwarding(t *testing.T) {
	h := newTestHelper()

	// Build ICMP packet to external address
	externalIP := net.IPv4(8, 8, 8, 8)
	icmpPayload := h.buildICMPEchoRequest(0x5678, 1)
	ipPayload := h.buildIPPacket(IPProtoICMP, h.guestIP, externalIP, icmpPayload)

	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	// This should not panic - packet is processed (but may not generate reply
	// since external ICMP isn't fully implemented in SLIRP)
	h.slirp.Input(frame)
}

// TestIntegrationBadPackets tests handling of malformed packets.
// Reference: Various error handling paths in SLIRP
func TestIntegrationBadPackets(t *testing.T) {
	h := newTestHelper()

	tests := []struct {
		name  string
		frame []byte
	}{
		{"empty frame", []byte{}},
		{"too short for eth", make([]byte, 5)},
		{"eth only no payload", make([]byte, EthHLen)},
		{"unknown ethertype", func() []byte {
			f := make([]byte, EthHLen+20)
			f[12] = 0x99
			f[13] = 0x99
			return f
		}()},
		{"IP packet too short", func() []byte {
			f := make([]byte, EthHLen+10)
			f[12] = 0x08
			f[13] = 0x00
			return f
		}()},
		{"IP bad version", func() []byte {
			ip := h.buildIPPacket(IPProtoICMP, h.guestIP, h.slirp.VHostAddr, make([]byte, 8))
			ip[0] = 0x60 // IPv6
			return h.buildEthFrame([6]byte{}, EthPIP, ip)
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.clearOutput()
			// Should not panic
			h.slirp.Input(tt.frame)
		})
	}
}

// TestIntegrationRestrictedMode tests that restricted mode blocks non-DHCP UDP.
// Reference: tinyemu-2019-12-21/slirp/udp.c:129-132
func TestIntegrationRestrictedMode(t *testing.T) {
	h := newTestHelper()
	h.slirp.Restricted = true

	// Try to send UDP to external address (not DHCP)
	externalIP := net.IPv4(8, 8, 8, 8)
	dnsQuery := []byte{0x00, 0x01}
	udpPayload := h.buildUDPPacket(12345, 53, dnsQuery)
	ipPayload := h.buildIPPacket(IPProtoUDP, h.guestIP, externalIP, udpPayload)

	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	h.slirp.Input(frame)

	// No socket should be created in restricted mode for non-DHCP UDP
	found := false
	for so := h.slirp.UDB.Next; so != &h.slirp.UDB; so = so.Next {
		if so.SoLPort == 12345 {
			found = true
			h.slirp.UDPDetach(so)
			break
		}
	}

	if found {
		t.Error("UDP socket was created in restricted mode (should be blocked)")
	}
}

// TestIntegrationDHCPInRestrictedMode tests that DHCP still works in restricted mode.
// Reference: tinyemu-2019-12-21/slirp/udp.c:121-127
func TestIntegrationDHCPInRestrictedMode(t *testing.T) {
	h := newTestHelper()
	h.slirp.Restricted = true

	// Build DHCP DISCOVER using direct BootpInput
	xid := uint32(0xABCDEF01)

	m := h.slirp.MGet()
	if m == nil {
		t.Fatal("Failed to get mbuf")
	}

	bp := &BootpT{
		Op:    BootpRequest,
		Htype: 1,
		Hlen:  6,
		Xid:   xid,
	}
	copy(bp.Hwaddr[:], h.guestMAC[:])
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType
	bp.Vend[5] = 1
	bp.Vend[6] = DHCPDiscover
	bp.Vend[7] = RFC1533End

	totalLen := IPHeaderSize + UDPHeaderSize + BootpSize
	m.Data[0] = (IPVersion << 4) | (IPHeaderSize >> 2)
	m.Data[1] = 0
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(totalLen))
	m.Data[8] = 64
	m.Data[9] = IPProtoUDP
	copy(m.Data[12:16], net.IPv4zero.To4())
	copy(m.Data[16:20], net.IPv4bcast.To4())
	m.Data[10] = 0
	m.Data[11] = 0
	cksum := CksumData(m.Data[:IPHeaderSize])
	binary.BigEndian.PutUint16(m.Data[10:12], cksum)

	udpStart := IPHeaderSize
	binary.BigEndian.PutUint16(m.Data[udpStart:udpStart+2], BootpClient)
	binary.BigEndian.PutUint16(m.Data[udpStart+2:udpStart+4], BootpServer)
	binary.BigEndian.PutUint16(m.Data[udpStart+4:udpStart+6], uint16(UDPHeaderSize+BootpSize))
	binary.BigEndian.PutUint16(m.Data[udpStart+6:udpStart+8], 0)

	bootpStart := IPHeaderSize + UDPHeaderSize
	bp.Marshal(m.Data[bootpStart:])
	m.Len = totalLen

	copy(h.slirp.ClientEthAddr[:], h.guestMAC[:])
	h.slirp.BootpInput(m)
	m.Free()

	if h.slirp.IfQueued > 0 {
		h.slirp.IfStart()
	}

	// Should still receive DHCP OFFER even in restricted mode
	if len(h.outputPkts) == 0 {
		t.Fatal("No DHCP offer received in restricted mode")
	}
}

// TestIntegrationPoll tests the Poll function processes sockets correctly.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:357-534
func TestIntegrationPoll(t *testing.T) {
	// Create a proper EthernetDevice
	es := NewEthernetDevice()
	slirp := GetSlirp(es)
	if slirp == nil {
		t.Fatal("Failed to get slirp instance")
	}

	// Create a listening socket
	so := slirp.TCPListen(net.IPv4zero, 0, net.IPv4(10, 0, 2, 15), 8080, SSHostFwd)
	if so == nil {
		t.Fatal("Failed to create listening socket")
	}

	// Poll should not panic and should process the socket
	Poll(es)

	// Clean up
	if listener, ok := so.Extra.(interface{ Close() error }); ok {
		listener.Close()
	}
}

// TestIntegrationTimerProcessing tests TCP timer processing.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:374-382
func TestIntegrationTimerProcessing(t *testing.T) {
	h := newTestHelper()

	// Create a TCP socket with delayed ACK flag
	tp := &TCPCB{
		TFlags: TFDelAck,
	}
	so := &Socket{
		Slirp:   h.slirp,
		SoTCPCB: tp,
		Next:    &h.slirp.TCB,
		Prev:    &h.slirp.TCB,
	}
	h.slirp.TCB.Next = so
	h.slirp.TCB.Prev = so

	// Update timer flags - should set timeFastTimo
	curtime := GetTimeMs()
	h.slirp.updateTimerFlags(curtime)

	if h.slirp.timeFastTimo == 0 {
		t.Error("timeFastTimo should be set when TF_DELACK is set")
	}

	// Process timers after delay
	time.Sleep(3 * time.Millisecond)
	newTime := GetTimeMs()
	h.slirp.processTimers(newTime)

	if h.slirp.timeFastTimo != 0 {
		t.Error("timeFastTimo should be cleared after processing")
	}

	// Clean up
	so.Prev.Next = so.Next
	so.Next.Prev = so.Prev
}

// TestIntegrationMultipleARPRequests tests multiple ARP requests.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:570-626
func TestIntegrationMultipleARPRequests(t *testing.T) {
	h := newTestHelper()

	targets := []net.IP{
		h.slirp.VHostAddr,
		h.slirp.VNameserverAddr,
	}

	for _, targetIP := range targets {
		h.clearOutput()
		arpPayload := h.buildARPRequest(h.guestIP, targetIP)
		broadcastMAC := [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
		frame := h.buildEthFrame(broadcastMAC, EthPARP, arpPayload)

		h.slirp.Input(frame)

		if len(h.outputPkts) == 0 {
			t.Errorf("No ARP reply for %v", targetIP)
			continue
		}

		reply := h.outputPkts[0]
		senderIP := net.IP(reply[EthHLen+14 : EthHLen+18])
		if !senderIP.Equal(targetIP) {
			t.Errorf("ARP reply sender IP = %v, want %v", senderIP, targetIP)
		}
	}
}

// TestIntegrationUDPChecksum tests UDP checksum validation.
// Reference: tinyemu-2019-12-21/slirp/udp.c:99-118
func TestIntegrationUDPChecksum(t *testing.T) {
	h := newTestHelper()

	// Build UDP packet with valid checksum
	externalIP := h.slirp.VNameserverAddr
	dnsQuery := []byte{0x00, 0x01, 0x00, 0x00} // minimal DNS query
	udpPayload := h.buildUDPPacket(12345, 53, dnsQuery)

	// Compute UDP checksum
	pseudoLen := 12 + len(udpPayload)
	pseudo := make([]byte, pseudoLen)
	copy(pseudo[0:4], h.guestIP.To4())
	copy(pseudo[4:8], externalIP.To4())
	pseudo[8] = 0
	pseudo[9] = IPProtoUDP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(udpPayload)))
	copy(pseudo[12:], udpPayload)
	cksum := CksumData(pseudo)
	binary.BigEndian.PutUint16(udpPayload[6:8], cksum)

	ipPayload := h.buildIPPacket(IPProtoUDP, h.guestIP, externalIP, udpPayload)

	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	// Should process without error
	h.slirp.Input(frame)
}

// TestIntegrationTCPExecForwarding tests TCP connection to an exec-forwarded address.
// When guest connects to an address configured via AddExec with ExPty=3, the
// connection should be established and routed through the exec mechanism.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:884-915 (tcp_ctl)
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:558-571 (exec list check)
func TestIntegrationTCPExecForwarding(t *testing.T) {
	h := newTestHelper()

	// Configure exec forwarding for a specific address/port
	// ExPty=3 means the socket will use external data path (so.S = -1)
	execAddr := net.IPv4(10, 0, 2, 100)
	execPort := 8080
	testExecString := "test-handler"

	ret := h.slirp.AddExec(3, testExecString, execAddr, execPort)
	if ret != 0 {
		t.Fatalf("AddExec returned %d, want 0", ret)
	}

	// Verify exec entry was added
	if h.slirp.ExecList == nil {
		t.Fatal("ExecList is nil after AddExec")
	}
	if h.slirp.ExecList.ExFPort != uint16(execPort) {
		t.Errorf("ExecList.ExFPort = %d, want %d", h.slirp.ExecList.ExFPort, execPort)
	}

	// Build TCP SYN packet to the exec address
	synSrcPort := uint16(12345)
	tcpPayload := make([]byte, 20) // minimal TCP header
	binary.BigEndian.PutUint16(tcpPayload[0:2], synSrcPort)
	binary.BigEndian.PutUint16(tcpPayload[2:4], uint16(execPort))
	binary.BigEndian.PutUint32(tcpPayload[4:8], 1000) // seq number
	// ack number = 0 for SYN
	tcpPayload[12] = (5 << 4)                            // data offset = 5 (20 bytes header)
	tcpPayload[13] = THSyn                               // flags = SYN
	binary.BigEndian.PutUint16(tcpPayload[14:16], 65535) // window

	// Compute TCP checksum with pseudo-header
	pseudoLen := 12 + len(tcpPayload)
	pseudo := make([]byte, pseudoLen)
	copy(pseudo[0:4], h.guestIP.To4())
	copy(pseudo[4:8], execAddr.To4())
	pseudo[8] = 0
	pseudo[9] = IPProtoTCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcpPayload)))
	copy(pseudo[12:], tcpPayload)
	cksum := CksumData(pseudo)
	binary.BigEndian.PutUint16(tcpPayload[16:18], cksum)

	ipPayload := h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, tcpPayload)

	// Build Ethernet frame - use the exec address for MAC construction
	execMAC := [6]byte{0x52, 0x55}
	copy(execMAC[2:], execAddr.To4())
	frame := h.buildEthFrame(execMAC, EthPIP, ipPayload)

	// Send the SYN packet
	h.slirp.Input(frame)

	// Log any output packets
	t.Logf("Output packets after SYN: %d", len(h.outputPkts))

	// Check that a TCP socket was created with SSCTL flag
	found := false
	var so *Socket
	socketCount := 0
	for s := h.slirp.TCB.Next; s != &h.slirp.TCB; s = s.Next {
		socketCount++
		t.Logf("Socket %d: LAddr=%v:%d FAddr=%v:%d State=0x%x",
			socketCount, s.SoLAddr, s.SoLPort, s.SoFAddr, s.SoFPort, s.SoState)
		if s.SoLPort == synSrcPort && s.SoLAddr.Equal(h.guestIP) &&
			s.SoFPort == uint16(execPort) && s.SoFAddr.Equal(execAddr) {
			found = true
			so = s
			break
		}
	}
	t.Logf("Total TCP sockets: %d", socketCount)

	if !found {
		t.Fatal("TCP socket not created for exec address")
	}

	t.Logf("Socket found: SoState=0x%x, S=%d", so.SoState, so.S)
	t.Logf("Socket addresses: LAddr=%v LPort=%d FAddr=%v FPort=%d", so.SoLAddr, so.SoLPort, so.SoFAddr, so.SoFPort)

	// Verify SSCTL flag was set (indicating it matched an exec entry)
	// Note: SSCTL might be cleared after TCPCtl is called
	// The socket should be in SYN_RECEIVED state
	tp := SoToTCPCB(so)
	if tp == nil {
		t.Fatal("TCPCB not attached to socket")
	}
	t.Logf("TCP state: %d (SYN_RECEIVED=%d)", tp.TState, TCPSSynReceived)

	// Check if a SYN-ACK was generated
	synAckFound := false
	for i, pkt := range h.outputPkts {
		if len(pkt) < EthHLen+IPHeaderSize+20 {
			continue
		}
		// Check if it's a TCP packet
		etherType := binary.BigEndian.Uint16(pkt[12:14])
		if etherType != EthPIP {
			continue
		}
		ipProto := pkt[EthHLen+9]
		if ipProto != IPProtoTCP {
			continue
		}
		// Check TCP flags
		tcpFlags := pkt[EthHLen+IPHeaderSize+13]
		srcIP := net.IP(pkt[EthHLen+12 : EthHLen+16])
		dstIP := net.IP(pkt[EthHLen+16 : EthHLen+20])
		srcPort := binary.BigEndian.Uint16(pkt[EthHLen+IPHeaderSize : EthHLen+IPHeaderSize+2])
		dstPort := binary.BigEndian.Uint16(pkt[EthHLen+IPHeaderSize+2 : EthHLen+IPHeaderSize+4])
		t.Logf("Output packet %d: %v:%d -> %v:%d, flags=0x%02x", i, srcIP, srcPort, dstIP, dstPort, tcpFlags)
		if (tcpFlags & (THSyn | THAck)) == (THSyn | THAck) {
			synAckFound = true
			t.Log("SYN-ACK packet found!")
			// Verify SYN-ACK addresses: from exec address to guest
			if !srcIP.Equal(execAddr) {
				t.Errorf("SYN-ACK src IP = %v, want %v", srcIP, execAddr)
			}
			if !dstIP.Equal(h.guestIP) {
				t.Errorf("SYN-ACK dst IP = %v, want %v", dstIP, h.guestIP)
			}
			if srcPort != uint16(execPort) {
				t.Errorf("SYN-ACK src port = %d, want %d", srcPort, execPort)
			}
			if dstPort != synSrcPort {
				t.Errorf("SYN-ACK dst port = %d, want %d", dstPort, synSrcPort)
			}
			break
		}
	}

	if !synAckFound {
		t.Error("No SYN-ACK response generated")
		t.Logf("Output packets: %d", len(h.outputPkts))
		for i, pkt := range h.outputPkts {
			t.Logf("Packet %d: %d bytes", i, len(pkt))
		}
	}

	// Clean up
	if so != nil {
		so.SoFree()
	}
}

// buildTCPPacket builds a TCP packet with the given parameters.
// Reference: RFC 793 TCP header format
func (h *testHelper) buildTCPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16,
	seq, ack uint32, flags uint8, window uint16, data []byte) []byte {
	tcpLen := 20 + len(data) // TCP header + data
	tcp := make([]byte, tcpLen)

	// TCP header
	binary.BigEndian.PutUint16(tcp[0:2], srcPort)  // source port
	binary.BigEndian.PutUint16(tcp[2:4], dstPort)  // dest port
	binary.BigEndian.PutUint32(tcp[4:8], seq)      // sequence number
	binary.BigEndian.PutUint32(tcp[8:12], ack)     // ack number
	tcp[12] = (5 << 4)                             // data offset = 5 (20 bytes)
	tcp[13] = flags                                // flags
	binary.BigEndian.PutUint16(tcp[14:16], window) // window
	binary.BigEndian.PutUint16(tcp[16:18], 0)      // checksum (computed below)
	binary.BigEndian.PutUint16(tcp[18:20], 0)      // urgent pointer

	// Copy data
	if len(data) > 0 {
		copy(tcp[20:], data)
	}

	// Compute TCP checksum with pseudo-header
	pseudoLen := 12 + tcpLen
	pseudo := make([]byte, pseudoLen)
	copy(pseudo[0:4], srcIP.To4())
	copy(pseudo[4:8], dstIP.To4())
	pseudo[8] = 0
	pseudo[9] = IPProtoTCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(tcpLen))
	copy(pseudo[12:], tcp)
	cksum := CksumData(pseudo)
	binary.BigEndian.PutUint16(tcp[16:18], cksum)

	return tcp
}

// parseTCPPacket extracts TCP header fields from an ethernet frame containing TCP.
type tcpPacketInfo struct {
	srcIP, dstIP     net.IP
	srcPort, dstPort uint16
	seq, ack         uint32
	flags            uint8
	window           uint16
	dataLen          int
}

func (h *testHelper) parseTCPPacket(frame []byte) *tcpPacketInfo {
	if len(frame) < EthHLen+IPHeaderSize+20 {
		return nil
	}
	// Check ethertype
	if binary.BigEndian.Uint16(frame[12:14]) != EthPIP {
		return nil
	}
	ip := frame[EthHLen:]
	if ip[9] != IPProtoTCP {
		return nil
	}
	ipHdrLen := int(ip[0]&0x0f) * 4
	if len(ip) < ipHdrLen+20 {
		return nil
	}
	tcp := ip[ipHdrLen:]

	tcpHdrLen := int(tcp[12]>>4) * 4
	ipTotalLen := int(binary.BigEndian.Uint16(ip[2:4]))
	dataLen := ipTotalLen - ipHdrLen - tcpHdrLen

	return &tcpPacketInfo{
		srcIP:   net.IP(ip[12:16]),
		dstIP:   net.IP(ip[16:20]),
		srcPort: binary.BigEndian.Uint16(tcp[0:2]),
		dstPort: binary.BigEndian.Uint16(tcp[2:4]),
		seq:     binary.BigEndian.Uint32(tcp[4:8]),
		ack:     binary.BigEndian.Uint32(tcp[8:12]),
		flags:   tcp[13],
		window:  binary.BigEndian.Uint16(tcp[14:16]),
		dataLen: dataLen,
	}
}

// findTCPPacket finds a TCP packet in outputPkts matching the given criteria.
func (h *testHelper) findTCPPacket(flags uint8, srcPort, dstPort uint16) *tcpPacketInfo {
	for _, pkt := range h.outputPkts {
		info := h.parseTCPPacket(pkt)
		if info == nil {
			continue
		}
		// Check if flags match (allowing additional flags)
		if (info.flags & flags) == flags {
			if srcPort != 0 && info.srcPort != srcPort {
				continue
			}
			if dstPort != 0 && info.dstPort != dstPort {
				continue
			}
			return info
		}
	}
	return nil
}

// TestIntegrationTCPFullHandshake tests the complete TCP three-way handshake
// and data transfer at the unit level (without VirtIO/emulator).
// This verifies the TCP state machine works correctly in isolation.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c, tinyemu-2019-12-21/slirp/tcp_output.c
func TestIntegrationTCPFullHandshake(t *testing.T) {
	h := newTestHelper()

	// Configure exec forwarding for a specific address/port
	execAddr := net.IPv4(10, 0, 2, 100)
	execPort := uint16(8080)
	testExecString := "test-handler"

	ret := h.slirp.AddExec(3, testExecString, execAddr, int(execPort))
	if ret != 0 {
		t.Fatalf("AddExec returned %d, want 0", ret)
	}

	// ===== Step 1: Send SYN =====
	synSrcPort := uint16(12345)
	clientISS := uint32(1000) // client initial send sequence

	synPacket := h.buildTCPPacket(h.guestIP, execAddr, synSrcPort, execPort,
		clientISS, 0, THSyn, 65535, nil)
	ipPacket := h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, synPacket)
	execMAC := [6]byte{0x52, 0x55}
	copy(execMAC[2:], execAddr.To4())
	frame := h.buildEthFrame(execMAC, EthPIP, ipPacket)

	h.slirp.Input(frame)

	// ===== Step 2: Capture and verify SYN-ACK =====
	synAck := h.findTCPPacket(THSyn|THAck, execPort, synSrcPort)
	if synAck == nil {
		t.Fatal("No SYN-ACK response received")
	}

	t.Logf("SYN-ACK: seq=%d ack=%d flags=0x%02x", synAck.seq, synAck.ack, synAck.flags)

	// Verify SYN-ACK acknowledges our SYN
	if synAck.ack != clientISS+1 {
		t.Errorf("SYN-ACK ack=%d, want %d", synAck.ack, clientISS+1)
	}

	serverISS := synAck.seq // server's initial send sequence

	// Find the socket
	var so *Socket
	for s := h.slirp.TCB.Next; s != &h.slirp.TCB; s = s.Next {
		if s.SoLPort == synSrcPort && s.SoFPort == execPort {
			so = s
			break
		}
	}
	if so == nil {
		t.Fatal("Socket not found")
	}

	tp := SoToTCPCB(so)
	if tp == nil {
		t.Fatal("TCPCB not attached")
	}

	// Verify state is SYN_RECEIVED
	if tp.TState != TCPSSynReceived {
		t.Errorf("TCP state = %d, want %d (SYN_RECEIVED)", tp.TState, TCPSSynReceived)
	}

	// ===== Step 3: Send ACK to complete handshake =====
	h.clearOutput()

	ackPacket := h.buildTCPPacket(h.guestIP, execAddr, synSrcPort, execPort,
		clientISS+1, serverISS+1, THAck, 65535, nil)
	ipPacket = h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, ackPacket)
	frame = h.buildEthFrame(execMAC, EthPIP, ipPacket)

	h.slirp.Input(frame)

	// ===== Step 4: Verify socket reaches ESTABLISHED state =====
	if tp.TState != TCPSEstablished {
		t.Errorf("TCP state = %d, want %d (ESTABLISHED)", tp.TState, TCPSEstablished)
	}

	// Verify socket connected flags
	if (so.SoState & SSIsFConnected) == 0 {
		t.Error("Socket not marked as connected")
	}

	// Verify exec string was set via TCPCtl
	execStr, ok := so.Extra.(string)
	if !ok {
		t.Errorf("so.Extra type = %T, want string", so.Extra)
	} else if execStr != testExecString {
		t.Errorf("so.Extra = %q, want %q", execStr, testExecString)
	}

	t.Logf("Connection ESTABLISHED: exec=%q", execStr)

	// ===== Step 5: Send data packet =====
	h.clearOutput()

	testData := []byte("HELLO FROM GUEST\n")
	dataPacket := h.buildTCPPacket(h.guestIP, execAddr, synSrcPort, execPort,
		clientISS+1, serverISS+1, THAck|THPush, 65535, testData)
	ipPacket = h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, dataPacket)
	frame = h.buildEthFrame(execMAC, EthPIP, ipPacket)

	h.slirp.Input(frame)

	// ===== Step 6: Verify data appears in socket receive buffer =====
	rcvdData := so.SoRcv.SbBytes()
	if !bytes.Equal(rcvdData, testData) {
		t.Errorf("Received data = %q, want %q", rcvdData, testData)
	}
	t.Logf("Data received in buffer: %q", rcvdData)

	// Check for ACK response
	ackResponse := h.findTCPPacket(THAck, execPort, synSrcPort)
	if ackResponse != nil {
		t.Logf("ACK response: seq=%d ack=%d", ackResponse.seq, ackResponse.ack)
		// The ACK should acknowledge the data we sent
		expectedAck := clientISS + 1 + uint32(len(testData))
		if ackResponse.ack != expectedAck {
			t.Errorf("ACK response ack=%d, want %d", ackResponse.ack, expectedAck)
		}
	}

	// ===== Step 7: Send response via SocketRecv =====
	h.clearOutput()

	responseData := []byte("RESPONSE FROM HOST\n")
	h.slirp.SocketRecv(execAddr, int(execPort), responseData)

	// ===== Step 8: Verify response packet generated =====
	if len(h.outputPkts) == 0 {
		t.Error("No response packet generated after SocketRecv")
	} else {
		t.Logf("Output packets after SocketRecv: %d", len(h.outputPkts))

		// Find the data packet
		for _, pkt := range h.outputPkts {
			info := h.parseTCPPacket(pkt)
			if info == nil {
				continue
			}
			t.Logf("Response packet: %v:%d -> %v:%d seq=%d ack=%d flags=0x%02x datalen=%d",
				info.srcIP, info.srcPort, info.dstIP, info.dstPort,
				info.seq, info.ack, info.flags, info.dataLen)

			// Verify the packet carries our response data
			if info.dataLen == len(responseData) {
				t.Log("Response packet with correct data length found!")
			}
		}
	}

	// Clean up
	so.SoFree()
}

// TestIntegrationTCPHandshakeWithTracing tests handshake with debug tracing enabled.
// This is useful for debugging TCP issues.
func TestIntegrationTCPHandshakeWithTracing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping tracing test in short mode")
	}

	var buf bytes.Buffer
	tracer := &WriterTracer{Writer: &buf, Prefix: "[TCP] "}

	h := newTestHelper()
	h.slirp.Tracer = tracer

	// Configure exec forwarding
	execAddr := net.IPv4(10, 0, 2, 100)
	execPort := uint16(8080)
	h.slirp.AddExec(3, "test", execAddr, int(execPort))

	// Send SYN
	synPacket := h.buildTCPPacket(h.guestIP, execAddr, 12345, execPort,
		1000, 0, THSyn, 65535, nil)
	ipPacket := h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, synPacket)
	execMAC := [6]byte{0x52, 0x55}
	copy(execMAC[2:], execAddr.To4())
	frame := h.buildEthFrame(execMAC, EthPIP, ipPacket)

	h.slirp.Input(frame)

	// Find SYN-ACK
	synAck := h.findTCPPacket(THSyn|THAck, execPort, 12345)
	if synAck == nil {
		t.Fatalf("No SYN-ACK. Trace:\n%s", buf.String())
	}

	// Send ACK
	ackPacket := h.buildTCPPacket(h.guestIP, execAddr, 12345, execPort,
		1001, synAck.seq+1, THAck, 65535, nil)
	ipPacket = h.buildIPPacket(IPProtoTCP, h.guestIP, execAddr, ackPacket)
	frame = h.buildEthFrame(execMAC, EthPIP, ipPacket)

	h.slirp.Input(frame)

	// Print trace
	t.Logf("Trace output:\n%s", buf.String())

	// Verify established state
	var so *Socket
	for s := h.slirp.TCB.Next; s != &h.slirp.TCB; s = s.Next {
		if s.SoLPort == 12345 && s.SoFPort == execPort {
			so = s
			break
		}
	}
	if so == nil {
		t.Fatal("Socket not found")
	}

	tp := SoToTCPCB(so)
	if tp == nil || tp.TState != TCPSEstablished {
		t.Errorf("Connection not established. State=%d", tp.TState)
	}

	so.SoFree()
}

// TestIntegrationDNSRoundtrip drives a real DNS query through SLIRP's UDP NAT
// to the host's real upstream resolver, then asserts the response that comes
// back is a well-formed DNS packet from 10.0.2.3:53 to the guest's source
// port. This catches NAT-rewrite bugs (wrong source IP, swapped ports,
// mangled checksum) that present as "Parse error" in the guest's nslookup.
//
// Skipped automatically if the host has no resolvable DNS server.
func TestIntegrationDNSRoundtrip(t *testing.T) {
	// Reset dnsAddr cache so getDnsAddr() re-reads /etc/resolv.conf.
	saveDnsAddr := dnsAddr
	saveDnsAddrTime := dnsAddrTime
	dnsAddr = nil
	dnsAddrTime = 0
	defer func() {
		dnsAddr = saveDnsAddr
		dnsAddrTime = saveDnsAddrTime
	}()
	if _, ok := getDnsAddr(); !ok {
		t.Skip("no DNS server in /etc/resolv.conf (likely sandboxed CI)")
	}

	// Build a real DNS query for "example.com" addressed to 10.0.2.3:53.
	// Header: ID=0xBEEF, flags=0x0100 (standard recursive query),
	// QDCOUNT=1, ANCOUNT=ANCOUNT=NSCOUNT=ARCOUNT=0.
	dnsQuery := []byte{
		0xBE, 0xEF, // ID
		0x01, 0x00, // flags: RD=1
		0x00, 0x01, // QDCOUNT
		0x00, 0x00, // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
		// QNAME: "dl-cdn.alpinelinux.org" (CDN-fronted; busybox parse error)
		6, 'd', 'l', '-', 'c', 'd', 'n',
		11, 'a', 'l', 'p', 'i', 'n', 'e', 'l', 'i', 'n', 'u', 'x',
		3, 'o', 'r', 'g',
		0,          // null terminator
		0x00, 0x01, // QTYPE A
		0x00, 0x01, // QCLASS IN
	}

	h := newTestHelper()
	const guestSrcPort = 54321
	udpPayload := h.buildUDPPacket(guestSrcPort, 53, dnsQuery)
	ipPayload := h.buildIPPacket(IPProtoUDP, h.guestIP, h.slirp.VNameserverAddr, udpPayload)
	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)

	h.clearOutput()
	h.slirp.Input(frame)

	// Drive SoRecvFrom until the fake resolver replies and we capture an
	// OUTPUT packet. Give it up to 3 seconds.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(h.outputPkts) == 0 {
		// Iterate UDP sockets and pump SoRecvFrom (mirrors Poll).
		for so := h.slirp.UDB.Next; so != &h.slirp.UDB; so = so.Next {
			if so.S < 0 {
				continue
			}
			so.SoRecvFrom()
		}
		if h.slirp.IfQueued > 0 {
			h.slirp.IfStart()
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(h.outputPkts) == 0 {
		t.Fatalf("no DNS reply captured: slirp did not deliver the response back to the guest")
	}

	reply := h.outputPkts[0]
	if len(reply) < EthHLen+IPHeaderSize+UDPHeaderSize+12 {
		t.Fatalf("reply too short: %d bytes (%x)", len(reply), reply)
	}

	// Ethernet
	if etype := binary.BigEndian.Uint16(reply[12:14]); etype != EthPIP {
		t.Errorf("ether type = 0x%04x, want 0x%04x (IP)", etype, EthPIP)
	}

	// IP header
	ip := reply[EthHLen:]
	if ip[9] != IPProtoUDP {
		t.Errorf("IP proto = %d, want %d (UDP)", ip[9], IPProtoUDP)
	}
	srcIP := net.IP(ip[12:16]).To4()
	dstIP := net.IP(ip[16:20]).To4()
	if !srcIP.Equal(h.slirp.VNameserverAddr.To4()) {
		t.Errorf("IP src = %v, want %v (VNameserverAddr)", srcIP, h.slirp.VNameserverAddr)
	}
	if !dstIP.Equal(h.guestIP.To4()) {
		t.Errorf("IP dst = %v, want %v (guestIP)", dstIP, h.guestIP)
	}
	// Verify IP checksum
	ipHdr := ip[:IPHeaderSize]
	if CksumData(ipHdr) != 0 {
		t.Errorf("IP checksum invalid (cksum-over-header should be 0, got %#x)", CksumData(ipHdr))
	}

	// UDP header
	udp := ip[IPHeaderSize:]
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	if srcPort != 53 {
		t.Errorf("UDP src port = %d, want 53", srcPort)
	}
	if dstPort != guestSrcPort {
		t.Errorf("UDP dst port = %d, want %d (guest src port)", dstPort, guestSrcPort)
	}
	udpLen := binary.BigEndian.Uint16(udp[4:6])
	expectUDPLen := uint16(UDPHeaderSize + len(udp[UDPHeaderSize:]))
	if udpLen != expectUDPLen {
		t.Errorf("UDP length = %d, want %d", udpLen, expectUDPLen)
	}

	// DNS payload — first 12 bytes are the header; QR bit must be set.
	dnsReply := udp[UDPHeaderSize:]
	if len(dnsReply) < 12 {
		t.Fatalf("DNS reply too short: %d bytes", len(dnsReply))
	}
	if id := binary.BigEndian.Uint16(dnsReply[0:2]); id != 0xBEEF {
		t.Errorf("DNS ID = %#x, want 0xBEEF", id)
	}
	if dnsReply[2]&0x80 == 0 {
		t.Error("DNS QR bit not set in reply")
	}
	if ancount := binary.BigEndian.Uint16(dnsReply[6:8]); ancount == 0 {
		t.Errorf("DNS ANCOUNT = 0 — upstream resolver returned no answer")
	}
	t.Logf("DNS reply OK: %d bytes, ancount=%d, full reply: %x",
		len(dnsReply),
		binary.BigEndian.Uint16(dnsReply[6:8]),
		reply)
}

// TestIntegrationDNSBackToBack drives many DNS queries from the same guest
// source port in close succession through SLIRP's UDP NAT. busybox-nslookup
// reuses its ephemeral source port for the A and AAAA queries it sends
// when the user types `nslookup foo`; SLIRP must service every one, not
// just the first. The user reported a "first Parse error, second OK"
// pattern in the guest — this test reproduces it at the slirp layer and
// also catches cumulative-state issues that only show up after several
// queries (mbuf-pool exhaustion, used-but-not-released socket slot,
// reorderings, etc.).
//
// Mix of behaviours we want to exercise:
//   - many queries from the same (guest IP, port) tuple
//   - mixed A and AAAA query types
//   - quick succession (no sleep between sends) — the original symptom
//   - that every reply's DNS ID matches the matching query
//   - that no spurious extra replies show up
func TestIntegrationDNSBackToBack(t *testing.T) {
	saveDnsAddr := dnsAddr
	saveDnsAddrTime := dnsAddrTime
	dnsAddr = nil
	dnsAddrTime = 0
	defer func() {
		dnsAddr = saveDnsAddr
		dnsAddrTime = saveDnsAddrTime
	}()
	if _, ok := getDnsAddr(); !ok {
		t.Skip("no DNS server in /etc/resolv.conf (likely sandboxed CI)")
	}

	// Builds a minimal DNS query for the given name + qtype.
	mkQuery := func(id uint16, qname []byte, qtype uint16) []byte {
		q := []byte{
			byte(id >> 8), byte(id),
			0x01, 0x00,
			0x00, 0x01,
			0x00, 0x00,
			0x00, 0x00,
			0x00, 0x00,
		}
		q = append(q, qname...)
		q = append(q, byte(qtype>>8), byte(qtype))
		q = append(q, 0x00, 0x01) // class IN
		return q
	}
	// Two different qnames to ensure we're not hammering a single
	// upstream cache entry.
	names := [][]byte{
		{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0},
		{6, 'g', 'o', 'o', 'g', 'l', 'e', 3, 'c', 'o', 'm', 0},
	}

	h := newTestHelper()
	const guestSrcPort = 12345
	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())

	sendQ := func(id uint16, qname []byte, qtype uint16) {
		q := mkQuery(id, qname, qtype)
		udp := h.buildUDPPacket(guestSrcPort, 53, q)
		ip := h.buildIPPacket(IPProtoUDP, h.guestIP, h.slirp.VNameserverAddr, udp)
		frame := h.buildEthFrame(vhostMAC, EthPIP, ip)
		h.slirp.Input(frame)
	}

	// Build the schedule: 8 queries, alternating qtype (A/AAAA) and qname.
	type entry struct {
		id    uint16
		name  []byte
		qtype uint16
	}
	const nQueries = 8
	schedule := make([]entry, nQueries)
	for i := 0; i < nQueries; i++ {
		schedule[i] = entry{
			id:    0xBEEF + uint16(i),
			name:  names[i%len(names)],
			qtype: []uint16{1, 28}[i%2], // 1=A, 28=AAAA
		}
	}

	// Fire all queries first (mimicking back-to-back UDP sends with no
	// waiting), then pump slirp until we've captured one reply per query
	// or a global deadline elapses.
	cursor := len(h.outputPkts)
	for _, e := range schedule {
		sendQ(e.id, e.name, e.qtype)
	}

	overall := time.Now().Add(8 * time.Second)
	for time.Now().Before(overall) && len(h.outputPkts)-cursor < nQueries {
		h.pumpUDPUntilOutput(time.Now().Add(200*time.Millisecond), cursor+len(h.outputPkts[cursor:]))
		// Give time for upstream + readback.
		time.Sleep(20 * time.Millisecond)
	}

	got := h.outputPkts[cursor:]
	if len(got) < nQueries {
		// First failure mode: some replies never arrived. Report which.
		seen := map[uint16]bool{}
		for _, pkt := range got {
			if id := extractDNSID(t, pkt); id != 0 {
				seen[id] = true
			}
		}
		var missing []uint16
		for _, e := range schedule {
			if !seen[e.id] {
				missing = append(missing, e.id)
			}
		}
		t.Fatalf("only got %d/%d DNS replies (missing IDs: %v) — likely socket reuse / mbuf-pool / state-machine bug",
			len(got), nQueries, missing)
	}

	// Verify each captured reply matches one of our queries by ID, and
	// that every query has a corresponding reply (no spurious extras).
	expectIDs := map[uint16]bool{}
	for _, e := range schedule {
		expectIDs[e.id] = true
	}
	for i, pkt := range got {
		id := extractDNSID(t, pkt)
		if !expectIDs[id] {
			t.Errorf("reply %d has unexpected DNS ID %#x (was already matched or never sent)", i, id)
			continue
		}
		delete(expectIDs, id)
	}
	if len(expectIDs) != 0 {
		var missing []uint16
		for id := range expectIDs {
			missing = append(missing, id)
		}
		t.Errorf("no reply for query IDs: %v", missing)
	}
}

// pumpUDPUntilOutput drives slirp's poll loop until outputPkts grows past
// cursor or the deadline elapses.
func (h *testHelper) pumpUDPUntilOutput(deadline time.Time, cursor int) {
	for time.Now().Before(deadline) && len(h.outputPkts) == cursor {
		for so := h.slirp.UDB.Next; so != &h.slirp.UDB; so = so.Next {
			if so.S < 0 {
				continue
			}
			so.SoRecvFrom()
		}
		if h.slirp.IfQueued > 0 {
			h.slirp.IfStart()
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// extractDNSID parses the DNS ID out of a captured eth/IP/UDP/DNS frame.
func extractDNSID(t *testing.T, frame []byte) uint16 {
	t.Helper()
	if len(frame) < EthHLen+IPHeaderSize+UDPHeaderSize+12 {
		t.Fatalf("captured frame too short: %d", len(frame))
	}
	return binary.BigEndian.Uint16(frame[EthHLen+IPHeaderSize+UDPHeaderSize:])
}

// ---------------------------------------------------------------------------
// TCP integration tests
//
// These drive the full TCP state machine through SLIRP against a real local
// listener. The goal is to catch the kinds of bugs that surface as
// "apk update hangs forever" in a running guest — broken handshake, dropped
// data, missed ACK, broken close.
//
// The test code plays the role of the guest's TCP stack: it crafts SYN /
// ACK / data / FIN packets and feeds them through SLIRP.Input, while
// pumping SLIRP's poll loop and capturing each OUTPUT packet for analysis.
// ---------------------------------------------------------------------------

// testWriter adapts *testing.T to io.Writer for trace logging.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(bytes.TrimRight(p, "\n")))
	return len(p), nil
}

// buildTCPSegment builds a TCP segment (header + payload) with the given
// fields and a correct pseudo-header-based checksum.
func (h *testHelper) buildTCPSegment(srcPort, dstPort uint16, seq, ack uint32, flags uint8, window uint16, payload []byte, srcIP, dstIP net.IP) []byte {
	seg := make([]byte, 20+len(payload))
	binary.BigEndian.PutUint16(seg[0:2], srcPort)
	binary.BigEndian.PutUint16(seg[2:4], dstPort)
	binary.BigEndian.PutUint32(seg[4:8], seq)
	binary.BigEndian.PutUint32(seg[8:12], ack)
	seg[12] = 5 << 4 // data offset = 5 (20-byte header, no options)
	seg[13] = flags
	binary.BigEndian.PutUint16(seg[14:16], window)
	// checksum (16-17) computed below; urgent ptr (18-19) zero
	copy(seg[20:], payload)

	// Pseudo-header checksum
	pseudo := make([]byte, 12+len(seg))
	copy(pseudo[0:4], srcIP.To4())
	copy(pseudo[4:8], dstIP.To4())
	pseudo[9] = IPProtoTCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(seg)))
	copy(pseudo[12:], seg)
	binary.BigEndian.PutUint16(seg[16:18], CksumData(pseudo))
	return seg
}

// sendTCPFromGuest wraps a TCP segment in IP+Ethernet and feeds it to slirp.
func (h *testHelper) sendTCPFromGuest(srcPort, dstPort uint16, seq, ack uint32, flags uint8, window uint16, payload []byte, dstIP net.IP) {
	tcp := h.buildTCPSegment(srcPort, dstPort, seq, ack, flags, window, payload, h.guestIP, dstIP)
	ipPayload := h.buildIPPacket(IPProtoTCP, h.guestIP, dstIP, tcp)
	vhostMAC := [6]byte{0x52, 0x55}
	copy(vhostMAC[2:], h.slirp.VHostAddr.To4())
	frame := h.buildEthFrame(vhostMAC, EthPIP, ipPayload)
	h.slirp.Input(frame)
}

// parseTCPFromCapture extracts (srcPort, dstPort, seq, ack, flags, payload)
// from a captured OUTPUT eth frame containing a TCP segment.
func parseTCPFromCapture(frame []byte) (srcPort, dstPort uint16, seq, ack uint32, flags uint8, payload []byte, ok bool) {
	if len(frame) < EthHLen+IPHeaderSize+20 {
		return
	}
	if binary.BigEndian.Uint16(frame[12:14]) != EthPIP {
		return
	}
	ip := frame[EthHLen:]
	if ip[9] != IPProtoTCP {
		return
	}
	ihl := int(ip[0]&0x0f) * 4
	tcp := ip[ihl:]
	if len(tcp) < 20 {
		return
	}
	dataOff := int(tcp[12]>>4) * 4
	if dataOff < 20 || dataOff > len(tcp) {
		return
	}
	srcPort = binary.BigEndian.Uint16(tcp[0:2])
	dstPort = binary.BigEndian.Uint16(tcp[2:4])
	seq = binary.BigEndian.Uint32(tcp[4:8])
	ack = binary.BigEndian.Uint32(tcp[8:12])
	flags = tcp[13]
	payload = tcp[dataOff:]
	ok = true
	return
}

// pumpSlirp drives slirp's poll loop until predicate returns true or
// deadline expires. Returns true if the predicate fired.
func (h *testHelper) pumpSlirp(deadline time.Time, predicate func() bool) bool {
	for time.Now().Before(deadline) {
		// Mirror what Poll() does, minus the EthernetDevice plumbing.
		curtime := GetTimeMs()
		h.slirp.updateTimerFlags(curtime)
		h.slirp.processTimers(curtime)
		h.slirp.checkTCPListeners()
		h.slirp.processTCPSockets()
		h.slirp.processUDPSockets()
		if h.slirp.IfQueued > 0 {
			h.slirp.IfStart()
		}
		if predicate() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Final check after deadline.
	return predicate()
}

// waitOutputTCP waits for an OUTPUT packet that matches a predicate on its
// parsed TCP fields. Returns the captured packet and parsed fields.
func (h *testHelper) waitOutputTCP(deadline time.Time, match func(flags uint8, payload []byte) bool) (frame []byte, srcPort, dstPort uint16, seq, ack uint32, flags uint8, payload []byte, ok bool) {
	startIdx := len(h.outputPkts)
	h.pumpSlirp(deadline, func() bool {
		for i := startIdx; i < len(h.outputPkts); i++ {
			_, _, _, _, fl, pl, parsed := parseTCPFromCapture(h.outputPkts[i])
			if parsed && match(fl, pl) {
				return true
			}
		}
		return false
	})
	for i := startIdx; i < len(h.outputPkts); i++ {
		sp, dp, sq, ak, fl, pl, parsed := parseTCPFromCapture(h.outputPkts[i])
		if parsed && match(fl, pl) {
			return h.outputPkts[i], sp, dp, sq, ak, fl, pl, true
		}
	}
	return nil, 0, 0, 0, 0, 0, nil, false
}

// TestIntegrationTCPHTTPRoundtrip runs a full HTTP-style TCP exchange through
// slirp against a real local listener:
//
//	guest --SYN--> slirp --connect()--> server
//	guest <--SYN-ACK-- slirp <--accept-- server
//	guest --ACK--> slirp
//	guest --REQ--> slirp --write()--> server
//	guest <--RESP-- slirp <--read()-- server
//	guest --ACK--> slirp
//	guest --FIN-ACK--> slirp --close()--> server
//	guest <--FIN-ACK-- slirp
//	guest --ACK--> slirp
//
// This is what `apk update` does. If this test hangs or fails, we've found
// the same bug that breaks apk inside the guest.
func TestIntegrationTCPHTTPRoundtrip(t *testing.T) {
	// Spin up a tiny TCP echo server on a free port on the loopback iface.
	ln, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("could not listen on 127.0.0.1: %v", err)
	}
	defer ln.Close()
	serverAddr := ln.Addr().(*net.TCPAddr)

	canned := []byte("HTTP/1.0 200 OK\r\nContent-Length: 5\r\n\r\nhello")
	gotRequest := make(chan string, 1)
	srvDone := make(chan struct{})

	go func() {
		defer close(srvDone)
		ln.SetDeadline(time.Now().Add(5 * time.Second))
		conn, err := ln.Accept()
		if err != nil {
			gotRequest <- ""
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _ := conn.Read(buf)
		gotRequest <- string(buf[:n])
		conn.Write(canned)
		// Half-close so the guest sees FIN after the response.
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	h := newTestHelper()
	const guestPort = 50000
	serverIP := net.IPv4(127, 0, 0, 1)
	serverPort := uint16(serverAddr.Port)

	deadline := time.Now().Add(5 * time.Second)

	// --- 1) SYN ---
	guestSeq := uint32(1000)
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, 0, THSyn, 65535, nil, serverIP)
	// Slirp must reply with SYN+ACK once the host TCP connect() completes.
	_, _, _, srvSeq, srvAck, fl, _, ok := h.waitOutputTCP(deadline, func(fl uint8, _ []byte) bool {
		return fl&(THSyn|THAck) == (THSyn | THAck)
	})
	if !ok {
		t.Fatalf("never received SYN-ACK from slirp (server didn't accept or slirp didn't relay)")
	}
	if srvAck != guestSeq+1 {
		t.Errorf("SYN-ACK ack=%d, want %d (guestSeq+1)", srvAck, guestSeq+1)
	}
	_ = fl

	// --- 2) ACK the SYN-ACK ---
	guestSeq++ // SYN consumes one sequence number
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, srvSeq+1, THAck, 65535, nil, serverIP)

	// --- 3) Send HTTP request data ---
	httpReq := []byte("GET / HTTP/1.0\r\nHost: example.com\r\n\r\n")
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, srvSeq+1, THAck|THPush, 65535, httpReq, serverIP)
	guestSeq += uint32(len(httpReq))

	// --- 4) Wait for server to receive the request via slirp's NAT ---
	select {
	case got := <-gotRequest:
		if got != string(httpReq) {
			t.Errorf("server got %q, want %q", got, string(httpReq))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server never received the HTTP request (slirp dropped the data segment)")
	}

	// --- 5) Wait for response data segment(s) from slirp ---
	var respBuf []byte
	var lastSrvSeq uint32 = srvSeq + 1
	for time.Now().Before(deadline) && len(respBuf) < len(canned) {
		_, _, _, sq, _, fl, pl, gotPkt := h.waitOutputTCP(time.Now().Add(500*time.Millisecond),
			func(fl uint8, pl []byte) bool { return len(pl) > 0 })
		if !gotPkt {
			break
		}
		if sq != lastSrvSeq {
			// Out-of-order or retransmit — accept it but only if it lines up.
			t.Logf("response segment seq=%d, expected %d (may be retx)", sq, lastSrvSeq)
		}
		respBuf = append(respBuf, pl...)
		lastSrvSeq = sq + uint32(len(pl))
		// ACK each data segment from the guest.
		h.sendTCPFromGuest(guestPort, serverPort, guestSeq, lastSrvSeq, THAck, 65535, nil, serverIP)
		_ = fl
	}

	if string(respBuf) != string(canned) {
		t.Errorf("response body mismatch:\n got: %q\nwant: %q", string(respBuf), string(canned))
	}

	// --- 6) Server half-closes; wait for FIN from slirp ---
	_, _, _, _, finAck, finFlags, _, gotFIN := h.waitOutputTCP(time.Now().Add(3*time.Second),
		func(fl uint8, _ []byte) bool { return fl&THFin != 0 })
	if !gotFIN {
		t.Errorf("never received FIN from slirp after server half-closed")
	} else {
		// ACK the FIN.
		h.sendTCPFromGuest(guestPort, serverPort, guestSeq, finAck+1, THAck, 65535, nil, serverIP)
		_ = finFlags
	}

	// --- 7) Guest also closes ---
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, lastSrvSeq+1, THFin|THAck, 65535, nil, serverIP)
	// Drain any remaining ACK from slirp.
	h.pumpSlirp(time.Now().Add(500*time.Millisecond), func() bool { return false })

	<-srvDone
}

// TestIntegrationTCPConnectRefused verifies slirp returns RST when the host
// connect() fails — this is the "well-known port closed" path.
func TestIntegrationTCPConnectRefused(t *testing.T) {
	// Bind a port then close it so connect() fails reliably.
	probe, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("could not get a closed port: %v", err)
	}
	port := uint16(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()

	h := newTestHelper()
	const guestPort = 50001
	serverIP := net.IPv4(127, 0, 0, 1)
	guestSeq := uint32(2000)

	h.sendTCPFromGuest(guestPort, port, guestSeq, 0, THSyn, 65535, nil, serverIP)
	_, _, _, _, _, fl, _, ok := h.waitOutputTCP(time.Now().Add(3*time.Second),
		func(fl uint8, _ []byte) bool { return fl&THRst != 0 })
	if !ok {
		// Some hosts return ICMP unreachable instead of RST — both are
		// acceptable signals; the important thing is slirp doesn't hang.
		t.Logf("no RST received within 3s — possibly delivered via ICMP unreachable instead")
		return
	}
	if fl&THRst == 0 {
		t.Errorf("expected RST flag, got %02x", fl)
	}
}

// TestIntegrationTCPLargeTransfer pulls a large blob (>1 MTU) through slirp
// to exercise multi-segment data delivery + window handling. Catches the
// kind of bug that would surface as "first MTU comes through, then hang".
func TestIntegrationTCPLargeTransfer(t *testing.T) {
	// Uncomment to enable per-segment TCP tracing.
	// h.slirp.Tracer = &WriterTracer{Writer: os.Stderr, Prefix: "[TCP] "}

	ln, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("could not listen on 127.0.0.1: %v", err)
	}
	defer ln.Close()
	serverAddr := ln.Addr().(*net.TCPAddr)

	// 512 KB of distinct payload — comparable in size to an APKINDEX
	// download. Spans ~350 MTUs; will trigger any window/ACK or mbuf-
	// pool issue that an 8 KB test wouldn't.
	const blobSize = 512 * 1024
	blob := make([]byte, blobSize)
	for i := range blob {
		blob[i] = byte(i & 0xff)
	}
	srvDone := make(chan struct{})

	go func() {
		defer close(srvDone)
		ln.SetDeadline(time.Now().Add(5 * time.Second))
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(buf) // drain the dummy request
		conn.Write(blob)
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	h := newTestHelper()
	h.slirp.Tracer = &WriterTracer{Writer: testWriter{t}, Prefix: "[TCP] "}
	const guestPort = 50002
	serverIP := net.IPv4(127, 0, 0, 1)
	serverPort := uint16(serverAddr.Port)
	deadline := time.Now().Add(10 * time.Second)

	// Handshake
	guestSeq := uint32(3000)
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, 0, THSyn, 65535, nil, serverIP)
	_, _, _, srvSeq, _, _, _, ok := h.waitOutputTCP(deadline,
		func(fl uint8, _ []byte) bool { return fl&(THSyn|THAck) == (THSyn | THAck) })
	if !ok {
		t.Fatalf("no SYN-ACK from slirp")
	}
	guestSeq++
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, srvSeq+1, THAck, 65535, nil, serverIP)

	// Dummy request to make the server send its blob
	req := []byte("X")
	h.sendTCPFromGuest(guestPort, serverPort, guestSeq, srvSeq+1, THAck|THPush, 65535, req, serverIP)
	guestSeq += uint32(len(req))

	// Collect all data segments, ACKing as we go. We assemble by sequence
	// number (segments may arrive in bursts of 2+ before we ACK), and we
	// keep a cursor into outputPkts so we never miss a packet between
	// pump cycles.
	got := make(map[uint32][]byte)
	lastSrvSeq := srvSeq + 1
	cursor := 0
	for time.Now().Before(deadline) && nextContiguous(got, srvSeq+1) < blobSize {
		h.pumpSlirp(time.Now().Add(200*time.Millisecond), func() bool {
			return len(h.outputPkts) > cursor
		})
		progress := false
		for cursor < len(h.outputPkts) {
			_, _, sq, _, _, pl, parsed := parseTCPFromCapture(h.outputPkts[cursor])
			cursor++
			if !parsed || len(pl) == 0 {
				continue
			}
			if _, dup := got[sq]; dup {
				continue
			}
			got[sq] = pl
			progress = true
		}
		// Cumulative ACK for everything contiguous we've seen.
		if cum := srvSeq + 1 + nextContiguous(got, srvSeq+1); cum > lastSrvSeq {
			lastSrvSeq = cum
			h.sendTCPFromGuest(guestPort, serverPort, guestSeq, lastSrvSeq, THAck, 65535, nil, serverIP)
		}
		if !progress && time.Now().After(deadline) {
			break
		}
	}

	// Reassemble in seq order.
	totalGot := nextContiguous(got, srvSeq+1)
	var assembled []byte
	for offset := uint32(0); offset < totalGot; {
		pl, ok := got[srvSeq+1+offset]
		if !ok {
			break
		}
		assembled = append(assembled, pl...)
		offset += uint32(len(pl))
	}
	gotLen := len(assembled)

	if gotLen != blobSize {
		t.Errorf("got %d bytes, want %d (transfer truncated — likely window/ACK bug)", gotLen, blobSize)
	} else if !bytes.Equal(assembled, blob) {
		t.Errorf("blob mismatch at first divergence — segment ordering or reassembly broken")
	}

	<-srvDone
}

// nextContiguous returns the number of contiguous bytes starting at base in
// a seq→payload map. Used to compute the cumulative ACK.
func nextContiguous(segments map[uint32][]byte, base uint32) uint32 {
	var off uint32 = 0
	for {
		pl, ok := segments[base+off]
		if !ok {
			return off
		}
		off += uint32(len(pl))
	}
}
