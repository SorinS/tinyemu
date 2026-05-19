//go:build darwin

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Terminal handles raw mode terminal I/O.
// Reference: tinyemu-2019-12-21/temu.c:58-66 (STDIODevice, oldtty, old_fd0_flags)
type Terminal struct {
	fd          int
	origTermios unix.Termios
	origFlags   int
}

// Termios action constants
const (
	tcsaNOW   = 0 // Make changes immediately
	tcsaDRAIN = 1 // Make changes after draining output
	tcsaFLUSH = 2 // Make changes after flushing
)

// NewTerminal creates a new Terminal in raw mode.
// Reference: tinyemu-2019-12-21/temu.c:74-97 (term_init)
//
// When stdin is not a TTY (e.g. when piping commands in for tests or
// scripted runs), raw-mode setup fails. We fall back to a passthrough
// Terminal whose Read uses non-blocking-style read-with-timeout via a
// background goroutine — letting the emulator's main loop poll for
// stdin bytes without blocking on it.
func NewTerminal(allowCtrlC bool) (*Terminal, error) {
	fd := int(os.Stdin.Fd())

	// Get current termios settings
	// Darwin uses TIOCGETA instead of TCGETS
	origTermios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		// Not a TTY — return a passthrough terminal. Tests, scripts,
		// and CI runs hit this path. Warn loudly so an interactive
		// user knows why Ctrl-A x won't exit the emulator: in
		// passthrough mode the kernel's line discipline buffers input
		// until newline and intercepts Ctrl-C as SIGINT instead of
		// passing it through. To exit, press Enter then send EOF
		// (Ctrl-D) or kill the process.
		fmt.Fprintln(os.Stderr,
			"tinyemu: stdin is not a TTY (IoctlGetTermios: "+err.Error()+
				"); Ctrl-A x won't exit. Press Ctrl-C in the launching shell or kill the process.")
		return newPassthroughTerminal(fd), nil
	}

	// Get current flags
	origFlags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return nil, err
	}

	t := &Terminal{
		fd:          fd,
		origTermios: *origTermios,
		origFlags:   origFlags,
	}

	// Set up raw mode
	raw := *origTermios

	// Input flags: disable various processing
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON

	// Output flags: enable post-processing
	raw.Oflag |= unix.OPOST

	// Local flags: disable echo, canonical mode, extensions
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.IEXTEN
	if !allowCtrlC {
		raw.Lflag &^= unix.ISIG
	}

	// Control flags: 8-bit characters, no parity
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8

	// Control characters: return after 1 byte, no timeout
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0

	// Darwin uses TIOCSETA instead of TCSETS
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return nil, err
	}

	// Set non-blocking mode
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, origFlags|unix.O_NONBLOCK); err != nil {
		// Restore original termios on error
		unix.IoctlSetTermios(fd, unix.TIOCSETA, origTermios)
		return nil, err
	}

	return t, nil
}

// Restore restores the terminal to its original state.
// Reference: tinyemu-2019-12-21/temu.c:68-72 (term_exit)
func (t *Terminal) Restore() {
	unix.IoctlSetTermios(t.fd, unix.TIOCSETA, &t.origTermios)
	unix.FcntlInt(uintptr(t.fd), unix.F_SETFL, t.origFlags)
}

// Read reads available data from the terminal.
// Returns 0 if no data is available.
func (t *Terminal) Read(buf []byte) int {
	n, err := unix.Read(t.fd, buf)
	if err != nil {
		return 0
	}
	return n
}

// ReadBlocking blocks until at least one byte is readable on the
// terminal (or an error), then reads available data. Intended for use
// from a dedicated reader goroutine so the emulator's main loop
// doesn't have to poll stdin every iteration. Sleeps in the kernel
// via poll(2) when there's nothing to read — costs no CPU.
func (t *Terminal) ReadBlocking(buf []byte) int {
	pfds := []unix.PollFd{{Fd: int32(t.fd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(pfds, -1)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0
		}
		if pfds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return 0
		}
		n, err := unix.Read(t.fd, buf)
		if err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return 0
		}
		return n
	}
}

// Write writes data to the terminal (stdout).
// Reference: tinyemu-2019-12-21/temu.c:99-103 (console_write)
func (t *Terminal) Write(buf []byte) (int, error) {
	return os.Stdout.Write(buf)
}

// GetSize returns the terminal window size.
// Reference: tinyemu-2019-12-21/temu.c:161-175 (console_get_size)
func (t *Terminal) GetSize() (width, height int) {
	ws, err := unix.IoctlGetWinsize(t.fd, unix.TIOCGWINSZ)
	if err != nil || ws.Col < 4 || ws.Row < 4 {
		return 80, 25 // Default size
	}
	return int(ws.Col), int(ws.Row)
}

// newPassthroughTerminal returns a Terminal that doesn't manipulate
// termios — used when stdin is a pipe / file / /dev/null. Read is
// non-blocking via the O_NONBLOCK flag (with a fallback that swallows
// EAGAIN). Restore is a no-op.
func newPassthroughTerminal(fd int) *Terminal {
	// Make stdin non-blocking so the main poll loop doesn't get stuck
	// reading from a slow pipe.
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err == nil {
		unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags|unix.O_NONBLOCK)
	}
	return &Terminal{fd: fd, origFlags: flags}
}
