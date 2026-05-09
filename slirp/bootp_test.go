package slirp

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// TestBOOTPConstants verifies the BOOTP constants match the C definitions.
// Reference: tinyemu-2019-12-21/slirp/bootp.h
func TestBOOTPConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      int
		expected int
	}{
		{"BootpServer", BootpServer, 67},
		{"BootpClient", BootpClient, 68},
		{"BootpRequest", BootpRequest, 1},
		{"BootpReply", BootpReply, 2},
		{"RFC1533Pad", RFC1533Pad, 0},
		{"RFC1533Netmask", RFC1533Netmask, 1},
		{"RFC1533Gateway", RFC1533Gateway, 3},
		{"RFC1533DNS", RFC1533DNS, 6},
		{"RFC1533Hostname", RFC1533Hostname, 12},
		{"RFC1533End", RFC1533End, 255},
		{"RFC2132ReqAddr", RFC2132ReqAddr, 50},
		{"RFC2132LeaseTime", RFC2132LeaseTime, 51},
		{"RFC2132MsgType", RFC2132MsgType, 53},
		{"RFC2132SrvID", RFC2132SrvID, 54},
		{"RFC2132Message", RFC2132Message, 56},
		{"DHCPDiscover", DHCPDiscover, 1},
		{"DHCPOffer", DHCPOffer, 2},
		{"DHCPRequest", DHCPRequest, 3},
		{"DHCPAck", DHCPAck, 5},
		{"DHCPNak", DHCPNak, 6},
		{"DHCPOptLen", DHCPOptLen, 312},
		{"NBBootpClients", NBBootpClients, 16},
		{"LeaseTime", LeaseTime, 24 * 3600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestRFC1533Cookie verifies the magic cookie value.
func TestRFC1533Cookie(t *testing.T) {
	expected := []byte{99, 130, 83, 99}
	if !bytes.Equal(RFC1533Cookie, expected) {
		t.Errorf("RFC1533Cookie = %v, want %v", RFC1533Cookie, expected)
	}
}

// TestBootpTSize verifies the BootpT structure size.
func TestBootpTSize(t *testing.T) {
	// The BOOTP structure size should be 548 bytes (without IP/UDP headers)
	if BootpSize != 548 {
		t.Errorf("BootpSize = %d, want 548", BootpSize)
	}
}

// TestParseBootpNil tests ParseBootp with insufficient data.
func TestParseBootpNil(t *testing.T) {
	// Should return nil for data smaller than BootpMinSize
	if ParseBootp(nil) != nil {
		t.Error("ParseBootp(nil) should return nil")
	}
	if ParseBootp(make([]byte, BootpMinSize-1)) != nil {
		t.Error("ParseBootp with insufficient data should return nil")
	}
}

// TestParseBootpAndMarshal tests round-trip parsing and marshaling.
func TestParseBootpAndMarshal(t *testing.T) {
	// Create test data
	data := make([]byte, BootpSize)
	data[0] = BootpRequest                            // Op
	data[1] = 1                                       // Htype (Ethernet)
	data[2] = 6                                       // Hlen
	data[3] = 0                                       // Hops
	binary.BigEndian.PutUint32(data[4:8], 0x12345678) // Xid
	binary.BigEndian.PutUint16(data[8:10], 100)       // Secs
	// Set client IP
	copy(data[12:16], net.IPv4(10, 0, 2, 15).To4())
	// Set hardware address
	copy(data[28:34], []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56})

	bp := ParseBootp(data)
	if bp == nil {
		t.Fatal("ParseBootp returned nil")
	}

	if bp.Op != BootpRequest {
		t.Errorf("Op = %d, want %d", bp.Op, BootpRequest)
	}
	if bp.Htype != 1 {
		t.Errorf("Htype = %d, want 1", bp.Htype)
	}
	if bp.Hlen != 6 {
		t.Errorf("Hlen = %d, want 6", bp.Hlen)
	}
	if bp.Xid != 0x12345678 {
		t.Errorf("Xid = %x, want 12345678", bp.Xid)
	}
	if bp.Secs != 100 {
		t.Errorf("Secs = %d, want 100", bp.Secs)
	}
	if !bp.Ciaddr.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Errorf("Ciaddr = %v, want 10.0.2.15", bp.Ciaddr)
	}

	expectedMAC := []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	if !bytes.Equal(bp.Hwaddr[:6], expectedMAC) {
		t.Errorf("Hwaddr = %v, want %v", bp.Hwaddr[:6], expectedMAC)
	}

	// Marshal back
	marshaled := make([]byte, BootpSize)
	bp.Marshal(marshaled)

	// Compare key fields
	if marshaled[0] != BootpRequest {
		t.Errorf("Marshaled Op = %d, want %d", marshaled[0], BootpRequest)
	}
	if binary.BigEndian.Uint32(marshaled[4:8]) != 0x12345678 {
		t.Error("Marshaled Xid mismatch")
	}
}

