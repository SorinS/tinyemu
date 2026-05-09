---
id: tg-8e3d
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 043: riscv_machine.c (7 functions, 280 lines)

Review C code batch 43 and ensure Go implementation matches exactly.

## C Functions to Review

- `fdt_prop_tab_u64` (tinyemu-2019-12-21/riscv_machine.c:465-472, 8 lines)
- `fdt_prop_tab_u64_2` (tinyemu-2019-12-21/riscv_machine.c:474-483, 10 lines)
- `fdt_prop_str` (tinyemu-2019-12-21/riscv_machine.c:485-489, 5 lines)
- `fdt_prop_tab_str` (tinyemu-2019-12-21/riscv_machine.c:492-525, 34 lines)
- `fdt_output` (tinyemu-2019-12-21/riscv_machine.c:528-578, 51 lines)
- `fdt_end` (tinyemu-2019-12-21/riscv_machine.c:580-585, 6 lines)
- `riscv_build_fdt` (tinyemu-2019-12-21/riscv_machine.c:587-752, 166 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_machine.c:465-752`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/riscv_machine.c`

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

