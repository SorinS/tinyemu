#!/bin/sh
# Assemble a single NASM file and run it through the requested
# emulator CPU backend, halting at the first HLT (or after a step
# budget). Prints final register state so you can diff against
# expectations.
#
# Usage:
#   ./run_asm.sh <x86|x86_64> <file.asm>            # 100k step budget
#   ./run_asm.sh <x86|x86_64> <file.asm> <steps>    # explicit budget
#
# Examples:
#   ./run_asm.sh x86_64 examples/hello.asm
#   ./run_asm.sh x86    examples/loop.asm 1000
#
# riscv64 isn't supported here (no NASM equivalent for that backend in
# this repo — use the riscv64 boot scripts directly).
#
# Requires: nasm (Homebrew: /opt/homebrew/bin/nasm) for asm → bin.
# The actual execution uses the in-tree Go tests as the runner
# harness — we generate a one-off `_asm_runner_test.go` next to the
# CPU package and invoke `go test` on it.

set -e

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    cat <<EOF >&2
Usage: $0 <x86|x86_64> <file.asm> [steps]
  x86       run via cpu/x86 (i386, 32-bit, flat real-mode-ish setup)
  x86_64    run via cpu/x86_64 (long mode, flat 64-bit, paging off)
  steps     step budget (default 100000; raise if the program loops)
EOF
    exit 1
fi

ARCH=$1
SRC=$2
STEPS=${3:-100000}

[ -r "$SRC" ] || { echo "missing source: $SRC" >&2; exit 1; }

case "$ARCH" in
    x86|x86_64) ;;
    *) echo "unsupported arch: $ARCH (expected x86 or x86_64)" >&2; exit 1 ;;
esac

ROOT="$(cd "$(dirname "$0")" && pwd)"
NASM="${NASM:-$(command -v nasm || true)}"
if [ -z "$NASM" ]; then
    if [ -x /opt/homebrew/bin/nasm ]; then
        NASM=/opt/homebrew/bin/nasm
    else
        echo "nasm not found. Install via 'brew install nasm'." >&2
        exit 1
    fi
fi

# Assemble to a temp .bin
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
BIN="$TMP/test.bin"
"$NASM" -f bin -o "$BIN" "$SRC"

# Generate a one-off Go test file that loads the bin into a flat
# memory map and runs Step until HLT or budget. Output is the final
# register state on stderr.
RUNNER_DIR="$ROOT/test/$ARCH"
RUNNER="$RUNNER_DIR/zz_run_asm_one_test.go"

mkdir -p "$RUNNER_DIR"

case "$ARCH" in
    x86_64)
        cat > "$RUNNER" <<'EOF'
package x86_64_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86_64"
	"github.com/jtolio/tinyemu-go/mem"
)

func TestRunAsmOne(t *testing.T) {
	binPath := os.Getenv("ASM_BIN")
	steps := 100000
	fmt.Sscanf(os.Getenv("ASM_STEPS"), "%d", &steps)
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read bin %s: %v", binPath, err)
	}
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 16*1024*1024, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := x86_64.NewCPU(mm)
	c.SetCR64(0, x86_64.CR0_PE)
	c.SetCR64(4, x86_64.CR4_PAE)
	c.SetEFER(x86_64.EFER_LME | x86_64.EFER_LMA)
	c.SetSegAccess(x86_64.CS, 1<<9)
	c.SetSegBase(x86_64.CS, 0)
	const codeBase = uint64(0x10000)
	for i, b := range data {
		_ = mm.Write8(codeBase+uint64(i), b)
	}
	c.SetRIP(codeBase)
	c.SetReg64(x86_64.RSP, 0x8000)
	ran := 0
	for ran = 0; ran < steps; ran++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			fmt.Fprintf(os.Stderr, "STEP-ERROR at RIP=%#x after %d steps: %v\n", c.GetRIP(), ran, err)
			break
		}
	}
	fmt.Fprintf(os.Stderr, "\n=== final state after %d steps ===\n", ran)
	fmt.Fprintf(os.Stderr, "RIP=%#x  RSP=%#x  RFLAGS=%#x  power_down=%v\n",
		c.GetRIP(), c.GetReg64(x86_64.RSP), c.GetRFLAGS(), c.IsPowerDown())
	regs := []struct {
		name string
		i    int
	}{
		{"RAX", x86_64.RAX}, {"RBX", x86_64.RBX}, {"RCX", x86_64.RCX}, {"RDX", x86_64.RDX},
		{"RSI", x86_64.RSI}, {"RDI", x86_64.RDI}, {"RBP", x86_64.RBP},
		{"R8", x86_64.R8}, {"R9", x86_64.R9}, {"R10", x86_64.R10}, {"R11", x86_64.R11},
		{"R12", x86_64.R12}, {"R13", x86_64.R13}, {"R14", x86_64.R14}, {"R15", x86_64.R15},
	}
	for _, r := range regs {
		fmt.Fprintf(os.Stderr, "  %s = %#016x\n", r.name, c.GetReg64(r.i))
	}
}
EOF
        ;;
    x86)
        cat > "$RUNNER" <<'EOF'
package x86_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/mem"
)

func TestRunAsmOne(t *testing.T) {
	binPath := os.Getenv("ASM_BIN")
	steps := 100000
	fmt.Sscanf(os.Getenv("ASM_STEPS"), "%d", &steps)
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read bin %s: %v", binPath, err)
	}
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 16*1024*1024, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := x86.NewCPU(mm)
	c.SetCR0(c.GetCR0() | x86.CR0_PE)
	const codeBase = uint32(0x10000)
	for i, b := range data {
		_ = mm.Write8(uint64(codeBase)+uint64(i), b)
	}
	c.SetEIP(codeBase)
	c.SetReg32(x86.ESP, 0x8000)
	ran := 0
	for ran = 0; ran < steps; ran++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			fmt.Fprintf(os.Stderr, "STEP-ERROR at EIP=%#x after %d steps: %v\n", c.GetEIP(), ran, err)
			break
		}
	}
	fmt.Fprintf(os.Stderr, "\n=== final state after %d steps ===\n", ran)
	fmt.Fprintf(os.Stderr, "EIP=%#x  ESP=%#x  EFLAGS=%#x  power_down=%v\n",
		c.GetEIP(), c.GetReg32(x86.ESP), c.GetEFLAGS(), c.IsPowerDown())
	regs := []struct {
		name string
		i    int
	}{
		{"EAX", x86.EAX}, {"EBX", x86.EBX}, {"ECX", x86.ECX}, {"EDX", x86.EDX},
		{"ESI", x86.ESI}, {"EDI", x86.EDI}, {"EBP", x86.EBP},
	}
	for _, r := range regs {
		fmt.Fprintf(os.Stderr, "  %s = %#010x\n", r.name, c.GetReg32(r.i))
	}
}
EOF
        ;;
esac

trap 'rm -f "$RUNNER"; rm -rf "$TMP"' EXIT

ASM_BIN="$BIN" ASM_STEPS="$STEPS" go test "$ROOT/test/$ARCH/" -run TestRunAsmOne -v 2>&1 | sed -n '/=== final state/,$p'
