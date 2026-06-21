#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
DIR="$ROOT/bin/sel4-arm64"
IMAGE="$DIR/sel4-arm64"
MEM=${MEM:-512}
[ -x "$TEMU" ] || { echo "missing emulator $TEMU" >&2; exit 1; }
[ -r "$IMAGE" ] || { echo "missing image $IMAGE" >&2; exit 1; }
exec "$TEMU" -machine virt -m "$MEM" -kernel "$IMAGE"
