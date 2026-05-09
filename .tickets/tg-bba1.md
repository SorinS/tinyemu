---
id: tg-bba1
status: closed
deps: [tg-e2a9]
links: []
created: 2026-01-25T03:30:33Z
type: task
priority: 1
assignee: JT Olio
parent: tg-6d6d
---
# Create rootfs overlay with network init scripts

Create rootfs-overlay/ with:
- /etc/init.d/S40network - starts udhcpc on eth0
- /etc/udhcpc.script - configures IP, route, and DNS from DHCP response

The scripts must:
1. Bring up eth0
2. Run udhcpc to get IP from slirp DHCP (10.0.2.2)
3. Configure /etc/resolv.conf with DNS server (10.0.2.3)
4. Set default route via gateway

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- Scripts are executable
- Network comes up automatically on boot
- /etc/resolv.conf populated from DHCP

