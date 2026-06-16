#!/bin/sh
# Assemble and run an assembly snippet in the temu emulator, printing the final
# register state. The ISA -- x86-64, 32-bit x86 (a "BITS 32" directive), or
# RISC-V -- is auto-detected from the source. Uses the in-tree assembler plus
# the asm/emu backend; no external assembler needed.
#
# Usage:
#   ./run_asm.sh <file.asm> [steps]    # run a file (steps = optional step cap)
#   ./run_asm.sh -                     # read the source from stdin (a string)
#
# This is a thin wrapper around:  temu -run-asm <file> [-asm-steps N]
set -e
ROOT="$(cd "$(dirname "$0")" && pwd)"
SRC="${1:--}"
STEPS="${2:-0}"

OS=$(uname -s | tr 'A-Z' 'a-z')
ARCH=$(uname -m)
TEMU=bin/temu.${OS}-${ARCH}.bin
[ ! -x "$TEMU" ] && echo "$TEMU not found, did you run make?" && exit 1
$TEMU -asm-steps "$STEPS" -run-asm "$SRC"
