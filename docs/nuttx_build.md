# Building NuttX for temu

How the NuttX guest images are built and run on temu. Two targets:

| Script | Board | Status |
|--------|-------|--------|
| `run_nuttx-riscv.sh` | `rv-virt` (riscv64) | **interactive NSH** |
| `run_nuttx-x64.sh`   | `qemu-intel64` (x86_64) | **interactive NSH** (multiboot2 + x2APIC; see below) |
| `run_nuttx-arm64.sh` | `qemu-armv8a` (arm64) | **interactive NSH** (no temu changes) |

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

### The five walls to NSH (all fixed)

NuttX boots multiboot2 → page-table init → long mode → kernel init → NSH. Each
wall was a "guest expects more than temu provides", found by tracing to the
faulting RIP (`TINYEMU_X64_ITRACE=1`; mode 2 = PM32, 4 = long64) and reading the
NuttX source assertion:

1. **multiboot2 loader** — `machine/pc/multiboot64.go`.
2. **CPU capability gate** — `x86_64_check_and_enable_capability` `cli;hlt`s
   unless CPUID.1.ECX has its required bits. Only **x2APIC(21) is unconditional**
   (PCID/XSAVE/TSC-deadline are `#ifdef`-gated → turned off in the config; temu
   has fxsave). Implemented **x2APIC** (MSR front-end over temu's LocalAPIC) +
   **TSC-deadline** timer + **FSGSBASE**; `run_nuttx-x64.sh` passes `-apic`.
3. **FSGSBASE** — `F3 0F AE /0-3` (RD/WR FS/GS base), added to opGroup15.
4. **ACPI** — `x86_64_cpu_init` reads the ACPI MADT (`acpi_lapic_get`) and PANICs
   if absent → `loadMultiboot64` now calls `installACPIDirect`.
5. **RAM size** — NuttX hard-codes `CONFIG_RAM_SIZE=256MiB`; with less, heap
   writes hit unbacked memory and corrupt the free list → `run_nuttx-x64.sh`
   defaults to `MEM=256`.

## arm64 (qemu-armv8a)

```sh
brew install aarch64-elf-gcc aarch64-elf-binutils      # bottled cross toolchain
# NuttX's gcc.cmake hard-codes the aarch64-none-elf- prefix; symlink it:
mkdir -p /tmp/arm-tc
for t in gcc g++ ld ar as objcopy objdump nm strip ranlib gcc-ar cpp; do
  ln -sf /opt/homebrew/bin/aarch64-elf-$t /tmp/arm-tc/aarch64-none-elf-$t
done
export PATH=/tmp/arm-tc:/tmp/nxvenv/bin:/opt/homebrew/bin:$PATH
# The nsh_gicv2 defconfig defaults to Clang (wants libclang_rt); use GNU instead:
sed -i '' 's/^CONFIG_ARM64_TOOLCHAIN_CLANG=y/CONFIG_ARM64_TOOLCHAIN_GNU_EABI=y/' \
  boards/arm64/qemu/qemu-armv8a/configs/nsh_gicv2/defconfig
cmake -B /tmp/nxbuild-arm -DBOARD_CONFIG=qemu-armv8a:nsh_gicv2 -GNinja .
ninja -C /tmp/nxbuild-arm
cp /tmp/nxbuild-arm/nuttx <repo>/bin/nuttx-arm64/nuttx
```

`nsh_gicv2` (not plain `nsh`) matches temu's GICv2. temu ELF-loads the kernel
(PT_LOAD by paddr, PC=entry 0x40280000, X0=DTB) — **no emulator changes needed**;
its PL011/GICv2/timer/PSCI already match qemu-armv8a.

## riscv64 (rv-virt)

Built the same way (cross toolchain `riscv-none-elf-` / `riscv64-unknown-elf-`,
`qemu-rv-virt:nsh` style config). temu boots the resulting ELF directly via its
riscv ELF bios path; see `run_nuttx-riscv.sh` and the project memory for the temu
fixes (16550 UART, PLIC alias, UART IRQ) that got it to an interactive shell.
