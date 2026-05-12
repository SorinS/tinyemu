package main

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/slirp"
	"github.com/jtolio/tinyemu-go/virtio"
)

// NewNetDevice returns a virtio.EthernetDevice for the named driver.
// "user" → in-process slirp (host-NAT userspace networking). Other
// drivers (tap, etc.) are not supported in this build.
func NewNetDevice(driver string) (*virtio.EthernetDevice, error) {
	switch driver {
	case "user":
		return slirp.NewEthernetDevice(), nil
	default:
		return nil, fmt.Errorf("Unsupported network driver '%s'\n", driver)
	}
}

// NetPoll services the slirp event loop once. Called from the main
// emulator tick.
func NetPoll(es *virtio.EthernetDevice) {
	slirp.Poll(es)
}
