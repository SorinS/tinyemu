# Curl Hang Investigation - RESOLVED

**Created:** 2026-01-25
**Resolved:** 2026-01-26
**Issue:** Emulator hangs when running `curl` in the buildroot guest image

## Resolution Summary

**Root Cause:** VirtIO block device DMA writes were overwriting the OpenSBI trap handler at physical address `0x800003c8`. The FDT (Flattened Device Tree) advertised the entire RAM region starting at `0x80000000` as available memory, but OpenSBI firmware occupies the first portion of this region.

**Fix:** Added a `reserved-memory` node to the FDT in `machine/fdt.go` that marks the first 2MB at `0x80000000` as `no-map`, preventing Linux from allocating DMA buffers in the OpenSBI region.

```go
// Reserved memory node - marks OpenSBI region as unavailable to Linux
const opensbiReservedSize uint64 = 0x200000 // 2MB
s.beginNode("reserved-memory")
s.propU32("#address-cells", 2)
s.propU32("#size-cells", 2)
s.propEmpty("ranges")

s.beginNodeNum("opensbi", RAMBaseAddr)
s.propU32Tab("reg", ...)
s.propEmpty("no-map")  // Critical: excludes from kernel memory map and DMA
s.endNode()
s.endNode()
```

**Result:** `curl` (with no arguments) now works correctly and displays help text.

## Technical Details

### The Bug

1. OpenSBI firmware (`fw_jump.bin`, ~273KB) is loaded at `0x80000000`
2. The FDT memory node advertised RAM from `0x80000000` with full size
3. No reserved-memory entries existed to protect OpenSBI
4. When `curl` started, the dynamic linker loaded shared libraries
5. Linux allocated DMA buffers at `0x80000000` (the start of "available" RAM)
6. VirtIO block device wrote 64KB of OpenSSL library data to this buffer
7. This overwrote the OpenSBI trap handler at `0x800003c8`
8. Next trap (timer interrupt or syscall) jumped to corrupted code
9. CPU got stuck in exception loop

### Memory Layout

```
0x80000000: OpenSBI firmware (fw_jump.bin)
0x800003c8: OpenSBI trap handler (_trap_handler)
0x80010000: End of corrupted region (64KB write)
0x80200000: Linux kernel (2MB aligned after OpenSBI)
```

### Evidence of Corruption

Before corruption (from fw_jump.bin binary):
```
0x800003d4: 93 d2 b2 00 = 0x00b2d293 (srli t0, t0, 11)
0x800003d8: 93 f2 32 00 = 0x0032f293 (andi t0, t0, 3)
```

After corruption (runtime):
```
0x800003d6: 54 54 50 5f = "TTP_" (ASCII text from "HTTP_" symbol)
```

The "TTP_" bytes were part of OpenSSL symbol names (`OSSL_HPKE_*` functions).

### Why Original TinyEMU Had the Same Bug

The original TinyEMU (C version) has identical FDT generation code with no reserved-memory handling:

```c
// riscv_machine.c line 563
re->address = 0; /* no reserved entry */
re->size = 0;
```

This bug may not have manifested in older buildroot images if they didn't trigger DMA allocations at the exact start of RAM.

## Files Modified

- `machine/fdt.go` - Added reserved-memory node for OpenSBI region

## Related Issues

A separate issue remains: `curl google.com` still hangs. This appears to be a different bug, possibly related to VirtIO network or DNS resolution. See `debug.md` for current investigation status.
