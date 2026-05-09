package slirp

import (
	"encoding/binary"
	"net"
)

// IP fragment reassembly structures.
// Reference: tinyemu-2019-12-21/slirp/ip.h:215-232

// IPQ represents an IP reassembly queue structure.
// Each fragment being reassembled is attached to one of these structures.
// Reference: tinyemu-2019-12-21/slirp/ip.h:215-222
type IPQ struct {
	FragLink QLink  // to ip headers of fragments
	IPLink   QLink  // to other reass headers
	TTL      uint8  // time for reass q to live
	Proto    uint8  // protocol of this fragment
	ID       uint16 // sequence id for reassembly
	Src      net.IP // source address
	Dst      net.IP // destination address
	Slirp    *Slirp // back pointer to slirp instance
	M        *Mbuf  // mbuf holding this IPQ (for dtom)
}

// QLink is a doubly linked list link.
// Reference: tinyemu-2019-12-21/slirp/ip.h:192-194
type QLink struct {
	Next      *QLink
	Prev      *QLink
	container interface{} // Go equivalent of container_of - stores pointer to containing struct
}

// IPASFrag represents an IP header when holding a fragment.
// Reference: tinyemu-2019-12-21/slirp/ip.h:229-232
type IPASFrag struct {
	Link QLink  // linked list
	Off  uint16 // fragment offset (in 8-byte units, then converted to bytes)
	Len  uint16 // fragment length (data only, not including IP header)
	TOS  uint8  // type of service (used for MF flag storage)
	M    *Mbuf  // mbuf holding this fragment
}

// ipInit initializes the IP layer.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:59-64
func (s *Slirp) ipInit() {
	// Initialize the IP fragment reassembly queue as empty circular list
	s.IPQ.IPLink.Next = &s.IPQ.IPLink
	s.IPQ.IPLink.Prev = &s.IPQ.IPLink
	s.IPQ.IPLink.container = &s.IPQ
	// Initialize fragment list sentinel (used for container_of operations)
	s.IPQ.FragLink.Next = &s.IPQ.FragLink
	s.IPQ.FragLink.Prev = &s.IPQ.FragLink
	s.IPQ.FragLink.container = &s.IPQ
	// udp_init and tcp_init are called separately in NewSlirp
}

