---
id: tg-def8
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 074: softfp_template.h (5 functions, 171 lines)

Review C code batch 74 and ensure Go implementation matches exactly.

## C Functions to Review

- `glue` (tinyemu-2019-12-21/softfp_template.h:898-939, 42 lines)
- `cvt_sf32_sf` (tinyemu-2019-12-21/softfp_template.h:945-976, 32 lines)
- `glue` (tinyemu-2019-12-21/softfp_template.h:978-1010, 33 lines)
- `cvt_sf64_sf` (tinyemu-2019-12-21/softfp_template.h:1016-1046, 31 lines)
- `glue` (tinyemu-2019-12-21/softfp_template.h:1048-1080, 33 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/softfp_template.h:898-1080`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/softfp_template.h`

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

**2026-01-24T18:55:18Z**

VERIFIED - All functions implemented in softfp package

Function mapping (C → Go):
- fclass_sf (glue, C:898-939) → FClass32, FClass64, FClass128 in pack.go and f128.go
- cvt_sf32_sf (C:945-976) → CvtF32ToF64 in convert.go:463-494
- cvt_sfN_sf32 (glue, C:978-1010) → CvtF64ToF32 in convert.go:496-537  
- cvt_sf64_sf (C:1016-1046) → CvtF64ToF128 in convert.go:539-572
- cvt_sfN_sf64 (glue, C:1048-1080) → CvtF128ToF64 in convert.go:574-614

All functions have C reference comments. All softfp tests pass.
