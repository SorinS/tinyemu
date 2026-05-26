package slirp

import (
	"net"
	"syscall"
	"time"
)

// Socket state bits.
// Reference: tinyemu-2019-12-21/slirp/socket.h:61-75
const (
	SSNoFDRef        = 0x001 // No fd reference
	SSIsFConnecting  = 0x002 // Socket is connecting to peer
	SSIsFConnected   = 0x004 // Socket is connected to peer
	SSFCantRcvMore   = 0x008 // Socket can't receive more from peer
	SSFCantSendMore  = 0x010 // Socket can't send more to peer
	SSFWDrain        = 0x040 // We received a FIN, drain data
	SSCTL            = 0x080
	SSFAcceptConn    = 0x100  // Socket is accepting connections
	SSFAcceptOnce    = 0x200  // Socket will die after one accept
	SSPersistentMask = 0xf000 // Unremovable state bits
	SSHostFwd        = 0x1000 // Socket describes host->guest forwarding
	SSIncoming       = 0x2000 // Connection was initiated by host on internet

	// Socket expiration times
	SOExpire     = 240000
	SOExpireFast = 10000
)

// Protocol emulation types.
// Reference: tinyemu-2019-12-21/slirp/misc.h:25-37
const (
	EmuNone      = 0x0
	EmuCTL       = 0x1
	EmuFTP       = 0x2
	EmuKSH       = 0x3
	EmuIRC       = 0x4
	EmuRealAudio = 0x5
	EmuRLogin    = 0x6
	EmuIdent     = 0x7
	EmuRSH       = 0x8
	EmuNoConnect = 0x10 // Don't connect
)

// Socket represents a network socket.
// Reference: tinyemu-2019-12-21/slirp/socket.h:18-54
type Socket struct {
	Next *Socket // next socket in list
	Prev *Socket // previous socket in list

	S int // the actual socket fd

	Slirp *Slirp // managing slirp instance

	SoM  *Mbuf  // pointer to the original SYN packet or PING reply
	SoTi []byte // pointer to original ti (for non-blocking connections)

	SoUrgc int // urgent data count

	SoFAddr net.IP // foreign host address
	SoLAddr net.IP // local host address
	SoFPort uint16 // foreign port
	SoLPort uint16 // local port

	SoIPTos uint8 // type of service
	SoEmu   uint8 // is the socket emulated?
	SoType  uint8 // type of socket (TCP or UDP)
	SoState int   // internal state flags

	SoTCPCB  interface{} // pointer to TCP protocol control block (tcpcb)
	SoExpire uint32      // when the socket will expire

	SoQueued  int // number of packets queued from this socket
	SoNQueued int // number of packets queued in a row

	// Send/receive buffers
	// Reference: tinyemu-2019-12-21/slirp/socket.h:51-52
	SoRcv SBuf
	SoSnd SBuf

	Extra interface{} // extra pointer
}

// SoLookup looks up a socket in a list by local and foreign address/port.
// Reference: tinyemu-2019-12-21/slirp/socket.c:15-32
func SoLookup(head *Socket, laddr net.IP, lport uint16, faddr net.IP, fport uint16) *Socket {
	for so := head.Next; so != head; so = so.Next {
		if so.SoLPort == lport &&
			so.SoLAddr.Equal(laddr) &&
			so.SoFAddr.Equal(faddr) &&
			so.SoFPort == fport {
			return so
		}
	}
	return nil
}

// SoCreate creates a new socket.
// Reference: tinyemu-2019-12-21/slirp/socket.c:40-52
func (s *Slirp) SoCreate() *Socket {
	so := &Socket{
		SoState: SSNoFDRef,
		S:       -1,
		Slirp:   s,
	}
	return so
}

// SoFree frees a socket and removes it from any queues.
// Reference: tinyemu-2019-12-21/slirp/socket.c:58-77
func (so *Socket) SoFree() {
	if so == nil {
		return
	}
	slirp := so.Slirp

	// Trace socket destruction
	if slirp != nil {
		proto := "TCP"
		if so.SoType == IPProtoUDP {
			proto = "UDP"
		} else if so.SoType == IPProtoICMP {
			proto = "ICMP"
		}
		slirp.traceSocketDestroy(so, proto)
	}

	// If this is an RSH socket with extra, free the extra socket too
	// Reference: tinyemu-2019-12-21/slirp/socket.c:62-65
	if so.SoEmu == EmuRSH && so.Extra != nil {
		if extraSo, ok := so.Extra.(*Socket); ok {
			extraSo.SoFree()
		}
		so.Extra = nil
	}

	// Clear cached last_so pointers
	// Reference: tinyemu-2019-12-21/slirp/socket.c:66-70
	if slirp != nil {
		if so == slirp.TCPLastSo {
			slirp.TCPLastSo = &slirp.TCB
		} else if so == slirp.UDPLastSo {
			slirp.UDPLastSo = &slirp.UDB
		}
	}

	// Free the mbuf if any
	// Reference: tinyemu-2019-12-21/slirp/socket.c:71
	if so.SoM != nil {
		so.SoM.MFree()
		so.SoM = nil
	}

	// Remove from queue if linked
	// Reference: tinyemu-2019-12-21/slirp/socket.c:73-74
	if so.Next != nil && so.Prev != nil {
		so.Prev.Next = so.Next
		so.Next.Prev = so.Prev
	}
	// In Go, GC handles the actual freeing
}

