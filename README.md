# tinyemu-go

A multi-ISA system emulator written in pure Go — RISC-V, x86, x86-64 and AArch64
(ARM64) CPUs, with device models, firmware support and user-mode networking, all
in one dependency-light module.

It began life as a Go transliteration of Fabrice Bellard's
[TinyEMU](https://bellard.org/tinyemu/) RISC-V emulator and has grown into a
broader emulation toolkit: three more CPU cores, a PC machine that boots real
firmware and Linux distributions, NASM-style and ARM64 assemblers/disassemblers,
and an assembly language server.

## What it can do

- **RISC-V (RV32/RV64)** — the original TinyEMU core; boots Linux on the `virt`
  board (VirtIO MMIO, PLIC, CLINT, generated device tree).
- **x86 (32-bit)** — boots Alpine Linux to an interactive busybox shell (and `vi`).
- **x86-64 (long mode)** — a PC machine with SeaBIOS, the 8259 PIC / optional
  local APIC, PIT, RTC/CMOS, IDE/ATA, VGA, `fw_cfg`/ACPI and VirtIO. Boots
  Alpine x86-64, runs UEFI firmware (OVMF), and hosts UEFI payloads — including
  Go programs compiled with [TamaGo](https://github.com/usbarmory/tamago),
  BareMetal-OS via Pure64, and OSv.
- **AArch64 (ARM64)** — a complete instruction set: integer core, MMU and
  exception handling, scalar floating point, and a broad slice of NEON/Advanced
  SIMD (arithmetic, compares, shifts, reductions, permutes, table/lane moves,
  by-element, pairwise, load/store and int↔FP conversions). Every instruction is
  validated byte-exact against `llvm-mc` and, for execution, against a real Apple
  Silicon CPU. Usable today through the assembler/disassembler and the
  `temu -run-asm` runner; a bootable ARM64 machine (GIC, PL011, timer, PSCI) is
  the next milestone.
- **Networking** — a from-scratch user-mode TCP/IP stack (`slirp`), with host
  port forwarding, so guests can reach the network without privileges.

## Repository layout

| Path | What's there |
|---|---|
| `cpu/riscv`, `cpu/x86`, `cpu/x86_64`, `cpu/arm64` | the four CPU cores |
| `machine/` | the RISC-V `virt` board; `machine/pc` is the x86/x86-64 PC machine |
| `devices/` | CLINT, PLIC, HTIF (RISC-V platform devices) |
| `virtio/` | VirtIO MMIO transport + block / net / console / 9P devices |
| `slirp/` | user-mode TCP/IP networking |
| `softfp/`, `mem/`, `p9/` | soft float, physical memory map, 9P filesystem |
| `asm/` | NASM-style x86/x86-64 assembler + disassembler; `asm/arm64`, `asm/riscv`; `asm/emu` execution backend |
| `lsp/` | an assembly language server with live inline register state |
| `cmd/temu` | the emulator CLI |
| `cmd/disasm` | a multi-ISA, objdump-style disassembler |
| `cmd/vmlinux-step` | a vmlinux single-stepper for kernel bring-up |
| `temubox/` | embeddable emulator API (see `temubox/example`) |

## Quick start

```sh
make build                 # builds bin/temu.<os>-<arch>.bin

# Run a short program in any ISA and print the final registers (ISA auto-detected):
echo 'mov rax, 5; add rax, 37' | bin/temu.*.bin -run-asm - -cpu-arch x86_64
echo 'add x0, x1, x2'          | bin/temu.*.bin -run-asm - -cpu-arch arm64

# Boot a machine (riscv64 | riscv32 | x86 | x86_64):
bin/temu.*.bin -machine x86_64 -m 512 -bios bin/seabios/bios.bin -drive disk.img
```

The embeddable entry point is [`temubox/example/main.go`](temubox/example/main.go).
See [`docs/USAGE.md`](docs/USAGE.md) for the full CLI.

### `temu` flags (selected)

`-machine` (riscv64/riscv32/x86/x86_64) · `-m` RAM MB · `-bios` · `-kernel` ·
`-initrd` · `-drive` (+`-rw`/`-ro`) · `-append` · `-net-user` ·
`-net-hostfwd tcp:8080:80` · `-apic` · `-run-asm` (+`-cpu-arch`, `-asm-steps`).

## Boot assets and run scripts

Boot images and firmware are *built* with `make` targets and *run* with the
`run_*.sh` scripts (the scripts never build — they boot prebuilt artifacts):

```sh
make seabios ovmf          # firmware
make baremetal             # Pure64 + BareMetal-OS kernel
make tamago TAMAGO_SRC=app.go   # compile a Go program into a UEFI image
./run_riscv64.sh           # RISC-V Linux
./run_baremetal.sh         # BareMetal-OS under SeaBIOS
./run_tamago.sh            # boot a TamaGo Go program under OVMF
./run_osv.sh               # OSv unikernel
```

`make help` lists every target. The asm language server builds with
`make go-asm` (see [`docs/LSP.md`](docs/LSP.md)).

## Validation

Correctness is held to differential testing rather than self-consistency:

- **RISC-V** is differentially tested against [Spike](https://github.com/riscv-software-src/riscv-isa-sim)
  (`make test-riscv-spike`) and the RISC-V architecture test suite.
- **x86 / x86-64** assembly is checked against `nasm`, plus the `test386` suite,
  and validated by booting real distributions.
- **ARM64** encodings are byte-exact against `llvm-mc`, and execution is checked
  lane-for-lane against a real Apple Silicon CPU via a compiled trampoline oracle.

```sh
make test          # all unit tests
make check         # fmt + vet + lint + tests
```

## Heritage

The RISC-V core (`cpu/riscv`) and the surrounding RISC-V machine/device stack are
inherited work: a Claude-generated transliteration of Bellard's TinyEMU from C to
Go, directed by JT Olio
([blog post on the project's genesis](https://www.jtolio.com/2026/01/a-pure-go-linux-environment-written-by-claude-directed-by-fabrice-bellard/)).
This fork (`github.com/sorins/tinyemu-go`) added the x86, x86-64 and ARM64 cores,
the PC machine, the assemblers/disassemblers and the language server.

## Licensing

See [LICENSE](LICENSE).
