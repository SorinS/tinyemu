//go:build !windows

package slirp

import "syscall"

// Per-OS wrappers around the POSIX socket calls slirp needs. The
// portable slirp code uses these helpers (taking plain int fds) so the
// package can also build on Windows, where syscall.Socket returns a
// Handle and the MSG_OOB / SO_OOBINLINE constants live elsewhere.
//
// The unix variant is a thin wrapper around the standard syscall
// package — see sock_windows.go for the Windows stub.

const (
	sockMSG_OOB      = syscall.MSG_OOB
	sockSO_OOBINLINE = syscall.SO_OOBINLINE
)

func sockSocketTCP() (int, error) {
	return syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
}

func sockSetNonblock(fd int) error {
	return syscall.SetNonblock(fd, true)
}

func sockSetsockoptInt(fd, level, opt, value int) error {
	return syscall.SetsockoptInt(fd, level, opt, value)
}

func sockConnectInet4(fd int, addr [4]byte, port int) error {
	sa := syscall.SockaddrInet4{Port: port}
	sa.Addr = addr
	return syscall.Connect(fd, &sa)
}

func sockRead(fd int, p []byte) (int, error)  { return syscall.Read(fd, p) }
func sockWrite(fd int, p []byte) (int, error) { return syscall.Write(fd, p) }

func sockSendto(fd int, p []byte, flags int) error {
	return syscall.Sendto(fd, p, flags, nil)
}

func sockShutdownRead(fd int) error  { return syscall.Shutdown(fd, syscall.SHUT_RD) }
func sockShutdownWrite(fd int) error { return syscall.Shutdown(fd, syscall.SHUT_WR) }
func sockClose(fd int) error         { return syscall.Close(fd) }

// sockDup duplicates a file descriptor — used by the hostfwd inbound path to
// take ownership of an accepted connection's fd out of the Go runtime.
func sockDup(fd int) (int, error) { return syscall.Dup(fd) }

// sockSOLSocket exposes SOL_SOCKET for the rare slirp call site that
// wants to use sockSetsockoptInt directly.
const sockSOLSocket = syscall.SOL_SOCKET
const sockSOReuseAddr = syscall.SO_REUSEADDR
