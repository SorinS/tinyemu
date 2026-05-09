---
id: tinyemu-go-m26
status: closed
deps: [tinyemu-go-97i]
links: []
created: 2026-01-16T19:35:06.743065699-05:00
type: bug
priority: 0
---
# Fix unmapped memory read handling

TinyEMU C returns 0 for reads from unmapped addresses, but Go returns an error.

## TinyEMU C Behavior
Reads from unmapped addresses return 0 (no exception).

## TinyEMU Go Behavior (mem/physmem.go)
Returns error for unmapped reads.

## Fix
Change Read8/16/32/64 to return 0 instead of error when no range found.

## Files to Modify
- mem/physmem.go

Reference: DEBUGGING_PLAN.md Phase 1.3


