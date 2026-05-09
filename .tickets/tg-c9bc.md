---
id: tg-c9bc
status: open
deps: [tg-2fda]
links: []
created: 2026-01-25T03:33:12Z
type: task
priority: 1
assignee: JT Olio
parent: tg-6d6d
---
# Test curl HTTPS in buildroot image

Verify curl works with HTTPS:
1. Boot image
2. Wait for network to come up
3. Run: curl -v https://example.com
4. Verify TLS handshake succeeds
5. Verify response received

This validates the full stack: networking, DNS, TLS, curl.

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- curl https://example.com returns HTML
- No certificate errors
- TLS 1.2 or 1.3 negotiated

