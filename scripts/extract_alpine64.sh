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
VMLINUX="$OUT/vmlinux"
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

# Decompress the bzImage to a raw vmlinux ELF so the direct -kernel boot
# (run64_iso.sh) skips the in-guest self-decompressor, which is brutally
# slow under the interpreter (~50s -> a few seconds to the kernel banner
# for this kernel). The upstream scripts/extract-vmlinux relies on GNU
# tr/grep tricks that silently no-op on macOS (it re-emits the bzImage),
# so find the gzip payload portably: every gzip magic (1f 8b 08) on a byte
# boundary, gunzip from there, and accept the first stream that yields an
# ELF (7f 45 4c 46). Other compressors aren't handled here — the Alpine
# lts kernel is gzip.
if [ ! -f "$VMLINUX" ] || [ "$KERNEL" -nt "$VMLINUX" ]; then
    echo "[extract_alpine64] decompressing $KERNEL -> $VMLINUX"
    found=0
    for hexoff in $(xxd -p "$KERNEL" | tr -d '\n' | grep -bo '1f8b08' | cut -d: -f1); do
        [ $((hexoff % 2)) -eq 0 ] || continue # only byte-aligned matches
        tail -c "+$((hexoff / 2 + 1))" "$KERNEL" | gunzip > "$VMLINUX" 2>/dev/null
        if [ "$(head -c 4 "$VMLINUX" 2>/dev/null | xxd -p)" = "7f454c46" ]; then
            found=1
            break
        fi
    done
    if [ "$found" != 1 ]; then
        rm -f "$VMLINUX"
        echo "[extract_alpine64] warning: could not decompress a vmlinux ELF; run64_iso.sh will fall back to the compressed vmlinuz" >&2
    fi
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

# nonlplug: replace the `nlplug-findfs` invocation in /init with a
# direct `mount -r -t iso9660 /dev/vda /media/cdrom`.  nlplug-findfs
# uses netlink+epoll in a way that hangs indefinitely on x86_64 (root
# cause TBD); since we know /dev/vda is the boot device via the
# alpine_dev=vda:iso9660 kernel cmdline, we can skip the device-
# discovery step entirely. This also disables modloop verification
# and apkovl pickup — fine for a "just get to a shell" boot.
build_nonlplug() {
    out=$1
    if [ -f "$out" ] && [ ! "$INITRD" -nt "$out" ] && [ ! "$0" -nt "$out" ]; then
        return
    fi
    echo "[extract_alpine64] building $(basename "$out")"
    tmp=$(mktemp -d)
    (cd "$tmp" && gunzip -c "$INITRD" | cpio -id 2>/dev/null)
    awk '
        /^ebegin "Mounting boot media"$/ {
            print "ebegin \"Mounting boot media (nlplug bypass)\""
            print "mkdir -p /media/cdrom"
            print "mount -r -t iso9660 /dev/vda /media/cdrom"
            skip = 1
            next
        }
        skip && /^eend / {
            print
            skip = 0
            next
        }
        !skip { print }
    ' "$tmp/init" > "$tmp/init.new"
    mv "$tmp/init.new" "$tmp/init"
    chmod +x "$tmp/init"
    (cd "$tmp" && find . | cpio -o -H newc 2>/dev/null) | gzip > "$out"
    rm -rf "$tmp"
}

build_nonlplug "$OUT/initrd.nonlplug"

# nonlplug-fast: nlplug bypass + drop hwdrivers, modloop, syslog,
# bootmisc, firstboot. Fastest path to a shell when you also need
# the nlplug workaround (e.g. x86_64).
build_nonlplug_fast() {
    out=$1
    if [ -f "$out" ] && [ ! "$INITRD" -nt "$out" ] && [ ! "$0" -nt "$out" ]; then
        return
    fi
    echo "[extract_alpine64] building $(basename "$out")"
    tmp=$(mktemp -d)
    (cd "$tmp" && gunzip -c "$INITRD" | cpio -id 2>/dev/null)
    awk '
        /^ebegin "Mounting boot media"$/ {
            print "ebegin \"Mounting boot media (nlplug bypass)\""
            print "mkdir -p /media/cdrom"
            print "mount -r -t iso9660 /dev/vda /media/cdrom"
            skip = 1
            next
        }
        skip && /^eend / {
            print
            skip = 0
            next
        }
        /^exec switch_root/ {
            print "rm -f \"$sysroot\"/etc/runlevels/sysinit/hwdrivers"
            print "rm -f \"$sysroot\"/etc/runlevels/sysinit/modloop"
            print "rm -f \"$sysroot\"/etc/runlevels/boot/syslog"
            print "rm -f \"$sysroot\"/etc/runlevels/boot/bootmisc"
            print "rm -f \"$sysroot\"/etc/runlevels/default/firstboot"
        }
        !skip { print }
    ' "$tmp/init" > "$tmp/init.new"
    mv "$tmp/init.new" "$tmp/init"
    chmod +x "$tmp/init"
    (cd "$tmp" && find . | cpio -o -H newc 2>/dev/null) | gzip > "$out"
    rm -rf "$tmp"
}

build_nonlplug_fast "$OUT/initrd.nonlplug-fast"
