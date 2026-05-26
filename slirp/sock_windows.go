//go:build windows

package slirp

import "syscall"

// Windows variant of the per-OS socket wrappers. We use Winsock through
// the standard library's syscall package, casting between int (the type
// the portable slirp code uses for so.S) and syscall.Handle at the
// boundary. Constants that don't exist in syscall on Windows are
// defined here from the canonical Winsock values.
//
// Note: full slirp networking on Windows still requires polling support
// (see device_poll_windows.go) — until that lands the user-mode network
// won't actually carry traffic, but the package builds.

const (
	// Winsock canonical values from <winsock2.h>.
	sockMSG_OOB      = 0x0001
	sockSO_OOBINLINE = 0x0100

	sockSOLSocket   = syscall.SOL_SOCKET
	sockSOReuseAddr = syscall.SO_REUSEADDR
)

func sockSocketTCP() (int, error) {
	h, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	return int(h), nil
}

func sockSetNonblock(fd int) error {
	return syscall.SetNonblock(syscall.Handle(fd), true)
}

func sockSetsockoptInt(fd, level, opt, value int) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), level, opt, value)
}

func sockConnectInet4(fd int, addr [4]byte, port int) error {
	sa := syscall.SockaddrInet4{Port: port}
	sa.Addr = addr
	return syscall.Connect(syscall.Handle(fd), &sa)
}

func sockRead(fd int, p []byte) (int, error) {
	return syscall.Read(syscall.Handle(fd), p)
}

func sockWrite(fd int, p []byte) (int, error) {
	return syscall.Write(syscall.Handle(fd), p)
}

func sockSendto(fd int, p []byte, flags int) error {
	return syscall.Sendto(syscall.Handle(fd), p, flags, nil)
}

func sockShutdownRead(fd int) error  { return syscall.Shutdown(syscall.Handle(fd), syscall.SHUT_RD) }
func sockShutdownWrite(fd int) error { return syscall.Shutdown(syscall.Handle(fd), syscall.SHUT_WR) }
func sockClose(fd int) error         { return syscall.Close(syscall.Handle(fd)) }
