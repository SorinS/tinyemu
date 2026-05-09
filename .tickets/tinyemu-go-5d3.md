---
id: tinyemu-go-5d3
status: closed
deps: []
links: []
created: 2026-01-15T15:51:06.484945792-05:00
type: task
priority: 1
---
# Phase 3 Integration: Full Linux Distribution Boot

## Completed: Linux Boot Fixed

**Bug Fixed**: Page-crossing instruction fetch was not handled correctly.

When a 32-bit instruction spans a page boundary (first 2 bytes on one page, second 2 bytes
on the next page), the Go implementation was raising a fetch fault instead of properly
fetching both halves like C TinyEMU does.

**Fix**: Added `fetchInstructionPageCrossing()`, `fetchInsn16()`, and `fetchInsn16Phys()`
methods to `cpu/mmu.go` that properly handle this case by translating each half's address
independently and combining them.

Reference: `riscv_cpu_template.h:272-281`, `riscv_cpu.c:570-586`

**TestLinuxBoot** now passes in ~4 seconds:
- Boots to shell prompt (`~ #`)
- Exits early once shell prompt is found

## Current Status

### Completed
- ✅ **VirtIO Block Device**: Boot from disk images works (`TestLinuxBoot` passes)
- ✅ **9P Protocol Implementation**: Complete 9P2000.L protocol support with 40+ message types
- ✅ **VirtIO 9P Device**: VirtIO transport for 9P with mount tag configuration
- ✅ **HostFS Implementation**: Host directory exposure via 9P with path validation

### Fixed: 9P Integration Bug
- ✅ **9P Getattr Mode Field**: Fixed bug where `os.FileMode` was used instead of Unix mode
  - Go's `os.FileMode` uses different bit layout (bit 31 for dir) vs Unix (S_IFDIR = 0x4000)
  - Fix: Use `sys.Mode` from `syscall.Stat_t` in `p9/hostfs.go:infoToStat()`
  - `TestLinuxBoot9PMount` now passes - can mount, ls, and cat files via 9P

Reference: `tinyemu-2019-12-21/fs_disk.c:388-410`

### Remaining Work
- Full Linux distribution support (Debian, Alpine) - now unblocked


