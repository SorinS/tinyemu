package slirp

import (
	"net"
	"syscall"
)

// SBuf is a circular buffer for socket send/receive buffers.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:14-22
type SBuf struct {
	SbCC      int    // actual chars in buffer
	SbDataLen int    // length of data buffer
	SbWPtr    int    // write pointer offset (where next bytes should be written)
	SbRPtr    int    // read pointer offset (where next byte should be read)
	SbData    []byte // actual data buffer
}

// SbFlush drops all data in the buffer.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:11
func (sb *SBuf) SbFlush() {
	sb.SbDrop(sb.SbCC)
}

// SbSpace returns available space in the buffer.
// Reference: tinyemu-2019-12-21/slirp/sbuf.h:12
func (sb *SBuf) SbSpace() int {
	return sb.SbDataLen - sb.SbCC
}

// SbFree frees the buffer data.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:13-16
//
// NOTE: Go version also resets all fields to zero. C only calls free(sb_data).
// This is safe because sbfree is always called immediately before sofree,
// so the stale fields in C are never accessed.
func (sb *SBuf) SbFree() {
	sb.SbData = nil
	sb.SbCC = 0
	sb.SbDataLen = 0
	sb.SbWPtr = 0
	sb.SbRPtr = 0
}

// SbDrop removes num bytes from the front of the buffer.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:19-32
func (sb *SBuf) SbDrop(num int) {
	// We can only drop how much we have
	// This should never succeed (the comment from C means: it should
	// never happen that num > sb_cc)
	if num > sb.SbCC {
		num = sb.SbCC
	}
	sb.SbCC -= num
	sb.SbRPtr += num
	if sb.SbRPtr >= sb.SbDataLen {
		sb.SbRPtr -= sb.SbDataLen
	}
}

// SbReserve allocates or reallocates the buffer to the specified size.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:35-55
func (sb *SBuf) SbReserve(size int) {
	if sb.SbData != nil {
		// Already alloced, realloc if necessary
		if sb.SbDataLen != size {
			sb.SbData = make([]byte, size)
			sb.SbWPtr = 0
			sb.SbRPtr = 0
			sb.SbCC = 0
			sb.SbDataLen = size
		}
	} else {
		sb.SbData = make([]byte, size)
		sb.SbWPtr = 0
		sb.SbRPtr = 0
		sb.SbCC = 0
		sb.SbDataLen = size
	}
}

// sbAppendSb copies data from an Mbuf into the sbuf.
// The caller is responsible to make sure there's enough room.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:124-153
func (sb *SBuf) sbAppendSb(m *Mbuf) {
	mLen := m.Len
	mData := m.Data[:m.Len]

	var n int
	if sb.SbWPtr < sb.SbRPtr {
		n = sb.SbRPtr - sb.SbWPtr
		if n > mLen {
			n = mLen
		}
		copy(sb.SbData[sb.SbWPtr:sb.SbWPtr+n], mData[:n])
	} else {
		// Do the right edge first
		n = sb.SbDataLen - sb.SbWPtr
		if n > mLen {
			n = mLen
		}
		copy(sb.SbData[sb.SbWPtr:sb.SbWPtr+n], mData[:n])
		mLen -= n
		if mLen > 0 {
			// Now the left edge
			nn := sb.SbRPtr
			if nn > mLen {
				nn = mLen
			}
			copy(sb.SbData[0:nn], mData[n:n+nn])
			n += nn
		}
	}

	sb.SbCC += n
	sb.SbWPtr += n
	if sb.SbWPtr >= sb.SbDataLen {
		sb.SbWPtr -= sb.SbDataLen
	}
}

// SbAppend writes data to a socket.
// It tries to send immediately if the buffer is empty, then buffers any remainder.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:64-117
func (so *Socket) SbAppend(m *Mbuf) {
	ret := 0

	// Shouldn't happen, but... e.g. foreign host closes connection
	if m.Len <= 0 {
		m.MFree()
		return
	}

	// If there is urgent data, call sosendoob
	// if not all was sent, sowrite will take care of the rest
	// (The rest of this function is just an optimisation)
	if so.SoUrgc != 0 {
		so.SoRcv.sbAppendSb(m)
		m.MFree()
		so.SoSendOOB()
		return
	}

	// We only write if there's nothing in the buffer,
	// otherwise it'll arrive out of order, and hence corrupt
	if so.SoRcv.SbCC == 0 {
		ret = so.Slirp.slirpSend(so, m.Data[:m.Len], 0)
	}

	if ret <= 0 {
		// Nothing was written
		// It's possible that the socket has closed, but
		// we don't need to check because if it has closed,
		// it will be detected in the normal way by soread()
		so.SoRcv.sbAppendSb(m)
	} else if ret != m.Len {
		// Something was written, but not everything..
		// sbappendsb the rest
		// C: m->m_data += ret; m->m_len -= ret;
		m.Adj(ret)
		so.SoRcv.sbAppendSb(m)
	}
	// Whatever happened, we free the mbuf
	m.MFree()
}

