package slirp

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"
)

// TCP input processing constants.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:44-51
const (
	TCPRexmtThresh = 3 // dup acks before fast retransmit
)

// TCPMaxSeg is the default maximum segment size.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:103
const TCPMaxSeg = IFMTU - 40 // MTU - IP header - TCP header

// TCPMaxWinShift is the maximum window shift.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:101
const TCPMaxWinShift = 14

// tcpSendSeqInit initializes send sequence numbers.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:166
func (tp *TCPCB) tcpSendSeqInit() {
	tp.SndUna = tp.Iss
	tp.SndNxt = tp.Iss
	tp.SndMax = tp.Iss
}

// tcpRcvSeqInit initializes receive sequence numbers.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:167
func (tp *TCPCB) tcpRcvSeqInit() {
	tp.RcvAdv = tp.Irs + 1
	tp.RcvNxt = tp.Irs + 1
}

// TCPSHaveRcvdSyn returns true if we have received SYN.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:139
func TCPSHaveRcvdSyn(state int16) bool {
	return state >= TCPSSynReceived
}

// TCPSHaveEstablished returns true if connection is established.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:140
func TCPSHaveEstablished(state int16) bool {
	return state >= TCPSEstablished
}

// TCPSHaveRcvdFin returns true if we have received FIN.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:141
func TCPSHaveRcvdFin(state int16) bool {
	return state >= TCPSTimeWait
}

// tcpfragListEmpty returns true if the reassembly queue is empty.
// Reference: tinyemu-2019-12-21/slirp/tcpip.h:66
// C macro: #define tcpfrag_list_empty(T) ((T)->seg_next == (struct tcpiphdr*)(T))
func (tp *TCPCB) tcpfragListEmpty() bool {
	// Handle uninitialized queue (nil pointers mean empty)
	if tp.ReassemblyHead.Next == nil {
		return true
	}
	return tp.ReassemblyHead.Next == &tp.ReassemblyHead
}

// tcpfragListFirst returns the first segment in the reassembly queue.
// Reference: tinyemu-2019-12-21/slirp/tcpip.h:64
// C macro: #define tcpfrag_list_first(T) qlink2tcpiphdr((T)->seg_next)
func (tp *TCPCB) tcpfragListFirst() *TCPIPHdr {
	return tp.ReassemblyHead.Next
}

// tcpfragListEnd returns true if ti is at the end of the reassembly queue
// (i.e., ti points to the sentinel/head).
// Reference: tinyemu-2019-12-21/slirp/tcpip.h:65
// C macro: #define tcpfrag_list_end(F, T) (tcpiphdr2qlink(F) == (struct qlink*)(T))
func (tp *TCPCB) tcpfragListEnd(ti *TCPIPHdr) bool {
	return ti == &tp.ReassemblyHead
}

// tcpfragRemove removes a segment from the reassembly queue.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:252 (remque call)
// This is equivalent to the BSD remque() function for doubly-linked lists.
func (tp *TCPCB) tcpfragRemove(ti *TCPIPHdr) {
	if ti == nil || ti == &tp.ReassemblyHead {
		return // Don't remove sentinel
	}
	// Unlink from doubly-linked list
	ti.Prev.Next = ti.Next
	ti.Next.Prev = ti.Prev
	// Clear links to help GC
	ti.Next = nil
	ti.Prev = nil
}

// tcpfragInsertAfter inserts a segment into the reassembly queue after the
// specified element. This is equivalent to BSD insque().
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:179 (insque call)
func (tp *TCPCB) tcpfragInsertAfter(ti *TCPIPHdr, after *TCPIPHdr) {
	ti.Next = after.Next
	ti.Prev = after
	after.Next.Prev = ti
	after.Next = ti
}

// soIsFConnecting sets socket to connecting state.
// Reference: tinyemu-2019-12-21/slirp/socket.c:658-663
func soIsFConnecting(so *Socket) {
	so.SoState &= ^(SSNoFDRef | SSIsFConnected | SSFCantRcvMore | SSFCantSendMore | SSFWDrain)
	so.SoState |= SSIsFConnecting
}

// soIsFConnected sets socket to connected state.
// Reference: tinyemu-2019-12-21/slirp/socket.c:666-670
func soIsFConnected(so *Socket) {
	so.SoState &= ^(SSIsFConnecting | SSFWDrain | SSNoFDRef)
	so.SoState |= SSIsFConnected
}

// tcpInputResult represents the result of tcp input processing.
type tcpInputResult int

const (
	tcpInputContinue tcpInputResult = iota
	tcpInputDrop
	tcpInputDropWithReset
	tcpInputDropAfterAck
	tcpInputReturn
)

// TCPInput processes an incoming TCP segment.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:216-1287
func (s *Slirp) TCPInput(m *Mbuf, iphlen int) {
	s.tcpInputImpl(m, iphlen, nil)
}

// tcpInputContinue continues TCP processing for a socket that just completed connection.
// This is called from handleConnectingSocket when a non-blocking connect completes.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:454 (tcp_input((struct mbuf *)NULL, sizeof(struct ip), so))
func (s *Slirp) tcpInputContinue(so *Socket) {
	s.tcpInputImpl(nil, 20, so) // 20 = sizeof(struct ip)
}

