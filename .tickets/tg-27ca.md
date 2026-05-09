---
id: tg-27ca
status: closed
deps: []
links: []
created: 2026-01-18T15:03:40Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 025: iomem.h + 1 more (19 functions, 281 lines)

Review C code batch 25 and ensure Go implementation matches exactly.

## C Functions to Review

- `cpu_register_ram` (tinyemu-2019-12-21/iomem.h:87-91, 5 lines)
- `phys_mem_get_dirty_bits` (tinyemu-2019-12-21/iomem.h:99-103, 5 lines)
- `phys_mem_set_dirty_bit` (tinyemu-2019-12-21/iomem.h:105-115, 11 lines)
- `phys_mem_is_dirty_bit` (tinyemu-2019-12-21/iomem.h:117-126, 10 lines)
- `set_irq` (tinyemu-2019-12-21/iomem.h:143-146, 4 lines)
- `console_read` (tinyemu-2019-12-21/jsemu.c:64-80, 17 lines)
- `console_queue_char` (tinyemu-2019-12-21/jsemu.c:83-91, 9 lines)
- `display_key_event` (tinyemu-2019-12-21/jsemu.c:94-99, 6 lines)
- `display_mouse_event` (tinyemu-2019-12-21/jsemu.c:104-122, 19 lines)
- `display_wheel_event` (tinyemu-2019-12-21/jsemu.c:125-131, 7 lines)
- `net_write_packet` (tinyemu-2019-12-21/jsemu.c:134-140, 7 lines)
- `net_set_carrier` (tinyemu-2019-12-21/jsemu.c:143-151, 9 lines)
- `fb_refresh1` (tinyemu-2019-12-21/jsemu.c:153-159, 7 lines)
- `console_init` (tinyemu-2019-12-21/jsemu.c:161-169, 9 lines)
- `vm_start` (tinyemu-2019-12-21/jsemu.c:183-199, 17 lines)
- `init_vm_fs` (tinyemu-2019-12-21/jsemu.c:201-216, 16 lines)
- `init_vm_drive` (tinyemu-2019-12-21/jsemu.c:218-232, 15 lines)
- `init_vm` (tinyemu-2019-12-21/jsemu.c:234-297, 64 lines)
- `virt_machine_run` (tinyemu-2019-12-21/jsemu.c:305-348, 44 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/iomem.h:87-348`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/iomem.h`
- `tinyemu-2019-12-21/jsemu.c`

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

