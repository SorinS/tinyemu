#!/bin/sh
# Boot Alpine Linux x86_64 to an OpenRC login on the cpu/x86_64 long-mode core.
#
# Direct kernel boot (the x86_64 backend has no real-mode/BIOS path): the
# extracted vmlinux + initramfs, plus the Alpine media as a raw .img that the
# guest mounts read-only as /dev/vda (iso9660) for the modloop + apk repo.
# Replaces the old run64_iso.sh alpine target.
#
# Usage:
#   ./run_alpine-x64.sh                 # boot to an Alpine login on ttyS0
#   ./run_alpine-x64.sh "single"        # extra kernel command line
#   MEM=1024 ./run_alpine-x64.sh        # guest RAM in MiB (default 512)
#
# Assets (built by `make alpine64`, i.e. scripts/extract_alpine64.sh, from the
# ISO in iso/) — all under bin/alpine-x64/:
#   vmlinux         pre-decompressed kernel (falls back to vmlinuz)
#   initrd.nonlplug Alpine initramfs patched to skip the nlplug-findfs hang
#   media.img       raw iso9660 media image (modloop + apks)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/alpine-x64"
"$ROOT/scripts/extract_alpine64.sh"   # idempotent: kernel + initrd + media.img

# Prefer the pre-decompressed ELF (skips the in-guest bzImage self-decompressor).
[ -r "$DIR/vmlinux" ] && KERNEL="$DIR/vmlinux" || KERNEL="$DIR/vmlinuz"
INITRD="$DIR/initrd.nonlplug"
IMG="$DIR/media.img"
MEM=${MEM:-512}
EXTRA="${1:-}"

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL (run 'make alpine64')" >&2; exit 1; }
[ -r "$IMG" ]    || { echo "missing media $IMG (run 'make alpine64')" >&2; exit 1; }

# modprobe.blacklist keeps the kernel off drivers for hardware we don't emulate
# (ata_piix's phantom-port probe alone is ~60s). module.sig_enforce=0 skips the
# expensive software-bignum module signature check.
APPEND="console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs module.sig_enforce=0 modprobe.blacklist=ata_piix,pata_acpi,usb-storage,usbhid $EXTRA"

echo "[run_alpine-x64] booting Alpine x86_64 (${MEM} MiB)"
exec "$TEMU" -machine x86_64 -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -drive "$IMG" -ro -net-user -append "$APPEND"
