package slirp

import (
	"bytes"
	"encoding/binary"
	"net"
)

// BOOTP/DHCP constants
// Reference: tinyemu-2019-12-21/slirp/bootp.h:1-93
const (
	// Port numbers
	BootpServer = 67
	BootpClient = 68

	// BOOTP operation codes
	BootpRequest = 1
	BootpReply   = 2

	// RFC 1533 options
	RFC1533Pad      = 0
	RFC1533Netmask  = 1
	RFC1533Gateway  = 3
	RFC1533DNS      = 6
	RFC1533Hostname = 12
	RFC1533End      = 255

	// RFC 2132 options
	RFC2132ReqAddr   = 50
	RFC2132LeaseTime = 51
	RFC2132MsgType   = 53
	RFC2132SrvID     = 54
	RFC2132Message   = 56

	// DHCP message types
	DHCPDiscover = 1
	DHCPOffer    = 2
	DHCPRequest  = 3
	DHCPAck      = 5
	DHCPNak      = 6

	// Sizes
	BootpVendorLen = 64
	DHCPOptLen     = 312

	// Number of BOOTP clients
	NBBootpClients = 16

	// Lease time in seconds (24 hours)
	LeaseTime = 24 * 3600
)

// RFC1533Cookie is the magic cookie for DHCP options
// Reference: tinyemu-2019-12-21/slirp/bootp.h:9
var RFC1533Cookie = []byte{99, 130, 83, 99}

// BOOTPClient represents a BOOTP/DHCP client entry.
// Reference: tinyemu-2019-12-21/slirp/bootp.h:115-118
type BOOTPClient struct {
	Allocated uint16
	MACAddr   [6]byte
}

// BootpT represents the BOOTP packet structure.
// This structure overlays the mbuf data following the IP and UDP headers.
// Reference: tinyemu-2019-12-21/slirp/bootp.h:95-113
type BootpT struct {
	// IP and UDP headers are handled separately
	Op     uint8     // packet opcode type
	Htype  uint8     // hardware addr type
	Hlen   uint8     // hardware addr length
	Hops   uint8     // gateway hops
	Xid    uint32    // transaction ID
	Secs   uint16    // seconds since boot began
	Unused uint16    // unused
	Ciaddr net.IP    // client IP address
	Yiaddr net.IP    // 'your' IP address
	Siaddr net.IP    // server IP address
	Giaddr net.IP    // gateway IP address
	Hwaddr [16]byte  // client hardware address
	Sname  [64]byte  // server host name
	File   [128]byte // boot file name
	Vend   [312]byte // vendor-specific area (DHCP options)
}

// BootpSize is the size of the BOOTP packet structure (without IP/UDP headers)
const BootpSize = 1 + 1 + 1 + 1 + 4 + 2 + 2 + 4 + 4 + 4 + 4 + 16 + 64 + 128 + 312 // 548 bytes

// BootpMinSize is the minimum BOOTP packet size we accept for input.
// This is the fixed BOOTP header (236 bytes) + DHCP magic cookie (4 bytes).
// DHCP clients may send shorter packets than the full 548-byte BOOTP structure.
const BootpMinSize = 1 + 1 + 1 + 1 + 4 + 2 + 2 + 4 + 4 + 4 + 4 + 16 + 64 + 128 + 4 // 240 bytes

// ParseBootp parses a BOOTP packet from mbuf data.
// The data should start at the BOOTP header (after IP and UDP headers).
// Accepts variable-length packets - DHCP clients may send shorter packets.
// Reference: tinyemu-2019-12-21/slirp/bootp.c
func ParseBootp(data []byte) *BootpT {
	// Need at least the minimum BOOTP size (fixed header + magic cookie)
	if len(data) < BootpMinSize {
		return nil
	}

	bp := &BootpT{
		Op:     data[0],
		Htype:  data[1],
		Hlen:   data[2],
		Hops:   data[3],
		Xid:    binary.BigEndian.Uint32(data[4:8]),
		Secs:   binary.BigEndian.Uint16(data[8:10]),
		Unused: binary.BigEndian.Uint16(data[10:12]),
		Ciaddr: net.IP(data[12:16]).To4(),
		Yiaddr: net.IP(data[16:20]).To4(),
		Siaddr: net.IP(data[20:24]).To4(),
		Giaddr: net.IP(data[24:28]).To4(),
	}
	copy(bp.Hwaddr[:], data[28:44])
	copy(bp.Sname[:], data[44:108])
	copy(bp.File[:], data[108:236])
	// Copy available vend (options) data - packets may be shorter than 548 bytes
	vendLen := len(data) - 236
	if vendLen > 312 {
		vendLen = 312 // cap at vend array size
	}
	if vendLen > 0 {
		copy(bp.Vend[:vendLen], data[236:236+vendLen])
	}

	return bp
}

