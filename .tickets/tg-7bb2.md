---
id: tg-7bb2
status: closed
deps: []
links: []
created: 2026-01-18T15:03:41Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 047: simplefb.c + 1 more (7 functions, 178 lines)

Review C code batch 47 and ensure Go implementation matches exactly.

## C Functions to Review

- `simplefb_refresh` (tinyemu-2019-12-21/simplefb.c:48-96, 49 lines)
- `simplefb_refresh1` (tinyemu-2019-12-21/simplefb.c:98-104, 7 lines)
- `simplefb_init` (tinyemu-2019-12-21/simplefb.c:106-127, 22 lines)
- `get_new_addr` (tinyemu-2019-12-21/slirp/bootp.c:39-56, 18 lines)
- `request_addr` (tinyemu-2019-12-21/slirp/bootp.c:58-74, 17 lines)
- `find_addr` (tinyemu-2019-12-21/slirp/bootp.c:76-92, 17 lines)
- `dhcp_decode` (tinyemu-2019-12-21/slirp/bootp.c:94-141, 48 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/simplefb.c:48-141`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/simplefb.c`
- `tinyemu-2019-12-21/slirp/bootp.c`

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

