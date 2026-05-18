#!/bin/sh
# Pull Alpine's boot files out of bin/alpine.iso. Idempotent — outputs
# into bin/alpine/.
#
# Outputs:
#   bin/alpine/vmlinuz    — boot/vmlinuz-lts from the ISO
#   bin/alpine/initrd     — boot/initramfs-lts from the ISO

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ISO="$ROOT/bin/alpine.iso"
OUT="$ROOT/bin/alpine"
KERNEL="$OUT/vmlinuz"
INITRD="$OUT/initrd"

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
