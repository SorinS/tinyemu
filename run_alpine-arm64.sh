#!/bin/sh
# Boot a full Alpine Linux (aarch64) userland on the cpu/arm64 "virt" board:
# Alpine's kernel + the Alpine minirootfs packed as an initramfs, so it comes up
# to a real Alpine shell (apk, /etc, the works) with no disk or install media.
#
# Usage:
#   ./run_alpine-arm64.sh                 # boot to an Alpine "/ #" shell
#   ./run_alpine-arm64.sh "loglevel=4"    # extra kernel command line
#   MEM=1024 ./run_alpine-arm64.sh        # guest RAM in MiB (default 512)
#
# Assets (fetch/build with `make alpine-arm64`):
#   bin/alpine-arm64/Image                       flat arm64 kernel (Alpine aarch64 virt)
#   bin/alpine-arm64/alpine-rootfs-initramfs.gz  Alpine minirootfs as an initramfs
#
# (For a tiny busybox-only shell, use run_arm64.sh.)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/alpine-arm64"
KERNEL="$DIR/Image"
INITRD="$DIR/alpine-rootfs-initramfs.gz"
MEM=${MEM:-512}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make alpine-arm64')" >&2; exit 1; }
[ -r "$INITRD" ] || { echo "missing $INITRD (run 'make alpine-arm64')" >&2; exit 1; }

# mitigations=off skips KPTI/Spectre alternatives the simplified MMU model can't
# satisfy (see run_arm64.sh). earlycon shows kernel output from the first
# instructions over the PL011 UART.
CMDLINE="console=ttyAMA0 earlycon=pl011,mmio32,0x9000000 loglevel=8 mitigations=off rdinit=/init $EXTRA"

echo "[run_alpine-arm64] booting Alpine aarch64 (${MEM} MiB) on the virt board"
echo "  cmdline: $CMDLINE"
exec "$TEMU" -machine virt -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -append "$CMDLINE"
