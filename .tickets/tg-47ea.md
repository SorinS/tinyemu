---
id: tg-47ea
status: closed
deps: []
links: []
created: 2026-01-17T23:27:00Z
type: task
priority: 2
assignee: JT Olio
---
# Fix P9 VirtQueue Integration Test

The TestP9DeviceVersionNegotiation test in virtio/p9_test.go is skipped due to virtqueue setup issues.

Location: virtio/p9_test.go:370-372

The TODO states: 'Fix this test - there's an issue with the virtqueue setup.'

The test attempts to:
1. Set up virtqueue with descriptor, available, and used ring addresses
2. Build a Tversion message
3. Negotiate the 9P protocol version

The skip message indicates basic unit tests pass, but this integration test that exercises the full virtqueue path fails.

## Files
- virtio/p9_test.go

## Acceptance Criteria

- Test runs without skip
- Virtqueue setup works correctly for P9 device
- Version negotiation completes successfully