// tcpInputImpl is the main TCP input implementation.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:216-1287
func (s *Slirp) tcpInputImpl(m *Mbuf, iphlen int, inso *Socket) {
	// Declare all variables upfront to avoid goto issues
	var (
		tp            *TCPCB
		so            *Socket
		tiflags       int
		tiwin         uint32
		todrop        int
		acked         int
		ourfinisacked bool
		needoutput    bool
		iss           uint32
		off           int
		optp          []byte
		optlen        int
		tiSeq         uint32
		tiAck         uint32
		tiUrp         uint16
		tiSrc         uint32
		tiDst         uint32
		tiSport       uint16
		tiDport       uint16
		tiLen         int
		tcpLen        int
		savedHdr      *SavedTCPIPHeader
		win           int
		ret           int
		contInput     bool
		headerLen     int
		firstDataByte byte
	)

	// If called with m == nil, then we're continuing the connect
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:240-253
	if m == nil {
		so = inso
		tp = SoToTCPCB(so)
		m = so.SoM
		so.SoM = nil
		tiflags = int(so.SoTi[33])
		tiwin = uint32(binary.BigEndian.Uint16(so.SoTi[34:36]))
		tiSeq = binary.BigEndian.Uint32(so.SoTi[24:28])
		tiAck = binary.BigEndian.Uint32(so.SoTi[28:32])

		// Continue connection processing
		if (so.SoState & SSNoFDRef) != 0 {
			tp = tp.TCPClose()
			// In continuation path, savedHdr is nil (header wasn't saved with socket)
			s.tcpDropWithReset(nil, m, tiflags, tiSeq, tiAck, 0)
			return
		}
		s.tcpTemplate(tp)
		if iss != 0 {
			tp.Iss = iss
		} else {
			tp.Iss = s.TCPIss
		}
		s.TCPIss += TCPIssIncr / 2
		tp.Irs = tiSeq
		tp.tcpSendSeqInit()
		tp.tcpRcvSeqInit()
		tp.TFlags |= TFAckNow
		tp.TState = TCPSSynReceived
		tp.TTimer[TCPTKeep] = TCPTVKeepInit

		// trimthenstep6
		tiSeq++
		tiLen = m.Len
		if tiLen > int(tp.RcvWnd) {
			todrop = tiLen - int(tp.RcvWnd)
			m.Adj(-todrop)
			tiLen = int(tp.RcvWnd)
			tiflags &^= THFin
		}
		tp.SndWL1 = tiSeq - 1
		tp.RcvUp = tiSeq
		goto step6
	}

	// Get IP and TCP header together in first mbuf.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:260-264
	if m.Len < iphlen+20 {
		m.Free()
		return
	}

	// Strip IP options if present
	if iphlen > IPHeaderSize {
		IPStripOptions(m)
		iphlen = IPHeaderSize
	}

	// Extract TCP header fields (before saving header, to know header length)
	// Note: IPInput already modified m.Data[2:4] to contain the payload length
	// (IP total length - IP header length), which equals the TCP segment length.
	// Reference: tinyemu-2019-12-21/slirp/ip_input.c:211 (ip->ip_len -= hlen)
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:291 (tilen = ip->ip_len, already reduced)
	tiLen = int(binary.BigEndian.Uint16(m.Data[2:4]))

	// Check TCP offset early to know full header length for saving
	// Reference: tcp_input.c:293-296
	off = int(m.Data[iphlen+12]>>4) << 2
	if off < 20 || off > tiLen {
		m.Free()
		return
	}

	// Save a copy of the IP+TCP header for RST/ICMP error responses.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:268-274
	// The C code saves save_ip = *ip so it can restore the header for
	// tcp_respond() and icmp_error() at the dropwithreset label.
	headerLen = iphlen + off
	savedHdr = &SavedTCPIPHeader{
		Header:     make([]byte, headerLen),
		IPTotalLen: uint16(m.Len),
		TOS:        m.Data[1],
		SrcIP:      binary.BigEndian.Uint32(m.Data[12:16]),
		DstIP:      binary.BigEndian.Uint32(m.Data[16:20]),
		SrcPort:    binary.BigEndian.Uint16(m.Data[iphlen : iphlen+2]),
		DstPort:    binary.BigEndian.Uint16(m.Data[iphlen+2 : iphlen+4]),
	}
	copy(savedHdr.Header, m.Data[:headerLen])
	// Store the total IP length in the saved header (like C's save_ip.ip_len += iphlen)
	binary.BigEndian.PutUint16(savedHdr.Header[2:4], savedHdr.IPTotalLen)

	// Verify TCP checksum.
	// Reference: tcp_input.c:276-287
	// Build pseudo-header for checksum verification by modifying IP header in-place:
	// - Bytes 0-7: zeroed (overlay area)
	// - Byte 8: 0 (ih_x1)
	// - Byte 9: protocol (TCP = 6)
	// - Bytes 10-11: TCP length (header + data)
	// - Bytes 12-15: source IP (unchanged)
	// - Bytes 16-19: dest IP (unchanged)
	tcpLen = tiLen // TCP header + data length
	for i := 0; i < 8; i++ {
		m.Data[i] = 0
	}
	m.Data[8] = 0
	m.Data[9] = IPProtoTCP
	binary.BigEndian.PutUint16(m.Data[10:12], uint16(tcpLen))

	if Cksum(m, tcpLen+IPHeaderSize) != 0 {
		m.Free()
		return
	}

	// Restore IP header from saved copy for subsequent processing.
	copy(m.Data[:iphlen], savedHdr.Header[:iphlen])

	// Adjust tiLen to be segment data length (subtract TCP header with options)
	// Reference: tcp_input.c:297-298
	tiLen -= off

	// Get TCP options if any
	if off > 20 {
		optlen = off - 20
		optp = m.Data[iphlen+20 : iphlen+20+optlen]
	}
	tiflags = int(m.Data[iphlen+13])

	// Convert TCP protocol specific fields to host format.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:304-311
	tiSeq = binary.BigEndian.Uint32(m.Data[iphlen+4 : iphlen+8])
	tiAck = binary.BigEndian.Uint32(m.Data[iphlen+8 : iphlen+12])
	tiwin = uint32(binary.BigEndian.Uint16(m.Data[iphlen+14 : iphlen+16]))
	tiUrp = binary.BigEndian.Uint16(m.Data[iphlen+18 : iphlen+20])

	// Extract addresses and ports
	tiSrc = binary.BigEndian.Uint32(m.Data[12:16])
	tiDst = binary.BigEndian.Uint32(m.Data[16:20])
	tiSport = binary.BigEndian.Uint16(m.Data[iphlen : iphlen+2])
	tiDport = binary.BigEndian.Uint16(m.Data[iphlen+2 : iphlen+4])

	// Drop TCP, IP headers and TCP options from mbuf.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:313-317
	// C: m->m_data += hdrlen; m->m_len -= hdrlen;
	headerLen = iphlen + off
	m.Adj(headerLen)

	// Trace TCP input
	s.traceTCPInput(uint32ToIP(tiSrc), uint32ToIP(tiDst), tiSport, tiDport,
		uint8(tiflags), tiSeq, tiAck, tiLen)

	// Save first data byte for small packet escape char optimization
	// Reference: tcp_input.c:1243-1246
	if len(m.Data) > 0 {
		firstDataByte = m.Data[0]
	}

	// Check restricted mode
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:319-328
	if s.Restricted {
		found := false
		for ex := s.ExecList; ex != nil; ex = ex.ExNext {
			if ex.ExFPort == tiDport && ipToUint32(ex.ExAddr) == tiDst {
				found = true
				break
			}
		}
		if !found {
			m.Free()
			return
		}
	}

findso:
	// Locate pcb for segment.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:329-342
	so = s.TCPLastSo
	if so.SoFPort != tiDport || so.SoLPort != tiSport ||
		ipToUint32(so.SoLAddr) != tiSrc || ipToUint32(so.SoFAddr) != tiDst {
		so = SoLookup(&s.TCB, uint32ToIP(tiSrc), tiSport, uint32ToIP(tiDst), tiDport)
		if so != nil {
			s.TCPLastSo = so
		}
	}

	// If no socket found, create one for new SYN-only connections.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:344-381
	if so == nil {
		if (tiflags & (THSyn | THFin | THRst | THUrg | THAck)) != THSyn {
			s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
			return
		}

		so = s.SoCreate()
		if so == nil {
			s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
			return
		}
		if s.tcpAttach(so) < 0 {
			so.SoFree()
			s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
			return
		}

		so.SoSnd.SbReserve(TCPSndSpace)
		so.SoRcv.SbReserve(TCPRcvSpace)

		so.SoLAddr = uint32ToIP(tiSrc)
		so.SoLPort = tiSport
		so.SoFAddr = uint32ToIP(tiDst)
		so.SoFPort = tiDport

		so.SoIPTos = s.tcpTos(so)
		if so.SoIPTos == 0 {
			so.SoIPTos = savedHdr.TOS
		}

		tp = SoToTCPCB(so)
		tp.TState = TCPSListen
	}

	// If still-connecting socket, drop (probably a retransmit SYN).
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:383-389
	if (so.SoState & SSIsFConnecting) != 0 {
		m.Free()
		return
	}

	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:391-397
	tp = SoToTCPCB(so)
	if tp == nil {
		s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
		return
	}
	if tp.TState == TCPSClosed {
		m.Free()
		return
	}

	// Reset idle time and keep-alive timer.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:401-409
	tp.TIdle = 0
	if SOOptions {
		tp.TTimer[TCPTKeep] = TCPTVKeepIntvl
	} else {
		tp.TTimer[TCPTKeep] = TCPTVKeepIdle
	}

	// Process options if not in LISTEN state, else do it below (after getting
	// remote address).
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:411-416
	if optp != nil && tp.TState != TCPSListen {
		tp.tcpDooptions(optp, &TCPIPHdr{Flags: uint8(tiflags)})
	}

	// Header prediction: check for the two common cases of a uni-directional
	// data xfer. If the packet has no control flags, is in-sequence, the window
	// didn't change and we're not retransmitting, it's a candidate.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:418-507
	if tp.TState == TCPSEstablished &&
		(tiflags&(THSyn|THFin|THRst|THUrg|THAck)) == THAck &&
		tiSeq == tp.RcvNxt &&
		tiwin != 0 && tiwin == tp.SndWnd &&
		tp.SndNxt == tp.SndMax {

		if tiLen == 0 {
			// Pure ACK
			if seqGT(tiAck, tp.SndUna) && seqLEQ(tiAck, tp.SndMax) &&
				tp.SndCwnd >= tp.SndWnd {
				if tp.TRtt != 0 && seqGT(tiAck, tp.TRtSeq) {
					tp.tcpXmitTimer(tp.TRtt)
				}
				acked = int(tiAck - tp.SndUna)
				so.SoSnd.SbDrop(acked)
				tp.SndUna = tiAck
				m.Free()

				if tp.SndUna == tp.SndMax {
					tp.TTimer[TCPTRexmt] = 0
				} else if tp.TTimer[TCPTPersist] == 0 {
					tp.TTimer[TCPTRexmt] = tp.TRxtCur
				}

				if so.SoSnd.SbCC > 0 {
					tp.TCPOutput()
				}
				return
			}
		} else if tiAck == tp.SndUna && tp.tcpfragListEmpty() &&
			tiLen <= so.SoRcv.SbSpace() {
			// Pure in-sequence data packet
			// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:480-507
			tp.RcvNxt += uint32(tiLen)

			if so.SoEmu != 0 {
				if s.tcpEmu(so, m) {
					so.SbAppend(m)
				}
			} else {
				so.SbAppend(m)
			}

			// Set TF_ACKNOW for in-order data - don't delay ACKs to maximize throughput
			// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:505
			tp.TFlags |= TFAckNow
			tp.TCPOutput()
			return
		}
	}

	// Calculate receive window
	win = so.SoRcv.SbSpace()
	if win < 0 {
		win = 0
	}
	tp.RcvWnd = uint32(max(win, int(tp.RcvAdv-tp.RcvNxt)))

	// Main state machine
	switch tp.TState {
	case TCPSListen:
		result := s.tcpInputListen(tp, so, m, tiflags, tiSeq, tiAck, tiwin, tiUrp, tiLen, optp, savedHdr, &iss, &contInput)
		switch result {
		case tcpInputDrop:
			m.Free()
			return
		case tcpInputDropWithReset:
			s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
			return
		case tcpInputReturn:
			return
		}
		// Fall through to cont_input processing

		if contInput {
			s.tcpTemplate(tp)
			if optp != nil {
				tp.tcpDooptions(optp, &TCPIPHdr{Flags: uint8(tiflags)})
			}
			if iss != 0 {
				tp.Iss = iss
			} else {
				tp.Iss = s.TCPIss
			}
			s.TCPIss += TCPIssIncr / 2
			tp.Irs = tiSeq
			tp.tcpSendSeqInit()
			tp.tcpRcvSeqInit()
			tp.TFlags |= TFAckNow
			tp.TState = TCPSSynReceived
			tp.TTimer[TCPTKeep] = TCPTVKeepInit

			// trimthenstep6
			tiSeq++
			if tiLen > int(tp.RcvWnd) {
				todrop = tiLen - int(tp.RcvWnd)
				m.Adj(-todrop)
				tiLen = int(tp.RcvWnd)
				tiflags &^= THFin
			}
			tp.SndWL1 = tiSeq - 1
			tp.RcvUp = tiSeq
			goto step6
		}

	// SYN_SENT state: validate ACK, check RST/SYN, transition to ESTABLISHED
	// or SYN_RECEIVED.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:656-694
	case TCPSSynSent:
		if (tiflags&THAck) != 0 &&
			(seqLEQ(tiAck, tp.Iss) || seqGT(tiAck, tp.SndMax)) {
			s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
			return
		}

		if (tiflags & THRst) != 0 {
			if (tiflags & THAck) != 0 {
				tp.TCPDrop(0)
			}
			m.Free()
			return
		}

		if (tiflags & THSyn) == 0 {
			m.Free()
			return
		}
		if (tiflags & THAck) != 0 {
			tp.SndUna = tiAck
			if seqLT(tp.SndNxt, tp.SndUna) {
				tp.SndNxt = tp.SndUna
			}
		}

		tp.TTimer[TCPTRexmt] = 0
		tp.Irs = tiSeq
		tp.tcpRcvSeqInit()
		tp.TFlags |= TFAckNow
		if (tiflags&THAck) != 0 && seqGT(tp.SndUna, tp.Iss) {
			soIsFConnected(so)
			tp.TState = TCPSEstablished
			s.tcpReass(tp, nil, nil)
			if tp.TRtt != 0 {
				tp.tcpXmitTimer(tp.TRtt)
			}
		} else {
			tp.TState = TCPSSynReceived
		}

		// trimthenstep6
		tiSeq++
		if tiLen > int(tp.RcvWnd) {
			todrop = tiLen - int(tp.RcvWnd)
			m.Adj(-todrop)
			tiLen = int(tp.RcvWnd)
			tiflags &^= THFin
		}
		tp.SndWL1 = tiSeq - 1
		tp.RcvUp = tiSeq
		goto step6
	}

	// States other than LISTEN or SYN_SENT: check segment is within receive
	// window, drop leading data and SYN if needed.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:713-758
	todrop = int(tp.RcvNxt - tiSeq)
	if todrop > 0 {
		if (tiflags & THSyn) != 0 {
			tiflags &^= THSyn
			tiSeq++
			if tiUrp > 1 {
				tiUrp--
			} else {
				tiflags &^= THUrg
			}
			todrop--
		}

		if todrop > tiLen || (todrop == tiLen && (tiflags&THFin) == 0) {
			tiflags &^= THFin
			tp.TFlags |= TFAckNow
			todrop = tiLen
		}
		m.Adj(todrop)
		tiSeq += uint32(todrop)
		tiLen -= todrop
		if int(tiUrp) > todrop {
			tiUrp -= uint16(todrop)
		} else {
			tiflags &^= THUrg
			tiUrp = 0
		}
	}

	// If new data received on closed connection, RST.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:759-767
	if (so.SoState&SSNoFDRef) != 0 && tp.TState > TCPSCloseWait && tiLen > 0 {
		tp = tp.TCPClose()
		s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
		return
	}

	// Drop trailing data beyond window. Handle TIME_WAIT SYN and zero window.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:769-805
	todrop = int(tiSeq+uint32(tiLen)) - int(tp.RcvNxt+tp.RcvWnd)
	if todrop > 0 {
		if todrop >= tiLen {
			if (tiflags&THSyn) != 0 && tp.TState == TCPSTimeWait &&
				seqGT(tiSeq, tp.RcvNxt) {
				iss = tp.RcvNxt + TCPIssIncr
				tp = tp.TCPClose()
				goto findso
			}

			if tp.RcvWnd == 0 && tiSeq == tp.RcvNxt {
				tp.TFlags |= TFAckNow
			} else {
				// dropafterack
				if (tiflags & THRst) == 0 {
					m.Free()
					tp.TFlags |= TFAckNow
					tp.TCPOutput()
				} else {
					m.Free()
				}
				return
			}
		}
		m.Adj(-todrop)
		tiLen -= todrop
		tiflags &^= (THPush | THFin)
	}

	// Process RST.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:807-833
	if (tiflags & THRst) != 0 {
		switch tp.TState {
		case TCPSSynReceived, TCPSEstablished, TCPSFinWait1, TCPSFinWait2, TCPSCloseWait:
			tp.TState = TCPSClosed
			tp.TCPClose()
			m.Free()
			return
		case TCPSClosing, TCPSLastAck, TCPSTimeWait:
			tp.TCPClose()
			m.Free()
			return
		}
	}

	// SYN in window is error - send RST and drop connection.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:835-842
	if (tiflags & THSyn) != 0 {
		tp = tp.TCPDrop(0)
		s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
		return
	}

	// If ACK bit is off, drop segment and return.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:844-847
	if (tiflags & THAck) == 0 {
		m.Free()
		return
	}

	// ACK processing.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:848-968
	switch tp.TState {
	case TCPSSynReceived:
		// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:858-893
		if seqGT(tp.SndUna, tiAck) || seqGT(tiAck, tp.SndMax) {
			s.tcpDropWithReset(savedHdr, m, tiflags, tiSeq, tiAck, tiLen)
			return
		}
		tp.TState = TCPSEstablished
		tp.SndUna = tiAck

		if (so.SoState & SSCTL) != 0 {
			ret = TCPCtl(so)
			if ret == 1 {
				soIsFConnected(so)
				so.SoState &^= SSCTL
			} else if ret == 2 {
				so.SoState &= SSPersistentMask
				so.SoState |= SSNoFDRef
			} else {
				needoutput = true
				tp.TState = TCPSFinWait1
			}
		} else {
			soIsFConnected(so)
		}

		s.tcpReass(tp, nil, nil)
		tp.SndWL1 = tiSeq - 1
		// Skip dup ack processing since snd_una==ti_ack would look like dup ack.
		// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:891-892 "goto synrx_to_est"
		goto synrxToEst

	case TCPSEstablished, TCPSFinWait1, TCPSFinWait2, TCPSCloseWait, TCPSClosing, TCPSLastAck, TCPSTimeWait:
		// Duplicate ACK handling.
		// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:911-968
		if seqLEQ(tiAck, tp.SndUna) {
			if tiLen == 0 && tiwin == tp.SndWnd {
				// Duplicate ACK processing
				if tp.TTimer[TCPTRexmt] == 0 || tiAck != tp.SndUna {
					tp.TDupAcks = 0
				} else {
					tp.TDupAcks++
					if tp.TDupAcks == TCPRexmtThresh {
						onxt := tp.SndNxt
						win := min(tp.SndWnd, tp.SndCwnd) / 2 / uint32(tp.TMaxSeg)
						if win < 2 {
							win = 2
						}
						tp.SndSsthresh = win * uint32(tp.TMaxSeg)
						tp.TTimer[TCPTRexmt] = 0
						tp.TRtt = 0
						tp.SndNxt = tiAck
						tp.SndCwnd = uint32(tp.TMaxSeg)
						tp.TCPOutput()
						tp.SndCwnd = tp.SndSsthresh + uint32(tp.TMaxSeg)*uint32(tp.TDupAcks)
						if seqGT(onxt, tp.SndNxt) {
							tp.SndNxt = onxt
						}
						m.Free()
						return
					} else if tp.TDupAcks > TCPRexmtThresh {
						tp.SndCwnd += uint32(tp.TMaxSeg)
						tp.TCPOutput()
						m.Free()
						return
					}
				}
			} else {
				tp.TDupAcks = 0
			}
			goto step6
		}
	}

synrxToEst:
	// Retract congestion window if inflated.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:970-978
	if tp.TDupAcks > TCPRexmtThresh && tp.SndCwnd > tp.SndSsthresh {
		tp.SndCwnd = tp.SndSsthresh
	}
	tp.TDupAcks = 0
	if seqGT(tiAck, tp.SndMax) {
		// dropafterack
		// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:979-980
		if (tiflags & THRst) == 0 {
			m.Free()
			tp.TFlags |= TFAckNow
			tp.TCPOutput()
		} else {
			m.Free()
		}
		return
	}
	acked = int(tiAck - tp.SndUna)

	// Update RTT.
	// If transmit timer is running and timed sequence number was acked,
	// update smoothed round trip time. Cancel timer backoff (Karn's alg).
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:984-992
	if tp.TRtt != 0 && seqGT(tiAck, tp.TRtSeq) {
		tp.tcpXmitTimer(tp.TRtt)
	}

	// Stop or restart retransmit timer.
	// If all outstanding data is acked, stop retransmit timer.
	// Otherwise restart it using current (possibly backed-off) value.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:994-1004
	if tiAck == tp.SndMax {
		tp.TTimer[TCPTRexmt] = 0
		needoutput = true
	} else if tp.TTimer[TCPTPersist] == 0 {
		tp.TTimer[TCPTRexmt] = tp.TRxtCur
	}

	// Open congestion window.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1005-1019
	{
		cw := tp.SndCwnd
		incr := uint32(tp.TMaxSeg)
		if cw > tp.SndSsthresh {
			incr = incr * incr / cw
		}
		tp.SndCwnd = min(cw+incr, uint32(TCPMaxWin)<<tp.SndScale)
	}

	// Drop data from send buffer.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1020-1031
	if acked > so.SoSnd.SbCC {
		tp.SndWnd -= uint32(so.SoSnd.SbCC)
		so.SoSnd.SbDrop(so.SoSnd.SbCC)
		ourfinisacked = true
	} else {
		so.SoSnd.SbDrop(acked)
		tp.SndWnd -= uint32(acked)
		ourfinisacked = false
	}
	tp.SndUna = tiAck
	if seqLT(tp.SndNxt, tp.SndUna) {
		tp.SndNxt = tp.SndUna
	}

	// State transitions based on FIN.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1033-1091
	switch tp.TState {
	case TCPSFinWait1:
		if ourfinisacked {
			if (so.SoState & SSFCantRcvMore) != 0 {
				tp.TTimer[TCPT2MSL] = TCPMaxIdle
			}
			tp.TState = TCPSFinWait2
		}
	case TCPSClosing:
		if ourfinisacked {
			tp.TState = TCPSTimeWait
			tp.TCPCancelTimers()
			tp.TTimer[TCPT2MSL] = 2 * TCPTVMSL
		}
	case TCPSLastAck:
		if ourfinisacked {
			tp.TCPClose()
			m.Free()
			return
		}
	case TCPSTimeWait:
		tp.TTimer[TCPT2MSL] = 2 * TCPTVMSL
		// dropafterack
		// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1088-1090
		if (tiflags & THRst) == 0 {
			m.Free()
			tp.TFlags |= TFAckNow
			tp.TCPOutput()
		} else {
			m.Free()
		}
		return
	}

step6:
	// Update window information.
	// Don't look at window if no ACK: TAC's send garbage on first SYN.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1094-1109
	if (tiflags&THAck) != 0 &&
		(seqLT(tp.SndWL1, tiSeq) ||
			(tp.SndWL1 == tiSeq && (seqLT(tp.SndWL2, tiAck) ||
				(tp.SndWL2 == tiAck && tiwin > tp.SndWnd)))) {
		tp.SndWnd = tiwin
		tp.SndWL1 = tiSeq
		tp.SndWL2 = tiAck
		if tp.SndWnd > tp.MaxSndWnd {
			tp.MaxSndWnd = tp.SndWnd
		}
		needoutput = true
	}

	// Process URG.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1111-1155
	if (tiflags&THUrg) != 0 && tiUrp != 0 && !TCPSHaveRcvdFin(tp.TState) {
		// Validate urgent pointer doesn't exceed buffer capacity
		// Reference: tcp_input.c:1116-1126
		if int(tiUrp)+so.SoRcv.SbCC > so.SoRcv.SbDataLen {
			tiUrp = 0
			tiflags &^= THUrg
			// C uses "goto dodata" but we fall through to data processing
		} else if seqGT(tiSeq+uint32(tiUrp), tp.RcvUp) {
			// Update urgent pointer if it advances
			// Reference: tcp_input.c:1141-1145
			tp.RcvUp = tiSeq + uint32(tiUrp)
			so.SoUrgc = so.SoRcv.SbCC + int(tp.RcvUp-tp.RcvNxt)
		}
	} else {
		// Pull receive urgent pointer along with receive window
		// Reference: tcp_input.c:1148-1155
		if seqGT(tp.RcvNxt, tp.RcvUp) {
			tp.RcvUp = tp.RcvNxt
		}
	}

	// Process segment data.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1158-1172 (dodata label)
	// Also: tcp_input.c:83-99 (TCP_REASS macro)
	// The macro handles the common case inline (segment is next expected on an
	// established connection with empty reassembly queue), avoiding linkage into
	// and removal from the queue. Set DELACK for in-order segments, but ack
	// immediately when segments are out of order (so fast retransmit can work).
	if (tiLen > 0 || (tiflags&THFin) != 0) && !TCPSHaveRcvdFin(tp.TState) {
		// TCP_REASS inline fast path: in-order segment on established
		// connection with empty reassembly queue
		// Reference: tcp_input.c:84-86
		if tiSeq == tp.RcvNxt && tp.tcpfragListEmpty() && tp.TState == TCPSEstablished {
			// Fast path: set TF_DELACK (not TF_ACKNOW)
			// Reference: tcp_input.c:87
			tp.TFlags |= TFDelAck
			// Advance rcv_nxt by data length
			// Reference: tcp_input.c:88
			tp.RcvNxt += uint32(tiLen)
			// Extract TH_FIN flag for later processing
			// Reference: tcp_input.c:89
			tiflags = tiflags & THFin
			// Handle emulation and append data to socket buffer
			// Reference: tcp_input.c:90-93
			if so.SoEmu != 0 {
				if s.tcpEmu(so, m) {
					so.SbAppend(m)
				}
			} else {
				so.SbAppend(m)
			}
		} else {
			// Slow path: out-of-order or complex case - call tcp_reass
			// Set TF_ACKNOW to force immediate ACK (for fast retransmit)
			// Reference: tcp_input.c:94-96
			tiflags = s.tcpReass(tp, &TCPIPHdr{
				Seq:   tiSeq,
				Flags: uint8(tiflags),
				TiLen: tiLen,
				Mbuf:  m,
			}, m)
			tp.TFlags |= TFAckNow
		}
	} else {
		m.Free()
		tiflags &^= THFin
	}

	// Process FIN.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1174-1234
	if (tiflags & THFin) != 0 {
		if !TCPSHaveRcvdFin(tp.TState) {
			// Set SS_FDRAIN and shutdown socket if no rx data buffered.
			// Reference: tcp_input.c:1180-1192
			so.SoFWDrain()
			tp.TFlags |= TFAckNow
			tp.RcvNxt++
		}
		switch tp.TState {
		// SYN_RECEIVED/ESTABLISHED -> CLOSE_WAIT (or LAST_ACK for EMU_CTL)
		// Reference: tcp_input.c:1200-1206
		case TCPSSynReceived, TCPSEstablished:
			if so.SoEmu == EmuCTL {
				tp.TState = TCPSLastAck
			} else {
				tp.TState = TCPSCloseWait
			}
		// FIN_WAIT_1 -> CLOSING
		// Reference: tcp_input.c:1212-1214
		case TCPSFinWait1:
			tp.TState = TCPSClosing
		// FIN_WAIT_2 -> TIME_WAIT
		// Reference: tcp_input.c:1221-1225
		case TCPSFinWait2:
			tp.TState = TCPSTimeWait
			tp.TCPCancelTimers()
			tp.TTimer[TCPT2MSL] = 2 * TCPTVMSL
		// TIME_WAIT: restart 2MSL timer
		// Reference: tcp_input.c:1230-1232
		case TCPSTimeWait:
			tp.TTimer[TCPT2MSL] = 2 * TCPTVMSL
		}
	}

	// Small packet escape char ACK optimization.
	// If this is a small packet (1-5 bytes) starting with escape char (0x1B),
	// ACK immediately. This improves interactive response time for terminal
	// applications with Nagle congestion avoidance.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1236-1246
	if tiLen > 0 && tiLen <= 5 && firstDataByte == 27 {
		tp.TFlags |= TFAckNow
	}

	// Return any desired output.
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1248-1254
	if needoutput || (tp.TFlags&TFAckNow) != 0 {
		tp.TCPOutput()
	}
}

