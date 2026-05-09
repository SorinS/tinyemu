package slirp

import (
	"encoding/binary"
	"net"
)

// ICMP types and codes.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.h:111-147
const (
	ICMPEchoReply     = 0  // echo reply
	ICMPUnreach       = 3  // dest unreachable
	ICMPSourceQuench  = 4  // packet lost, slow down
	ICMPRedirect      = 5  // shorter route
	ICMPEcho          = 8  // echo service
	ICMPRouterAdvert  = 9  // router advertisement
	ICMPRouterSolicit = 10 // router solicitation
	ICMPTimXceed      = 11 // time exceeded
	ICMPParamProb     = 12 // ip header bad
	ICMPTStamp        = 13 // timestamp request
	ICMPTStampReply   = 14 // timestamp reply
	ICMPIReq          = 15 // information request
	ICMPIReqReply     = 16 // information reply
	ICMPMaskReq       = 17 // address mask request
	ICMPMaskReply     = 18 // address mask reply

	ICMPMaxType = 18

	// ICMP_UNREACH codes
	ICMPUnreachNet         = 0  // bad net
	ICMPUnreachHost        = 1  // bad host
	ICMPUnreachProtocol    = 2  // bad protocol
	ICMPUnreachPort        = 3  // bad port
	ICMPUnreachNeedFrag    = 4  // IP_DF caused drop
	ICMPUnreachSrcFail     = 5  // src route failed
	ICMPUnreachNetUnknown  = 6  // unknown net
	ICMPUnreachHostUnknown = 7  // unknown host
	ICMPUnreachIsolated    = 8  // src host isolated
	ICMPUnreachNetProhib   = 9  // prohibited access
	ICMPUnreachHostProhib  = 10 // ditto
	ICMPUnreachTOSNet      = 11 // bad tos for net
	ICMPUnreachTOSHost     = 12 // bad tos for host

	// ICMP_REDIRECT codes
	ICMPRedirectNet     = 0 // for network
	ICMPRedirectHost    = 1 // for host
	ICMPRedirectTOSNet  = 2 // for tos and net
	ICMPRedirectTOSHost = 3 // for tos and host

	// ICMP_TIMXCEED codes
	ICMPTimXceedInTrans = 0 // ttl==0 in transit
	ICMPTimXceedReass   = 1 // ttl==0 in reass

	// ICMP_PARAMPROB codes
	ICMPParamProbOptAbsent = 1 // req. opt. absent

	// ICMP lengths
	ICMPMinLen  = 8                    // abs minimum
	ICMPTSLen   = 8 + 3*4              // timestamp
	ICMPMaskLen = 12                   // address mask
	ICMPAdvLen  = 8 + IPHeaderSize + 8 // min for error advice

	// ICMPMaxDataLen is the maximum data length for ICMP error payloads.
	// ICMP fragmentation is illegal per RFC 792 - all hosts must accept 576 bytes.
	// Maximum payload = IPMSS(576) - IP_header(20) - ICMP_header(8) = 548 bytes.
	// This ensures ICMP errors never exceed the minimum MTU and need no fragmentation.
	// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:183-187
	ICMPMaxDataLen = IPMSS - 28
)

// icmpFlush is the list of actions for icmp_error() on RX of an icmp message.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:41-61
var icmpFlush = [19]int{
	0, // ECHO REPLY (0)
	1,
	1,
	1, // DEST UNREACH (3)
	1, // SOURCE QUENCH (4)
	1, // REDIRECT (5)
	1,
	1,
	0, // ECHO (8)
	1, // ROUTERADVERT (9)
	1, // ROUTERSOLICIT (10)
	1, // TIME EXCEEDED (11)
	1, // PARAMETER PROBLEM (12)
	0, // TIMESTAMP (13)
	0, // TIMESTAMP REPLY (14)
	0, // INFO (15)
	0, // INFO REPLY (16)
	0, // ADDR MASK (17)
	0, // ADDR MASK REPLY (18)
}

// ICMPPingMsg is the message sent when emulating PING.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:38
var ICMPPingMsg = []byte("This is a pseudo-PING packet used by Slirp to emulate ICMP ECHO-REQUEST packets.\n")

// ICMP represents an ICMP header.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.h:46-91
type ICMP struct {
	Type  uint8  // type of message
	Code  uint8  // type sub code
	Cksum uint16 // ones complement cksum of struct
	ID    uint16 // identifier (for echo)
	Seq   uint16 // sequence number (for echo)
	// For ICMP error messages, the data portion starts with the original IP header
}

