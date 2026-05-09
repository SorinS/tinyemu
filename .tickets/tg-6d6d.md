---
id: tg-6d6d
status: open
deps: []
links: []
created: 2026-01-25T03:27:47Z
type: epic
priority: 1
assignee: JT Olio
---
# Buildroot minimal image (<10MB) with curl and DNS

Create a minimal Linux image using Buildroot that boots in TinyEMU with working networking (DHCP + DNS) and curl for HTTPS requests. Target size: <10MB total (kernel + rootfs + BBL).

## Acceptance Criteria

- Image boots successfully in TinyEMU
- DHCP acquires IP from slirp (10.0.2.15)
- DNS resolution works (can resolve hostnames)
- curl https://example.com succeeds
- Total size (kernel + rootfs + BBL) < 10MB

