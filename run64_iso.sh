#!/bin/sh
# Boot a 64-bit guest OS via the cpu/x86_64 long-mode emulator.
#
# Usage:
#   ./run64_iso.sh tinycore
#
# This is the 64-bit counterpart of run_iso.sh. The cpu/x86_64
# backend does not yet model real-mode opcodes — instead the chassis
# follows the AMD64 "direct kernel boot" protocol: it parses the
# bzImage header, sets up identity-mapped 4-level paging, drops the
# CPU directly into long mode with CS.L=1, and jumps to
# protected_mode_start + 0x200 (the startup_64 entry).
#
# Many opcodes the kernel exercises haven't been wired yet (SSE/SSE2,
# REP MOVS/STOS/SCAS, ADC/SBB, byte ALU forms, port I/O). Expect the
# first run to hit ErrNotImplemented somewhere in the kernel's early
# bring-up; the console output identifies the next opcode to add.

set -e

if [ $# -ne 1 ]; then
    echo "Usage: $0 <tinycore>" >&2
    exit 1
fi

NAME=$1
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU" >&2; exit 1; }

case $NAME in
    tinycore)
        KERNEL="$ROOT/bin/tinycore64/vmlinuz64"
        INITRD=""  # try kernel-only first
        MEM=128
        # Match the isolinux.cfg default with output redirected to
        # COM1, no APIC/ACPI/SMP/KASLR. loglevel=8 maximises early
        # printk output so missing-opcode failures get a clear
        # context. console_msg_format=syslog/timestamp adds prefixes.
        APPEND="console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable cde"
        ;;
    *)
        echo "unknown OS '$NAME'" >&2
        exit 1
        ;;
esac

[ -r "$KERNEL" ] || { echo "missing kernel: $KERNEL" >&2; exit 1; }

echo "Starting $NAME (x86_64) at: $(date)"

if [ -n "$INITRD" ] && [ -r "$INITRD" ]; then
    exec "$TEMU" -machine x86_64 -m "$MEM" -kernel "$KERNEL" -initrd "$INITRD" -net-user -append "$APPEND"
else
    exec "$TEMU" -machine x86_64 -m "$MEM" -kernel "$KERNEL" -net-user -append "$APPEND"
fi