// ICMPHeaderSize is the size of an ICMP header (8 bytes).
const ICMPHeaderSize = 8

// ParseICMP parses an ICMP header from a byte slice.
func ParseICMP(data []byte) *ICMP {
	if len(data) < ICMPHeaderSize {
		return nil
	}
	return &ICMP{
		Type:  data[0],
		Code:  data[1],
		Cksum: binary.BigEndian.Uint16(data[2:4]),
		ID:    binary.BigEndian.Uint16(data[4:6]),
		Seq:   binary.BigEndian.Uint16(data[6:8]),
	}
}

// Marshal writes the ICMP header to a byte slice.
func (icmp *ICMP) Marshal(data []byte) {
	if len(data) < ICMPHeaderSize {
		return
	}
	data[0] = icmp.Type
	data[1] = icmp.Code
	binary.BigEndian.PutUint16(data[2:4], icmp.Cksum)
	binary.BigEndian.PutUint16(data[4:6], icmp.ID)
	binary.BigEndian.PutUint16(data[6:8], icmp.Seq)
}

// ICMPInput processes a received ICMP message.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:66-166
func (s *Slirp) ICMPInput(m *Mbuf, hlen int) {
	if m == nil || m.Len < hlen+ICMPMinLen {
		if m != nil {
			m.Free()
		}
		return
	}

	// Parse IP header
	ip := ParseIPRaw(m.Data[:m.Len])
	if ip == nil {
		m.Free()
		return
	}
	icmpLen := int(ip.Len)

	// Check minimum length
	if icmpLen < ICMPMinLen {
		m.Free()
		return
	}

	// Verify ICMP checksum
	// Temporarily adjust mbuf to point to ICMP data
	icmpData := m.Data[hlen:]
	origLen := m.Len
	m.Len -= hlen
	tmpData := m.Data
	m.Data = icmpData

	if Cksum(m, icmpLen) != 0 {
		m.Data = tmpData
		m.Len = origLen
		m.Free()
		return
	}
	m.Data = tmpData
	m.Len = origLen

	// Parse ICMP header
	icp := ParseICMP(m.Data[hlen:])
	if icp == nil {
		m.Free()
		return
	}

	switch icp.Type {
	case ICMPEcho:
		// Change to echo reply
		m.Data[hlen] = ICMPEchoReply

		// Add hlen back to ip_len since ip_input subtracted it
		newLen := ip.Len + uint16(hlen)
		binary.BigEndian.PutUint16(m.Data[2:4], newLen)

		// Check if destined for our virtual host
		if ip.Dst.Equal(s.VHostAddr) {
			s.ICMPReflect(m)
		} else {
			// Forward ICMP echo to external host via UDP echo port
			// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:105-146
			s.icmpForwardEcho(m, ip, hlen)
		}

	case ICMPUnreach, ICMPTimXceed, ICMPParamProb, ICMPSourceQuench,
		ICMPTStamp, ICMPMaskReq, ICMPRedirect:
		m.Free()

	default:
		m.Free()
	}
}

// icmpForwardEcho forwards an ICMP echo request to an external host via UDP.
// This creates a UDP socket and sends a ping message to the echo port (7).
// When a reply arrives, SoRecvFrom will handle it and reflect the ICMP reply.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:105-146
func (s *Slirp) icmpForwardEcho(m *Mbuf, ip *IPRaw, hlen int) {
	// Create a socket for the ICMP ping
	so := s.SoCreate()
	if so == nil {
		m.Free()
		return
	}

	// Attach as UDP socket
	if s.UDPAttach(so) < 0 {
		so.SoFree()
		m.Free()
		return
	}

	// Store the mbuf for when we receive the reply
	so.SoM = m

	// Set up socket fields
	// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:116-122
	so.SoFAddr = ip.Dst
	so.SoFPort = 7 // Echo port (network byte order handled by sendto)
	so.SoLAddr = ip.Src
	so.SoLPort = 9 // Discard port
	so.SoIPTos = ip.TOS
	so.SoType = IPProtoICMP // Mark as ICMP socket
	so.SoState = SSIsFConnected

	// Determine the actual destination address
	// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:126-137
	var destIP net.IP
	vnetMask := ipToUint32(s.VNetworkMask)
	vnetAddr := ipToUint32(s.VNetworkAddr)
	soFAddr := ipToUint32(so.SoFAddr)

	if (soFAddr & vnetMask) == vnetAddr {
		// It's an alias
		if so.SoFAddr.Equal(s.VNameserverAddr) {
			// DNS server - use real DNS
			if addr, ok := getDnsAddr(); ok {
				destIP = addr
			} else {
				destIP = LoopbackAddr
			}
		} else {
			destIP = LoopbackAddr
		}
	} else {
		destIP = so.SoFAddr
	}

	// Send the ping message via UDP
	// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:139-145
	conn, ok := so.Extra.(*net.UDPConn)
	if !ok || conn == nil {
		s.ICMPError(m, ICMPUnreach, ICMPUnreachNet, 0, "no connection")
		s.UDPDetach(so)
		return
	}

	addr := &net.UDPAddr{
		IP:   destIP,
		Port: int(so.SoFPort),
	}

	_, err := conn.WriteToUDP(ICMPPingMsg, addr)
	if err != nil {
		s.ICMPError(m, ICMPUnreach, ICMPUnreachNet, 0, err.Error())
		s.UDPDetach(so)
		return
	}

	// Socket will remain active until reply arrives (handled by SoRecvFrom)
	// or it times out and is cleaned up
}

