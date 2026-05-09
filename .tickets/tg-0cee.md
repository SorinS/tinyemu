---
id: tg-0cee
status: closed
deps: []
links: []
created: 2026-01-18T15:03:42Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 079: vga.c (6 functions, 227 lines)

Review C code batch 79 and ensure Go implementation matches exactly.

## C Functions to Review

- `vga_draw_glyph8` (tinyemu-2019-12-21/vga.c:122-142, 21 lines)
- `vga_draw_glyph9` (tinyemu-2019-12-21/vga.c:144-170, 27 lines)
- `c6_to_8` (tinyemu-2019-12-21/vga.c:179-185, 7 lines)
- `update_palette16` (tinyemu-2019-12-21/vga.c:187-209, 23 lines)
- `vga_text_refresh` (tinyemu-2019-12-21/vga.c:213-346, 134 lines)
- `vga_refresh` (tinyemu-2019-12-21/vga.c:348-362, 15 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/vga.c:122-362`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/vga.c`

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

