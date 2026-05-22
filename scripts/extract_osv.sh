#!/bin/sh
# Pull OSv loader artifacts from the upstream GitHub release.
# Idempotent — outputs into bin/osv/.
#
# OSv (https://osv.io) is a unikernel: a single-application OS designed to
# run on a hypervisor with virtio-only hardware. No ACPI, no APIC, no
# BIOS — exactly the profile of our x86_64 backend.
#
# Outputs:
#   bin/osv/loader.elf          — standard OSv loader (multiboot ELF, ~6 MB)
#   bin/osv/loader-microvm.elf  — microvm variant (no ACPI/BIOS deps, ~5 MB)
#
# Source: https://github.com/cloudius-systems/osv/releases (v0.57.0 "Magpie")

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/bin/osv"
RELEASE="v0.57.0"
BASE="https://github.com/cloudius-systems/osv/releases/download/$RELEASE"

mkdir -p "$OUT"

fetch() {
    asset=$1
    out=$2
    if [ -f "$out" ] && [ ! "$0" -nt "$out" ]; then
        return
    fi
    echo "[extract_osv] fetching $asset"
    tmp=$(mktemp -t osv.XXXXXX)
    if ! curl -fsSL -o "$tmp.gz" "$BASE/$asset"; then
        rm -f "$tmp" "$tmp.gz"
        echo "[extract_osv] failed to fetch $asset" >&2
        return 1
    fi
    gunzip -f "$tmp.gz"
    mv "$tmp" "$out"
}

fetch osv-loader.elf.x86_64.gz "$OUT/loader.elf"
fetch osv-loader-microvm.elf.x86_64.gz "$OUT/loader-microvm.elf"

echo "[extract_osv] OK"
echo "  $OUT/loader.elf          (standard build)"
echo "  $OUT/loader-microvm.elf  (microvm build — no ACPI/BIOS deps)"
