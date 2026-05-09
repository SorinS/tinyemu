package slirp

// Cksum computes the Internet checksum for the data in mbuf m.
// Returns 0xffff for nil/empty mbufs (checksum of no data is all ones).
// Reference: tinyemu-2019-12-21/slirp/cksum.c:48-139
func Cksum(m *Mbuf, length int) uint16 {
	if m == nil || m.Len == 0 {
		// Match C behavior: empty data returns ~0 & 0xffff = 0xffff
		return 0xffff
	}

	data := m.Data[:m.Len]
	if length < len(data) {
		data = data[:length]
	}

	return CksumData(data)
}

// CksumData computes the Internet checksum for the given data.
// This implements the one's complement sum algorithm from RFC 1071.
//
// The C version has byte_swapped handling because it casts pointers to read
// 16-bit words directly from memory (e.g., `sum += w[0]` where w is uint16_t*).
// On odd-aligned addresses, this causes shifted byte reads. Go doesn't need
// this because we explicitly read bytes and construct 16-bit values:
// `sum += uint32(data[i])<<8 | uint32(data[i+1])` - always consistent.
//
// Reference: tinyemu-2019-12-21/slirp/cksum.c:48-139
func CksumData(data []byte) uint16 {
	var sum uint32
	length := len(data)
	i := 0

	// Main loop - process 16-bit words (big-endian, network byte order)
	for length >= 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
		i += 2
		length -= 2
	}

	// Handle odd trailing byte
	if length > 0 {
		sum += uint32(data[i]) << 8
	}

	// Fold 32-bit sum to 16 bits
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	return ^uint16(sum)
}

// CksumPartial computes a partial checksum (without final complement).
// Useful for combining checksums of different parts.
func CksumPartial(data []byte) uint32 {
	var sum uint32
	length := len(data)
	i := 0

	for length >= 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
		i += 2
		length -= 2
	}

	if length > 0 {
		sum += uint32(data[i]) << 8
	}

	return sum
}

// CksumFinish folds and complements a partial checksum.
func CksumFinish(sum uint32) uint16 {
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
