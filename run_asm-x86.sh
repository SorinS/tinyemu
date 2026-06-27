#!/bin/sh
# Assemble & run an asm snippet as 32-bit x86 (BITS 32), printing the final register state.
# Forces the ISA via -cpu-arch x86_32; temu REJECTS the source if its own BITS
# directive, "; arch:" directive, or detected ISA contradicts 32-bit x86 (BITS 32) (e.g. a
# "BITS 32" or RISC-V mnemonics handed to the wrong wrapper).
#
# Usage:
#   ./run_asm-x86.sh <file.asm> [steps]   # run a file (steps = optional cap)
#   ./run_asm-x86.sh -          [steps]   # read the source from stdin
#
# Arch-specific wrapper around:  temu -cpu-arch x86_32 -run-asm <file> [-asm-steps N]
# (./run_asm.sh is the ISA-auto-detecting equivalent.)
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
SRC="${1:--}"
STEPS="${2:-0}"
OS=$(uname -s | tr 'A-Z' 'a-z')
ARCH=$(uname -m)
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"
[ -x "$TEMU" ] || { echo "$TEMU not found, did you run make?" >&2; exit 1; }
exec "$TEMU" -cpu-arch x86_32 -asm-steps "$STEPS" -run-asm "$SRC"
