package slirp

// TCP constants and state definitions.
// Reference: tinyemu-2019-12-21/slirp/tcp.h

const (
	// PRSlowHz is the number of slow timeouts per second (approximately 2).
	// Reference: tinyemu-2019-12-21/slirp/tcp.h:38
	PRSlowHz = 2

	// TCPIssIncr is the increment for tcp_iss each second.
	// Reference: tinyemu-2019-12-21/slirp/tcp.h:162
	TCPIssIncr = 125 * 1024
)

// TCP connection states.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:122-135
const (
	TCPSClosed      = 0  // closed
	TCPSListen      = 1  // listening for connection
	TCPSSynSent     = 2  // active, have sent syn
	TCPSSynReceived = 3  // have sent and received syn
	TCPSEstablished = 4  // established
	TCPSCloseWait   = 5  // rcvd fin, waiting for close
	TCPSFinWait1    = 6  // have closed, sent fin
	TCPSClosing     = 7  // closed xchd FIN; await FIN ACK
	TCPSLastAck     = 8  // had fin and close; await FIN ACK
	TCPSFinWait2    = 9  // have closed, fin is acked
	TCPSTimeWait    = 10 // in 2*msl quiet wait after close
)

// TCP flag constants.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:53-62
const (
	TFAckNow     = 0x0001 // ack peer immediately
	TFDelAck     = 0x0002 // ack, but try to delay it
	TFNoDelay    = 0x0004 // don't delay packets to coalesce
	TFNoOpt      = 0x0008 // don't use tcp options
	TFSentFin    = 0x0010 // have sent FIN
	TFReqScale   = 0x0020 // have/will request window scaling
	TFRcvdScale  = 0x0040 // other side has requested scaling
	TFReqTstmp   = 0x0080 // have/will request timestamps
	TFRcvdTstmp  = 0x0100 // a timestamp was received in SYN
	TFSackPermit = 0x0200 // other side said I could SACK
)

// TCP RTT scaling constants.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:140-143
const (
	TCPRTTScale    = 8 // multiplier for srtt; 3 bits frac.
	TCPRTTShift    = 3 // shift for srtt; 3 bits frac.
	TCPRTTVarScale = 4 // multiplier for rttvar; 2 bits
	TCPRTTVarShift = 2 // shift for rttvar; 2 bits
)

// TCP header flags.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:61-66
const (
	THFin  = 0x01
	THSyn  = 0x02
	THRst  = 0x04
	THPush = 0x08
	THAck  = 0x10
	THUrg  = 0x20
)

// TCP option constants.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:74-78
const (
	TCPOptEOL    = 0
	TCPOptNOP    = 1
	TCPOptMaxSeg = 2

	TCPOLenMaxSeg = 4 // length of MAXSEG option
)

// TCP buffer space constants.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:41-42
const (
	TCPSndSpace = 8192
	TCPRcvSpace = 8192
)

// TCPTNTimers is the number of TCP timers.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:40
const TCPTNTimers = 4

// SavedTCPIPHeader preserves the original TCP/IP header for RST and ICMP responses.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:268-274 (save_ip = *ip)
// The C code saves the IP header before stripping so it can be used later for
// tcp_respond() and icmp_error() calls at dropwithreset and other error paths.
type SavedTCPIPHeader struct {
	// Header contains the full TCP/IP header bytes (IP + TCP + options)
	// This is saved before headers are stripped from the mbuf
	Header []byte

	// IPTotalLen is the total IP packet length (restored for error responses)
	// Reference: tcp_input.c:274 (save_ip.ip_len += iphlen)
	IPTotalLen uint16

	// TOS is the IP Type of Service byte
	TOS uint8

	// Extracted header fields for convenience (in host byte order)
	SrcIP   uint32 // source IP address
	DstIP   uint32 // destination IP address
	SrcPort uint16 // source TCP port
	DstPort uint16 // destination TCP port
}

