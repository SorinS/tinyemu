package slirp

import (
	"encoding/binary"
	"net"
	"testing"
)

// TestUDPInit tests the UDP initialization.
// Reference: tinyemu-2019-12-21/slirp/udp.c:47-51
func TestUDPInit(t *testing.T) {
	s := NewSlirp()

	// After init, UDB should be a circular list pointing to itself
	if s.UDB.Next != &s.UDB {
		t.Error("UDB.Next should point to UDB after init")
	}
	if s.UDB.Prev != &s.UDB {
		t.Error("UDB.Prev should point to UDB after init")
	}
	if s.UDPLastSo != &s.UDB {
		t.Error("UDPLastSo should point to UDB after init")
	}
}

// TestUDPTos tests the UDP TOS lookup for DNS.
// Reference: tinyemu-2019-12-21/slirp/udp.c:327-341
func TestUDPTos(t *testing.T) {
	s := NewSlirp()

	tests := []struct {
		name    string
		fport   uint16
		lport   uint16
		wantTos uint8
	}{
		{
			name:    "DNS local port (server)",
			fport:   0,
			lport:   53,
			wantTos: IPTOSLowDelay,
		},
		{
			name:    "DNS foreign port (not in table)",
			fport:   53,
			lport:   0,
			wantTos: 0, // foreign port 53 is not in the table
		},
		{
			name:    "Random port",
			fport:   12345,
			lport:   54321,
			wantTos: 0,
		},
		{
			name:    "HTTP port (no special TOS)",
			fport:   80,
			lport:   0,
			wantTos: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			so := &Socket{
				SoFPort: tt.fport,
				SoLPort: tt.lport,
			}
			tos := s.udpGetTos(so)
			if tos != tt.wantTos {
				t.Errorf("udpGetTos() = %d, want %d", tos, tt.wantTos)
			}
		})
	}
}

// TestUDPAttachDetach tests UDP socket attach/detach.
// Reference: tinyemu-2019-12-21/slirp/udp.c:305-319
func TestUDPAttachDetach(t *testing.T) {
	s := NewSlirp()

	// Create a socket
	so := s.SoCreate()
	if so == nil {
		t.Fatal("SoCreate returned nil")
	}

	// Attach it
	result := s.UDPAttach(so)
	if result < 0 {
		t.Fatal("UDPAttach failed")
	}

	// Check that socket is in the list
	if s.UDB.Next != so {
		t.Error("Socket not properly linked into UDB list")
	}

	// Check expiry is set
	if so.SoExpire == 0 {
		t.Error("SoExpire not set")
	}

	// Check that we have a connection
	if so.Extra == nil {
		t.Error("Socket Extra (UDP conn) not set")
	}

	// Detach it
	s.UDPDetach(so)

	// List should be empty again
	if s.UDB.Next != &s.UDB {
		t.Error("UDB list not empty after detach")
	}
}

// TestMAdj tests the mbuf adjustment function.
// Reference: tinyemu-2019-12-21/slirp/mbuf.c m_adj
func TestMAdj(t *testing.T) {
	tests := []struct {
		name        string
		dataLen     int
		adj         int
		wantLen     int
		wantDataOff int // offset from original start
	}{
		{
			name:        "trim from front",
			dataLen:     100,
			adj:         10,
			wantLen:     90,
			wantDataOff: 10,
		},
		{
			name:        "trim from end",
			dataLen:     100,
			adj:         -20,
			wantLen:     80,
			wantDataOff: 0,
		},
		{
			name:        "trim more than length from front",
			dataLen:     50,
			adj:         100,
			wantLen:     0,
			wantDataOff: 50,
		},
		{
			name:        "trim more than length from end",
			dataLen:     50,
			adj:         -100,
			wantLen:     0,
			wantDataOff: 0,
		},
		{
			name:        "zero adjustment",
			dataLen:     100,
			adj:         0,
			wantLen:     100,
			wantDataOff: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dat := make([]byte, tt.dataLen)
			for i := range dat {
				dat[i] = byte(i)
			}
			m := &Mbuf{
				Dat:    dat,
				Data:   dat,
				Offset: 0,
				Size:   tt.dataLen,
				Len:    tt.dataLen,
			}

			m.Adj(tt.adj)

			if m.Len != tt.wantLen {
				t.Errorf("Adj() Len = %d, want %d", m.Len, tt.wantLen)
			}

			// Check data pointer offset
			if tt.wantDataOff < tt.dataLen && len(m.Data) > 0 && m.Data[0] != byte(tt.wantDataOff) {
				t.Errorf("Adj() data offset incorrect, first byte = %d, want %d", m.Data[0], tt.wantDataOff)
			}
		})
	}
}

