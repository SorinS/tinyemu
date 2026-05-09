//go:build linux

package devices

import (
	"os"
	"testing"
)

// canOpenTun checks if we can open and configure /dev/net/tun.
// This requires CAP_NET_ADMIN or root.
func canOpenTun() bool {
	// Actually try to open a TUN device - this tests both open and ioctl
	tun, err := OpenTun("")
	if err != nil {
		return false
	}
	tun.Close()
	return true
}

func TestDefaultMACAddr(t *testing.T) {
	mac := DefaultMACAddr()
	// Reference: tinyemu-2019-12-21/temu.c:438-443
	expected := [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	if mac != expected {
		t.Errorf("DefaultMACAddr() = %v, want %v", mac, expected)
	}
}

func TestOpenTunNoPermission(t *testing.T) {
	if canOpenTun() {
		t.Skip("test requires no permission to open /dev/net/tun")
	}

	_, err := OpenTun("tap0")
	if err == nil {
		t.Error("expected error when opening TUN without permission")
	}
}

func TestOpenTunWithPermission(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root to open /dev/net/tun")
	}

	// Use a unique name to avoid conflicts
	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	// Check that we got a valid fd
	if tun.Fd() < 0 {
		t.Error("expected valid fd")
	}

	// Check that we got a name
	if tun.Name() == "" {
		t.Error("expected non-empty interface name")
	}

	t.Logf("Opened TAP interface: %s (fd=%d)", tun.Name(), tun.Fd())
}

func TestTunDeviceWritePacket(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	// Writing a packet shouldn't panic or error
	// (it may fail silently if the interface isn't configured)
	packet := make([]byte, 64)
	tun.WritePacket(packet)
}

func TestTunDeviceSelectFill(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	// Without callbacks, SelectFill should return the fd but not want read
	fd, wantRead := tun.SelectFill()
	if fd < 0 {
		t.Error("expected valid fd")
	}
	if wantRead {
		t.Error("expected wantRead=false without callbacks")
	}

	// With callbacks that return true, should want read
	tun.SetCallbacks(func() bool { return true }, func([]byte) {})
	fd, wantRead = tun.SelectFill()
	if fd < 0 {
		t.Error("expected valid fd")
	}
	if !wantRead {
		t.Error("expected wantRead=true with canWrite returning true")
	}
}

func TestTunDeviceSelectPollNoData(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	received := false
	tun.SetCallbacks(
		func() bool { return true },
		func(buf []byte) { received = true },
	)

	// selectRet <= 0 should not read
	tun.SelectFill()
	tun.SelectPoll(0, false)
	if received {
		t.Error("should not receive packet with selectRet=0")
	}

	// fdReady=false should not read
	tun.SelectFill()
	tun.SelectPoll(1, false)
	if received {
		t.Error("should not receive packet with fdReady=false")
	}
}

func TestTunDevicePoll(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	// Poll without callbacks should not panic
	tun.Poll()

	// Poll with callbacks but canWrite=false should not read
	received := false
	tun.SetCallbacks(
		func() bool { return false },
		func(buf []byte) { received = true },
	)
	tun.Poll()
	if received {
		t.Error("should not receive when canWrite returns false")
	}
}

func TestTunDeviceClose(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}

	// Close should work
	if err := tun.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Double close should be safe
	if err := tun.Close(); err != nil {
		t.Errorf("double Close failed: %v", err)
	}

	// Operations on closed device should be safe
	tun.WritePacket(make([]byte, 64))
	tun.Poll()

	fd, wantRead := tun.SelectFill()
	if fd != -1 || wantRead {
		t.Error("closed device should return -1, false from SelectFill")
	}
}

func TestTunDeviceFile(t *testing.T) {
	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	f := tun.File()
	if f == nil {
		t.Error("expected non-nil File")
	}

	// Check that the file has the same fd
	if int(f.Fd()) != tun.Fd() {
		t.Errorf("File fd = %d, tun fd = %d", f.Fd(), tun.Fd())
	}
}

// TestTunDeviceIntegration tests sending and receiving packets.
// This is skipped by default as it requires network setup.
func TestTunDeviceIntegration(t *testing.T) {
	if os.Getenv("TEST_TUN_INTEGRATION") == "" {
		t.Skip("set TEST_TUN_INTEGRATION=1 to run integration tests")
	}

	if !canOpenTun() {
		t.Skip("test requires CAP_NET_ADMIN or root")
	}

	tun, err := OpenTun("")
	if err != nil {
		t.Fatalf("OpenTun failed: %v", err)
	}
	defer tun.Close()

	t.Logf("Created TAP interface: %s", tun.Name())
	t.Log("To test packet I/O:")
	t.Logf("  sudo ip link set %s up", tun.Name())
	t.Logf("  sudo ip addr add 192.168.100.1/24 dev %s", tun.Name())
	t.Log("  ping -c 1 192.168.100.2")
}
