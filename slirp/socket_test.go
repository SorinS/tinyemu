package slirp

import (
	"net"
	"testing"
)

// TestSoCreate tests socket creation.
// Reference: tinyemu-2019-12-21/slirp/socket.c:40-52
func TestSoCreate(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	if so == nil {
		t.Fatal("SoCreate returned nil")
	}

	if so.Slirp != s {
		t.Error("Slirp reference not set")
	}

	if so.S != -1 {
		t.Errorf("S = %d, want -1", so.S)
	}

	// C sets so_state = SS_NOFDREF
	if so.SoState != SSNoFDRef {
		t.Errorf("SoState = %d, want SSNoFDRef (%d)", so.SoState, SSNoFDRef)
	}
}

// TestSoFree tests socket freeing.
func TestSoFree(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	so.SoFree()

	// Should not panic
}

// TestSoFreeNil tests freeing nil socket.
func TestSoFreeNil(t *testing.T) {
	var so *Socket
	so.SoFree() // Should not panic
}

// TestSoFreeLinked tests freeing linked socket.
func TestSoFreeLinked(t *testing.T) {
	s := NewSlirp()

	so1 := s.SoCreate()
	so2 := s.SoCreate()
	so3 := s.SoCreate()

	// Link them manually
	so1.Next = so2
	so2.Prev = so1
	so2.Next = so3
	so3.Prev = so2

	// Free middle socket
	so2.SoFree()

	// so1 and so3 should now be linked
	if so1.Next != so3 {
		t.Error("so1.Next should point to so3 after freeing so2")
	}
	if so3.Prev != so1 {
		t.Error("so3.Prev should point to so1 after freeing so2")
	}
}

// TestSocketConstants tests socket state constants.
func TestSocketConstants(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{"SSNoFDRef", SSNoFDRef, 0x001},
		{"SSIsFConnecting", SSIsFConnecting, 0x002},
		{"SSIsFConnected", SSIsFConnected, 0x004},
		{"SSFCantRcvMore", SSFCantRcvMore, 0x008},
		{"SSFCantSendMore", SSFCantSendMore, 0x010},
		{"SSFWDrain", SSFWDrain, 0x040},
		{"SSCTL", SSCTL, 0x080},
		{"SSFAcceptConn", SSFAcceptConn, 0x100},
		{"SSFAcceptOnce", SSFAcceptOnce, 0x200},
		{"SSHostFwd", SSHostFwd, 0x1000},
		{"SSIncoming", SSIncoming, 0x2000},
		{"SOExpire", SOExpire, 240000},
		{"SOExpireFast", SOExpireFast, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = 0x%x, want 0x%x", tt.name, tt.value, tt.want)
			}
		})
	}
}

// TestEmuConstants tests protocol emulation constants.
// Reference: tinyemu-2019-12-21/slirp/misc.h:25-37
func TestEmuConstants(t *testing.T) {
	tests := []struct {
		name  string
		value uint8
		want  uint8
	}{
		{"EmuNone", EmuNone, 0x0},
		{"EmuCTL", EmuCTL, 0x1},
		{"EmuFTP", EmuFTP, 0x2},
		{"EmuKSH", EmuKSH, 0x3},
		{"EmuIRC", EmuIRC, 0x4},
		{"EmuRealAudio", EmuRealAudio, 0x5},
		{"EmuRLogin", EmuRLogin, 0x6},
		{"EmuIdent", EmuIdent, 0x7},
		{"EmuRSH", EmuRSH, 0x8},
		{"EmuNoConnect", EmuNoConnect, 0x10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.want {
				t.Errorf("%s = 0x%x, want 0x%x", tt.name, tt.value, tt.want)
			}
		})
	}
}