// tcpInputListen handles TCP input in LISTEN state.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:538-642
func (s *Slirp) tcpInputListen(tp *TCPCB, so *Socket, m *Mbuf, tiflags int, tiSeq, tiAck, tiwin uint32, tiUrp uint16, tiLen int, optp []byte, savedHdr *SavedTCPIPHeader, iss *uint32, contInput *bool) tcpInputResult {
	if (tiflags & THRst) != 0 {
		return tcpInputDrop
	}
	if (tiflags & THAck) != 0 {
		return tcpInputDropWithReset
	}
	if (tiflags & THSyn) == 0 {
		return tcpInputDrop
	}

	// Check if destined for control address
	vnetMask := ipToUint32(s.VNetworkMask)
	vnetAddr := ipToUint32(s.VNetworkAddr)
	vhostAddr := ipToUint32(s.VHostAddr)
	vnsAddr := ipToUint32(s.VNameserverAddr)

	if (ipToUint32(so.SoFAddr) & vnetMask) == vnetAddr {
		if ipToUint32(so.SoFAddr) != vhostAddr &&
			ipToUint32(so.SoFAddr) != vnsAddr {
			for ex := s.ExecList; ex != nil; ex = ex.ExNext {
				if ex.ExFPort == so.SoFPort &&
					ipToUint32(so.SoFAddr) == ipToUint32(ex.ExAddr) {
					so.SoState |= SSCTL
					break
				}
			}
			if (so.SoState & SSCTL) != 0 {
				*contInput = true
				return tcpInputContinue
			}
		}
	}

	// Skip noconnect emulation
	// Reference: tcp_input.c:576-579
	if (so.SoEmu & EmuNoConnect) != 0 {
		so.SoEmu &^= EmuNoConnect
		*contInput = true
		return tcpInputContinue
	}

	// Try to connect to the foreign host
	// Reference: tcp_input.c:581-614
	connectResult, connectErr := s.tcpFConnect(so)
	if connectResult == -1 && connectErr != syscall.EINPROGRESS && connectErr != syscall.EWOULDBLOCK {
		// Connection failed - handle error
		// Reference: tcp_input.c:582-601
		if connectErr == syscall.ECONNREFUSED {
			// ACK the SYN, send RST to refuse the connection
			// Reference: tcp_input.c:585-588
			// tcp_respond(tp, ti, m, ti->ti_seq+1, (tcp_seq)0, TH_RST|TH_ACK)
			s.tcpRespondWithHeader(savedHdr, tiSeq+1, 0, THRst|THAck)
		} else {
			// Send ICMP error
			// Reference: tcp_input.c:589-598
			code := uint8(ICMPUnreachNet)
			if connectErr == syscall.EHOSTUNREACH {
				code = ICMPUnreachHost
			}
			// Restore the original packet for ICMP error
			// Reference: tcp_input.c:591-598
			icmpM := s.restoreMbufForICMP(savedHdr, m, tiSeq, tiAck, tiwin, tiUrp)
			if icmpM != nil {
				errMsg := ""
				if connectErr != nil {
					errMsg = connectErr.Error()
				}
				s.ICMPError(icmpM, ICMPUnreach, code, 0, errMsg)
				icmpM.Free()
			}
		}
		tp.TCPClose()
		m.Free()
		return tcpInputReturn
	}

	// Connection in progress - save state
	// Reference: tcp_input.c:602-613
	so.SoM = m
	so.SoTi = make([]byte, 40)
	// Save the TCP/IP header info we'll need later
	binary.BigEndian.PutUint32(so.SoTi[24:28], tiSeq)
	binary.BigEndian.PutUint32(so.SoTi[28:32], tiAck)
	so.SoTi[33] = uint8(tiflags)
	binary.BigEndian.PutUint16(so.SoTi[34:36], uint16(tiwin))

	// Set up the template now so that retransmit timers can output correctly.
	// In the C code, tcp_template is called later when the connection completes,
	// but we need it earlier because Go's timer handling may call TCPOutput
	// before the connect completion is processed.
	s.tcpTemplate(tp)
	tp.Iss = s.TCPIss
	s.TCPIss += TCPIssIncr / 2
	tp.Irs = tiSeq
	tp.tcpSendSeqInit()
	tp.tcpRcvSeqInit()
	tp.TFlags |= TFAckNow

	tp.TTimer[TCPTKeep] = TCPTVKeepInit
	tp.TState = TCPSSynReceived
	return tcpInputReturn
}

