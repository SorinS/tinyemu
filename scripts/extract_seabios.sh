#!/bin/sh
# Stage a SeaBIOS image at bin/seabios/bios.bin.
#
# SeaBIOS is the open-source BIOS implementation that QEMU/KVM ship by
# default. We need it to boot floppy/HDD images that depend on legacy
# BIOS calls (INT 10h/13h/16h/etc.) — MenuetOS, FreeDOS, etc.
#
# Priority:
#   1) Reuse the homebrew-qemu copy if present
#      (/opt/homebrew/share/qemu/bios-256k.bin) — no download needed.
#   2) Reuse the linux-qemu copy if present
#      (/usr/share/seabios/bios.bin or /usr/share/qemu/bios-256k.bin).
#   3) Fall back to downloading from QEMU's pc-bios repo.
#
# Output: bin/seabios/bios.bin (256 KB).

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/bin/seabios"
mkdir -p "$OUT"
DEST="$OUT/bios.bin"

if [ -f "$DEST" ] && [ ! "$0" -nt "$DEST" ]; then
    echo "[extract_seabios] up to date: $DEST"
    exit 0
fi

CANDIDATES="
/opt/homebrew/share/qemu/bios-256k.bin
/usr/share/seabios/bios.bin
/usr/share/qemu/bios-256k.bin
/opt/local/share/qemu/bios-256k.bin
"
for src in $CANDIDATES; do
    if [ -f "$src" ]; then
        echo "[extract_seabios] copying from $src"
        cp "$src" "$DEST"
        sz=$(wc -c < "$DEST")
        echo "[extract_seabios] OK ($sz bytes)"
        exit 0
    fi
done

URL="https://github.com/qemu/qemu/raw/master/pc-bios/bios-256k.bin"
echo "[extract_seabios] no local SeaBIOS found, fetching from QEMU upstream"
if ! curl -fsSL -o "$DEST" "$URL"; then
    rm -f "$DEST"
    echo "[extract_seabios] download failed; install qemu locally (brew install qemu)" >&2
    exit 1
fi
sz=$(wc -c < "$DEST")
echo "[extract_seabios] OK ($sz bytes) from $URL"