// IPInput processes an incoming IP packet.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:71-233
func (s *Slirp) IPInput(m *Mbuf) {
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:81-83
	if m.Len < IPHeaderSize {
		return
	}

	// Get IP header from mbuf data
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:85
	ip := ParseIPRaw(m.Data[:m.Len])
	if ip == nil {
		m.Free()
		return
	}

	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:87-89
	if ip.HL>>4 != IPVersion && (m.Data[0]>>4) != IPVersion {
		m.Free()
		return
	}

	// Check actual version from raw byte
	if m.Data[0]>>4 != IPVersion {
		m.Free()
		return
	}

	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:91-94
	hlen := int(m.Data[0]&0x0f) << 2
	if hlen < IPHeaderSize || hlen > m.Len {
		m.Free()
		return
	}

	// Verify IP header checksum
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:100-102
	if Cksum(m, hlen) != 0 {
		m.Free()
		return
	}

	// Get IP length and validate
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:107-110
	// Fields are in network byte order in the packet
	ipLen := int(binary.BigEndian.Uint16(m.Data[2:4]))
	if ipLen < hlen {
		m.Free()
		return
	}

	// Get other fields (convert from network byte order)
	ipID := binary.BigEndian.Uint16(m.Data[4:6])
	ipOff := binary.BigEndian.Uint16(m.Data[6:8])

	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:120-122
	if m.Len < ipLen {
		m.Free()
		return
	}

	// Restricted mode check
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:124-143
	if s.Restricted {
		dstIP := binary.BigEndian.Uint32(m.Data[16:20])
		vnetMask := ipToUint32(s.VNetworkMask)
		vnetAddr := ipToUint32(s.VNetworkAddr)

		if (dstIP & vnetMask) == vnetAddr {
			// Destination is in virtual network
			if dstIP == 0xffffffff && m.Data[9] != IPProtoUDP {
				// Broadcast but not UDP
				m.Free()
				return
			}
		} else {
			// Destination is outside virtual network
			invMask := ^vnetMask
			if (dstIP & invMask) == invMask {
				// Broadcast address
				m.Free()
				return
			}
			// Check exec list
			found := false
			for ex := s.ExecList; ex != nil; ex = ex.ExNext {
				if ipToUint32(ex.ExAddr) == dstIP {
					found = true
					break
				}
			}
			if !found {
				m.Free()
				return
			}
		}
	}

	// Trim mbuf if longer than IP length
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:146-147
	if m.Len > ipLen {
		m.Len = ipLen
	}

	// Check TTL and send ICMP error if zero
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:150-153
	if m.Data[8] == 0 {
		s.ICMPError(m, ICMPTimXceed, ICMPTimXceedInTrans, 0, "ttl")
		m.Free()
		return
	}

	// Handle IP fragmentation
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:164-211
	if ipOff&^IPDF != 0 {
		// Packet is fragmented (either offset != 0 or MF flag set)
		// Look for queue of fragments of this datagram
		// Reference: tinyemu-2019-12-21/slirp/ip_input.c:171-180
		var fp *IPQ
		ipProto := m.Data[9]
		ipSrc := net.IP(m.Data[12:16])
		ipDst := net.IP(m.Data[16:20])

		for l := s.IPQ.IPLink.Next; l != &s.IPQ.IPLink; l = l.Next {
			fpCand := qlinkToIPQ(l)
			if ipID == fpCand.ID &&
				ipSrc.Equal(fpCand.Src) &&
				ipDst.Equal(fpCand.Dst) &&
				ipProto == fpCand.Proto {
				fp = fpCand
				break
			}
		}

		// Adjust ip_len to not reflect header,
		// set ip_mff if more fragments are expected,
		// convert offset of this to bytes.
		// Reference: tinyemu-2019-12-21/slirp/ip_input.c:188-194
		fragIPLen := uint16(ipLen) - uint16(hlen)
		if ipOff&IPMF != 0 {
			m.Data[1] |= 1 // Store MF in TOS low bit
		} else {
			m.Data[1] &= ^uint8(1)
		}
		fragOff := (ipOff & IPOffMask) << 3 // Convert to bytes

		// If datagram marked as having more fragments
		// or if this is not the first fragment,
		// attempt reassembly; if it succeeds, proceed.
		// Reference: tinyemu-2019-12-21/slirp/ip_input.c:201-208
		if (m.Data[1]&1) != 0 || fragOff != 0 {
			reassembledIP, reassembledM := s.ipReass(m, fragOff, fragIPLen, ipID, ipProto, ipSrc, ipDst, fp)
			if reassembledIP == nil {
				return // Fragment stored, waiting for more
			}
			// Reassembly complete, continue processing with reassembled packet
			// Use the returned mbuf since the original may have been freed
			// Reference: tinyemu-2019-12-21/slirp/ip_input.c:205 (m = dtom(slirp, ip))
			m = reassembledM
			ipLen = int(reassembledIP.Len) + hlen
		} else {
			// Not actually fragmented (offset=0, MF=0), but had other flags
			if fp != nil {
				s.ipFreef(fp)
			}
		}
	}

	// Subtract header length from IP length for upper layers
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:211
	// Note: The C code modifies ip->ip_len in place. We store this for later use.
	// The protocol handlers expect ip_len to be the payload length.
	payloadLen := ipLen - hlen

	// Update the IP length field in the mbuf to reflect payload only
	// This matches C behavior where ip->ip_len -= hlen
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(payloadLen))

	// Also update ID and Off to host byte order in the packet
	// (C code does NTOHS on these fields)
	binary.BigEndian.PutUint16(m.Data[4:6], ipID)
	binary.BigEndian.PutUint16(m.Data[6:8], ipOff)

	// Strip IP options if present
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c (implicit - options handled before dispatch)
	if hlen > IPHeaderSize {
		IPStripOptions(m)
		hlen = IPHeaderSize
	}

	// Dispatch based on protocol
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:216-228
	proto := m.Data[9]
	switch proto {
	case IPProtoTCP:
		s.TCPInput(m, hlen)
	case IPProtoUDP:
		s.UDPInput(m, hlen)
	case IPProtoICMP:
		s.ICMPInput(m, hlen)
	default:
		m.Free()
	}
}

