package slirp

import (
	"encoding/binary"
	"net"
	"testing"
)

// TestParseICMP tests ICMP header parsing.
func TestParseICMP(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantType uint8
		wantCode uint8
		wantID   uint16
		wantSeq  uint16
		wantNil  bool
	}{
		{
			name:     "echo request",
			data:     []byte{8, 0, 0, 0, 0, 1, 0, 2}, // type=8, code=0, id=1, seq=2
			wantType: ICMPEcho,
			wantCode: 0,
			wantID:   1,
			wantSeq:  2,
		},
		{
			name:     "echo reply",
			data:     []byte{0, 0, 0, 0, 0, 1, 0, 2},
			wantType: ICMPEchoReply,
			wantCode: 0,
			wantID:   1,
			wantSeq:  2,
		},
		{
			name:     "dest unreachable",
			data:     []byte{3, 1, 0, 0, 0, 0, 0, 0},
			wantType: ICMPUnreach,
			wantCode: ICMPUnreachHost,
		},
		{
			name:    "too short",
			data:    []byte{8, 0, 0, 0},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			icmp := ParseICMP(tt.data)
			if tt.wantNil {
				if icmp != nil {
					t.Errorf("ParseICMP() = %v, want nil", icmp)
				}
				return
			}
			if icmp == nil {
				t.Fatal("ParseICMP() = nil, want non-nil")
			}
			if icmp.Type != tt.wantType {
				t.Errorf("Type = %d, want %d", icmp.Type, tt.wantType)
			}
			if icmp.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", icmp.Code, tt.wantCode)
			}
			if icmp.ID != tt.wantID {
				t.Errorf("ID = %d, want %d", icmp.ID, tt.wantID)
			}
			if icmp.Seq != tt.wantSeq {
				t.Errorf("Seq = %d, want %d", icmp.Seq, tt.wantSeq)
			}
		})
	}
}

// TestICMPMarshal tests ICMP header marshaling.
func TestICMPMarshal(t *testing.T) {
	icmp := &ICMP{
		Type:  ICMPEcho,
		Code:  0,
		Cksum: 0x1234,
		ID:    0x5678,
		Seq:   0x9abc,
	}

	data := make([]byte, ICMPHeaderSize)
	icmp.Marshal(data)

	if data[0] != ICMPEcho {
		t.Errorf("Type = %d, want %d", data[0], ICMPEcho)
	}
	if data[1] != 0 {
		t.Errorf("Code = %d, want 0", data[1])
	}
	if binary.BigEndian.Uint16(data[2:4]) != 0x1234 {
		t.Errorf("Cksum = %x, want 0x1234", binary.BigEndian.Uint16(data[2:4]))
	}
	if binary.BigEndian.Uint16(data[4:6]) != 0x5678 {
		t.Errorf("ID = %x, want 0x5678", binary.BigEndian.Uint16(data[4:6]))
	}
	if binary.BigEndian.Uint16(data[6:8]) != 0x9abc {
		t.Errorf("Seq = %x, want 0x9abc", binary.BigEndian.Uint16(data[6:8]))
	}
}

// TestICMPConstants tests that ICMP constants have correct values.
func TestICMPConstants(t *testing.T) {
	// Reference: tinyemu-2019-12-21/slirp/ip_icmp.h
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{"ICMPEchoReply", ICMPEchoReply, 0},
		{"ICMPUnreach", ICMPUnreach, 3},
		{"ICMPSourceQuench", ICMPSourceQuench, 4},
		{"ICMPRedirect", ICMPRedirect, 5},
		{"ICMPEcho", ICMPEcho, 8},
		{"ICMPTimXceed", ICMPTimXceed, 11},
		{"ICMPParamProb", ICMPParamProb, 12},
		{"ICMPMinLen", ICMPMinLen, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.value, tt.want)
			}
		})
	}
}

