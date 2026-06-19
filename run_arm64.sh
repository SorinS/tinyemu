#!/bin/sh
# Boot an arm64 Linux kernel on the cpu/arm64 "virt" board (GICv2 + PL011 +
# generic timer + PSCI + VirtIO-MMIO), with a generated device tree.
#
# Usage:
#   ./run_arm64.sh                 # boot bin/arm64virt/Image + initramfs
#   ./run_arm64.sh "init=/bin/sh"  # extra kernel command line
#   MEM=1024 ./run_arm64.sh        # guest RAM in MiB (default 512)
#
# Assets (build/fetch with `make arm64`):
#   bin/arm64virt/Image            flat arm64 kernel Image (decompressed)
#   bin/arm64virt/initramfs-virt   initramfs
#
# The kernel prints over the PL011 UART; `earlycon` shows output from the very
# first instructions, so even a partial boot is visible on the console.

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/arm64virt"
KERNEL="$DIR/Image"
INITRD="$DIR/initramfs-virt"
MEM=${MEM:-512}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make arm64')" >&2; exit 1; }

CMDLINE="console=ttyAMA0 earlycon=pl011,mmio32,0x9000000 loglevel=8 $EXTRA"

set -- -machine virt -m "$MEM" -kernel "$KERNEL" -append "$CMDLINE"
[ -r "$INITRD" ] && set -- "$@" -initrd "$INITRD"

echo "[run_arm64] booting $KERNEL (${MEM} MiB) on the virt board"
echo "  cmdline: $CMDLINE"
exec "$TEMU" "$@"