// TestDHCPDecode tests the DHCP option decoding.
func TestDHCPDecode(t *testing.T) {
	bp := &BootpT{}

	// Set up DHCP options with magic cookie
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType // Message type option
	bp.Vend[5] = 1              // Length
	bp.Vend[6] = DHCPDiscover   // DHCP Discover
	bp.Vend[7] = RFC2132ReqAddr // Requested address option
	bp.Vend[8] = 4              // Length
	copy(bp.Vend[9:13], net.IPv4(10, 0, 2, 100).To4())
	bp.Vend[13] = RFC1533End

	msgType, reqAddr := dhcpDecode(bp)

	if msgType != DHCPDiscover {
		t.Errorf("msgType = %d, want %d", msgType, DHCPDiscover)
	}
	if !reqAddr.Equal(net.IPv4(10, 0, 2, 100)) {
		t.Errorf("reqAddr = %v, want 10.0.2.100", reqAddr)
	}
}

// TestDHCPDecodeNoCookie tests decoding with invalid magic cookie.
func TestDHCPDecodeNoCookie(t *testing.T) {
	bp := &BootpT{}

	// No magic cookie
	bp.Vend[0] = 0
	bp.Vend[1] = 0
	bp.Vend[2] = 0
	bp.Vend[3] = 0

	msgType, reqAddr := dhcpDecode(bp)

	if msgType != 0 {
		t.Errorf("msgType = %d, want 0", msgType)
	}
	if !reqAddr.Equal(net.IPv4zero) {
		t.Errorf("reqAddr = %v, want 0.0.0.0", reqAddr)
	}
}

// TestDHCPDecodeRequest tests DHCPREQUEST with client IP fallback.
func TestDHCPDecodeRequest(t *testing.T) {
	bp := &BootpT{}
	bp.Ciaddr = net.IPv4(10, 0, 2, 50)

	// Set up DHCP options with magic cookie but no requested address
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType // Message type option
	bp.Vend[5] = 1              // Length
	bp.Vend[6] = DHCPRequest    // DHCP Request
	bp.Vend[7] = RFC1533End

	msgType, reqAddr := dhcpDecode(bp)

	if msgType != DHCPRequest {
		t.Errorf("msgType = %d, want %d", msgType, DHCPRequest)
	}
	// Should fall back to Ciaddr
	if !reqAddr.Equal(net.IPv4(10, 0, 2, 50)) {
		t.Errorf("reqAddr = %v, want 10.0.2.50", reqAddr)
	}
}

// TestGetNewAddr tests address allocation.
func TestGetNewAddr(t *testing.T) {
	s := NewSlirp()

	mac1 := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	mac2 := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x57}

	// Allocate first address
	bc1, addr1 := s.getNewAddr(mac1)
	if bc1 == nil {
		t.Fatal("getNewAddr returned nil")
	}
	if !addr1.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Errorf("addr1 = %v, want 10.0.2.15", addr1)
	}
	if bc1.Allocated != 1 {
		t.Errorf("bc1.Allocated = %d, want 1", bc1.Allocated)
	}
	// Save MAC like bootp_reply does
	copy(bc1.MACAddr[:], mac1[:])

	// Allocate second address
	bc2, addr2 := s.getNewAddr(mac2)
	if bc2 == nil {
		t.Fatal("getNewAddr returned nil for second allocation")
	}
	if !addr2.Equal(net.IPv4(10, 0, 2, 16)) {
		t.Errorf("addr2 = %v, want 10.0.2.16", addr2)
	}
	copy(bc2.MACAddr[:], mac2[:])

	// Same MAC should return same slot (because MAC was saved)
	bc1Again, addr1Again := s.getNewAddr(mac1)
	if bc1Again != bc1 {
		t.Error("Same MAC should return same BOOTPClient")
	}
	if !addr1Again.Equal(addr1) {
		t.Errorf("addr1Again = %v, want %v", addr1Again, addr1)
	}
}

