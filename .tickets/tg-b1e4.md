---
id: tg-b1e4
status: closed
deps: []
links: [tg-4666, tg-da69, tg-99bb, tg-c359, tg-eed5]
created: 2026-01-23T03:04:36Z
type: task
priority: 1
assignee: JT Olio
---
# TCP testing: Complete handshake unit test

Create a unit test that completes the full TCP three-way handshake.

## Context
See docs/1769135871-tcp-integration-test-plan.md for full analysis.
See docs/COMMIT_EXPECTATIONS.md for commit standards.

The current TestIntegrationTCPExecForwarding only sends SYN and verifies SYN-ACK.
We need to complete the handshake and verify data transfer at the unit level.

## Implementation
Extend integration_test.go with TestIntegrationTCPFullHandshake:
1. Send SYN packet
2. Capture and verify SYN-ACK
3. Send ACK to complete handshake
4. Verify socket reaches TCPSEstablished state
5. Send data packet
6. Verify data appears in so.SoRcv buffer
7. Send response via SocketRecv
8. Verify response packet generated

## Acceptance Criteria
- Test passes with full handshake completion
- If test fails, we know the issue is in TCP state machine (not VirtIO/emulator)
- If test passes, we know the issue is in VirtIO/emulator integration

Reference: slirp/integration_test.go, slirp/tcp_input.go

