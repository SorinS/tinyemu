#!/bin/sh
# Boot TinyCore Linux x86 (32-bit) to a busybox shell on the cpu/x86 emulator.
# Self-contained: the initramfs IS the whole OS (no disk/media), so this is a
# direct kernel + initrd boot. Replaces the old run86_iso.sh tinycore target.
#
# Usage:
#   ./run_tinycore-x86.sh               # boot to a "/ #" shell on ttyS0
#   ./run_tinycore-x86.sh "loglevel=4"  # extra kernel command line
#   MEM=512 ./run_tinycore-x86.sh       # guest RAM in MiB (default 256)
#
# Assets (built by `make tinycore`, i.e. scripts/extract_tinycore.sh, from
# iso/TinyCore.iso) under bin/tinycore-x86/:
#   vmlinuz   kernel
#   initrd    core.gz patched with a ttyS0 auto-login getty

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/tinycore-x86"
"$ROOT/scripts/extract_tinycore.sh"   # idempotent: builds the serial initramfs

KERNEL="$DIR/vmlinuz"
INITRD="$DIR/initrd"
MEM=${MEM:-256}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make tinycore')" >&2; exit 1; }
[ -r "$INITRD" ] || { echo "missing initrd $INITRD (run 'make tinycore')" >&2; exit 1; }

APPEND="console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable text superuser $EXTRA"

echo "[run_tinycore-x86] booting TinyCore x86 (${MEM} MiB)"
exec "$TEMU" -machine x86 -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -net-user -append "$APPEND"
