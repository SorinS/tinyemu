package slirp

import (
	"net"
)

// ExList represents an exec list entry for port forwarding.
// Reference: tinyemu-2019-12-21/slirp/misc.h:11-17
type ExList struct {
	ExFPort uint16  // foreign port
	ExAddr  net.IP  // address
	ExPty   int     // pty mode (0, 1, 2, or 3)
	ExExec  string  // command to execute
	ExNext  *ExList // next entry
}

// TCPCtl does misc. config of SLiRP while it's running.
// Return 0 if this connection is to be closed, 1 otherwise,
// return 2 if this is a command-line connection.
// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:884-915
func TCPCtl(so *Socket) int {
	if so == nil || so.Slirp == nil {
		return 0
	}
	slirp := so.Slirp

	// Check if destination is not the virtual host address
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:894
	if !so.SoFAddr.Equal(slirp.VHostAddr) {
		// Check if it's in the exec list (pty_exec)
		// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:896-908
		for ex := slirp.ExecList; ex != nil; ex = ex.ExNext {
			// Match on port and address
			// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:897-898
			if ex.ExFPort == so.SoFPort && so.SoFAddr.Equal(ex.ExAddr) {
				// ex_pty == 3: special mode that sets socket descriptor to -1
				// and stores exec string in extra field
				// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:899-903
				if ex.ExPty == 3 {
					so.S = -1
					so.Extra = ex.ExExec
					slirp.traceTCPCtl(so, 1, ex.ExExec)
					return 1
				}
				// Otherwise call fork_exec (which is disabled and returns 0)
				// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:904-906
				slirp.traceTCPCtl(so, 0, ex.ExExec)
				return ForkExec()
			}
		}
	}

	// Write error message to send buffer
	// Reference: tinyemu-2019-12-21/slirp/tcp_subr.c:910-914
	errorMsg := []byte("Error: No application configured.\r\n")
	so.SoSnd.SbAppendBytes(errorMsg)
	slirp.traceTCPCtl(so, 0, "")
	return 0
}