// TCPIPHdr represents a combined TCP/IP header.
// Reference: tinyemu-2019-12-21/slirp/tcpip.h:39-42
type TCPIPHdr struct {
	// Reassembly queue linkage (for tcp_reass)
	// Reference: tinyemu-2019-12-21/slirp/tcpip.h:60-66
	Next *TCPIPHdr // next segment in reassembly queue
	Prev *TCPIPHdr // previous segment in reassembly queue

	// Overlaid IP header fields
	Mbuf *Mbuf  // back pointer to mbuf
	X1   uint8  // unused
	Pr   uint8  // protocol
	Len  uint16 // length (IP header ti_len, not TCP segment length)
	Src  uint32 // source address
	Dst  uint32 // destination address

	// TCP header fields
	Sport uint16 // source port
	Dport uint16 // destination port
	Seq   uint32 // sequence number
	Ack   uint32 // acknowledgment number
	OffX2 uint8  // data offset (upper 4 bits) + reserved
	Flags uint8  // flags
	Win   uint16 // window
	Sum   uint16 // checksum
	Urp   uint16 // urgent pointer

	// TCP segment data length (for reassembly)
	// Reference: tinyemu-2019-12-21/slirp/tcpip.h:46 (ti_len via ih_len)
	TiLen int // segment data length (not including headers)
}

// TCPCB is the TCP control block, one per TCP connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:42-127
type TCPCB struct {
	// Reassembly queue head (sentinel for circular doubly-linked list)
	// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:43-44
	// In C, seg_next/seg_prev point to tcpiphdr structs and the tcpcb itself
	// acts as the sentinel. In Go, we use an embedded TCPIPHdr as the sentinel.
	// When empty: ReassemblyHead.Next == &ReassemblyHead
	ReassemblyHead TCPIPHdr

	TState    int16              // state of this connection
	TTimer    [TCPTNTimers]int16 // tcp timers
	TRxtShift int16              // log(2) of rexmt exp. backoff
	TRxtCur   int16              // current retransmit value
	TDupAcks  int16              // consecutive dup acks recd
	TMaxSeg   uint16             // maximum segment size
	TForce    int8               // 1 if forcing out a byte
	TFlags    uint16             // flags (TF_*)

	TTemplate TCPIPHdr // static skeletal packet for xmit

	TSocket *Socket // back pointer to socket

	// Send sequence variables (RFC 783)
	SndUna uint32 // send unacknowledged
	SndNxt uint32 // send next
	SndUp  uint32 // send urgent pointer
	SndWL1 uint32 // window update seg seq number
	SndWL2 uint32 // window update seg ack number
	Iss    uint32 // initial send sequence number
	SndWnd uint32 // send window

	// Receive sequence variables
	RcvWnd uint32 // receive window
	RcvNxt uint32 // receive next
	RcvUp  uint32 // receive urgent pointer
	Irs    uint32 // initial receive sequence number

	// Additional variables for this implementation
	RcvAdv uint32 // advertised window
	SndMax uint32 // highest sequence number sent

	// Congestion control
	SndCwnd     uint32 // congestion-controlled window
	SndSsthresh uint32 // snd_cwnd size threshold for slow start

	// Transmit timing
	TIdle     int16  // inactivity time
	TRtt      int16  // round trip time
	TRtSeq    uint32 // sequence number being timed
	TSrtt     int16  // smoothed round-trip time
	TRttVar   int16  // variance in round-trip time
	TRttMin   uint16 // minimum rtt allowed
	MaxSndWnd uint32 // largest window peer has offered

	// Out-of-band data
	TOobFlags int8 // have some
	TIobc     int8 // input character

	TSoftError int16 // possible error not yet reported

	// RFC 1323 variables
	SndScale        uint8  // window scaling for send window
	RcvScale        uint8  // window scaling for recv window
	RequestRScale   uint8  // pending window scaling
	RequestedSScale uint8  // requested scale
	TsRecent        uint32 // timestamp echo data
	TsRecentAge     uint32 // when last updated
	LastAckSent     uint32 // last ack sent
}

// Out-of-band flags.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:114-115
const (
	TCPOobHaveData = 0x01
	TCPOobHadData  = 0x02
)

// SoToTCPCB returns the TCPCB associated with a socket.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:129
func SoToTCPCB(so *Socket) *TCPCB {
	if so == nil || so.SoTCPCB == nil {
		return nil
	}
	tp, ok := so.SoTCPCB.(*TCPCB)
	if !ok {
		return nil
	}
	return tp
}

// TCPRexmtVal calculates the retransmission value.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:158-159
func (tp *TCPCB) TCPRexmtVal() int16 {
	return (tp.TSrtt >> TCPRTTShift) + tp.TRttVar
}

