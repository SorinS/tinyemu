---
id: tg-ac79
status: closed
deps: []
links: []
created: 2026-01-17T16:45:54Z
type: task
priority: 2
assignee: JT Olio
---
# Add unit tests for RISC-V Sv39 virtual memory translation.

## Background
Linux boot uses Sv39 paging (satp mode=8). While boot reaches kernel entry with MMU enabled, we have no isolated tests for page table walking and address translation.

## Tests Needed
1. Page table walk for valid mappings
2. Page fault for unmapped addresses
3. Permission checking (R/W/X bits)
4. User vs supervisor page access
5. Megapage (2MB) and gigapage (1GB) support
6. TLB flush (sfence.vma) behavior
7. SATP register handling

## Implementation
- Create test page tables in memory
- Verify translations produce correct physical addresses
- Verify faults are raised for invalid access

## Acceptance Criteria
- All Sv39 translation cases are tested
- Page fault handling is verified
- mem/ or cpu/ coverage increases

## References
- RISC-V Privileged Spec (Sv39 section)
- cpu/mmu.go or equivalent
- tinyemu-2019-12-21/riscv_cpu.c (MMU code)

