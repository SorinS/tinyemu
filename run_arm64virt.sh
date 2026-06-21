#!/bin/sh
# Boot a minimal arm64 Linux + busybox shell on the cpu/arm64 "virt" board
# (GICv2 + PL011 + generic timer + PSCI + VirtIO-MMIO + a generated device tree).
# Self-contained: a busybox-only initramfs boots straight to an interactive
# "~ #" shell — no distro init, no boot media. (For the full Alpine userland,
# use run_alpine-arm64.sh.)
#
# Usage:
#   ./run_arm64.sh                 # boot to a busybox shell
#   ./run_arm64.sh "loglevel=4"    # extra kernel command line
#   MEM=1024 ./run_arm64.sh        # guest RAM in MiB (default 512)
#
# Assets (fetch/build with `make alpine-arm64`):
#   bin/arm64virt/Image                flat arm64 kernel Image (decompressed)
#   bin/arm64virt/busybox-initramfs.gz minimal busybox initramfs
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
INITRD="$DIR/busybox-initramfs.gz"
MEM=${MEM:-512}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make alpine-arm64')" >&2; exit 1; }
[ -r "$INITRD" ] || { echo "missing $INITRD (run 'make alpine-arm64')" >&2; exit 1; }

# mitigations=off forces KPTI / Spectre alternatives off: those patch the
# exception path into a page-table-switching trampoline this emulator's
# simplified MMU/cache model doesn't satisfy (the kernel only enables them
# because our ID registers read as a vulnerable CPU).
CMDLINE="console=ttyAMA0 earlycon=pl011,mmio32,0x9000000 loglevel=8 mitigations=off rdinit=/init $EXTRA"

echo "[run_arm64] booting a minimal busybox shell (${MEM} MiB) on the virt board"
echo "  cmdline: $CMDLINE"
exec "$TEMU" -machine virt -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -append "$CMDLINE"