// ICMPError sends an ICMP error message in response to a situation.
// msrc is used as a template but is NOT freed.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:188-300
func (s *Slirp) ICMPError(msrc *Mbuf, icmpType, code uint8, minsize int, message string) {
	// Only handle UNREACH and TIMXCEED
	if icmpType != ICMPUnreach && icmpType != ICMPTimXceed {
		return
	}

	// Check msrc
	if msrc == nil {
		return
	}

	ip := ParseIPRaw(msrc.Data[:msrc.Len])
	if ip == nil {
		return
	}

	// Only reply to fragment 0
	if ip.Off&IPOffMask != 0 {
		return
	}

	shlen := int(ip.HL) << 2
	sIPLen := int(ip.Len)

	// Check if it's an ICMP packet and whether we should send an error
	if ip.Proto == IPProtoICMP {
		if msrc.Len < shlen+ICMPHeaderSize {
			return
		}
		icpType := msrc.Data[shlen]
		if icpType > 18 || icmpFlush[icpType] != 0 {
			return
		}
	}

	// Get a new mbuf
	m := s.MGet()
	if m == nil {
		return
	}

	// Make sure there's enough room
	newMSize := IPHeaderSize + ICMPMinLen + msrc.Len + ICMPMaxDataLen
	if newMSize > m.Size {
		m.Inc(newMSize)
	}

	// Copy source data to new mbuf
	copy(m.Data, msrc.Data[:msrc.Len])
	m.Len = msrc.Len

	// Get IP header pointer in new mbuf
	hlen := IPHeaderSize // no options in reply

	// Fill in ICMP header
	// Adjust data pointer to ICMP portion
	icmpOffset := hlen

	// Calculate payload size
	if minsize != 0 {
		sIPLen = shlen + ICMPMinLen // return header+8b only
	} else if sIPLen > ICMPMaxDataLen {
		sIPLen = ICMPMaxDataLen
	}

	m.Len = ICMPMinLen + sIPLen // 8 bytes ICMP header + payload

	// Set ICMP fields
	m.Data[icmpOffset] = icmpType
	m.Data[icmpOffset+1] = code
	m.Data[icmpOffset+2] = 0 // checksum (will be computed later)
	m.Data[icmpOffset+3] = 0
	m.Data[icmpOffset+4] = 0 // id
	m.Data[icmpOffset+5] = 0
	m.Data[icmpOffset+6] = 0 // seq
	m.Data[icmpOffset+7] = 0

	// Copy the original IP header (and 8 bytes of data) to ICMP payload
	copy(m.Data[icmpOffset+ICMPMinLen:], msrc.Data[:sIPLen])

	// Convert the copied IP header fields to network byte order
	// (they were in host order in the original packet)
	copyIP := m.Data[icmpOffset+ICMPMinLen:]
	ipLen := binary.BigEndian.Uint16(copyIP[2:4])
	binary.BigEndian.PutUint16(copyIP[2:4], ipLen) // ip_len
	ipID := binary.BigEndian.Uint16(copyIP[4:6])
	binary.BigEndian.PutUint16(copyIP[4:6], ipID) // ip_id
	ipOff := binary.BigEndian.Uint16(copyIP[6:8])
	binary.BigEndian.PutUint16(copyIP[6:8], ipOff) // ip_off

	// Compute ICMP checksum
	icmpData := m.Data[icmpOffset : icmpOffset+m.Len]
	m.Data[icmpOffset+2] = 0
	m.Data[icmpOffset+3] = 0
	cksum := CksumData(icmpData)
	binary.BigEndian.PutUint16(m.Data[icmpOffset+2:icmpOffset+4], cksum)

	// Add IP header length
	m.Len += hlen

	// Fill in IP header
	m.Data[0] = (4 << 4) | (uint8(hlen) >> 2) // version + header length
	m.Data[1] = (ip.TOS & 0x1E) | 0xC0        // high priority for errors
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(m.Len))

	m.Data[8] = MaxTTL
	m.Data[9] = IPProtoICMP

	// Swap addresses
	copy(m.Data[16:20], ip.Src)            // ip_dst = original ip_src
	copy(m.Data[12:16], s.VHostAddr.To4()) // ip_src = vhost_addr

	// Send the packet
	s.IPOutput(nil, m)
}

