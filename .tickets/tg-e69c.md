---
id: tg-e69c
status: closed
deps: [tg-de42]
links: []
created: 2026-01-25T03:39:36Z
type: task
priority: 1
assignee: JT Olio
parent: tg-4094
---
# Test curl HTTPS in Ubuntu image

Verify curl works with HTTPS in Ubuntu:
1. Boot image
2. Wait for network to come up
3. Run: curl -v https://example.com
4. Verify TLS handshake succeeds
5. Verify response received

Reference: docs/image-build-plan-1769311563.md

## Acceptance Criteria

- curl https://example.com returns HTML
- No certificate errors
- TLS negotiated successfully


## Notes

**2026-01-25T04:43:31Z**

Dropped - Ubuntu RISC-V images too large (722MB cloud image vs 40MB target). No minimal prebuilt option available.
