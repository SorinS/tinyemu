---
id: tg-99bb
status: closed
deps: []
links: [tg-4666, tg-da69, tg-b1e4, tg-c359, tg-eed5]
created: 2026-01-23T03:04:29Z
type: task
priority: 1
assignee: JT Olio
---
# TCP testing: Debug tracing infrastructure

Add debug tracing to understand why TCP handshake fails in boot tests.

## Context
See docs/1769135871-tcp-integration-test-plan.md for full analysis.
See docs/COMMIT_EXPECTATIONS.md for commit standards.

## Implementation
Add a debug mode for slirp that logs:
- Every packet in/out with hex dump (first 64 bytes)
- TCP state transitions
- Socket creation/destruction

Add tracing to:
- slirp.OutputFunc (packet leaving slirp toward guest)
- slirp.Input (packet from guest into slirp)
- TCPInput/TCPOutput calls
- TCPCtl invocations

## Acceptance Criteria
- Running TestLinuxBootNetworkTCP with tracing enabled shows:
  - SYN packet received from guest
  - SYN-ACK packet generated
  - Whether SYN-ACK reaches VirtIO
  - Whether ACK is received from guest

Reference: slirp/slirp.go, slirp/tcp_input.go, slirp/tcp_output.go