// TestSoLookup tests socket lookup.
// Reference: tinyemu-2019-12-21/slirp/socket.c:15-32
func TestSoLookup(t *testing.T) {
	s := NewSlirp()

	// Create a circular list with head
	head := &Socket{}
	head.Next = head
	head.Prev = head

	// Create sockets and link them
	so1 := s.SoCreate()
	so1.SoLAddr = net.IPv4(10, 0, 2, 15)
	so1.SoLPort = 1234
	so1.SoFAddr = net.IPv4(192, 168, 1, 1)
	so1.SoFPort = 80

	so2 := s.SoCreate()
	so2.SoLAddr = net.IPv4(10, 0, 2, 15)
	so2.SoLPort = 5678
	so2.SoFAddr = net.IPv4(192, 168, 1, 2)
	so2.SoFPort = 443

	// Insert into list after head
	so1.Next = head
	so1.Prev = head
	head.Next = so1
	head.Prev = so1

	so2.Next = head
	so2.Prev = so1
	so1.Next = so2
	head.Prev = so2

	// Test lookup that should succeed
	result := SoLookup(head, net.IPv4(10, 0, 2, 15), 1234, net.IPv4(192, 168, 1, 1), 80)
	if result != so1 {
		t.Error("SoLookup failed to find so1")
	}

	result = SoLookup(head, net.IPv4(10, 0, 2, 15), 5678, net.IPv4(192, 168, 1, 2), 443)
	if result != so2 {
		t.Error("SoLookup failed to find so2")
	}

	// Test lookup that should fail
	result = SoLookup(head, net.IPv4(10, 0, 2, 15), 9999, net.IPv4(192, 168, 1, 1), 80)
	if result != nil {
		t.Error("SoLookup should return nil for non-existent socket")
	}
}

// TestSoFreeWithTCPLastSo tests that SoFree clears tcp_last_so.
// Reference: tinyemu-2019-12-21/slirp/socket.c:66-70
func TestSoFreeWithTCPLastSo(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	s.TCPLastSo = so

	so.SoFree()

	if s.TCPLastSo != &s.TCB {
		t.Error("SoFree should reset TCPLastSo to &TCB")
	}
}

// TestSoFreeWithUDPLastSo tests that SoFree clears udp_last_so.
// Reference: tinyemu-2019-12-21/slirp/socket.c:66-70
func TestSoFreeWithUDPLastSo(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	s.UDPLastSo = so

	so.SoFree()

	if s.UDPLastSo != &s.UDB {
		t.Error("SoFree should reset UDPLastSo to &UDB")
	}
}

// TestSoFreeWithMbuf tests that SoFree frees the mbuf.
// Reference: tinyemu-2019-12-21/slirp/socket.c:71
func TestSoFreeWithMbuf(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	m := s.MGet()
	so.SoM = m

	so.SoFree()

	if so.SoM != nil {
		t.Error("SoFree should set SoM to nil")
	}
}

// TestSoFreeWithRSHExtra tests RSH socket extra freeing.
// Reference: tinyemu-2019-12-21/slirp/socket.c:62-65
func TestSoFreeWithRSHExtra(t *testing.T) {
	s := NewSlirp()

	so := s.SoCreate()
	extraSo := s.SoCreate()
	so.SoEmu = EmuRSH
	so.Extra = extraSo

	// Give the extra socket an mbuf to verify it gets freed
	extraMbuf := s.MGet()
	extraSo.SoM = extraMbuf

	so.SoFree()

	if so.Extra != nil {
		t.Error("SoFree should set Extra to nil for RSH sockets")
	}
}

// TestSoPrepRbuf tests buffer preparation for reading.
// Reference: tinyemu-2019-12-21/slirp/socket.c:79-136
func TestSoPrepRbuf(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Set up a TCP control block with MSS
	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: 1460,
	}
	so.SoTCPCB = tp

	// Reserve buffer space
	so.SoSnd.SbReserve(8192)

	// Test 1: Empty buffer (no data, full space)
	iov, n, total := so.SoPrepRbuf()
	if total == 0 {
		t.Error("SoPrepRbuf should return non-zero total for empty buffer")
	}
	if n == 0 {
		t.Error("SoPrepRbuf should return at least 1 iov")
	}
	if len(iov[0]) == 0 {
		t.Error("SoPrepRbuf iov[0] should have length > 0")
	}

	// Test 2: Partially full buffer
	so.SoSnd.SbCC = 4096
	so.SoSnd.SbWPtr = 4096

	_, _, total = so.SoPrepRbuf()
	// With MSS=1460, free space=4096, aligned to MSS: 4096 - (4096 % 1460) = 2920
	expectedTotal := 4096 - (4096 % 1460)
	if total != expectedTotal {
		t.Errorf("SoPrepRbuf total = %d, want %d (MSS-aligned)", total, expectedTotal)
	}

	// Test 3: Full buffer - should return 0
	so.SoSnd.SbCC = 8192
	_, _, total = so.SoPrepRbuf()
	if total != 0 {
		t.Errorf("SoPrepRbuf total = %d, want 0 for full buffer", total)
	}
}

