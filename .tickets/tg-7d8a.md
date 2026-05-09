---
id: tg-7d8a
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 042: riscv_machine.c (23 functions, 300 lines)

Review C code batch 42 and ensure Go implementation matches exactly.

## C Functions to Review

- `rtc_get_real_time` (tinyemu-2019-12-21/riscv_machine.c:82-88, 7 lines)
- `rtc_get_time` (tinyemu-2019-12-21/riscv_machine.c:90-100, 11 lines)
- `htif_read` (tinyemu-2019-12-21/riscv_machine.c:102-127, 26 lines)
- `htif_handle_cmd` (tinyemu-2019-12-21/riscv_machine.c:129-151, 23 lines)
- `htif_write` (tinyemu-2019-12-21/riscv_machine.c:153-177, 25 lines)
- `clint_read` (tinyemu-2019-12-21/riscv_machine.c:195-219, 25 lines)
- `clint_write` (tinyemu-2019-12-21/riscv_machine.c:221-239, 19 lines)
- `plic_update_mip` (tinyemu-2019-12-21/riscv_machine.c:241-251, 11 lines)
- `plic_read` (tinyemu-2019-12-21/riscv_machine.c:256-282, 27 lines)
- `plic_write` (tinyemu-2019-12-21/riscv_machine.c:284-301, 18 lines)
- `plic_set_irq` (tinyemu-2019-12-21/riscv_machine.c:303-314, 12 lines)
- `get_ram_ptr` (tinyemu-2019-12-21/riscv_machine.c:316-319, 4 lines)
- `fdt_init` (tinyemu-2019-12-21/riscv_machine.c:361-366, 6 lines)
- `fdt_alloc_len` (tinyemu-2019-12-21/riscv_machine.c:368-376, 9 lines)
- `fdt_put32` (tinyemu-2019-12-21/riscv_machine.c:378-382, 5 lines)
- `fdt_put_data` (tinyemu-2019-12-21/riscv_machine.c:385-394, 10 lines)
- `fdt_begin_node` (tinyemu-2019-12-21/riscv_machine.c:396-401, 6 lines)
- `fdt_begin_node_num` (tinyemu-2019-12-21/riscv_machine.c:403-408, 6 lines)
- `fdt_end_node` (tinyemu-2019-12-21/riscv_machine.c:410-414, 5 lines)
- `fdt_get_string_offset` (tinyemu-2019-12-21/riscv_machine.c:416-438, 23 lines)
- `fdt_prop` (tinyemu-2019-12-21/riscv_machine.c:440-447, 8 lines)
- `fdt_prop_tab_u32` (tinyemu-2019-12-21/riscv_machine.c:449-458, 10 lines)
- `fdt_prop_u32` (tinyemu-2019-12-21/riscv_machine.c:460-463, 4 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_machine.c:82-463`
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

