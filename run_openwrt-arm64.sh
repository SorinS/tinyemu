#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
FW="$ROOT/bin/freebsd-arm64/edk2-code.fd"
DIR="$ROOT/bin/openwrt-arm64"
IMG="$DIR/openwrt-24.10.0-armsr-armv8-generic-ext4-combined-efi.img"
MEM=${MEM:-512}

[ -x "$TEMU" ] || { echo "missing emulator $TEMU" >&2; exit 1; }
[ -r "$FW" ] || { echo "missing UEFI firmware $FW" >&2; exit 1; }
[ -r "$IMG" ] || { echo "missing OpenWrt image $IMG" >&2; exit 1; }

exec "$TEMU" -machine virt -m "$MEM" -bios "$FW" -drive "$IMG"