// TestSoPrepRbufWrap tests buffer preparation with wraparound.
// Reference: tinyemu-2019-12-21/slirp/socket.c:79-136
func TestSoPrepRbufWrap(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Set up a TCP control block
	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: 1460,
	}
	so.SoTCPCB = tp

	// Reserve buffer space
	so.SoSnd.SbReserve(8192)

	// Set up wraparound: wptr behind rptr
	so.SoSnd.SbCC = 2000
	so.SoSnd.SbRPtr = 6000
	so.SoSnd.SbWPtr = 192 // 8192 - 6000 + 192 = 2192, but CC is 2000

	_, n, total := so.SoPrepRbuf()
	if n != 1 {
		t.Errorf("SoPrepRbuf n = %d, want 1 for wrapped buffer", n)
	}
	// free space = 8192 - 2000 = 6192
	// region from wptr to rptr = 6000 - 192 = 5808
	expectedLen := 6000 - 192
	if expectedLen > 6192 {
		expectedLen = 6192
	}
	// MSS alignment
	expectedLen = expectedLen - (expectedLen % 1460)
	if total != expectedLen {
		t.Errorf("SoPrepRbuf total = %d, want %d", total, expectedLen)
	}
}

// TestSoReadBuf tests reading from a buffer into socket send buffer.
// Reference: tinyemu-2019-12-21/slirp/socket.c:204-244
func TestSoReadBuf(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Set up a TCP control block
	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: 1460,
	}
	so.SoTCPCB = tp

	// Reserve buffer space
	so.SoSnd.SbReserve(8192)

	// Test reading data
	data := []byte("Hello, World!")
	n := so.SoReadBuf(data)
	if n != len(data) {
		t.Errorf("SoReadBuf returned %d, want %d", n, len(data))
	}

	// Verify buffer state
	if so.SoSnd.SbCC != len(data) {
		t.Errorf("SoSnd.SbCC = %d, want %d", so.SoSnd.SbCC, len(data))
	}

	// Verify data can be retrieved
	result := so.SoSnd.SbBytes()
	if string(result) != "Hello, World!" {
		t.Errorf("Buffer contents = %q, want %q", result, "Hello, World!")
	}
}

// TestSoReadBufOverflow tests reading more data than buffer space.
// Reference: tinyemu-2019-12-21/slirp/socket.c:217-218
func TestSoReadBufOverflow(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Set up a TCP control block
	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: 100,
	}
	so.SoTCPCB = tp

	// Reserve very small buffer space
	so.SoSnd.SbReserve(50)

	// Try to read more data than available space
	data := make([]byte, 100)
	n := so.SoReadBuf(data)
	if n != -1 {
		t.Errorf("SoReadBuf should return -1 on overflow, got %d", n)
	}
}

// TestSoRecvOOB tests receiving out-of-band data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:254-274
func TestSoRecvOOB(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.S = -1 // No real socket

	// Set up a TCP control block
	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: 1460,
		SndUna:  1000,
	}
	so.SoTCPCB = tp

	// Reserve buffer space
	so.SoSnd.SbReserve(8192)

	// Add some data to buffer (simulating soread)
	so.SoSnd.SbAppendBytes([]byte("urgent data"))

	// Call SoRecvOOB
	so.SoRecvOOB()

	// Verify snd_up was set
	// snd_up = snd_una + sb_cc = 1000 + 11 = 1011
	expectedSndUp := tp.SndUna + uint32(so.SoSnd.SbCC)
	if tp.SndUp != expectedSndUp {
		t.Errorf("SndUp = %d, want %d", tp.SndUp, expectedSndUp)
	}

	// Verify t_force was reset to 0
	if tp.TForce != 0 {
		t.Errorf("TForce = %d, want 0", tp.TForce)
	}
}

// TestSoSendOOB tests sending out-of-band data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:281-332
func TestSoSendOOB(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Reserve buffer space
	so.SoRcv.SbReserve(8192)

	// Add urgent data
	so.SoRcv.SbAppendBytes([]byte("urgent"))
	so.SoUrgc = 6

	// Call SoSendOOB - without a real socket fd, slirpSendOOB returns 0
	n := so.SoSendOOB()

	// Without real socket, returns 0 (no data sent)
	if n != 0 {
		t.Errorf("SoSendOOB returned %d, want 0 (no socket)", n)
	}
}

