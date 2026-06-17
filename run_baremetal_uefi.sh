#!/bin/sh
# Boot Return Infinity's BareMetal kernel via Pure64's *UEFI* loader, under
# OVMF, on the cpu/x86_64 long-mode emulator.
#
# This is the UEFI counterpart of run_baremetal.sh (which uses Pure64's BIOS
# MBR under SeaBIOS). The BIOS path caps the user payload at 8 KiB because
# the MBR loads a fixed 32 KiB via INT 13h. The UEFI path has no such cap:
# OVMF reads BOOTX64.EFI (any size) as a file from the FAT EFI System
# Partition, so the payload is bounded only by the loader's 60 KiB embedded
# region (Pure64 + kernel + payload), not by a fixed disk read.
#
# Usage:
#   ./run_baremetal_uefi.sh                              # boot with hello.bin payload
#   ./run_baremetal_uefi.sh "" bin/baremetal/your.bin    # custom payload
#   TINYEMU_BIOS_DEBUG=stderr ./run_baremetal_uefi.sh    # watch the OVMF log
#
# Boot chain (see Pure64 docs/Boot Process.md):
#   temu loads OVMF as -bios; OVMF runs SEC->PEI->DXE->BDS, scans the FAT
#   volume for \EFI\BOOT\BOOTX64.EFI and launches it. BOOTX64.EFI is
#   Pure64's hand-rolled PE32+ UEFI loader (uefi.sys) with a payload blob
#   spliced in at file offset 0x1000. The loader sets the video mode, copies
#   32 KiB from that blob to 0x8000, gets the UEFI memory map, and jumps to
#   0x8000 (the second-stage pure64-uefi.sys). Pure64 sets up the 64-bit
#   environment, then relocates the <=26 KiB after itself (the kernel +
#   payload) to 0x100000 and jumps there.
#
# Disk/blob layout (matches Pure64's `dd ... bs=4096 seek=1` recipe):
#   BOOTX64.EFI = uefi.sys, with [pure64-uefi.sys ++ kernel.sys ++ payload]
#   written at offset 0x1000 (the loader's PAYLOAD label; 60 KiB region).
#
# Inputs (in bin/baremetal/ — copy the first two from a built Pure64 tree,
# e.g. ~/Dev/Assembler/Pure64.git/bin/, via PURE64=... below):
#   uefi.sys          Pure64 UEFI loader (the PE32+ BOOTX64.EFI body)
#   pure64-uefi.sys   Pure64 second-stage loader (UEFI build)
#   kernel.sys        BareMetal kernel (<=20 KiB)
#   <payload.bin>     flat binary linked at ORG 0x1E0000 (default hello.bin)
#
# Env knobs:
#   MEM=1024                    guest RAM in MiB (default 256)
#   OVMF=bin/ovmf/OVMF_DEBUG.fd firmware (release OVMF.fd is the default and is
#                               ~20% faster + quiet; use the DEBUG build to trace
#                               the SEC/PEI/DXE firmware log)
#   PURE64=~/Dev/Assembler/Pure64.git   re-copy uefi.sys/pure64-uefi.sys from here
#   TINYEMU_BIOS_DEBUG=stderr   stream the OVMF SEC/PEI/DXE log
#
# STATUS (2026-06-11): WORKS — boots to the BareMetal kernel's
# "[ BareMetal ] ... system ready" banner. The whole chain is exercised on
# temu: OVMF reads the FAT ESP, validates Pure64's hand-rolled PE32+, loads
# it at IMAGE_BASE 0x400000, transfers control, installs the 4 fw_cfg ACPI
# tables, and provides a GOP; Pure64's loader prints "UEFI OK", relocates
# the kernel to 0x100000, and runs it.
#
# The one non-obvious requirement: Pure64's loader REQUIRES a Graphics
# Output Protocol (it does LocateProtocol(GOP) and bails to its `error`
# path — printing "UEFI Error" = msg_uefi+msg_error — if absent; it also
# requires ACPI, which temu's fw_cfg tables already provide). temu only
# exposes a GOP-capable display when a video device is enabled, so this
# script sets TINYEMU_STDVGA=1 by default (std-VGA Bochs DISPI -> OVMF's
# QemuVideoDxe -> GOP). No emulator code change was needed.
#
# Payload output + the LFB gotcha: the appended payload (hello.bin) runs
# fine and reaches 0x1E0000 — but its output goes wherever the kernel's
# b_output (API slot 0x100018) points. The BareMetal kernel's lfb_init
# REROUTES b_output from serial to the graphical framebuffer whenever an
# LFB is present (drivers/lfb/lfb.asm) — and we must enable one
# (TINYEMU_STDVGA=1) for Pure64's loader. temu is headless, so payload text
# written to the framebuffer is invisible. The kernel's own banner uses
# os_debug_string -> b_output_serial directly, so it always shows on serial.
#
# To SEE payload output over serial, build the BareMetal kernel with NO_LFB
# (it then leaves b_output on the serial port that serial_init set):
#   cd ~/Dev/Assembler/BareMetal.git/src
#   nasm -dNO_VGA -dNO_LFB kernel.asm -o ../../tinyemu-go.git/bin/baremetal/kernel.sys
# Verified end-to-end: with that kernel, hello.bin prints
# "Hello from a BareMetal payload!" on the serial console after
# "system ready". (The default kernel.sys here is that NO_LFB build.)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

