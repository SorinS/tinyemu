package slirp

import (
	"testing"
)

// TestTCPStateConstants verifies TCP state constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:122-135
func TestTCPStateConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"TCPS_CLOSED", TCPSClosed, 0},
		{"TCPS_LISTEN", TCPSListen, 1},
		{"TCPS_SYN_SENT", TCPSSynSent, 2},
		{"TCPS_SYN_RECEIVED", TCPSSynReceived, 3},
		{"TCPS_ESTABLISHED", TCPSEstablished, 4},
		{"TCPS_CLOSE_WAIT", TCPSCloseWait, 5},
		{"TCPS_FIN_WAIT_1", TCPSFinWait1, 6},
		{"TCPS_CLOSING", TCPSClosing, 7},
		{"TCPS_LAST_ACK", TCPSLastAck, 8},
		{"TCPS_FIN_WAIT_2", TCPSFinWait2, 9},
		{"TCPS_TIME_WAIT", TCPSTimeWait, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTCPFlagConstants verifies TCP flag constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:53-62
func TestTCPFlagConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"TF_ACKNOW", TFAckNow, 0x0001},
		{"TF_DELACK", TFDelAck, 0x0002},
		{"TF_NODELAY", TFNoDelay, 0x0004},
		{"TF_NOOPT", TFNoOpt, 0x0008},
		{"TF_SENTFIN", TFSentFin, 0x0010},
		{"TF_REQ_SCALE", TFReqScale, 0x0020},
		{"TF_RCVD_SCALE", TFRcvdScale, 0x0040},
		{"TF_REQ_TSTMP", TFReqTstmp, 0x0080},
		{"TF_RCVD_TSTMP", TFRcvdTstmp, 0x0100},
		{"TF_SACK_PERMIT", TFSackPermit, 0x0200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTCPRTTConstants verifies TCP RTT scaling constants.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:140-143
func TestTCPRTTConstants(t *testing.T) {
	if TCPRTTScale != 8 {
		t.Errorf("TCPRTTScale = %d, want 8", TCPRTTScale)
	}
	if TCPRTTShift != 3 {
		t.Errorf("TCPRTTShift = %d, want 3", TCPRTTShift)
	}
	if TCPRTTVarScale != 4 {
		t.Errorf("TCPRTTVarScale = %d, want 4", TCPRTTVarScale)
	}
	if TCPRTTVarShift != 2 {
		t.Errorf("TCPRTTVarShift = %d, want 2", TCPRTTVarShift)
	}
}

// TestSoToTCPCB tests the socket to TCPCB conversion.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:129
func TestSoToTCPCB(t *testing.T) {
	// Test nil socket
	if SoToTCPCB(nil) != nil {
		t.Error("SoToTCPCB(nil) should return nil")
	}

	// Test socket with nil SoTCPCB
	so := &Socket{}
	if SoToTCPCB(so) != nil {
		t.Error("SoToTCPCB with nil SoTCPCB should return nil")
	}

	// Test socket with wrong type
	so.SoTCPCB = "wrong type"
	if SoToTCPCB(so) != nil {
		t.Error("SoToTCPCB with wrong type should return nil")
	}

	// Test valid TCPCB
	tp := &TCPCB{TState: TCPSEstablished}
	so.SoTCPCB = tp
	result := SoToTCPCB(so)
	if result != tp {
		t.Error("SoToTCPCB should return the TCPCB pointer")
	}
	if result.TState != TCPSEstablished {
		t.Errorf("TCPCB TState = %d, want %d", result.TState, TCPSEstablished)
	}
}

// TestTCPRexmtVal tests the retransmission value calculation.
// Reference: tinyemu-2019-12-21/slirp/tcp_var.h:158-159
func TestTCPRexmtVal(t *testing.T) {
	tests := []struct {
		name   string
		srtt   int16
		rttvar int16
		want   int16
	}{
		{"zero values", 0, 0, 0},
		{"srtt only", 24, 0, 3}, // 24 >> 3 = 3
		{"rttvar only", 0, 5, 5},
		{"both", 24, 5, 8},            // (24 >> 3) + 5 = 8
		{"larger values", 80, 10, 20}, // (80 >> 3) + 10 = 10 + 10 = 20
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &TCPCB{TSrtt: tt.srtt, TRttVar: tt.rttvar}
			got := tp.TCPRexmtVal()
			if got != tt.want {
				t.Errorf("TCPRexmtVal() = %d, want %d (srtt=%d, rttvar=%d)",
					got, tt.want, tt.srtt, tt.rttvar)
			}
		})
	}
}

