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
    echo "Usage: $0 <tinycore|alpine|alpine-debug> [upstream|bare|single|nonlplug|nohw|nomodloop|fast|superfast|nonlplug-fast]" >&2
    echo "  alpine       : Alpine-standard x86_64 — defaults to initrd.nonlplug (boots to login)" >&2
    echo "  alpine-debug : Alpine-virt x86_64 with full System.map for fault triage" >&2
    echo "  upstream     : use the unpatched Alpine initrd — will hang in nlplug (diagnostic only)" >&2
    echo "  bare         : drop straight to /bin/sh from initramfs" >&2
    echo "  nohw/.../fast: use patched initrd variant from bin/alpine64/" >&2
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
        # Prefer the patched initramfs that adds a ttyS0 auto-login
        # getty (see scripts/extract_tinycore64.sh). Without it
        # upstream's inittab only respawns getty on tty1, so the
        # shell ends up on a VGA console we don't model and the
        # boot looks "hung" after "login[…]: root login on 'tty1'".
        # Falls back to the upstream corepure64.gz if the extract
        # script hasn't been run yet.
        "$ROOT/scripts/extract_tinycore64.sh"
        if [ -r "$ROOT/bin/tinycore64/corepure64-serial.gz" ]; then
            INITRD="$ROOT/bin/tinycore64/corepure64-serial.gz"
        else
            INITRD="$ROOT/bin/tinycore64/corepure64.gz"
        fi
        MEM=128
        # Match the isolinux.cfg default with output redirected to
        # COM1, no APIC/ACPI/SMP/KASLR. loglevel=8 maximises early
        # printk output so missing-opcode failures get a clear
        # context. console_msg_format=syslog/timestamp adds prefixes.
        APPEND="console=ttyS0,115200 loglevel=8 earlyprintk=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable cde"
        ;;
    alpine)
        # Alpine-standard x86_64 — same boot path as run86_iso.sh's
        # alpine target. The /init script + initramfs were proven to
        # boot end-to-end on the 32-bit emulator; reusing the standard
        # ISO's flow (rather than alpine-virt's) on x86_64 gives the
        # same shell-with-OpenRC experience.
        #
        # Default to the patched `initrd.nonlplug` because the upstream
        # initrd's nlplug-findfs hangs indefinitely on x86_64 — both
        # the helper and Alpine's init end up on futex(FUTEX_WAIT),
        # most likely a still-open atomic-ordering / lock-free op bug
        # in cpu/x86_64 (see scripts/extract_alpine64.sh nonlplug build
        # comment + project memory). The nonlplug variant replaces the
        # nlplug-findfs call with a direct `mount /dev/vda /media/cdrom`
        # and lets the rest of Alpine's /init + switch_root + OpenRC
        # proceed. Use the `upstream` variant below to reproduce the
        # nlplug hang for diagnosis.
        "$ROOT/scripts/extract_alpine64.sh"
        # Prefer the pre-decompressed ELF (extract_alpine64.sh produces it)
        # to skip the in-guest bzImage self-decompressor — much faster to the
        # kernel banner under the interpreter. Falls back to the compressed
        # vmlinuz if extraction wasn't possible.
        if [ -r "$ROOT/bin/alpine64/vmlinux" ]; then
            KERNEL="$ROOT/bin/alpine64/vmlinux"
        else
            KERNEL="$ROOT/bin/alpine64/vmlinuz"
        fi
        INITRD="$ROOT/bin/alpine64/initrd.nonlplug"
        ISO="$ROOT/bin/alpine/alpine-standard-3.23.4-x86_64.iso"
        MEM=512
        # modprobe.blacklist: keep the kernel from auto-loading drivers
        # for hardware we don't emulate. ata_piix in particular has a
        # ~60-second probe timeout per phantom port. usb-storage is
        # similar — usbdelay=1 helps but blacklisting is faster.
        APPEND="console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable libata.force=disable ide=disable alpine_dev=vda:iso9660 usbdelay=1 modules=virtio_pci,virtio_blk,virtio_net,loop,squashfs module.sig_enforce=0 modprobe.blacklist=ata_piix,pata_acpi,usb-storage,usbhid"
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
        echo "[run64_iso] bare mode: rdinit=/bin/sh (no init scripts, raw busybox shell)"
        ;;
    shell)
        # TinyCore-specific: TinyCore's inittab respawns getty/login on
        # tty1 only — without a VGA console there's no way to reach the
        # shell over the serial line, so the boot looks "hung" after
        # "login[…]: root login on 'tty1'". rdinit=/bin/sh sidesteps
        # the whole TinyCore init chain (udev probes, extension
        # loading, etc.) and gives an immediate busybox shell on
        # ttyS0 = stdin/stdout. ~2.5 minutes to the `/ #` prompt.
        APPEND="$APPEND rdinit=/bin/sh"
        echo "[run64_iso] shell mode: rdinit=/bin/sh (no TinyCore init, raw busybox shell on serial)"
        ;;
    single)
        # Drop into Alpine's single-user shell early in /init, BEFORE
        # nlplug-findfs runs. Lets you poke around the partially-set-up
        # initramfs without waiting on hardware probes.
        APPEND="$APPEND single"
        echo "[run64_iso] single mode: stops at single-user shell pre-nlplug"
        ;;
    nonlplug)
        # Patched initrd that replaces the nlplug-findfs call with a
        # direct `mount /dev/vda /media/cdrom`. Works around the
        # x86_64 nlplug-findfs hang while keeping the rest of Alpine's
        # /init flow (sysroot tmpfs, switch_root, OpenRC). This is now
        # the alpine target's default; the variant is kept as an
        # explicit override.
        candidate="$ROOT/bin/alpine64/initrd.nonlplug"
        if [ -f "$candidate" ]; then
            INITRD="$candidate"
            echo "[run64_iso] using nonlplug initrd ($candidate)"
        else
            echo "[run64_iso] warning: nonlplug initrd missing — run scripts/extract_alpine64.sh" >&2
        fi
        ;;
    upstream)
        # Switch the alpine target back to the unpatched upstream
        # initrd. WARNING: this WILL hang at "Mounting boot media:"
        # because nlplug-findfs blocks on a futex that never resolves
        # under our emulator. Only useful for reproducing the hang for
        # diagnosis (pair with TINYEMU_X64_USYS=1).
        candidate="$ROOT/bin/alpine64/initrd"
        if [ -f "$candidate" ]; then
            INITRD="$candidate"
            echo "[run64_iso] using UPSTREAM initrd ($candidate) — will hang in nlplug-findfs (diagnostic only)" >&2
        fi
        ;;
    nohw|nomodloop|fast|superfast|nonlplug-fast)
        # Use the matching patched-initrd variant (built by
        # scripts/extract_alpine64.sh). Each variant skips slow OpenRC
        # services to shorten boot time.
        candidate="$ROOT/bin/alpine64/initrd.$VARIANT"
        if [ -f "$candidate" ]; then
            INITRD="$candidate"
            echo "[run64_iso] using $VARIANT initrd ($candidate)"
        else
            echo "[run64_iso] warning: '$VARIANT' has no effect (missing $candidate)" >&2
        fi
        ;;
    *)
        echo "Unknown variant '$VARIANT' (expected: upstream|bare|shell|single|nonlplug|nohw|nomodloop|fast|superfast|nonlplug-fast)" >&2
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
