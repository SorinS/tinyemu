#!/bin/sh
# TINYEMU_X86_USERPF=1 — when the kernel takes a user-mode PF to an
# address < 0x1000, dump registers + memory near EAX/ESI/EDI. Originally
# added to localise NULL derefs in Alpine userspace; still useful for
# any "userspace crashes early" question.
export TINYEMU_X86_USERPF=1
DEBUG_NAME=userpf
. "$(dirname "$0")/_runner.sh" "$@"