// tcpDooptions parses TCP options from a SYN segment.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1290-1325
func (tp *TCPCB) tcpDooptions(cp []byte, ti *TCPIPHdr) {
	cnt := len(cp)
	i := 0
	for cnt > 0 {
		if i >= len(cp) {
			break
		}
		opt := cp[i]
		if opt == TCPOptEOL {
			break
		}
		var optlen int
		if opt == TCPOptNOP {
			optlen = 1
		} else {
			if i+1 >= len(cp) {
				break
			}
			optlen = int(cp[i+1])
			if optlen <= 0 {
				break
			}
		}
		// Process option
		if opt == TCPOptMaxSeg {
			// Only process if length is correct, SYN flag is set, and we have enough data
			if optlen == TCPOLenMaxSeg && (ti.Flags&THSyn) != 0 && i+4 <= len(cp) {
				mss := uint16(cp[i+2])<<8 | uint16(cp[i+3])
				tp.tcpMss(mss)
			}
		}
		// Move to next option
		cnt -= optlen
		i += optlen
	}
}

// tcpXmitTimer collects a new round-trip time estimate and updates averages
// and current timeout.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1372-1440
func (tp *TCPCB) tcpXmitTimer(rtt int16) {
	if tp.TSrtt != 0 {
		// srtt is stored as fixed point with 3 bits after the
		// binary point (i.e., scaled by 8). The following magic
		// is equivalent to the smoothing algorithm in rfc793 with
		// an alpha of .875 (srtt = rtt/8 + srtt*7/8 in fixed
		// point). Adjust rtt to origin 0.
		delta := rtt - 1 - (tp.TSrtt >> TCPRTTShift)
		tp.TSrtt += delta
		if tp.TSrtt <= 0 {
			tp.TSrtt = 1
		}
		// We accumulate a smoothed rtt variance (actually, a
		// smoothed mean difference), then set the retransmit
		// timer to smoothed rtt + 4 times the smoothed variance.
		// rttvar is stored as fixed point with 2 bits after the
		// binary point (scaled by 4). The following is
		// equivalent to rfc793 smoothing with an alpha of .75
		// (rttvar = rttvar*3/4 + |delta| / 4).
		if delta < 0 {
			delta = -delta
		}
		delta -= tp.TRttVar >> TCPRTTVarShift
		tp.TRttVar += delta
		if tp.TRttVar <= 0 {
			tp.TRttVar = 1
		}
	} else {
		// No rtt measurement yet - use the unsmoothed rtt.
		// Set the variance to half the rtt (so our first
		// retransmit happens at 3*rtt).
		tp.TSrtt = rtt << TCPRTTShift
		tp.TRttVar = rtt << (TCPRTTVarShift - 1)
	}
	tp.TRtt = 0
	tp.TRxtShift = 0

	// The retransmit should happen at rtt + 4 * rttvar.
	// Because of the way we do the smoothing, srtt and rttvar
	// will each average +1/2 tick of bias.
	tp.TRxtCur = tcptRangeSet(tp.TCPRexmtVal(), int16(tp.TRttMin), TCPTVRexmtMax)

	// We received an ack for a packet that wasn't retransmitted;
	// it is probably safe to discard any error indications we've
	// received recently.
	tp.TSoftError = 0
}

// tcpMss determines a reasonable value for maxseg size.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1459-1487
func (tp *TCPCB) tcpMss(offer uint16) int {
	so := tp.TSocket

	// mss = min(IF_MTU, IF_MRU) - sizeof(struct tcpiphdr)
	// tcpiphdr is 40 bytes (20 IP + 20 TCP)
	mss := IFMTU - 40
	if offer != 0 && int(offer) < mss {
		mss = int(offer)
	}
	if mss < 32 {
		mss = 32
	}
	if mss < int(tp.TMaxSeg) || offer != 0 {
		tp.TMaxSeg = uint16(mss)
	}

	tp.SndCwnd = uint32(mss)

	// Reserve socket buffer space, rounding up to mss boundary
	// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1477-1482
	// C calls sbreserve unconditionally; sbreserve reallocates if size differs
	sndSpace := TCPSndSpace
	if (TCPSndSpace % mss) != 0 {
		sndSpace += mss - (TCPSndSpace % mss)
	}
	rcvSpace := TCPRcvSpace
	if (TCPRcvSpace % mss) != 0 {
		rcvSpace += mss - (TCPRcvSpace % mss)
	}
	so.SoSnd.SbReserve(sndSpace)
	so.SoRcv.SbReserve(rcvSpace)

	return mss
}
