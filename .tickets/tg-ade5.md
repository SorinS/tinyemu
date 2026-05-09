---
id: tg-ade5
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 012: fs_net.c (7 functions, 259 lines)

Review C code batch 12 and ensure Go implementation matches exactly.

## C Functions to Review

- `fs_open_wget` (tinyemu-2019-12-21/fs_net.c:837-869, 33 lines)
- `fs_preload_file` (tinyemu-2019-12-21/fs_net.c:872-883, 12 lines)
- `find_preload_archive` (tinyemu-2019-12-21/fs_net.c:885-896, 12 lines)
- `fs_preload_archive` (tinyemu-2019-12-21/fs_net.c:898-980, 83 lines)
- `fs_preload_files` (tinyemu-2019-12-21/fs_net.c:982-1003, 22 lines)
- `fs_open` (tinyemu-2019-12-21/fs_net.c:1008-1080, 73 lines)
- `fs_create` (tinyemu-2019-12-21/fs_net.c:1082-1105, 24 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/fs_net.c:837-1105`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/fs_net.c`

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

