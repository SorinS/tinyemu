#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
DIR="$ROOT/bin/nuttx-riscv"
CFG="$DIR/nuttx-riscv.cfg"
[ -x "$TEMU" ] || { echo "missing emulator $TEMU" >&2; exit 1; }
exec "$TEMU" "$CFG"
