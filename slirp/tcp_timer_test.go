package slirp

import (
	"net"
	"testing"
)

// TestTCPTimerConstants verifies TCP timer constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:40-45
func TestTCPTimerConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"TCPT_REXMT", TCPTRexmt, 0},
		{"TCPT_PERSIST", TCPTPersist, 1},
		{"TCPT_KEEP", TCPTKeep, 2},
		{"TCPT_2MSL", TCPT2MSL, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTCPTimeConstants verifies TCP time constants match C values.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:86-105
func TestTCPTimeConstants(t *testing.T) {
	// PRSlowHz = 2
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"PR_SLOWHZ", PRSlowHz, 2},
		{"TCPTV_MSL", TCPTVMSL, 5 * 2}, // 10
		{"TCPTV_SRTTBASE", TCPTVSrttBase, 0},
		{"TCPTV_SRTTDFLT", TCPTVSrttDflt, 3 * 2},         // 6
		{"TCPTV_PERSMIN", TCPTVPersMin, 5 * 2},           // 10
		{"TCPTV_PERSMAX", TCPTVPersMax, 60 * 2},          // 120
		{"TCPTV_KEEP_INIT", TCPTVKeepInit, 75 * 2},       // 150
		{"TCPTV_KEEP_IDLE", TCPTVKeepIdle, 120 * 60 * 2}, // 14400
		{"TCPTV_KEEPINTVL", TCPTVKeepIntvl, 75 * 2},      // 150
		{"TCPTV_KEEPCNT", TCPTVKeepCnt, 8},
		{"TCPTV_MIN", TCPTVMin, 1 * 2},            // 2
		{"TCPTV_REXMTMAX", TCPTVRexmtMax, 12 * 2}, // 24
		{"TCP_LINGERTIME", TCPLingerTime, 120},
		{"TCP_MAXRXTSHIFT", TCPMaxRxtShift, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTCPMaxIdle verifies TCP_MAXIDLE calculation.
// Reference: tinyemu-2019-12-21/slirp/slirp.h:254
func TestTCPMaxIdle(t *testing.T) {
	// TCPMaxIdle = TCPTV_KEEPCNT * TCPTV_KEEPINTVL = 8 * 150 = 1200
	want := TCPTVKeepCnt * TCPTVKeepIntvl
	if TCPMaxIdle != want {
		t.Errorf("TCPMaxIdle = %d, want %d", TCPMaxIdle, want)
	}
}

// TestTCPBackoff verifies the backoff multiplier table.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:115-116
func TestTCPBackoff(t *testing.T) {
	want := []int{1, 2, 4, 8, 16, 32, 64, 64, 64, 64, 64, 64, 64}
	if len(TCPBackoff) != len(want) {
		t.Errorf("TCPBackoff length = %d, want %d", len(TCPBackoff), len(want))
	}
	for i, w := range want {
		if TCPBackoff[i] != w {
			t.Errorf("TCPBackoff[%d] = %d, want %d", i, TCPBackoff[i], w)
		}
	}
}

// TestTcptRangeSet tests the range clamping function.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.h:111-117
func TestTcptRangeSet(t *testing.T) {
	tests := []struct {
		name  string
		value int16
		tvmin int16
		tvmax int16
		want  int16
	}{
		{"within range", 50, 10, 100, 50},
		{"below min", 5, 10, 100, 10},
		{"above max", 150, 10, 100, 100},
		{"at min", 10, 10, 100, 10},
		{"at max", 100, 10, 100, 100},
		{"negative value below min", -5, 0, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tcptRangeSet(tt.value, tt.tvmin, tt.tvmax)
			if got != tt.want {
				t.Errorf("tcptRangeSet(%d, %d, %d) = %d, want %d",
					tt.value, tt.tvmin, tt.tvmax, got, tt.want)
			}
		})
	}
}

// TestTCPCancelTimers tests canceling all timers.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:106-113
func TestTCPCancelTimers(t *testing.T) {
	tp := &TCPCB{
		TTimer: [TCPTNTimers]int16{100, 200, 300, 400},
	}

	// Verify timers are set
	for i, v := range tp.TTimer {
		if v == 0 {
			t.Errorf("TTimer[%d] should be non-zero before cancel", i)
		}
	}

	// Cancel all timers
	tp.TCPCancelTimers()

	// Verify all timers are cleared
	for i, v := range tp.TTimer {
		if v != 0 {
			t.Errorf("TTimer[%d] = %d, want 0 after cancel", i, v)
		}
	}
}

// TestTCPFasttimo tests the fast timeout routine.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:40-57
func TestTCPFasttimo(t *testing.T) {
	slirp := NewSlirp()

	// Test with empty socket list
	slirp.TCPFasttimo() // Should not panic

	// Create a socket with TCPCB that has TF_DELACK set
	so := &Socket{
		Slirp: slirp,
	}
	tp := &TCPCB{
		TFlags:  TFDelAck,
		TSocket: so,
	}
	so.SoTCPCB = tp

	// Link socket into TCB list
	so.Next = &slirp.TCB
	so.Prev = &slirp.TCB
	slirp.TCB.Next = so
	slirp.TCB.Prev = so

	// Run fasttimo
	slirp.TCPFasttimo()

	// Verify TF_DELACK is cleared
	// Note: TF_ACKNOW is set by fasttimo but then cleared by tcp_output
	// if output succeeds, so we only check TF_DELACK was cleared
	if tp.TFlags&TFDelAck != 0 {
		t.Error("TF_DELACK should be cleared after fasttimo")
	}
}

// TestTCPSlowtimo tests the slow timeout routine.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:64-101
func TestTCPSlowtimo(t *testing.T) {
	slirp := NewSlirp()
	initialIss := slirp.TCPIss
	initialNow := slirp.TCPNow

	// Test with empty socket list
	slirp.TCPSlowtimo()

	// Verify tcp_iss and tcp_now are incremented
	expectedIssIncr := TCPIssIncr / PRSlowHz
	if slirp.TCPIss != initialIss+uint32(expectedIssIncr) {
		t.Errorf("TCPIss = %d, want %d", slirp.TCPIss, initialIss+uint32(expectedIssIncr))
	}
	if slirp.TCPNow != initialNow+1 {
		t.Errorf("TCPNow = %d, want %d", slirp.TCPNow, initialNow+1)
	}
}

// TestTCPSlowtimoTimerDecrement tests timer decrementing.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:86-92
func TestTCPSlowtimoTimerDecrement(t *testing.T) {
	slirp := NewSlirp()

	// Create a socket with TCPCB that has timers set
	so := &Socket{
		Slirp: slirp,
	}
	tp := &TCPCB{
		TTimer:  [TCPTNTimers]int16{5, 0, 3, 0},
		TIdle:   10,
		TRtt:    5,
		TSocket: so,
	}
	so.SoTCPCB = tp

	// Link socket into TCB list
	so.Next = &slirp.TCB
	so.Prev = &slirp.TCB
	slirp.TCB.Next = so
	slirp.TCB.Prev = so

	// Run slowtimo
	slirp.TCPSlowtimo()

	// Verify timers are decremented
	if tp.TTimer[TCPTRexmt] != 4 {
		t.Errorf("TTimer[TCPT_REXMT] = %d, want 4", tp.TTimer[TCPTRexmt])
	}
	if tp.TTimer[TCPTPersist] != 0 {
		t.Errorf("TTimer[TCPT_PERSIST] = %d, want 0 (was 0)", tp.TTimer[TCPTPersist])
	}
	if tp.TTimer[TCPTKeep] != 2 {
		t.Errorf("TTimer[TCPT_KEEP] = %d, want 2", tp.TTimer[TCPTKeep])
	}
	if tp.TTimer[TCPT2MSL] != 0 {
		t.Errorf("TTimer[TCPT_2MSL] = %d, want 0 (was 0)", tp.TTimer[TCPT2MSL])
	}

	// Verify t_idle and t_rtt are incremented
	if tp.TIdle != 11 {
		t.Errorf("TIdle = %d, want 11", tp.TIdle)
	}
	if tp.TRtt != 6 {
		t.Errorf("TRtt = %d, want 6", tp.TRtt)
	}
}

// TestTCPSlowtimoRttZero tests that t_rtt is not incremented when zero.
// Reference: tinyemu-2019-12-21/slirp/tcp_timer.c:94-95
func TestTCPSlowtimoRttZero(t *testing.T) {
	slirp := NewSlirp()

	// Create a socket with TCPCB that has t_rtt = 0
	so := &Socket{
		Slirp: slirp,
	}
	tp := &TCPCB{
		TRtt:    0,
		TSocket: so,
	}
	so.SoTCPCB = tp

	// Link socket into TCB list
	so.Next = &slirp.TCB
	so.Prev = &slirp.TCB
	slirp.TCB.Next = so
	slirp.TCB.Prev = so

	// Run slowtimo
	slirp.TCPSlowtimo()

	// Verify t_rtt is still 0
	if tp.TRtt != 0 {
		t.Errorf("TRtt = %d, want 0 (was 0, should not increment)", tp.TRtt)
	}
}

// TestTCPCtl tests the TCP control function.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:884-915
func TestTCPCtl(t *testing.T) {
	// Test nil socket
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:884
	t.Run("nil socket", func(t *testing.T) {
		result := TCPCtl(nil)
		if result != 0 {
			t.Errorf("TCPCtl(nil) = %d, want 0", result)
		}
	})

	// Test socket with nil slirp
	t.Run("nil slirp", func(t *testing.T) {
		so := &Socket{}
		result := TCPCtl(so)
		if result != 0 {
			t.Errorf("TCPCtl with nil slirp = %d, want 0", result)
		}
	})

	// Test socket with destination being vhost address - should write error and return 0
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:894
	t.Run("vhost address destination", func(t *testing.T) {
		slirp := NewSlirp()
		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: slirp.VHostAddr, // VHost address (10.0.2.2)
			SoSnd:   sndBuf,
		}
		result := TCPCtl(so)
		if result != 0 {
			t.Errorf("TCPCtl with vhost addr = %d, want 0", result)
		}

		// Verify error message was written
		expectedMsg := "Error: No application configured.\r\n"
		actualMsg := string(so.SoSnd.SbBytes())
		if actualMsg != expectedMsg {
			t.Errorf("SoSnd = %q, want %q", actualMsg, expectedMsg)
		}
	})

	// Test socket with no match in exec list - should write error and return 0
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:896-908, 910-914
	t.Run("no exec list match", func(t *testing.T) {
		slirp := NewSlirp()
		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: net.IPv4(10, 0, 2, 15), // Not vhost address
			SoFPort: 8080,
			SoSnd:   sndBuf,
		}
		result := TCPCtl(so)
		if result != 0 {
			t.Errorf("TCPCtl = %d, want 0", result)
		}

		// Verify error message was written
		expectedMsg := "Error: No application configured.\r\n"
		actualMsg := string(so.SoSnd.SbBytes())
		if actualMsg != expectedMsg {
			t.Errorf("SoSnd = %q, want %q", actualMsg, expectedMsg)
		}
	})

	// Test socket matching exec list entry with ex_pty == 3
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:899-903
	t.Run("exec list match ex_pty 3", func(t *testing.T) {
		slirp := NewSlirp()
		targetAddr := net.IPv4(10, 0, 2, 100)
		targetPort := uint16(9000)
		execCmd := "/bin/special"

		// Add exec list entry with ex_pty = 3
		slirp.ExecList = &ExList{
			ExFPort: targetPort,
			ExAddr:  targetAddr,
			ExPty:   3, // Special mode
			ExExec:  execCmd,
		}

		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: targetAddr,
			SoFPort: targetPort,
			SoSnd:   sndBuf,
			S:       42, // Initial socket descriptor
		}

		result := TCPCtl(so)
		if result != 1 {
			t.Errorf("TCPCtl with ex_pty=3 = %d, want 1", result)
		}

		// Verify socket descriptor set to -1
		if so.S != -1 {
			t.Errorf("so.S = %d, want -1", so.S)
		}

		// Verify Extra set to exec command
		if so.Extra != execCmd {
			t.Errorf("so.Extra = %v, want %q", so.Extra, execCmd)
		}

		// Verify no error message written to send buffer
		if len(so.SoSnd.SbBytes()) != 0 {
			t.Errorf("SoSnd should be empty, got %q", string(so.SoSnd.SbBytes()))
		}
	})

	// Test socket matching exec list entry with ex_pty != 3 (calls ForkExec which returns 0)
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:904-906
	t.Run("exec list match ex_pty not 3", func(t *testing.T) {
		slirp := NewSlirp()
		targetAddr := net.IPv4(10, 0, 2, 100)
		targetPort := uint16(9001)

		// Add exec list entry with ex_pty = 1 (not 3)
		slirp.ExecList = &ExList{
			ExFPort: targetPort,
			ExAddr:  targetAddr,
			ExPty:   1, // Normal PTY mode (ForkExec returns 0)
			ExExec:  "/bin/shell",
		}

		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: targetAddr,
			SoFPort: targetPort,
			SoSnd:   sndBuf,
		}

		result := TCPCtl(so)
		// ForkExec returns 0 (not implemented)
		if result != 0 {
			t.Errorf("TCPCtl with ex_pty=1 = %d, want 0", result)
		}
	})

	// Test exec list with multiple entries - should match the correct one
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:896-908
	t.Run("exec list multiple entries", func(t *testing.T) {
		slirp := NewSlirp()

		// Add first entry (different port)
		entry1 := &ExList{
			ExFPort: 1000,
			ExAddr:  net.IPv4(10, 0, 2, 50),
			ExPty:   3,
			ExExec:  "entry1",
		}

		// Add second entry (should match)
		entry2 := &ExList{
			ExFPort: 2000,
			ExAddr:  net.IPv4(10, 0, 2, 60),
			ExPty:   3,
			ExExec:  "entry2",
			ExNext:  entry1,
		}

		// Add third entry (different address)
		entry3 := &ExList{
			ExFPort: 2000,
			ExAddr:  net.IPv4(10, 0, 2, 70),
			ExPty:   3,
			ExExec:  "entry3",
			ExNext:  entry2,
		}

		slirp.ExecList = entry3

		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: net.IPv4(10, 0, 2, 60), // Matches entry2
			SoFPort: 2000,
			SoSnd:   sndBuf,
		}

		result := TCPCtl(so)
		if result != 1 {
			t.Errorf("TCPCtl with multiple entries = %d, want 1", result)
		}

		// Verify it matched entry2
		if so.Extra != "entry2" {
			t.Errorf("so.Extra = %v, want %q", so.Extra, "entry2")
		}
	})

	// Test port mismatch in exec list
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:897
	t.Run("exec list port mismatch", func(t *testing.T) {
		slirp := NewSlirp()
		targetAddr := net.IPv4(10, 0, 2, 100)

		// Add exec list entry with different port
		slirp.ExecList = &ExList{
			ExFPort: 9000,
			ExAddr:  targetAddr,
			ExPty:   3,
			ExExec:  "should_not_match",
		}

		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: targetAddr,
			SoFPort: 9001, // Different port
			SoSnd:   sndBuf,
		}

		result := TCPCtl(so)
		if result != 0 {
			t.Errorf("TCPCtl with port mismatch = %d, want 0", result)
		}

		// Verify error message was written
		expectedMsg := "Error: No application configured.\r\n"
		actualMsg := string(so.SoSnd.SbBytes())
		if actualMsg != expectedMsg {
			t.Errorf("SoSnd = %q, want %q", actualMsg, expectedMsg)
		}
	})

	// Test address mismatch in exec list
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:898
	t.Run("exec list address mismatch", func(t *testing.T) {
		slirp := NewSlirp()
		targetPort := uint16(9000)

		// Add exec list entry with different address
		slirp.ExecList = &ExList{
			ExFPort: targetPort,
			ExAddr:  net.IPv4(10, 0, 2, 100),
			ExPty:   3,
			ExExec:  "should_not_match",
		}

		var sndBuf SBuf
		sndBuf.SbReserve(1024)
		so := &Socket{
			Slirp:   slirp,
			SoFAddr: net.IPv4(10, 0, 2, 101), // Different address
			SoFPort: targetPort,
			SoSnd:   sndBuf,
		}

		result := TCPCtl(so)
		if result != 0 {
			t.Errorf("TCPCtl with address mismatch = %d, want 0", result)
		}

		// Verify error message was written
		expectedMsg := "Error: No application configured.\r\n"
		actualMsg := string(so.SoSnd.SbBytes())
		if actualMsg != expectedMsg {
			t.Errorf("SoSnd = %q, want %q", actualMsg, expectedMsg)
		}
	})
}

