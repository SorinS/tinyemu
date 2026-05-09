package slirp

import (
	"encoding/binary"
	"errors"
	"net"
)

// UDP header field offsets
// Reference: tinyemu-2019-12-21/slirp/udp.h:43-48
const (
	udpSrcPortOffset = 0
	udpDstPortOffset = 2
	udpLenOffset     = 4
	udpSumOffset     = 6
)

// tosEntry represents an entry in the UDP TOS table.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:79-84 (struct tos_t)
type tosEntry struct {
	fport uint16 // foreign port
	lport uint16 // local port
	tos   uint8  // type of service
	emu   uint8  // emulation flag
}

// udpTos is the table of TOS values for specific UDP ports.
// Reference: tinyemu-2019-12-21/slirp/udp.c:321-324
var udpTos = []tosEntry{
	{0, 53, IPTOSLowDelay, 0}, // DNS
	{0, 0, 0, 0},              // terminator
}

// UDPInit initializes UDP state.
// This is called from NewSlirp.
// Reference: tinyemu-2019-12-21/slirp/udp.c:47-51
func (s *Slirp) UDPInit() {
	s.UDB.Next = &s.UDB
	s.UDB.Prev = &s.UDB
	s.UDPLastSo = &s.UDB
}

// UDPInput processes an incoming UDP packet.
// m.Data points at IP packet header, m.Len is the length of the IP packet.
// Reference: tinyemu-2019-12-21/slirp/udp.c:57-226
func (s *Slirp) UDPInput(m *Mbuf, iphlen int) {
	if m == nil {
		return
	}

	// Strip IP options, if any; should skip this,
	// make available to user, and use on returned packets,
	// but we don't yet have a way to check the checksum
	// with options still present.
	if iphlen > IPHeaderSize {
		IPStripOptions(m)
		iphlen = IPHeaderSize
	}

	// Get IP and UDP header together in first mbuf
	if m.Len < iphlen+UDPHeaderSize {
		m.Free()
		return
	}

	ip := ParseIPRaw(m.Data[:m.Len])
	if ip == nil {
		m.Free()
		return
	}

	// UDP header starts after IP header
	uh := m.Data[iphlen:]

	// Make mbuf data length reflect UDP length.
	// If not enough data to reflect UDP length, drop.
	udpLen := int(binary.BigEndian.Uint16(uh[udpLenOffset : udpLenOffset+2]))
	ipLen := int(ip.Len)

	if ipLen != udpLen {
		if udpLen > ipLen {
			m.Free()
			return
		}
		m.Adj(udpLen - ipLen)
		ip.Len = uint16(udpLen)
		// Update in mbuf
		binary.BigEndian.PutUint16(m.Data[2:4], uint16(udpLen))
	}

	// Save a copy of the IP header in case we want restore it
	// for sending an ICMP error message in response.
	saveIP := make([]byte, iphlen)
	copy(saveIP, m.Data[:iphlen])
	// tcp_input subtracts this, so add it back
	saveIPLen := ip.Len + uint16(iphlen)
	binary.BigEndian.PutUint16(saveIP[2:4], saveIPLen)

	// Checksum extended UDP header and data.
	uhSum := binary.BigEndian.Uint16(uh[udpSumOffset : udpSumOffset+2])
	if uhSum != 0 {
		// Build pseudo-header for checksum
		// Zero out mbuf pointer area in ipovly overlay (first 8 bytes of IP header)
		for i := 0; i < 8; i++ {
			m.Data[i] = 0
		}
		// Set ih_x1 to 0 (byte 8 in ip overlay, but we use standard IP header)
		m.Data[8] = 0
		// Set ih_pr to UDP (byte 9)
		m.Data[9] = IPProtoUDP
		// Set ih_len to UDP length (bytes 10-11, replacing IP checksum)
		binary.BigEndian.PutUint16(m.Data[10:12], uint16(udpLen))

		if Cksum(m, udpLen+IPHeaderSize) != 0 {
			m.Free()
			return
		}
	}

	// Handle DHCP/BOOTP
	dstPort := binary.BigEndian.Uint16(uh[udpDstPortOffset : udpDstPortOffset+2])
	if dstPort == BootpServer {
		s.BootpInput(m)
		m.Free()
		return
	}

	// In restricted mode, drop non-BOOTP UDP
	if s.Restricted {
		m.Free()
		return
	}

	// TFTP handling would go here (commented out in C)

	// Locate pcb for datagram
	srcPort := binary.BigEndian.Uint16(uh[udpSrcPortOffset : udpSrcPortOffset+2])
	so := s.UDPLastSo
	if so.SoLPort != srcPort || !so.SoLAddr.Equal(ip.Src) {
		// Search for matching socket
		var found *Socket
		for tmp := s.UDB.Next; tmp != &s.UDB; tmp = tmp.Next {
			if tmp.SoLPort == srcPort && tmp.SoLAddr.Equal(ip.Src) {
				found = tmp
				break
			}
		}
		if found != nil {
			so = found
			s.UDPLastSo = so
		} else {
			so = nil
		}
	}

	if so == nil {
		// If there's no socket for this packet, create one
		so = s.SoCreate()
		if so == nil {
			m.Free()
			return
		}
		if s.UDPAttach(so) < 0 {
			so.SoFree()
			m.Free()
			return
		}

		// Setup fields
		so.SoLAddr = ip.Src
		so.SoLPort = srcPort

		tos := s.udpGetTos(so)
		if tos == 0 {
			tos = ip.TOS
		}
		so.SoIPTos = tos

		// XXXXX Here, check if it's in udpexec_list,
		// and if it is, do the fork_exec() etc.
	}

	so.SoFAddr = ip.Dst
	so.SoFPort = dstPort

	// Adjust mbuf to point to UDP data (skip IP+UDP headers)
	// C: m->m_data += iphlen; m->m_len -= iphlen;
	iphlen += UDPHeaderSize
	m.Adj(iphlen)

	// Now we sendto() the packet
	err := s.SoSendTo(so, m)
	if err != nil {
		// Restore mbuf for ICMP by moving data pointer back
		// With our new offset-based design, this is now possible!
		// C: m->m_data -= iphlen; m->m_len += iphlen;
		m.Prepend(iphlen)
		copy(m.Data, saveIP)
		s.ICMPError(m, ICMPUnreach, ICMPUnreachNet, 0, err.Error())
	}

	// Free old so_m if it exists
	if so.SoM != nil {
		so.SoM.Free()
	}

	// Restore the orig mbuf packet
	m.Len += iphlen
	// In C, this would restore m->m_data -= iphlen
	// For Go, we need to rebuild with saved IP
	so.SoM = m // ICMP backup
}

