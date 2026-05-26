//go:build windows

package slirp

// Windows builds of slirp don't currently support real socket polling —
// the unix poll/POLLPRI machinery isn't available, and the slirp socket
// fds on Windows would need WSAEventSelect/WSAPoll plumbing we haven't
// written yet. We provide conservative stubs so the package still
// compiles on Windows: never report OOB data, and never advance a
// connecting socket via the poll path (the connect will still finish
// via the normal send/read codepaths when data flows). This keeps
// cmd/temu buildable on Windows; full slirp networking on Windows is
// tracked separately.

type sockPollState int

const (
	sockPollPending sockPollState = iota
	sockPollWritable
	sockPollFailed
)

func pollSocketOOB(fd int) bool                  { return false }
func pollSocketWritable(fd int) sockPollState    { return sockPollPending }
