#!/bin/sh
# Boot Alpine Linux 3.19 directly from the ISO.
#
# The ISO is attached as virtio-blk (kernel sees /dev/vda); Alpine's
# init script picks it up via `alpine_dev=vda:iso9660`.
#
# Console is the 16550 UART at COM1, routed to stdin/stdout. Ctrl-A x
# exits the emulator.
set -e
exec bin/temu.darwin-arm64.bin \
    -machine x86 \
    -m 512 \
    -kernel bin/vmlinuz-alpine-x86 \
    -initrd bin/initrd-alpine-x86 \
    -drive bin/alpine-standard-3.19.0-x86.iso -ro \
    -net-user \
    -append "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable alpine_dev=vda:iso9660 modules=loop,squashfs,sd-mod,usb-storage,virtio_blk,virtio_net,virtio_pci"
