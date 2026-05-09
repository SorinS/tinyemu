---
id: tg-d7ab
status: closed
deps: []
links: []
created: 2026-01-23T18:25:50Z
type: task
priority: 2
assignee: JT Olio
tags: [c-review, port-verification]
---
# Review batch 030: machine.c + 2 more (22 functions, 293 lines)

Review C code batch 30 and ensure Go implementation matches exactly.

## C Functions to Review

- `config_additional_file_load` (tinyemu-2019-12-21/machine.c:544-564, 21 lines)
- `config_additional_file_load_cb` (tinyemu-2019-12-21/machine.c:566-579, 14 lines)
- `vm_add_cmdline` (tinyemu-2019-12-21/machine.c:581-597, 17 lines)
- `virt_machine_free_config` (tinyemu-2019-12-21/machine.c:599-624, 26 lines)
- `virt_machine_init` (tinyemu-2019-12-21/machine.c:626-630, 5 lines)
- `virt_machine_set_defaults` (tinyemu-2019-12-21/machine.c:632-635, 4 lines)
- `virt_machine_end` (tinyemu-2019-12-21/machine.c:637-640, 4 lines)
- `virt_machine_get_sleep_duration` (tinyemu-2019-12-21/machine.h:151-154, 4 lines)
- `virt_machine_interp` (tinyemu-2019-12-21/machine.h:155-158, 4 lines)
- `vm_mouse_is_absolute` (tinyemu-2019-12-21/machine.h:159-162, 4 lines)
- `vm_send_mouse_event` (tinyemu-2019-12-21/machine.h:163-167, 5 lines)
- `vm_send_key_event` (tinyemu-2019-12-21/machine.h:168-171, 4 lines)
- `bus_map_irq` (tinyemu-2019-12-21/pci.c:63-68, 6 lines)
- `pci_device_set_irq` (tinyemu-2019-12-21/pci.c:70-91, 22 lines)
- `devfn_alloc` (tinyemu-2019-12-21/pci.c:93-101, 9 lines)
- `pci_register_device` (tinyemu-2019-12-21/pci.c:104-136, 33 lines)
- `pci_device_get_irq` (tinyemu-2019-12-21/pci.c:138-142, 5 lines)
- `pci_device_config_read` (tinyemu-2019-12-21/pci.c:144-171, 28 lines)
- `pci_device_get_mem_map` (tinyemu-2019-12-21/pci.c:173-176, 4 lines)
- `pci_device_get_port_map` (tinyemu-2019-12-21/pci.c:178-181, 4 lines)
- `pci_register_bar` (tinyemu-2019-12-21/pci.c:183-209, 27 lines)
- `pci_update_mappings` (tinyemu-2019-12-21/pci.c:211-253, 43 lines)

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
   - Add/update comments referencing C code: `// Reference: tinyemu-2019-12-21/machine.c:544-253`
   - Fix ANY deviations including error handling differences
6. **Write tests** to confirm behavior matches C code

## Critical Reminders

- Match C behavior exactly - even "improved" error handling can break Linux boot
- The C code works. Our Go code doesn't boot Linux. Any deviation is suspect.

## Files

- `tinyemu-2019-12-21/machine.c`
- `tinyemu-2019-12-21/machine.h`
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

Finally, once you are done, close the ticket with `tk` and commit any changes 
you have made.


## Acceptance Criteria

- All functions reviewed against C source
- Go implementations exist with C reference comments
- No behavioral deviations from C
- Tests confirm C-matching behavior
- go test and go vet pass


## Notes

**2026-01-24T18:58:01Z**

VERIFIED/SKIPPED

machine.c functions:
- config_additional_file_load, config_additional_file_load_cb: SKIPPED - HTTP config loading (HTTP support skipped)
- vm_add_cmdline: Handled in cmd/temu/config.go
- virt_machine_free_config: Not needed (Go GC)
- virt_machine_init: → NewMachine in machine/machine.go
- virt_machine_set_defaults: Handled in NewMachine defaults
- virt_machine_end: → Machine.Close in machine/machine.go

machine.h inline functions:
- virt_machine_get_sleep_duration: → Machine.GetSleepDuration in machine/machine.go:434
- virt_machine_interp: → Machine.Run in machine/machine.go:426
- vm_mouse_is_absolute: SKIPPED - Graphics/mouse support not ported
- vm_send_mouse_event: SKIPPED - Graphics/mouse support not ported
- vm_send_key_event: SKIPPED - Graphics/keyboard support not ported

pci.c (all functions):
- SKIPPED - RISC-V machines use VirtIO-MMIO transport, not PCI
- PCI is only used for x86 machines which are explicitly skipped

All machine tests pass.