MEM=${MEM:-256}
# Default to the release OVMF build: ~20% faster than OVMF_DEBUG.fd (no DEBUG()
# serial spew or assertions), which is most of the slowness lever short of a
# firmware snapshot. Set OVMF=.../OVMF_DEBUG.fd to trace the firmware boot.
OVMF="${OVMF:-$ROOT/bin/ovmf/OVMF.fd}"
[ -r "$OVMF" ] || OVMF="$ROOT/bin/ovmf/OVMF_DEBUG.fd" # fall back if release absent
DIR="$ROOT/bin/baremetal"
PAYLOAD=${2:-$DIR/hello.bin}

UEFI_LOADER="$DIR/uefi.sys"
PURE64_SYS="$DIR/pure64-uefi.sys"
KERNEL="$DIR/kernel.sys"
EFI="$DIR/BOOTX64.EFI"
ESP="$DIR/esp-uefi.img"

# Optionally refresh the Pure64 loader artifacts from a built Pure64 tree
# (PURE64=path to the Pure64 repo whose bin/ holds the freshly-built .sys).
if [ -n "$PURE64" ] && [ -d "$PURE64/bin" ]; then
    cp "$PURE64/bin/uefi.sys" "$UEFI_LOADER"
    cp "$PURE64/bin/pure64-uefi.sys" "$PURE64_SYS"
fi

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU (run: go build -o $TEMU ./cmd/temu)" >&2; exit 1; }
[ -r "$OVMF" ] || { echo "missing OVMF firmware $OVMF (see bin/ovmf/get_omvf.sh)" >&2; exit 1; }
for f in "$UEFI_LOADER" "$PURE64_SYS" "$KERNEL"; do
    [ -r "$f" ] || { echo "missing $f -- copy Pure64 UEFI artifacts into $DIR/ (see header)" >&2; exit 1; }
done
[ -r "$PAYLOAD" ] || { echo "missing payload $PAYLOAD" >&2; exit 1; }

# Rebuild BOOTX64.EFI + the ESP image only when an input changed: the ESP is
# missing/stale, the payload differs from the one last baked in, or any input
# (.sys/payload) is newer than the cached ESP. Building the ESP is slow
# (hdiutil create+attach+detach on macOS), so skip it when nothing changed.
MARKER="$DIR/.esp-inputs"
rebuild=0
{ [ -r "$ESP" ] && [ -r "$EFI" ]; } || rebuild=1
[ -r "$MARKER" ] && [ "$(cat "$MARKER" 2>/dev/null)" = "$PAYLOAD" ] || rebuild=1
for f in "$UEFI_LOADER" "$PURE64_SYS" "$KERNEL" "$PAYLOAD"; do
    [ "$f" -nt "$ESP" ] && rebuild=1