// UDPOutput sends a UDP datagram from a socket.
// Reference: tinyemu-2019-12-21/slirp/udp.c:279-302
func (s *Slirp) UDPOutput(so *Socket, m *Mbuf, addr *SockaddrIn) int {
	saddr := *addr

	// Check if destination is on virtual network
	vnetMask := ipToUint32(s.VNetworkMask)
	vnetAddr := ipToUint32(s.VNetworkAddr)
	faddr := ipToUint32(so.SoFAddr)

	if (faddr & vnetMask) == vnetAddr {
		invMask := ^vnetMask

		if (faddr & invMask) == invMask {
			// Broadcast address - use vhost
			saddr.Addr = s.VHostAddr
		} else if addr.Addr.Equal(LoopbackAddr) || !so.SoFAddr.Equal(s.VHostAddr) {
			saddr.Addr = so.SoFAddr
		}
	}

	daddr := SockaddrIn{
		Addr: so.SoLAddr,
		Port: so.SoLPort,
	}

	return s.UDPOutput2(so, m, &saddr, &daddr, int(so.SoIPTos))
}

// UDPAttach attaches a UDP socket to the system.
// Returns the socket fd on success, -1 on error.
// Reference: tinyemu-2019-12-21/slirp/udp.c:305-312
func (s *Slirp) UDPAttach(so *Socket) int {
	// Create UDP socket using Go's net package
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return -1
	}

	// Store the connection in the socket
	so.Extra = conn
	so.S = 0 // Indicate socket is valid (we don't have actual fd)

	// Set expiry time
	so.SoExpire = GetTimeMs() + SOExpire

	// Insert into UDP list
	so.Next = s.UDB.Next
	so.Prev = &s.UDB
	s.UDB.Next.Prev = so
	s.UDB.Next = so

	s.traceSocketCreate(so, "UDP")

	return 0
}

