---
id: tinyemu-go-eal
status: closed
deps: []
links: []
created: 2026-01-15T15:52:16.096376641-05:00
type: task
priority: 1
---
# RISC-V Compliance Test Suite Integration

Build riscv-tests from source (rv64ui, rv64um, rv64ua, rv64uf, rv64ud, rv64uc), generate golden signatures using Spike, embed binaries in testdata/riscv-tests/isa/. Tests use signature comparison against reference. Run via go test ./...


