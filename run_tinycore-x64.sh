#!/bin/sh
# Boot TinyCore Linux (CorePure64) x86_64 to a busybox shell on the cpu/x86_64
# long-mode core. Self-contained: the initramfs IS the whole OS, so there is no
# disk/media — just a direct kernel + initrd boot. Replaces run64_iso.sh tinycore.
#
# Usage:
#   ./run_tinycore-x64.sh               # boot to a "/ #" shell on ttyS0
#   ./run_tinycore-x64.sh "loglevel=4"  # extra kernel command line
#   MEM=256 ./run_tinycore-x64.sh       # guest RAM in MiB (default 128)
#
# Assets (built by `make tinycore64`, i.e. scripts/extract_tinycore64.sh) under
# bin/tinycore-x64/:
#   vmlinux64               pre-decompressed kernel (falls back to vmlinuz64)
#   corepure64-serial.gz    initramfs patched with a ttyS0 auto-login getty
#                           (falls back to the upstream corepure64.gz)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/tinycore-x64"
"$ROOT/scripts/extract_tinycore64.sh"   # idempotent: builds the serial initramfs

[ -r "$DIR/vmlinux64" ] && KERNEL="$DIR/vmlinux64" || KERNEL="$DIR/vmlinuz64"
[ -r "$DIR/corepure64-serial.gz" ] && INITRD="$DIR/corepure64-serial.gz" || INITRD="$DIR/corepure64.gz"
MEM=${MEM:-128}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make tinycore64')" >&2; exit 1; }
[ -r "$INITRD" ] || { echo "missing initrd $INITRD (run 'make tinycore64')" >&2; exit 1; }

APPEND="console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable cde $EXTRA"

echo "[run_tinycore-x64] booting TinyCore x86_64 (${MEM} MiB)"
exec "$TEMU" -machine x86_64 -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -net-user -append "$APPEND"
