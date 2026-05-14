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
# Alpine /init blocks on a stdin read after "Installing packages: ok"
# (apk add inherits the script's stdin = /dev/console and reads it
# even without --overlay-from-stdin in some code paths). We pre-feed
# Ctrl-D (\x04 = EOF) via -stdin-prefix so the read returns 0 bytes
# and /init falls through to the "/sbin/init not found in new root"
# branch, landing at the busybox emergency shell `~ #` prompt. After
# that, all host keystrokes flow through normally and you get a
# working interactive shell + vi.
#
# Five Ctrl-D bytes are needed in practice: the kernel's serial console
# init consumes a few before the userspace process actually inherits
# stdin. One leaks into the shell as a trailing `?` echo — harmless.
set -e
exec bin/temu.darwin-arm64.bin \
    -machine x86 \
    -m 512 \
    -kernel bin/bzImage-qemux86.bin \
    -initrd bin/initrd-alpine-x86 \
    -drive bin/alpine-standard-3.19.0-x86.iso -ro \
    -net-user \
    -stdin-prefix '\x04\x04\x04\x04\x04' \
    -append "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs"