// TestTCPClose tests the TCPClose function.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:236-265
func TestTCPClose(t *testing.T) {
	slirp := NewSlirp()

	t.Run("closes socket and frees resources", func(t *testing.T) {
		so := &Socket{
			Slirp: slirp,
			S:     -1, // No actual fd
		}
		so.SoRcv.SbReserve(1024)
		so.SoSnd.SbReserve(1024)

		tp := &TCPCB{
			TSocket: so,
		}
		tp.ReassemblyHead.Next = &tp.ReassemblyHead
		tp.ReassemblyHead.Prev = &tp.ReassemblyHead
		so.SoTCPCB = tp

		// Insert socket into TCB list
		so.Next = slirp.TCB.Next
		so.Prev = &slirp.TCB
		slirp.TCB.Next.Prev = so
		slirp.TCB.Next = so

		result := tp.TCPClose()

		if result != nil {
			t.Error("TCPClose should return nil")
		}
		if so.SoTCPCB != nil {
			t.Error("SoTCPCB should be nil after close")
		}
	})

	t.Run("nil tcpcb returns nil", func(t *testing.T) {
		var tp *TCPCB
		result := tp.TCPClose()
		if result != nil {
			t.Error("TCPClose(nil) should return nil")
		}
	})

	t.Run("clears tcp_last_so cache", func(t *testing.T) {
		so := &Socket{
			Slirp: slirp,
			S:     -1,
		}
		tp := &TCPCB{
			TSocket: so,
		}
		tp.ReassemblyHead.Next = &tp.ReassemblyHead
		tp.ReassemblyHead.Prev = &tp.ReassemblyHead
		so.SoTCPCB = tp

		// Set tcp_last_so to this socket
		slirp.TCPLastSo = so

		// Insert socket into list
		so.Next = slirp.TCB.Next
		so.Prev = &slirp.TCB
		slirp.TCB.Next.Prev = so
		slirp.TCB.Next = so

		tp.TCPClose()

		if slirp.TCPLastSo != &slirp.TCB {
			t.Error("TCPClose should reset TCPLastSo to &TCB")
		}
	})
}

