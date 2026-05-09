---
id: tg-9d9c
status: closed
deps: []
links: []
created: 2026-01-18T15:03:42Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 092: x86_machine.c (10 functions, 250 lines)

Review C code batch 92 and ensure Go implementation matches exactly.

## C Functions to Review

- `pit_get_count` (tinyemu-2019-12-21/x86_machine.c:764-782, 19 lines)
- `pit_get_out` (tinyemu-2019-12-21/x86_machine.c:785-815, 31 lines)
- `pit_load_count` (tinyemu-2019-12-21/x86_machine.c:817-824, 8 lines)
- `pit_write` (tinyemu-2019-12-21/x86_machine.c:826-874, 49 lines)
- `pit_read` (tinyemu-2019-12-21/x86_machine.c:876-914, 39 lines)
- `speaker_write` (tinyemu-2019-12-21/x86_machine.c:916-922, 7 lines)
- `speaker_read` (tinyemu-2019-12-21/x86_machine.c:924-937, 14 lines)
- `pit_update_irq` (tinyemu-2019-12-21/x86_machine.c:941-981, 41 lines)
- `serial_init` (tinyemu-2019-12-21/x86_machine.c:1036-1062, 27 lines)
- `serial_update_irq` (tinyemu-2019-12-21/x86_machine.c:1064-1078, 15 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/x86_machine.c:764-1078`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/x86_machine.c`

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

