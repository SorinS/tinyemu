#!/bin/sh
# Build a TamaGo UEFI app image from a Go source file into bin/tamago/:
#   app       the TamaGo-compiled ELF
#   app.efi   the PE32+ EFI application (objcopy of app)
#   esp.img   a FAT EFI System Partition holding \EFI\BOOT\BOOTX64.EFI
#
# Incremental: each artifact is rebuilt only when its input is newer (or the
# source file differs from the one last built), so re-running when nothing
# changed is a fast no-op — the slow steps (TamaGo compile, hdiutil ESP build)
# are skipped. Used by `make tamago` and by run_tamago.sh.
#
# Usage:  sh scripts/build_tamago.sh [path/to/app.go]
#         (default source: bin/tamago/main.go, a sample written on first run)
#
# Env (overridable):
#   TAMAGO=~/Apps/tamago-go1.26.4/bin/go   the TamaGo compiler
#   GOBOOT=~/Dev/Go.Code/go-boot.git       a checkout of usbarmory/go-boot
#   objcopy from GNU binutils on PATH (macOS: brew install binutils)

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OS=$(uname -s | tr A-Z a-z)
DIR="$ROOT/bin/tamago"
TAMAGO=${TAMAGO:-$HOME/Apps/tamago-go1.26.4/bin/go}
GOBOOT=${GOBOOT:-$HOME/Dev/Go.Code/go-boot.git}

# go-boot build constants (kept in sync with its Makefile).
IMAGE_BASE=0x10000000
TEXT_START=268500992 # 0x10010000 = IMAGE_BASE + 0x10000
BUILD_TAGS=linkcpuinit,linkramsize,linkramstart,linkprintk

mkdir -p "$DIR"
SRC=${1:-$DIR/main.go}

# On first run (default source absent) drop a sample program exercising the Go
# runtime — goroutines, the GC, and the monotonic clock.
if [ "$SRC" = "$DIR/main.go" ] && [ ! -f "$SRC" ]; then
    echo "[build_tamago] writing sample program to $SRC"
    cat > "$SRC" <<'GO'
// A standalone TamaGo UEFI Go program. Built by scripts/build_tamago.sh and
// booted by run_tamago.sh. Edit freely — the whole Go runtime is available
// bare-metal.

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

[ -x "$TAMAGO" ] || { echo "missing TamaGo compiler $TAMAGO (set TAMAGO=...)" >&2; exit 1; }
[ -d "$GOBOOT" ] || { echo "missing go-boot checkout $GOBOOT (set GOBOOT=...)" >&2; exit 1; }
[ -r "$SRC" ]    || { echo "missing Go source $SRC" >&2; exit 1; }

# GNU objcopy (for the ELF -> PE/EFI step); on macOS it ships with binutils.
if ! command -v objcopy >/dev/null 2>&1; then
    export PATH="/opt/homebrew/opt/binutils/bin:/usr/local/opt/binutils/bin:$PATH"
fi
command -v objcopy >/dev/null 2>&1 || { echo "need GNU objcopy (brew install binutils)" >&2; exit 1; }

ESP="$DIR/esp.img"
MARKER="$DIR/.app-src"

# Decide whether a rebuild is needed: outputs missing, the source path differs
# from the one last built, or the source is newer than the compiled ELF.
rebuild=0
{ [ -r "$ESP" ] && [ -r "$DIR/app.efi" ] && [ -r "$DIR/app" ]; } || rebuild=1
[ -r "$MARKER" ] && [ "$(cat "$MARKER" 2>/dev/null)" = "$SRC" ] || rebuild=1
[ "$SRC" -nt "$DIR/app" ] && rebuild=1
if [ "$rebuild" -eq 0 ]; then
    echo "[build_tamago] $ESP is up to date (source unchanged)"
    exit 0
fi

# Build inside the go-boot module so its resolved go.mod/go.sum supply the
# uefi/x64 bootstrap and all transitive dependencies. The source is staged into
# a scratch package directory.
echo "[build_tamago] building $SRC with TamaGo"
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

echo "[build_tamago] converting ELF -> PE32+ EFI application"
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
echo "[build_tamago] building ESP $ESP"
case $OS in
    darwin) build_esp_darwin ;;
    *)      build_esp_mtools ;;
esac
printf '%s\n' "$SRC" > "$MARKER"
echo "[build_tamago] done -> $ESP"
