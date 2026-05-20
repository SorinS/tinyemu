#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"
DIR="${SCRIPT_DIR}/bin/riscv64"
[ ! -d "$DIR" ] && echo "Folder $DIR does not exist" && exit 1

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m) 

BIN=${SCRIPT_DIR}/bin/temu.${OS}-${ARCH}.bin
[ ! -x "$BIN" ] && echo "Binary: ${BIN} does not exist or not executable" && exit 2

CFG=${SCRIPT_DIR}/bin/riscv64/root-riscv64.cfg
[ ! -r "$CFG" ] && echo "Config file: ${CFG} does not exist or not readable" && exit 3

exec ${BIN} ${CFG}
