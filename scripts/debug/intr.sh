#!/bin/sh
# TINYEMU_X64_INTR=1 — logs every LIDT load and every interrupt the
# emulator delivers (vector, IDTR state, gate bytes, new RIP/CS).
# Useful when the kernel goes idle waiting for an IRQ that the
# emulator either never raises or delivers to the wrong vector.
export TINYEMU_X64_INTR=1
DEBUG_NAME=intr
. "$(dirname "$0")/_runner.sh" "$@"
