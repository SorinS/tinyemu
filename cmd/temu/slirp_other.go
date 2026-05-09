//go:build !linux

package main

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/virtio"
)

func NewNetDevice(driver string) (*virtio.EthernetDevice, error) {
	return nil, fmt.Errorf("Unsupported network driver '%s'\n", driver)
}

func NetPoll(es *virtio.EthernetDevice) {}
