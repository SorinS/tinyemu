---
id: tinyemu-go-je8
status: closed
deps: [tinyemu-go-d9u, tinyemu-go-777]
links: []
created: 2026-01-16T21:27:31.396694395-05:00
type: task
priority: 1
---
# Pass RV64UC Compliance Tests

All rv64uc-p-* (compressed instruction) tests must pass.

The C extension is heavily used by compiled Linux kernels and userspace. We've already fixed one compressed instruction bug (JAL/JALR link address).

Prerequisites: rv64ui tests pass


