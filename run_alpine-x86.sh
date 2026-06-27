#!/bin/sh
# Boot Alpine Linux x86 (32-bit) to an OpenRC login on the cpu/x86 emulator.
#
# Direct kernel boot: the extracted vmlinuz + initramfs, plus the Alpine media
# as a raw .img the guest mounts read-only as /dev/vda (iso9660) for the modloop
# + apk repo. Replaces the old run86_iso.sh alpine target.
#
# Usage:
#   ./run_alpine-x86.sh                 # boot to an Alpine login on ttyS0
#   ./run_alpine-x86.sh "single"        # extra kernel command line
#   MEM=768 ./run_alpine-x86.sh         # guest RAM in MiB (default 512)
#
# Assets (built by `make alpine`, i.e. scripts/extract_alpine.sh, from the ISO
# in iso/) — all under bin/alpine-x86/:
#   vmlinuz / initrd  kernel + Alpine initramfs
#   media.img         raw iso9660 media image (modloop + apks)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/alpine-x86"
"$ROOT/scripts/extract_alpine.sh"   # idempotent: kernel + initrd + media.img

KERNEL="$DIR/vmlinuz"
INITRD="$DIR/initrd"
IMG="$DIR/media.img"
MEM=${MEM:-512}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make alpine')" >&2; exit 1; }
[ -r "$IMG" ]    || { echo "missing media $IMG (run 'make alpine')" >&2; exit 1; }

APPEND="console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs $EXTRA"

echo "[run_alpine-x86] booting Alpine x86 (${MEM} MiB)"
exec "$TEMU" -machine x86 -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -drive "$IMG" -ro -net-user -append "$APPEND"
