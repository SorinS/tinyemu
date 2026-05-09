---
id: tg-7b2b
status: closed
deps: []
links: []
created: 2026-01-25T02:39:34Z
type: task
priority: 3
assignee: JT Olio
---
# Create Alpine-based minimal rootfs with DNS support

The current TinyEMU rootfs (from bellard.org) uses glibc without NSS modules, so DNS resolution doesn't work. Create a new minimal rootfs using Alpine Linux (which uses musl libc with built-in DNS resolution).

Approach:
- Use buildroot with musl or Alpine Linux minirootfs
- Include busybox with networking tools
- Configure /etc/resolv.conf to use 10.0.2.3 (SLIRP DNS proxy)
- Keep image size minimal (under 5MB if possible)
- Test that 'nc www.google.com 80' works with hostname resolution

This will enable tests to use hostnames instead of pre-resolved IPs.

Reference: https://alpinelinux.org/downloads/ (mini root filesystem)
Reference: https://buildroot.org/ (alternative with musl option)