// tcpDropWithReset sends an RST and drops the connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1268-1278
//
// The savedHdr parameter contains the preserved IP+TCP header data needed
// to construct the RST response. It may be nil in cases where the header
// was not preserved (e.g., continuation path with m == nil).
//
// Per C code at dropwithreset:
//   - If TH_ACK is set: tcp_respond(tp, ti, m, 0, ti->ti_ack, TH_RST)
//   - Otherwise: tcp_respond(tp, ti, m, ti->ti_seq+ti->ti_len, 0, TH_RST|TH_ACK)
//     (with ti->ti_len++ if TH_SYN is set)
func (s *Slirp) tcpDropWithReset(savedHdr *SavedTCPIPHeader, m *Mbuf, tiflags int, tiSeq, tiAck uint32, tiLen int) {
	// Free the mbuf since we can't reuse it (headers are stripped)
	if m != nil {
		m.Free()
	}

	// Cannot send RST without saved header data
	// Reference: tcp_input.c:1268-1278
	if savedHdr == nil {
		return
	}

	// Reference: tcp_input.c:1270-1277
	// Three cases for RST sending:
	// 1. TH_ACK set: tcp_respond(tp, ti, m, 0, ti->ti_ack, TH_RST)
	// 2. TH_SYN set: ti->ti_len++, then tcp_respond with ti->ti_seq+ti->ti_len
	// 3. Neither: tcp_respond(tp, ti, m, ti->ti_seq+ti->ti_len, 0, TH_RST|TH_ACK)
	if (tiflags & THAck) != 0 {
		// Case 1: ACK set - respond with RST, using their ack as our seq
		s.tcpRespondWithHeader(savedHdr, 0, tiAck, THRst)
	} else {
		// Case 2 & 3: No ACK - respond with RST|ACK
		seq := tiSeq + uint32(tiLen)
		if (tiflags & THSyn) != 0 {
			// SYN counts as one sequence number
			seq++
		}
		s.tcpRespondWithHeader(savedHdr, seq, 0, THRst|THAck)
	}
}

// tcpRespondWithHeader sends a TCP response using saved header data.
// This is used when the original mbuf headers have been stripped and we need
// to send RST or other responses using the preserved header information.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:102-167
func (s *Slirp) tcpRespondWithHeader(savedHdr *SavedTCPIPHeader, ack, seq uint32, flags int) {
	if savedHdr == nil {
		return
	}

	// Create a new mbuf for the response
	m := s.MGet()
	if m == nil {
		return
	}

	// Allocate space for link header + IP + TCP (no options, no data)
	// Reference: tcp_subr.c:127-131
	const tcpipHdrLen = 40 // 20 bytes IP + 20 bytes TCP
	m.SetBuffer(make([]byte, IFMaxLinkHdr+tcpipHdrLen))
	m.Len = tcpipHdrLen

	// Build header at offset IFMaxLinkHdr, then adjust
	ti := m.Data[IFMaxLinkHdr:]

	// Build IP header (swap src/dst from saved header)
	// Reference: tcp_subr.c:136-137 (xchg macro)
	ti[0] = 0x45                                          // version 4, IHL 5
	ti[1] = 0                                             // TOS
	binary.BigEndian.PutUint16(ti[2:4], tcpipHdrLen)      // total length
	binary.BigEndian.PutUint16(ti[4:6], 0)                // IP ID (will be set by ipOutput)
	binary.BigEndian.PutUint16(ti[6:8], 0)                // fragment offset
	ti[8] = MaxTTL                                        // TTL (RST uses MAXTTL)
	ti[9] = IPProtoTCP                                    // protocol
	binary.BigEndian.PutUint16(ti[10:12], 0)              // IP checksum (computed by ipOutput)
	binary.BigEndian.PutUint32(ti[12:16], savedHdr.DstIP) // src = original dst (swapped)
	binary.BigEndian.PutUint32(ti[16:20], savedHdr.SrcIP) // dst = original src (swapped)

	// Build TCP header (swap src/dst ports)
	// Reference: tcp_subr.c:136-137 (xchg macro)
	binary.BigEndian.PutUint16(ti[20:22], savedHdr.DstPort) // src port = original dst (swapped)
	binary.BigEndian.PutUint16(ti[22:24], savedHdr.SrcPort) // dst port = original src (swapped)
	binary.BigEndian.PutUint32(ti[24:28], seq)              // sequence number
	binary.BigEndian.PutUint32(ti[28:32], ack)              // acknowledgment number
	ti[32] = 5 << 4                                         // data offset (5 * 4 = 20 bytes, no options)
	ti[33] = uint8(flags)                                   // TCP flags
	binary.BigEndian.PutUint16(ti[34:36], 0)                // window (0 for RST)
	binary.BigEndian.PutUint16(ti[36:38], 0)                // checksum (computed below)
	binary.BigEndian.PutUint16(ti[38:40], 0)                // urgent pointer

	// Build TCP pseudo-header for checksum calculation
	// Reference: RFC 793, tcp_subr.c:141-154
	// Pseudo-header: src IP (4) + dst IP (4) + zero (1) + protocol (1) + TCP length (2) = 12 bytes
	pseudoHdr := make([]byte, 12)
	binary.BigEndian.PutUint32(pseudoHdr[0:4], savedHdr.DstIP) // src IP (swapped)
	binary.BigEndian.PutUint32(pseudoHdr[4:8], savedHdr.SrcIP) // dst IP (swapped)
	pseudoHdr[8] = 0                                           // zero
	pseudoHdr[9] = IPProtoTCP                                  // protocol
	binary.BigEndian.PutUint16(pseudoHdr[10:12], 20)           // TCP length (header only)

	// Calculate TCP checksum over pseudo-header + TCP header
	var sum uint32
	sum = CksumPartial(pseudoHdr)
	sum += CksumPartial(ti[20:40]) // TCP header only

	// Fold and complement
	tcpSum := CksumFinish(sum)
	binary.BigEndian.PutUint16(ti[36:38], tcpSum)

	// Adjust mbuf to point to IP header start
	// C: m->m_data = (caddr_t)ti;
	m.SetDataOffset(m.Offset + IFMaxLinkHdr)
	m.Len = tcpipHdrLen

	// TTL is already set to MaxTTL for RST packets
	// Reference: tcp_subr.c:157-160

	// Send the packet
	// Reference: tcp_subr.c:162
	s.ipOutput(nil, m)
}

