---
id: tg-a05a
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 026: json.c (17 functions, 285 lines)

Review C code batch 26 and ensure Go implementation matches exactly.

## C Functions to Review

- `parse_string` (tinyemu-2019-12-21/json.c:40-94, 55 lines)
- `json_object_get2` (tinyemu-2019-12-21/json.c:96-106, 11 lines)
- `json_object_get` (tinyemu-2019-12-21/json.c:108-120, 13 lines)
- `json_object_set` (tinyemu-2019-12-21/json.c:122-146, 25 lines)
- `json_array_get` (tinyemu-2019-12-21/json.c:148-160, 13 lines)
- `json_array_set` (tinyemu-2019-12-21/json.c:162-184, 23 lines)
- `json_get_str` (tinyemu-2019-12-21/json.c:186-191, 6 lines)
- `json_get_error` (tinyemu-2019-12-21/json.c:193-198, 6 lines)
- `json_string_new2` (tinyemu-2019-12-21/json.c:200-211, 12 lines)
- `json_string_new` (tinyemu-2019-12-21/json.c:213-216, 4 lines)
- `json_error_new` (tinyemu-2019-12-21/json.c:218-230, 13 lines)
- `json_object_new` (tinyemu-2019-12-21/json.c:232-240, 9 lines)
- `json_array_new` (tinyemu-2019-12-21/json.c:242-250, 9 lines)
- `json_free` (tinyemu-2019-12-21/json.c:252-292, 41 lines)
- `skip_spaces` (tinyemu-2019-12-21/json.c:294-316, 23 lines)
- `is_ident_first` (tinyemu-2019-12-21/json.c:318-323, 6 lines)
- `parse_ident` (tinyemu-2019-12-21/json.c:325-340, 16 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/json.c:40-340`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/json.c`

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

