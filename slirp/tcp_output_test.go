package slirp

import (
	"testing"
)

// newTestSBuf creates an SBuf with optional initial data for testing.
func newTestSBuf(capacity int, data []byte) SBuf {
	sb := SBuf{}
	sb.SbReserve(capacity)
	if len(data) > 0 {
		sb.SbAppendBytes(data)
	}
	return sb
}

// TestSeqComparisons tests the TCP sequence number comparison functions.
// Reference: tinyemu-2019-12-21/slirp/tcp.h:146-149
func TestSeqComparisons(t *testing.T) {
	tests := []struct {
		name    string
		a, b    uint32
		lt, leq bool
		gt, geq bool
	}{
		// Basic cases
		{"equal", 100, 100, false, true, false, true},
		{"a < b", 100, 200, true, true, false, false},
		{"a > b", 200, 100, false, false, true, true},
		{"zero equal", 0, 0, false, true, false, true},

		// Wraparound tests: when b just wrapped past max
		// 0xFFFFFF00 is 256 less than 0, and 0x00000100 is 256 past 0
		// So 0xFFFFFF00 < 0x00000100 in sequence space (512 apart, a is "before")
		{"wraparound a near max", 0xFFFFFF00, 0x00000100, true, true, false, false},
		{"wraparound b near max", 0x00000100, 0xFFFFFF00, false, false, true, true},

		// Adjacent values around wraparound point
		// 0xFFFFFFFF + 1 = 0x00000000 in sequence space, so 0xFFFFFFFF < 0x00000000
		{"adjacent at wrap", 0xFFFFFFFF, 0x00000000, true, true, false, false},
		{"adjacent at wrap rev", 0x00000000, 0xFFFFFFFF, false, false, true, true},

		// One apart at various points
		{"one apart low", 0, 1, true, true, false, false},
		{"one apart low rev", 1, 0, false, false, true, true},
		{"one apart mid", 0x7FFFFFFF, 0x80000000, true, true, false, false},
		{"one apart mid rev", 0x80000000, 0x7FFFFFFF, false, false, true, true},

		// At exactly 2^31 difference, both sides are "less than" due to signed wraparound
		// This is the edge case where the relationship is ambiguous (exactly half the space apart)
		{"at boundary", 0x80000000, 0x00000000, true, true, false, false},
		{"at boundary rev", 0x00000000, 0x80000000, true, true, false, false},

		// Values just under the boundary (2^31 - 1 apart) - unambiguous
		{"just under boundary", 0x7FFFFFFF, 0x00000000, false, false, true, true},
		{"just under boundary rev", 0x00000000, 0x7FFFFFFF, true, true, false, false},

		// Max value tests
		{"max equal", 0xFFFFFFFF, 0xFFFFFFFF, false, true, false, true},
		{"max vs max-1", 0xFFFFFFFF, 0xFFFFFFFE, false, false, true, true},
		{"max-1 vs max", 0xFFFFFFFE, 0xFFFFFFFF, true, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seqLT(tt.a, tt.b); got != tt.lt {
				t.Errorf("seqLT(%d, %d) = %v, want %v", tt.a, tt.b, got, tt.lt)
			}
			if got := seqLEQ(tt.a, tt.b); got != tt.leq {
				t.Errorf("seqLEQ(%d, %d) = %v, want %v", tt.a, tt.b, got, tt.leq)
			}
			if got := seqGT(tt.a, tt.b); got != tt.gt {
				t.Errorf("seqGT(%d, %d) = %v, want %v", tt.a, tt.b, got, tt.gt)
			}
			if got := seqGEQ(tt.a, tt.b); got != tt.geq {
				t.Errorf("seqGEQ(%d, %d) = %v, want %v", tt.a, tt.b, got, tt.geq)
			}
		})
	}
}

