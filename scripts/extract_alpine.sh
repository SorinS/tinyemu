#!/bin/sh
# Pull Alpine's boot files out of bin/alpine.iso. Idempotent — outputs
# into bin/alpine/.
#
# Outputs:
#   bin/alpine/vmlinuz             — boot/vmlinuz-lts from the ISO
#   bin/alpine/initrd              — boot/initramfs-lts from the ISO
#   bin/alpine/initrd.nohw         — drop hwdrivers from sysinit
#                                    (~55s saved; coldplug modprobe storm).
#   bin/alpine/initrd.nomodloop    — drop modloop from sysinit
#                                    (~110s saved; openssl RSA-SHA verify
#                                    over modloop.squashfs).
#   bin/alpine/initrd.fast         — nohw + nomodloop combined.
#   bin/alpine/initrd.superfast    — fast + drop syslog/bootmisc/firstboot
#                                    from boot and default runlevels.
#                                    Cuts more userspace init; useful when
#                                    you just want a login prompt fast.
#                                    Do NOT use for BENCH.md numbers.
#
# `bare` mode (init=/bin/sh, no OpenRC at all) lives in run_iso.sh as a
# kernel-cmdline append, not as an initrd variant — there's nothing to
# patch in /init, the kernel just runs busybox sh as PID 1.

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ISO="$ROOT/iso/alpine.iso"; [ -f "$ISO" ] || ISO="$ROOT/bin/alpine.iso"
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

# build_variant <out-path> <relative-path…> — patches /init in a fresh
# copy of the upstream initrd so the listed paths (relative to $sysroot
# inside the running guest) are removed before switch_root. Typically
# used to drop /etc/runlevels/{sysinit,boot,default}/<service> symlinks
# so OpenRC never starts those services.
build_variant() {
    out=$1
    shift
    if [ -f "$out" ] && [ ! "$INITRD" -nt "$out" ] && [ ! "$0" -nt "$out" ]; then
        return
    fi
    echo "[extract_alpine] building $(basename "$out")"
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
