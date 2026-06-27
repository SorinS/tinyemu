#!/bin/sh
# Build a TinyCorePure64 initramfs that auto-logs root on ttyS0 in
# addition to tty1. Upstream corepure64.gz only spawns getty on tty1
# (the VGA console), so an emulator without a display sees the boot
# reach `login[…]: root login on 'tty1'` and then go silent on the
# serial console where stdin/stdout live. Adding a ttyS0 getty makes
# the shell reachable over the serial line.
#
# Idempotent: re-running with no changes is cheap and produces the
# same output. Re-runs when the upstream corepure64.gz changes or
# when this script's mtime is newer than the cached overlay.
#
# Inputs:
#   bin/tinycore-x64/corepure64.gz   — upstream cpio.gz initramfs
#
# Outputs:
#   bin/tinycore-x64/serial-overlay.gz   — a tiny cpio.gz with just
#                                       /etc/inittab
#   bin/tinycore-x64/corepure64-serial.gz — the concatenation of the
#                                        upstream corepure64.gz and
#                                        serial-overlay.gz. This is
#                                        what `-initrd` should point
#                                        at when we want an
#                                        interactive shell on the
#                                        serial port.
#
# Why concatenation works: the Linux initramfs loader accepts multiple
# cpio archives back-to-back inside a single file (see
# Documentation/early-userspace/buffer-format.rst). Each successive
# entry overwrites earlier ones, so a later /etc/inittab wins. This
# lets us add a serial getty without unpacking, modifying, and re-
# packing the whole upstream image — neat, fast, and unaffected by
# upstream churn.

set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/bin/tinycore-x64"
CORE="$OUT/corepure64.gz"
OVERLAY="$OUT/serial-overlay.gz"
INITRD="$OUT/corepure64-serial.gz"

[ -r "$CORE" ] || { echo "missing $CORE — fetch corepure64.gz from upstream first" >&2; exit 1; }

# Rebuild the overlay if missing or older than this script. The
# inittab is small enough to inline. The format mirrors what
# upstream's etc/inittab uses for tty1 — same auto-login wrapper,
# same flags — just with ttyS0 added.
if [ ! -f "$OVERLAY" ] || [ "$0" -nt "$OVERLAY" ]; then
    echo "[extract_tinycore64] building serial-overlay.gz"
    tmp=$(mktemp -d)
    mkdir -p "$tmp/etc"
    cat > "$tmp/etc/inittab" <<'INITTAB'
# Patched by scripts/extract_tinycore64.sh — adds a ttyS0 getty so the
# serial console (mapped to stdin/stdout by the emulator) reaches the
# busybox shell. Without this the boot stops responding after the
# tty1 login spawns because we have no VGA console.

::sysinit:/etc/init.d/rcS

# Auto-login root on tty1 (the upstream default) AND on ttyS0.
# `-nl /sbin/autologin` tells busybox-getty to skip the login prompt
# and run /sbin/autologin instead, which exec()s the user's shell.
tty1::respawn:/sbin/getty -nl /sbin/autologin 38400 tty1
ttyS0::respawn:/sbin/getty -nl /sbin/autologin -L 115200 ttyS0 vt100

# Restart / shutdown plumbing — copied verbatim from upstream so the
# inittab stays a complete override rather than a partial one.
::restart:/etc/init.d/rc.shutdown
::restart:/sbin/init
::ctrlaltdel:/sbin/reboot
::shutdown:/etc/init.d/rc.shutdown
INITTAB
    (cd "$tmp" && find etc | cpio -o -H newc 2>/dev/null) | gzip > "$OVERLAY"
    rm -rf "$tmp"
fi

# Concatenate. Cheap — corepure64.gz is ~17 MB, overlay is tens of
# bytes. The output is what `./run64_iso.sh tinycore` points at.
if [ ! -f "$INITRD" ] || [ "$CORE" -nt "$INITRD" ] || [ "$OVERLAY" -nt "$INITRD" ]; then
    echo "[extract_tinycore64] writing $INITRD"
    cat "$CORE" "$OVERLAY" > "$INITRD"
fi