// UDPDetach detaches a UDP socket.
// Reference: tinyemu-2019-12-21/slirp/udp.c:315-319
func (s *Slirp) UDPDetach(so *Socket) {
	if so == nil {
		return
	}

	// Close the system socket
	if conn, ok := so.Extra.(*net.UDPConn); ok && conn != nil {
		conn.Close()
	}

	so.SoFree()
}

// udpGetTos returns the TOS value for a UDP socket based on port.
// Reference: tinyemu-2019-12-21/slirp/udp.c:327-341
func (s *Slirp) udpGetTos(so *Socket) uint8 {
	for i := 0; udpTos[i].tos != 0; i++ {
		if (udpTos[i].fport != 0 && so.SoFPort == udpTos[i].fport) ||
			(udpTos[i].lport != 0 && so.SoLPort == udpTos[i].lport) {
			so.SoEmu = udpTos[i].emu
			return udpTos[i].tos
		}
	}
	return 0
}

// SoSendTo sends data from an mbuf to the socket's foreign address.
// This is a wrapper around Socket.SoSendTo for Slirp method compatibility.
// Reference: tinyemu-2019-12-21/slirp/socket.c:532-573
func (s *Slirp) SoSendTo(so *Socket, m *Mbuf) error {
	if so == nil || m == nil {
		return nil
	}
	ret := so.SoSendTo(m)
	if ret < 0 {
		return errors.New("sosendto failed")
	}
	return nil
}

// UDPListen creates a UDP socket for port forwarding.
// Returns the socket on success, nil on error.
// Reference: tinyemu-2019-12-21/slirp/udp.c:344-386
func (s *Slirp) UDPListen(haddr net.IP, hport uint16, laddr net.IP, lport uint16, flags int) *Socket {
	// Create socket
	so := s.SoCreate()
	if so == nil {
		return nil
	}

	// Create UDP socket and bind
	addr := &net.UDPAddr{
		IP:   haddr,
		Port: int(hport),
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		so.SoFree()
		return nil
	}

	// Store the connection
	so.Extra = conn
	so.S = 0 // Indicate socket is valid

	// Set expiry
	so.SoExpire = GetTimeMs() + SOExpire

	// Insert into UDP list
	so.Next = s.UDB.Next
	so.Prev = &s.UDB
	s.UDB.Next.Prev = so
	s.UDB.Next = so

	s.traceSocketCreate(so, "UDP-listen")

	// Set SO_REUSEADDR (Go does this by default for ListenUDP)

	// Get the actual bound address
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	so.SoFPort = uint16(localAddr.Port)

	if localAddr.IP.Equal(net.IPv4zero) || localAddr.IP.Equal(LoopbackAddr) {
		so.SoFAddr = s.VHostAddr
	} else {
		so.SoFAddr = localAddr.IP
	}

	so.SoLPort = lport
	so.SoLAddr = laddr

	// If not SSFAcceptOnce, set no expiry
	if flags != SSFAcceptOnce {
		so.SoExpire = 0
	}

	so.SoState &= SSPersistentMask
	so.SoState |= SSIsFConnected | flags

	return so
}
