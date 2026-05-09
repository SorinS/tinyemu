---
id: tg-5d1f
status: closed
deps: []
links: []
created: 2026-01-18T15:03:41Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 044: riscv_machine.c (10 functions, 278 lines)

Review C code batch 44 and ensure Go implementation matches exactly.

## C Functions to Review

- `copy_bios` (tinyemu-2019-12-21/riscv_machine.c:754-816, 63 lines)
- `riscv_flush_tlb_write_range` (tinyemu-2019-12-21/riscv_machine.c:818-823, 6 lines)
- `riscv_machine_set_defaults` (tinyemu-2019-12-21/riscv_machine.c:825-827, 3 lines)
- `riscv_machine_init` (tinyemu-2019-12-21/riscv_machine.c:829-978, 150 lines)
- `riscv_machine_end` (tinyemu-2019-12-21/riscv_machine.c:980-987, 8 lines)
- `riscv_machine_get_sleep_duration` (tinyemu-2019-12-21/riscv_machine.c:990-1012, 23 lines)
- `riscv_machine_interp` (tinyemu-2019-12-21/riscv_machine.c:1014-1018, 5 lines)
- `riscv_vm_send_key_event` (tinyemu-2019-12-21/riscv_machine.c:1020-1027, 8 lines)
- `riscv_vm_mouse_is_absolute` (tinyemu-2019-12-21/riscv_machine.c:1029-1032, 4 lines)
- `riscv_vm_send_mouse_event` (tinyemu-2019-12-21/riscv_machine.c:1034-1041, 8 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/riscv_machine.c:754-1041`
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

Finally, once you are done, close the ticket with `tk`. Create tickets
for any new bugs or issues you discover. Then commit all changes to git.

git add -A && git commit


## Acceptance Criteria

- All functions reviewed against C source
- Go implementations exist with C reference comments
- No behavioral deviations from C
- Tests confirm C-matching behavior
- go test and go vet pass

