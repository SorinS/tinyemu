---
id: tg-1cd9
status: closed
deps: []
links: []
created: 2026-01-18T15:03:40Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 011: fs_net.c (20 functions, 291 lines)

Review C code batch 11 and ensure Go implementation matches exactly.

## C Functions to Review

- `inode_search_path` (tinyemu-2019-12-21/fs_net.c:523-529, 7 lines)
- `is_empty_dir` (tinyemu-2019-12-21/fs_net.c:531-543, 13 lines)
- `inode_dirent_delete_no_decref` (tinyemu-2019-12-21/fs_net.c:545-558, 14 lines)
- `inode_dirent_delete` (tinyemu-2019-12-21/fs_net.c:560-566, 7 lines)
- `flush_dir` (tinyemu-2019-12-21/fs_net.c:568-577, 10 lines)
- `fs_delete` (tinyemu-2019-12-21/fs_net.c:579-584, 6 lines)
- `fid_create` (tinyemu-2019-12-21/fs_net.c:586-594, 9 lines)
- `inode_to_qid` (tinyemu-2019-12-21/fs_net.c:596-606, 11 lines)
- `fs_statfs` (tinyemu-2019-12-21/fs_net.c:608-619, 12 lines)
- `fs_attach` (tinyemu-2019-12-21/fs_net.c:621-629, 9 lines)
- `fs_walk` (tinyemu-2019-12-21/fs_net.c:631-648, 18 lines)
- `fs_mkdir` (tinyemu-2019-12-21/fs_net.c:650-666, 17 lines)
- `fs_trim_cache` (tinyemu-2019-12-21/fs_net.c:670-697, 28 lines)
- `fs_open_end` (tinyemu-2019-12-21/fs_net.c:699-707, 9 lines)
- `fs_open_write_cb` (tinyemu-2019-12-21/fs_net.c:709-722, 14 lines)
- `fs_wget_set_loaded` (tinyemu-2019-12-21/fs_net.c:724-745, 22 lines)
- `fs_wget_set_error` (tinyemu-2019-12-21/fs_net.c:747-758, 12 lines)
- `fs_read_archive` (tinyemu-2019-12-21/fs_net.c:760-786, 27 lines)
- `fs_error_archive` (tinyemu-2019-12-21/fs_net.c:788-797, 10 lines)
- `fs_open_cb` (tinyemu-2019-12-21/fs_net.c:799-834, 36 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/fs_net.c:523-834`
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

Finally, once you are done, close the ticket with `tk`. Create tickets
for any new bugs or issues you discover. Then commit all changes to git.

git add -A && git commit


## Acceptance Criteria

- All functions reviewed against C source
- Go implementations exist with C reference comments
- No behavioral deviations from C
- Tests confirm C-matching behavior
- go test and go vet pass

