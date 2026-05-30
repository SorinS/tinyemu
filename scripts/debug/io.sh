#!/bin/sh
# TINYEMU_X86_IO_DEBUG=1 — logs every IN/OUT port access. Very chatty;
# pipe through grep when reviewing. Useful for finding "kernel pokes
# port X, nothing happens" device-side hangs (PIC, PIT, serial, virtio
# port-IO).
export TINYEMU_X86_IO_DEBUG=1
DEBUG_NAME=io
. "$(dirname "$0")/_runner.sh" "$@"