// Marshal writes the BOOTP packet to a byte slice.
func (bp *BootpT) Marshal(data []byte) {
	if len(data) < BootpSize {
		return
	}

	data[0] = bp.Op
	data[1] = bp.Htype
	data[2] = bp.Hlen
	data[3] = bp.Hops
	binary.BigEndian.PutUint32(data[4:8], bp.Xid)
	binary.BigEndian.PutUint16(data[8:10], bp.Secs)
	binary.BigEndian.PutUint16(data[10:12], bp.Unused)

	if bp.Ciaddr != nil {
		copy(data[12:16], bp.Ciaddr.To4())
	} else {
		copy(data[12:16], net.IPv4zero.To4())
	}
	if bp.Yiaddr != nil {
		copy(data[16:20], bp.Yiaddr.To4())
	} else {
		copy(data[16:20], net.IPv4zero.To4())
	}
	if bp.Siaddr != nil {
		copy(data[20:24], bp.Siaddr.To4())
	} else {
		copy(data[20:24], net.IPv4zero.To4())
	}
	if bp.Giaddr != nil {
		copy(data[24:28], bp.Giaddr.To4())
	} else {
		copy(data[24:28], net.IPv4zero.To4())
	}

	copy(data[28:44], bp.Hwaddr[:])
	copy(data[44:108], bp.Sname[:])
	copy(data[108:236], bp.File[:])
	copy(data[236:548], bp.Vend[:])
}

// SockaddrIn represents a socket address for UDP output.
// Reference: tinyemu-2019-12-21/slirp/slirp.h struct sockaddr_in
type SockaddrIn struct {
	Addr net.IP
	Port uint16
}

// getNewAddr finds or allocates a new DHCP address for a client.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:39-56
func (s *Slirp) getNewAddr(macaddr [6]byte) (*BOOTPClient, net.IP) {
	for i := 0; i < NBBootpClients; i++ {
		bc := &s.BootpClients[i]
		if bc.Allocated == 0 || bytes.Equal(macaddr[:], bc.MACAddr[:]) {
			bc.Allocated = 1
			// Calculate IP: vdhcp_startaddr + i
			startIP := ipToUint32(s.VDHCPStartAddr)
			addr := uint32ToIP(startIP + uint32(i))
			return bc, addr
		}
	}
	return nil, nil
}

// requestAddr requests a specific DHCP address for a client.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:58-74
func (s *Slirp) requestAddr(reqAddr net.IP, macaddr [6]byte) *BOOTPClient {
	reqAddrInt := ipToUint32(reqAddr)
	dhcpAddrInt := ipToUint32(s.VDHCPStartAddr)

	if reqAddrInt >= dhcpAddrInt && reqAddrInt < (dhcpAddrInt+NBBootpClients) {
		idx := reqAddrInt - dhcpAddrInt
		bc := &s.BootpClients[idx]
		if bc.Allocated == 0 || bytes.Equal(macaddr[:], bc.MACAddr[:]) {
			bc.Allocated = 1
			return bc
		}
	}
	return nil
}

// findAddr finds an existing DHCP lease by MAC address.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:76-92
func (s *Slirp) findAddr(macaddr [6]byte) (*BOOTPClient, net.IP) {
	for i := 0; i < NBBootpClients; i++ {
		if bytes.Equal(macaddr[:], s.BootpClients[i].MACAddr[:]) {
			bc := &s.BootpClients[i]
			bc.Allocated = 1
			startIP := ipToUint32(s.VDHCPStartAddr)
			addr := uint32ToIP(startIP + uint32(i))
			return bc, addr
		}
	}
	return nil, nil
}

