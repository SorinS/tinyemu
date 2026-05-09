---
id: tg-1cae
status: closed
deps: []
links: []
created: 2026-01-22T13:20:44Z
type: task
priority: 3
assignee: JT Olio
---
# Implement TCP checksum verification

In tcp_input.go:263, TCP checksum verification is skipped (marked as 'simplified - just accept for now'). The C slirp code verifies TCP checksums. While the guest OS also verifies checksums, implementing this provides defense-in-depth against malformed packets from the real network.

