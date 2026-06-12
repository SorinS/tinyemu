#!/bin/sh
# Compile a Go program with TamaGo and UEFI-boot it under OVMF on temu.
#
# This is the generic version of run_go-boot.sh: instead of booting go-boot's
# shell, it builds YOUR Go code into a UEFI application and runs it. It is the
# fastest way to write bare-metal Go and see it execute on the emulator.
#
# Usage:
#   ./run_tamago.sh                  # build + boot bin/tamago/main.go
#                                    #   (a sample is written there on first run)
#   ./run_tamago.sh path/to/app.go   # build + boot a specific Go source file
#   MEM=2048 ./run_tamago.sh         # override guest RAM (MiB)
#
# How it works
# ------------
# Your program imports github.com/usbarmory/go-boot/uefi/x64, which provides
# the UEFI bootstrap (the `cpuinit` entry point, UEFI Boot/Runtime Services,
# and a console wired to the firmware ConOut -> 16550 serial = temu
# stdout/stdin). TamaGo (a bare-metal Go toolchain, GOOS=tamago GOARCH=amd64)
# compiles it to an ELF; objcopy converts that to a PE32+ EFI application;
# it is dropped on a FAT EFI System Partition as \EFI\BOOT\BOOTX64.EFI; and
# OVMF (temu's -bios) finds and launches it — exactly the path go-boot uses,
# with your code as the payload. The full Go runtime is available: goroutines,
# channels, the garbage collector, fmt, time, etc.
#
# Writing a program
# -----------------
# The minimum is a package main that imports uefi/x64 and prints with fmt
# (output goes to the serial console). See the generated bin/tamago/main.go.
# End by powering off with x64.UEFI.Runtime.ResetSystem(uefi.EfiResetShutdown)
# or returning to the firmware with x64.UEFI.Boot.Exit(0).
#
# Requirements (paths overridable via env)
# ----------------------------------------
#   TAMAGO=~/Apps/tamago-go1.26.4/bin/go   the TamaGo compiler
#   GOBOOT=~/Dev/Go.Code/go-boot.git       a checkout of usbarmory/go-boot
#   objcopy from GNU binutils on PATH (macOS: brew install binutils)
#   OVMF=bin/ovmf/OVMF.fd                  UEFI firmware (see bin/ovmf/get_omvf.sh)
#
# Artifacts live in bin/tamago/: main.go (your source), app (ELF), app.efi
# (the UEFI app), esp.img (the FAT boot disk).

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/tamago"
TAMAGO=${TAMAGO:-$HOME/Apps/tamago-go1.26.4/bin/go}
GOBOOT=${GOBOOT:-$HOME/Dev/Go.Code/go-boot.git}
# Prefer the firmware kept alongside this target's artifacts; fall back to
# the shared copy (and seed the local one from it) so bin/tamago is
# self-contained.
if [ -n "$OVMF" ]; then
    :
elif [ -r "$DIR/OVMF.fd" ]; then
    OVMF="$DIR/OVMF.fd"
elif [ -r "$ROOT/bin/ovmf/OVMF.fd" ]; then
    mkdir -p "$DIR"
    cp "$ROOT/bin/ovmf/OVMF.fd" "$DIR/OVMF.fd"
    OVMF="$DIR/OVMF.fd"
fi
MEM=${MEM:-1024}

# go-boot build constants (kept in sync with its Makefile).
IMAGE_BASE=0x10000000
TEXT_START=268500992 # 0x10010000 = IMAGE_BASE + 0x10000
BUILD_TAGS=linkcpuinit,linkramsize,linkramstart,linkprintk

mkdir -p "$DIR"
SRC=${1:-$DIR/main.go}

# On first run (no source given and none present), drop a sample program
# that exercises the Go runtime — goroutines, the GC, and the (now working)
# monotonic clock.
if [ "$SRC" = "$DIR/main.go" ] && [ ! -f "$SRC" ]; then
    echo "[run_tamago] writing sample program to $SRC"
    cat > "$SRC" <<'GO'
// A standalone TamaGo UEFI Go program. Built and booted by run_tamago.sh.
// Edit freely — the whole Go runtime is available bare-metal.

//go:build amd64

package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/usbarmory/go-boot/uefi"
	"github.com/usbarmory/go-boot/uefi/x64"
)