// SbCopy copies data from the sbuf to a destination buffer.
// Doesn't update the sbuf rptr; that's done in SbDrop when data is acked.
// Reference: tinyemu-2019-12-21/slirp/sbuf.c:161-181
func (sb *SBuf) SbCopy(off, length int, to []byte) {
	from := sb.SbRPtr + off
	if from >= sb.SbDataLen {
		from -= sb.SbDataLen
	}

	if from < sb.SbWPtr {
		if length > sb.SbCC {
			length = sb.SbCC
		}
		copy(to, sb.SbData[from:from+length])
	} else {
		// re-use off variable
		off = sb.SbDataLen - from
		if off > length {
			off = length
		}
		copy(to, sb.SbData[from:from+off])
		length -= off
		if length > 0 {
			copy(to[off:], sb.SbData[:length])
		}
	}
}

// SbAppendBytes appends raw bytes to the sbuf.
// This is a helper for cases where we want to add data directly.
func (sb *SBuf) SbAppendBytes(data []byte) {
	if len(data) == 0 {
		return
	}

	// Create a temporary mbuf-like structure for sbAppendSb
	m := &Mbuf{
		Data: data,
		Len:  len(data),
	}
	sb.sbAppendSb(m)
}

// SbBytes returns a copy of the data in the buffer.
// This is useful for testing and debugging.
func (sb *SBuf) SbBytes() []byte {
	if sb.SbCC == 0 {
		return nil
	}
	result := make([]byte, sb.SbCC)
	sb.SbCopy(0, sb.SbCC, result)
	return result
}

// slirpSend sends data on a socket using the system send call.
// Returns the number of bytes sent, or -1 on error.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:773-782
//
// When called with nil/empty data, this can be used to probe connection status.
// A zero-byte send that succeeds indicates the socket is connected.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:438 (send(so->s, &ret, 0, 0))
//
// For incoming connections via TCPListen, the socket is a Go *net.TCPConn stored
// in so.Extra rather than a raw file descriptor in so.S. This function handles
// both cases transparently.
func (s *Slirp) slirpSend(so *Socket, data []byte, flags int) int {
	if so == nil {
		return -1
	}

	// Check for incoming connections via TCPListen - they use net.TCPConn in Extra
	// rather than a raw file descriptor in S.
	if conn, ok := so.Extra.(*net.TCPConn); ok && conn != nil {
		if len(data) == 0 {
			return 0 // Zero-byte send succeeds for connected net.Conn
		}
		n, err := conn.Write(data)
		if err != nil {
			// For net.TCPConn, Write is blocking by default, so errors are typically
			// connection errors. Return -1 to indicate error.
			return -1
		}
		return n
	}

	// Standard case: raw socket file descriptor in so.S
	if so.S < 0 {
		return -1
	}

	// Handle zero-byte send for connection probing
	// In C: send(so->s, &ret, 0, 0) - sends 0 bytes to check if connected
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:438
	if len(data) == 0 {
		// Use send() with empty buffer to probe connection status
		// A successful 0-byte send indicates the socket is connected
		err := syscall.Sendto(so.S, []byte{}, 0, nil)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK ||
				err == syscall.EINPROGRESS || err == syscall.ENOTCONN {
				return 0 // Still connecting, not an error
			}
			return -1
		}
		return 0
	}

	// Use syscall.Write for plain send (equivalent to send() without flags)
	// For TCP sockets, write() and send() with flags=0 are equivalent
	// Reference: tinyemu-2019-12-21/slirp/slirp.c:781
	n, err := syscall.Write(so.S, data)
	if err != nil {
		// Check for would-block conditions
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return 0 // Would block, try again later
		}
		return -1
	}
	return n
}

// slirpSendOOB sends out-of-band (urgent) data on a socket.
// Returns the number of bytes sent, or -1 on error.
// Reference: tinyemu-2019-12-21/slirp/socket.c:297-298, 318
func (s *Slirp) slirpSendOOB(so *Socket, data []byte) int {
	if so == nil || so.S < 0 || len(data) == 0 {
		return 0
	}

	// Use syscall.Sendto with MSG_OOB flag for out-of-band data
	// Reference: tinyemu-2019-12-21/slirp/socket.c:297
	err := syscall.Sendto(so.S, data, syscall.MSG_OOB, nil)
	if err != nil {
		// Check for would-block conditions
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return 0 // Would block, try again later
		}
		return -1
	}
	return len(data)
}
