#!/bin/sh
# Boot go-boot (a TamaGo UEFI app) with networking under OVMF on temu, and
# auto-start a Go net/http server inside the guest that is reachable from
# the host. While this is running, from another terminal:
#
#     curl http://127.0.0.1:8080/
#
# and you get a response served by Go code running bare-metal in the guest.
#
# Usage:
#   ./run_goboot_serve.sh                 # forward host 8080 -> guest :80
#   HOSTPORT=9000 ./run_goboot_serve.sh   # forward a different host port
#   SKIP_BUILD=1 ./run_goboot_serve.sh    # reuse bin/go-boot/go-boot.efi as-is
#
# The full path the request travels:
#   curl -> slirp hostfwd (host 127.0.0.1:HOSTPORT) -> virtio-net-pci ->
#   OVMF VirtioNetDxe/SNP -> go-net (gVisor TCP/IP) -> net/http in the guest.
#
# How it works: go-boot is built with NET=gvisor (which adds the `net` and
# `serve` shell commands and a gVisor userspace TCP/IP stack), dropped on a
# FAT ESP, and booted by OVMF. This script feeds the two shell commands that
# bring the interface up and start the server, then keeps the console open
# so you can keep typing (and so temu keeps running). Stop it with Ctrl-C.
#
# Requirements (overridable via env):
#   TAMAGO=~/Apps/tamago-go1.26.4/bin/go   the TamaGo compiler
#   GOBOOT=~/Dev/Go.Code/go-boot.git       a go-boot checkout that has the
#                                          `serve` command (cmd/serve.go) and
#                                          the StationAddress tolerance patch
#   GNU objcopy on PATH (macOS: brew install binutils)
#   OVMF=bin/ovmf/OVMF.fd                  UEFI firmware

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

DIR="$ROOT/bin/go-boot"
TAMAGO=${TAMAGO:-$HOME/Apps/tamago-go1.26.4/bin/go}
GOBOOT=${GOBOOT:-$HOME/Dev/Go.Code/go-boot.git}
OVMF="${OVMF:-$ROOT/bin/ovmf/OVMF_DEBUG.fd}"
MEM=${MEM:-1024}
HOSTPORT=${HOSTPORT:-8080}
EFI="$DIR/go-boot.efi"
ESP="$DIR/esp.img"

mkdir -p "$DIR"
[ -x "$TEMU" ] || { echo "missing emulator $TEMU (go build -o $TEMU ./cmd/temu)" >&2; exit 1; }
[ -r "$OVMF" ] || { echo "missing OVMF firmware $OVMF" >&2; exit 1; }

if ! command -v objcopy >/dev/null 2>&1; then
    export PATH="/opt/homebrew/opt/binutils/bin:/usr/local/opt/binutils/bin:/opt/homebrew/bin:$PATH"
fi

# Build go-boot with networking (NET=gvisor → the `net`/`serve` commands and
# the gVisor stack; CONSOLE=COM1 → the serial console temu drives).
if [ "${SKIP_BUILD:-0}" != "1" ]; then
    [ -x "$TAMAGO" ] || { echo "missing TamaGo compiler $TAMAGO (set TAMAGO=...)" >&2; exit 1; }
    [ -d "$GOBOOT" ] || { echo "missing go-boot checkout $GOBOOT (set GOBOOT=...)" >&2; exit 1; }
    command -v objcopy >/dev/null 2>&1 || { echo "need GNU objcopy (brew install binutils)" >&2; exit 1; }
    echo "[run_goboot_serve] building go-boot (NET=gvisor) from $GOBOOT"
    make -C "$GOBOOT" CONSOLE=COM1 NET=gvisor TAMAGO="$TAMAGO" >/dev/null
    cp "$GOBOOT/go-boot.efi" "$EFI"
fi
[ -r "$EFI" ] || { echo "missing $EFI (run without SKIP_BUILD, or build go-boot NET=gvisor)" >&2; exit 1; }

# Build a FAT EFI System Partition with \EFI\BOOT\BOOTX64.EFI = go-boot.efi.
build_esp_darwin() {
    tmp="$DIR/.esp-build"
    rm -f "$tmp.dmg" "$ESP"
    hdiutil create -megabytes 64 -fs MS-DOS -volname GOBOOT -layout NONE -o "$tmp" >/dev/null
    mnt=$(mktemp -d)
    hdiutil attach "$tmp.dmg" -mountpoint "$mnt" >/dev/null
    mkdir -p "$mnt/EFI/BOOT"
    COPYFILE_DISABLE=1 cp "$EFI" "$mnt/EFI/BOOT/BOOTX64.EFI"
    hdiutil detach "$mnt" >/dev/null
    rmdir "$mnt" 2>/dev/null || true
    mv "$tmp.dmg" "$ESP"
}
build_esp_mtools() {
    command -v mformat >/dev/null 2>&1 || { echo "need mtools on $OS" >&2; exit 1; }
    rm -f "$ESP"
    dd if=/dev/zero of="$ESP" bs=1048576 count=64 status=none
    mformat -i "$ESP" -F -v GOBOOT ::
    mmd -i "$ESP" ::/EFI ::/EFI/BOOT
    mcopy -i "$ESP" "$EFI" ::/EFI/BOOT/BOOTX64.EFI
}
echo "[run_goboot_serve] building ESP $ESP"
case $OS in
    darwin) build_esp_darwin ;;
    *)      build_esp_mtools ;;
esac

echo
echo "Starting go-boot + Go web server under OVMF (x86_64, ${MEM} MiB)."
echo "Once you see 'HTTP server listening on :80', from ANOTHER terminal run:"
echo "    curl http://127.0.0.1:${HOSTPORT}/"
echo "Stop with Ctrl-C. (The go-boot '>' shell stays interactive too.)"
echo

LOG=$(mktemp -t goboot_serve)
trap 'rm -f "$LOG"' EXIT

# Feed the two shell commands once the firmware/app is up, then hand the
# console back to the terminal (the trailing cat) so it stays interactive
# and temu keeps running. tee mirrors temu's output to $LOG so the feeder
# can tell when the go-boot banner has appeared.
{
    for _ in $(seq 1 300); do
        grep -q 'UEFI x64' "$LOG" 2>/dev/null && break
        sleep 1
    done
    sleep 8
    # Static config matching slirp's network: guest 10.0.2.15/24, the NIC's
    # MAC, gateway 10.0.2.2.
    printf 'net 10.0.2.15/24 02:00:00:00:00:01 10.0.2.2\n'
    sleep 6
    printf 'serve\n'
    cat # keep stdin open + forward your keystrokes to the go-boot shell
} | "$TEMU" -machine x86_64 -m "$MEM" -apic -bios "$OVMF" \
        -drive "$ESP" -net-user -net-hostfwd "tcp:${HOSTPORT}:80" 2>&1 | tee "$LOG"
