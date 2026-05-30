#!/bin/sh
# TINYEMU_X64_RIPSAMPLE=N — every Nth executed instruction, write the
# current RIP (and CS) to stderr. The default 10000 is fine for spot
# checks; lower it to find tight loops, higher it for less noise.
# Pair with `scripts/sym.sh <RIP>` to map samples back to kernel
# symbols.
: "${TINYEMU_X64_RIPSAMPLE:=10000}"
export TINYEMU_X64_RIPSAMPLE
DEBUG_NAME=ripsample
. "$(dirname "$0")/_runner.sh" "$@"