// dhcpDecode extracts the DHCP message type and requested address from options.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:94-141
func dhcpDecode(bp *BootpT) (msgType int, reqAddr net.IP) {
	msgType = 0
	reqAddr = net.IPv4zero

	p := bp.Vend[:]

	// Check magic cookie
	if !bytes.Equal(p[:4], RFC1533Cookie) {
		return
	}
	p = p[4:]

	for len(p) > 0 {
		tag := p[0]
		if tag == RFC1533Pad {
			p = p[1:]
		} else if tag == RFC1533End {
			break
		} else {
			p = p[1:]
			if len(p) == 0 {
				break
			}
			optLen := int(p[0])
			p = p[1:]

			if len(p) < optLen {
				break
			}

			switch tag {
			case RFC2132MsgType:
				if optLen >= 1 {
					msgType = int(p[0])
				}
			case RFC2132ReqAddr:
				if optLen >= 4 {
					reqAddr = net.IP(p[:4]).To4()
				}
			}
			p = p[optLen:]
		}
	}

	// If DHCPREQUEST and no requested address, use client IP
	if msgType == DHCPRequest && reqAddr.Equal(net.IPv4zero) && !bp.Ciaddr.Equal(net.IPv4zero) {
		reqAddr = bp.Ciaddr
	}

	return
}