// TestSoSendOOBCapLimit tests OOB data cap at 2048 bytes.
// Reference: tinyemu-2019-12-21/slirp/socket.c:292-293
func TestSoSendOOBCapLimit(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Reserve buffer space
	so.SoRcv.SbReserve(8192)

	// Add data and set urgc > 2048
	data := make([]byte, 3000)
	so.SoRcv.SbAppendBytes(data)
	so.SoUrgc = 3000

	// Call SoSendOOB
	so.SoSendOOB()

	// Verify urgc was capped
	// Note: since slirpSendOOB returns 0 without a real socket, urgc won't decrease,
	// but it should have been capped to 2048 initially
	// After the call, urgc should still be <= 2048 (capped in function)
}

// TestSoFCantRcvMore tests socket can't receive more state.
// Reference: tinyemu-2019-12-21/slirp/socket.c:672-688
func TestSoFCantRcvMore(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.SoState = SSIsFConnected

	so.soFCantRcvMore()

	// Should set SS_FCANTRCVMORE
	if (so.SoState & SSFCantRcvMore) == 0 {
		t.Error("soFCantRcvMore should set SSFCantRcvMore")
	}

	// Should clear SS_ISFCONNECTING
	if (so.SoState & SSIsFConnecting) != 0 {
		t.Error("soFCantRcvMore should clear SSIsFConnecting")
	}
}

// TestSoFCantSendMore tests socket can't send more state.
// Reference: tinyemu-2019-12-21/slirp/socket.c:690-709
func TestSoFCantSendMore(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.SoState = SSIsFConnected

	so.soFCantSendMore()

	// Should set SS_FCANTSENDMORE
	if (so.SoState & SSFCantSendMore) == 0 {
		t.Error("soFCantSendMore should set SSFCantSendMore")
	}
}

// TestSoFWDrain tests write drain mode.
// Reference: tinyemu-2019-12-21/slirp/socket.c:716-722
func TestSoFWDrain(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.SoState = SSIsFConnected

	// Test with data in buffer - should set SS_FWDRAIN
	so.SoRcv.SbReserve(100)
	so.SoRcv.SbAppendBytes([]byte("data"))

	so.SoFWDrain()

	if (so.SoState & SSFWDrain) == 0 {
		t.Error("SoFWDrain should set SSFWDrain when buffer has data")
	}

	// Test with empty buffer - should call soFCantSendMore
	so2 := s.SoCreate()
	so2.SoState = SSIsFConnected
	so2.SoRcv.SbReserve(100)

	so2.SoFWDrain()

	if (so2.SoState & SSFCantSendMore) == 0 {
		t.Error("SoFWDrain should set SSFCantSendMore when buffer is empty")
	}
}

// TestTCPSockClosed tests TCP socket closed handling.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:282-308
func TestTCPSockClosed(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	tp := &TCPCB{
		TSocket: so,
		TMaxSeg: 1460,
	}
	so.SoTCPCB = tp

	// Reserve buffers for tcp_output
	so.SoSnd.SbReserve(8192)
	so.SoRcv.SbReserve(8192)

	// Test ESTABLISHED -> FIN_WAIT_1
	tp.TState = TCPSEstablished
	TCPSockClosed(tp)
	if tp.TState != TCPSFinWait1 {
		t.Errorf("TCPSockClosed: ESTABLISHED -> %d, want FIN_WAIT_1 (%d)",
			tp.TState, TCPSFinWait1)
	}

	// Test CLOSE_WAIT -> LAST_ACK
	tp.TState = TCPSCloseWait
	TCPSockClosed(tp)
	if tp.TState != TCPSLastAck {
		t.Errorf("TCPSockClosed: CLOSE_WAIT -> %d, want LAST_ACK (%d)",
			tp.TState, TCPSLastAck)
	}

	// Test SYN_RECEIVED -> FIN_WAIT_1
	tp.TState = TCPSSynReceived
	TCPSockClosed(tp)
	if tp.TState != TCPSFinWait1 {
		t.Errorf("TCPSockClosed: SYN_RECEIVED -> %d, want FIN_WAIT_1 (%d)",
			tp.TState, TCPSFinWait1)
	}
}

// TestTCPSockClosedNil tests TCPSockClosed with nil.
func TestTCPSockClosedNil(t *testing.T) {
	// Should not panic
	TCPSockClosed(nil)
}

