package slirp

// TCP timer definitions and functions.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c

// Timer indices.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:40-45
const (
	TCPTRexmt   = 0 // retransmit
	TCPTPersist = 1 // retransmit persistence
	TCPTKeep    = 2 // keep alive
	TCPT2MSL    = 3 // 2*msl quiet time timer
)

// Time constants.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:86-105
const (
	TCPTVMSL       = 5 * PRSlowHz        // max seg lifetime (hah!)
	TCPTVSrttBase  = 0                   // base roundtrip time; if 0, no idea yet
	TCPTVSrttDflt  = 3 * PRSlowHz        // assumed RTT if no info
	TCPTVPersMin   = 5 * PRSlowHz        // retransmit persistence
	TCPTVPersMax   = 60 * PRSlowHz       // maximum persist interval
	TCPTVKeepInit  = 75 * PRSlowHz       // initial connect keep alive
	TCPTVKeepIdle  = 120 * 60 * PRSlowHz // dflt time before probing
	TCPTVKeepIntvl = 75 * PRSlowHz       // default probe interval
	TCPTVKeepCnt   = 8                   // max probes before drop
	TCPTVMin       = 1 * PRSlowHz        // minimum allowable value
	TCPTVRexmtMax  = 12 * PRSlowHz       // max allowable REXMT value
	TCPLingerTime  = 120                 // linger at most 2 minutes
	TCPMaxRxtShift = 12                  // maximum retransmits
)

// TCPMaxIdle is the maximum idle time before connection is dropped.
// Reference: tinyemu-2019-12-21/slirp/slirp.h:254
const TCPMaxIdle = TCPTVKeepCnt * TCPTVKeepIntvl

// SOOptions controls whether keepalive is enabled.
// Reference: tinyemu-2019-12-21/slirp/slirp.h:253
// Reference: tinyemu-2019-12-21/slirp/slirp_config.h:10
// DO_KEEPALIVE is 0, so SO_OPTIONS is always false.
const SOOptions = false

// TCPBackoff is the backoff multiplier table for retransmissions.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:115-116
var TCPBackoff = [TCPMaxRxtShift + 1]int{1, 2, 4, 8, 16, 32, 64, 64, 64, 64, 64, 64, 64}

// tcptRangeSet forces a time value to be in a certain range.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:111-117
func tcptRangeSet(value, tvmin, tvmax int16) int16 {
	if value < tvmin {
		return tvmin
	}
	if value > tvmax {
		return tvmax
	}
	return value
}

// TCPFasttimo is the fast timeout routine for processing delayed acks.
// Called every 200ms.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:40-57
func (slirp *Slirp) TCPFasttimo() {
	so := slirp.TCB.Next
	if so == nil {
		return
	}
	for so != &slirp.TCB {
		tp := SoToTCPCB(so)
		if tp != nil && (tp.TFlags&TFDelAck) != 0 {
			tp.TFlags &^= TFDelAck
			tp.TFlags |= TFAckNow
			tp.TCPOutput()
		}
		so = so.Next
	}
}

// TCPSlowtimo is the TCP protocol timeout routine called every 500 ms.
// Updates the timers in all active tcb's and causes finite state machine
// actions if timers expire.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:64-101
func (slirp *Slirp) TCPSlowtimo() {
	ip := slirp.TCB.Next
	if ip == nil {
		return
	}
	for ip != &slirp.TCB {
		ipnxt := ip.Next
		tp := SoToTCPCB(ip)
		if tp == nil {
			ip = ipnxt
			continue
		}
		for i := 0; i < TCPTNTimers; i++ {
			if tp.TTimer[i] != 0 {
				tp.TTimer[i]--
				if tp.TTimer[i] == 0 {
					tp.tcpTimers(i)
					// Check if socket was removed (connection closed)
					if ipnxt.Prev != ip {
						goto tpgone
					}
				}
			}
		}
		tp.TIdle++
		if tp.TRtt != 0 {
			tp.TRtt++
		}
	tpgone:
		ip = ipnxt
	}
	slirp.TCPIss += TCPIssIncr / PRSlowHz // increment iss
	slirp.TCPNow++                        // for timestamps
}

// TCPCancelTimers cancels all timers for a TCP connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:106-113
func (tp *TCPCB) TCPCancelTimers() {
	for i := 0; i < TCPTNTimers; i++ {
		tp.TTimer[i] = 0
	}
}