// bootpReply sends a BOOTP/DHCP reply.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:143-305
func (s *Slirp) bootpReply(bp *BootpT) {
	var bc *BOOTPClient
	var daddr SockaddrIn
	var preqAddr net.IP

	// Extract exact DHCP msg type
	dhcpMsgType, preqAddr := dhcpDecode(bp)

	if dhcpMsgType == 0 {
		dhcpMsgType = DHCPRequest // Force reply for old BOOTP clients
	}

	if dhcpMsgType != DHCPDiscover && dhcpMsgType != DHCPRequest {
		return
	}

	// Get the client mac address
	var hwaddr [6]byte
	copy(hwaddr[:], bp.Hwaddr[:6])
	copy(s.ClientEthAddr[:], hwaddr[:])

	m := s.MGet()
	if m == nil {
		return
	}

	// Reserve space for headers
	// m->m_data += IF_MAXLINKHDR
	dataOffset := IFMaxLinkHdr

	// Make sure we have enough space
	if len(m.Data) < dataOffset+BootpSize+IPHeaderSize+UDPHeaderSize {
		m.Free()
		return
	}

	// rbp is the BOOTP reply we're building
	// Note: UDPOutput2 will prepend IP/UDP headers, so we just write the BOOTP data
	// at dataOffset, and after SetDataOffset(dataOffset), m.Data will point to it.
	rbp := &BootpT{}

	if dhcpMsgType == DHCPDiscover {
		if !preqAddr.Equal(net.IPv4zero) {
			bc = s.requestAddr(preqAddr, s.ClientEthAddr)
			if bc != nil {
				daddr.Addr = preqAddr
			}
		}
		if bc == nil {
			bc, daddr.Addr = s.getNewAddr(s.ClientEthAddr)
			if bc == nil {
				m.Free()
				return
			}
		}
		copy(bc.MACAddr[:], s.ClientEthAddr[:])
	} else if !preqAddr.Equal(net.IPv4zero) {
		bc = s.requestAddr(preqAddr, s.ClientEthAddr)
		if bc != nil {
			daddr.Addr = preqAddr
			copy(bc.MACAddr[:], s.ClientEthAddr[:])
		} else {
			daddr.Addr = net.IPv4zero
		}
	} else {
		bc, daddr.Addr = s.findAddr(hwaddr)
		if bc == nil {
			// If never assigned, behaves as if it was already assigned (windows fix)
			// goto new_addr in C jumps to get_new_addr and then falls through to
			// the memcpy at line 194, so we must copy the MAC here too.
			bc, daddr.Addr = s.getNewAddr(s.ClientEthAddr)
			if bc == nil {
				m.Free()
				return
			}
			copy(bc.MACAddr[:], s.ClientEthAddr[:])
		}
	}

	saddr := SockaddrIn{
		Addr: s.VHostAddr,
		Port: BootpServer,
	}
	daddr.Port = BootpClient

	rbp.Op = BootpReply
	rbp.Xid = bp.Xid
	rbp.Htype = 1
	rbp.Hlen = 6
	copy(rbp.Hwaddr[:], bp.Hwaddr[:6])

	rbp.Yiaddr = daddr.Addr // Client IP address
	rbp.Siaddr = saddr.Addr // Server IP address

	// Build DHCP options
	q := rbp.Vend[:]
	copy(q[:4], RFC1533Cookie)
	qIdx := 4

	if bc != nil {
		if dhcpMsgType == DHCPDiscover {
			q[qIdx] = RFC2132MsgType
			qIdx++
			q[qIdx] = 1
			qIdx++
			q[qIdx] = DHCPOffer
			qIdx++
		} else { // DHCPREQUEST
			q[qIdx] = RFC2132MsgType
			qIdx++
			q[qIdx] = 1
			qIdx++
			q[qIdx] = DHCPAck
			qIdx++
		}

		if s.BootpFilename != "" {
			copy(rbp.File[:], s.BootpFilename)
		}

		// Server ID
		q[qIdx] = RFC2132SrvID
		qIdx++
		q[qIdx] = 4
		qIdx++
		copy(q[qIdx:qIdx+4], saddr.Addr.To4())
		qIdx += 4

		// Netmask
		q[qIdx] = RFC1533Netmask
		qIdx++
		q[qIdx] = 4
		qIdx++
		copy(q[qIdx:qIdx+4], s.VNetworkMask.To4())
		qIdx += 4

		if !s.Restricted {
			// Gateway
			q[qIdx] = RFC1533Gateway
			qIdx++
			q[qIdx] = 4
			qIdx++
			copy(q[qIdx:qIdx+4], saddr.Addr.To4())
			qIdx += 4

			// DNS
			q[qIdx] = RFC1533DNS
			qIdx++
			q[qIdx] = 4
			qIdx++
			copy(q[qIdx:qIdx+4], s.VNameserverAddr.To4())
			qIdx += 4
		}

		// Lease time
		q[qIdx] = RFC2132LeaseTime
		qIdx++
		q[qIdx] = 4
		qIdx++
		binary.BigEndian.PutUint32(q[qIdx:qIdx+4], LeaseTime)
		qIdx += 4

		// Hostname
		if s.ClientHostname != "" {
			hostnameLen := len(s.ClientHostname)
			q[qIdx] = RFC1533Hostname
			qIdx++
			q[qIdx] = byte(hostnameLen)
			qIdx++
			copy(q[qIdx:qIdx+hostnameLen], s.ClientHostname)
			qIdx += hostnameLen
		}
	} else {
		// NAK
		nakMsg := "requested address not available"

		q[qIdx] = RFC2132MsgType
		qIdx++
		q[qIdx] = 1
		qIdx++
		q[qIdx] = DHCPNak
		qIdx++

		q[qIdx] = RFC2132Message
		qIdx++
		q[qIdx] = byte(len(nakMsg))
		qIdx++
		copy(q[qIdx:qIdx+len(nakMsg)], nakMsg)
		qIdx += len(nakMsg)
	}
	q[qIdx] = RFC1533End

	// Broadcast address
	daddr.Addr = net.IPv4bcast

	// Write the BOOTP packet to the mbuf at dataOffset
	// After SetDataOffset(dataOffset), m.Data will point here
	rbp.Marshal(m.Data[dataOffset:])

	// Set mbuf length: sizeof(struct bootp_t) - sizeof(struct ip) - sizeof(struct udphdr)
	m.Len = BootpSize
	// Advance data pointer past IP+UDP headers
	// C: m->m_data += dataOffset;
	m.SetDataOffset(m.Offset + dataOffset)

	s.UDPOutput2(nil, m, &saddr, &daddr, IPTOSLowDelay)
}

