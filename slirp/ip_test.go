package slirp

import (
	"encoding/binary"
	"net"
	"testing"
)

// TestParseIP tests IP header parsing.
func TestParseIP(t *testing.T) {
	pkt := make([]byte, IPHeaderSize)
	pkt[0] = 0x45                                  // version 4, header length 5
	pkt[1] = 0x10                                  // TOS
	binary.BigEndian.PutUint16(pkt[2:4], 100)      // length
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234)   // ID
	binary.BigEndian.PutUint16(pkt[6:8], 0x4000)   // DF flag
	pkt[8] = 64                                    // TTL
	pkt[9] = IPProtoTCP                            // protocol
	binary.BigEndian.PutUint16(pkt[10:12], 0x5678) // checksum
	copy(pkt[12:16], net.IPv4(192, 168, 1, 1).To4())
	copy(pkt[16:20], net.IPv4(10, 0, 0, 1).To4())

	ip, hlen := ParseIP(pkt)
	if ip == nil {
		t.Fatal("ParseIP returned nil")
	}

	if hlen != IPHeaderSize {
		t.Errorf("hlen = %d, want %d", hlen, IPHeaderSize)
	}

	if ip.Version() != 4 {
		t.Errorf("Version = %d, want 4", ip.Version())
	}
	if ip.HeaderLen() != 5 {
		t.Errorf("HeaderLen = %d, want 5", ip.HeaderLen())
	}
	if ip.HeaderLenBytes() != 20 {
		t.Errorf("HeaderLenBytes = %d, want 20", ip.HeaderLenBytes())
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
	if ip.Sum != 0x5678 {
		t.Errorf("Sum = %x, want 0x5678", ip.Sum)
	}
}

// TestParseIPTooShort tests parsing with too short data.
func TestParseIPTooShort(t *testing.T) {
	pkt := make([]byte, 10) // too short
	ip, hlen := ParseIP(pkt)
	if ip != nil {
		t.Error("ParseIP should return nil for short data")
	}
	if hlen != 0 {
		t.Errorf("hlen = %d, want 0 for short data", hlen)
	}
}

// TestIPMarshal tests IP header marshaling.
func TestIPMarshal(t *testing.T) {
	ip := &IP{
		VersionIHL: 0x45,
		TOS:        0x10,
		Len:        100,
		ID:         0x1234,
		Off:        0x4000,
		TTL:        64,
		Proto:      IPProtoTCP,
		Sum:        0x5678,
		Src:        net.IPv4(192, 168, 1, 1),
		Dst:        net.IPv4(10, 0, 0, 1),
	}

	data := make([]byte, IPHeaderSize)
	ip.Marshal(data)

	if data[0] != 0x45 {
		t.Errorf("data[0] = %x, want 0x45", data[0])
	}
	if data[1] != 0x10 {
		t.Errorf("data[1] = %x, want 0x10", data[1])
	}
	if binary.BigEndian.Uint16(data[2:4]) != 100 {
		t.Errorf("Len = %d, want 100", binary.BigEndian.Uint16(data[2:4]))
	}
	if binary.BigEndian.Uint16(data[4:6]) != 0x1234 {
		t.Errorf("ID = %x, want 0x1234", binary.BigEndian.Uint16(data[4:6]))
	}
	if binary.BigEndian.Uint16(data[6:8]) != 0x4000 {
		t.Errorf("Off = %x, want 0x4000", binary.BigEndian.Uint16(data[6:8]))
	}
	if data[8] != 64 {
		t.Errorf("TTL = %d, want 64", data[8])
	}
	if data[9] != IPProtoTCP {
		t.Errorf("Proto = %d, want %d", data[9], IPProtoTCP)
	}
	if binary.BigEndian.Uint16(data[10:12]) != 0x5678 {
		t.Errorf("Sum = %x, want 0x5678", binary.BigEndian.Uint16(data[10:12]))
	}
}

// TestIPMarshalTooShort tests marshaling to too short buffer.
func TestIPMarshalTooShort(t *testing.T) {
	ip := &IP{
		VersionIHL: 0x45,
	}
	data := make([]byte, 10) // too short

	// Should not panic
	ip.Marshal(data)
}