// TestTCPOutflags tests the tcp_outflags array.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:43-47
func TestTCPOutflags(t *testing.T) {
	tests := []struct {
		state    int
		expected uint8
	}{
		{TCPSClosed, THRst | THAck},
		{TCPSListen, 0},
		{TCPSSynSent, THSyn},
		{TCPSSynReceived, THSyn | THAck},
		{TCPSEstablished, THAck},
		{TCPSCloseWait, THAck},
		{TCPSFinWait1, THFin | THAck},
		{TCPSClosing, THFin | THAck},
		{TCPSLastAck, THFin | THAck},
		{TCPSFinWait2, THAck},
		{TCPSTimeWait, THAck},
	}

	for _, tt := range tests {
		if tcpOutflags[tt.state] != tt.expected {
			t.Errorf("tcpOutflags[%d] = %x, want %x", tt.state, tcpOutflags[tt.state], tt.expected)
		}
	}
}

// TestTCPSetPersist tests the TCPSetPersist function.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:479-492
func TestTCPSetPersist(t *testing.T) {
	tp := &TCPCB{
		TSrtt:     100 << TCPRTTShift,
		TRttVar:   50 << TCPRTTVarShift,
		TRxtShift: 0,
	}

	tp.TCPSetPersist()

	// Check that persist timer is set within valid range
	if tp.TTimer[TCPTPersist] < TCPTVPersMin {
		t.Errorf("Persist timer %d is less than min %d", tp.TTimer[TCPTPersist], TCPTVPersMin)
	}
	if tp.TTimer[TCPTPersist] > TCPTVPersMax {
		t.Errorf("Persist timer %d is greater than max %d", tp.TTimer[TCPTPersist], TCPTVPersMax)
	}

	// Check that rxtshift was incremented
	if tp.TRxtShift != 1 {
		t.Errorf("TRxtShift = %d, want 1", tp.TRxtShift)
	}

	// Call again to test backoff
	oldTimer := tp.TTimer[TCPTPersist]
	tp.TCPSetPersist()

	// Timer should be longer (backoff)
	if tp.TTimer[TCPTPersist] <= oldTimer && tp.TTimer[TCPTPersist] < TCPTVPersMax {
		t.Errorf("Persist timer should increase with backoff")
	}

	// Check that rxtshift was incremented again
	if tp.TRxtShift != 2 {
		t.Errorf("TRxtShift = %d, want 2", tp.TRxtShift)
	}
}

// TestTCPSetPersistMaxShift tests that TCPSetPersist caps the shift value.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:490-491
func TestTCPSetPersistMaxShift(t *testing.T) {
	tp := &TCPCB{
		TSrtt:     100 << TCPRTTShift,
		TRttVar:   50 << TCPRTTVarShift,
		TRxtShift: TCPMaxRxtShift,
	}

	tp.TCPSetPersist()

	// Shift should not exceed max
	if tp.TRxtShift > TCPMaxRxtShift {
		t.Errorf("TRxtShift %d exceeds max %d", tp.TRxtShift, TCPMaxRxtShift)
	}
}

// TestTCPOutputNoSocket tests TCPOutput with nil socket.
func TestTCPOutputNoSocket(t *testing.T) {
	tp := &TCPCB{
		TSocket: nil,
	}

	// Should return 0 without panic
	result := tp.TCPOutput()
	if result != 0 {
		t.Errorf("TCPOutput with nil socket = %d, want 0", result)
	}
}

// TestTCPOutputNoSlirp tests TCPOutput with nil slirp.
func TestTCPOutputNoSlirp(t *testing.T) {
	so := &Socket{
		Slirp: nil,
	}
	tp := &TCPCB{
		TSocket: so,
	}

	// Should return 0 without panic
	result := tp.TCPOutput()
	if result != 0 {
		t.Errorf("TCPOutput with nil slirp = %d, want 0", result)
	}
}

