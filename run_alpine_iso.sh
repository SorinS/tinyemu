#!/bin/sh
# Boot Alpine Linux 3.19 from its ISO image.
#
# Uses the Yocto qemux86 kernel (which has virtio_pci_modern/_legacy
# built-in — Alpine's own kernel separates them into modules that
# trip a kernel oops in our emulator) combined with the Alpine
# initramfs. The ISO is attached as virtio-blk (kernel sees /dev/vda
# with vda1/vda2 partitions); Alpine's init picks it up via
# `alpine_dev=vda:iso9660`.
#
# Console is the 16550 UART at COM1, routed to stdin/stdout. Ctrl-A x
# exits the emulator. nlplug-findfs may segfault inside musl libc
# (open SSE/MMX issue) and Alpine drops into the emergency recovery
# shell after `usbdelay=10` seconds — that's where you currently land.
set -e
exec bin/temu.darwin-arm64.bin \
    -machine x86 \
    -m 512 \
    -kernel bin/bzImage-qemux86.bin \
    -initrd bin/initrd-alpine-x86 \
    -drive bin/alpine-standard-3.19.0-x86.iso -ro \
    -net-user \
    -append "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable alpine_dev=vda:iso9660 usbdelay=10 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs"
