#!/bin/sh
# Boot MenuetOS from a floppy via SeaBIOS on the x86_64 emulator.
#
# Usage:
#   ./run_menuet.sh                 # MenuetOS 64 (bin/menuet64/M6416000.IMG)
#   IMG=path/to/floppy.img ./run_menuet.sh
#
# Boot path: temu runs SeaBIOS (real mode), which reads the MenuetOS boot
# floppy through the FDC (-fda) and hands off to MenuetOS's loader. This is
# the legacy-BIOS counterpart to the OVMF/UEFI path.
#
# IMPORTANT — the VESA wall: MenuetOS sets a graphics mode early. Two ways
# that can go:
#   * If it programs the Bochs VBE "dispi" registers directly (ports
#     0x1CE/0x1CF or the std-VGA MMIO), the std-VGA device this script
#     enables (TINYEMU_STDVGA=1) services it.
#   * If it calls the real-mode VESA BIOS (INT 10h, AX=4Fxx), it needs a
#     VGA BIOS option ROM — which SeaBIOS does not load here yet (the
#     std-VGA device provides the hardware, not the INT 10h interface).
#     In that case expect it to stop at the same VESA probe as before;
#     wiring a VGA BIOS (SeaVGABIOS / bin/halfix.git/vgabios.bin) on top of
#     std-VGA is the next step.
# Either way temu is headless: there's no window — this proves the boot
# reaches/passes the graphics probe, it doesn't display a desktop.
#
# Env knobs:
#   IMG=path                    floppy image to boot (default: MenuetOS 64)
#   MEM=256                     guest RAM in MiB (default 128)
#   TINYEMU_STDVGA=0            disable the std-VGA framebuffer (default on)
#   TINYEMU_BIOS_DEBUG=stderr   watch the SeaBIOS log (off by default)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

BIOS="$ROOT/bin/seabios/bios.bin"
IMG=${IMG:-$ROOT/bin/menuet64/M6416000.IMG}
MEM=${MEM:-128}

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU" >&2; exit 1; }
[ -r "$BIOS" ] || { echo "missing SeaBIOS $BIOS (run scripts/extract_seabios.sh)" >&2; exit 1; }
[ -r "$IMG" ]  || { echo "missing floppy image $IMG" >&2; exit 1; }

# Provide the QEMU std-VGA (Bochs VBE) graphics device so MenuetOS's
# graphics-mode setup has hardware to talk to. Override with
# TINYEMU_STDVGA=0 to see the pre-framebuffer behaviour.
: "${TINYEMU_STDVGA:=1}"
export TINYEMU_STDVGA

echo "Booting MenuetOS via SeaBIOS (x86_64, ${MEM} MiB) at: $(date)"
echo "  floppy: $IMG   (std-VGA=${TINYEMU_STDVGA}; exit temu with Ctrl-A x)"

exec "$TEMU" -machine x86_64 -m "$MEM" -bios "$BIOS" -fda "$IMG"
