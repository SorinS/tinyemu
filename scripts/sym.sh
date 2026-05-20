#!/bin/sh
# Resolve a kernel virtual address to "symbol+offset" via System.map.
#
# Usage:
#   ./scripts/sym.sh <hex-address>            — uses default System.map
#   ./scripts/sym.sh <hex-address> <map-file> — explicit map file
#
# Examples:
#   ./scripts/sym.sh ffffffff8235c6cc
#   ./scripts/sym.sh ffffffff8235c6cc bin/alpine64-debug/System.map-virt
#
# Prints e.g. "x86_64_start_kernel+0x52c" — the nearest symbol whose
# address is ≤ the queried address, plus the byte offset into it.

addr_arg="$1"
map="${2:-$(dirname "$0")/../bin/alpine64-debug/System.map-virt}"

if [ -z "$addr_arg" ] || [ ! -f "$map" ]; then
    echo "usage: $0 <hex-address> [map-file]" >&2
    exit 1
fi

addr_arg=${addr_arg#0x}

python3 - "$addr_arg" "$map" <<'PY'
import sys

target_hex = sys.argv[1]
map_path = sys.argv[2]
target = int(target_hex, 16)

best = (0, "??", "?")
with open(map_path) as f:
    for line in f:
        parts = line.split()
        if len(parts) != 3 or len(parts[0]) != 16:
            continue
        addr = int(parts[0], 16)
        if addr > target:
            break
        best = (addr, parts[2], parts[1])

off = target - best[0]
print(f"{best[1]}+{off:#x} ({best[2]}, base={best[0]:#x})")
PY