// restoreMbufForICMP creates an mbuf with restored IP+TCP headers for ICMP error.
// This is used when tcp_fconnect fails and we need to send an ICMP error.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:591-598
//
// The C code does:
//
//	HTONL(ti->ti_seq);             /* restore tcp header */
//	HTONL(ti->ti_ack);
//	HTONS(ti->ti_win);
//	HTONS(ti->ti_urp);
//	m->m_data -= sizeof(struct tcpiphdr)+off-sizeof(struct tcphdr);
//	m->m_len  += sizeof(struct tcpiphdr)+off-sizeof(struct tcphdr);
//	*ip=save_ip;
//	icmp_error(m, ICMP_UNREACH,code, 0,strerror(errno));
func (s *Slirp) restoreMbufForICMP(savedHdr *SavedTCPIPHeader, origM *Mbuf, tiSeq, tiAck, tiwin uint32, tiUrp uint16) *Mbuf {
	if savedHdr == nil {
		return nil
	}

	// Create a new mbuf with the restored header
	m := s.MGet()
	if m == nil {
		return nil
	}

	// Calculate header length from saved header
	headerLen := len(savedHdr.Header)
	if headerLen < 40 { // minimum IP (20) + TCP (20)
		m.Free()
		return nil
	}

	// ICMPError expects the original IP packet in the mbuf
	// We need: IP header + TCP header (with fields in network order) + some payload
	// Reference: tcp_input.c:595-597 - restore m_data and m_len to include full header

	// Allocate space for full header plus some payload bytes
	payloadLen := origM.Len
	if payloadLen > 8 {
		payloadLen = 8 // ICMP only needs 8 bytes of payload
	}
	totalLen := headerLen + payloadLen
	m.SetBuffer(make([]byte, totalLen))
	m.Len = totalLen

	// Copy the saved header (this is the original IP+TCP header before processing)
	// Reference: tcp_input.c:597 - *ip=save_ip
	copy(m.Data[:headerLen], savedHdr.Header)

	// The saved header has TCP seq/ack/win/urp in their original network byte order,
	// but we processed them to host order and may have modified them.
	// The C code converts them back to network order before icmp_error.
	// Reference: tcp_input.c:591-594
	// However, our savedHdr.Header contains the original bytes, so we should
	// use those directly. The tiSeq etc. passed to us are the host-order values
	// that we need to put back in network order.

	// Restore TCP header fields to network byte order
	// TCP seq is at IP header (20) + offset 4 = offset 24 in typical packet
	ipHdrLen := int(m.Data[0]&0x0F) << 2
	if ipHdrLen < 20 {
		ipHdrLen = 20
	}
	tcpOffset := ipHdrLen

	// Write the (potentially modified) TCP fields back in network order
	// Reference: tcp_input.c:591-594
	binary.BigEndian.PutUint32(m.Data[tcpOffset+4:tcpOffset+8], tiSeq)
	binary.BigEndian.PutUint32(m.Data[tcpOffset+8:tcpOffset+12], tiAck)
	binary.BigEndian.PutUint16(m.Data[tcpOffset+14:tcpOffset+16], uint16(tiwin))
	binary.BigEndian.PutUint16(m.Data[tcpOffset+18:tcpOffset+20], tiUrp)

	// Copy payload if any
	if payloadLen > 0 && len(origM.Data) >= payloadLen {
		copy(m.Data[headerLen:], origM.Data[:payloadLen])
	}

	return m
}

// tcpReass handles TCP reassembly.
// Returns TH_FIN if reassembly now includes a segment with FIN.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:104-209
func (s *Slirp) tcpReass(tp *TCPCB, ti *TCPIPHdr, m *Mbuf) int {
	so := tp.TSocket
	var flags int
	var q *TCPIPHdr

	// Call with ti==NULL after become established to
	// force pre-ESTABLISHED data up to user socket.
	// Reference: tcp_input.c:112-117
	if ti == nil {
		goto present
	}

	// Find a segment which begins after this one does.
	// Reference: tcp_input.c:119-125
	for q = tp.tcpfragListFirst(); !tp.tcpfragListEnd(q); q = q.Next {
		if seqGT(q.Seq, ti.Seq) {
			break
		}
	}

	// If there is a preceding segment, it may provide some of
	// our data already. If so, drop the data from the incoming
	// segment. If it provides all of our data, drop us.
	// Reference: tcp_input.c:127-153
	if !tp.tcpfragListEnd(q.Prev) {
		qPrev := q.Prev
		// conversion to int (in i) handles seq wraparound
		i := int(int32(qPrev.Seq) + int32(qPrev.TiLen) - int32(ti.Seq))
		if i > 0 {
			if i >= ti.TiLen {
				m.MFree()
				// Try to present any queued data
				// at the left window edge to the user.
				// This is needed after the 3-WHS completes.
				goto present
			}
			m.Adj(i)
			ti.TiLen -= i
			ti.Seq += uint32(i)
		}
		q = qPrev.Next
	}
	ti.Mbuf = m

	// While we overlap succeeding segments trim them or,
	// if they are completely covered, dequeue them.
	// Reference: tcp_input.c:156-174
	for !tp.tcpfragListEnd(q) {
		i := int(int32(ti.Seq) + int32(ti.TiLen) - int32(q.Seq))
		if i <= 0 {
			break
		}
		if i < q.TiLen {
			q.Seq += uint32(i)
			q.TiLen -= i
			q.Mbuf.Adj(i)
			break
		}
		q = q.Next
		mToFree := q.Prev.Mbuf
		tp.tcpfragRemove(q.Prev)
		mToFree.MFree()
	}

	// Stick new segment in its place.
	// Reference: tcp_input.c:176-179
	tp.tcpfragInsertAfter(ti, q.Prev)

present:
	// Present data to user, advancing rcv_nxt through
	// completed sequence space.
	// Reference: tcp_input.c:181-209
	if !TCPSHaveEstablished(tp.TState) {
		return 0
	}
	ti = tp.tcpfragListFirst()
	if tp.tcpfragListEnd(ti) || ti.Seq != tp.RcvNxt {
		return 0
	}
	// In SYN_RECEIVED state, don't present data even if contiguous
	// Reference: tcp_input.c:191-192
	if tp.TState == TCPSSynReceived && ti.TiLen != 0 {
		return 0
	}
	for {
		tp.RcvNxt += uint32(ti.TiLen)
		flags = int(ti.Flags) & THFin
		mData := ti.Mbuf
		// Save next pointer before removing (tcpfragRemove clears ti.Next)
		nextTi := ti.Next
		tp.tcpfragRemove(ti)
		ti = nextTi
		// Handle SS_FCANTSENDMORE by freeing data instead of appending
		// Reference: tcp_input.c:199-200
		if (so.SoState & SSFCantSendMore) != 0 {
			mData.MFree()
		} else {
			if so.SoEmu != 0 {
				if s.tcpEmu(so, mData) {
					so.SbAppend(mData)
				}
			} else {
				so.SbAppend(mData)
			}
		}
		// Continue while not at sentinel and next segment is contiguous
		// Reference: tcp_input.c:207 - while condition
		if tp.tcpfragListEnd(ti) || ti.Seq != tp.RcvNxt {
			break
		}
	}
	return flags
}

// tcpTemplate creates a template TCP/IP header for a connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:64-87
func (s *Slirp) tcpTemplate(tp *TCPCB) {
	so := tp.TSocket
	n := &tp.TTemplate

	n.Mbuf = nil
	n.X1 = 0
	n.Pr = IPProtoTCP
	n.Len = 20
	n.Src = ipToUint32(so.SoFAddr)
	n.Dst = ipToUint32(so.SoLAddr)
	n.Sport = so.SoFPort
	n.Dport = so.SoLPort
	n.Seq = 0
	n.Ack = 0
	n.OffX2 = 5 << 4
	n.Flags = 0
	n.Win = 0
	n.Sum = 0
	n.Urp = 0
}

