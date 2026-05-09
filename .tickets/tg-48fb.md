---
id: tg-48fb
status: closed
deps: []
links: []
created: 2026-01-18T15:03:40Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 028: list.h + 1 more (16 functions, 188 lines)

Review C code batch 28 and ensure Go implementation matches exactly.

## C Functions to Review

- `init_list_head` (tinyemu-2019-12-21/list.h:36-40, 5 lines)
- `__list_add` (tinyemu-2019-12-21/list.h:43-50, 8 lines)
- `list_add` (tinyemu-2019-12-21/list.h:53-56, 4 lines)
- `list_add_tail` (tinyemu-2019-12-21/list.h:59-62, 4 lines)
- `list_del` (tinyemu-2019-12-21/list.h:64-73, 10 lines)
- `list_empty` (tinyemu-2019-12-21/list.h:75-78, 4 lines)
- `vm_error` (tinyemu-2019-12-21/machine.c:44-54, 11 lines)
- `vm_get_int` (tinyemu-2019-12-21/machine.c:56-70, 15 lines)
- `vm_get_int_opt` (tinyemu-2019-12-21/machine.c:72-86, 15 lines)
- `vm_get_str2` (tinyemu-2019-12-21/machine.c:88-108, 21 lines)
- `vm_get_str` (tinyemu-2019-12-21/machine.c:110-113, 4 lines)
- `vm_get_str_opt` (tinyemu-2019-12-21/machine.c:115-118, 4 lines)
- `strdup_null` (tinyemu-2019-12-21/machine.c:120-126, 7 lines)
- `cmdline_subst` (tinyemu-2019-12-21/machine.c:129-172, 44 lines)
- `find_name` (tinyemu-2019-12-21/machine.c:174-194, 21 lines)
- `virt_machine_find_class` (tinyemu-2019-12-21/machine.c:211-221, 11 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/list.h:36-221`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/list.h`
- `tinyemu-2019-12-21/machine.c`

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

