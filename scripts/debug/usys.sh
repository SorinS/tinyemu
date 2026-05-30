#!/bin/sh
# TINYEMU_X64_USYS=1 — logs every user-mode SYSCALL with the SysV ABI
# arg registers and a small symbolic name table for common syscalls.
# Use to identify what a hung user process is blocked on (originally
# added to find nlplug-findfs / Alpine-init both ending up on
# futex(FUTEX_WAIT)).
#
# Typical use for the nlplug-futex investigation:
#   ./scripts/debug/usys.sh alpine 600        # broken upstream initrd
#   # Then grep for the futex syscall in /tmp/debug_usys.log:
#   grep -nE 'futex|SYS_futex' /tmp/debug_usys.log | tail -20
export TINYEMU_X64_USYS=1
DEBUG_NAME=usys
# Default to the upstream initrd (which hangs in nlplug) so this
# captures the bug case. Override via DEBUG_VARIANT= to point at
# a different boot.
: "${DEBUG_VARIANT:=upstream}"
export DEBUG_VARIANT
. "$(dirname "$0")/_runner.sh" "$@"