// tcpTimers handles TCP timer processing.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:121-292
func (tp *TCPCB) tcpTimers(timer int) *TCPCB {
	switch timer {
	case TCPT2MSL:
		// 2 MSL timeout in shutdown went off.
		// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:136-142
		if tp.TState != TCPSTimeWait && tp.TIdle <= TCPMaxIdle {
			tp.TTimer[TCPT2MSL] = TCPTVKeepIntvl
		} else {
			tp = tp.TCPClose()
		}

	case TCPTRexmt:
		// Retransmission timer went off.
		// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:149-243
		tp.TRxtShift++
		if tp.TRxtShift > TCPMaxRxtShift {
			// Hack: halve t_maxseg to break up NULLs
			tp.TMaxSeg >>= 1
			if tp.TMaxSeg < 32 {
				// Connection must die
				tp.TRxtShift = TCPMaxRxtShift
				tp = tp.TCPDrop(int(tp.TSoftError))
				return tp
			}
			// Set rxtshift to 6, still at maximum backoff time
			tp.TRxtShift = 6
		}
		rexmt := int16(tp.TCPRexmtVal()) * int16(TCPBackoff[tp.TRxtShift])
		tp.TRxtCur = tcptRangeSet(rexmt, int16(tp.TRttMin), TCPTVRexmtMax)
		tp.TTimer[TCPTRexmt] = tp.TRxtCur

		// If losing, clobber srtt estimate
		if tp.TRxtShift > TCPMaxRxtShift/4 {
			tp.TRttVar += tp.TSrtt >> TCPRTTShift
			tp.TSrtt = 0
		}
		tp.SndNxt = tp.SndUna
		tp.TRtt = 0

		// Close congestion window down to one segment
		win := min(tp.SndWnd, tp.SndCwnd) / 2 / uint32(tp.TMaxSeg)
		if win < 2 {
			win = 2
		}
		tp.SndCwnd = uint32(tp.TMaxSeg)
		tp.SndSsthresh = win * uint32(tp.TMaxSeg)
		tp.TDupAcks = 0
		tp.TCPOutput()

	case TCPTPersist:
		// Persistence timer into zero window.
		// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:249-254
		tp.TCPSetPersist()
		tp.TForce = 1
		tp.TCPOutput()
		tp.TForce = 0

	case TCPTKeep:
		// Keep-alive timer went off.
		// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:260-288
		if tp.TState < TCPSEstablished {
			tp = tp.TCPDrop(0)
			return tp
		}

		if SOOptions && tp.TState <= TCPSCloseWait {
			if tp.TIdle >= TCPTVKeepIdle+TCPMaxIdle {
				tp = tp.TCPDrop(0)
				return tp
			}
			// Send keepalive probe
			tp.TCPRespond(nil, tp.RcvNxt, tp.SndUna-1, 0)
			tp.TTimer[TCPTKeep] = TCPTVKeepIntvl
		} else {
			tp.TTimer[TCPTKeep] = TCPTVKeepIdle
		}
	}

	return tp
}

// TCPOutput and TCPSetPersist are implemented in tcp_output.go

// TCPClose closes a TCP connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:236-265
func (tp *TCPCB) TCPClose() *TCPCB {
	if tp == nil {
		return nil
	}
	so := tp.TSocket
	if so == nil {
		return nil
	}
	slirp := so.Slirp

	// Free the reassembly queue
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:247-254
	for !tp.tcpfragListEmpty() {
		ti := tp.tcpfragListFirst()
		m := ti.Mbuf
		tp.tcpfragRemove(ti)
		if m != nil {
			m.MFree()
		}
	}

	// Clear TCP control block from socket
	so.SoTCPCB = nil

	// Clobber input socket cache
	if slirp != nil && so == slirp.TCPLastSo {
		slirp.TCPLastSo = &slirp.TCB
	}

	// Close the socket fd
	if so.S >= 0 {
		sockClose(so.S)
		so.S = -1
	}

	// Free socket buffers
	so.SoRcv.SbFree()
	so.SoSnd.SbFree()

	// Free the socket
	so.SoFree()

	return nil
}

// TCPDrop drops a TCP connection, reporting the specified error.
// If connection is synchronized, send a RST to peer.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:217-228
func (tp *TCPCB) TCPDrop(errno int) *TCPCB {
	if tp == nil {
		return nil
	}

	if TCPSHaveRcvdSyn(tp.TState) {
		tp.TState = TCPSClosed
		tp.TCPOutput()
	}
	return tp.TCPClose()
}

// TCPRespond sends a TCP response.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:103
func (tp *TCPCB) TCPRespond(m *Mbuf, ack, seq uint32, flags int) {
	if tp == nil || tp.TSocket == nil || tp.TSocket.Slirp == nil {
		return
	}
	tp.TSocket.Slirp.tcpRespond(tp, m, ack, seq, flags)
}
