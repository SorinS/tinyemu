#!/bin/sh
# Boot go-boot (a TamaGo UEFI application) under OVMF on the cpu/x86_64
# long-mode emulator.
#
# Usage:
#   ./run_go-boot.sh
#
# Unlike run64_iso.sh (which plays bootloader and jumps straight into a
# Linux kernel), this exercises the FULL UEFI firmware path: temu loads
# OVMF as its BIOS; OVMF runs SEC -> PEI -> DXE -> BDS, finds
# \EFI\BOOT\BOOTX64.EFI on the attached FAT disk (built here from
# bin/go-boot/go-boot.efi), and launches it. go-boot's TamaGo runtime
# comes up and presents an interactive shell on the serial console
# (COM1 = temu stdin/stdout). Type `help` at the `>` prompt; exit temu
# with Ctrl-A x.
#
# Two flags are load-bearing:
#   -apic   : OVMF asserts a software-enabled local APIC (CpuMpPei reads
#             SVR bit 8). temu only wires a local APIC when -apic is given
#             — it is flag-gated so legacy PIC-only Linux boots are
#             unaffected (see machine/pc/lapic.go).
#   -m 1024 : go-boot/TamaGo hardcodes a 704 MiB RAM region based at its
#             image base (0x10000000) and parks its initial stack at the
#             top of it (~0x3c000000), so it needs ~960 MiB of mapped RAM.
#             Upstream runs it with -m 8G; 1 GiB is the practical minimum.
#             Too little RAM and the stack lands above mapped memory, the
#             pushes are dropped, and the first RET jumps into garbage.
#
# Env knobs:
#   MEM=2048              override guest RAM (MiB)
#   TINYEMU_BIOS_DEBUG=stderr   watch the OVMF SEC/PEI/DXE log inline
#                               (default: routed to bin/go-boot/ovmf-debug.log
#                               so only go-boot's console shows)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

MEM=${MEM:-1024}
OVMF="$ROOT/bin/ovmf/OVMF_DEBUG.fd"
EFI="$ROOT/bin/go-boot/go-boot.efi"
ESP="$ROOT/bin/go-boot/esp.img"

[ -x "$TEMU" ] || { echo "missing emulator binary $TEMU (run: make or go build -o $TEMU ./cmd/temu)" >&2; exit 1; }
[ -r "$OVMF" ] || { echo "missing OVMF firmware $OVMF (see bin/ovmf/get_omvf.sh)" >&2; exit 1; }
[ -r "$EFI" ]  || { echo "missing UEFI app $EFI (copy go-boot.efi from the go-boot build)" >&2; exit 1; }

# Build a FAT EFI System Partition holding \EFI\BOOT\BOOTX64.EFI whenever
# it is missing or older than the go-boot binary. OVMF's PlatformRecovery
# scans every FAT volume for \EFI\BOOT\BOOTX64.EFI and launches it.
build_esp_darwin() {
	tmp="$ROOT/bin/go-boot/.esp-build"
	rm -f "$tmp.dmg" "$ESP"
	hdiutil create -megabytes 64 -fs MS-DOS -volname GOBOOT -layout NONE -o "$tmp" >/dev/null
	mnt=$(mktemp -d)
	hdiutil attach "$tmp.dmg" -mountpoint "$mnt" >/dev/null
	mkdir -p "$mnt/EFI/BOOT"
	# COPYFILE_DISABLE keeps macOS from writing ._ resource-fork files.
	COPYFILE_DISABLE=1 cp "$EFI" "$mnt/EFI/BOOT/BOOTX64.EFI"
	hdiutil detach "$mnt" >/dev/null
	rmdir "$mnt" 2>/dev/null || true
	mv "$tmp.dmg" "$ESP"
}

build_esp_mtools() {
	command -v mformat >/dev/null 2>&1 || {
		echo "need 'mtools' (mformat/mcopy) to build the ESP on $OS" >&2; exit 1; }
	rm -f "$ESP"
	dd if=/dev/zero of="$ESP" bs=1048576 count=64 status=none
	mformat -i "$ESP" -F -v GOBOOT ::
	mmd -i "$ESP" ::/EFI ::/EFI/BOOT
	mcopy -i "$ESP" "$EFI" ::/EFI/BOOT/BOOTX64.EFI
}

if [ ! -f "$ESP" ] || [ "$EFI" -nt "$ESP" ]; then
	echo "[run_go-boot] building ESP image $ESP from $EFI"
	case $OS in
		darwin) build_esp_darwin ;;
		*)      build_esp_mtools ;;
	esac
fi

# Route OVMF's verbose port-0x402 debug log to a file so only go-boot's
# serial console shows in the terminal. Override with TINYEMU_BIOS_DEBUG=stderr.
: "${TINYEMU_BIOS_DEBUG:=$ROOT/bin/go-boot/ovmf-debug.log}"
export TINYEMU_BIOS_DEBUG

echo "Starting go-boot under OVMF (x86_64, ${MEM} MiB) at: $(date)"
echo "  (type 'help' at the go-boot '>' prompt; exit temu with Ctrl-A x)"

exec "$TEMU" -machine x86_64 -m "$MEM" -apic -bios "$OVMF" -drive "$ESP"