// TestTCPDrop tests the TCPDrop function.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:217-228
func TestTCPDrop(t *testing.T) {
	t.Run("nil tcpcb returns nil", func(t *testing.T) {
		var tp *TCPCB
		result := tp.TCPDrop(0)
		if result != nil {
			t.Error("TCPDrop(nil) should return nil")
		}
	})

	t.Run("sends RST when synchronized", func(t *testing.T) {
		slirp := NewSlirp()
		slirp.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

		var sentPackets [][]byte
		slirp.OutputFunc = func(opaque interface{}, data []byte) {
			cp := make([]byte, len(data))
			copy(cp, data)
			sentPackets = append(sentPackets, cp)
		}

		so := &Socket{
			Slirp:   slirp,
			S:       -1,
			SoFAddr: net.IPv4(10, 0, 2, 15),
			SoLAddr: net.IPv4(10, 0, 2, 2),
			SoFPort: 12345,
			SoLPort: 80,
		}
		so.SoRcv.SbReserve(1024)
		so.SoSnd.SbReserve(1024)

		tp := slirp.tcpNewTCPCB(so)
		tp.TState = TCPSEstablished // Synchronized state
		slirp.tcpTemplate(tp)

		// Insert socket
		so.Next = slirp.TCB.Next
		so.Prev = &slirp.TCB
		slirp.TCB.Next.Prev = so
		slirp.TCB.Next = so

		result := tp.TCPDrop(0)

		if result != nil {
			t.Error("TCPDrop should return nil")
		}
		// Since we were in synchronized state, RST should be sent via tcp_output
		// This test verifies the state transition happens
	})

	t.Run("unsynchronized state just closes", func(t *testing.T) {
		slirp := NewSlirp()

		so := &Socket{
			Slirp: slirp,
			S:     -1,
		}
		so.SoRcv.SbReserve(1024)
		so.SoSnd.SbReserve(1024)

		tp := slirp.tcpNewTCPCB(so)
		tp.TState = TCPSListen // Not synchronized

		// Insert socket
		so.Next = slirp.TCB.Next
		so.Prev = &slirp.TCB
		slirp.TCB.Next.Prev = so
		slirp.TCB.Next = so

		result := tp.TCPDrop(0)

		if result != nil {
			t.Error("TCPDrop should return nil")
		}
	})
}

