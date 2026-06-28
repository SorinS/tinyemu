# Building NuttX for temu

How the NuttX guest images are built and run on temu. Two targets:

| Script | Board | Status |
|--------|-------|--------|
| `run_nuttx-riscv.sh` | `rv-virt` (riscv64) | **interactive NSH** |
| `run_nuttx-x64.sh`   | `qemu-intel64` (x86_64) | builds + boots via multiboot2 to the CPU-capability gate (needs x2APIC — see below) |

NuttX is **not distributed as a binary** — it is built from source (Apache NuttX
+ the matching `nuttx-apps`). The repos used here:

- `~/Dev/CPP/nuttx.git`  — apache/nuttx (master)
- `~/Dev/CPP/apps`       — apache/nuttx-apps (master), cloned alongside

## x86_64 (qemu-intel64)

### Host prerequisites (macOS arm64)

```sh
brew install x86_64-elf-gcc x86_64-elf-binutils   # bottled, the cross toolchain
brew install cmake ninja                          # build system
python3 -m venv /tmp/nxvenv                        # kconfiglib (NuttX's CMake
/tmp/nxvenv/bin/pip install kconfiglib             #   config uses olddefconfig)
```

`kconfig-frontends` is **not** in Homebrew; the CMake build path uses `kconfiglib`
(the pip package provides `olddefconfig`, `defconfig`, etc.). Put the venv's bin
on PATH for the configure/build.

### Configure + build

```sh
export PATH=/tmp/nxvenv/bin:/opt/homebrew/bin:$PATH
cd ~/Dev/CPP/nuttx.git
cmake -B /tmp/nxbuild -DBOARD_CONFIG=qemu-intel64:nsh -GNinja \
  -DCMAKE_C_COMPILER=/opt/homebrew/bin/x86_64-elf-gcc \
  -DCMAKE_CXX_COMPILER=/opt/homebrew/bin/x86_64-elf-g++ \
  -DCMAKE_ASM_COMPILER=/opt/homebrew/bin/x86_64-elf-gcc \
  -DCMAKE_AR=/opt/homebrew/bin/x86_64-elf-ar \
  -DCMAKE_ASM_FLAGS="-Wa,--divide" -DCMAKE_C_FLAGS="-Wa,--divide" \
  .
ninja -C /tmp/nxbuild
cp /tmp/nxbuild/nuttx <repo>/bin/nuttx-x64/nuttx     # stage for run_nuttx-x64.sh
```

The NuttX CMake toolchain file assumes the host compiler *is* the x86_64 compiler
(true on Linux/x86_64), so on macOS arm64 the cross-compiler must be forced with
`-DCMAKE_C_COMPILER=...` etc.

### Two NuttX-master fixes needed on this toolchain

1. **`-Wa,--divide`** — NuttX's x86_64 register-offset macros expand to displacements
   containing `/` (e.g. `(8*(... / 8))(%rdi)`). i386 GAS treats `/` as a comment
   character unless given `--divide`, so without it every offset is an "unbalanced
   parenthesis" assembler error. Passed via `CMAKE_ASM_FLAGS`/`CMAKE_C_FLAGS`.
2. **`romfs_stub.c` missing from CMake** — `boards/x86_64/qemu/qemu-intel64/src/
   CMakeLists.txt` omits `romfs_stub.c`, so the weak `romfs_img` fallback isn't
   linked and `board_late_initialize` fails with `undefined reference to romfs_img`.
   Add `romfs_stub.c` to the `set(SRCS ...)` list. (Upstream CMake-vs-Make gap.)

### How temu boots it

NuttX qemu-intel64 is a **multiboot2** ELF (header magic `0xE85250D6` at file
offset 0x1000; the `.multiboot1` section is a 0-byte marker). temu's PC board loads
it via `machine/pc/multiboot64.go` (added for this): it loads the PT_LOAD segments
by physical address and enters 32-bit protected mode with `EAX=0x36D76289`,
`EBX`=multiboot2 info struct, then NuttX brings up long mode itself.

### Current wall: CPU capability gate

`x86_64_check_and_enable_capability` requires CPUID.1.ECX bits and `cli; hlt`s if
any are missing. temu advertises RDRAND but not **PCID(17) / x2APIC(21) /
TSC-Deadline(24) / XSAVE(26)**. In NuttX's `intel64_check_capability.c` only
**x2APIC is unconditional**; PCID / TSC-Deadline / XSAVE are `#ifdef`-gated and can
be turned off in the board config (NuttX then uses fxsave, which temu has, and the
periodic LAPIC timer). So reaching NSH needs an **x2APIC MSR interface** on temu's
existing (xAPIC/MMIO) LAPIC — NuttX talks to the APIC only via x2APIC MSRs (EOI on
every interrupt, ICR for IPIs, the timer). Debug knob: `TINYEMU_X64_ITRACE=1`.

## riscv64 (rv-virt)

Built the same way (cross toolchain `riscv-none-elf-` / `riscv64-unknown-elf-`,
`qemu-rv-virt:nsh` style config). temu boots the resulting ELF directly via its
riscv ELF bios path; see `run_nuttx-riscv.sh` and the project memory for the temu
fixes (16550 UART, PLIC alias, UART IRQ) that got it to an interactive shell.
