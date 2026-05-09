---
id: tinyemu-go-97i
status: closed
deps: []
links: []
created: 2026-01-16T19:35:11.890798323-05:00
type: task
priority: 0
---
# Write unmapped memory access regression test

Before fixing unmapped memory handling, write a test that documents expected behavior.

## Test Cases
1. Write to unmapped address should not return error (match C behavior)
2. Read from unmapped address should return 0 (match C behavior)
3. Write to boundary address 0x10000 (first addr after low RAM) should not fault

## Files to Create/Modify
- mem/physmem_test.go

Reference: DEBUGGING_PLAN.md Phase 1.1


