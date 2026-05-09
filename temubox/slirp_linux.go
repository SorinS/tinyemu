//go:build linux

package temubox

import (
	"github.com/jtolio/tinyemu-go/slirp"
	"github.com/jtolio/tinyemu-go/virtio"
)

func newNetDevice() *virtio.EthernetDevice {
	return slirp.NewEthernetDevice()
}

func netPoll(es *virtio.EthernetDevice) {
	slirp.Poll(es)
}