// SoPrepRbuf prepares the iovec for reading into the socket send buffer.
// Returns the total number of bytes that can be read.
// Reference: tinyemu-2019-12-21/slirp/socket.c:79-136
func (so *Socket) SoPrepRbuf() (iov [2][]byte, n int, total int) {
	sb := &so.SoSnd
	freeLen := sb.SbDataLen - sb.SbCC
	tp := SoToTCPCB(so)
	mss := 1460 // default MSS
	if tp != nil && tp.TMaxSeg > 0 {
		mss = int(tp.TMaxSeg)
	}

	if freeLen <= 0 {
		return iov, 0, 0
	}

	// Initialize iov[1]
	iov[1] = nil

	if sb.SbWPtr < sb.SbRPtr {
		// Write pointer is behind read pointer - single region
		iov[0] = sb.SbData[sb.SbWPtr:sb.SbRPtr]
		if len(iov[0]) > freeLen {
			iov[0] = iov[0][:freeLen]
		}
		if len(iov[0]) > mss {
			iov[0] = iov[0][:len(iov[0])-(len(iov[0])%mss)]
		}
		n = 1
	} else {
		// Write pointer is at or ahead of read pointer - may be two regions
		iov[0] = sb.SbData[sb.SbWPtr:]
		if len(iov[0]) > freeLen {
			iov[0] = iov[0][:freeLen]
		}
		remainingLen := freeLen - len(iov[0])
		if remainingLen > 0 {
			iov[1] = sb.SbData[:sb.SbRPtr]
			if len(iov[1]) > remainingLen {
				iov[1] = iov[1][:remainingLen]
			}
			totalLen := len(iov[0]) + len(iov[1])
			if totalLen > mss {
				lss := totalLen % mss
				if len(iov[1]) > lss {
					iov[1] = iov[1][:len(iov[1])-lss]
					n = 2
				} else {
					lss -= len(iov[1])
					iov[0] = iov[0][:len(iov[0])-lss]
					n = 1
				}
			} else {
				n = 2
			}
		} else {
			if len(iov[0]) > mss {
				iov[0] = iov[0][:len(iov[0])-(len(iov[0])%mss)]
			}
			n = 1
		}
	}

	total = len(iov[0])
	if n == 2 && iov[1] != nil {
		total += len(iov[1])
	}
	return iov, n, total
}

// SoRead reads from the socket fd into the send buffer.
// Returns bytes read, or -1 on disconnect, 0 on would-block.
// Reference: tinyemu-2019-12-21/slirp/socket.c:144-202
func (so *Socket) SoRead() int {
	sb := &so.SoSnd
	iov, nv, _ := so.SoPrepRbuf()

	if nv == 0 {
		return 0
	}

	// Try to read using system call
	var nn int
	var err error

	// In Go we don't have readv, so we read into the first buffer
	// and then the second if needed (like the !HAVE_READV path in C)
	if so.S >= 0 {
		nn, err = sockRead(so.S, iov[0])
	} else {
		nn = -1
		err = syscall.EBADF
	}

	if nn <= 0 {
		if err == syscall.EINTR || err == syscall.EAGAIN {
			return 0
		}
		so.soFCantRcvMore()
		TCPSockClosed(SoToTCPCB(so))
		return -1
	}

	// If there's a second buffer and we read all of the first, try reading more
	// Reference: tinyemu-2019-12-21/slirp/socket.c:186-191
	if nv == 2 && nn == len(iov[0]) && iov[1] != nil {
		ret, _ := sockRead(so.S, iov[1])
		if ret > 0 {
			nn += ret
		}
	}

	// Update buffer fields
	// Reference: tinyemu-2019-12-21/slirp/socket.c:196-200
	sb.SbCC += nn
	sb.SbWPtr += nn
	if sb.SbWPtr >= sb.SbDataLen {
		sb.SbWPtr -= sb.SbDataLen
	}
	return nn
}