// IPSlowtimo handles IP timer processing.
// If a timer expires on a reassembly queue, discard it.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:442-461
func (s *Slirp) IPSlowtimo() {
	// Walk through reassembly queues and decrement TTL
	// If TTL reaches 0, free the fragments
	l := s.IPQ.IPLink.Next
	if l == nil {
		return
	}

	for l != &s.IPQ.IPLink {
		fp := qlinkToIPQ(l)
		l = l.Next
		fp.TTL--
		if fp.TTL == 0 {
			s.ipFreef(fp)
		}
	}
}

// qlinkToIPQ converts a QLink pointer to the containing IPQ.
// This is equivalent to container_of(l, struct ipq, ip_link) in C.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:44-46
func qlinkToIPQ(l *QLink) *IPQ {
	// In Go, we store the back-pointer in the IPQ structure
	// The IPQ is stored in the mbuf, and we need to find it
	// by traversing. For now, we'll use a different approach:
	// store the IPQ pointer in the QLink via an interface.
	// Actually, we'll make IPQ embed its own container reference.
	//
	// Since Go doesn't have container_of, we need to restructure.
	// The simplest approach: store IPQ pointer in a wrapper.
	return l.container.(*IPQ)
}

// qlinkToIPASFrag converts a QLink pointer to the containing IPASFrag.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:44-46
func qlinkToIPASFrag(l *QLink) *IPASFrag {
	return l.container.(*IPASFrag)
}

// ipFreef frees a fragment reassembly header and all associated datagrams.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:398-410
func (s *Slirp) ipFreef(fp *IPQ) {
	// Free all fragments in the queue
	for q := fp.FragLink.Next; q != &fp.FragLink; {
		// q.container should be an *IPASFrag
		frag, ok := q.container.(*IPASFrag)
		if !ok {
			// This shouldn't happen, but skip if not a fragment
			q = q.Next
			continue
		}
		q = q.Next
		ipDeq(frag)
		if frag.M != nil {
			frag.M.MFreem()
		}
	}
	// Remove from the reassembly queue list
	remque(&fp.IPLink)
	// Free the IPQ's mbuf
	if fp.M != nil {
		fp.M.Free()
	}
}

// ipEnq puts an IP fragment on a reassembly chain.
// Like insque, but pointers in middle of structure.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:416-425
func ipEnq(p *IPASFrag, prev *IPASFrag) {
	p.Link.Prev = &prev.Link
	p.Link.Next = prev.Link.Next
	if prev.Link.Next != nil {
		// Update the next node's prev pointer to point to p
		prev.Link.Next.Prev = &p.Link
	}
	prev.Link.Next = &p.Link
}

// ipDeq removes an IP fragment from a reassembly chain.
// To ip_enq as remque is to insque.
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:430-435
func ipDeq(p *IPASFrag) {
	// In C: ((struct ipasfrag *)(p->ipf_prev))->ipf_next = p->ipf_next;
	// The prev/next might be the sentinel (which is in IPQ, not IPASFrag)
	// We just need to update the links, not access the containing structure
	if p.Link.Prev != nil {
		p.Link.Prev.Next = p.Link.Next
	}
	if p.Link.Next != nil {
		p.Link.Next.Prev = p.Link.Prev
	}
}

