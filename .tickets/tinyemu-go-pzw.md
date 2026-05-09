---
id: tinyemu-go-pzw
status: closed
deps: []
links: []
created: 2026-01-16T20:15:09.670216479-05:00
type: bug
priority: 1
---
# Investigate bootloader stuck in loop at PC 0x800010da

RESOLVED: Boot loop was caused by compressed JAL/JALR link address bug.

## Root Cause
C.JALR (compressed jump-and-link) was setting ra = PC+4 instead of PC+2. This caused incorrect return addresses, leading to the infinite memory-fill loop.

## Fix
Modified cpu/exec.go to pass instruction size to execute32(), so JAL/JALR use PC+insnSize for link address.

## Verification
- C.JALR at 0x800010cc now correctly sets ra=0x800010ce
- Boot proceeds past FDT parsing
- Kernel entry at 0x80200000 is reached

## New Issue Found
After this fix, a new issue appears: after exception handling, SRET/MRET returns to PC=0x0, causing illegal instruction loop. This is tracked in a separate ticket.

Commit: cda1715


