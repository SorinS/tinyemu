---
id: tg-aa0d
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 004: block_net.c (12 functions, 275 lines)

Review C code batch 4 and ensure Go implementation matches exactly.

## C Functions to Review

- `bf_find_block` (tinyemu-2019-12-21/block_net.c:117-134, 18 lines)
- `bf_free_block` (tinyemu-2019-12-21/block_net.c:136-142, 7 lines)
- `bf_add_block` (tinyemu-2019-12-21/block_net.c:144-168, 25 lines)
- `bf_get_sector_count` (tinyemu-2019-12-21/block_net.c:170-174, 5 lines)
- `bf_start_load_block` (tinyemu-2019-12-21/block_net.c:176-198, 23 lines)
- `bf_start_load_prefetch_group` (tinyemu-2019-12-21/block_net.c:200-237, 38 lines)
- `bf_prefetch_group_onload` (tinyemu-2019-12-21/block_net.c:239-260, 22 lines)
- `bf_rw_async1` (tinyemu-2019-12-21/block_net.c:262-339, 78 lines)
- `bf_update_block` (tinyemu-2019-12-21/block_net.c:341-354, 14 lines)
- `bf_read_onload` (tinyemu-2019-12-21/block_net.c:356-368, 13 lines)
- `bf_read_async` (tinyemu-2019-12-21/block_net.c:370-385, 16 lines)
- `bf_write_async` (tinyemu-2019-12-21/block_net.c:387-402, 16 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/block_net.c:117-402`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/block_net.c`

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

