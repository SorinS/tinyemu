#!/bin/sh
# Boot FreeBSD (aarch64) on the cpu/arm64 "virt" board via UEFI firmware.
#
# Unlike Linux (a flat Image we jump to directly), FreeBSD arm64 boots through
# UEFI: edk2 firmware (-bios) starts the CPU at its reset vector, finds the EFI
# boot partition on the disk, and chain-loads FreeBSD's loader.efi -> kernel.
#
# This boots the pre-installed UFS disk image to a working FreeBSD shell. For an
# interactive root shell: at the loader's beastie menu press a key, then "2"
# (Boot Single user); at "Enter full pathname of shell or RETURN for /bin/sh:"
# press Enter -> root@:/ #. Multiuser autoboot reaches login: in ~40s.
#
# Usage:
#   ./run_freebsd-arm64.sh                 # boot the FreeBSD UFS disk image
#   ./run_freebsd-arm64.sh other.raw       # boot a different disk image / ISO
#   MEM=4096 ./run_freebsd-arm64.sh        # guest RAM in MiB (default 2048)
#
# Assets (in bin/freebsd-arm64/):
#   edk2-code.fd                                edk2/UEFI firmware (from qemu)
#   FreeBSD-14.3-RELEASE-arm64-aarch64-ufs.raw  the pre-installed disk image
#     (from download.freebsd.org/releases/VM-IMAGES/14.3-RELEASE/aarch64/Latest/
#      FreeBSD-14.3-RELEASE-arm64-aarch64-ufs.raw.xz, decompressed with xz -d)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/freebsd-arm64"
FW="$DIR/edk2-code.fd"
DISK="${1:-$DIR/FreeBSD-14.3-RELEASE-arm64-aarch64-ufs.raw}"
MEM=${MEM:-2048}

[ -x "$TEMU" ] || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$FW" ]   || { echo "missing UEFI firmware $FW" >&2; exit 1; }
[ -r "$DISK" ] || { echo "missing disk image $DISK" >&2; exit 1; }

echo "[run_freebsd-arm64] booting FreeBSD via UEFI (${MEM} MiB) on the virt board"
echo "  firmware: $FW"
echo "  disk:     $DISK"
exec "$TEMU" -machine virt -m "$MEM" -bios "$FW" -drive "$DISK"
