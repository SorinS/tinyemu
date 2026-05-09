---
id: tg-de42
status: closed
deps: [tg-4990]
links: []
created: 2026-01-25T03:39:20Z
type: task
priority: 1
assignee: JT Olio
parent: tg-4094
---
# Test DHCP and DNS in Ubuntu image

Verify networking works in Ubuntu:
1. Boot image
2. Wait for systemd-networkd/netplan to configure network
3. Check 'ip addr' shows 10.0.2.15 on eth0
4. Check 'ip route' shows default via 10.0.2.2
5. Check /etc/resolv.conf or systemd-resolved status
6. Test DNS: host example.com or ping example.com

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- eth0 has IP 10.0.2.15
- Default route configured
- DNS resolution works


## Notes

**2026-01-25T04:43:31Z**

Dropped - Ubuntu RISC-V images too large (722MB cloud image vs 40MB target). No minimal prebuilt option available.