// TestRequestAddr tests requesting a specific address.
func TestRequestAddr(t *testing.T) {
	s := NewSlirp()

	mac := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	// Request an address in the valid range
	bc := s.requestAddr(net.IPv4(10, 0, 2, 17), mac)
	if bc == nil {
		t.Fatal("requestAddr returned nil")
	}
	if bc.Allocated != 1 {
		t.Errorf("bc.Allocated = %d, want 1", bc.Allocated)
	}

	// Request an address out of range
	bcInvalid := s.requestAddr(net.IPv4(10, 0, 2, 100), mac)
	if bcInvalid != nil {
		t.Error("requestAddr should return nil for out-of-range address")
	}

	// Request an address below DHCP start
	bcBelow := s.requestAddr(net.IPv4(10, 0, 2, 14), mac)
	if bcBelow != nil {
		t.Error("requestAddr should return nil for address below DHCP start")
	}
}

// TestFindAddr tests finding an existing lease.
func TestFindAddr(t *testing.T) {
	s := NewSlirp()

	mac := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	// Should not find anything initially
	bc, _ := s.findAddr(mac)
	if bc != nil {
		t.Error("findAddr should return nil for unknown MAC")
	}

	// Allocate an address and save the MAC (as bootp_reply does)
	bcAlloc, _ := s.getNewAddr(mac)
	if bcAlloc == nil {
		t.Fatal("getNewAddr returned nil")
	}
	copy(bcAlloc.MACAddr[:], mac[:])

	// Now should find it
	bc, addr := s.findAddr(mac)
	if bc == nil {
		t.Fatal("findAddr returned nil for known MAC")
	}
	if !addr.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Errorf("addr = %v, want 10.0.2.15", addr)
	}
}

// TestIPConversions tests the IP conversion helpers.
func TestIPConversions(t *testing.T) {
	ip := net.IPv4(10, 0, 2, 15)
	n := ipToUint32(ip)

	expected := uint32(10<<24 | 0<<16 | 2<<8 | 15)
	if n != expected {
		t.Errorf("ipToUint32(%v) = %d, want %d", ip, n, expected)
	}

	converted := uint32ToIP(n)
	if !converted.Equal(ip) {
		t.Errorf("uint32ToIP(%d) = %v, want %v", n, converted, ip)
	}
}

// TestBootpInputNil tests BootpInput with nil/invalid mbuf.
func TestBootpInputNil(t *testing.T) {
	s := NewSlirp()

	// Should not panic with nil
	s.BootpInput(nil)

	// Should not panic with too small mbuf
	m := s.MGet()
	m.Len = 10 // Too small
	s.BootpInput(m)
}

