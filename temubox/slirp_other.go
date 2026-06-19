//go:build !linux

package temubox

import (
	"github.com/sorins/tinyemu-go/virtio"
)

func newNetDevice() *virtio.EthernetDevice {
	return nil
}

func netPoll(es *virtio.EthernetDevice) {}
