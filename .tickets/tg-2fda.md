---
id: tg-2fda
status: open
deps: [tg-69bd]
links: []
created: 2026-01-25T03:32:32Z
type: task
priority: 1
assignee: JT Olio
parent: tg-6d6d
---
# Test DHCP and DNS in buildroot image

Verify networking works:
1. Boot image
2. Check 'ip addr' shows 10.0.2.15 on eth0
3. Check 'ip route' shows default via 10.0.2.2
4. Check /etc/resolv.conf contains nameserver 10.0.2.3
5. Test DNS: nslookup example.com (or ping by hostname if available)

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- eth0 has IP 10.0.2.15
- Default route via 10.0.2.2
- DNS server configured as 10.0.2.3
- Hostname resolution works

