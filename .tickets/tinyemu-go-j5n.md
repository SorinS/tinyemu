---
id: tinyemu-go-j5n
status: closed
deps: [tinyemu-go-af5]
links: []
created: 2026-01-15T15:51:11.619728056-05:00
type: feature
priority: 2
---
# Implement VirtIO Network Device

RX/TX queues, MAC address handling, packet send/receive



## Notes

**2026-01-24T21:19:18Z**

VirtIO Network Device is implemented and verified working via:
- TestLinuxBootTCPHostServer - guest-to-host TCP data exchange
- TestLinuxBootTCPGuestServer - host-to-guest TCP data exchange

Core functionality implemented in virtio/net.go:
- RX/TX queues with descriptor chain processing
- MAC address handling
- Packet send/receive via EthernetDevice interface
- IRQ delivery for received packets
