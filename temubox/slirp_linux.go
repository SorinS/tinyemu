//go:build linux

package temubox

import (
	"github.com/sorins/tinyemu-go/slirp"
	"github.com/sorins/tinyemu-go/virtio"
)

func newNetDevice() *virtio.EthernetDevice {
	return slirp.NewEthernetDevice()
}

func netPoll(es *virtio.EthernetDevice) {
	slirp.Poll(es)
}
