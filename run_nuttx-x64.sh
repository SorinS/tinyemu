#!/bin/sh
# Boot NuttX (Apache NuttX, qemu-intel64 board) on temu's cpu/x86_64 long-mode
# core. NuttX is a multiboot1 ELF kernel; temu's PC board loads it via the
# multiboot path (machine/pc/multiboot64.go) and enters it in 32-bit protected
# mode with EAX=0x2BADB002 / EBX=info. Console + input are the legacy COM1 UART.
#
# Usage:
#   ./run_nuttx-x64.sh                 # boot to an "nsh>" shell
#   MEM=256 ./run_nuttx-x64.sh         # guest RAM in MiB (default 128)
#
# The nuttx binary is built from source — see docs/nuttx_build.md. Stage it at
# bin/nuttx-x64/nuttx.

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/nuttx-x64"
KERNEL="$DIR/nuttx"
MEM=${MEM:-128}

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing $KERNEL (build NuttX qemu-intel64:nsh — see docs/nuttx_build.md)" >&2; exit 1; }

echo "[run_nuttx-x64] booting NuttX x86_64 (qemu-intel64, ${MEM} MiB) via multiboot"
# -apic: NuttX requires a local APIC (x2APIC) — its capability check halts the
# CPU without it.
exec "$TEMU" -machine x86_64 -m "$MEM" -apic -kernel "$KERNEL"
