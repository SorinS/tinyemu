---
id: tg-f0e9
status: closed
deps: []
links: []
created: 2026-01-18T15:03:40Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 031: pci.c (17 functions, 298 lines)

Review C code batch 31 and ensure Go implementation matches exactly.

## C Functions to Review

- `pci_write_bar` (tinyemu-2019-12-21/pci.c:256-278, 23 lines)
- `pci_device_config_write8` (tinyemu-2019-12-21/pci.c:280-338, 59 lines)
- `pci_device_config_write` (tinyemu-2019-12-21/pci.c:341-367, 27 lines)
- `pci_data_write` (tinyemu-2019-12-21/pci.c:370-385, 16 lines)
- `pci_data_read` (tinyemu-2019-12-21/pci.c:389-403, 15 lines)
- `pci_device_get_dma_ptr` (tinyemu-2019-12-21/pci.c:407-410, 4 lines)
- `pci_device_set_config8` (tinyemu-2019-12-21/pci.c:412-415, 4 lines)
- `pci_device_set_config16` (tinyemu-2019-12-21/pci.c:417-420, 4 lines)
- `pci_device_get_devfn` (tinyemu-2019-12-21/pci.c:422-425, 4 lines)
- `pci_add_capability` (tinyemu-2019-12-21/pci.c:428-441, 14 lines)
- `i440fx_write_addr` (tinyemu-2019-12-21/pci.c:454-459, 6 lines)
- `i440fx_read_addr` (tinyemu-2019-12-21/pci.c:461-465, 5 lines)
- `i440fx_write_data` (tinyemu-2019-12-21/pci.c:467-480, 14 lines)
- `i440fx_read_data` (tinyemu-2019-12-21/pci.c:482-494, 13 lines)
- `i440fx_set_irq` (tinyemu-2019-12-21/pci.c:496-513, 18 lines)
- `i440fx_init` (tinyemu-2019-12-21/pci.c:515-554, 40 lines)
- `i440fx_map_interrupts` (tinyemu-2019-12-21/pci.c:557-588, 32 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/pci.c:256-588`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/pci.c`

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

**2026-01-22T03:36:51Z**

Skipping: pci.c is x86-only code. The i440FX and PIIX3 are Intel x86 chipsets. RISC-V machine uses MMIO-based VirtIO instead of PCI (riscv_machine.c has zero PCI references). Per docs/tinyemu-porting-plan.md, x86/KVM emulation is a non-goal for initial release.
