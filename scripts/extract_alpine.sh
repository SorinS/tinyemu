#!/bin/sh
# Pull Alpine's boot files out of bin/alpine.iso. Idempotent — outputs
# into bin/alpine/.
#
# Outputs:
#   bin/alpine/vmlinuz       — boot/vmlinuz-lts from the ISO
#   bin/alpine/initrd        — boot/initramfs-lts from the ISO
#   bin/alpine/initrd.nohw   — same initrd, but /init is patched to delete
#                              /etc/runlevels/sysinit/hwdrivers before
#                              switch_root. The kernel still boots fully;
#                              we just skip the coldplug-driven modprobe
#                              storm (saves ~80s of wall time at our
#                              emulator's clock rate).

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ISO="$ROOT/bin/alpine.iso"
OUT="$ROOT/bin/alpine"
KERNEL="$OUT/vmlinuz"
INITRD="$OUT/initrd"
INITRD_NOHW="$OUT/initrd.nohw"

[ -f "$ISO" ] || { echo "missing $ISO" >&2; exit 1; }
mkdir -p "$OUT"

if [ ! -f "$KERNEL" ] || [ ! -f "$INITRD" ] || [ "$ISO" -nt "$KERNEL" ]; then
    echo "[extract_alpine] extracting boot files from $ISO"
    tmp=$(mktemp -d)
    bsdtar -C "$tmp" -xf "$ISO" boot/vmlinuz-lts boot/initramfs-lts
    cp "$tmp/boot/vmlinuz-lts" "$KERNEL"
    cp "$tmp/boot/initramfs-lts" "$INITRD"
    rm -rf "$tmp"
fi

if [ ! -f "$INITRD_NOHW" ] || [ "$INITRD" -nt "$INITRD_NOHW" ] || [ "$0" -nt "$INITRD_NOHW" ]; then
    echo "[extract_alpine] building nohw variant"
    tmp=$(mktemp -d)
    (cd "$tmp" && gunzip -c "$INITRD" | cpio -id 2>/dev/null)
    awk '
        /^exec switch_root/ {
            print "rm -f \"$sysroot\"/etc/runlevels/sysinit/hwdrivers"
        }
        { print }
    ' "$tmp/init" > "$tmp/init.new"
    mv "$tmp/init.new" "$tmp/init"
    chmod +x "$tmp/init"
    (cd "$tmp" && find . | cpio -o -H newc 2>/dev/null) | gzip > "$INITRD_NOHW"
    rm -rf "$tmp"
fi
