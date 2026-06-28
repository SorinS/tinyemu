#!/bin/sh
# Boot NuttX (Apache NuttX, qemu-armv8a board, GICv2 config) on temu's arm64
# "virt" board. NuttX arm64 is an ELF linked at 0x40000000 (the RAM base); temu
# ELF-loads it (PT_LOAD by physical address, PC = ELF entry, X0 = DTB pointer),
# the same path used for seL4. Console + input are the PL011 UART at 0x09000000.
#
# Usage:
#   ./run_nuttx-arm64.sh               # boot to an "nsh>" shell
#   MEM=256 ./run_nuttx-arm64.sh       # guest RAM in MiB (default 128)
#
# The nuttx binary is built from source — see docs/nuttx_build.md. Stage it at
# bin/nuttx-arm64/nuttx.

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

KERNEL="$ROOT/bin/nuttx-arm64/nuttx"
MEM=${MEM:-128}   # NuttX qemu-armv8a CONFIG_RAM_SIZE = 128 MiB

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing $KERNEL (build NuttX qemu-armv8a:nsh_gicv2 — see docs/nuttx_build.md)" >&2; exit 1; }

echo "[run_nuttx-arm64] booting NuttX arm64 (qemu-armv8a GICv2, ${MEM} MiB)"
exec "$TEMU" -machine virt -m "$MEM" -kernel "$KERNEL"
