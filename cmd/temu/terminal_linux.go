//go:build linux

package main

import (
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
func NewTerminal(allowCtrlC bool) (*Terminal, error) {
	fd := int(os.Stdin.Fd())

	// Get current termios settings
	origTermios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		// Not a TTY (pipe / file / /dev/null). Fall back to a
		// passthrough Terminal — useful for scripted runs and tests.
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

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, err
	}

	// Set non-blocking mode
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, origFlags|unix.O_NONBLOCK); err != nil {
		// Restore original termios on error
		unix.IoctlSetTermios(fd, unix.TCSETS, origTermios)
		return nil, err
	}

	return t, nil
}

// Restore restores the terminal to its original state.
// Reference: tinyemu-2019-12-21/temu.c:68-72 (term_exit)
func (t *Terminal) Restore() {
	unix.IoctlSetTermios(t.fd, unix.TCSETS, &t.origTermios)
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
// termios — used when stdin is a pipe / file / /dev/null. Restore is
// a no-op.
func newPassthroughTerminal(fd int) *Terminal {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err == nil {
		unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags|unix.O_NONBLOCK)
	}
	return &Terminal{fd: fd, origFlags: flags}
}
