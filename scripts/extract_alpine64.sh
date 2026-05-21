#!/bin/sh
# Pull Alpine x86_64 standard boot files out of bin/alpine64.iso.
# Idempotent — outputs into bin/alpine64/.
#
# Same shape as extract_alpine.sh (which handles the 32-bit alpine-
# standard ISO for the x86 emulator). Builds patched-/init variants
# that skip slow OpenRC services in the post-switch_root rootfs.
#
# Outputs:
#   bin/alpine64/vmlinuz             — boot/vmlinuz-lts from the ISO
#   bin/alpine64/initrd              — boot/initramfs-lts from the ISO
#   bin/alpine64/initrd.nohw         — drop hwdrivers from sysinit
#   bin/alpine64/initrd.nomodloop    — drop modloop from sysinit
#   bin/alpine64/initrd.fast         — nohw + nomodloop
#   bin/alpine64/initrd.superfast    — fast + drop syslog/bootmisc/firstboot

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ISO="$ROOT/bin/alpine/alpine-standard-3.23.4-x86_64.iso"
OUT="$ROOT/bin/alpine64"
KERNEL="$OUT/vmlinuz"
INITRD="$OUT/initrd"

[ -f "$ISO" ] || { echo "missing $ISO" >&2; exit 1; }
mkdir -p "$OUT"

if [ ! -f "$KERNEL" ] || [ ! -f "$INITRD" ] || [ "$ISO" -nt "$KERNEL" ]; then
    echo "[extract_alpine64] extracting boot files from $ISO"
    tmp=$(mktemp -d)
    bsdtar -C "$tmp" -xf "$ISO" boot/vmlinuz-lts boot/initramfs-lts
    cp "$tmp/boot/vmlinuz-lts" "$KERNEL"
    cp "$tmp/boot/initramfs-lts" "$INITRD"
    rm -rf "$tmp"
fi

# build_variant <out-path> <relative-path…> — patches /init in a fresh
# copy of the upstream initrd so the listed paths (relative to $sysroot
# inside the running guest) are removed before switch_root.
build_variant() {
    out=$1
    shift
    if [ -f "$out" ] && [ ! "$INITRD" -nt "$out" ] && [ ! "$0" -nt "$out" ]; then
        return
    fi
    echo "[extract_alpine64] building $(basename "$out")"
    tmp=$(mktemp -d)
    (cd "$tmp" && gunzip -c "$INITRD" | cpio -id 2>/dev/null)
    awk -v skip="$*" '
        /^exec switch_root/ {
            n = split(skip, paths, " ")
            for (i = 1; i <= n; i++) {
                print "rm -f \"$sysroot\"" paths[i]
            }
        }
        { print }
    ' "$tmp/init" > "$tmp/init.new"
    mv "$tmp/init.new" "$tmp/init"
    chmod +x "$tmp/init"
    (cd "$tmp" && find . | cpio -o -H newc 2>/dev/null) | gzip > "$out"
    rm -rf "$tmp"
}

build_variant "$OUT/initrd.nohw" \
    /etc/runlevels/sysinit/hwdrivers
build_variant "$OUT/initrd.nomodloop" \
    /etc/runlevels/sysinit/modloop
build_variant "$OUT/initrd.fast" \
    /etc/runlevels/sysinit/hwdrivers \
    /etc/runlevels/sysinit/modloop
build_variant "$OUT/initrd.superfast" \
    /etc/runlevels/sysinit/hwdrivers \
    /etc/runlevels/sysinit/modloop \
    /etc/runlevels/boot/syslog \
    /etc/runlevels/boot/bootmisc \
    /etc/runlevels/default/firstboot