// tcpRespond sends a TCP response.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:102-167
func (s *Slirp) tcpRespond(tp *TCPCB, m *Mbuf, ack, seq uint32, flags int) {
	var win int
	if tp != nil {
		win = tp.TSocket.SoRcv.SbSpace()
	}

	var tlen int
	if m == nil || cap(m.Data) < 40 {
		// No mbuf or mbuf too small - create new one
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:119-126
		if tp == nil && m == nil {
			return
		}
		newM := s.MGet()
		if newM == nil {
			if m != nil {
				m.Free()
			}
			return
		}
		if m != nil {
			m.Free()
		}
		m = newM
		tlen = 0
		m.SetBuffer(make([]byte, IFMaxLinkHdr+40))
		// C: m->m_data += IF_MAXLINKHDR - advance data pointer past link header
		m.SetDataOffset(IFMaxLinkHdr)
		m.Len = 40
		if tp != nil {
			// C: *mtod(m, struct tcpiphdr *) = *ti - copy template
			s.marshalTCPIPHdrTo(&tp.TTemplate, m.Data)
		}
		flags = THAck
	} else {
		// Restore original mbuf with full data
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:127-139
		// C: m->m_data = (caddr_t)ti - point to header
		m.Len = 40
		tlen = 0

		// C: xchg(ti->ti_dst.s_addr, ti->ti_src.s_addr, uint32_t)
		// Swap src/dst IP (offsets 12-15 and 16-19 in IP header)
		srcIP := binary.BigEndian.Uint32(m.Data[12:16])
		dstIP := binary.BigEndian.Uint32(m.Data[16:20])
		binary.BigEndian.PutUint32(m.Data[12:16], dstIP)
		binary.BigEndian.PutUint32(m.Data[16:20], srcIP)

		// C: xchg(ti->ti_dport, ti->ti_sport, uint16_t)
		// Swap src/dst port (offsets 20-21 and 22-23 in TCP header)
		srcPort := binary.BigEndian.Uint16(m.Data[20:22])
		dstPort := binary.BigEndian.Uint16(m.Data[22:24])
		binary.BigEndian.PutUint16(m.Data[20:22], dstPort)
		binary.BigEndian.PutUint16(m.Data[22:24], srcPort)
	}

	// Now ti points to the IP+TCP header (m.Data after potential offset adjustment)
	ti := m.Data

	// C: ti->ti_len = htons((u_short)(sizeof (struct tcphdr) + tlen))
	// ti_len is ih_len at offset 10-11 in the pseudo-header overlay
	// This overwrites ip_sum for checksum calculation
	binary.BigEndian.PutUint16(ti[10:12], 20+uint16(tlen))
	tlen += 40
	m.Len = tlen

	// C: ti->ti_mbuf = NULL; ti->ti_x1 = 0
	// Clear pseudo-header fields for checksum calculation.
	// ti_mbuf (bytes 0-7) overlays the first 8 bytes of the IP header.
	// ti_x1 (byte 8) overlays ip_ttl.
	// These must be 0 for correct TCP checksum calculation.
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:69-72
	ti[0] = 0
	ti[1] = 0
	binary.BigEndian.PutUint16(ti[2:4], 0)
	binary.BigEndian.PutUint16(ti[4:6], 0)
	binary.BigEndian.PutUint16(ti[6:8], 0)
	ti[8] = 0

	// C: ti->ti_seq = htonl(seq); ti->ti_ack = htonl(ack)
	binary.BigEndian.PutUint32(ti[24:28], seq)
	binary.BigEndian.PutUint32(ti[28:32], ack)

	// C: ti->ti_x2 = 0; ti->ti_off = sizeof (struct tcphdr) >> 2
	ti[32] = 5 << 4 // data offset = 5 (20 bytes), x2 = 0

	// C: ti->ti_flags = flags
	ti[33] = uint8(flags)

	// C: ti->ti_win = htons((uint16_t) (win >> tp->rcv_scale))
	if tp != nil {
		binary.BigEndian.PutUint16(ti[34:36], uint16(win>>tp.RcvScale))
	} else {
		binary.BigEndian.PutUint16(ti[34:36], uint16(win))
	}

	// C: ti->ti_urp = 0; ti->ti_sum = 0
	binary.BigEndian.PutUint16(ti[38:40], 0)
	binary.BigEndian.PutUint16(ti[36:38], 0)

	// C: ti->ti_sum = cksum(m, tlen)
	sum := Cksum(m, tlen)
	binary.BigEndian.PutUint16(ti[36:38], sum)

	// C: ((struct ip *)ti)->ip_len = tlen
	// Restore ip_len at offset 2-3
	binary.BigEndian.PutUint16(ti[2:4], uint16(tlen))

	// C: ((struct ip *)ti)->ip_ttl = (flags & TH_RST) ? MAXTTL : IPDEFTTL
	// Restore ip_ttl at offset 8 (was zeroed for checksum)
	if (flags & THRst) != 0 {
		ti[8] = MaxTTL
	} else {
		ti[8] = IPDefTTL
	}

	// C: (void) ip_output((struct socket *)0, m)
	s.ipOutput(nil, m)
}

// marshalTCPIPHdrTo converts a TCPIPHdr to bytes in the given buffer.
// For TCP checksum calculation, bytes 0-7 must be 0 (they overlay ti_mbuf
// in the pseudo-header structure). The actual IP header fields are set later.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:69-72 (ti_mbuf = NULL, ti_x1 = 0)
func (s *Slirp) marshalTCPIPHdrTo(ti *TCPIPHdr, buf []byte) {
	buf[0] = 0                              // ti_mbuf[0] = 0 (overlays ip version/IHL, set by ipOutput)
	buf[1] = 0                              // ti_mbuf[1] = 0 (overlays ip TOS)
	binary.BigEndian.PutUint16(buf[4:6], 0) // ti_mbuf[4-5] = 0 (overlays ID)
	binary.BigEndian.PutUint16(buf[6:8], 0) // ti_mbuf[6-7] = 0 (overlays flags + frag)
	buf[8] = 0                              // ti_x1 = 0 for TCP checksum, TTL set later
	buf[9] = IPProtoTCP
	binary.BigEndian.PutUint16(buf[10:12], 0)
	binary.BigEndian.PutUint32(buf[12:16], ti.Src)
	binary.BigEndian.PutUint32(buf[16:20], ti.Dst)
	binary.BigEndian.PutUint16(buf[20:22], ti.Sport)
	binary.BigEndian.PutUint16(buf[22:24], ti.Dport)
	binary.BigEndian.PutUint32(buf[24:28], ti.Seq)
	binary.BigEndian.PutUint32(buf[28:32], ti.Ack)
	buf[32] = ti.OffX2
	buf[33] = ti.Flags
	binary.BigEndian.PutUint16(buf[34:36], ti.Win)
	binary.BigEndian.PutUint16(buf[36:38], ti.Sum)
	binary.BigEndian.PutUint16(buf[38:40], ti.Urp)
}

// tcpAttach attaches a TCP control block to a socket.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:459-468
func (s *Slirp) tcpAttach(so *Socket) int {
	tp := s.tcpNewTCPCB(so)
	if tp == nil {
		return -1
	}

	so.Next = s.TCB.Next
	so.Prev = &s.TCB
	s.TCB.Next.Prev = so
	s.TCB.Next = so

	s.traceSocketCreate(so, "TCP")

	return 0
}

// tcpNewTCPCB creates a new TCP control block.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:174-210
func (s *Slirp) tcpNewTCPCB(so *Socket) *TCPCB {
	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: TCPMaxSeg,
		TFlags:  0,
	}

	// Initialize reassembly queue as empty circular list (sentinel points to itself)
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:184
	// C code: tp->seg_next = tp->seg_prev = (struct tcpiphdr*)tp;
	tp.ReassemblyHead.Next = &tp.ReassemblyHead
	tp.ReassemblyHead.Prev = &tp.ReassemblyHead

	tp.TSrtt = 0
	tp.TRttVar = TCPTVSrttDflt << 2
	tp.TRttMin = TCPTVMin

	tp.TRxtCur = tcptRangeSet(
		int16(((0>>2)+(TCPTVSrttDflt<<2))>>1),
		TCPTVMin, TCPTVRexmtMax)

	tp.SndCwnd = uint32(TCPMaxWin) << TCPMaxWinShift
	tp.SndSsthresh = uint32(TCPMaxWin) << TCPMaxWinShift
	tp.TState = TCPSClosed

	so.SoTCPCB = tp
	return tp
}

