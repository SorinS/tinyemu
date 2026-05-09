---
id: tinyemu-go-fkp
status: closed
deps: [tinyemu-go-b1w]
links: []
created: 2026-01-16T21:26:54.907021423-05:00
type: task
priority: 0
---
# Implement Compliance Test Runner

Implement a test runner in cpu/compliance_test.go that:

1. Loads ELF test binaries from testdata/riscv-tests/isa/
2. Sets up minimal bare-metal machine (RAM + CPU, no devices)
3. Runs until tohost write or instruction timeout
4. Extracts pass/fail from tohost value  
5. Optionally compares signature memory region against reference

Should work with standard riscv-tests binaries (rv64ui-p-*, etc).

Run via: go test -v -run TestRISCVCompliance ./cpu/

Reference: docs/TESTING_STRATEGY.md for design details


