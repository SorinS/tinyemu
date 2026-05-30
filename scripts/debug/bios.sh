#!/bin/sh
# TINYEMU_BIOS_DEBUG=1 — logs every SeaBIOS debug-port write (0x402).
# Only useful when booting via the BIOS shim (SeaBIOS path / MenuetOS).
# Has no effect on Alpine direct-kernel boot.
export TINYEMU_BIOS_DEBUG=1
DEBUG_NAME=bios
. "$(dirname "$0")/_runner.sh" "$@"
