---
id: tg-ba28
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 039: riscv_cpu.c (13 functions, 150 lines)

Review C code batch 39 and ensure Go implementation matches exactly.

## C Functions to Review

- `raise_interrupt` (tinyemu-2019-12-21/riscv_cpu.c:1186-1197, 12 lines)
- `sext` (tinyemu-2019-12-21/riscv_cpu.c:1199-1202, 4 lines)
- `get_field1` (tinyemu-2019-12-21/riscv_cpu.c:1204-1214, 11 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1229-1258, 30 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1261-1264, 4 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1266-1272, 7 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1274-1277, 4 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1279-1282, 4 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1284-1287, 4 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1289-1321, 33 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1323-1328, 6 lines)
- `glue` (tinyemu-2019-12-21/riscv_cpu.c:1330-1333, 4 lines)
- `riscv_cpu_init` (tinyemu-2019-12-21/riscv_cpu.c:1349-1375, 27 lines)

## Instructions

1. **Read the C code carefully** for each function listed above
2. If the C code is **only** for x86 emulation or /dev/kvm support, you can
   skip that function and move on. We are also skipping graphics such as
   framebuffer support. Everything else (including the network stack) we are
   porting. Please ask if you have uncertainty or this seems unclear regarding
   any function at all.
3. **Find the corresponding Go code** in the appropriate package
4. **If Go code doesn't exist:**
   - Write the Go implementation matching C behavior exactly
   - Write tests following docs/COMMIT_EXPECTATIONS.md
   - Target 80%+ test coverage.
5. **If Go code exists:**
   - Compare line-by-line for exact behavioral match
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_cpu.c:1186-1375`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/riscv_cpu.c`

## Acceptance Criteria

- [ ] All functions reviewed against C source
- [ ] Go implementations exist for all functions (or documented as intentionally skipped)
- [ ] Comments reference C code with file:line format
- [ ] No behavioral deviations from C (especially error handling)
- [ ] Tests written confirming C-matching behavior
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes

## Last notes 

Finally, once you are done, close the ticket with `tk` and commit any changes 
you have made.


## Acceptance Criteria

- All functions reviewed against C source
- Go implementations exist with C reference comments
- No behavioral deviations from C
- Tests confirm C-matching behavior
- go test and go vet pass


## Notes

**2026-01-24T18:51:34Z**

VERIFIED - All functions implemented correctly in cpu/ package

Function mapping (C → Go):
- raise_interrupt (C:1186-1197) → checkInterrupts + raiseInterrupt in exec.go:115-152
- sext (C:1199-1202) → sextC in compressed.go:512-514 (exact algorithm)
- get_field1 (C:1204-1214) → getField1 in compressed.go:505-508 (equivalent algorithm)
- glue(riscv_cpu_interp, MAX_XLEN) (C:1229-1258) → Run in exec.go:83-100
- glue(riscv_cpu_get_cycles, MAX_XLEN) (C:1261-1264) → GetCycles in cpu.go:213-215
- glue(riscv_cpu_set_mip, MAX_XLEN) (C:1266-1272) → SetMIP in cpu.go:221-227
- glue(riscv_cpu_reset_mip, MAX_XLEN) (C:1274-1277) → ResetMIP in cpu.go:231-233
- glue(riscv_cpu_get_mip, MAX_XLEN) (C:1279-1282) → GetMIP in cpu.go:237-239
- glue(riscv_cpu_get_power_down, MAX_XLEN) (C:1284-1287) → IsPowerDown in cpu.go:249-251
- glue(riscv_cpu_init, MAX_XLEN) (C:1289-1321) → NewCPU in cpu.go:117-158
- glue(riscv_cpu_end, MAX_XLEN) (C:1323-1328) → Not needed (Go GC)
- glue(riscv_cpu_get_misa, MAX_XLEN) (C:1330-1333) → GetMISA in cpu.go:242-244
- riscv_cpu_init (C:1349-1375) → NewCPU with maxXLEN param

All CPU tests pass (go test ./cpu/... -short)