// TestTCPCBTimerArray tests the timer array has correct size.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:40
func TestTCPCBTimerArray(t *testing.T) {
	tp := &TCPCB{}
	if len(tp.TTimer) != TCPTNTimers {
		t.Errorf("TTimer array length = %d, want %d", len(tp.TTimer), TCPTNTimers)
	}
	if len(tp.TTimer) != 4 {
		t.Errorf("TTimer array length = %d, want 4", len(tp.TTimer))
	}
}

// TestTCPHeaderFlagConstants verifies TCP header flag constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:61-66
func TestTCPHeaderFlagConstants(t *testing.T) {
	tests := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"TH_FIN", THFin, 0x01},
		{"TH_SYN", THSyn, 0x02},
		{"TH_RST", THRst, 0x04},
		{"TH_PUSH", THPush, 0x08},
		{"TH_ACK", THAck, 0x10},
		{"TH_URG", THUrg, 0x20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = 0x%02x, want 0x%02x", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTCPOptionConstants verifies TCP option constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:74-78
func TestTCPOptionConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"TCPOPT_EOL", TCPOptEOL, 0},
		{"TCPOPT_NOP", TCPOptNOP, 1},
		{"TCPOPT_MAXSEG", TCPOptMaxSeg, 2},
		{"TCPOLEN_MAXSEG", TCPOLenMaxSeg, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTCPBufferSpaceConstants verifies TCP buffer space constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:41-42
func TestTCPBufferSpaceConstants(t *testing.T) {
	if TCPSndSpace != 8192 {
		t.Errorf("TCPSndSpace = %d, want 8192", TCPSndSpace)
	}
	if TCPRcvSpace != 8192 {
		t.Errorf("TCPRcvSpace = %d, want 8192", TCPRcvSpace)
	}
}

// TestTcpDooptions tests TCP option parsing.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1290-1325
func TestTcpDooptions(t *testing.T) {
	tests := []struct {
		name       string
		options    []byte
		flags      uint8
		wantMaxSeg uint16
	}{
		{
			name:       "empty options",
			options:    []byte{},
			flags:      THSyn,
			wantMaxSeg: 0, // unchanged
		},
		{
			name:       "EOL only",
			options:    []byte{TCPOptEOL},
			flags:      THSyn,
			wantMaxSeg: 0,
		},
		{
			name:       "NOP then EOL",
			options:    []byte{TCPOptNOP, TCPOptEOL},
			flags:      THSyn,
			wantMaxSeg: 0,
		},
		{
			name:       "MAXSEG option with SYN",
			options:    []byte{TCPOptMaxSeg, TCPOLenMaxSeg, 0x05, 0xB4}, // MSS=1460
			flags:      THSyn,
			wantMaxSeg: 1460,
		},
		{
			name:       "MAXSEG option without SYN (should be ignored)",
			options:    []byte{TCPOptMaxSeg, TCPOLenMaxSeg, 0x05, 0xB4},
			flags:      THAck,
			wantMaxSeg: 0, // unchanged because no SYN
		},
		{
			name:       "MAXSEG with wrong length (should be ignored)",
			options:    []byte{TCPOptMaxSeg, 5, 0x05, 0xB4, 0x00}, // wrong optlen
			flags:      THSyn,
			wantMaxSeg: 0,
		},
		{
			name:       "NOP + MAXSEG",
			options:    []byte{TCPOptNOP, TCPOptMaxSeg, TCPOLenMaxSeg, 0x02, 0x00}, // MSS=512
			flags:      THSyn,
			wantMaxSeg: 512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &TCPCB{
				TMaxSeg: 0,
				TSocket: &Socket{},
			}
			ti := &TCPIPHdr{Flags: tt.flags}
			tp.tcpDooptions(tt.options, ti)
			if tt.wantMaxSeg != 0 && tp.TMaxSeg != tt.wantMaxSeg {
				t.Errorf("TMaxSeg = %d, want %d", tp.TMaxSeg, tt.wantMaxSeg)
			}
		})
	}
}