// insque inserts element after head in the QLink list.
// Reference: tinyemu-2019-12-21/slirp/misc.c:19-29
func insque(element, head *QLink) {
	element.Next = head.Next
	head.Next = element
	element.Prev = head
	if element.Next != nil {
		element.Next.Prev = element
	}
}

// remque removes element from the QLink list.
// Reference: tinyemu-2019-12-21/slirp/misc.c:31-38
func remque(element *QLink) {
	if element.Next != nil {
		element.Next.Prev = element.Prev
	}
	if element.Prev != nil {
		element.Prev.Next = element.Next
	}
	element.Prev = nil
}

// ipReass takes an incoming datagram fragment and tries to reassemble it
// into a whole datagram. If a chain for reassembly of this datagram already
// exists, then it is given as fp; otherwise have to make a chain.
// Returns the reassembled IP header and its containing mbuf on success.
// The returned mbuf may be different from the input mbuf (which may have been freed).
// Reference: tinyemu-2019-12-21/slirp/ip_input.c:243-392
func (s *Slirp) ipReass(m *Mbuf, ipOff, ipLen, ipID uint16, ipProto uint8, ipSrc, ipDst net.IP, fp *IPQ) (*IP, *Mbuf) {
	hlen := int(m.Data[0]&0x0f) << 2

	// Presence of header sizes in mbufs would confuse code below.
	// Fragment m_data is concatenated.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:261-262
	dataLen := m.Len - hlen

	// Create fragment structure
	frag := &IPASFrag{
		Off: ipOff,
		Len: uint16(dataLen),
		TOS: m.Data[1], // TOS field, will store MF flag in low bit
		M:   m,
	}
	frag.Link.container = frag

	// Declare variables here to avoid goto jumping over declarations
	var q *IPASFrag
	var prev *IPASFrag

	// If first fragment to arrive, create a reassembly queue.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:267-283
	if fp == nil {
		t := s.MGet()
		if t == nil {
			m.MFreem()
			return nil, nil
		}
		fp = &IPQ{
			TTL:   IPFragTTL,
			Proto: ipProto,
			ID:    ipID,
			Src:   ipSrc,
			Dst:   ipDst,
			Slirp: s,
			M:     t,
		}
		fp.IPLink.container = fp
		fp.FragLink.container = fp
		// Initialize fragment list as empty
		fp.FragLink.Next = &fp.FragLink
		fp.FragLink.Prev = &fp.FragLink
		// Insert into reassembly queue list
		insque(&fp.IPLink, &s.IPQ.IPLink)

		// Insert as first fragment
		frag.Link.Next = &fp.FragLink
		frag.Link.Prev = &fp.FragLink
		fp.FragLink.Next = &frag.Link
		fp.FragLink.Prev = &frag.Link
		goto checkComplete
	}

	// Find a segment which begins after this one does.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:288-291
	for ql := fp.FragLink.Next; ql != &fp.FragLink; ql = ql.Next {
		q = qlinkToIPASFrag(ql)
		if q.Off > ipOff {
			break
		}
		prev = q
		q = nil
	}

	// If there is a preceding segment, it may provide some of
	// our data already. If so, drop the data from the incoming
	// segment. If it provides all of our data, drop us.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:297-308
	if prev != nil {
		i := int(prev.Off) + int(prev.Len) - int(ipOff)
		if i > 0 {
			if i >= dataLen {
				// Complete overlap, drop this fragment
				m.MFreem()
				return nil, nil
			}
			// Trim overlapping data from front by adjusting mbuf
			// Reference: tinyemu-2019-12-21/slirp/ip_input.c:304-306
			m.Adj(i)
			dataLen -= i
			ipOff += uint16(i)
			frag.Off = ipOff
			frag.Len = uint16(dataLen)
		}
	}

	// While we overlap succeeding segments trim them or,
	// if they are completely covered, dequeue them.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:313-326
	for ql := fp.FragLink.Next; ql != &fp.FragLink; {
		q = qlinkToIPASFrag(ql)
		if q.Off <= ipOff {
			ql = ql.Next
			continue
		}
		endOff := int(ipOff) + dataLen
		if endOff <= int(q.Off) {
			break
		}
		i := endOff - int(q.Off)
		if i < int(q.Len) {
			// Partial overlap, trim the following segment
			// Reference: tinyemu-2019-12-21/slirp/ip_input.c:317-320
			q.Len -= uint16(i)
			q.Off += uint16(i)
			if q.M != nil {
				q.M.Adj(i) // Match C: m_adj(dtom(slirp, q), i)
			}
			break
		}
		// Complete overlap, dequeue the following segment
		ql = ql.Next
		ipDeq(q)
		if q.M != nil {
			q.M.MFreem()
		}
	}

	// Insert new segment in its place
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:328-333
	if prev != nil {
		ipEnq(frag, prev)
	} else {
		// Insert at beginning (after FragLink sentinel)
		// Find the first real fragment to insert before
		if fp.FragLink.Next == &fp.FragLink {
			// Empty list
			frag.Link.Next = &fp.FragLink
			frag.Link.Prev = &fp.FragLink
			fp.FragLink.Next = &frag.Link
			fp.FragLink.Prev = &frag.Link
		} else {
			// Insert before first element
			first := qlinkToIPASFrag(fp.FragLink.Next)
			frag.Link.Next = &first.Link
			frag.Link.Prev = &fp.FragLink
			first.Link.Prev = &frag.Link
			fp.FragLink.Next = &frag.Link
		}
	}

checkComplete:
	// Check for complete reassembly.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:334-342
	next := 0
	var lastFrag *IPASFrag
	for ql := fp.FragLink.Next; ql != &fp.FragLink; ql = ql.Next {
		q := qlinkToIPASFrag(ql)
		if int(q.Off) != next {
			return nil, nil // Gap in fragments
		}
		next += int(q.Len)
		lastFrag = q
	}
	// Check if last fragment has MF cleared (TOS low bit stores MF)
	if lastFrag != nil && (lastFrag.TOS&1) != 0 {
		return nil, nil // More fragments expected
	}

	// Reassembly is complete; concatenate fragments.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:347-355
	firstFrag := qlinkToIPASFrag(fp.FragLink.Next)
	resultM := firstFrag.M

	// Copy data from first fragment starting after header
	firstHlen := int(resultM.Data[0]&0x0f) << 2
	// Shift data so it starts at the beginning (remove header for now, we'll add it back)
	copy(resultM.Data[firstHlen:], resultM.Data[firstHlen:resultM.Len])

	for ql := fp.FragLink.Next.Next; ql != &fp.FragLink; ql = ql.Next {
		q := qlinkToIPASFrag(ql)
		if q.M != nil {
			// Append fragment data (skip IP header in each fragment)
			fragHlen := int(q.M.Data[0]&0x0f) << 2
			fragData := q.M.Data[fragHlen:q.M.Len]
			resultM.Cat(q.M)
			_ = fragData // Cat will handle the concatenation
			q.M = nil    // Cat freed it
		}
	}

	// Create header for new IP packet by modifying header of first packet.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:363-386
	// Restore IP header with updated length
	binary.BigEndian.PutUint16(resultM.Data[2:4], uint16(next))
	resultM.Data[1] &= ^uint8(1) // Clear TOS low bit (MF flag storage)
	copy(resultM.Data[12:16], fp.Src.To4())
	copy(resultM.Data[16:20], fp.Dst.To4())

	// Remove reassembly queue
	remque(&fp.IPLink)
	// Free IPQ mbuf (fragments already freed via Cat or will be freed below)
	if fp.M != nil {
		fp.M.Free()
	}

	// Parse and return the reassembled IP header and its mbuf
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:205 (m = dtom(slirp, ip))
	ip, _ := ParseIP(resultM.Data[:resultM.Len])
	return ip, resultM
}
