#!/bin/sh
# Pull Alpine's boot files out of bin/alpine.iso. Idempotent — outputs
# into bin/alpine/.
#
# Outputs:
#   bin/alpine/vmlinuz           — boot/vmlinuz-lts from the ISO
#   bin/alpine/initrd            — boot/initramfs-lts from the ISO
#   bin/alpine/initrd.nohw       — initrd with the hwdrivers sysinit
#                                  service disabled (skips the coldplug
#                                  modprobe storm; saves ~80s wall).
#   bin/alpine/initrd.nomodloop  — initrd with the modloop sysinit
#                                  service disabled (skips the openssl
#                                  RSA-SHA verify pass over the modloop
#                                  squashfs; saves ~110s wall). The
#                                  kernel still boots; built-in drivers
#                                  cover the minimal-boot needs but
#                                  loadable modules won't be available.
#   bin/alpine/initrd.fast       — both of the above. Useful for
#                                  iteration; do NOT use for BENCH.md
#                                  measurements (it hides the bignum
#                                  CPU workload that's our best
#                                  representative benchmark).

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

# build_variant <out-path> <flags…> — patches /init in a fresh copy of
# the upstream initrd so the listed sysinit services are removed before
# switch_root. Each flag corresponds to /etc/runlevels/sysinit/<service>.
build_variant() {
    out=$1
    shift
    if [ -f "$out" ] && [ ! "$INITRD" -nt "$out" ] && [ ! "$0" -nt "$out" ]; then
        return
    fi
    echo "[extract_alpine] building $(basename "$out") (skip: $*)"
    tmp=$(mktemp -d)
    (cd "$tmp" && gunzip -c "$INITRD" | cpio -id 2>/dev/null)
    awk -v skip="$*" '
        /^exec switch_root/ {
            n = split(skip, svc, " ")
            for (i = 1; i <= n; i++) {
                print "rm -f \"$sysroot\"/etc/runlevels/sysinit/" svc[i]
            }
        }
        { print }
    ' "$tmp/init" > "$tmp/init.new"
    mv "$tmp/init.new" "$tmp/init"
    chmod +x "$tmp/init"
    (cd "$tmp" && find . | cpio -o -H newc 2>/dev/null) | gzip > "$out"
    rm -rf "$tmp"
}

build_variant "$OUT/initrd.nohw" hwdrivers
build_variant "$OUT/initrd.nomodloop" modloop
build_variant "$OUT/initrd.fast" hwdrivers modloop