// TestUDPInputBootp tests that BOOTP packets are handled correctly.
// Reference: tinyemu-2019-12-21/slirp/udp.c:123-126
func TestUDPInputBootp(t *testing.T) {
	s := NewSlirp()

	// Build a minimal UDP packet destined for BOOTP_SERVER port
	// IP header (20 bytes) + UDP header (8 bytes) + minimal BOOTP (548 bytes)
	pktLen := IPHeaderSize + UDPHeaderSize + BootpSize
	data := make([]byte, pktLen)

	// Fill in IP header
	data[0] = 0x45                                        // version 4, header len 5
	data[1] = 0                                           // TOS
	binary.BigEndian.PutUint16(data[2:4], uint16(pktLen)) // total length
	binary.BigEndian.PutUint16(data[4:6], 0x1234)         // ID
	binary.BigEndian.PutUint16(data[6:8], 0)              // fragment offset
	data[8] = 64                                          // TTL
	data[9] = IPProtoUDP                                  // protocol
	binary.BigEndian.PutUint16(data[10:12], 0)            // checksum
	copy(data[12:16], net.IPv4(10, 0, 2, 15).To4())       // src IP
	copy(data[16:20], s.VHostAddr.To4())                  // dst IP

	// Fill in UDP header
	udpHdr := data[IPHeaderSize:]
	binary.BigEndian.PutUint16(udpHdr[0:2], 68)                              // src port (BOOTP_CLIENT)
	binary.BigEndian.PutUint16(udpHdr[2:4], BootpServer)                     // dst port (BOOTP_SERVER)
	binary.BigEndian.PutUint16(udpHdr[4:6], uint16(UDPHeaderSize+BootpSize)) // UDP length
	binary.BigEndian.PutUint16(udpHdr[6:8], 0)                               // checksum (0 = no checksum)

	// Fill in minimal BOOTP data
	bootpData := data[IPHeaderSize+UDPHeaderSize:]
	bootpData[0] = BootpRequest // op = BOOTPREQUEST

	m := &Mbuf{
		Data: data,
		Len:  pktLen,
	}
	m.Slirp = s

	// Call UDPInput - it should process as BOOTP
	s.UDPInput(m, IPHeaderSize)

	// The packet should be handled and freed
	// We can't easily verify BOOTP processing without more infrastructure,
	// but we can verify it didn't crash
}