// SoReadBuf reads data from a buffer into the socket send buffer.
// Returns bytes read, or -1 on error.
// Reference: tinyemu-2019-12-21/slirp/socket.c:204-244
func (so *Socket) SoReadBuf(buf []byte) int {
	sb := &so.SoSnd
	size := len(buf)
	iov, nv, total := so.SoPrepRbuf()

	if total < size {
		so.soFCantRcvMore()
		TCPSockClosed(SoToTCPCB(so))
		return -1
	}

	copy := size
	nn := copy
	if nn > len(iov[0]) {
		nn = len(iov[0])
	}
	for i := 0; i < nn; i++ {
		iov[0][i] = buf[i]
	}

	copy -= nn
	if copy > 0 && nv == 2 && iov[1] != nil {
		for i := 0; i < copy; i++ {
			iov[1][i] = buf[nn+i]
		}
	}

	// Update buffer fields
	// Reference: tinyemu-2019-12-21/slirp/socket.c:232-236
	sb.SbCC += size
	sb.SbWPtr += size
	if sb.SbWPtr >= sb.SbDataLen {
		sb.SbWPtr -= sb.SbDataLen
	}
	return size
}

// SoRecvOOB receives out-of-band (urgent) data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:254-274
func (so *Socket) SoRecvOOB() {
	tp := SoToTCPCB(so)
	if tp == nil {
		return
	}

	// Read the urgent data
	so.SoRead()

	// Set the urgent pointer and force output
	// Reference: tinyemu-2019-12-21/slirp/socket.c:270-273
	tp.SndUp = tp.SndUna + uint32(so.SoSnd.SbCC)
	tp.TForce = 1
	tp.TCPOutput()
	tp.TForce = 0
}

// SoSendOOB sends urgent out-of-band data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:281-332
func (so *Socket) SoSendOOB() int {
	sb := &so.SoRcv
	var buff [2048]byte

	if so.SoUrgc > 2048 {
		so.SoUrgc = 2048
	}

	var n int

	if sb.SbRPtr < sb.SbWPtr {
		// We can send it directly
		// Reference: tinyemu-2019-12-21/slirp/socket.c:295-298
		sendLen := so.SoUrgc
		if sendLen > sb.SbCC {
			sendLen = sb.SbCC
		}
		data := sb.SbData[sb.SbRPtr : sb.SbRPtr+sendLen]
		n = so.Slirp.slirpSendOOB(so, data)
		so.SoUrgc -= n
	} else {
		// Must copy to linear buffer
		// Reference: tinyemu-2019-12-21/slirp/socket.c:306-317
		length := sb.SbDataLen - sb.SbRPtr
		if length > so.SoUrgc {
			length = so.SoUrgc
		}
		copy(buff[:length], sb.SbData[sb.SbRPtr:sb.SbRPtr+length])
		so.SoUrgc -= length
		if so.SoUrgc > 0 {
			nn := sb.SbWPtr
			if nn > so.SoUrgc {
				nn = so.SoUrgc
			}
			copy(buff[length:length+nn], sb.SbData[:nn])
			so.SoUrgc -= nn
			length += nn
		}
		n = so.Slirp.slirpSendOOB(so, buff[:length])
	}

	// Update buffer
	// Reference: tinyemu-2019-12-21/slirp/socket.c:326-329
	sb.SbCC -= n
	sb.SbRPtr += n
	if sb.SbRPtr >= sb.SbDataLen {
		sb.SbRPtr -= sb.SbDataLen
	}

	return n
}

// soFCantRcvMore is called when the socket can't receive more data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:672-688
func (so *Socket) soFCantRcvMore() {
	if (so.SoState & SSNoFDRef) == 0 {
		if so.S >= 0 {
			sockShutdownRead(so.S)
		}
	}
	so.SoState &^= SSIsFConnecting
	if (so.SoState & SSFCantSendMore) != 0 {
		so.SoState &= SSPersistentMask
		so.SoState |= SSNoFDRef // Don't select it
	} else {
		so.SoState |= SSFCantRcvMore
	}
}

// soFCantSendMore is called when the socket can't send more data.
// Reference: tinyemu-2019-12-21/slirp/socket.c:690-709
func (so *Socket) soFCantSendMore() {
	if (so.SoState & SSNoFDRef) == 0 {
		if so.S >= 0 {
			sockShutdownWrite(so.S)
		}
	}
	so.SoState &^= SSIsFConnecting
	if (so.SoState & SSFCantRcvMore) != 0 {
		so.SoState &= SSPersistentMask
		so.SoState |= SSNoFDRef // Don't select it
	} else {
		so.SoState |= SSFCantSendMore
	}
}