func main() {
	// The UEFI watchdog would reset us after ~5 min; disable it.
	x64.UEFI.Boot.SetWatchdogTimer(0)

	fmt.Printf("\n=== standalone TamaGo UEFI Go program on temu ===\n")
	fmt.Printf("runtime : %s  %s/%s  NumCPU=%d\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())

	// Concurrency: fan out squares across goroutines, collect over a channel.
	const N = 10
	ch := make(chan int, N)
	var wg sync.WaitGroup
	for i := 1; i <= N; i++ {
		wg.Add(1)
		go func(n int) { defer wg.Done(); ch <- n * n }(i)
	}
	go func() { wg.Wait(); close(ch) }()
	sum := 0
	for v := range ch {
		sum += v
	}
	fmt.Printf("goroutines: sum(1..%d squared) = %d\n", N, sum)

	// Timing: the monotonic clock now works (CPUID 0x15 TSC frequency).
	start := time.Now()
	acc := 0
	for i := 0; i < 5_000_000; i++ {
		acc += i
	}
	fmt.Printf("compute   : 5M-iteration loop in %v\n", time.Since(start))

	// Heap + GC.
	runtime.GC()
	m := &runtime.MemStats{}
	runtime.ReadMemStats(m)
	fmt.Printf("gc        : cycles=%d  heap=%d KiB\n", m.NumGC, m.HeapAlloc/1024)

	fmt.Printf("=== done — powering off ===\n")
	time.Sleep(200 * time.Millisecond)
	_ = x64.UEFI.Runtime.ResetSystem(uefi.EfiResetShutdown)

	for { // unreachable if shutdown works; keeps the compiler happy
	}
}
GO
fi

[ -x "$TEMU" ]   || { echo "missing emulator $TEMU (go build -o $TEMU ./cmd/temu)" >&2; exit 1; }
[ -x "$TAMAGO" ] || { echo "missing TamaGo compiler $TAMAGO (set TAMAGO=...)" >&2; exit 1; }
[ -d "$GOBOOT" ] || { echo "missing go-boot checkout $GOBOOT (set GOBOOT=...)" >&2; exit 1; }
[ -r "$OVMF" ]   || { echo "missing OVMF firmware $OVMF" >&2; exit 1; }
[ -r "$SRC" ]    || { echo "missing Go source $SRC" >&2; exit 1; }

# GNU objcopy (for the ELF -> PE/EFI step); on macOS it ships with binutils.
if ! command -v objcopy >/dev/null 2>&1; then
    export PATH="/opt/homebrew/opt/binutils/bin:/usr/local/opt/binutils/bin:$PATH"
fi
command -v objcopy >/dev/null 2>&1 || { echo "need GNU objcopy (brew install binutils)" >&2; exit 1; }

# Build inside the go-boot module so its resolved go.mod/go.sum supply the
# uefi/x64 bootstrap and all transitive dependencies (no separate module to
# keep in sync). The source is staged into a scratch package directory.
echo "[run_tamago] building $SRC with TamaGo"
STAGE="$GOBOOT/.tamago-app"
rm -rf "$STAGE"; mkdir -p "$STAGE"
cp "$SRC" "$STAGE/main.go"
(
    cd "$GOBOOT"
    GOOS=tamago GOOSPKG=github.com/usbarmory/tamago GOARCH=amd64 \
        "$TAMAGO" build -tags "$BUILD_TAGS" -trimpath \
        -ldflags "-s -w -E cpuinit -T $TEXT_START -R 0x1000" \
        -o "$DIR/app" ./.tamago-app
)
rm -rf "$STAGE"

echo "[run_tamago] converting ELF -> PE32+ EFI application"
objcopy \
    --strip-debug \
    --output-target efi-app-x86_64 \
    --subsystem=efi-app \
    --image-base "$IMAGE_BASE" \
    --stack=0x10000 \
    "$DIR/app" "$DIR/app.efi"
# Adjust the PE Characteristics field (matches go-boot's Makefile).
printf '\x26\x02' | dd of="$DIR/app.efi" bs=1 seek=150 count=2 conv=notrunc 2>/dev/null

# Build a FAT EFI System Partition with \EFI\BOOT\BOOTX64.EFI = app.efi.
ESP="$DIR/esp.img"
build_esp_darwin() {
    tmp="$DIR/.esp-build"
    rm -f "$tmp.dmg" "$ESP"
    hdiutil create -megabytes 64 -fs MS-DOS -volname TAMAGO -layout NONE -o "$tmp" >/dev/null
    mnt=$(mktemp -d)
    hdiutil attach "$tmp.dmg" -mountpoint "$mnt" >/dev/null
    mkdir -p "$mnt/EFI/BOOT"
    COPYFILE_DISABLE=1 cp "$DIR/app.efi" "$mnt/EFI/BOOT/BOOTX64.EFI"
    hdiutil detach "$mnt" >/dev/null
    rmdir "$mnt" 2>/dev/null || true
    mv "$tmp.dmg" "$ESP"
}
build_esp_mtools() {
    command -v mformat >/dev/null 2>&1 || { echo "need mtools on $OS" >&2; exit 1; }
    rm -f "$ESP"
    dd if=/dev/zero of="$ESP" bs=1048576 count=64 status=none
    mformat -i "$ESP" -F -v TAMAGO ::
    mmd -i "$ESP" ::/EFI ::/EFI/BOOT
    mcopy -i "$ESP" "$DIR/app.efi" ::/EFI/BOOT/BOOTX64.EFI
}
echo "[run_tamago] building ESP $ESP"
case $OS in
    darwin) build_esp_darwin ;;
    *)      build_esp_mtools ;;
esac

echo "Starting TamaGo app under OVMF (x86_64, ${MEM} MiB) at: $(date)"
echo "  (exit temu with Ctrl-A x, or it powers off when the program ends)"

# -apic: OVMF asserts a software-enabled local APIC (flag-gated in temu so
#        legacy PIC-only Linux boots are unaffected — see machine/pc/lapic.go).
exec "$TEMU" -machine x86_64 -m "$MEM" -apic -bios "$OVMF" -drive "$ESP"