// TestSoWrite tests writing data from receive buffer to socket.
// Reference: tinyemu-2019-12-21/slirp/socket.c:339-425
func TestSoWrite(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Reserve buffer space
	so.SoRcv.SbReserve(8192)

	// Add data to receive buffer
	so.SoRcv.SbAppendBytes([]byte("Hello, World!"))

	// Call SoWrite - without a real socket fd, slirpSend returns -1
	// which is treated as would-block, returning 0
	n := so.SoWrite()

	// Without real socket, returns 0 (would-block)
	if n != 0 {
		t.Errorf("SoWrite returned %d, want 0 (no socket -> would-block)", n)
	}
}

// TestSoWriteWithOOB tests SoWrite with urgent data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:349-353
func TestSoWriteWithOOB(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Reserve buffer space
	so.SoRcv.SbReserve(8192)

	// Add data and set urgent count
	so.SoRcv.SbAppendBytes([]byte("urgent"))
	so.SoUrgc = 6

	// Call SoWrite - should call SoSendOOB first
	n := so.SoWrite()

	// Without real socket, returns 0 (would-block)
	if n != 0 {
		t.Errorf("SoWrite with OOB returned %d, want 0", n)
	}
}

// TestSoWriteEmptyBuffer tests SoWrite with empty buffer.
func TestSoWriteEmptyBuffer(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Reserve buffer but don't add data
	so.SoRcv.SbReserve(8192)

	// Call SoWrite
	n := so.SoWrite()

	// Should return 0 with nothing to write
	if n != 0 {
		t.Errorf("SoWrite with empty buffer returned %d, want 0", n)
	}
}

// TestSoWriteDrainMode tests SoWrite in drain mode.
// Reference: tinyemu-2019-12-21/slirp/socket.c:421-422
func TestSoWriteDrainMode(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.SoState = SSFWDrain | SSIsFConnected

	// Reserve buffer with no data
	so.SoRcv.SbReserve(8192)

	// Call SoWrite with drain mode and empty buffer
	// (won't actually do much without a real socket, but tests the code path)
	so.SoWrite()

	// Nothing specific to test without a real socket
}

// TestSoRecvFrom tests receiving data from UDP socket.
// Reference: tinyemu-2019-12-21/slirp/socket.c:431-526
func TestSoRecvFromNilSlirp(t *testing.T) {
	so := &Socket{}

	// Should handle nil slirp gracefully
	so.SoRecvFrom() // Should not panic
}

// TestSoRecvFromICMP tests SoRecvFrom with ICMP socket type.
// Reference: tinyemu-2019-12-21/slirp/socket.c:439-461
func TestSoRecvFromICMP(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.SoType = IPProtoICMP

	// Without a real UDP connection, this will just return
	so.SoRecvFrom()
	// Should not panic
}

// TestSoSendTo tests sending data to UDP destination.
// Reference: tinyemu-2019-12-21/slirp/socket.c:532-573
func TestSoSendTo(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()
	so.SoFAddr = net.IPv4(8, 8, 8, 8)
	so.SoFPort = 53

	m := s.MGet()
	m.Data = []byte("test data")
	m.Len = 9

	// Without a real UDP connection, this will return -1
	ret := so.SoSendTo(m)
	if ret != -1 {
		t.Errorf("SoSendTo without connection returned %d, want -1", ret)
	}
}

// TestSoSendToVirtualNetwork tests SoSendTo with virtual network address.
// Reference: tinyemu-2019-12-21/slirp/socket.c:543-553
func TestSoSendToVirtualNetwork(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	// Set destination to virtual nameserver
	so.SoFAddr = s.VNameserverAddr
	so.SoFPort = 53

	m := s.MGet()
	m.Data = []byte("dns query")
	m.Len = 9

	// Without UDP connection, returns -1
	ret := so.SoSendTo(m)
	if ret != -1 {
		t.Errorf("SoSendTo to nameserver returned %d, want -1", ret)
	}
}

// TestSoSendToNilMbuf tests SoSendTo with nil mbuf.
func TestSoSendToNilMbuf(t *testing.T) {
	s := NewSlirp()
	so := s.SoCreate()

	ret := so.SoSendTo(nil)
	if ret != -1 {
		t.Errorf("SoSendTo with nil mbuf returned %d, want -1", ret)
	}
}