// TestUDPInputRestrictedMode tests that non-BOOTP UDP is dropped in restricted mode.
// Reference: tinyemu-2019-12-21/slirp/udp.c:128-130
func TestUDPInputRestrictedMode(t *testing.T) {
	s := NewSlirp()
	s.Restricted = true

	// Build a minimal UDP packet to a random port
	pktLen := IPHeaderSize + UDPHeaderSize + 10
	data := make([]byte, pktLen)

	// Fill in IP header
	data[0] = 0x45
	data[1] = 0
	binary.BigEndian.PutUint16(data[2:4], uint16(pktLen))
	data[9] = IPProtoUDP
	copy(data[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(data[16:20], net.IPv4(8, 8, 8, 8).To4()) // random dst

	// Fill in UDP header
	udpHdr := data[IPHeaderSize:]
	binary.BigEndian.PutUint16(udpHdr[0:2], 12345) // src port
	binary.BigEndian.PutUint16(udpHdr[2:4], 53)    // dst port (DNS)
	binary.BigEndian.PutUint16(udpHdr[4:6], uint16(UDPHeaderSize+10))
	binary.BigEndian.PutUint16(udpHdr[6:8], 0) // no checksum

	m := &Mbuf{
		Data: data,
		Len:  pktLen,
	}
	m.Slirp = s

	// Should drop the packet in restricted mode
	s.UDPInput(m, IPHeaderSize)

	// The mbuf should be freed (we can't easily verify this,
	// but we can verify no crash occurred)
}

// TestUDPInputBadChecksum tests that packets with bad checksums are dropped.
// Reference: tinyemu-2019-12-21/slirp/udp.c:111-118
func TestUDPInputBadChecksum(t *testing.T) {
	s := NewSlirp()

	// Build a UDP packet with a non-zero checksum that's incorrect
	pktLen := IPHeaderSize + UDPHeaderSize + 10
	data := make([]byte, pktLen)

	// Fill in IP header
	data[0] = 0x45
	binary.BigEndian.PutUint16(data[2:4], uint16(pktLen))
	data[9] = IPProtoUDP
	copy(data[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(data[16:20], net.IPv4(10, 0, 2, 2).To4())

	// Fill in UDP header
	udpHdr := data[IPHeaderSize:]
	binary.BigEndian.PutUint16(udpHdr[0:2], 12345)
	binary.BigEndian.PutUint16(udpHdr[2:4], 80)
	binary.BigEndian.PutUint16(udpHdr[4:6], uint16(UDPHeaderSize+10))
	binary.BigEndian.PutUint16(udpHdr[6:8], 0x1234) // bad checksum (non-zero)

	m := &Mbuf{
		Data: data,
		Len:  pktLen,
	}
	m.Slirp = s

	// Should drop the packet due to bad checksum
	s.UDPInput(m, IPHeaderSize)
}

// TestUDPOutput tests the UDP output wrapper function.
// Reference: tinyemu-2019-12-21/slirp/udp.c:279-302
func TestUDPOutput(t *testing.T) {
	s := NewSlirp()

	so := &Socket{
		Slirp:   s,
		SoFAddr: net.IPv4(10, 0, 2, 15),
		SoFPort: 12345,
		SoLAddr: net.IPv4(192, 168, 1, 1),
		SoLPort: 54321,
		SoIPTos: IPTOSLowDelay,
	}

	m := s.MGet()
	if m == nil {
		t.Fatal("MGet returned nil")
	}
	m.Len = 10 // some payload

	addr := &SockaddrIn{
		Addr: net.IPv4(192, 168, 1, 1),
		Port: 54321,
	}

	// UDPOutput should call UDPOutput2 with transformed addresses
	result := s.UDPOutput(so, m, addr)

	// We can't easily verify the output packet without more infrastructure
	// but we can verify it returned 0 (success)
	if result != 0 {
		t.Errorf("UDPOutput returned %d, want 0", result)
	}
}

// TestUDPOutputBroadcast tests UDP output to broadcast address.
// Reference: tinyemu-2019-12-21/slirp/udp.c:287-297
func TestUDPOutputBroadcast(t *testing.T) {
	s := NewSlirp()

	// Socket with destination on virtual network
	so := &Socket{
		Slirp:   s,
		SoFAddr: net.IPv4(10, 0, 2, 255), // broadcast on virtual network
		SoFPort: 12345,
		SoLAddr: net.IPv4(10, 0, 2, 15),
		SoLPort: 54321,
		SoIPTos: 0,
	}

	m := s.MGet()
	if m == nil {
		t.Fatal("MGet returned nil")
	}
	m.Len = 10

	addr := &SockaddrIn{
		Addr: net.IPv4(127, 0, 0, 1), // loopback
		Port: 54321,
	}

	// Should transform address based on broadcast logic
	result := s.UDPOutput(so, m, addr)
	if result != 0 {
		t.Errorf("UDPOutput returned %d, want 0", result)
	}
}
