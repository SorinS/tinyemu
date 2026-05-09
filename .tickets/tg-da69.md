---
id: tg-da69
status: closed
deps: []
links: [tg-4666, tg-99bb, tg-b1e4, tg-c359, tg-eed5]
created: 2026-01-23T03:04:59Z
type: task
priority: 2
assignee: JT Olio
---
# TCP testing: VirtIO network packet tracing

Add packet tracing to VirtIO network device to debug packet delivery.

## Context
See docs/1769135871-tcp-integration-test-plan.md for full analysis.
See docs/COMMIT_EXPECTATIONS.md for commit standards.

If unit tests pass but boot tests fail, the issue is likely in VirtIO/emulator
integration. This ticket adds tracing to understand packet flow.

## Implementation
Add tracing to virtio/net.go to log when packets are:
- Received from guest (transmit queue processing)
- Delivered to guest (receive queue)
- IRQ delivered for received packets
- Queue notifications processed

## Acceptance Criteria
- Can trace a packet from slirp.OutputFunc through to guest receipt
- Can identify if packets are queued but not delivered
- Can identify IRQ delivery timing issues

Reference: virtio/net.go, virtio/virtio.go


## Notes

**2026-01-24T21:18:40Z**

Already implemented via virtio.DebugNetRX flag in virtio/net.go. This tracing was used successfully to debug TCP issues in the parent epic. The tracing shows:
- Packet writes and their sizes
- Available ring indices and descriptor details
- Packet contents in hex
- Used ring verification
