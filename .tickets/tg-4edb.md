---
id: tg-4edb
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 066: tcp_output.c + 1 more (9 functions, 258 lines)

Review C code batch 66 and ensure Go implementation matches exactly.

## C Functions to Review

- `tcp_setpersist` (tinyemu-2019-12-21/slirp/tcp_output.c:480-492, 13 lines)
- `tcp_init` (tinyemu-2019-12-21/slirp/tcp_subr.c:51-56, 6 lines)
- `tcp_template` (tinyemu-2019-12-21/slirp/tcp_subr.c:65-87, 23 lines)
- `tcp_respond` (tinyemu-2019-12-21/slirp/tcp_subr.c:103-167, 65 lines)
- `tcp_newtcpcb` (tinyemu-2019-12-21/slirp/tcp_subr.c:175-210, 36 lines)
- `tcp_drop` (tinyemu-2019-12-21/slirp/tcp_subr.c:217-228, 12 lines)
- `tcp_close` (tinyemu-2019-12-21/slirp/tcp_subr.c:237-265, 29 lines)
- `tcp_sockclosed` (tinyemu-2019-12-21/slirp/tcp_subr.c:282-308, 27 lines)
- `tcp_fconnect` (tinyemu-2019-12-21/slirp/tcp_subr.c:320-366, 47 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/slirp/tcp_output.c:480-366`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/slirp/tcp_output.c`
- `tinyemu-2019-12-21/slirp/tcp_subr.c`

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

