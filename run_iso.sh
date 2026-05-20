#!/bin/sh
# Boot one of the supported guest OSes via the x86 emulator.
#
# Usage:
#   ./run_iso.sh tinycore
#   ./run_iso.sh alpine
#   ./run_iso.sh alpine nohw         # skip hwdrivers coldplug (~80s)
#   ./run_iso.sh alpine nomodloop    # skip modloop verify (~110s)
#   ./run_iso.sh alpine fast         # nohw + nomodloop combined
#   ./run_iso.sh alpine superfast    # fast + skip syslog/bootmisc/firstboot
#   ./run_iso.sh alpine bare         # init=/bin/sh; raw busybox shell, no OpenRC
#
# fast/superfast/bare are for iteration speed only — do NOT use for
# BENCH.md numbers, they hide the modloop bignum workload that's our
# best representative CPU benchmark.
#
# The matching scripts/extract_<name>.sh handles pulling kernel+initrd
# out of bin/<name>.iso into bin/<name>/. This script just composes the
# emulator invocation.

set -e

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    echo "Usage: $0 <tinycore|alpine> [nohw|nomodloop|fast|superfast|bare]" >&2
    exit 1
fi

NAME=$1
VARIANT=${2:-}

ROOT="$(cd "$(dirname "$0")" && pwd)"
TEMU="$ROOT/bin/temu.darwin-arm64.bin"
EXTRACT="$ROOT/scripts/extract_$NAME.sh"

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU" >&2; exit 1; }
[ -x "$EXTRACT" ] || { echo "unknown OS '$NAME' (no $EXTRACT)" >&2; exit 1; }

"$EXTRACT"

KERNEL="$ROOT/bin/$NAME/vmlinuz"
INITRD="$ROOT/bin/$NAME/initrd"
EXTRA_APPEND=""
case $VARIANT in
    "")
        ;;
    nohw|nomodloop|fast|superfast)
        candidate="$ROOT/bin/$NAME/initrd.$VARIANT"
        if [ -f "$candidate" ]; then
            INITRD="$candidate"
            echo "[run_iso] using $VARIANT initrd ($candidate)"
        else
            echo "[run_iso] warning: '$VARIANT' has no effect for $NAME (missing $candidate)" >&2
        fi
        ;;
    bare)
        # No initrd patch — kernel cmdline replaces the post-switchroot
        # init binary with /bin/sh. OpenRC never runs, services don't
        # start, you get a raw busybox prompt as soon as Alpine's /init
        # switch_root's.
        EXTRA_APPEND=" init=/bin/sh"
        echo "[run_iso] bare mode: init=/bin/sh (no OpenRC, raw busybox shell)"
        ;;
    *)
        echo "Unknown variant '$VARIANT' (expected nohw|nomodloop|fast|superfast|bare)" >&2
        exit 1
        ;;
esac

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

APPEND="$APPEND$EXTRA_APPEND"

echo "Starting $NAME at: $(date)"

exec "$TEMU" \
    -machine x86 \
    -m "$MEM" \
    -kernel "$KERNEL" \
    -initrd "$INITRD" \
    -drive "$ISO" -ro \
    -net-user \
    -append "$APPEND"