// TestIPVersionMethods tests version getter/setter.
func TestIPVersionMethods(t *testing.T) {
	ip := &IP{}

	ip.SetVersion(4)
	if ip.Version() != 4 {
		t.Errorf("Version = %d, want 4", ip.Version())
	}

	ip.SetVersion(6)
	if ip.Version() != 6 {
		t.Errorf("Version = %d, want 6", ip.Version())
	}
}

// TestIPHeaderLenMethods tests header length getter/setter.
func TestIPHeaderLenMethods(t *testing.T) {
	ip := &IP{}

	ip.SetHeaderLen(5)
	if ip.HeaderLen() != 5 {
		t.Errorf("HeaderLen = %d, want 5", ip.HeaderLen())
	}
	if ip.HeaderLenBytes() != 20 {
		t.Errorf("HeaderLenBytes = %d, want 20", ip.HeaderLenBytes())
	}

	ip.SetHeaderLen(15) // maximum
	if ip.HeaderLen() != 15 {
		t.Errorf("HeaderLen = %d, want 15", ip.HeaderLen())
	}
	if ip.HeaderLenBytes() != 60 {
		t.Errorf("HeaderLenBytes = %d, want 60", ip.HeaderLenBytes())
	}
}

// TestIPFromMbuf tests IP extraction from mbuf.
func TestIPFromMbuf(t *testing.T) {
	s := NewSlirp()

	t.Run("valid mbuf", func(t *testing.T) {
		m := s.MGet()
		m.Data[0] = 0x45
		m.Data[1] = 0x10
		binary.BigEndian.PutUint16(m.Data[2:4], 100)
		m.Data[8] = 64
		m.Data[9] = IPProtoTCP
		copy(m.Data[12:16], net.IPv4(192, 168, 1, 1).To4())
		copy(m.Data[16:20], net.IPv4(10, 0, 0, 1).To4())
		m.Len = IPHeaderSize

		ip := IPFromMbuf(m)
		if ip == nil {
			t.Fatal("IPFromMbuf returned nil")
		}
		if ip.Version() != 4 {
			t.Errorf("Version = %d, want 4", ip.Version())
		}
	})

	t.Run("nil mbuf", func(t *testing.T) {
		ip := IPFromMbuf(nil)
		if ip != nil {
			t.Error("IPFromMbuf(nil) should return nil")
		}
	})

	t.Run("too short mbuf", func(t *testing.T) {
		m := s.MGet()
		m.Len = 10 // too short
		ip := IPFromMbuf(m)
		if ip != nil {
			t.Error("IPFromMbuf should return nil for short mbuf")
		}
	})
}

// TestWriteIPToMbuf tests writing IP header to mbuf.
func TestWriteIPToMbuf(t *testing.T) {
	s := NewSlirp()

	t.Run("valid write", func(t *testing.T) {
		m := s.MGet()
		ip := &IP{
			VersionIHL: 0x45,
			TOS:        0x10,
			Len:        100,
			ID:         0x1234,
			TTL:        64,
			Proto:      IPProtoTCP,
			Src:        net.IPv4(192, 168, 1, 1),
			Dst:        net.IPv4(10, 0, 0, 1),
		}

		WriteIPToMbuf(m, ip)

		if m.Data[0] != 0x45 {
			t.Errorf("data[0] = %x, want 0x45", m.Data[0])
		}
	})

	t.Run("nil mbuf", func(t *testing.T) {
		ip := &IP{VersionIHL: 0x45}
		WriteIPToMbuf(nil, ip) // Should not panic
	})
}

// TestIPConstants tests IP protocol constants.
func TestIPConstants(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{"IPVersion", IPVersion, 4},
		{"IPDF", IPDF, 0x4000},
		{"IPMF", IPMF, 0x2000},
		{"IPOffMask", IPOffMask, 0x1fff},
		{"MaxTTL", MaxTTL, 255},
		{"IPDefTTL", IPDefTTL, 64},
		{"IPMSS", IPMSS, 576},
		{"IPProtoICMP", IPProtoICMP, 1},
		{"IPProtoTCP", IPProtoTCP, 6},
		{"IPProtoUDP", IPProtoUDP, 17},
		{"IPHeaderSize", IPHeaderSize, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.value, tt.want)
			}
		})
	}
}

