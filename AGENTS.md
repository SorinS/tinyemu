# tinyemu-go — Agent Memory

## Project Overview

**tinyemu-go** is a Go reimplementation of Fabrice Bellard's TinyEMU. It started as a RISC-V-only emulator and is being extended to multi-arch support (RISC-V + x86-32 PC emulation).

- **Module**: `github.com/sorins/tinyemu-go`
- **Root**: `/Users/sorins/Dev/Go.Code/tinyemu-go.git`
- **Reference emulator**: `halfix.git` at `/Users/sorins/Dev/Go.Code/halfix.git` (C-based x86-32 PC emulator)

## Philosophy

This emulator is intended to be a **serious tool**, not a toy. Design goals:
- Accurate enough to boot real operating systems (Linux, DOS, etc.)
- Clean, well-tested Go code
- Standard PC-compatible behavior (where practical)
- All code must have unit/integration tests

## Architecture

### Generic Interfaces

```
cpu.Core        — generic CPU interface (Run, Step, Reset, etc.)
machine.Board   — generic machine interface (Run, LoadBIOS, GetCPU, etc.)
```

### Factory Pattern

`machine.NewBoard(machineType, cfg)` dispatches to architecture-specific constructors:
- `"riscv64"` → `machine.New(cfg)` (RISC-V virt machine)
- `"riscv32"` → `machine.New(cfg)` with MaxXLEN=32
- `"x86"` → `machine/pc.New(pcCfg)` (x86-32 PC)

### Directory Layout

```
cmd/temu/           — main emulator binary
cpu/                — CPU interfaces and RISC-V core
cpu/x86/            — x86-32 CPU core (~3400 lines)
machine/            — machine interfaces and RISC-V virt board
machine/pc/         — x86 PC board (PIC, PIT, RTC, I/O ports, UART, keyboard)
mem/                — physical memory map, MMIO, IRQ signals
virtio/             — VirtIO devices (console, block, net, 9P)
slirp/              — SLIRP networking stack
temubox/            — high-level emulator box management
```

## x86 CPU Core (`cpu/x86`)

### Implemented (~170 opcodes)

**Registers**: Full 32-bit register file with 8/16/32-bit union accessors (AL/AH/AX/EAX, etc.)

**Prefixes**: 0xF3 REP, 0xF2 REPNZ, 0x66 operand-size, 0x67 address-size, segment overrides (0x2E CS, 0x36 SS, 0x3E DS, 0x26 ES, 0x64 FS, 0x65 GS)

**Data Movement**: MOV (reg-reg, reg-mem, reg-imm, mem-imm, Sreg), PUSH, POP, LEA, LES, LDS, MOVSX, MOVZX, BSWAP, XCHG

**Arithmetic**: ADD, ADC, SUB, SBB, CMP, INC, DEC, NEG, NOT, MUL, IMUL, DIV, IDIV

**Logic**: AND, OR, XOR, TEST

**Shifts/Rotates**: SHL/SAL, SHR, SAR, ROL, ROR, RCL, RCR, SHLD, SHRD

**Control Flow**: JMP (near/far), CALL (near/far), RET (near/far), conditional JCC (all 16 conditions), LOOP/LOOPE/LOOPNE, JCXZ

**String Ops**: MOVS, STOS, LODS, CMPS, SCAS with full REP/REPNZ support, respects DF flag

**Stack**: ENTER, LEAVE, PUSHAD/POPAD, PUSHA/POPA, PUSHF/POPF

**System**: LGDT, LIDT, LMSW, MOV CRn, CPUID, RDTSC, HLT, CLI, STI, NOP

**ModR/M**: Full 16-bit and 32-bit addressing mode parser including SIB byte

**Group handlers**: Group1 (80-83), Group2 (D0-D3/C0-C1), Group3 (F6-F7), Group4-5 (FE-FF)

**Paging**: 32-bit two-level page walk (PDE→PTE), 4KB and 4MB PSE pages, CR0.WP support, Accessed/Dirty bits, #PF with error code

**Interrupts**: Real-mode IVT and protected-mode IDT gate handling (32-bit interrupt/trap gates), hardware interrupt delivery from PIC, `INT imm8`, `IRET`

### Not Yet Implemented

