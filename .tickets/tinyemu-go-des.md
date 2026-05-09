---
id: tinyemu-go-des
status: closed
deps: [tinyemu-go-1pk, tinyemu-go-m26]
links: []
created: 2026-01-16T15:24:06.845904003-05:00
type: task
priority: 1
---
# Debug Linux Boot: Identify Why Kernel Doesn't Produce Output

The emulator fails at instruction ~46,475 with Store/AMO Access Fault (mcause=0x7) at address 0x10000.

## ROOT CAUSE CONFIRMED
Go returns error for unmapped writes; C silently ignores them. Address 0x10000 is the first byte after low RAM (0x0-0xFFFF).

## Status
Blocked on fixing unmapped memory handling (tinyemu-go-1pk, tinyemu-go-m26).

## Fixed Issues (commit 258c1e6)
- MRET/SRET xPIE handling
- CLINT mtime calculation (instruction-counter based)
- TIME CSR alignment

## Remaining
- Fix unmapped memory write/read handling
- Verify boot progresses past instruction 46,475

Reference: PROJECT_STATUS_REPORT.md, DEBUGGING_PLAN.md


