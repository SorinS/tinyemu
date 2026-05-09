---
id: tg-1caa
status: closed
deps: []
links: []
created: 2026-01-18T15:03:42Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 082: virtio.c (8 functions, 216 lines)

Review C code batch 82 and ensure Go implementation matches exactly.

## C Functions to Review

- `memcpy_to_from_queue` (tinyemu-2019-12-21/virtio.c:380-442, 63 lines)
- `memcpy_from_queue` (tinyemu-2019-12-21/virtio.c:444-450, 7 lines)
- `memcpy_to_queue` (tinyemu-2019-12-21/virtio.c:452-458, 7 lines)
- `virtio_consume_desc` (tinyemu-2019-12-21/virtio.c:461-478, 18 lines)
- `get_desc_rw_size` (tinyemu-2019-12-21/virtio.c:480-515, 36 lines)
- `queue_notify` (tinyemu-2019-12-21/virtio.c:518-544, 27 lines)
- `virtio_config_read` (tinyemu-2019-12-21/virtio.c:546-576, 31 lines)
- `virtio_config_write` (tinyemu-2019-12-21/virtio.c:578-604, 27 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/virtio.c:380-604`
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

Finally, once you are done, close the ticket with `tk`. Create tickets
for any new bugs or issues you discover. Then commit all changes to git.

git add -A && git commit


## Acceptance Criteria

- All functions reviewed against C source
- Go implementations exist with C reference comments
- No behavioral deviations from C
- Tests confirm C-matching behavior
- go test and go vet pass