- **Segment overrides in ModR/M**: memory accesses in `cpu/x86` hardcoded to `DS` base; prefix scanner captures `segOverride` but `executeOpcode` ignores it
- **Protected mode advanced**: segment descriptor loading (LAR, LSL, ARPL), task switching, TSS
- **TLB**: page table walk happens on every memory access (correct but slow)
- **Privilege-level stack switch on interrupts**: not implemented (deferred; Linux kernel handlers run in ring 0)
- **FPU/MMX/SSE**: all floating-point and SIMD instructions
- **Most 0F extended opcodes**: beyond the basics (CPUID, RDTSC, BSWAP, MOVSX, MOVZX, SHLD, SHRD, CMPSX, LAR, LSL)
- **Segment limits and attributes**: currently only base addresses are tracked
- **A20 gate wraparound**: A20 mask is set to 0xFFFFFFFF (always enabled)
- **CR3 TLB flush on write**: not implemented

### Fixed During Session

- **Operand-size override in real mode**: `0xB8-0xBF`, `0xA0-0xA3`, PUSH/POP all respect `operandSize` correctly. The AGENTS.md issue was outdated.
- **`SetSegLimit` accessor**: Added to `cpu/x86/cpu.go` so the bzImage loader can set segment limits.

## x86 PC Board (`machine/pc`)

### Implemented

- **Memory map**: Low RAM 0x00000-0x9FFFF, VGA hole 0xA0000-0xBFFFF, BIOS ROM 0xC0000-0xFFFFF + alias at 0xFFF00000
- **BIOS ROM**: 64KB, `LoadBIOS()` copies BIOS image to end of ROM (aligning with reset vector at 0xFFFF0)
- **8259 PIC**: Dual 8259 cascade, IRR/ISR/IMR, EOI, priority rotation
- **8254 PIT**: 3 channels, modes 0-5, channel 0 wired to PIC IRQ0
- **CMOS/RTC**: 128-byte NVRAM, ports 0x70/0x71, returns memory size in BIOS-standard locations
- **I/O port dispatcher**: Flat 64K port array, `RegisterIOPort()`, `In8/16/32()`, `Out8/16/32()`
- **I/O port wiring**: CPU `ioRead8`/`ioWrite8` callbacks registered in `PC.New()` to call into I/O dispatcher
- **UART 16550**: Minimal COM1 implementation (ports 0x3F8-0x3FF) for `console=ttyS0` output
- **Keyboard controller (8042)**: Minimal stub (ports 0x60/0x64) responding to self-test (0xAA→0x55) and interface test (0xAB→0x00)
- **bzImage direct loader**: Parses setup header, loads setup sectors to `0x90000`, protected-mode kernel to `0x100000`, populates zero page with boot params (`type_of_loader`, `cmd_line_ptr`, `ramdisk_image`/`ramdisk_size`), sets flat GDT/IDT segments, and jumps to 32-bit entry point

### Not Yet Implemented

- **VGA/graphics**: not implemented
- **PCI/ISA bus**: not implemented
- **Floppy/HDD controller**: not implemented
- **Serial/parallel ports**: only UART 16550 COM1
- **APIC/IOAPIC**: not implemented (8259 only)

## Build & Test Status

```bash
# Build everything
go build ./...

# x86 CPU unit tests — ALL PASS
go test ./cpu/x86 -v

# x86 PC board tests — ALL PASS
go test ./machine/pc -v -timeout=30s

# p9 package — ALL PASS (HostFS now works on macOS)
go test ./p9 -v

# virtio package — ALL PASS (HostFS tests now pass on macOS)
go test ./virtio -v

# RISC-V tests — ALL PASS
go test ./cpu/... ./machine/...

# Full suite with nasm in PATH — ALL PASS except cmd/temu (pre-existing)
export PATH="/opt/homebrew/bin:$PATH"
go test ./cpu/... ./machine/... ./mem/... ./devices/... ./slirp/... ./virtio/... ./test/x86
```

### Known Pre-existing Failures

- `cmd/temu` tests: `TestLoadConfigInvalidMachine` and `TestBuildConfigFromCLI` fail because x86 machine types were added to config validation but test expectations weren't updated.

## Active Work: x86 Linux Boot

**Goal**: Boot a Linux bzImage with `console=ttyS0` using direct 32-bit entry point (bypassing real-mode setup).