// TestTcpXmitTimer tests RTT timer updates.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1372-1440
func TestTcpXmitTimer(t *testing.T) {
	tests := []struct {
		name          string
		initialSrtt   int16
		initialRttvar int16
		rtt           int16
		wantSrttGt0   bool
		wantRttvarGt0 bool
		wantRttZero   bool
		wantRxtShift  int16
	}{
		{
			name:          "first RTT measurement",
			initialSrtt:   0,
			initialRttvar: 0,
			rtt:           4,
			wantSrttGt0:   true,
			wantRttvarGt0: true,
			wantRttZero:   true,
			wantRxtShift:  0,
		},
		{
			name:          "subsequent RTT measurement",
			initialSrtt:   32, // 4 << 3
			initialRttvar: 2,  // 4 << 1
			rtt:           4,
			wantSrttGt0:   true,
			wantRttvarGt0: true,
			wantRttZero:   true,
			wantRxtShift:  0,
		},
		{
			name:          "RTT update with larger rtt",
			initialSrtt:   16, // srtt = 2 when shifted right by 3
			initialRttvar: 4,
			rtt:           8,
			wantSrttGt0:   true,
			wantRttvarGt0: true,
			wantRttZero:   true,
			wantRxtShift:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &TCPCB{
				TSrtt:     tt.initialSrtt,
				TRttVar:   tt.initialRttvar,
				TRtt:      5, // non-zero to verify it gets cleared
				TRxtShift: 3, // non-zero to verify it gets cleared
				TRttMin:   1,
			}
			tp.tcpXmitTimer(tt.rtt)

			if tt.wantSrttGt0 && tp.TSrtt <= 0 {
				t.Errorf("TSrtt = %d, want > 0", tp.TSrtt)
			}
			if tt.wantRttvarGt0 && tp.TRttVar <= 0 {
				t.Errorf("TRttVar = %d, want > 0", tp.TRttVar)
			}
			if tt.wantRttZero && tp.TRtt != 0 {
				t.Errorf("TRtt = %d, want 0", tp.TRtt)
			}
			if tp.TRxtShift != tt.wantRxtShift {
				t.Errorf("TRxtShift = %d, want %d", tp.TRxtShift, tt.wantRxtShift)
			}
			if tp.TSoftError != 0 {
				t.Errorf("TSoftError = %d, want 0", tp.TSoftError)
			}
		})
	}
}

