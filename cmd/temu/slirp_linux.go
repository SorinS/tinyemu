//go:build linux

package main

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/slirp"
	"github.com/jtolio/tinyemu-go/virtio"
)

func NewNetDevice(driver string) (*virtio.EthernetDevice, error) {
	switch driver {
	case "user":
		// Slirp userspace networking
		// Reference: tinyemu-2019-12-21/temu.c:785-789
		return slirp.NewEthernetDevice(), nil
	default:
		return nil, fmt.Errorf("Unsupported network driver '%s'\n", driver)
	}
}

func NetPoll(es *virtio.EthernetDevice) {
	slirp.Poll(es)
}
