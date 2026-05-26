//go:build !windows

package slirp

import "golang.org/x/sys/unix"

// sockPollState reports the outcome of polling a connecting socket for
// writability — see pollSocketWritable.
type sockPollState int

const (
	sockPollPending sockPollState = iota
	sockPollWritable
	sockPollFailed
)

// pollSocketOOB reports whether fd looks like it has genuine
// out-of-band (urgent) data ready to read. We narrow the check to
// POLLPRI WITHOUT POLLIN/POLLHUP because Darwin (and some Linux
// configurations) fires POLLPRI spuriously when the kernel has any
// out-of-band-ish state (peer half-close, error queue, etc.) — not just
// real RFC-1122 urgent data. Mistakenly entering the OOB path makes
// SoRecvOOB set tp.SndUp = SndUna + SbCC, after which every subsequent
// data segment is tagged TH_URG. Real Linux receivers special-case URG,
// which manifests as "first MTU comes through, then hang" (apk update
// style).
func pollSocketOOB(fd int) bool {
	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLPRI}}
	n, _ := unix.Poll(pollFds, 0) // timeout=0 for non-blocking check
	rev := pollFds[0].Revents
	return n > 0 && (rev&unix.POLLPRI) != 0 && (rev&(unix.POLLIN|unix.POLLHUP)) == 0
}

// pollSocketWritable polls a connecting socket to see whether the
// non-blocking connect has completed. A non-blocking connect socket
// becomes writable when the connection completes.
// Reference: tinyemu-2019-12-21/slirp/slirp.c:315-320 (slirp_select_fill
// adds connecting sockets to write set).
func pollSocketWritable(fd int) sockPollState {
	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
	n, err := unix.Poll(pollFds, 0)
	if err != nil || n <= 0 {
		return sockPollPending
	}
	if (pollFds[0].Revents & (unix.POLLERR | unix.POLLHUP)) != 0 {
		return sockPollFailed
	}
	if (pollFds[0].Revents & unix.POLLOUT) == 0 {
		return sockPollPending
	}
	return sockPollWritable
}
