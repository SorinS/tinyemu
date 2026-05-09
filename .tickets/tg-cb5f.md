---
id: tg-cb5f
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 094: x86_machine.c (11 functions, 260 lines)

Review C code batch 94 and ensure Go implementation matches exactly.

## C Functions to Review

- `ld_port` (tinyemu-2019-12-21/x86_machine.c:1476-1502, 27 lines)
- `pc_machine_set_defaults` (tinyemu-2019-12-21/x86_machine.c:1504-1507, 4 lines)
- `sigalrm_handler` (tinyemu-2019-12-21/x86_machine.c:1511-1513, 3 lines)
- `kvm_set_cpuid` (tinyemu-2019-12-21/x86_machine.c:1518-1546, 29 lines)
- `kvm_map_ram` (tinyemu-2019-12-21/x86_machine.c:1549-1573, 25 lines)
- `kvm_register_ram` (tinyemu-2019-12-21/x86_machine.c:1576-1599, 24 lines)
- `kvm_set_ram_addr` (tinyemu-2019-12-21/x86_machine.c:1601-1619, 19 lines)
- `kvm_get_dirty_bits` (tinyemu-2019-12-21/x86_machine.c:1621-1639, 19 lines)
- `kvm_free_ram` (tinyemu-2019-12-21/x86_machine.c:1641-1646, 6 lines)
- `kvm_pic_set_irq` (tinyemu-2019-12-21/x86_machine.c:1648-1658, 11 lines)
- `kvm_init` (tinyemu-2019-12-21/x86_machine.c:1660-1752, 93 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/x86_machine.c:1476-1752`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/x86_machine.c`

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