// TestTCPOutputIdleSlowStart tests that idle connections slow start.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:76-83
func TestTCPOutputIdleSlowStart(t *testing.T) {
	s := NewSlirp()
	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   newTestSBuf(8192, nil),
		SoRcv:   newTestSBuf(8192, nil),
	}

	tp := &TCPCB{
		TSocket: so,
		TState:  TCPSEstablished,
		TMaxSeg: 1460,
		SndCwnd: 8192, // Large cwnd
		SndWnd:  8192,
		TRxtCur: 1,
		TIdle:   100, // Idle for a while
		SndMax:  1000,
		SndUna:  1000, // snd_max == snd_una means idle
	}
	so.SoTCPCB = tp

	tp.TCPOutput()

	// After idle period, cwnd should be reset to t_maxseg
	if tp.SndCwnd != uint32(tp.TMaxSeg) {
		t.Errorf("SndCwnd after idle = %d, want %d", tp.SndCwnd, tp.TMaxSeg)
	}
}

// TestTCPOutputConstants tests that constants are defined correctly.
func TestTCPOutputConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      int
		expected int
	}{
		{"TCPMaxWin", TCPMaxWin, 65535},
		{"MaxTCPOptLen", MaxTCPOptLen, 32},
		{"TCPNStates", TCPNStates, 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestSBufSpaceInTCPOutput tests that SbSpace works correctly for TCP output.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:12
func TestSBufSpaceInTCPOutput(t *testing.T) {
	var sb SBuf
	sb.SbReserve(1000)
	sb.SbCC = 100

	space := sb.SbSpace()
	if space != 900 {
		t.Errorf("SbSpace = %d, want 900", space)
	}

	// Empty buffer
	var sb2 SBuf
	sb2.SbReserve(500)
	space2 := sb2.SbSpace()
	if space2 != 500 {
		t.Errorf("SbSpace of empty = %d, want 500", space2)
	}

	// Full buffer
	var sb3 SBuf
	sb3.SbReserve(200)
	sb3.SbCC = 200
	space3 := sb3.SbSpace()
	if space3 != 0 {
		t.Errorf("SbSpace of full = %d, want 0", space3)
	}
}

// TestSBufDataLenInTCPOutput tests that SbDataLen works correctly.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:14-22
func TestSBufDataLenInTCPOutput(t *testing.T) {
	var sb SBuf
	sb.SbReserve(1000)
	sb.SbCC = 100

	datalen := sb.SbDataLen
	if datalen != 1000 {
		t.Errorf("SbDataLen = %d, want 1000", datalen)
	}
}

// TestTCPOutputSendAllRemainingData tests that when we can send all remaining
// data, we send it regardless of TFNoDelay being set.
// This tests the fix for the silly window avoidance condition.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:168-170
// Note: C code has (1 || idle || tp->t_flags & TF_NODELAY) which is always true.
func TestTCPOutputSendAllRemainingData(t *testing.T) {
	var outputCalled bool
	var outputPacket []byte

	s := NewSlirp()
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
		outputPacket = make([]byte, len(pkt))
		copy(outputPacket, pkt)
	}
	s.CanOutput = func(opaque interface{}) bool { return true }

	// Create socket with small amount of data (less than t_maxseg)
	data := []byte("hello world")
	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   newTestSBuf(8192, data), // 11 bytes to send
		SoRcv:   newTestSBuf(8192, nil),
		SoIPTos: 0,
	}

	tp := &TCPCB{
		TSocket: so,
		TState:  TCPSEstablished,
		TMaxSeg: 1460, // Much larger than our data
		SndCwnd: 8192,
		SndWnd:  8192,
		SndMax:  1000,
		SndUna:  1000,
		SndNxt:  1000,
		RcvNxt:  2000,
		TFlags:  0, // Specifically NOT setting TFNoDelay
		TRxtCur: 10,
		TTemplate: TCPIPHdr{
			Src:   0x0A000202,
			Dst:   0x0A00020F,
			Sport: 12345,
			Dport: 80,
		},
	}
	so.SoTCPCB = tp

	// Set client ethernet address so we can output
	s.ClientEthAddr = [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	result := tp.TCPOutput()

	// Should succeed
	if result != 0 {
		t.Errorf("TCPOutput() = %d, want 0", result)
	}

	// Output should have been called - this is the key assertion.
	// Before the fix, TFNoDelay was required to send partial data.
	// After the fix, we send whenever we can send all remaining data.
	if !outputCalled {
		t.Error("Expected output to be called when sending all remaining data")
	}

	// Verify that SndNxt was advanced by the data length
	expectedSndNxt := uint32(1000 + len(data))
	if tp.SndNxt != expectedSndNxt {
		t.Errorf("SndNxt = %d, want %d", tp.SndNxt, expectedSndNxt)
	}
	_ = outputPacket // silence unused variable warning
}

// TestTCPOutputDoesNotSendPartialDataIfNotAllRemaining tests that we don't
// send partial data when we can't send all remaining data and other conditions
// aren't met.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:165-177
func TestTCPOutputDoesNotSendPartialDataIfNotAllRemaining(t *testing.T) {
	var outputCalled bool

	s := NewSlirp()
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool { return true }

	// Create socket with data where we can't send all remaining
	data := make([]byte, 2000) // 2000 bytes
	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   newTestSBuf(8192, data),
		SoRcv:   newTestSBuf(8192, nil),
	}

	tp := &TCPCB{
		TSocket:   so,
		TState:    TCPSEstablished,
		TMaxSeg:   1460,
		SndCwnd:   500, // Small window, can only send 500
		SndWnd:    500, // Small window
		SndMax:    1000,
		SndUna:    1000,
		SndNxt:    1000,
		RcvNxt:    2000,
		TFlags:    0, // No TFNoDelay
		MaxSndWnd: 0, // No max_sndwnd check
		TRxtCur:   10,
		TTemplate: TCPIPHdr{
			Src:   0x0A000202,
			Dst:   0x0A00020F,
			Sport: 12345,
			Dport: 80,
		},
	}
	so.SoTCPCB = tp

	s.ClientEthAddr = [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	result := tp.TCPOutput()

	// Should return 0 (no error, but no send either)
	if result != 0 {
		t.Errorf("TCPOutput() = %d, want 0", result)
	}

	// Output should NOT have been called because:
	// - len (500) != t_maxseg (1460)
	// - len + off (500 + 0 = 500) < sb_cc (2000) - can't send all
	// - t_force is 0
	// - max_sndwnd is 0
	// - snd_nxt (1000) == snd_max (1000), not retransmitting
	if outputCalled {
		t.Error("Expected output NOT to be called for partial data when conditions aren't met")
	}

	// Should have set persist timer instead
	if tp.TTimer[TCPTPersist] == 0 {
		t.Error("Expected persist timer to be set")
	}
}

// TestTCPOutputSendMaxSeg tests sending a full segment.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:166-167
func TestTCPOutputSendMaxSeg(t *testing.T) {
	var outputCalled bool

	s := NewSlirp()
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool { return true }

	// Create socket with exactly t_maxseg bytes
	data := make([]byte, 1460)
	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   newTestSBuf(8192, data),
		SoRcv:   newTestSBuf(8192, nil),
	}

	tp := &TCPCB{
		TSocket: so,
		TState:  TCPSEstablished,
		TMaxSeg: 1460,
		SndCwnd: 8192,
		SndWnd:  8192,
		SndMax:  1000,
		SndUna:  1000,
		SndNxt:  1000,
		RcvNxt:  2000,
		TRxtCur: 10,
		TTemplate: TCPIPHdr{
			Src:   0x0A000202,
			Dst:   0x0A00020F,
			Sport: 12345,
			Dport: 80,
		},
	}
	so.SoTCPCB = tp

	s.ClientEthAddr = [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	tp.TCPOutput()

	// Should send because len == t_maxseg
	if !outputCalled {
		t.Error("Expected output to be called when len == t_maxseg")
	}
}

// TestTCPOutputAckNow tests that TFAckNow forces a send.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:204-205
func TestTCPOutputAckNow(t *testing.T) {
	var outputCalled bool

	s := NewSlirp()
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool { return true }

	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   newTestSBuf(8192, nil), // No data
		SoRcv:   newTestSBuf(8192, nil),
	}

	tp := &TCPCB{
		TSocket: so,
		TState:  TCPSEstablished,
		TMaxSeg: 1460,
		SndCwnd: 8192,
		SndWnd:  8192,
		SndMax:  1000,
		SndUna:  1000,
		SndNxt:  1000,
		RcvNxt:  2000,
		TFlags:  TFAckNow, // Force ACK
		TRxtCur: 10,
		TTemplate: TCPIPHdr{
			Src:   0x0A000202,
			Dst:   0x0A00020F,
			Sport: 12345,
			Dport: 80,
		},
	}
	so.SoTCPCB = tp

	s.ClientEthAddr = [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	result := tp.TCPOutput()
	if result != 0 {
		t.Errorf("TCPOutput() = %d, want 0", result)
	}

	// Should send because TFAckNow is set
	if !outputCalled {
		t.Error("Expected output to be called when TFAckNow is set")
	}

	// TFAckNow should be cleared
	if (tp.TFlags & TFAckNow) != 0 {
		t.Error("Expected TFAckNow to be cleared after send")
	}
}

// TestTCPOutputForce tests t_force with zero window.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:99-124
func TestTCPOutputForce(t *testing.T) {
	var outputCalled bool

	s := NewSlirp()
	s.OutputFunc = func(opaque interface{}, pkt []byte) {
		outputCalled = true
	}
	s.CanOutput = func(opaque interface{}) bool { return true }

	data := []byte("hello")
	so := &Socket{
		Slirp:   s,
		SoState: SSIsFConnected,
		SoSnd:   newTestSBuf(8192, data),
		SoRcv:   newTestSBuf(8192, nil),
	}

	tp := &TCPCB{
		TSocket: so,
		TState:  TCPSEstablished,
		TMaxSeg: 1460,
		SndCwnd: 0, // Zero window
		SndWnd:  0, // Zero window
		SndMax:  1000,
		SndUna:  1000,
		SndNxt:  1000,
		RcvNxt:  2000,
		TForce:  1, // Force send
		TRxtCur: 10,
		TTemplate: TCPIPHdr{
			Src:   0x0A000202,
			Dst:   0x0A00020F,
			Sport: 12345,
			Dport: 80,
		},
	}
	so.SoTCPCB = tp

	s.ClientEthAddr = [6]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}

	result := tp.TCPOutput()
	if result != 0 {
		t.Errorf("TCPOutput() = %d, want 0", result)
	}

	// Should send 1 byte due to t_force with zero window
	if !outputCalled {
		t.Error("Expected output to be called when t_force is set")
	}

	// Should have sent exactly 1 byte (SndNxt should advance by 1)
	if tp.SndNxt != 1001 {
		t.Errorf("SndNxt = %d, want 1001 (forced 1 byte)", tp.SndNxt)
	}
}

