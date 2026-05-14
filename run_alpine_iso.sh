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
# exits the emulator.
#
# Cmdline notes:
#   libata.force=disable ide=disable  — skip the legacy IDE controller
#       on our PIIX3 (the ATAPI probe stalls in async EH; we don't need
#       it because the ISO is on virtio-blk anyway).
#   usbdelay=1                         — nlplug's per-device delay; with
#       libata disabled there's nothing to wait on.
#
# Boot lands in Alpine's emergency recovery shell because there's no
# /sbin/init in the empty sysroot. The init script blocks on stdin
# (apk waiting for overlay file list). Type Ctrl-D once to advance
# past that; you'll then get `~ #` and a working busybox shell + vi.
set -e
exec bin/temu.darwin-arm64.bin \
    -machine x86 \
    -m 512 \
    -kernel bin/bzImage-qemux86.bin \
    -initrd bin/initrd-alpine-x86 \
    -drive bin/alpine-standard-3.19.0-x86.iso -ro \
    -net-user \
    -append "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs"
