#!/bin/sh
# Pull TinyCore's boot files out of bin/TinyCore.iso and produce a
# combined initramfs whose /etc/inittab also spawns an auto-login getty
# on ttyS0. Idempotent — outputs into bin/tinycore/.
#
# Outputs:
#   bin/tinycore/vmlinuz   — kernel from boot/vmlinuz in the ISO
#   bin/tinycore/core.gz   — initramfs from boot/core.gz in the ISO
#   bin/tinycore/initrd    — core.gz concatenated with the inittab
#                            overlay. This is what -initrd points at.

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ISO="$ROOT/bin/TinyCore.iso"
OUT="$ROOT/bin/tinycore"
KERNEL="$OUT/vmlinuz"
CORE="$OUT/core.gz"
OVERLAY="$OUT/overlay.gz"
INITRD="$OUT/initrd"

[ -f "$ISO" ] || { echo "missing $ISO" >&2; exit 1; }
mkdir -p "$OUT"

# Step 1: extract vmlinuz + core.gz from the ISO if missing or stale.
if [ ! -f "$KERNEL" ] || [ ! -f "$CORE" ] || [ "$ISO" -nt "$KERNEL" ]; then
    echo "[extract_tinycore] extracting boot files from $ISO"
    tmp=$(mktemp -d)
    bsdtar -C "$tmp" -xf "$ISO" boot/vmlinuz boot/core.gz
    cp "$tmp/boot/vmlinuz" "$KERNEL"
    cp "$tmp/boot/core.gz" "$CORE"
    rm -rf "$tmp"
fi

# Step 2: build the /etc/inittab overlay if missing or older than this script.
if [ ! -f "$OVERLAY" ] || [ "$0" -nt "$OVERLAY" ]; then
    echo "[extract_tinycore] building inittab overlay"
    tmp=$(mktemp -d)
    mkdir -p "$tmp/etc"
    cat > "$tmp/etc/inittab" <<'INITTAB'
# Patched by extract_tinycore.sh — adds ttyS0 auto-login getty so the
# serial console has an interactive shell.
::sysinit:/etc/init.d/rcS

tty1::respawn:/sbin/getty -nl /sbin/autologin 38400 tty1
ttyS0::respawn:/sbin/getty -nl /sbin/autologin -L 115200 ttyS0 vt100

::restart:/etc/init.d/rc.shutdown
::restart:/sbin/init
::ctrlaltdel:/sbin/reboot
::shutdown:/etc/init.d/rc.shutdown
INITTAB
    (cd "$tmp" && find etc | cpio -o -H newc 2>/dev/null) | gzip > "$OVERLAY"
    rm -rf "$tmp"
fi

# Step 3: combine core.gz + overlay.gz into the final initramfs the
# kernel will load. Concatenated gzip-cpio streams are valid initramfs
# input; later entries override earlier ones.
if [ ! -f "$INITRD" ] || [ "$CORE" -nt "$INITRD" ] || [ "$OVERLAY" -nt "$INITRD" ]; then
    echo "[extract_tinycore] combining initramfs streams"
    cat "$CORE" "$OVERLAY" > "$INITRD"
fi
