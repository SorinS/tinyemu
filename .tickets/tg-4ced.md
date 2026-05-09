---
id: tg-4ced
status: closed
deps: []
links: []
created: 2026-01-17T16:45:54Z
type: task
priority: 2
assignee: JT Olio
---
# Add tests for supervisor and machine mode instruction behavior.

## Background
Current compliance tests only cover user-mode instructions (rv64ui, rv64um, rv64ua, rv64uc, rv64uf, rv64ud). Privilege-level behavior is untested.

## Tests Needed

### Machine Mode (rv64mi)
- CSR access permissions
- Trap handling (mcause, mepc, mtval)
- Interrupt enable/disable (mie, mip)
- Exception delegation setup

### Supervisor Mode (rv64si)  
- Trap delegation (medeleg, mideleg)
- S-mode CSR access (sstatus, sie, sip, etc.)
- Timer interrupt delegation (MTIP -> STIP)
- Page fault handling setup

## Options
1. Build rv64si-p-* and rv64mi-p-* from riscv-tests source
2. Write custom Go tests for privilege behavior
3. Both

## Acceptance Criteria
- Trap delegation is tested
- M-mode -> S-mode transitions are tested
- CSR access at different privilege levels is tested

## References
- RISC-V Privileged Spec
- tinyemu-2019-12-21/riscv_cpu.c (trap handling)
- cpu/csr.go

