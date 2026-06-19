package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/sorins/tinyemu-go/slirp"
	"github.com/sorins/tinyemu-go/virtio"
)

// ApplyHostfwd installs slirp host→guest port-forwarding rules on a slirp-
// backed EthernetDevice. Each rule is "proto:hostport:guestport" (proto is
// tcp or udp): the host listens on 127.0.0.1:hostport and NATs to the
// guest's DHCP address (10.0.2.15) on guestport. Multiple rules are
// comma-separated. Example: "tcp:8080:80,tcp:2222:22".
func ApplyHostfwd(es *virtio.EthernetDevice, spec string) error {
	if spec == "" {
		return nil
	}
	s := slirp.GetSlirp(es)
	if s == nil {
		return fmt.Errorf("-net-hostfwd requires user-mode networking (-net-user)")
	}
	hostAddr := net.IPv4(127, 0, 0, 1)
	guestAddr := net.IPv4(10, 0, 2, 15) // slirp DHCP start (first guest lease)
	for _, rule := range strings.Split(spec, ",") {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		parts := strings.Split(rule, ":")
		if len(parts) != 3 {
			return fmt.Errorf("bad hostfwd rule %q (want proto:hostport:guestport)", rule)
		}
		isUDP := false
		switch strings.ToLower(parts[0]) {
		case "tcp":
		case "udp":
			isUDP = true
		default:
			return fmt.Errorf("bad hostfwd proto %q (want tcp or udp)", parts[0])
		}
		hostPort, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("bad hostfwd host port %q: %v", parts[1], err)
		}
		guestPort, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("bad hostfwd guest port %q: %v", parts[2], err)
		}
		if s.AddHostfwd(isUDP, hostAddr, hostPort, guestAddr, guestPort) < 0 {
			return fmt.Errorf("could not forward host %s:%d -> guest %s:%d (port in use?)",
				hostAddr, hostPort, guestAddr, guestPort)
		}
		fmt.Printf("hostfwd: %s 127.0.0.1:%d -> 10.0.2.15:%d\n", parts[0], hostPort, guestPort)
	}
	return nil
}

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
