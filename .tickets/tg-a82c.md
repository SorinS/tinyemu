---
id: tg-a82c
status: closed
deps: []
links: []
created: 2026-01-18T15:03:41Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 063: tcp_input.c (1 functions, 1072 lines)

Please see docs/1768939013-tcp-input-full-implementation-plan.md for context.

Review C code batch 63 and ensure Go implementation matches exactly.

## C Functions to Review

- `tcp_input` (tinyemu-2019-12-21/slirp/tcp_input.c:216-1287, 1072 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/slirp/tcp_input.c:216-1287`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/slirp/tcp_input.c`

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


## Notes

**2026-01-20T20:03:38Z**

Simplified implementation exists. Full implementation broken into subtasks - see docs/1768939013-tcp-input-full-implementation-plan.md and tickets: tg-d9c0, tg-7831, tg-82b7, tg-28b7, tg-82e4, tg-6cb4, tg-4682, tg-6385, tg-93f2, tg-390f
