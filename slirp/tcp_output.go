package slirp

import (
	"encoding/binary"
)

// TCP output routines.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c

// TCPMaxWin is the largest value for (unscaled) window.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:100
const TCPMaxWin = 65535

// MaxTCPOptLen is the maximum TCP options length.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:50
const MaxTCPOptLen = 32

// TCPNStates is the number of TCP states.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:120
const TCPNStates = 11

// tcpOutflags maps TCP state to output flags.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:43-47
var tcpOutflags = [TCPNStates]uint8{
	THRst | THAck, // TCPS_CLOSED
	0,             // TCPS_LISTEN
	THSyn,         // TCPS_SYN_SENT
	THSyn | THAck, // TCPS_SYN_RECEIVED
	THAck,         // TCPS_ESTABLISHED
	THAck,         // TCPS_CLOSE_WAIT
	THFin | THAck, // TCPS_FIN_WAIT_1
	THFin | THAck, // TCPS_CLOSING
	THFin | THAck, // TCPS_LAST_ACK
	THAck,         // TCPS_FIN_WAIT_2
	THAck,         // TCPS_TIME_WAIT
}

// Sequence number comparison functions.
// These handle wraparound by doing signed comparison after subtraction.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:146-149

// seqLT returns true if a < b in sequence space.
func seqLT(a, b uint32) bool {
	return int32(a-b) < 0
}

// seqLEQ returns true if a <= b in sequence space.
func seqLEQ(a, b uint32) bool {
	return int32(a-b) <= 0
}

// seqGT returns true if a > b in sequence space.
func seqGT(a, b uint32) bool {
	return int32(a-b) > 0
}

// seqGEQ returns true if a >= b in sequence space.
func seqGEQ(a, b uint32) bool {
	return int32(a-b) >= 0
}

