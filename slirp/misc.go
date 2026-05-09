package slirp

import (
	"fmt"
	"time"
)

// GetTimeMs returns the current time in milliseconds from a monotonic clock.
// This is used for timing operations like TCP retransmission and connection expiry.
//
// Reference: tinyemu-2019-12-21/slirp/misc.c:88-95
func GetTimeMs() uint32 {
	// The C code uses clock_gettime(CLOCK_MONOTONIC, &ts) and returns
	// ts.tv_sec * 1000 + ts.tv_nsec / 1000000
	// Go's time package provides monotonic time via time.Now()
	d := time.Since(startTime)
	return uint32(d.Milliseconds())
}

// startTime is used as the base for monotonic time calculations.
// This matches the behavior of CLOCK_MONOTONIC which is relative to an
// arbitrary start point (typically system boot).
var startTime = time.Now()

// Lprint prints a formatted message to stdout.
// This is a simple logging function used throughout slirp.
//
// Reference: tinyemu-2019-12-21/slirp/misc.c:267-274
//
// Note: In Go, use fmt.Printf or log.Printf directly. This function
// is provided for API compatibility during porting.
func Lprint(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// ForkExec is not implemented.
// The C code has this disabled (returns 0) via #if 1 preprocessor directive.
//
// Reference: tinyemu-2019-12-21/slirp/misc.c:100-105
// The enabled version just returns 0 (not implemented).
func ForkExec() int {
	return 0
}

// The following C functions are NOT ported because Go has built-in equivalents:
//
// strerror (misc.c:71-79):
//   - Go errors implement Error() string
//   - Use error.Error() or fmt.Errorf() instead
//   - The C code is a fallback for systems without strerror (#ifndef HAVE_STRERROR)
//
// strdup (misc.c:253-264):
//   - Go strings are immutable and garbage collected
//   - Simply assign: newStr := oldStr
//   - The C code is a fallback for systems without strdup (#ifndef HAVE_STRDUP)
//
// os_socket (misc.c:83-86):
//   - Go's net package provides socket operations
//   - Use net.Dial(), net.Listen(), etc.
//   - Raw socket access via syscall.Socket() if needed
//
// fd_nonblock (misc.c:280-298):
//   - Go's net package handles non-blocking I/O internally
//   - All net.Conn operations are non-blocking by default
//   - Use SetDeadline() for timeout control
//
// fd_block (misc.c:300-318):
//   - See fd_nonblock note above
//   - Go's net package abstracts this away
//
// add_exec (misc.c:40-60):
//   - Implemented in slirp.go:addExecInternal
//   - Checks for duplicate port/address before adding
