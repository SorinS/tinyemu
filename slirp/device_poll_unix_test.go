//go:build !windows

package slirp

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestPollSocketsOOB: the batched OOB check (one poll() for all fds, the
// replacement for the per-socket poll() that cost ~16% of CPU) must not
// false-positive on a socket that merely has normal readable data, and must
// handle the empty case. (Genuine TCP urgent data is virtually never used by
// the workloads temu runs, so the important property is "no false OOB".)
func TestPollSocketsOOB(t *testing.T) {
	if got := pollSocketsOOB(nil); got != nil {
		t.Errorf("pollSocketsOOB(nil) = %v, want nil", got)
	}

	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Skipf("socketpair unavailable: %v", err)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])

	// Normal (non-urgent) data: fds[0] becomes POLLIN-readable, NOT POLLPRI.
	if _, err := unix.Write(fds[1], []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := pollSocketsOOB([]int{fds[0]}); got[fds[0]] {
		t.Errorf("normal readable socket falsely reported as OOB (revents had POLLIN, not urgent)")
	}
}
