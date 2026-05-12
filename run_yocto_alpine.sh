#!/bin/sh
# Boot Alpine init from the ISO using the Yocto qemux86 kernel (known-good
# kernel) instead of Alpine's own LTS kernel. Useful for isolating Alpine
# kernel quirks from Alpine userspace quirks.
#
# The ISO is attached as virtio-blk (kernel sees /dev/vda); Alpine's init
# script picks it up via `alpine_dev=vda:iso9660`.
set -e
exec bin/temu.darwin-arm64.bin \
    -machine x86 \
    -m 512 \
    -kernel bin/bzImage-qemux86.bin \
    -initrd bin/initrd-alpine-x86 \
    -drive bin/alpine-standard-3.19.0-x86.iso -ro \
    -net-user \
    -append "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable alpine_dev=vda:iso9660 modules=loop,squashfs,virtio_pci,virtio_blk,virtio_net"