// TCPOutput sends TCP output for a connection.
// This is the main TCP output routine that figures out what should be sent
// and sends it.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:56-477
func (tp *TCPCB) TCPOutput() int {
	so := tp.TSocket
	if so == nil {
		return 0
	}
	slirp := so.Slirp
	if slirp == nil {
		return 0
	}

	// Determine length of data that should be transmitted,
	// and flags that will be used.
again:
	var sendalot int
	idle := tp.SndMax == tp.SndUna
	if idle && tp.TIdle >= tp.TRxtCur {
		// We have been idle for "a while" and no acks are
		// expected to clock out any data we send --
		// slow start to get ack "clock" running again.
		tp.SndCwnd = uint32(tp.TMaxSeg)
	}

	off := int(tp.SndNxt - tp.SndUna)
	win := min(int64(tp.SndWnd), int64(tp.SndCwnd))

	flags := tcpOutflags[tp.TState]

	// If in persist timeout with window of 0, send 1 byte.
	// Otherwise, if window is small but nonzero
	// and timer expired, we will send what we can
	// and go to transmit state.
	if tp.TForce != 0 {
		if win == 0 {
			// If we still have some data to send, then
			// clear the FIN bit.
			if off < so.SoSnd.SbCC {
				flags &^= THFin
			}
			win = 1
		} else {
			tp.TTimer[TCPTPersist] = 0
			tp.TRxtShift = 0
		}
	}

	length := min(int64(so.SoSnd.SbCC), win) - int64(off)

	if length < 0 {
		// If FIN has been sent but not acked,
		// but we haven't been called to retransmit,
		// len will be -1. Otherwise, window shrank
		// after we sent into it.
		length = 0
		if win == 0 {
			tp.TTimer[TCPTRexmt] = 0
			tp.SndNxt = tp.SndUna
		}
	}

	if int(length) > int(tp.TMaxSeg) {
		length = int64(tp.TMaxSeg)
		sendalot = 1
	}

	if seqLT(tp.SndNxt+uint32(length), tp.SndUna+uint32(so.SoSnd.SbCC)) {
		flags &^= THFin
	}

	// Calculate receive window space
	rcvWin := so.SoRcv.SbSpace()

	// Sender silly window avoidance.
	// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:165-177
	if length > 0 {
		if int(length) == int(tp.TMaxSeg) {
			goto send
		}
		// Note: C code has (1 || idle || tp->t_flags & TF_NODELAY) which is always true
		// due to the leading 1, so we just check if we can send all remaining data.
		if int(length)+off >= so.SoSnd.SbCC {
			goto send
		}
		if tp.TForce != 0 {
			goto send
		}
		if tp.MaxSndWnd > 0 && int(length) >= int(tp.MaxSndWnd)/2 {
			goto send
		}
		if seqLT(tp.SndNxt, tp.SndMax) {
			goto send
		}
	}

	// Compare available window to amount of window
	// known to peer (as advertised window less
	// next expected input).
	if rcvWin > 0 {
		// "adv" is the amount we can increase the window
		adv := min(int64(rcvWin), int64(TCPMaxWin)<<tp.RcvScale) -
			int64(tp.RcvAdv-tp.RcvNxt)

		if adv >= int64(2*tp.TMaxSeg) {
			goto send
		}
		if 2*adv >= int64(so.SoRcv.SbDataLen) {
			goto send
		}
	}

	// Send if we owe peer an ACK.
	if (tp.TFlags & TFAckNow) != 0 {
		goto send
	}
	if (flags & (THSyn | THRst)) != 0 {
		goto send
	}
	if seqGT(tp.SndUp, tp.SndUna) {
		goto send
	}
	// If our state indicates that FIN should be sent
	// and we have not yet done so, or we're retransmitting the FIN,
	// then we need to send.
	if (flags&THFin) != 0 &&
		((tp.TFlags&TFSentFin) == 0 || tp.SndNxt == tp.SndUna) {
		goto send
	}

	// TCP window updates are not reliable, rather a polling protocol
	// using "persist" packets is used to insure receipt of window
	// updates.
	if so.SoSnd.SbCC > 0 && tp.TTimer[TCPTRexmt] == 0 &&
		tp.TTimer[TCPTPersist] == 0 {
		tp.TRxtShift = 0
		tp.TCPSetPersist()
	}

	// No reason to send a segment, just return.
	return 0

send:
	// Before ESTABLISHED, force sending of initial options
	// unless TCP set not to do any options.
	var opt [MaxTCPOptLen]byte
	var optlen int
	hdrlen := 40 // sizeof(struct tcpiphdr) = 20 IP + 20 TCP

	if (flags & THSyn) != 0 {
		tp.SndNxt = tp.Iss
		if (tp.TFlags & TFNoOpt) == 0 {
			opt[0] = TCPOptMaxSeg
			opt[1] = 4
			mss := tp.tcpMss(0)
			binary.BigEndian.PutUint16(opt[2:4], uint16(mss))
			optlen = 4
		}
	}

	hdrlen += optlen

	// Adjust data length if insertion of options will
	// bump the packet length beyond the t_maxseg length.
	if int(length) > int(tp.TMaxSeg)-optlen {
		length = int64(tp.TMaxSeg) - int64(optlen)
		sendalot = 1
	}

	// Grab a header mbuf, attaching a copy of data to
	// be transmitted.
	var m *Mbuf
	if length > 0 {
		m = slirp.MGet()
		if m == nil {
			return 1
		}
		// Reserve space for link header
		m.SetBuffer(make([]byte, IFMaxLinkHdr+hdrlen+int(length)))
		m.Len = hdrlen + int(length)

		// Copy data from send buffer
		so.SoSnd.SbCopy(off, int(length), m.Data[IFMaxLinkHdr+hdrlen:])

		// If we're sending everything we've got, set PUSH.
		if off+int(length) == so.SoSnd.SbCC {
			flags |= THPush
		}
	} else {
		m = slirp.MGet()
		if m == nil {
			return 1
		}
		m.SetBuffer(make([]byte, IFMaxLinkHdr+hdrlen))
		m.Len = hdrlen
	}

	// Build TCP/IP header at offset IFMaxLinkHdr
	ti := m.Data[IFMaxLinkHdr:]

	// Copy template
	tp.marshalTemplate(ti)

	// Fill in fields
	// If resending a FIN, be sure not to use a new sequence number.
	if (flags&THFin) != 0 && (tp.TFlags&TFSentFin) != 0 &&
		tp.SndNxt == tp.SndMax {
		tp.SndNxt--
	}

	// Determine sequence number for this segment
	var seq uint32
	if length > 0 || (flags&(THSyn|THFin)) != 0 || tp.TTimer[TCPTPersist] != 0 {
		seq = tp.SndNxt
	} else {
		seq = tp.SndMax
	}
	binary.BigEndian.PutUint32(ti[24:28], seq)       // ti_seq
	binary.BigEndian.PutUint32(ti[28:32], tp.RcvNxt) // ti_ack

	// Copy options if any
	if optlen > 0 {
		copy(ti[40:40+optlen], opt[:optlen])
		ti[32] = uint8((20 + optlen) >> 2 << 4) // ti_off (data offset)
	}

	ti[33] = flags // ti_flags

	// Calculate receive window. Don't shrink window,
	// but avoid silly window syndrome.
	winAdv := int64(rcvWin)
	if winAdv < int64(so.SoRcv.SbDataLen)/4 && winAdv < int64(tp.TMaxSeg) {
		winAdv = 0
	}
	if winAdv > int64(TCPMaxWin)<<tp.RcvScale {
		winAdv = int64(TCPMaxWin) << tp.RcvScale
	}
	if winAdv < int64(tp.RcvAdv-tp.RcvNxt) {
		winAdv = int64(tp.RcvAdv - tp.RcvNxt)
	}
	binary.BigEndian.PutUint16(ti[34:36], uint16(winAdv>>tp.RcvScale)) // ti_win

	// Urgent pointer
	if seqGT(tp.SndUp, tp.SndUna) {
		binary.BigEndian.PutUint16(ti[38:40], uint16(tp.SndUp-seq)) // ti_urp
		ti[33] |= THUrg
	} else {
		// Drag urgent pointer along
		tp.SndUp = tp.SndUna
	}

	// Put TCP length in extended header, and then
	// checksum extended header and data.
	// ti_len overlays the IP checksum field (bytes 10-11) in the pseudo-header.
	// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:386-387
	if length > 0 || optlen > 0 {
		tcpLen := 20 + optlen + int(length)
		binary.BigEndian.PutUint16(ti[10:12], uint16(tcpLen)) // ti_len (overlays ip_sum)
	}

	// Compute TCP checksum over pseudo-header and data
	// Adjust to point to IP header start
	// C: m->m_data = (caddr_t)ti;
	m.SetDataOffset(m.Offset + IFMaxLinkHdr)
	sum := Cksum(m, hdrlen+int(length))
	binary.BigEndian.PutUint16(ti[36:38], sum) // ti_sum

	// In transmit state, time the transmission and arrange for
	// the retransmit. In persist state, just set snd_max.
	if tp.TForce == 0 || tp.TTimer[TCPTPersist] == 0 {
		startseq := tp.SndNxt

		// Advance snd_nxt over sequence space of this segment.
		if (flags & (THSyn | THFin)) != 0 {
			if (flags & THSyn) != 0 {
				tp.SndNxt++
			}
			if (flags & THFin) != 0 {
				tp.SndNxt++
				tp.TFlags |= TFSentFin
			}
		}
		tp.SndNxt += uint32(length)

		if seqGT(tp.SndNxt, tp.SndMax) {
			tp.SndMax = tp.SndNxt
			// Time this transmission if not a retransmission and
			// not currently timing anything.
			if tp.TRtt == 0 {
				tp.TRtt = 1
				tp.TRtSeq = startseq
			}
		}

		// Set retransmit timer if not currently set,
		// and not doing an ack or a keep-alive probe.
		if tp.TTimer[TCPTRexmt] == 0 && tp.SndNxt != tp.SndUna {
			tp.TTimer[TCPTRexmt] = tp.TRxtCur
			if tp.TTimer[TCPTPersist] != 0 {
				tp.TTimer[TCPTPersist] = 0
				tp.TRxtShift = 0
			}
		}
	} else {
		if seqGT(tp.SndNxt+uint32(length), tp.SndMax) {
			tp.SndMax = tp.SndNxt + uint32(length)
		}
	}

	// Fill in IP length and desired time to live and
	// send to IP level.
	m.Len = hdrlen + int(length)

	// Set IP length, TTL, and TOS.
	// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:451-454
	// Note: C does not set ip_p here; it relies on the template.
	binary.BigEndian.PutUint16(ti[2:4], uint16(m.Len)) // ip_len
	ti[8] = IPDefTTL                                   // ip_ttl
	ti[1] = so.SoIPTos                                 // ip_tos

	// Trace TCP output
	slirp.traceTCPOutput(
		uint32ToIP(binary.BigEndian.Uint32(ti[12:16])),
		uint32ToIP(binary.BigEndian.Uint32(ti[16:20])),
		binary.BigEndian.Uint16(ti[20:22]),
		binary.BigEndian.Uint16(ti[22:24]),
		ti[33],
		binary.BigEndian.Uint32(ti[24:28]),
		binary.BigEndian.Uint32(ti[28:32]),
		int(length),
	)

	err := slirp.IPOutput(so, m)
	if err != 0 {
		return err
	}

	// Data sent (as far as we can tell).
	// If this advertises a larger window than any other segment,
	// then remember the size of the advertised window.
	// Any pending ACK has now been sent.
	if winAdv > 0 && seqGT(tp.RcvNxt+uint32(winAdv), tp.RcvAdv) {
		tp.RcvAdv = tp.RcvNxt + uint32(winAdv)
	}
	tp.LastAckSent = tp.RcvNxt
	tp.TFlags &^= (TFAckNow | TFDelAck)

	if sendalot != 0 {
		goto again
	}

	return 0
}

