---
id: tg-81ad
status: closed
deps: []
links: []
created: 2026-01-18T15:03:40Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 008: fs.c + 1 more (16 functions, 256 lines)

Review C code batch 8 and ensure Go implementation matches exactly.

## C Functions to Review

- `fs_dup` (tinyemu-2019-12-21/fs.c:34-39, 6 lines)
- `fs_walk_path1` (tinyemu-2019-12-21/fs.c:41-93, 53 lines)
- `fs_walk_path` (tinyemu-2019-12-21/fs.c:95-98, 4 lines)
- `fs_end` (tinyemu-2019-12-21/fs.c:100-104, 5 lines)
- `fs_delete` (tinyemu-2019-12-21/fs_disk.c:60-66, 7 lines)
- `fid_create` (tinyemu-2019-12-21/fs_disk.c:69-76, 8 lines)
- `errno_to_p9` (tinyemu-2019-12-21/fs_disk.c:91-101, 11 lines)
- `p9_flags_to_host` (tinyemu-2019-12-21/fs_disk.c:121-131, 11 lines)
- `stat_to_qid` (tinyemu-2019-12-21/fs_disk.c:133-143, 11 lines)
- `fs_statfs` (tinyemu-2019-12-21/fs_disk.c:145-156, 12 lines)
- `compose_path` (tinyemu-2019-12-21/fs_disk.c:158-170, 13 lines)
- `fs_attach` (tinyemu-2019-12-21/fs_disk.c:172-188, 17 lines)
- `fs_walk` (tinyemu-2019-12-21/fs_disk.c:190-210, 21 lines)
- `fs_mkdir` (tinyemu-2019-12-21/fs_disk.c:213-231, 19 lines)
- `fs_open` (tinyemu-2019-12-21/fs_disk.c:233-261, 29 lines)
- `fs_create` (tinyemu-2019-12-21/fs_disk.c:263-291, 29 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/fs.c:34-291`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/fs.c`
- `tinyemu-2019-12-21/fs_disk.c`

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

