---
id: tinyemu-go-7za
status: closed
deps: [tinyemu-go-cqd, tinyemu-go-8cs]
links: []
created: 2026-01-16T21:27:26.345719554-05:00
type: task
priority: 1
---
# Pass RV64UF and RV64UD Compliance Tests

All rv64uf-p-* (single-precision float) and rv64ud-p-* (double-precision float) tests must pass.

These test the F and D extensions which are used by many Linux userspace programs.

Prerequisites: rv64ui, rv64um, rv64ua tests pass