// ICMPReflect reflects the IP packet back to the source.
// Reference: tinyemu-2019-12-21/slirp/ip_icmp.c:306-351
func (s *Slirp) ICMPReflect(m *Mbuf) {
	if m == nil {
		return
	}

	ip := ParseIPRaw(m.Data[:m.Len])
	if ip == nil {
		m.Free()
		return
	}

	hlen := int(ip.HL) << 2
	optlen := hlen - IPHeaderSize

	// Compute ICMP checksum
	// Point to ICMP data
	icmpLen := int(ip.Len) - hlen
	if icmpLen < ICMPMinLen {
		m.Free()
		return
	}

	// Set ICMP checksum to 0 before computing
	m.Data[hlen+2] = 0
	m.Data[hlen+3] = 0

	// Compute checksum over ICMP portion
	cksum := CksumData(m.Data[hlen : hlen+icmpLen])
	binary.BigEndian.PutUint16(m.Data[hlen+2:hlen+4], cksum)

	// Strip out original options by copying rest of data back
	if optlen > 0 {
		copy(m.Data[IPHeaderSize:], m.Data[hlen:m.Len])
		hlen -= optlen
		m.Data[0] = (m.Data[0] & 0xf0) | (uint8(hlen) >> 2) // update header length

		// Update ip_len
		newLen := binary.BigEndian.Uint16(m.Data[2:4]) - uint16(optlen)
		binary.BigEndian.PutUint16(m.Data[2:4], newLen)

		m.Len -= optlen
	}

	// Set TTL
	m.Data[8] = MaxTTL

	// Swap src and dst addresses
	var tmpAddr [4]byte
	copy(tmpAddr[:], m.Data[16:20])    // save dst
	copy(m.Data[16:20], m.Data[12:16]) // dst = src
	copy(m.Data[12:16], tmpAddr[:])    // src = old dst

	// Send the packet
	s.IPOutput(nil, m)
}

// ParseIPRaw parses an IP header from raw bytes.
// This is a simpler version that works directly with byte slice.
func ParseIPRaw(data []byte) *IPRaw {
	if len(data) < IPHeaderSize {
		return nil
	}
	return &IPRaw{
		HL:    data[0] & 0x0f,
		TOS:   data[1],
		Len:   binary.BigEndian.Uint16(data[2:4]),
		ID:    binary.BigEndian.Uint16(data[4:6]),
		Off:   binary.BigEndian.Uint16(data[6:8]),
		TTL:   data[8],
		Proto: data[9],
		Sum:   binary.BigEndian.Uint16(data[10:12]),
		Src:   net.IP(data[12:16]),
		Dst:   net.IP(data[16:20]),
	}
}

// IPRaw is a raw IP header for direct byte manipulation.
type IPRaw struct {
	HL    uint8
	TOS   uint8
	Len   uint16
	ID    uint16
	Off   uint16
	TTL   uint8
	Proto uint8
	Sum   uint16
	Src   net.IP
	Dst   net.IP
}

// IPOutput sends an IP packet.
// The packet in mbuf m contains a skeletal IP header (with len, off, ttl, proto, tos, src, dst).
// The mbuf chain containing the packet will be freed.
// This delegates to ipOutput which has the full implementation including fragmentation.
// Reference: tinyemu-2019-12-21/slirp/ip_output.c:53-172
func (s *Slirp) IPOutput(so *Socket, m *Mbuf) int {
	return s.ipOutput(so, m)
}
