#!/bin/sh
# Boot one of the supported guest OSes via the x86 emulator.
#
# Usage:
#   ./run_iso.sh tinycore
#   ./run_iso.sh alpine
#
# The matching scripts/extract_<name>.sh handles pulling kernel+initrd
# out of bin/<name>.iso into bin/<name>/. This script just composes the
# emulator invocation.

set -e

if [ $# -ne 1 ]; then
    echo "Usage: $0 <tinycore|alpine>" >&2
    exit 1
fi

NAME=$1
ROOT="$(cd "$(dirname "$0")" && pwd)"
TEMU="$ROOT/bin/temu.darwin-arm64.bin"
EXTRACT="$ROOT/scripts/extract_$NAME.sh"

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU" >&2; exit 1; }
[ -x "$EXTRACT" ] || { echo "unknown OS '$NAME' (no $EXTRACT)" >&2; exit 1; }

"$EXTRACT"

KERNEL="$ROOT/bin/$NAME/vmlinuz"
INITRD="$ROOT/bin/$NAME/initrd"

case $NAME in
    tinycore)
        ISO="$ROOT/bin/TinyCore.iso"
        MEM=256
        APPEND="console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable text superuser"
        ;;
    alpine)
        ISO="$ROOT/bin/alpine.iso"
        MEM=512
        APPEND="console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs"
        ;;
    *)
        echo "unknown OS '$NAME'" >&2
        exit 1
        ;;
esac

echo "Starting $NAME at: $(date)"

exec "$TEMU" \
    -machine x86 \
    -m "$MEM" \
    -kernel "$KERNEL" \
    -initrd "$INITRD" \
    -drive "$ISO" -ro \
    -net-user \
    -append "$APPEND"
