#!/bin/sh
# Boot Return Infinity's BareMetal kernel via Pure64's BIOS loader,
# under our SeaBIOS shim.
#
# Usage:
#   ./run_baremetal.sh                                  # plain boot, no payload
#   ./run_baremetal.sh trace                            # bios+rip debug knobs on
#   ./run_baremetal.sh "" bin/baremetal/hello.bin       # boot with a user payload
#   ./run_baremetal.sh trace bin/baremetal/hello.bin    # both
#
# A payload is a flat binary linked at ORG 0x1E0000 (where BareMetal's
# init_sys copies it before jumping). Maximum payload size is 8 KiB
# in this boot path: Pure64's BIOS MBR (bios-novideo.sys) loads exactly
# 32 KiB from disk into 0x8000, of which 4 KiB is the Pure64 loader
# itself and 20 KiB is the BareMetal kernel — leaving 8 KiB for the
# payload that got appended to the kernel image. BareMetal's init_sys
# copies a 16 KiB window from "kernel-end" to 0x1E0000, but bytes
# past the 8-KiB disk-load mark are uninitialised RAM, so anything
# bigger than 8 KiB will execute garbage past its real bytes. Larger
# payloads need a BMFS-formatted disk (Pure64's bios.sys path) or a
# small bootstrap that pulls more bytes off the disk via b_nvs_read.
#
# The payload calls into the kernel via the fixed pointer slots at
# 0x100010 onwards — see api/libBareMetal.asm in the BareMetal source
# and docs/baremetal-payload-example/hello.asm for a worked example.
#
# Artefact layout in bin/baremetal/:
#   bios-novideo.sys           MBR boot sector (512 B; reads sectors 16..63)
#   pure64-bios-novideo.sys    second-stage loader at sector 16 (4 KiB)
#   kernel.sys                 BareMetal kernel (padded to exactly 20 KiB)
#   <your-payload.bin>         appended to kernel.sys on the disk image
#
# We assemble these into a flat 1 MiB disk image at runtime so SeaBIOS
# sees a sane CHS geometry (s=2048; anything below ~1000 sectors makes
# SeaBIOS treat the disk as unbootable). The image is regenerated every
# run so updates to the input binaries pick up automatically.
#
# Why SeaBIOS? Pure64's `bios-*.sys` are BIOS MBRs that call INT 13h
# for disk reads and INT 15h for the e820 memory map; they need a real
# BIOS underneath. Our SeaBIOS path is the same shim used to bring up
# MenuetOS in machine/pc/floppy.go + the GDT walk fix from 106e8b1.

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS="$(uname -s | tr A-Z a-z)"
ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
BIOS="$ROOT/bin/seabios/bios.bin"

[ -x "$TEMU" ] || { echo "missing emulator: $TEMU (run 'go build -o $TEMU ./cmd/temu')" >&2; exit 1; }
[ -r "$BIOS" ] || { echo "missing SeaBIOS image: $BIOS" >&2; exit 1; }

MBR="$ROOT/bin/baremetal/bios-novideo.sys"
LOADER="$ROOT/bin/baremetal/pure64-bios-novideo.sys"
KERNEL="$ROOT/bin/baremetal/kernel.sys"
for f in "$MBR" "$LOADER" "$KERNEL"; do
    [ -r "$f" ] || { echo "missing $f -- build Pure64 + BareMetal and copy into bin/baremetal/" >&2; exit 1; }
done

# Optional second arg: a flat-binary payload to append after kernel.sys.
# BareMetal's init_sys checks the first qword past KERNELSIZE; non-zero
# triggers the copy to 0x1E0000 and the start_payload jump.
PAYLOAD="${2:-}"
if [ -n "$PAYLOAD" ]; then
    [ -r "$PAYLOAD" ] || { echo "missing payload: $PAYLOAD" >&2; exit 1; }
    psize=$(wc -c <"$PAYLOAD" | tr -d ' ')
    if [ "$psize" -gt 8192 ]; then
        echo "payload $PAYLOAD is ${psize} bytes; the Pure64+BareMetal boot path" >&2
        echo "caps at 8 KiB (DAP_SECTORS=64 in Pure64's MBR + KERNELSIZE=20KiB in" >&2
        echo "BareMetal). Bigger payloads need a BMFS disk or an in-payload" >&2
        echo "bootstrap that reads more sectors via b_nvs_read." >&2
        exit 1
    fi
    echo "[run_baremetal] payload: $PAYLOAD ($psize bytes)"
fi

# Build the disk image at /tmp so we don't dirty the working tree. The
# layout matches Pure64's MBR DAP defaults: sector 0 = MBR, sector 16 =
# loader, sector 24 = kernel; padded out to 1 MiB so SeaBIOS computes a
# nonzero cylinder count.
IMG="/tmp/temu-baremetal.img"
(
    cat "$MBR"
    dd if=/dev/zero bs=512 count=15 status=none
    cat "$LOADER"
    cat "$KERNEL"
    if [ -n "$PAYLOAD" ]; then
        cat "$PAYLOAD"
    fi
    dd if=/dev/zero bs=512 count=2000 status=none
) > "$IMG"
# Truncate to exactly 1 MiB
if command -v truncate >/dev/null 2>&1; then
    truncate -s 1048576 "$IMG"
else
    # macOS truncate ships under coreutils as gtruncate; if neither is
    # available, fall back to dd seek/write (slower but portable).
    dd if=/dev/zero of="$IMG" bs=1 count=1 seek=1048575 conv=notrunc status=none
fi

echo "[run_baremetal] disk image: $IMG ($(wc -c <"$IMG") bytes)"

case "${1:-}" in
    trace)
        # SeaBIOS log to ./tinyemu-bios.log + sampled RIPs to stderr.
        # Useful when investigating "boot doesn't print anything" -- the
        # boot may already be alive but past Pure64's pre-kernel checks.
        export TINYEMU_BIOS_DEBUG=1
        export TINYEMU_X64_RIPSAMPLE=200000
        echo "[run_baremetal] trace mode: BIOS log + RIP sampling enabled"
        ;;
    "")
        ;;
    *)
        echo "Usage: $0 [trace] [payload-binary]" >&2
        exit 1
        ;;
esac

exec "$TEMU" -machine x86_64 -m 128 \
    -bios "$BIOS" \
    -drive "$IMG"
