#!/bin/sh
# TINYEMU_VIRTIO_PCI_DEBUG=1 — logs every virtio-pci R/W (status, PFN
# writes, queue notifies). Use to confirm whether the kernel is issuing
# requests the device never completes (request-side hang) vs whether
# the kernel has gone quiet (e.g. waiting on something else).
export TINYEMU_VIRTIO_PCI_DEBUG=1
DEBUG_NAME=virtio_pci
. "$(dirname "$0")/_runner.sh" "$@"
