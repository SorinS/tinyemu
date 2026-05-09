---
id: tinyemu-go-d9u
status: closed
deps: [tinyemu-go-fkp]
links: []
created: 2026-01-16T21:27:05.47939048-05:00
type: task
priority: 0
---
# Pass RV64UI Base Integer Compliance Tests

All rv64ui-p-* tests must pass (base RV64I integer instructions).

This is the minimum bar before attempting Linux boot. If any rv64ui test fails, there's an instruction-level bug that will break boot.

Expected: 53 tests (rv64ui-p-add, rv64ui-p-addi, etc.)

After this passes, proceed to rv64um and rv64ua tests.