// TCPSetPersist sets the persist timer.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:479-492
func (tp *TCPCB) TCPSetPersist() {
	// C promotes shorts to int for arithmetic operations. We must do the same
	// to avoid int16 overflow when adding t_srtt and t_rttvar.
	t := (int(tp.TSrtt>>2) + int(tp.TRttVar)) >> 1

	// Start/restart persistence timer.
	// The multiplication is done in int (like C), then truncated to int16 on assignment.
	tp.TTimer[TCPTPersist] = tcptRangeSet(
		int16(t*TCPBackoff[tp.TRxtShift]),
		TCPTVPersMin,
		TCPTVPersMax,
	)
	if tp.TRxtShift < TCPMaxRxtShift {
		tp.TRxtShift++
	}
}

// marshalTemplate copies the TCP/IP template to the given buffer.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:322-324
func (tp *TCPCB) marshalTemplate(buf []byte) {
	// For TCP checksum calculation, bytes 0-7 must be 0 (they overlay ti_mbuf
	// in the pseudo-header structure). The actual IP header fields (version, IHL,
	// TOS, length, ID, flags, fragment offset) are set later by ipOutput.
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:69 (ti_mbuf = NULL)
	//
	// IP header (20 bytes) - pseudo-header overlay:
	// bytes 0-7: ti_mbuf (set to 0 for checksum)
	// byte 8: ti_x1 (set to 0 for checksum, later becomes TTL)
	// byte 9: ti_pr (protocol)
	// bytes 10-11: ti_len (TCP length for checksum, later becomes IP checksum)
	buf[0] = 0 // ti_mbuf[0] = 0 (overlays ip version/IHL, set by ipOutput)
	buf[1] = 0 // ti_mbuf[1] = 0 (overlays ip TOS, set later)
	// bytes 2-3: total length (set later)
	binary.BigEndian.PutUint16(buf[4:6], 0)                  // ti_mbuf[4-5] = 0 (overlays ID)
	binary.BigEndian.PutUint16(buf[6:8], 0)                  // ti_mbuf[6-7] = 0 (overlays flags + frag offset)
	buf[8] = 0                                               // ti_x1 = 0 for TCP checksum, TTL set later
	buf[9] = IPProtoTCP                                      // ti_pr = protocol
	binary.BigEndian.PutUint16(buf[10:12], tp.TTemplate.Len) // ti_len = TCP header length (default 20, may be overwritten if data/options)
	binary.BigEndian.PutUint32(buf[12:16], tp.TTemplate.Src) // source IP
	binary.BigEndian.PutUint32(buf[16:20], tp.TTemplate.Dst) // dest IP

	// TCP header (20 bytes minimum)
	binary.BigEndian.PutUint16(buf[20:22], tp.TTemplate.Sport) // source port
	binary.BigEndian.PutUint16(buf[22:24], tp.TTemplate.Dport) // dest port
	// bytes 24-27: sequence number (set later)
	// bytes 28-31: ack number (set later)
	buf[32] = 5 << 4 // data offset (5 = 20 bytes, no options)
	buf[33] = 0      // flags (set later)
	// bytes 34-35: window (set later)
	// bytes 36-37: checksum (computed later)
	// bytes 38-39: urgent pointer (set later)
}