// SoFWDrain sets write drain mode.
// Reference: tinyemu-2019-12-21/slirp/socket.c:716-722
func (so *Socket) SoFWDrain() {
	if so.SoRcv.SbCC > 0 {
		so.SoState |= SSFWDrain
	} else {
		so.soFCantSendMore()
	}
}

// SoWrite writes data from so_rcv to the socket.
// Returns bytes written, -1 on error, 0 on would-block.
// Reference: tinyemu-2019-12-21/slirp/socket.c:339-425
func (so *Socket) SoWrite() int {
	sb := &so.SoRcv
	dataLen := sb.SbCC

	// Send OOB data first if any
	// Reference: tinyemu-2019-12-21/slirp/socket.c:349-353
	if so.SoUrgc != 0 {
		so.SoSendOOB()
		if sb.SbCC == 0 {
			return 0
		}
	}

	// Prepare iovec for writing
	// Reference: tinyemu-2019-12-21/slirp/socket.c:360-379
	var iov [2][]byte
	var n int

	if sb.SbRPtr < sb.SbWPtr {
		iov[0] = sb.SbData[sb.SbRPtr:sb.SbWPtr]
		if len(iov[0]) > dataLen {
			iov[0] = iov[0][:dataLen]
		}
		n = 1
	} else {
		iov[0] = sb.SbData[sb.SbRPtr:]
		if len(iov[0]) > dataLen {
			iov[0] = iov[0][:dataLen]
		}
		dataLen -= len(iov[0])
		if dataLen > 0 {
			iov[1] = sb.SbData[:sb.SbWPtr]
			if len(iov[1]) > dataLen {
				iov[1] = iov[1][:dataLen]
			}
			n = 2
		} else {
			n = 1
		}
	}

	// Send the data (using slirp_send like !HAVE_READV path)
	// Reference: tinyemu-2019-12-21/slirp/socket.c:387
	nn := so.Slirp.slirpSend(so, iov[0], 0)

	// Handle would-block
	// Reference: tinyemu-2019-12-21/slirp/socket.c:389-391
	if nn < 0 {
		// slirpSend returns -1 on error (including no socket)
		// Treat as would-block for now
		return 0
	}

	if nn <= 0 {
		so.soFCantSendMore()
		TCPSockClosed(SoToTCPCB(so))
		return -1
	}

	// If there's a second buffer and we sent all of the first, try sending more
	// Reference: tinyemu-2019-12-21/slirp/socket.c:401-407
	if n == 2 && nn == len(iov[0]) && iov[1] != nil {
		ret := so.Slirp.slirpSend(so, iov[1], 0)
		if ret > 0 {
			nn += ret
		}
	}

	// Update buffer
	// Reference: tinyemu-2019-12-21/slirp/socket.c:411-415
	sb.SbCC -= nn
	sb.SbRPtr += nn
	if sb.SbRPtr >= sb.SbDataLen {
		sb.SbRPtr -= sb.SbDataLen
	}

	// If in DRAIN mode and no more data, set CANTSENDMORE
	// Reference: tinyemu-2019-12-21/slirp/socket.c:421-422
	if (so.SoState&SSFWDrain) != 0 && sb.SbCC == 0 {
		so.soFCantSendMore()
	}

	return nn
}

