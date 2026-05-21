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

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    echo "Usage: $0 <tinycore|alpine-debug> [bare]" >&2
    echo "  bare: drop straight to /bin/sh from initramfs (no Alpine init script)" >&2
    exit 1
fi

NAME=$1
VARIANT=${2:-}
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU" >&2; exit 1; }

case $NAME in
    tinycore)
        # Prefer the pre-decompressed ELF (vmlinux64) so we skip the
        # bzImage decompressor — see machine/pc/vmlinux64.go.
        if [ -r "$ROOT/bin/tinycore64/vmlinux64" ]; then
            KERNEL="$ROOT/bin/tinycore64/vmlinux64"
        else
            KERNEL="$ROOT/bin/tinycore64/vmlinuz64"
        fi
        INITRD=""  # try kernel-only first
        MEM=128
        # Match the isolinux.cfg default with output redirected to
        # COM1, no APIC/ACPI/SMP/KASLR. loglevel=8 maximises early
        # printk output so missing-opcode failures get a clear
        # context. console_msg_format=syslog/timestamp adds prefixes.
        APPEND="console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable cde"
        ;;
    alpine-debug)
        # Alpine 3.19 'virt' x86_64 kernel — extracted from
        # bin/iso/alpine-virt-3.19.1-x86-64.iso into bin/alpine64-debug/.
        # Used specifically for boot debugging because it ships a
        # full System.map-virt symbol table that the TinyCorePure64
        # kernel was stripped of. Pair with ./scripts/sym.sh to
        # resolve any RIP/CR2 from a fault trace to a Linux symbol.
        #
        # The pre-decompressed inner ELF (vmlinux-virt) is the path
        # of choice — same shortcut as tinycore's vmlinux64.
        if [ -r "$ROOT/bin/alpine64-debug/vmlinux-virt" ]; then
            KERNEL="$ROOT/bin/alpine64-debug/vmlinux-virt"
        else
            KERNEL="$ROOT/bin/alpine64-debug/vmlinuz-virt"
        fi
        INITRD="$ROOT/bin/alpine64-debug/initramfs-virt"
        ISO="$ROOT/bin/iso/alpine-virt-3.19.1-x86-64.iso"
        MEM=512
        # Matches run86_iso.sh's alpine path: attach the Alpine ISO as
        # virtio-blk-pci /dev/vda, tell Alpine's init it's the boot
        # media, load the virtio modules it needs to mount it.
        #
        # libata.force=disable ide=disable: skip the legacy IDE/SATA
        # probe (we have no IDE controller; the probe times out slowly).
        #
        # module.sig_enforce=0: skip per-module RSA-SHA256 verify
        # (no value here, expensive under software big-int math).
        APPEND="console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs module.sig_enforce=0"
        ;;
    *)
        echo "unknown OS '$NAME'" >&2
        exit 1
        ;;
esac

[ -r "$KERNEL" ] || { echo "missing kernel: $KERNEL" >&2; exit 1; }

# `bare` variant: bypass Alpine's /init script (which calls nlplug-findfs
# and would normally probe boot media), drop straight to /bin/sh from
# the initramfs. Useful when the ISO isn't attached or for diagnosing
# the kernel boot path in isolation.
case $VARIANT in
    "")
        ;;
    bare)
        APPEND="$APPEND rdinit=/bin/sh"
        # Keep the ISO attached so you can insmod virtio_blk and mount
        # /dev/vda manually from the shell — useful for debugging the
        # I/O path in isolation.
        echo "[run64_iso] bare mode: rdinit=/bin/sh (no Alpine init, raw busybox shell)"
        ;;
    *)
        echo "Unknown variant '$VARIANT' (expected: bare)" >&2
        exit 1
        ;;
esac

echo "Starting $NAME (x86_64) at: $(date)"

# Build the exec args. Use eval-friendly array semantics.
ARGS="-machine x86_64 -m $MEM -kernel $KERNEL"
[ -n "$INITRD" ] && [ -r "$INITRD" ] && ARGS="$ARGS -initrd $INITRD"
[ -n "$ISO" ] && [ -r "$ISO" ] && ARGS="$ARGS -drive $ISO -ro"
ARGS="$ARGS -net-user -append"

exec "$TEMU" $ARGS "$APPEND"
