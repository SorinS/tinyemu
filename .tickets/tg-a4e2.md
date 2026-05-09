---
id: tg-a4e2
status: closed
deps: []
links: []
created: 2026-01-18T15:03:41Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 060: socket.c (8 functions, 282 lines)

Review C code batch 60 and ensure Go implementation matches exactly.

## C Functions to Review

- `solookup` (tinyemu-2019-12-21/slirp/socket.c:15-32, 18 lines)
- `socreate` (tinyemu-2019-12-21/slirp/socket.c:40-52, 13 lines)
- `sofree` (tinyemu-2019-12-21/slirp/socket.c:58-77, 20 lines)
- `sopreprbuf` (tinyemu-2019-12-21/slirp/socket.c:79-136, 58 lines)
- `soread` (tinyemu-2019-12-21/slirp/socket.c:144-202, 59 lines)
- `soreadbuf` (tinyemu-2019-12-21/slirp/socket.c:204-244, 41 lines)
- `sorecvoob` (tinyemu-2019-12-21/slirp/socket.c:254-274, 21 lines)
- `sosendoob` (tinyemu-2019-12-21/slirp/socket.c:281-332, 52 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/slirp/socket.c:15-332`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/slirp/socket.c`

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

