package slirp

import (
	"encoding/binary"
	"net"
)

// IP constants
// Reference: tinyemu-2019-12-21/slirp/ip.h
const (
	IPVersion = 4

	// Fragment offset field flags
	IPDF      = 0x4000 // don't fragment flag
	IPMF      = 0x2000 // more fragments flag
	IPOffMask = 0x1fff // mask for fragmenting bits

	// Maximum packet size
	IPMaxPacket = 65535

	// IP type of service
	IPTOSLowDelay    = 0x10
	IPTOSThroughput  = 0x08
	IPTOSReliability = 0x04

	// Time to live
	MaxTTL    = 255 // maximum time to live (seconds)
	IPDefTTL  = 64  // default ttl, from RFC 1340
	IPFragTTL = 60  // time to live for frags, slowhz
	IPTTLDEC  = 1   // subtracted when forwarding

	// Default maximum segment size
	IPMSS = 576

	// Protocol numbers
	IPProtoICMP = 1
	IPProtoTCP  = 6
	IPProtoUDP  = 17
)

// IP represents an IPv4 header.
// Reference: tinyemu-2019-12-21/slirp/ip.h:75-94
type IP struct {
	VersionIHL uint8  // version (4 bits) + header length (4 bits)
	TOS        uint8  // type of service
	Len        uint16 // total length
	ID         uint16 // identification
	Off        uint16 // fragment offset field
	TTL        uint8  // time to live
	Proto      uint8  // protocol
	Sum        uint16 // checksum
	Src        net.IP // source address (4 bytes for IPv4)
	Dst        net.IP // destination address (4 bytes for IPv4)
}

// IPHeaderSize is the size of an IP header without options (20 bytes).
const IPHeaderSize = 20

// Version returns the IP version (should be 4).
func (ip *IP) Version() uint8 {
	return ip.VersionIHL >> 4
}

// SetVersion sets the IP version.
func (ip *IP) SetVersion(v uint8) {
	ip.VersionIHL = (ip.VersionIHL & 0x0f) | (v << 4)
}

// HeaderLen returns the header length in 32-bit words.
func (ip *IP) HeaderLen() uint8 {
	return ip.VersionIHL & 0x0f
}

// SetHeaderLen sets the header length in 32-bit words.
func (ip *IP) SetHeaderLen(hl uint8) {
	ip.VersionIHL = (ip.VersionIHL & 0xf0) | (hl & 0x0f)
}

// HeaderLenBytes returns the header length in bytes.
func (ip *IP) HeaderLenBytes() int {
	return int(ip.HeaderLen()) << 2
}

// ParseIP parses an IP header from a byte slice.
// Returns the IP header and the number of bytes consumed.
func ParseIP(data []byte) (*IP, int) {
	if len(data) < IPHeaderSize {
		return nil, 0
	}

	ip := &IP{
		VersionIHL: data[0],
		TOS:        data[1],
		Len:        binary.BigEndian.Uint16(data[2:4]),
		ID:         binary.BigEndian.Uint16(data[4:6]),
		Off:        binary.BigEndian.Uint16(data[6:8]),
		TTL:        data[8],
		Proto:      data[9],
		Sum:        binary.BigEndian.Uint16(data[10:12]),
		Src:        net.IP(data[12:16]),
		Dst:        net.IP(data[16:20]),
	}

	return ip, ip.HeaderLenBytes()
}

// Marshal writes the IP header to a byte slice.
// The slice must be at least IPHeaderSize bytes.
func (ip *IP) Marshal(data []byte) {
	if len(data) < IPHeaderSize {
		return
	}

	data[0] = ip.VersionIHL
	data[1] = ip.TOS
	binary.BigEndian.PutUint16(data[2:4], ip.Len)
	binary.BigEndian.PutUint16(data[4:6], ip.ID)
	binary.BigEndian.PutUint16(data[6:8], ip.Off)
	data[8] = ip.TTL
	data[9] = ip.Proto
	binary.BigEndian.PutUint16(data[10:12], ip.Sum)
	copy(data[12:16], ip.Src.To4())
	copy(data[16:20], ip.Dst.To4())
}

// IPFromMbuf interprets the mbuf data as an IP header.
// This is equivalent to mtod(m, struct ip *) in C.
// Reference: tinyemu-2019-12-21/slirp/mbuf.h:45
func IPFromMbuf(m *Mbuf) *IP {
	if m == nil || m.Len < IPHeaderSize {
		return nil
	}
	ip, _ := ParseIP(m.Data[:m.Len])
	return ip
}

// WriteIPToMbuf writes the IP header to the beginning of the mbuf.
func WriteIPToMbuf(m *Mbuf, ip *IP) {
	if m == nil || len(m.Data) < IPHeaderSize {
		return
	}
	ip.Marshal(m.Data)
}

// IPStripOptions strips IP options from an mbuf, leaving only the standard
// 20-byte IP header. The data following the options is moved up to fill
// the gap.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:671-685 (ip_stripoptions)
func IPStripOptions(m *Mbuf) {
	if m == nil || m.Len < IPHeaderSize {
		return
	}

	// Get header length from the IP header
	hl := int(m.Data[0]&0x0f) << 2 // ip_hl in 32-bit words, convert to bytes

	// Calculate options length
	olen := hl - IPHeaderSize
	if olen <= 0 {
		return // No options to strip
	}

	// Calculate remaining data length after options
	// i = m->m_len - (sizeof(struct ip) + olen)
	i := m.Len - (IPHeaderSize + olen)

	// Move data after options to where options were
	// memcpy(opts, opts + olen, (unsigned)i)
	// opts = (caddr_t)(ip + 1) which is right after the IP header
	if i > 0 {
		copy(m.Data[IPHeaderSize:], m.Data[IPHeaderSize+olen:IPHeaderSize+olen+i])
	}

	// Reduce mbuf length
	m.Len -= olen

	// Set header length to 5 (20 bytes, no options)
	// ip->ip_hl = sizeof(struct ip) >> 2
	m.Data[0] = (m.Data[0] & 0xf0) | (IPHeaderSize >> 2)
}