// TestICMPFlush tests the icmpFlush table values.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:41-61
func TestICMPFlush(t *testing.T) {
	// Types that should NOT cause icmp_error to be sent (value 0)
	noFlush := []int{0, 8, 13, 14, 15, 16, 17, 18} // ECHO_REPLY, ECHO, timestamps, etc.
	for _, typ := range noFlush {
		if icmpFlush[typ] != 0 {
			t.Errorf("icmpFlush[%d] = %d, want 0", typ, icmpFlush[typ])
		}
	}

	// Types that SHOULD cause icmp_error to be suppressed (value 1)
	flush := []int{3, 4, 5, 11, 12} // UNREACH, SOURCE_QUENCH, REDIRECT, TIMXCEED, PARAMPROB
	for _, typ := range flush {
		if icmpFlush[typ] != 1 {
			t.Errorf("icmpFlush[%d] = %d, want 1", typ, icmpFlush[typ])
		}
	}
}

// buildEchoRequest builds an ICMP echo request packet with IP header.
func buildEchoRequest(src, dst net.IP, id, seq uint16, payload []byte) []byte {
	totalLen := IPHeaderSize + ICMPHeaderSize + len(payload)
	pkt := make([]byte, totalLen)

	// IP header
	pkt[0] = 0x45                                          // version 4, header length 5 (20 bytes)
	pkt[1] = 0                                             // TOS
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen)) // total length
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234)           // ID
	pkt[6] = 0                                             // flags + fragment offset
	pkt[7] = 0
	pkt[8] = 64          // TTL
	pkt[9] = IPProtoICMP // protocol
	// checksum at [10:12] - compute later
	copy(pkt[12:16], src.To4())
	copy(pkt[16:20], dst.To4())

	// ICMP header
	pkt[20] = ICMPEcho // type
	pkt[21] = 0        // code
	// checksum at [22:24] - compute later
	binary.BigEndian.PutUint16(pkt[24:26], id)
	binary.BigEndian.PutUint16(pkt[26:28], seq)

	// payload
	if len(payload) > 0 {
		copy(pkt[28:], payload)
	}

	// Compute IP checksum
	ipChecksum := CksumData(pkt[:IPHeaderSize])
	binary.BigEndian.PutUint16(pkt[10:12], ipChecksum)

	// Compute ICMP checksum
	icmpChecksum := CksumData(pkt[IPHeaderSize:])
	binary.BigEndian.PutUint16(pkt[22:24], icmpChecksum)

	return pkt
}

// TestICMPInputEchoToHost tests ICMP echo request to virtual host.
func TestICMPInputEchoToHost(t *testing.T) {
	s := NewSlirp()

	// Build echo request to virtual host
	src := net.IPv4(10, 0, 2, 15)
	dst := s.VHostAddr
	payload := []byte("test payload")
	pkt := buildEchoRequest(src, dst, 0x1234, 0x0001, payload)

	// Create mbuf with packet
	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Process the ICMP packet
	s.ICMPInput(m, IPHeaderSize)

	// In this simplified implementation, the packet is reflected back.
	// The IfOutput function just frees the mbuf.
	// A full test would verify the response packet.
}

// TestICMPInputTooShort tests ICMP input with too short packet.
func TestICMPInputTooShort(t *testing.T) {
	s := NewSlirp()

	// Create mbuf with too short packet
	m := s.MGet()
	m.Data[0] = 0x45 // minimal IP header start
	m.Len = 4        // way too short

	// Should not panic, just free the mbuf
	s.ICMPInput(m, IPHeaderSize)
}

// TestICMPInputBadChecksum tests ICMP input with bad checksum.
func TestICMPInputBadChecksum(t *testing.T) {
	s := NewSlirp()

	// Build echo request with bad checksum
	src := net.IPv4(10, 0, 2, 15)
	dst := s.VHostAddr
	pkt := buildEchoRequest(src, dst, 0x1234, 0x0001, nil)

	// Corrupt the ICMP checksum
	pkt[22] = 0xff
	pkt[23] = 0xff

	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Should drop the packet
	s.ICMPInput(m, IPHeaderSize)
}

