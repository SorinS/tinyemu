#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
BIOS="$ROOT/bin/seabios/bios.bin"
DIR="$ROOT/bin/openwrt-x64"
IMG="$DIR/openwrt-24.10.0-x86-64-generic-ext4-combined.img"

[ -x "$TEMU" ] || { echo "missing emulator $TEMU" >&2; exit 1; }
[ -r "$BIOS" ] || { echo "missing SeaBIOS $BIOS" >&2; exit 1; }
[ -r "$IMG" ] || { echo "missing OpenWrt image $IMG" >&2; exit 1; }

exec "$TEMU" -machine x86_64 -m 256 -apic \
    -bios "$BIOS" \
    -drive "$IMG"
