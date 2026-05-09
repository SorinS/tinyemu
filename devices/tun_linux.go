//go:build linux

package devices

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TunDevice represents a TAP network device.
// It provides Ethernet frame I/O via Linux's TUN/TAP interface.
//
// Reference: tinyemu-2019-12-21/temu.c:351-354 (TunState)
// Reference: tinyemu-2019-12-21/temu.c:414-451 (tun_open)
type TunDevice struct {
	mu           sync.Mutex
	fd           int
	name         string
	closed       bool
	selectFilled bool // Used by poll-based event loop

	// Callbacks set by the VirtIO network device
	deviceCanWritePacket func() bool
	deviceWritePacket    func(buf []byte)
}

// TUN/TAP ioctl constants
// Reference: linux/if_tun.h
const (
	tunSetIFF   = 0x400454ca // TUNSETIFF
	iffTAP      = 0x0002     // IFF_TAP
	iffNoPi     = 0x1000     // IFF_NO_PI
	maxIfNameSz = 16         // IFNAMSIZ
)

// ifreq is the structure for network interface requests.
// Reference: linux/if.h
type ifreq struct {
	name  [maxIfNameSz]byte
	flags uint16
	_     [22]byte // padding to match struct size
}

// OpenTun opens a TAP device with the given interface name.
// If name is empty, the kernel will assign a name.
//
// Reference: tinyemu-2019-12-21/temu.c:414-451 (tun_open)
func OpenTun(name string) (*TunDevice, error) {
	// Open /dev/net/tun
	// Reference: tinyemu-2019-12-21/temu.c:421
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("could not open /dev/net/tun: %w", err)
	}

	// Set up the interface request
	// Reference: tinyemu-2019-12-21/temu.c:426-428
	var ifr ifreq
	ifr.flags = iffTAP | iffNoPi

	if name != "" {
		copy(ifr.name[:], name)
	}

	// Configure the TUN device
	// Reference: tinyemu-2019-12-21/temu.c:429
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(tunSetIFF), uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("could not configure /dev/net/tun: %w", errno)
	}

	// Set non-blocking mode
	// Reference: tinyemu-2019-12-21/temu.c:435
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("failed to set non-blocking mode: %w", err)
	}

	// Extract the actual interface name (kernel may have assigned one)
	actualName := string(ifr.name[:])
	for i, b := range ifr.name {
		if b == 0 {
			actualName = string(ifr.name[:i])
			break
		}
	}

	return &TunDevice{
		fd:   fd,
		name: actualName,
	}, nil
}

// Name returns the interface name.
func (t *TunDevice) Name() string {
	return t.name
}

// Fd returns the underlying file descriptor.
func (t *TunDevice) Fd() int {
	return t.fd
}

// SetCallbacks sets the callbacks for receiving packets.
// These are typically set by the VirtIO network device.
func (t *TunDevice) SetCallbacks(canWrite func() bool, writePacket func([]byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deviceCanWritePacket = canWrite
	t.deviceWritePacket = writePacket
}

// WritePacket sends a packet to the network.
// This is called when the guest transmits a packet.
//
// Reference: tinyemu-2019-12-21/temu.c:356-361 (tun_write_packet)
func (t *TunDevice) WritePacket(buf []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return
	}

	// Reference: tinyemu-2019-12-21/temu.c:360
	unix.Write(t.fd, buf)
}

// SelectFill prepares for a select() call by checking if we should
// monitor the fd for incoming packets.
//
// Reference: tinyemu-2019-12-21/temu.c:363-375 (tun_select_fill)
func (t *TunDevice) SelectFill() (fd int, wantRead bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return -1, false
	}

	// Check if the guest can receive a packet
	// Reference: tinyemu-2019-12-21/temu.c:370
	if t.deviceCanWritePacket != nil {
		t.selectFilled = t.deviceCanWritePacket()
	} else {
		t.selectFilled = false
	}

	// Reference: tinyemu-2019-12-21/temu.c:371-374
	return t.fd, t.selectFilled
}

// SelectPoll handles the result of a select() call.
// If there's data available and the guest can receive it, read and forward.
//
// Reference: tinyemu-2019-12-21/temu.c:377-394 (tun_select_poll)
func (t *TunDevice) SelectPoll(selectRet int, fdReady bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return
	}

	// Reference: tinyemu-2019-12-21/temu.c:386-387
	if selectRet <= 0 {
		return
	}

	// Reference: tinyemu-2019-12-21/temu.c:388-391
	if t.selectFilled && fdReady {
		buf := make([]byte, 2048)
		n, err := unix.Read(t.fd, buf)
		if err == nil && n > 0 && t.deviceWritePacket != nil {
			t.deviceWritePacket(buf[:n])
		}
	}
}

// Poll reads any available packets and forwards them to the guest.
// This is an alternative to SelectFill/SelectPoll for goroutine-based I/O.
func (t *TunDevice) Poll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return
	}

	// Check if the guest can receive a packet
	if t.deviceCanWritePacket == nil || !t.deviceCanWritePacket() {
		return
	}

	// Try to read a packet (non-blocking)
	buf := make([]byte, 2048)
	n, err := unix.Read(t.fd, buf)
	if err == nil && n > 0 && t.deviceWritePacket != nil {
		t.deviceWritePacket(buf[:n])
	}
}

// Close closes the TUN device.
func (t *TunDevice) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true
	return unix.Close(t.fd)
}

// DefaultMACAddr returns the default MAC address used by TinyEMU.
// Reference: tinyemu-2019-12-21/temu.c:438-443
func DefaultMACAddr() [6]byte {
	return [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
}

// File returns an *os.File for the TUN device.
// This is useful for integration with Go's select/poll mechanisms.
func (t *TunDevice) File() *os.File {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	// Create a File from the fd without closing it when the File is closed
	return os.NewFile(uintptr(t.fd), t.name)
}
