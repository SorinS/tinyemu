---
id: tg-0b7b
status: closed
deps: []
links: []
created: 2026-01-17T22:22:15Z
type: task
priority: 2
assignee: JT Olio
---
# Privileged Mode Transition Tests (rv64si/rv64mi style)

Add tests matching the rv64si (supervisor) and rv64mi (machine mode) compliance test style.

The riscv-tests suite includes privileged mode tests that C TinyEMU passes. Our Go port should pass equivalent tests:

- CSR access restrictions by privilege level
- Trap handling edge cases across privilege modes
- Timer interrupt handling and delegation
- Page fault handling in S-mode and U-mode

These tests validate behavior the C code handles correctly. The goal is to match C TinyEMU, not to add new functionality.

Note: We already have good trap_test.go coverage. This ticket is about ensuring we cover the same edge cases as rv64si/rv64mi.