// BootpInput processes an incoming BOOTP packet.
// Reference: tinyemu-2019-12-21/slirp/bootp.c:307-314
func (s *Slirp) BootpInput(m *Mbuf) {
	// Use BootpMinSize for input validation - DHCP clients may send shorter packets
	if m == nil || m.Len < IPHeaderSize+UDPHeaderSize+BootpMinSize {
		return
	}

	// Parse BOOTP packet (skip IP and UDP headers)
	bp := ParseBootp(m.Data[IPHeaderSize+UDPHeaderSize:])
	if bp == nil {
		return
	}

	if bp.Op == BootpRequest {
		s.bootpReply(bp)
	}
}

// UDPHeaderSize is the size of a UDP header
const UDPHeaderSize = 8

// UDPOutput2 sends a UDP datagram with specified source and destination.
// Reference: tinyemu-2019-12-21/slirp/udp.c:228-277
func (s *Slirp) UDPOutput2(so *Socket, m *Mbuf, saddr, daddr *SockaddrIn, iptos int) int {
	if m == nil {
		return -1
	}

	// Adjust for header - prepend UDP/IP header space
	totalLen := m.Len + IPHeaderSize + UDPHeaderSize

	// Create new buffer with headers
	newData := make([]byte, totalLen)

	// Copy BOOTP data after headers
	copy(newData[IPHeaderSize+UDPHeaderSize:], m.Data[:m.Len])

	// Fill in UDP header
	udpHdr := newData[IPHeaderSize:]
	binary.BigEndian.PutUint16(udpHdr[0:2], saddr.Port)                  // source port
	binary.BigEndian.PutUint16(udpHdr[2:4], daddr.Port)                  // dest port
	binary.BigEndian.PutUint16(udpHdr[4:6], uint16(m.Len+UDPHeaderSize)) // UDP length
	binary.BigEndian.PutUint16(udpHdr[6:8], 0)                           // checksum (initially 0)

	// Fill in IP header
	ipHdr := newData[:IPHeaderSize]
	ipHdr[0] = (IPVersion << 4) | (IPHeaderSize >> 2)        // version + header length
	ipHdr[1] = byte(iptos)                                   // TOS
	binary.BigEndian.PutUint16(ipHdr[2:4], uint16(totalLen)) // total length
	binary.BigEndian.PutUint16(ipHdr[4:6], s.IPID)           // ID
	s.IPID++
	binary.BigEndian.PutUint16(ipHdr[6:8], 0)   // fragment offset
	ipHdr[8] = IPDefTTL                         // TTL
	ipHdr[9] = IPProtoUDP                       // protocol
	binary.BigEndian.PutUint16(ipHdr[10:12], 0) // checksum (initially 0)
	copy(ipHdr[12:16], saddr.Addr.To4())        // source address
	copy(ipHdr[16:20], daddr.Addr.To4())        // dest address

	// Compute UDP checksum (with pseudo-header)
	udpLen := m.Len + UDPHeaderSize
	udpSum := s.udpChecksum(newData, saddr.Addr, daddr.Addr, udpLen)
	if udpSum == 0 {
		udpSum = 0xffff
	}
	binary.BigEndian.PutUint16(udpHdr[6:8], udpSum)

	// Compute IP checksum
	ipSum := CksumData(ipHdr)
	binary.BigEndian.PutUint16(ipHdr[10:12], ipSum)

	// Update mbuf
	m.Data = newData
	m.Len = totalLen

	// Send via IP output
	return s.IPOutput(so, m)
}

// udpChecksum computes UDP checksum with pseudo-header.
func (s *Slirp) udpChecksum(data []byte, src, dst net.IP, udpLen int) uint16 {
	// Build pseudo-header
	pseudoLen := 12 + udpLen // pseudo-header + UDP data
	pseudo := make([]byte, pseudoLen)

	// Pseudo-header: src IP, dst IP, zero, protocol, UDP length
	copy(pseudo[0:4], src.To4())
	copy(pseudo[4:8], dst.To4())
	pseudo[8] = 0
	pseudo[9] = IPProtoUDP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(udpLen))

	// Copy UDP header + data
	copy(pseudo[12:], data[IPHeaderSize:IPHeaderSize+udpLen])

	return CksumData(pseudo)
}

// Helper functions to convert between IP and uint32

func ipToUint32(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip4)
}

func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}
