---
id: tinyemu-go-7wz
status: closed
deps: []
links: []
created: 2026-01-16T19:36:00.237055826-05:00
type: feature
priority: 2
---
# Add optional instruction tracing to CPU

Add ability to output per-instruction trace for debugging and comparison with C TinyEMU.

## Purpose
If fixing unmapped memory doesn't fully resolve boot, instruction traces can identify divergence points between Go and C implementations.

## Implementation
Add TraceInstructions bool and TraceWriter io.Writer to CPU struct. In execution loop, optionally output PC, instruction, privilege level.

## Files to Modify
- cpu/cpu.go or cpu/exec.go

Reference: DEBUGGING_PLAN.md Phase 2.2, PROJECT_STATUS_REPORT.md Recommendations


