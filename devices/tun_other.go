//go:build !linux

package devices

import "errors"

// TunDevice is a stub for non-Linux systems.
// TUN/TAP support requires Linux.
type TunDevice struct{}

// OpenTun returns an error on non-Linux systems.
func OpenTun(name string) (*TunDevice, error) {
	return nil, errors.New("TUN/TAP devices are only supported on Linux")
}

// Name returns empty string on non-Linux systems.
func (t *TunDevice) Name() string { return "" }

// Fd returns -1 on non-Linux systems.
func (t *TunDevice) Fd() int { return -1 }

// SetCallbacks is a no-op on non-Linux systems.
func (t *TunDevice) SetCallbacks(canWrite func() bool, writePacket func([]byte)) {}

// WritePacket is a no-op on non-Linux systems.
func (t *TunDevice) WritePacket(buf []byte) {}

// SelectFill returns -1, false on non-Linux systems.
func (t *TunDevice) SelectFill() (fd int, wantRead bool) { return -1, false }

// SelectPoll is a no-op on non-Linux systems.
func (t *TunDevice) SelectPoll(selectRet int, fdReady bool) {}

// Poll is a no-op on non-Linux systems.
func (t *TunDevice) Poll() {}

// Close is a no-op on non-Linux systems.
func (t *TunDevice) Close() error { return nil }

// DefaultMACAddr returns the default MAC address.
func DefaultMACAddr() [6]byte {
	return [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
}