// tcpFConnect connects to a foreign host.
// Returns 0 on success or EINPROGRESS, -1 on error (with errno set via returned error).
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:320-366
func (s *Slirp) tcpFConnect(so *Socket) (int, error) {
	fd, err := sockSocketTCP()
	if err != nil {
		return -1, err
	}

	sockSetNonblock(fd)
	sockSetsockoptInt(fd, sockSOLSocket, sockSOReuseAddr, 1)
	sockSetsockoptInt(fd, sockSOLSocket, sockSO_OOBINLINE, 1)

	var destIP net.IP
	vnetMask := ipToUint32(s.VNetworkMask)
	vnetAddr := ipToUint32(s.VNetworkAddr)
	soFAddr := ipToUint32(so.SoFAddr)

	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:339-349
	if (soFAddr & vnetMask) == vnetAddr {
		// It's an alias
		if so.SoFAddr.Equal(s.VNameserverAddr) {
			// Try to get the system's DNS server
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

	var addr4 [4]byte
	copy(addr4[:], destIP.To4())
	err = sockConnectInet4(fd, addr4, int(so.SoFPort))
	so.S = fd
	soIsFConnecting(so)

	if err != nil && err != syscall.EINPROGRESS {
		return -1, err
	}
	return 0, nil
}

// tcpTos returns the type of service for a socket.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:494-519
func (s *Slirp) tcpTos(so *Socket) uint8 {
	// Table matches C tcptos[] array exactly
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:473-487
	tcptos := []struct {
		fport uint16
		lport uint16
		tos   uint8
		emu   uint8
	}{
		{0, 20, IPTOSThroughput, 0},                       // ftp data
		{21, 21, IPTOSLowDelay, EmuFTP},                   // ftp control
		{0, 23, IPTOSLowDelay, 0},                         // telnet
		{0, 80, IPTOSThroughput, 0},                       // WWW
		{0, 513, IPTOSLowDelay, EmuRLogin | EmuNoConnect}, // rlogin
		{0, 514, IPTOSLowDelay, EmuRSH | EmuNoConnect},    // shell
		{0, 544, IPTOSLowDelay, EmuKSH},                   // kshell
		{0, 543, IPTOSLowDelay, 0},                        // klogin
		{0, 6667, IPTOSThroughput, EmuIRC},                // IRC
		{0, 6668, IPTOSThroughput, EmuIRC},                // IRC undernet
		{0, 7070, IPTOSLowDelay, EmuRealAudio},            // RealAudio control
		{0, 113, IPTOSLowDelay, EmuIdent},                 // identd protocol
	}

	fport := so.SoFPort
	lport := so.SoLPort

	// Match C behavior: check ports (already in host byte order in Go)
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:500-507
	for _, t := range tcptos {
		if (t.fport != 0 && fport == t.fport) || (t.lport != 0 && lport == t.lport) {
			so.SoEmu = t.emu
			return t.tos
		}
	}

	// User-added emulations (tcpemu list) not implemented - always NULL in C TinyEMU
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:509-516

	return 0
}

// tcpEmu handles TCP emulation for various protocols.
// Returns true if the mbuf should be sbappend()ed, false otherwise.
// NOTE: if you return false you MUST m_free() the mbuf!
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:545-877
func (s *Slirp) tcpEmu(so *Socket, m *Mbuf) bool {
	switch so.SoEmu {
	case EmuIdent:
		// Identification protocol as per RFC-1413
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:562-604
		soRcv := &so.SoRcv

		// Copy data to receive buffer
		if m.Len > 0 && soRcv.SbWPtr+m.Len <= soRcv.SbDataLen {
			copy(soRcv.SbData[soRcv.SbWPtr:], m.Data[:m.Len])
			soRcv.SbWPtr += m.Len
			soRcv.SbRPtr += m.Len
		}

		// NULL terminate for string operations
		if m.Len < len(m.Data) {
			m.Data[m.Len] = 0
		}

		// Look for CR or LF
		hasCRLF := false
		for i := 0; i < m.Len; i++ {
			if m.Data[i] == '\r' || m.Data[i] == '\n' {
				hasCRLF = true
				break
			}
		}

		if hasCRLF {
			// Parse "port1, port2" from the data
			var n1, n2 uint16
			dataStr := string(soRcv.SbData[:soRcv.SbWPtr])
			if _, err := fmt.Sscanf(dataStr, "%d%*[ ,]%d", &n1, &n2); err == nil {
				// n2 is the one on our host - look up the connection
				// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:582-594
				for tmpso := s.TCB.Next; tmpso != &s.TCB; tmpso = tmpso.Next {
					if tmpso.SoLAddr.Equal(so.SoLAddr) &&
						tmpso.SoLPort == n2 &&
						tmpso.SoFAddr.Equal(so.SoFAddr) &&
						tmpso.SoFPort == n1 {
						// Found matching connection, get actual port
						if listener, ok := tmpso.Extra.(*net.TCPListener); ok && listener != nil {
							if addr := listener.Addr().(*net.TCPAddr); addr != nil {
								n2 = uint16(addr.Port)
							}
						} else if conn, ok := tmpso.Extra.(net.Conn); ok && conn != nil {
							if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
								n2 = uint16(addr.Port)
							}
						}
						break
					}
				}
			}

			// Format response
			response := fmt.Sprintf("%d,%d\r\n", n1, n2)
			if len(response) <= soRcv.SbDataLen {
				copy(soRcv.SbData, response)
				soRcv.SbCC = len(response)
				soRcv.SbRPtr = 0
				soRcv.SbWPtr = soRcv.SbCC
			}
		}
		m.Free()
		return false

	case EmuFTP:
		// FTP PORT and PASV emulation
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:606-677
		if m.Len < len(m.Data) {
			m.Data[m.Len] = 0 // NUL terminate for strstr
		}
		dataStr := string(m.Data[:m.Len])

		// Check for PORT command
		if idx := strings.Index(dataStr, "ORT"); idx >= 0 {
			// Need to emulate the PORT command
			// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:608-640
			var n1, n2, n3, n4, n5, n6 uint32
			var buff string
			bptr := dataStr[idx:]
			n, _ := fmt.Sscanf(bptr, "ORT %d,%d,%d,%d,%d,%d\r\n%256s",
				&n1, &n2, &n3, &n4, &n5, &n6, &buff)
			if n < 6 {
				return true
			}

			laddr := uint32ToIP((n1 << 24) | (n2 << 16) | (n3 << 8) | n4)
			lport := uint16((n5 << 8) | n6)

			newSo := s.TCPListen(net.IPv4zero, 0, laddr, lport, SSFAcceptOnce)
			if newSo == nil {
				return true
			}

			n6 = uint32(newSo.SoFPort)
			n5 = (n6 >> 8) & 0xff
			n6 &= 0xff

			laddrInt := ipToUint32(newSo.SoFAddr)
			n1 = (laddrInt >> 24) & 0xff
			n2 = (laddrInt >> 16) & 0xff
			n3 = (laddrInt >> 8) & 0xff
			n4 = laddrInt & 0xff

			// Adjust length and rewrite
			prefix := m.Data[:idx]
			var newData string
			if n == 7 {
				newData = fmt.Sprintf("ORT %d,%d,%d,%d,%d,%d\r\n%s", n1, n2, n3, n4, n5, n6, buff)
			} else {
				newData = fmt.Sprintf("ORT %d,%d,%d,%d,%d,%d\r\n", n1, n2, n3, n4, n5, n6)
			}
			newLen := len(prefix) + len(newData)
			if newLen <= m.Size {
				copy(m.Data[idx:], newData)
				m.Len = newLen
			}
			return true
		}

		// Check for PASV response
		if idx := strings.Index(dataStr, "27 Entering"); idx >= 0 {
			// Need to emulate the PASV response
			// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:641-674
			var n1, n2, n3, n4, n5, n6 uint32
			var buff string
			bptr := dataStr[idx:]
			n, _ := fmt.Sscanf(bptr, "27 Entering Passive Mode (%d,%d,%d,%d,%d,%d)\r\n%256s",
				&n1, &n2, &n3, &n4, &n5, &n6, &buff)
			if n < 6 {
				return true
			}

			laddr := uint32ToIP((n1 << 24) | (n2 << 16) | (n3 << 8) | n4)
			lport := uint16((n5 << 8) | n6)

			newSo := s.TCPListen(net.IPv4zero, 0, laddr, lport, SSFAcceptOnce)
			if newSo == nil {
				return true
			}

			n6 = uint32(newSo.SoFPort)
			n5 = (n6 >> 8) & 0xff
			n6 &= 0xff

			laddrInt := ipToUint32(newSo.SoFAddr)
			n1 = (laddrInt >> 24) & 0xff
			n2 = (laddrInt >> 16) & 0xff
			n3 = (laddrInt >> 8) & 0xff
			n4 = laddrInt & 0xff

			// Adjust length and rewrite
			prefix := m.Data[:idx]
			var newData string
			if n == 7 {
				newData = fmt.Sprintf("27 Entering Passive Mode (%d,%d,%d,%d,%d,%d)\r\n%s", n1, n2, n3, n4, n5, n6, buff)
			} else {
				newData = fmt.Sprintf("27 Entering Passive Mode (%d,%d,%d,%d,%d,%d)\r\n", n1, n2, n3, n4, n5, n6)
			}
			newLen := len(prefix) + len(newData)
			if newLen <= m.Size {
				copy(m.Data[idx:], newData)
				m.Len = newLen
			}
			return true
		}
		return true

	case EmuKSH:
		// Kerberos rsh (kshell) and shell services
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:679-698
		so.SoEmu = 0

		// Parse NUL-terminated decimal ASCII port number
		var lport uint16
		for i := 0; i < m.Len-1; i++ {
			if m.Data[i] < '0' || m.Data[i] > '9' {
				return true // invalid number
			}
			lport = lport*10 + uint16(m.Data[i]-'0')
		}

		// Check for NUL terminator and valid port
		if m.Len > 0 && m.Data[m.Len-1] == 0 && lport != 0 {
			newSo := s.TCPListen(net.IPv4zero, 0, so.SoLAddr, lport, SSFAcceptOnce)
			if newSo != nil {
				// Rewrite with new port
				portStr := fmt.Sprintf("%d", newSo.SoFPort)
				if len(portStr)+1 <= m.Size {
					copy(m.Data, portStr)
					m.Data[len(portStr)] = 0
					m.Len = len(portStr) + 1
				}
			}
		}
		return true

	case EmuIRC:
		// IRC DCC CHAT, DCC SEND, and DCC MOVE emulation
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:700-743
		if m.Len < len(m.Data) {
			m.Data[m.Len] = 0 // NULL terminate
		}
		dataStr := string(m.Data[:m.Len])

		idx := strings.Index(dataStr, "DCC")
		if idx < 0 {
			return true
		}

		bptr := dataStr[idx:]
		var buff string
		var laddr, lport, n1 uint32

		// Try DCC CHAT
		if n, _ := fmt.Sscanf(bptr, "DCC CHAT %256s %d %d", &buff, &laddr, &lport); n == 3 {
			newSo := s.TCPListen(net.IPv4zero, 0, uint32ToIP(laddr), uint16(lport), SSFAcceptOnce)
			if newSo == nil {
				return true
			}
			prefix := m.Data[:idx]
			newData := fmt.Sprintf("DCC CHAT chat %d %d\x01\n",
				ipToUint32(newSo.SoFAddr), newSo.SoFPort)
			newLen := len(prefix) + len(newData)
			if newLen <= m.Size {
				copy(m.Data[idx:], newData)
				m.Len = newLen
			}
			return true
		}

		// Try DCC SEND
		if n, _ := fmt.Sscanf(bptr, "DCC SEND %256s %d %d %d", &buff, &laddr, &lport, &n1); n == 4 {
			newSo := s.TCPListen(net.IPv4zero, 0, uint32ToIP(laddr), uint16(lport), SSFAcceptOnce)
			if newSo == nil {
				return true
			}
			prefix := m.Data[:idx]
			newData := fmt.Sprintf("DCC SEND %s %d %d %d\x01\n",
				buff, ipToUint32(newSo.SoFAddr), newSo.SoFPort, n1)
			newLen := len(prefix) + len(newData)
			if newLen <= m.Size {
				copy(m.Data[idx:], newData)
				m.Len = newLen
			}
			return true
		}

		// Try DCC MOVE
		if n, _ := fmt.Sscanf(bptr, "DCC MOVE %256s %d %d %d", &buff, &laddr, &lport, &n1); n == 4 {
			newSo := s.TCPListen(net.IPv4zero, 0, uint32ToIP(laddr), uint16(lport), SSFAcceptOnce)
			if newSo == nil {
				return true
			}
			prefix := m.Data[:idx]
			newData := fmt.Sprintf("DCC MOVE %s %d %d %d\x01\n",
				buff, ipToUint32(newSo.SoFAddr), newSo.SoFPort, n1)
			newLen := len(prefix) + len(newData)
			if newLen <= m.Size {
				copy(m.Data[idx:], newData)
				m.Len = newLen
			}
			return true
		}
		return true

	case EmuRealAudio:
		// RealAudio emulation - parse binary protocol to find port numbers
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:745-870
		// NOTE: C code uses 'static int ra = 0' which shares state across ALL
		// RealAudio connections (a bug causing interference between connections).
		// Go uses so.Extra for per-socket state, which is more correct behavior.
		raTbl := []byte{0x50, 0x4e, 0x41, 0x00} // "PNA\0"

		for i := 0; i < m.Len; {
			ra := so.Extra
			if ra == nil {
				ra = 0
			}
			raState, _ := ra.(int)

			switch raState {
			case 0, 2, 3:
				if m.Data[i] != raTbl[raState] {
					so.Extra = 0
					i++
					continue
				}
			case 1:
				// We may get 0x50 several times, ignore them
				if m.Data[i] == 0x50 {
					so.Extra = 1
					i++
					continue
				} else if m.Data[i] != raTbl[raState] {
					so.Extra = 0
					i++
					continue
				}
			case 4:
				// skip version number
				i++
				so.Extra = raState + 1
				continue
			case 5:
				// The difference between versions 1.0 and 2.0
				if i+1 < m.Len && m.Data[i+1] == 0x02 {
					i += 8
				} else {
					i += 4
				}
				so.Extra = raState + 1
				continue
			case 6:
				// This is the field containing the port number
				if i+1 >= m.Len {
					return true
				}
				lport := (uint16(m.Data[i]) << 8) + uint16(m.Data[i+1])
				if lport < 6970 {
					lport += 256 // don't know why
				}
				if lport < 6970 || lport > 7170 {
					return true // failed
				}

				// Try to get UDP port between 6970-7170
				var p uint16
				for p = 6970; p < 7071; p++ {
					if s.UDPListen(net.IPv4zero, p, so.SoLAddr, lport, SSFAcceptOnce) != nil {
						break
					}
				}
				if p == 7071 {
					p = 0
				}

				// Rewrite port in packet
				m.Data[i] = byte((p >> 8) & 0xff)
				m.Data[i+1] = byte(p & 0xff)
				so.Extra = 0
				return true
			default:
				so.Extra = 0
			}
			so.Extra = raState + 1
			i++
		}
		return true

	default:
		// Not emulated, won't call tcp_emu again
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:872-875
		so.SoEmu = 0
		return true
	}
}

// ipOutput sends an IP packet.
// The packet in mbuf m contains a skeletal IP header (with len, off, ttl, proto, tos, src, dst).
// The mbuf chain containing the packet will be freed.
// Reference: tinyemu-2019-12-21/slirp/ip_output.c:53-172
func (s *Slirp) ipOutput(so *Socket, m0 *Mbuf) int {
	if m0 == nil || m0.Len < IPHeaderSize {
		if m0 != nil {
			m0.Free()
		}
		return -1
	}

	m := m0
	hlen := IPHeaderSize
	errorVal := 0

	// Fill in IP header.
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:70-73
	m.Data[0] = (IPVersion << 4) | uint8(hlen>>2) // ip_v = IPVERSION, ip_hl = hlen >> 2

	// ip->ip_off &= IP_DF - keep only DF bit
	ipOff := binary.BigEndian.Uint16(m.Data[6:8])
	ipOff &= IPDF
	binary.BigEndian.PutUint16(m.Data[6:8], ipOff)

	// ip->ip_id = htons(slirp->ip_id++)
	s.IPID++
	binary.BigEndian.PutUint16(m.Data[4:6], s.IPID)

	// Get ip_len for comparison (already in network byte order from caller)
	ipLen := binary.BigEndian.Uint16(m.Data[2:4])

	// If small enough for interface, can just send directly.
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:78-86
	if ipLen <= IFMTU {
		// ip->ip_sum = 0; ip->ip_sum = cksum(m, hlen)
		m.Data[10] = 0
		m.Data[11] = 0
		sum := Cksum(m, hlen)
		binary.BigEndian.PutUint16(m.Data[10:12], sum)

		s.IfOutput(so, m)
		return 0
	}

	// Too large for interface; fragment if possible.
	// Must be able to put at least 8 bytes per fragment.
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:92-101
	if ipOff&IPDF != 0 {
		// DF flag is set, can't fragment
		m0.Free()
		return -1
	}

	// Calculate data bytes per packet (8-byte aligned)
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:97
	dataLen := (IFMTU - hlen) &^ 7 // ip databytes per packet
	if dataLen < 8 {
		m0.Free()
		return -1
	}

	// Fragment the packet
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:103-164
	firstLen := dataLen
	mhlen := IPHeaderSize

	// Loop through length of segment after first fragment,
	// make new header and copy data of each part and link onto chain.
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:110-144
	var lastM *Mbuf
	lastM = m0

	for off := hlen + dataLen; off < int(ipLen); off += dataLen {
		fragM := s.MGet()
		if fragM == nil {
			errorVal = -1
			goto sendorfree
		}

		// Reserve header space: m->m_data += IF_MAXLINKHDR
		// In Go, we work with the Data slice directly, starting at position IFMaxLinkHdr
		// But since Go mbuf.Data is the full buffer, we start from beginning
		// and the IfOutput/IfEncap handles the actual framing

		// Copy IP header from original
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:121-122
		copy(fragM.Data[:mhlen], m0.Data[:mhlen])
		fragM.Len = mhlen

		// Calculate fragment offset
		// mhip->ip_off = ((off - hlen) >> 3) + (ip->ip_off & ~IP_MF)
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:125
		fragOff := uint16((off-hlen)>>3) + (ipOff &^ IPMF)

		// Propagate MF flag from original if set
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:126-127
		if ipOff&IPMF != 0 {
			fragOff |= IPMF
		}

		// Calculate length for this fragment
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:128-131
		fragDataLen := dataLen
		if off+dataLen >= int(ipLen) {
			fragDataLen = int(ipLen) - off
		} else {
			fragOff |= IPMF // More fragments follow
		}

		// Set ip_len and ip_off in fragment header
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:132,139
		binary.BigEndian.PutUint16(fragM.Data[2:4], uint16(fragDataLen+mhlen))
		binary.BigEndian.PutUint16(fragM.Data[6:8], fragOff)

		// Copy data from original mbuf
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:134-137
		if fragM.Copy(m0, off, fragDataLen) < 0 {
			fragM.Free()
			errorVal = -1
			goto sendorfree
		}

		// Compute checksum
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:140-141
		fragM.Data[10] = 0
		fragM.Data[11] = 0
		binary.BigEndian.PutUint16(fragM.Data[10:12], Cksum(fragM, mhlen))

		// Link onto chain
		// Reference: tinyemu-2019-12-21/slirp/ip_output.c:142-143
		lastM.NextPkt = fragM
		lastM = fragM
	}

	// Update first fragment by trimming what's been copied out
	// and updating header, then send each fragment (in order).
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:148-154
	m = m0
	m.Adj(hlen + firstLen - int(ipLen)) // Trim excess data
	binary.BigEndian.PutUint16(m.Data[2:4], uint16(m.Len))
	binary.BigEndian.PutUint16(m.Data[6:8], ipOff|IPMF)
	m.Data[10] = 0
	m.Data[11] = 0
	binary.BigEndian.PutUint16(m.Data[10:12], Cksum(m, hlen))

sendorfree:
	// Send or free all fragments
	// Reference: tinyemu-2019-12-21/slirp/ip_output.c:156-163
	for m = m0; m != nil; {
		nextM := m.NextPkt
		m.NextPkt = nil
		if errorVal == 0 {
			s.IfOutput(so, m)
		} else {
			m.Free()
		}
		m = nextM
	}

	return errorVal
}

// ipChecksum computes the IP header checksum.
func ipChecksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// tcpConnect accepts an incoming TCP connection on a listening socket.
// This is called when a listening socket (SS_FACCEPTCONN) has a pending connection.
// If conn is non-nil, it's an already-accepted connection; otherwise Accept is called.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:381-454
func (s *Slirp) tcpConnect(inso *Socket, conn *net.TCPConn) {
	var so *Socket
	var tp *TCPCB

	// Get the listener from the socket
	listener, ok := inso.Extra.(*net.TCPListener)
	if !ok || listener == nil {
		if conn != nil {
			conn.Close()
		}
		return
	}

	// If it's an SS_FACCEPTONCE socket, no need to socreate()
	// another socket, just use the accept() socket.
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:397-412
	if (inso.SoState & SSFAcceptOnce) != 0 {
		// FACCEPTONCE already have a tcpcb
		so = inso
	} else {
		so = s.SoCreate()
		if so == nil {
			// If it failed, get rid of the pending connection
			if conn != nil {
				conn.Close()
			}
			return
		}
		if s.tcpAttach(so) < 0 {
			// NOT sofree - just free the socket struct
			// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:407
			if conn != nil {
				conn.Close()
			}
			return
		}
		so.SoLAddr = inso.SoLAddr
		so.SoLPort = inso.SoLPort
	}

	// Initialize MSS
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:414
	tp = SoToTCPCB(so)
	if tp != nil {
		tp.tcpMss(0)
	}

	// Accept the connection if not already provided
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:416-419
	if conn == nil {
		var err error
		conn, err = listener.AcceptTCP()
		if err != nil {
			if tp != nil {
				tp.TCPClose() // This will sofree() as well
			}
			return
		}
	}

	// Set non-blocking and socket options
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:420-426
	// In Go, TCPConn is already non-blocking via goroutines
	conn.SetNoDelay(true) // TCP_NODELAY

	// Get remote address from accepted connection
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:428-429
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
	so.SoFPort = uint16(remoteAddr.Port)
	so.SoFAddr = remoteAddr.IP.To4()
	if so.SoFAddr == nil {
		so.SoFAddr = remoteAddr.IP
	}

	// Translate connections from localhost to the real hostname
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:431-432
	if so.SoFAddr == nil || so.SoFAddr.Equal(net.IPv4zero) || so.SoFAddr.Equal(LoopbackAddr) {
		so.SoFAddr = s.VHostAddr
	}

	// Close the accept() socket, set right state
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:435-439
	if (inso.SoState & SSFAcceptOnce) != 0 {
		// If we only accept once, close the listener
		listener.Close()
		so.SoState = SSNoFDRef // Don't select it yet, even though we have an FD
	}

	// Store the connection
	so.Extra = conn

	// Mark as incoming connection
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:440-441
	so.SoState |= SSIncoming

	// Set TOS and emulation
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:443
	so.SoIPTos = s.tcpTos(so)

	// Get TCPCB (may have been set above or in tcpAttach)
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:444
	tp = SoToTCPCB(so)
	if tp == nil {
		conn.Close()
		return
	}

	// Create TCP template
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:446
	s.tcpTemplate(tp)

	// Set state to SYN_SENT and initialize timers/sequence numbers
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:448-452
	tp.TState = TCPSSynSent
	tp.TTimer[TCPTKeep] = TCPTVKeepInit
	tp.Iss = s.TCPIss
	s.TCPIss += TCPIssIncr / 2
	tp.tcpSendSeqInit()

	// Send initial SYN
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:453
	tp.TCPOutput()
}

// checkTCPListeners checks for incoming connections on TCP listening sockets.
// This is called from Poll to handle incoming connections that were accepted
// by the OS on listening sockets created by TCPListen.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:412-419
func (s *Slirp) checkTCPListeners() {
	// Build a list of sockets to process (to avoid modifying list while iterating)
	var listeners []*Socket
	for so := s.TCB.Next; so != &s.TCB; so = so.Next {
		if (so.SoState & SSFAcceptConn) != 0 {
			listeners = append(listeners, so)
		}
	}

	// Process each listener
	for _, so := range listeners {
		// Check if the listener has a pending connection
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:416-417
		listener, ok := so.Extra.(*net.TCPListener)
		if !ok || listener == nil {
			continue
		}

		// Set a very short deadline to make Accept non-blocking.
		// This simulates the select() check in C code.
		// Using a small positive timeout (1ms) to allow the syscall to check
		// for pending connections without blocking for long.
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:284-286 (select on SS_FACCEPTCONN)
		listener.SetDeadline(time.Now().Add(time.Millisecond))
		conn, err := listener.AcceptTCP()

		// Reset deadline for future accepts
		listener.SetDeadline(time.Time{})

		if err != nil {
			// No pending connection or error - continue
			continue
		}

		// Pass the accepted connection to tcpConnect
		// Reference: tinyemu-2019-12-21/slirp/slirp.c:417
		s.tcpConnect(so, conn)
	}
}
