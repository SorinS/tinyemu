#!/bin/sh
# Boot TinyCore Linux from the extracted vmlinuz + core.gz initramfs.
# Run `tar xf bin/TinyCore-17.0.iso boot/vmlinuz boot/core.gz` if you
# haven't already extracted these into bin/vmlinuz-tinycore /
# bin/initrd-tinycore.
set -e
exec bin/temu.darwin-arm64.bin \
    -machine x86 \
    -m 256 \
    -kernel bin/vmlinuz-tinycore \
    -initrd bin/initrd-tinycore \
    -net-user \
    -append "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable text"
