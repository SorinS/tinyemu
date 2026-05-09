---
id: tg-dc2c
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 088: virtio.c (1 functions, 581 lines)

Review C code batch 88 and ensure Go implementation matches exactly.

## C Functions to Review

- `virtio_9p_recv_request` (tinyemu-2019-12-21/virtio.c:2042-2622, 581 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/virtio.c:2042-2622`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/virtio.c`

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


## Notes

**2026-01-23T18:33:13Z**

This large ticket (581 lines) has been broken down into 6 smaller sub-tickets for easier review:

1. **tg-71d2**: Setup, header parsing, statfs (lines 2042-2103)
2. **tg-3c22**: lopen, lcreate, symlink, mknod, readlink (lines 2104-2223)
3. **tg-0f95**: getattr, setattr, xattrwalk, readdir, fsync (lines 2224-2317)
4. **tg-0f94**: lock, getlock, link, mkdir, renameat, unlinkat (lines 2318-2454)
5. **tg-474b**: version, attach, flush, walk (lines 2455-2542)
6. **tg-cae9**: read, write, clunk, error handling (lines 2543-2622)

Complete all sub-tickets to fulfill this parent ticket.

**2026-01-24T18:56:53Z**

VERIFIED - All sub-tickets completed

This parent ticket was broken into 6 sub-tickets, all now closed:
- tg-71d2 [closed]: setup, header parsing, statfs
- tg-3c22 [closed]: lopen, lcreate, symlink, mknod, readlink
- tg-0f95 [closed]: getattr, setattr, xattrwalk, readdir, fsync
- tg-0f94 [closed]: lock, getlock, link, mkdir, renameat, unlinkat
- tg-474b [closed]: version, attach, flush, walk
- tg-cae9 [closed]: read, write, clunk, error handling

Full virtio_9p_recv_request function verified against C code.
All virtio tests pass.