**Approach**:
1. **bzImage direct loader** (in progress): Parse bzImage setup header, load setup sectors to `0x90000`, protected-mode kernel to `0x100000`, populate zero page with boot params (`type_of_loader`, `cmd_line_ptr`, `ramdisk_image`/`ramdisk_size`), set registers (`EAX=0x53726448`, `EBX=zero_page_ptr`), jump to 32-bit entry point.
2. **UART 16550** (`machine/pc/uart.go` created, needs wiring in `PC.New()`): Minimal COM1 for `console=ttyS0` output.
3. **8042 keyboard stub**: Ports `0x60`/`0x64` so Linux doesn't hang probing keyboard controller.
4. **Initrd support**: Load initrd into high RAM and set `ramdisk_image`/`ramdisk_size` in zero page.

**What we don't need for direct 32-bit boot**:
- Real-mode fixes, segment overrides, VGA, BIOS `int 0x10`, A20 gate handling

**Blockers for BIOS-based boot (SeaBIOS, etc.)**:
- Segment overrides not wired (ModR/M memory accesses hardcoded to DS)
- VGA text mode / BIOS int 0x10

## How to Run

```bash
# RISC-V
cd bin && ./run.sh ../testdata/boot/root-riscv64.cfg

# x86 (once boot is working)
# ./run.sh config.json with "machine": "x86"
```

## Recent Changes

- Phase 1: Multi-arch refactoring (interfaces, factory, directory reorganization)
- Phase 2: x86-32 CPU core (~120 opcodes + expansion)
- Phase 2b: More instructions (DIV, IDIV, IMUL, ADC, SBB, CMP, string ops, system ops)
- Phase 3a: PC board with PIC, PIT, RTC, I/O dispatcher
- Phase 3b: PIC → CPU interrupt delivery (`SetINTR`, interrupt ack handler, `checkInterrupts` in `Step()`)
- Phase 3c: Protected-mode IDT gates (`handleInterrupt` with real-mode IVT and protected-mode gate descriptor parsing)
- Phase 3d: Paging core (`translateAddress`, `raisePageFault`, `readPhys*`/`writePhys*`, `CR0_PG` + `CR4_PSE`)
- Phase 3e: HostFS on macOS (`p9/hostfs_darwin.go` with `syscall.UtimesNano`, `Atimespec` accessors, `NAME_MAX`)
- Phase 4a: x86 Linux boot infrastructure
  - UART 16550 COM1 (`machine/pc/uart.go`) for `console=ttyS0`
  - 8042 keyboard stub (`machine/pc/kbd.go`) for ports 0x60/0x64
  - bzImage direct loader (`machine/pc/bzimage.go`) with zero page setup and 32-bit entry point
  - `SetSegLimit` accessor added to `cpu/x86`
  - `cmd/temu` updated to keep kernelData separate for x86 bzImage boot

## Next Steps (Roadmap)

### Immediate (Critical for Linux Boot)
1. ~~Finish UART 16550 wiring in `PC.New()`~~ DONE
2. ~~Implement 8042 keyboard controller stub (`machine/pc/kbd.go`)~~ DONE
3. ~~Implement bzImage direct loader in `LoadBIOS()`~~ DONE
4. **Test boot with a real bzImage + initrd** — NEXT STEP
5. Fix any instruction-level issues that arise during real kernel boot

### Short Term (Real BIOS Boot)
5. Wire segment overrides through memory access functions
6. Implement minimal VGA text mode (port 0x3B0-0x3DF, memory 0xB8000)
7. Implement A20 gate behavior

### Medium Term (OS Boot)
8. ATA/IDE controller for disk boot
9. Protected mode advanced: segment limits, LAR/LSL/ARPL
10. APIC/IOAPIC for modern OS support
11. PCI bus enumeration stub

### Long Term (Serious Tool)
12. FPU (x87) emulation
13. MMX/SSE support
14. SMP/multi-core support
15. Save/restore state (snapshots)
16. Performance optimization (JIT or threaded interpreter)

## Configuration

JSON config format (strict JSON — no comments, no trailing commas):
```json
{
  "machine": "x86",
  "memory_size": 536870912,
  "bios": "bios.bin",
  "kernel": "vmlinuz",
  "cmdline": "console=ttyS0"
}
```

Valid machine types: `riscv64`, `riscv32`, `x86`, `x86_64` (x86_64 currently aliases to x86 stub).

## Development Environment

- **Go**: 1.26.2 at `/opt/homebrew/bin/go` (also available via `go` when `/opt/homebrew/bin` is in PATH)
- **nasm**: 3.x at `/opt/homebrew/bin/nasm` (required for `test/x86` assembly tests)