// SoRecvFrom receives data from a UDP socket.
// Reference: tinyemu-2019-12-21/slirp/socket.c:431-526
func (so *Socket) SoRecvFrom() {
	slirp := so.Slirp
	if slirp == nil {
		return
	}

	// ICMP ping reply handling
	// Reference: tinyemu-2019-12-21/slirp/socket.c:439-461
	if so.SoType == IPProtoICMP {
		// For ICMP, we receive the reply and reflect it back
		conn, ok := so.Extra.(*net.UDPConn)
		if !ok || conn == nil {
			return
		}

		buff := make([]byte, 256)
		conn.SetReadDeadline(time.Now().Add(time.Millisecond))
		n, _, err := conn.ReadFromUDP(buff)
		if err != nil || n <= 0 {
			// Check if this is a timeout (no data available yet)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Just no data available - wait for next poll
				return
			}
			// Real error - send ICMP unreachable
			code := uint8(ICMPUnreachPort)
			// Could check for specific errors like host/net unreachable
			if so.SoM != nil {
				slirp.ICMPError(so.SoM, ICMPUnreach, code, 0, "")
			}
		} else {
			// Got a ping reply - reflect it back
			if so.SoM != nil {
				slirp.ICMPReflect(so.SoM)
				so.SoM = nil // Don't m_free() it again
			}
		}
		// No need for this socket anymore (unless we're waiting for data)
		if err == nil || (err != nil && !err.(net.Error).Timeout()) {
			slirp.UDPDetach(so)
		}
		return
	}

	// Normal UDP packet
	// Reference: tinyemu-2019-12-21/slirp/socket.c:462-525
	m := slirp.MGet()
	if m == nil {
		return
	}
	// Skip link header space for UDP data
	// C: m->m_data += IF_MAXLINKHDR;
	m.SetDataOffset(IFMaxLinkHdr)

	conn, ok := so.Extra.(*net.UDPConn)
	if !ok || conn == nil {
		m.Free()
		return
	}

	// Get the available room and read the data
	maxLen := m.FreeRoom()

	// Set a short read deadline to make this non-blocking
	// In the C code, select() is used to check for data before reading
	conn.SetReadDeadline(time.Now().Add(time.Millisecond))

	// Read from the socket
	n, addr, err := conn.ReadFromUDP(m.Data[:maxLen])
	if err != nil || n < 0 {
		// Check if this is a timeout (no data available)
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Just no data available - not an error
			m.Free()
			return
		}
		// Real error - send ICMP unreachable
		code := uint8(ICMPUnreachPort)
		if so.SoM != nil {
			slirp.ICMPError(so.SoM, ICMPUnreach, code, 0, "")
		}
		m.Free()
		return
	}
	m.Len = n

	// Update expiry time
	// Reference: tinyemu-2019-12-21/slirp/socket.c:512-517
	if so.SoExpire != 0 {
		curtime := GetTimeMs()
		// DNS lookups (port 53) expire faster
		if so.SoFPort == 53<<8|53>>8 { // htons(53)
			so.SoExpire = curtime + SOExpireFast
		} else {
			so.SoExpire = curtime + SOExpire
		}
	}

	// Send the packet via UDP output
	// Reference: tinyemu-2019-12-21/slirp/socket.c:523
	srcAddr := &SockaddrIn{
		Port: uint16(addr.Port),
		Addr: addr.IP,
	}
	slirp.UDPOutput(so, m, srcAddr)
}

// SoSendTo sends data to a UDP socket destination.
// Reference: tinyemu-2019-12-21/slirp/socket.c:532-573
func (so *Socket) SoSendTo(m *Mbuf) int {
	slirp := so.Slirp
	if slirp == nil || m == nil {
		return -1
	}

	// Build destination address
	// Reference: tinyemu-2019-12-21/slirp/socket.c:542-553
	var destIP net.IP

	// Check if destination is in the virtual network (an alias)
	soFAddr := ipToUint32(so.SoFAddr)
	vnetMask := ipToUint32(slirp.VNetworkMask)
	vnetAddr := ipToUint32(slirp.VNetworkAddr)

	// Reference: tinyemu-2019-12-21/slirp/socket.c:543-553
	if (soFAddr & vnetMask) == vnetAddr {
		// It's an alias
		if so.SoFAddr.Equal(slirp.VNameserverAddr) {
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

	conn, ok := so.Extra.(*net.UDPConn)
	if !ok || conn == nil {
		return -1
	}

	addr := &net.UDPAddr{
		IP:   destIP,
		Port: int(so.SoFPort),
	}

	_, err := conn.WriteToUDP(m.Data[:m.Len], addr)
	if err != nil {
		return -1
	}

	// Update expiry and state
	// Reference: tinyemu-2019-12-21/slirp/socket.c:568-571
	if so.SoExpire != 0 {
		so.SoExpire = GetTimeMs() + SOExpire
	}
	so.SoState &= SSPersistentMask
	so.SoState |= SSIsFConnected // So that it gets select()ed

	return 0
}

// TCPSockClosed handles socket closure for TCP.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:282-308
func TCPSockClosed(tp *TCPCB) {
	if tp == nil {
		return
	}

	switch tp.TState {
	case TCPSClosed, TCPSListen, TCPSSynSent:
		// C: tp->t_state = TCPS_CLOSED; tp = tcp_close(tp);
		// After tcp_close, tp is NULL in C, so tcp_output is not called.
		tp.TState = TCPSClosed
		tp.TCPClose()
		return // Don't call tcp_output after close
	case TCPSSynReceived, TCPSEstablished:
		tp.TState = TCPSFinWait1
	case TCPSCloseWait:
		tp.TState = TCPSLastAck
	}
	// C: if (tp) tcp_output(tp)
	// Only reached if we didn't call tcp_close above
	tp.TCPOutput()
}
