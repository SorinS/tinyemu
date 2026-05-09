---
id: tinyemu-go-1pk
status: closed
deps: [tinyemu-go-97i]
links: []
created: 2026-01-16T19:35:01.598518188-05:00
type: bug
priority: 0
---
# Fix unmapped memory write handling

TinyEMU C silently ignores writes to unmapped addresses, but Go returns an error causing Store Access Fault. This is the ROOT CAUSE of boot failure at instruction ~46,475.

## TinyEMU C Behavior (riscv_cpu.c:462-468)
Writes to unmapped addresses are silently ignored (no exception).

## TinyEMU Go Behavior (mem/physmem.go:395-398)
Returns error for unmapped writes, causing CPU to raise Store Access Fault.

## Fix
Change Write8/16/32/64 to return nil instead of error when no range found.

## Files to Modify
- mem/physmem.go

Reference: PROJECT_STATUS_REPORT.md, DEBUGGING_PLAN.md Phase 1.2


