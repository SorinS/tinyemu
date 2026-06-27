#!/bin/sh
# Experiment harness: boot alpine x86_64 with a customizable kernel
# cmdline so we can drop noapic / nolapic / acpi=off one at a time
# and see what fails. Mirrors run64_iso.sh's "alpine" target.
#
# Usage:
#   scripts/exp_alpine.sh "<extra cmdline flags>"
#
# The base cmdline contains everything alpine needs *except* the three
# flags we're experimenting with. Pass the subset of "noapic nolapic
# acpi=off" you still want.

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

"$ROOT/scripts/extract_alpine64.sh" >/dev/null

KERNEL="$ROOT/bin/alpine-x64/vmlinuz"
INITRD="$ROOT/bin/alpine-x64/initrd.nonlplug"
ISO="$ROOT/bin/alpine-x86/alpine-standard-3.23.4-x86_64.iso"

EXTRA="${1:-}"
APPEND="console=ttyS0,115200 pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs module.sig_enforce=0 modprobe.blacklist=ata_piix,pata_acpi,usb-storage,usbhid $EXTRA"

echo "[exp] cmdline: $APPEND"
exec "$TEMU" -machine x86_64 -m 512 -kernel "$KERNEL" -initrd "$INITRD" -drive "$ISO" -ro -net-user -append "$APPEND"
