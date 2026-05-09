---
id: tg-3fd7
status: closed
deps: []
links: []
created: 2026-01-17T22:20:34Z
type: task
priority: 1
assignee: JT Olio
---
# Userspace Execution Through MMU Test

Add test that validates the M->S->U privilege transition with MMU enabled.

The Linux boot fails when running init in userspace. This test should exercise that specific path:

1. Set up Sv39 page tables with U bit for user pages
2. Write simple test code (e.g., ECALL instruction) to user page  
3. Execute SRET from S-mode to U-mode at user page address
4. Verify instruction fetch works through MMU with U-mode permissions
5. Verify ECALL traps back to S-mode correctly

This matches what C TinyEMU does when Linux runs init. The goal is to find where Go behavior diverges from C.