// TestMarshalTemplate tests that marshalTemplate produces valid headers.
// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:322-324
func TestMarshalTemplate(t *testing.T) {
	tp := &TCPCB{
		TTemplate: TCPIPHdr{
			Src:   0x0A000202, // 10.0.2.2
			Dst:   0x0A00020F, // 10.0.2.15
			Sport: 12345,
			Dport: 80,
		},
	}

	buf := make([]byte, 40)
	tp.marshalTemplate(buf)

	// Check IP version/IHL and TTL are 0 for TCP checksum calculation.
	// These are set by ipOutput before sending the packet.
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:69-72 (ti_mbuf = NULL, ti_x1 = 0)
	if buf[0] != 0x00 {
		t.Errorf("IP version/IHL = %02x, want 0x00 (set by ipOutput)", buf[0])
	}

	// TTL is 0 for checksum, set by tcpOutput after checksum calculation
	if buf[8] != 0x00 {
		t.Errorf("IP TTL = %d, want 0 (set after checksum)", buf[8])
	}

	// Check protocol
	if buf[9] != IPProtoTCP {
		t.Errorf("IP protocol = %d, want %d", buf[9], IPProtoTCP)
	}

	// Check TCP data offset
	if buf[32] != (5 << 4) {
		t.Errorf("TCP data offset = %02x, want %02x", buf[32], 5<<4)
	}
}
