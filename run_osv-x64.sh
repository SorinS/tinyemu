#!/bin/sh
# Launch OSv on cpu/x86_64 via the PVH boot protocol.
#
# OSv (https://osv.io) is a single-application unikernel. The release
# tarball lives at:
#   https://github.com/cloudius-systems/osv/releases/tag/v0.57.0
# Run scripts/extract_osv.sh to fetch a prebuilt loader into bin/osv/.
#
# Usage:
#   ./run_osv.sh                       # standard loader, native console
#   ./run_osv.sh microvm               # microvm loader (smaller, no ACPI deps)
#
# OSv expects a kernel command line that's passed via PVH start_info.
# By default we let it boot to its native shell ("/").

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS="$(uname -s | tr A-Z a-z)"
ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
[ -x "$TEMU" ] || { echo "missing $TEMU — run 'make build'" >&2; exit 1; }

VARIANT="${1:-standard}"
case "$VARIANT" in
    standard) KERNEL="$ROOT/bin/osv-x64/loader.elf" ;;
    microvm)  KERNEL="$ROOT/bin/osv-x64/loader-microvm.elf" ;;
    *)        echo "Usage: $0 [standard|microvm]" >&2; exit 1 ;;
esac
[ -f "$KERNEL" ] || { echo "missing $KERNEL — run scripts/extract_osv.sh" >&2; exit 1; }

# OSv kernel cmdline. "--nopci" if you want to skip our virtio probe
# (helpful for first-attempt debug). Default: ask for the native CLI.
APPEND="${OSV_APPEND:---verbose --early-console --console=serial --nopci /}"

echo "Starting OSv ($VARIANT) at: $(date)"
echo "[run_osv] kernel: $KERNEL"
echo "[run_osv] append: $APPEND"

exec "$TEMU" -machine x86_64 -m 512 -kernel "$KERNEL" -append "$APPEND"
