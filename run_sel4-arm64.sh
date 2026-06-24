#!/bin/sh
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
DIR="$ROOT/bin/sel4-arm64"
IMAGE="$DIR/sel4-arm64"
# seL4's elfloader carries its own DTB (it ignores the one we pass) declaring
# RAM [0x40000000..0x80000000) = 1 GiB, and allocates the root task's paging
# structures near the top of it. With less RAM those land in an unbacked gap,
# the writes vanish, and the kernel asserts in map_it_pd_cap. Default to 1 GiB.
MEM=${MEM:-1024}
[ -x "$TEMU" ] || { echo "missing emulator $TEMU" >&2; exit 1; }
[ -r "$IMAGE" ] || { echo "missing image $IMAGE" >&2; exit 1; }
exec "$TEMU" -machine virt -m "$MEM" -kernel "$IMAGE"