done

# --- Build a FAT EFI System Partition holding \EFI\BOOT\BOOTX64.EFI. ---
build_esp_darwin() {
    tmp="$DIR/.esp-build"
    rm -f "$tmp.dmg" "$ESP"
    hdiutil create -megabytes 64 -fs MS-DOS -volname BAREMETAL -layout NONE -o "$tmp" >/dev/null
    mnt=$(mktemp -d)
    hdiutil attach "$tmp.dmg" -mountpoint "$mnt" >/dev/null
    mkdir -p "$mnt/EFI/BOOT"
    COPYFILE_DISABLE=1 cp "$EFI" "$mnt/EFI/BOOT/BOOTX64.EFI"
    hdiutil detach "$mnt" >/dev/null
    rmdir "$mnt" 2>/dev/null || true
    mv "$tmp.dmg" "$ESP"
}

build_esp_mtools() {
    command -v mformat >/dev/null 2>&1 || {
        echo "need 'mtools' (mformat/mcopy) to build the ESP on $OS" >&2; exit 1; }
    rm -f "$ESP"
    dd if=/dev/zero of="$ESP" bs=1048576 count=64 status=none
    mformat -i "$ESP" -F -v BAREMETAL ::
    mmd -i "$ESP" ::/EFI ::/EFI/BOOT
    mcopy -i "$ESP" "$EFI" ::/EFI/BOOT/BOOTX64.EFI
}

if [ "$rebuild" -eq 1 ]; then
    # Splice [pure64-uefi.sys ++ kernel.sys ++ payload] into a copy of uefi.sys
    # at file offset 0x1000 (the loader's PAYLOAD label), then build the ESP.
    PAYLOAD_OFF=4096   # 0x1000
    blob="$DIR/.uefi-blob"
    cat "$PURE64_SYS" "$KERNEL" "$PAYLOAD" > "$blob"
    blobsz=$(wc -c < "$blob")
    # The loader copies 32 KiB from PAYLOAD to 0x8000 and Pure64 relocates the
    # <=26 KiB after itself; the embedded region is 60 KiB. Warn past that.
    if [ "$blobsz" -gt 61440 ]; then
        echo "[run_baremetal_uefi] WARNING: blob is ${blobsz} B > 60 KiB embedded region; will be truncated" >&2
    fi
    cp "$UEFI_LOADER" "$EFI"
    dd if="$blob" of="$EFI" bs=1 seek=$PAYLOAD_OFF conv=notrunc status=none
    rm -f "$blob"
    echo "[run_baremetal_uefi] BOOTX64.EFI = uefi.sys + ${blobsz} B blob @ 0x1000 (pure64+kernel+$(basename "$PAYLOAD"))"

    echo "[run_baremetal_uefi] building ESP image $ESP"
    case $OS in
        darwin) build_esp_darwin ;;
        *)      build_esp_mtools ;;
    esac
    printf '%s\n' "$PAYLOAD" > "$MARKER"
else
    echo "[run_baremetal_uefi] reusing ESP image $ESP (inputs unchanged)"
fi

echo "Starting BareMetal (UEFI/Pure64) under OVMF (x86_64, ${MEM} MiB) at: $(date)"
echo "  (exit temu with Ctrl-A x)"

# Pure64's UEFI loader requires a Graphics Output Protocol. temu only
# publishes one when a video device is enabled, so default the std-VGA
# Bochs DISPI device on (OVMF's QemuVideoDxe binds it and installs the
# GOP). Override with TINYEMU_STDVGA=0 to reproduce the no-GOP failure.
export TINYEMU_STDVGA="${TINYEMU_STDVGA:-1}"

# -apic: OVMF + Pure64 both need a software-enabled local APIC (Pure64
#        brings up SMP via the LAPIC). It is flag-gated in temu so legacy
#        PIC-only Linux boots are unaffected (machine/pc/lapic.go).
exec "$TEMU" -machine x86_64 -m "$MEM" -apic -bios "$OVMF" -drive "$ESP"
