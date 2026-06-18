#!/bin/sh
# Build (incrementally) a Go program with TamaGo and UEFI-boot it under OVMF on
# temu.
#
# This is the generic version of run_go-boot.sh: instead of booting go-boot's
# shell, it runs YOUR Go code as a UEFI application. It is the fastest way to
# write bare-metal Go and see it execute on the emulator.
#
# The image build lives in `make tamago` (scripts/build_tamago.sh) and is
# INCREMENTAL: the slow steps (TamaGo compile, hdiutil ESP build) only run when
# the source changed. This script delegates the build there and then boots the
# resulting bin/tamago/esp.img — so re-running with an unchanged program skips
# straight to the boot.
#
# Usage:
#   ./run_tamago.sh                  # build (if needed) + boot bin/tamago/main.go
#                                    #   (a sample is written there on first run)
#   ./run_tamago.sh path/to/app.go   # build (if needed) + boot a specific source
#   MEM=2048 ./run_tamago.sh         # override guest RAM (MiB)
#   make tamago TAMAGO_SRC=app.go    # just build the image (no boot)
#
# How it works
# ------------
# Your program imports github.com/usbarmory/go-boot/uefi/x64, which provides
# the UEFI bootstrap (the `cpuinit` entry point, UEFI Boot/Runtime Services,
# and a console wired to the firmware ConOut -> 16550 serial = temu
# stdout/stdin). TamaGo (a bare-metal Go toolchain, GOOS=tamago GOARCH=amd64)
# compiles it to an ELF; objcopy converts that to a PE32+ EFI application;
# it is dropped on a FAT EFI System Partition as \EFI\BOOT\BOOTX64.EFI; and
# OVMF (temu's -bios) finds and launches it — exactly the path go-boot uses,
# with your code as the payload. The full Go runtime is available: goroutines,
# channels, the garbage collector, fmt, time, etc.
#
# Requirements (paths overridable via env; see scripts/build_tamago.sh)
# --------------------------------------------------------------------
#   TAMAGO=~/Apps/tamago-go1.26.4/bin/go   the TamaGo compiler
#   GOBOOT=~/Dev/Go.Code/go-boot.git       a checkout of usbarmory/go-boot
#   objcopy from GNU binutils on PATH (macOS: brew install binutils)
#   OVMF=bin/ovmf/OVMF.fd                  UEFI firmware (see bin/ovmf/get_omvf.sh)
#
# Artifacts live in bin/tamago/: main.go (your source), app (ELF), app.efi
# (the UEFI app), esp.img (the FAT boot disk).

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/tamago"
MEM=${MEM:-1024}
SRC=${1:-$DIR/main.go}

# Prefer the firmware kept alongside this target's artifacts; fall back to
# the shared copy (and seed the local one from it) so bin/tamago is
# self-contained.
if [ -n "$OVMF" ]; then
    :
elif [ -r "$DIR/OVMF.fd" ]; then
    OVMF="$DIR/OVMF.fd"
elif [ -r "$ROOT/bin/ovmf/OVMF.fd" ]; then
    mkdir -p "$DIR"
    cp "$ROOT/bin/ovmf/OVMF.fd" "$DIR/OVMF.fd"
    OVMF="$DIR/OVMF.fd"
fi

[ -x "$TEMU" ] || { echo "missing emulator $TEMU (go build -o $TEMU ./cmd/temu)" >&2; exit 1; }
[ -r "$OVMF" ] || { echo "missing OVMF firmware $OVMF (see bin/ovmf/get_omvf.sh)" >&2; exit 1; }

# Build the app image (incremental — a no-op when the source is unchanged).
sh "$ROOT/scripts/build_tamago.sh" "$SRC"
ESP="$DIR/esp.img"
[ -r "$ESP" ] || { echo "missing $ESP (build failed?)" >&2; exit 1; }

echo "Starting TamaGo app under OVMF (x86_64, ${MEM} MiB) at: $(date)"
echo "  (exit temu with Ctrl-A x, or it powers off when the program ends)"

# -apic: OVMF asserts a software-enabled local APIC (flag-gated in temu so
#        legacy PIC-only Linux boots are unaffected — see machine/pc/lapic.go).
exec "$TEMU" -machine x86_64 -m "$MEM" -apic -bios "$OVMF" -drive "$ESP"