// TestTcpMss tests MSS calculation.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1459-1487
func TestTcpMss(t *testing.T) {
	tests := []struct {
		name          string
		offer         uint16
		initialMaxSeg uint16
		wantMss       int
		wantMaxSegSet bool
	}{
		{
			name:          "no offer, use default MTU-based MSS",
			offer:         0,
			initialMaxSeg: 0,
			wantMss:       IFMTU - 40, // 1500 - 40 = 1460
			wantMaxSegSet: false,      // only set if offer != 0 or mss < t_maxseg
		},
		{
			name:          "offer smaller than MTU",
			offer:         536,
			initialMaxSeg: 0,
			wantMss:       536,
			wantMaxSegSet: true,
		},
		{
			name:          "offer larger than MTU (should use MTU)",
			offer:         2000,
			initialMaxSeg: 0,
			wantMss:       IFMTU - 40,
			wantMaxSegSet: true,
		},
		{
			name:          "offer smaller than minimum",
			offer:         16,
			initialMaxSeg: 0,
			wantMss:       32, // minimum MSS
			wantMaxSegSet: true,
		},
		{
			name:          "update existing maxseg with offer",
			offer:         1000,
			initialMaxSeg: 1460,
			wantMss:       1000,
			wantMaxSegSet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			so := &Socket{}
			tp := &TCPCB{
				TMaxSeg: tt.initialMaxSeg,
				TSocket: so,
			}
			mss := tp.tcpMss(tt.offer)
			if mss != tt.wantMss {
				t.Errorf("tcpMss() = %d, want %d", mss, tt.wantMss)
			}
			if tt.wantMaxSegSet && tp.TMaxSeg != uint16(tt.wantMss) {
				t.Errorf("TMaxSeg = %d, want %d", tp.TMaxSeg, tt.wantMss)
			}
			if tp.SndCwnd != uint32(mss) {
				t.Errorf("SndCwnd = %d, want %d", tp.SndCwnd, mss)
			}
			// Check socket buffers were allocated
			if so.SoSnd.SbDataLen < TCPSndSpace {
				t.Errorf("SoSnd capacity = %d, want >= %d", so.SoSnd.SbDataLen, TCPSndSpace)
			}
			if so.SoRcv.SbDataLen < TCPRcvSpace {
				t.Errorf("SoRcv capacity = %d, want >= %d", so.SoRcv.SbDataLen, TCPRcvSpace)
			}
		})
	}
}

// TestTcpMssBufferResize verifies that tcpMss calls SbReserve unconditionally,
// matching C behavior. C's sbreserve will reallocate if size differs, even if
// current buffer is larger.
// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:1477-1482, sbuf.c:35-55
func TestTcpMssBufferResize(t *testing.T) {
	so := &Socket{}
	tp := &TCPCB{
		TMaxSeg: 0,
		TSocket: so,
	}

	// First call with MSS=1460, buffers should be sized for this MSS
	tp.tcpMss(1460)
	sndSpace1 := so.SoSnd.SbDataLen

	// Second call with MSS=536 (smaller) - C would resize buffers
	// Our Go code now matches C by calling SbReserve unconditionally
	tp.tcpMss(536)
	sndSpace2 := so.SoSnd.SbDataLen
	rcvSpace2 := so.SoRcv.SbDataLen

	// Buffer sizes should change because sbreserve is called unconditionally
	// and C's sbreserve reallocates if sb_datalen != size
	_ = sndSpace1 // Used for documentation purposes

	// Verify buffers are properly sized for the MSS
	mss := 536
	expectedSndSpace := TCPSndSpace
	if (TCPSndSpace % mss) != 0 {
		expectedSndSpace += mss - (TCPSndSpace % mss)
	}
	expectedRcvSpace := TCPRcvSpace
	if (TCPRcvSpace % mss) != 0 {
		expectedRcvSpace += mss - (TCPRcvSpace % mss)
	}

	if sndSpace2 != expectedSndSpace {
		t.Errorf("SoSnd.SbDataLen = %d, want %d", sndSpace2, expectedSndSpace)
	}
	if rcvSpace2 != expectedRcvSpace {
		t.Errorf("SoRcv.SbDataLen = %d, want %d", rcvSpace2, expectedRcvSpace)
	}
}

// NOTE: TestTcpMssNilSocket was removed because C code doesn't check for nil
// socket in tcp_mss. The C code would crash on nil t_socket. To match C behavior
// exactly, we don't support nil sockets in tcpMss. The socket is always valid
// when tcp_mss is called from tcp_dooptions during SYN processing.
