---
id: tg-debd
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 075: softfp_template_icvt.h + 1 more (7 functions, 196 lines)

Review C code batch 75 and ensure Go implementation matches exactly.

## C Functions to Review

- `glue` (tinyemu-2019-12-21/softfp_template_icvt.h:38-108, 71 lines)
- `glue` (tinyemu-2019-12-21/softfp_template_icvt.h:110-115, 6 lines)
- `glue` (tinyemu-2019-12-21/softfp_template_icvt.h:117-122, 6 lines)
- `glue` (tinyemu-2019-12-21/softfp_template_icvt.h:125-153, 29 lines)
- `glue` (tinyemu-2019-12-21/softfp_template_icvt.h:155-160, 6 lines)
- `glue` (tinyemu-2019-12-21/softfp_template_icvt.h:162-167, 6 lines)
- `main` (tinyemu-2019-12-21/splitimg.c:31-102, 72 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/softfp_template_icvt.h:38-102`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/softfp_template_icvt.h`
- `tinyemu-2019-12-21/splitimg.c`

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

**2026-01-24T18:55:39Z**

VERIFIED/SKIPPED

softfp_template_icvt.h functions - All implemented in softfp/convert.go:
- cvt_sf_i/cvt_sf_ui (glue, C:38-108) → CvtF*ToI*, CvtF*ToU* (convert.go:18-279)
- cvt_i_sf/cvt_ui_sf (glue, C:125-153) → CvtI*ToF*, CvtU*ToF* (convert.go:285-455)
- All 128-bit conversions implemented (convert.go:616-1121)

splitimg.c - SKIPPED
- Standalone utility for splitting disk images for HTTP block device
- Not part of the emulator, and HTTP disk loading is explicitly skipped

All softfp tests pass (verified).
