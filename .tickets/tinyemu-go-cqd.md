---
id: tinyemu-go-cqd
status: closed
deps: [tinyemu-go-d9u]
links: []
created: 2026-01-16T21:27:15.074885645-05:00
type: task
priority: 0
---
# Pass RV64UM and RV64UA Compliance Tests

All rv64um-p-* (multiply/divide) and rv64ua-p-* (atomics) tests must pass.

These extensions are required for Linux boot. The kernel uses atomics extensively for SMP synchronization.

Prerequisites: rv64ui tests pass


