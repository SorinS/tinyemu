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

// pollSocketsOOB checks many fds for genuine OOB (urgent) data in a SINGLE
// poll() syscall and returns the set of fds that have it. Polling each socket
// individually cost ~16% of CPU under network load (one poll() per socket per
// network pass); batching collapses that to one poll() per pass regardless of
// how many sockets are open.
//
// We narrow each fd's result to POLLPRI WITHOUT POLLIN/POLLHUP: Darwin (and
// some Linux configs) fire POLLPRI spuriously when the kernel has any
// out-of-band-ish state (peer half-close, error queue, etc.) — not just real
// RFC-1122 urgent data. Mistakenly entering the OOB path makes SoRecvOOB set
// tp.SndUp = SndUna + SbCC, after which every subsequent data segment is
// tagged TH_URG; real receivers special-case URG, which manifests as "first
// MTU comes through, then hang" (apk-update style).
func pollSocketsOOB(fds []int) map[int]bool {
	if len(fds) == 0 {
		return nil
	}
	pfds := make([]unix.PollFd, len(fds))
	for i, fd := range fds {
		pfds[i] = unix.PollFd{Fd: int32(fd), Events: unix.POLLPRI}
	}
	n, _ := unix.Poll(pfds, 0)
	if n <= 0 {
		return nil
	}
	var out map[int]bool
	for i := range pfds {
		rev := pfds[i].Revents
		if (rev&unix.POLLPRI) != 0 && (rev&(unix.POLLIN|unix.POLLHUP)) == 0 {
			if out == nil {
				out = make(map[int]bool)
			}
			out[fds[i]] = true
		}
	}
	return out
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