// TestBOOTPClientStruct tests the BOOTPClient structure.
func TestBOOTPClientStruct(t *testing.T) {
	bc := BOOTPClient{
		Allocated: 1,
		MACAddr:   [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56},
	}

	if bc.Allocated != 1 {
		t.Errorf("Allocated = %d, want 1", bc.Allocated)
	}

	expectedMAC := [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	if bc.MACAddr != expectedMAC {
		t.Errorf("MACAddr = %v, want %v", bc.MACAddr, expectedMAC)
	}
}

// TestSockaddrIn tests the SockaddrIn structure.
func TestSockaddrIn(t *testing.T) {
	sa := SockaddrIn{
		Addr: net.IPv4(10, 0, 2, 2),
		Port: 67,
	}

	if !sa.Addr.Equal(net.IPv4(10, 0, 2, 2)) {
		t.Errorf("Addr = %v, want 10.0.2.2", sa.Addr)
	}
	if sa.Port != 67 {
		t.Errorf("Port = %d, want 67", sa.Port)
	}
}

// TestUDPOutput2 tests basic UDP output functionality.
func TestUDPOutput2(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	// Set up some test data
	testData := []byte("test data")
	copy(m.Data[:len(testData)], testData)
	m.Len = len(testData)

	saddr := &SockaddrIn{
		Addr: net.IPv4(10, 0, 2, 2),
		Port: 67,
	}
	daddr := &SockaddrIn{
		Addr: net.IPv4(255, 255, 255, 255),
		Port: 68,
	}

	// This should not panic
	result := s.UDPOutput2(nil, m, saddr, daddr, IPTOSLowDelay)

	// Result should be 0 (success) since IPOutput accepts it
	if result != 0 {
		t.Errorf("UDPOutput2 returned %d, want 0", result)
	}
}

// TestBootpMarshalNilFields tests marshaling with nil IP fields.
func TestBootpMarshalNilFields(t *testing.T) {
	bp := &BootpT{
		Op:    BootpReply,
		Htype: 1,
		Hlen:  6,
		Xid:   0x12345678,
		// Leave IP fields nil
	}

	data := make([]byte, BootpSize)
	bp.Marshal(data) // Should not panic

	if data[0] != BootpReply {
		t.Errorf("Op = %d, want %d", data[0], BootpReply)
	}

	// Should have written zeros for nil IPs
	for i := 12; i < 28; i++ {
		if data[i] != 0 {
			t.Errorf("data[%d] = %d, want 0 for nil IP", i, data[i])
		}
	}
}

// TestDHCPDecodePadding tests handling of PAD options.
func TestDHCPDecodePadding(t *testing.T) {
	bp := &BootpT{}

	// Set up options with padding
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC1533Pad     // Padding
	bp.Vend[5] = RFC1533Pad     // Padding
	bp.Vend[6] = RFC2132MsgType // Message type option
	bp.Vend[7] = 1              // Length
	bp.Vend[8] = DHCPRequest    // DHCP Request
	bp.Vend[9] = RFC1533End

	msgType, _ := dhcpDecode(bp)

	if msgType != DHCPRequest {
		t.Errorf("msgType = %d, want %d (with padding)", msgType, DHCPRequest)
	}
}

// TestGetNewAddrExhaustion tests address pool exhaustion.
func TestGetNewAddrExhaustion(t *testing.T) {
	s := NewSlirp()

	// Allocate all addresses
	for i := 0; i < NBBootpClients; i++ {
		mac := [6]byte{0x52, 0x54, 0x00, byte(i), 0x00, 0x00}
		bc, _ := s.getNewAddr(mac)
		if bc == nil {
			t.Fatalf("Failed to allocate address %d", i)
		}
	}

	// Try to allocate one more - should fail
	mac := [6]byte{0x52, 0x54, 0x00, 0xFF, 0xFF, 0xFF}
	bc, _ := s.getNewAddr(mac)
	if bc != nil {
		t.Error("getNewAddr should return nil when pool is exhausted")
	}
}

// TestBootpReplyFindAddrFallback tests the "windows fix" path where a DHCPREQUEST
// comes with no requested address and findAddr fails, so getNewAddr is used as fallback.
// The C code (goto new_addr) falls through to memcpy the MAC at line 194.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:203-210,194
func TestBootpReplyFindAddrFallback(t *testing.T) {
	s := NewSlirp()

	// Create a DHCPREQUEST packet with no requested address
	// and no prior lease (findAddr will fail)
	bp := &BootpT{
		Op:     BootpRequest,
		Htype:  1,
		Hlen:   6,
		Xid:    0x12345678,
		Ciaddr: net.IPv4zero, // Must be explicitly zero, not nil
		Yiaddr: net.IPv4zero,
		Siaddr: net.IPv4zero,
		Giaddr: net.IPv4zero,
		// Hwaddr contains a MAC that has never been seen
	}
	mac := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	copy(bp.Hwaddr[:], mac[:])

	// Set up DHCP options: DHCPREQUEST with no requested address
	copy(bp.Vend[:4], RFC1533Cookie)
	bp.Vend[4] = RFC2132MsgType
	bp.Vend[5] = 1
	bp.Vend[6] = DHCPRequest
	bp.Vend[7] = RFC1533End

	// Call bootpReply - this should:
	// 1. dhcpDecode returns DHCPRequest, reqAddr=0.0.0.0
	// 2. Enter else branch (preqAddr == 0)
	// 3. findAddr fails (MAC never seen)
	// 4. getNewAddr succeeds (pool not exhausted)
	// 5. MAC should be copied to bc.MACAddr (the fix we're testing)
	s.bootpReply(bp)

	// Verify the MAC was copied to the BOOTP client entry
	// The first slot should have been allocated and have our MAC
	bc := &s.BootpClients[0]
	if bc.Allocated != 1 {
		t.Error("BOOTP client should be allocated")
	}
	if !bytes.Equal(bc.MACAddr[:], mac[:]) {
		t.Errorf("MAC not copied: got %v, want %v", bc.MACAddr, mac)
	}
}