// TestICMPError tests ICMP error message generation.
func TestICMPError(t *testing.T) {
	s := NewSlirp()

	// Build a sample IP packet that would cause an error
	pkt := make([]byte, IPHeaderSize+8)
	pkt[0] = 0x45 // version 4, header length 5
	pkt[1] = 0    // TOS
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = 64         // TTL
	pkt[9] = IPProtoUDP // protocol
	copy(pkt[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(pkt[16:20], net.IPv4(192, 168, 1, 1).To4())

	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Generate ICMP error (destination unreachable)
	s.ICMPError(m, ICMPUnreach, ICMPUnreachNet, 0, "test error")

	// The error packet is sent via IfOutput which just frees it.
	// A full test would capture and verify the error packet.
}

// TestICMPErrorNilMbuf tests ICMPError with nil mbuf.
func TestICMPErrorNilMbuf(t *testing.T) {
	s := NewSlirp()

	// Should not panic
	s.ICMPError(nil, ICMPUnreach, ICMPUnreachNet, 0, "")
}

// TestICMPErrorInvalidType tests ICMPError with invalid type.
func TestICMPErrorInvalidType(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	m.Data[0] = 0x45
	m.Len = IPHeaderSize + 8

	// Should return early for invalid types
	s.ICMPError(m, ICMPEcho, 0, 0, "") // ICMP_ECHO is not a valid error type
}

// TestICMPErrorSizeLimit tests that ICMP errors never exceed minimum MTU (576 bytes).
// ICMP fragmentation is illegal - all hosts must accept 576 byte packets.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:183-187
func TestICMPErrorSizeLimit(t *testing.T) {
	s := NewSlirp()
	// Set client MAC so packets are sent instead of going to ARP
	s.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	// Track output packets
	var outputPackets [][]byte
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		pktCopy := make([]byte, len(pkt))
		copy(pktCopy, pkt)
		outputPackets = append(outputPackets, pktCopy)
	}
	s.CanOutput = func(opaque interface{}) bool { return true }

	// Build a large source packet (1000 bytes) that would trigger ICMPMaxDataLen limit
	// ICMPMaxDataLen = 548, so a 1000 byte packet should be truncated
	largeSize := 1000
	pkt := make([]byte, largeSize)
	pkt[0] = 0x45 // version 4, header length 5
	pkt[1] = 0    // TOS
	binary.BigEndian.PutUint16(pkt[2:4], uint16(largeSize))
	pkt[8] = 64         // TTL
	pkt[9] = IPProtoUDP // protocol
	copy(pkt[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(pkt[16:20], net.IPv4(192, 168, 1, 1).To4())
	// Fill rest with dummy data
	for i := IPHeaderSize; i < largeSize; i++ {
		pkt[i] = byte(i)
	}

	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Generate ICMP error
	s.ICMPError(m, ICMPUnreach, ICMPUnreachNet, 0, "")

	// Verify an ICMP error was generated
	if len(outputPackets) == 0 {
		t.Fatal("Expected ICMP error packet to be generated")
	}

	// Find the ICMP error packet (skip Ethernet header which is 14 bytes)
	for _, outPkt := range outputPackets {
		if len(outPkt) < EthHLen+IPHeaderSize {
			continue
		}
		ipPkt := outPkt[EthHLen:]

		// Check IP version to ensure this is a valid IP packet
		if (ipPkt[0] >> 4) != 4 {
			continue // Not IPv4
		}

		ipLen := binary.BigEndian.Uint16(ipPkt[2:4])

		// Check if it's an ICMP packet
		if ipPkt[9] != IPProtoICMP {
			continue
		}

		// Verify the packet is at most IPMSS (576 bytes)
		if ipLen > IPMSS {
			t.Errorf("ICMP error packet size %d exceeds IPMSS (%d)", ipLen, IPMSS)
		}

		// Verify it's within IFMTU as well
		if ipLen > IFMTU {
			t.Errorf("ICMP error packet size %d exceeds IFMTU (%d)", ipLen, IFMTU)
		}

		t.Logf("ICMP error packet size: %d bytes (max allowed: %d)", ipLen, IPMSS)
		return
	}

	t.Error("No ICMP packet found in output")
}

// TestICMPErrorFragment tests ICMPError with non-zero fragment.
func TestICMPErrorFragment(t *testing.T) {
	s := NewSlirp()

	// Build packet with non-zero fragment offset
	pkt := make([]byte, IPHeaderSize+8)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[6] = 0x00 // flags
	pkt[7] = 0x10 // fragment offset = 16
	pkt[9] = IPProtoUDP
	copy(pkt[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(pkt[16:20], net.IPv4(192, 168, 1, 1).To4())

	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Should not send error for non-first fragment
	s.ICMPError(m, ICMPUnreach, ICMPUnreachNet, 0, "")
}

// TestICMPReflect tests ICMPReflect function.
func TestICMPReflect(t *testing.T) {
	s := NewSlirp()

	// Build a packet to reflect
	src := net.IPv4(10, 0, 2, 15)
	dst := s.VHostAddr
	pkt := buildEchoRequest(src, dst, 0x1234, 0x0001, []byte("hello"))

	// Change type to echo reply (simulating what ICMPInput does)
	pkt[20] = ICMPEchoReply

	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Reflect the packet
	s.ICMPReflect(m)

	// The reflected packet is sent via IfOutput which just frees it.
}

// TestICMPReflectNilMbuf tests ICMPReflect with nil mbuf.
func TestICMPReflectNilMbuf(t *testing.T) {
	s := NewSlirp()

	// Should not panic
	s.ICMPReflect(nil)
}

// TestParseIPRaw tests the IPRaw parsing function.
func TestParseIPRaw(t *testing.T) {
	pkt := make([]byte, IPHeaderSize)
	pkt[0] = 0x45                                // version 4, header length 5
	pkt[1] = 0x10                                // TOS
	binary.BigEndian.PutUint16(pkt[2:4], 100)    // length
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234) // ID
	binary.BigEndian.PutUint16(pkt[6:8], 0x4000) // DF flag
	pkt[8] = 64                                  // TTL
	pkt[9] = IPProtoTCP                          // protocol
	copy(pkt[12:16], net.IPv4(192, 168, 1, 1).To4())
	copy(pkt[16:20], net.IPv4(10, 0, 0, 1).To4())

	ip := ParseIPRaw(pkt)
	if ip == nil {
		t.Fatal("ParseIPRaw returned nil")
	}

	if ip.HL != 5 {
		t.Errorf("HL = %d, want 5", ip.HL)
	}
	if ip.TOS != 0x10 {
		t.Errorf("TOS = %x, want 0x10", ip.TOS)
	}
	if ip.Len != 100 {
		t.Errorf("Len = %d, want 100", ip.Len)
	}
	if ip.ID != 0x1234 {
		t.Errorf("ID = %x, want 0x1234", ip.ID)
	}
	if ip.Off != 0x4000 {
		t.Errorf("Off = %x, want 0x4000", ip.Off)
	}
	if ip.TTL != 64 {
		t.Errorf("TTL = %d, want 64", ip.TTL)
	}
	if ip.Proto != IPProtoTCP {
		t.Errorf("Proto = %d, want %d", ip.Proto, IPProtoTCP)
	}
	if !ip.Src.Equal(net.IPv4(192, 168, 1, 1)) {
		t.Errorf("Src = %v, want 192.168.1.1", ip.Src)
	}
	if !ip.Dst.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("Dst = %v, want 10.0.0.1", ip.Dst)
	}
}

// TestParseIPRawTooShort tests ParseIPRaw with too short data.
func TestParseIPRawTooShort(t *testing.T) {
	pkt := make([]byte, 10) // too short
	ip := ParseIPRaw(pkt)
	if ip != nil {
		t.Error("ParseIPRaw should return nil for short data")
	}
}

// TestIPOutputClearsFragmentOffset tests that IPOutput correctly clears
// the fragment offset bits while preserving the DF flag.
// This is a regression test for the bug where only byte 6 was masked,
// leaving stale fragment offset bits in byte 7.
// Reference: tinyemu-2019-12-21/slirp/ip_output.c:71
func TestIPOutputClearsFragmentOffset(t *testing.T) {
	s := NewSlirp()

	// Create an mbuf with stale fragment offset bits
	m := s.MGet()
	m.Len = IPHeaderSize + 8 // IP header + 8 bytes data

	// Set up IP header with DF flag AND some fragment offset bits
	// ip_off = DF | MF | 0x1FF (some offset)
	m.Data[0] = 0x45 // version 4, header length 5
	m.Data[1] = 0    // TOS
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(m.Len))
	binary.BigEndian.PutUint16(m.Data[4:6], 0) // ID (will be set by IPOutput)
	// Set ip_off with DF, MF, and fragment offset bits
	// IPDF = 0x4000, IPMF = 0x2000, offset = 0x01FF
	binary.BigEndian.PutUint16(m.Data[6:8], IPDF|IPMF|0x01FF)
	m.Data[8] = 64          // TTL
	m.Data[9] = IPProtoICMP // protocol
	copy(m.Data[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(m.Data[16:20], net.IPv4(10, 0, 2, 2).To4())

	// Call IPOutput
	s.IPOutput(nil, m)

	// Verify that only DF flag remains, MF and offset are cleared
	ipOff := binary.BigEndian.Uint16(m.Data[6:8])
	if ipOff != IPDF {
		t.Errorf("ip_off = 0x%04x, want 0x%04x (only DF)", ipOff, IPDF)
	}
}

// TestIPOutputFragmentation tests that IPOutput correctly fragments large packets.
// This verifies the exported IPOutput function delegates to ipOutput which has
// the full fragmentation implementation.
// Reference: tinyemu-2019-12-21/slirp/ip_output.c:92-164
func TestIPOutputFragmentation(t *testing.T) {
	s := NewSlirp()
	s.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	var sentPackets [][]byte
	s.OutputFunc = func(opaque interface{}, data []byte) {
		cp := make([]byte, len(data))
		copy(cp, data)
		sentPackets = append(sentPackets, cp)
	}

	// Create a 2000-byte packet (requires fragmentation) without DF flag
	m := s.MGet()
	totalLen := 2000
	m.MInc(totalLen)
	m.Len = totalLen

	// Set up IP header
	m.Data[0] = 0x45 // version 4, header length 5
	m.Data[1] = 0    // TOS
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(m.Data[4:6], 0)        // ID
	binary.BigEndian.PutUint16(m.Data[6:8], 0)        // flags (no DF)
	m.Data[8] = 64                                    // TTL
	m.Data[9] = IPProtoTCP                            // protocol
	copy(m.Data[12:16], net.IPv4(10, 0, 2, 15).To4()) // src
	copy(m.Data[16:20], net.IPv4(10, 0, 2, 2).To4())  // dst

	// Fill data portion with pattern
	for i := IPHeaderSize; i < totalLen; i++ {
		m.Data[i] = byte(i & 0xff)
	}

	// Call the exported IPOutput (not ipOutput)
	result := s.IPOutput(nil, m)

	if result != 0 {
		t.Errorf("IPOutput returned %d, want 0", result)
	}

	// Should produce 2 fragments
	if len(sentPackets) != 2 {
		t.Errorf("expected 2 fragments, got %d", len(sentPackets))
		return
	}

	// Verify first fragment has MF flag set
	frag1 := sentPackets[0][14:] // Skip ethernet header
	frag1Off := binary.BigEndian.Uint16(frag1[6:8])
	if frag1Off&IPMF == 0 {
		t.Error("first fragment should have MF flag set")
	}
	if frag1Off&IPOffMask != 0 {
		t.Error("first fragment should have offset 0")
	}

	// Verify second fragment has correct offset
	frag2 := sentPackets[1][14:] // Skip ethernet header
	frag2Off := binary.BigEndian.Uint16(frag2[6:8])
	expectedOffset := (IFMTU - IPHeaderSize) / 8 // 1480 / 8 = 185
	if frag2Off&IPOffMask != uint16(expectedOffset) {
		t.Errorf("second fragment offset = %d, want %d", frag2Off&IPOffMask, expectedOffset)
	}
}

// TestIPOutputDFSetReturnsError tests that IPOutput returns -1 when packet > MTU
// and DF flag is set.
// Reference: tinyemu-2019-12-21/slirp/ip_output.c:92-95
func TestIPOutputDFSetReturnsError(t *testing.T) {
	s := NewSlirp()

	var packetSent bool
	s.OutputFunc = func(opaque interface{}, data []byte) {
		packetSent = true
	}

	// Create a 2000-byte packet with DF flag set
	m := s.MGet()
	totalLen := 2000
	m.MInc(totalLen)
	m.Len = totalLen

	m.Data[0] = 0x45
	m.Data[1] = 0
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(m.Data[6:8], IPDF) // DF flag set
	m.Data[8] = 64
	m.Data[9] = IPProtoTCP
	copy(m.Data[12:16], net.IPv4(10, 0, 2, 15).To4())
	copy(m.Data[16:20], net.IPv4(10, 0, 2, 2).To4())

	result := s.IPOutput(nil, m)

	if result != -1 {
		t.Errorf("IPOutput returned %d, want -1 (DF set, can't fragment)", result)
	}

	if packetSent {
		t.Error("no packet should be sent when DF is set and packet exceeds MTU")
	}
}

// TestICMPInputEchoToExternalHost tests ICMP echo forwarding to external hosts.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:105-146
func TestICMPInputEchoToExternalHost(t *testing.T) {
	s := NewSlirp()

	// Build echo request to external host (not vhost)
	src := net.IPv4(10, 0, 2, 15)
	dst := net.IPv4(8, 8, 8, 8) // External IP (Google DNS)
	payload := []byte("test payload")
	pkt := buildEchoRequest(src, dst, 0x1234, 0x0001, payload)

	// Create mbuf with packet
	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Process the ICMP packet
	s.ICMPInput(m, IPHeaderSize)

	// Verify that a UDP socket was created for the ping
	// The socket should be in the UDB list with SoType == IPProtoICMP
	found := false
	for so := s.UDB.Next; so != &s.UDB; so = so.Next {
		if so.SoType == IPProtoICMP {
			found = true
			// Verify socket fields
			if !so.SoFAddr.Equal(dst) {
				t.Errorf("SoFAddr = %v, want %v", so.SoFAddr, dst)
			}
			if so.SoFPort != 7 {
				t.Errorf("SoFPort = %d, want 7 (echo port)", so.SoFPort)
			}
			if !so.SoLAddr.Equal(src) {
				t.Errorf("SoLAddr = %v, want %v", so.SoLAddr, src)
			}
			if so.SoM == nil {
				t.Error("SoM should hold the original mbuf")
			}
			// Clean up
			s.UDPDetach(so)
			break
		}
	}

	if !found {
		t.Error("Expected ICMP socket to be created in UDB list")
	}
}

// TestICMPInputEchoToAliasHost tests ICMP echo forwarding to virtual network alias.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:126-137
func TestICMPInputEchoToAliasHost(t *testing.T) {
	s := NewSlirp()

	// Build echo request to an alias address (e.g., DNS server alias)
	src := net.IPv4(10, 0, 2, 15)
	dst := s.VNameserverAddr // DNS server alias
	payload := []byte("test")
	pkt := buildEchoRequest(src, dst, 0x5678, 0x0002, payload)

	// Create mbuf with packet
	m := s.MGet()
	copy(m.Data, pkt)
	m.Len = len(pkt)

	// Process the ICMP packet
	s.ICMPInput(m, IPHeaderSize)

	// Verify that a UDP socket was created
	// The destination should be resolved (either to real DNS or loopback)
	found := false
	for so := s.UDB.Next; so != &s.UDB; so = so.Next {
		if so.SoType == IPProtoICMP {
			found = true
			// The SoFAddr should still be the original alias address
			if !so.SoFAddr.Equal(dst) {
				t.Errorf("SoFAddr = %v, want %v", so.SoFAddr, dst)
			}
			// Clean up
			s.UDPDetach(so)
			break
		}
	}

	if !found {
		t.Error("Expected ICMP socket to be created for alias address")
	}
}