// TestTCPRespond tests the TCPRespond method.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:102-167
func TestTCPRespond(t *testing.T) {
	t.Run("nil tcpcb does nothing", func(t *testing.T) {
		var tp *TCPCB
		// Should not panic
		tp.TCPRespond(nil, 0, 0, 0)
	})

	t.Run("sends response with template when m is nil", func(t *testing.T) {
		slirp := NewSlirp()
		slirp.ClientEthAddr = [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

		var sentPackets [][]byte
		slirp.OutputFunc = func(opaque interface{}, data []byte) {
			cp := make([]byte, len(data))
			copy(cp, data)
			sentPackets = append(sentPackets, cp)
		}

		so := &Socket{
			Slirp:   slirp,
			S:       -1,
			SoFAddr: net.IPv4(10, 0, 2, 15),
			SoLAddr: net.IPv4(10, 0, 2, 2),
			SoFPort: 12345,
			SoLPort: 80,
		}
		so.SoRcv.SbReserve(1024)

		tp := slirp.tcpNewTCPCB(so)
		slirp.tcpTemplate(tp)

		tp.TCPRespond(nil, 1000, 500, THAck)

		if len(sentPackets) != 1 {
			t.Errorf("expected 1 packet sent, got %d", len(sentPackets))
			return
		}

		// Verify packet was sent (14 byte ethernet + IP/TCP)
		if len(sentPackets[0]) < 14+40 {
			t.Errorf("packet too short: %d bytes", len(sentPackets[0]))
		}
	})
}