// TestIPStripOptions tests stripping IP options from an mbuf.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:671-685 (ip_stripoptions)
func TestIPStripOptions(t *testing.T) {
	s := NewSlirp()

	t.Run("strip 4-byte option", func(t *testing.T) {
		m := s.MGet()
		// Build IP header with options (header length = 6 = 24 bytes)
		m.Data[0] = 0x46                                // version 4, header length 6 (24 bytes)
		m.Data[1] = 0x00                                // TOS
		binary.BigEndian.PutUint16(m.Data[2:4], 44)     // total length (24 header + 20 payload)
		binary.BigEndian.PutUint16(m.Data[4:6], 0x1234) // ID
		m.Data[8] = 64                                  // TTL
		m.Data[9] = IPProtoTCP                          // protocol
		copy(m.Data[12:16], net.IPv4(192, 168, 1, 1).To4())
		copy(m.Data[16:20], net.IPv4(10, 0, 0, 1).To4())
		// 4 bytes of options (bytes 20-23)
		m.Data[20] = 0x01 // NOP option
		m.Data[21] = 0x01 // NOP option
		m.Data[22] = 0x01 // NOP option
		m.Data[23] = 0x00 // End of options
		// 20 bytes of payload starting at byte 24
		payload := []byte("12345678901234567890")
		copy(m.Data[24:44], payload)
		m.Len = 44

		IPStripOptions(m)

		// Check header length is now 5 (20 bytes)
		if m.Data[0]&0x0f != 5 {
			t.Errorf("header length = %d, want 5", m.Data[0]&0x0f)
		}

		// Check mbuf length is reduced by 4 (options length)
		if m.Len != 40 {
			t.Errorf("m.Len = %d, want 40", m.Len)
		}

		// Check payload was moved to correct position (right after 20-byte header)
		if string(m.Data[20:40]) != string(payload) {
			t.Errorf("payload = %q, want %q", string(m.Data[20:40]), string(payload))
		}
	})

	t.Run("strip 40-byte options (max)", func(t *testing.T) {
		m := s.MGet()
		// Build IP header with maximum options (header length = 15 = 60 bytes)
		m.Data[0] = 0x4f                                // version 4, header length 15 (60 bytes)
		m.Data[1] = 0x00                                // TOS
		binary.BigEndian.PutUint16(m.Data[2:4], 70)     // total length (60 header + 10 payload)
		binary.BigEndian.PutUint16(m.Data[4:6], 0x5678) // ID
		m.Data[8] = 64                                  // TTL
		m.Data[9] = IPProtoUDP                          // protocol
		copy(m.Data[12:16], net.IPv4(10, 0, 0, 1).To4())
		copy(m.Data[16:20], net.IPv4(10, 0, 0, 2).To4())
		// 40 bytes of options (bytes 20-59)
		for i := 20; i < 60; i++ {
			m.Data[i] = byte(i)
		}
		// 10 bytes of payload starting at byte 60
		payload := []byte("ABCDEFGHIJ")
		copy(m.Data[60:70], payload)
		m.Len = 70

		IPStripOptions(m)

		// Check header length is now 5 (20 bytes)
		if m.Data[0]&0x0f != 5 {
			t.Errorf("header length = %d, want 5", m.Data[0]&0x0f)
		}

		// Check mbuf length is reduced by 40 (options length)
		if m.Len != 30 {
			t.Errorf("m.Len = %d, want 30", m.Len)
		}

		// Check payload was moved to correct position
		if string(m.Data[20:30]) != string(payload) {
			t.Errorf("payload = %q, want %q", string(m.Data[20:30]), string(payload))
		}
	})

	t.Run("no options to strip", func(t *testing.T) {
		m := s.MGet()
		// Standard IP header (header length = 5 = 20 bytes)
		m.Data[0] = 0x45 // version 4, header length 5 (20 bytes)
		m.Data[1] = 0x00
		binary.BigEndian.PutUint16(m.Data[2:4], 40) // total length
		m.Data[8] = 64
		m.Data[9] = IPProtoTCP
		copy(m.Data[12:16], net.IPv4(192, 168, 1, 1).To4())
		copy(m.Data[16:20], net.IPv4(10, 0, 0, 1).To4())
		payload := []byte("12345678901234567890")
		copy(m.Data[20:40], payload)
		m.Len = 40

		IPStripOptions(m)

		// Header length should still be 5
		if m.Data[0]&0x0f != 5 {
			t.Errorf("header length = %d, want 5", m.Data[0]&0x0f)
		}

		// Length should be unchanged
		if m.Len != 40 {
			t.Errorf("m.Len = %d, want 40", m.Len)
		}

		// Payload should be unchanged
		if string(m.Data[20:40]) != string(payload) {
			t.Errorf("payload modified unexpectedly")
		}
	})

	t.Run("nil mbuf", func(t *testing.T) {
		// Should not panic
		IPStripOptions(nil)
	})

	t.Run("mbuf too short", func(t *testing.T) {
		m := s.MGet()
		m.Len = 10 // too short for IP header
		// Should not panic
		IPStripOptions(m)
	})

	t.Run("options with no payload", func(t *testing.T) {
		m := s.MGet()
		// IP header with 4-byte options but no payload
		m.Data[0] = 0x46 // version 4, header length 6 (24 bytes)
		m.Data[1] = 0x00
		binary.BigEndian.PutUint16(m.Data[2:4], 24) // total length = header only
		m.Data[8] = 64
		m.Data[9] = IPProtoICMP
		copy(m.Data[12:16], net.IPv4(192, 168, 1, 1).To4())
		copy(m.Data[16:20], net.IPv4(10, 0, 0, 1).To4())
		m.Data[20] = 0x01 // NOP
		m.Data[21] = 0x01 // NOP
		m.Data[22] = 0x01 // NOP
		m.Data[23] = 0x00 // End
		m.Len = 24

		IPStripOptions(m)

		// Header length should be 5
		if m.Data[0]&0x0f != 5 {
			t.Errorf("header length = %d, want 5", m.Data[0]&0x0f)
		}

		// Length should be 20 (just header, no options, no payload)
		if m.Len != 20 {
			t.Errorf("m.Len = %d, want 20", m.Len)
		}
	})

	t.Run("preserves other header fields", func(t *testing.T) {
		m := s.MGet()
		m.Data[0] = 0x46 // version 4, header length 6
		m.Data[1] = 0x10 // TOS
		binary.BigEndian.PutUint16(m.Data[2:4], 30)
		binary.BigEndian.PutUint16(m.Data[4:6], 0xABCD)   // ID
		binary.BigEndian.PutUint16(m.Data[6:8], 0x4000)   // flags
		m.Data[8] = 128                                   // TTL
		m.Data[9] = IPProtoUDP                            // protocol
		binary.BigEndian.PutUint16(m.Data[10:12], 0x1234) // checksum
		copy(m.Data[12:16], net.IPv4(1, 2, 3, 4).To4())
		copy(m.Data[16:20], net.IPv4(5, 6, 7, 8).To4())
		m.Data[20] = 0x01 // option
		m.Data[21] = 0x01
		m.Data[22] = 0x01
		m.Data[23] = 0x00
		m.Len = 24

		IPStripOptions(m)

		// Version should be preserved
		if m.Data[0]>>4 != 4 {
			t.Errorf("version = %d, want 4", m.Data[0]>>4)
		}

		// TOS should be preserved
		if m.Data[1] != 0x10 {
			t.Errorf("TOS = %x, want 0x10", m.Data[1])
		}

		// ID should be preserved
		if binary.BigEndian.Uint16(m.Data[4:6]) != 0xABCD {
			t.Errorf("ID = %x, want 0xABCD", binary.BigEndian.Uint16(m.Data[4:6]))
		}

		// TTL should be preserved
		if m.Data[8] != 128 {
			t.Errorf("TTL = %d, want 128", m.Data[8])
		}

		// Protocol should be preserved
		if m.Data[9] != IPProtoUDP {
			t.Errorf("protocol = %d, want %d", m.Data[9], IPProtoUDP)
		}

		// Source IP should be preserved
		if !net.IP(m.Data[12:16]).Equal(net.IPv4(1, 2, 3, 4)) {
			t.Errorf("src IP = %v, want 1.2.3.4", net.IP(m.Data[12:16]))
		}

		// Dest IP should be preserved
		if !net.IP(m.Data[16:20]).Equal(net.IPv4(5, 6, 7, 8)) {
			t.Errorf("dst IP = %v, want 5.6.7.8", net.IP(m.Data[16:20]))
		}
	})
}
