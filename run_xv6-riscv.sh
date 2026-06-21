#!/bin/sh
# Boot the MIT xv6-riscv kernel on the RISC-V virt board.
#
# This is an unmodified upstream xv6-riscv image built from
# https://github.com/mit-pdos/xv6-riscv. It expects the QEMU-virt
# memory map (UART at 0x10000000, PLIC at 0x0c000000, VirtIO at
# 0x10001000) and the RISC-V SSTC timer extension.
#
# Because tinyemu-go uses the original TinyEMU memory map (HTIF at
# 0x40008000, PLIC at 0x40100000, VirtIO at 0x40010000) and does not
# yet model the SSTC stimecmp CSR, this image is expected to fail to
# boot fully in its current form. It is kept intentionally unpatched
# so that bringing it up serves as a regression/compat test for the
# emulator.
#
# Usage:
#   ./run_xv6-riscv.sh

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/xv6-riscv"
CFG="$DIR/xv6-riscv.cfg"

[ -x "$TEMU" ] || { echo "missing emulator $TEMU (run 'make build')" >&2; exit 1; }
[ -r "$CFG" ]  || { echo "missing config $CFG" >&2; exit 1; }
[ -r "$DIR/kernel" ] || { echo "missing xv6 kernel $DIR/kernel" >&2; exit 1; }
[ -r "$DIR/fs.img" ] || { echo "missing xv6 fs $DIR/fs.img" >&2; exit 1; }

echo "[run_xv6-riscv] booting unmodified xv6-riscv on the RISC-V virt board"
echo "  kernel: $DIR/kernel"
echo "  fs:     $DIR/fs.img"
exec "$TEMU" "$CFG"
