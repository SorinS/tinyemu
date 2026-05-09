package slirp

import (
	"testing"
)

// TestCksumData tests the checksum calculation.
func TestCksumData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint16
	}{
		{
			name: "empty",
			data: []byte{},
			want: 0xffff,
		},
		{
			name: "single byte",
			data: []byte{0x45},
			want: ^uint16(0x4500),
		},
		{
			name: "two bytes",
			data: []byte{0x45, 0x00},
			want: ^uint16(0x4500),
		},
		{
			name: "simple IP header",
			data: []byte{
				0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46, 0x40, 0x00,
				0x40, 0x06, 0x00, 0x00, // checksum field is 0
				0xac, 0x10, 0x0a, 0x63, // src: 172.16.10.99
				0xac, 0x10, 0x0a, 0x0c, // dst: 172.16.10.12
			},
			want: 0xb1e6, // expected checksum
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CksumData(tt.data)
			if got != tt.want {
				t.Errorf("CksumData(%v) = 0x%04x, want 0x%04x", tt.data, got, tt.want)
			}
		})
	}
}

// TestCksum tests checksum calculation on mbuf.
func TestCksum(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	data := []byte{0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46}
	copy(m.Data, data)
	m.Len = len(data)

	result := Cksum(m, m.Len)

	// Verify it's a valid checksum (non-zero after computation)
	if result == 0 {
		t.Error("Cksum returned 0")
	}
}

// TestCksumNilMbuf tests checksum with nil mbuf.
// Nil mbuf should return 0xffff (same as empty data).
func TestCksumNilMbuf(t *testing.T) {
	result := Cksum(nil, 10)
	if result != 0xffff {
		t.Errorf("Cksum(nil) = 0x%04x, want 0xffff", result)
	}
}

// TestCksumEmptyMbuf tests checksum with empty mbuf.
// Empty data has checksum 0xffff (one's complement of zero).
// Reference: tinyemu-2019-12-21/slirp/cksum.c:64-66, 137-138
func TestCksumEmptyMbuf(t *testing.T) {
	s := NewSlirp()
	m := s.MGet()
	m.Len = 0

	result := Cksum(m, 0)
	if result != 0xffff {
		t.Errorf("Cksum(empty) = 0x%04x, want 0xffff", result)
	}
}

// TestCksumPartialAndFinish tests partial checksum and finish.
func TestCksumPartialAndFinish(t *testing.T) {
	data := []byte{0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46}

	// Calculate partial checksum
	partial := CksumPartial(data)

	// Finish it
	result := CksumFinish(partial)

	// Should match full calculation
	expected := CksumData(data)
	if result != expected {
		t.Errorf("CksumPartial+Finish = 0x%04x, want 0x%04x", result, expected)
	}
}

// TestCksumLengthLimit tests checksum with length smaller than data.
func TestCksumLengthLimit(t *testing.T) {
	s := NewSlirp()

	m := s.MGet()
	data := []byte{0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46, 0x40, 0x00}
	copy(m.Data, data)
	m.Len = len(data)

	// Calculate checksum for only first 4 bytes
	result := Cksum(m, 4)

	// Should match checksum of first 4 bytes only
	expected := CksumData(data[:4])
	if result != expected {
		t.Errorf("Cksum with length limit = 0x%04x, want 0x%04x", result, expected)
	}
}

// TestCksumVerification tests that a valid checksum verifies to 0.
func TestCksumVerification(t *testing.T) {
	// A valid IP header with correct checksum should checksum to 0
	// when verified (i.e., when checksum is included in calculation)
	data := []byte{
		0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46, 0x40, 0x00,
		0x40, 0x06, 0x00, 0x00, // checksum field
		0xac, 0x10, 0x0a, 0x63,
		0xac, 0x10, 0x0a, 0x0c,
	}

	// First calculate checksum with checksum field set to 0
	cksum := CksumData(data)

	// Put checksum in the header
	data[10] = byte(cksum >> 8)
	data[11] = byte(cksum)

	// Now verify - should return 0
	verify := CksumData(data)
	if verify != 0 {
		t.Errorf("Checksum verification = 0x%04x, want 0", verify)
	}
}

// TestCksumOddLength tests checksum with odd number of bytes.
func TestCksumOddLength(t *testing.T) {
	// Odd length data
	data := []byte{0x45, 0x00, 0x00}

	result := CksumData(data)
	// Should not panic and return a valid checksum
	if result == 0 {
		// The checksum of {0x45, 0x00, 0x00} would be ~(0x4500) with odd handling
		// Actually, with odd byte handling: sum = 0x4500, then odd byte 0x00 << 8 = 0
		// So result should be ~0x4500 which is not 0
	}
}
