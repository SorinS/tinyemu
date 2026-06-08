#!/bin/sh
# Boot Linux THROUGH go-boot, under OVMF, on the cpu/x86_64 emulator.
#
# Usage:
#   ./run_go-boot-linux.sh
#
# This chains the whole UEFI OS-load path:
#   temu -> OVMF (SEC/PEI/DXE/BDS) -> go-boot (TamaGo UEFI loader)
#        -> Linux kernel (EFI-aware boot) -> /bin/sh
#
# Unlike run64_iso.sh (temu plays bootloader and jumps straight into the
# kernel), here a real UEFI bootloader does the work: go-boot reads a UAPI
# Type #1 boot loader entry (\loader\entries\arch.conf) off the FAT disk,
# loads the kernel + initrd, builds EFI boot parameters (memory map +
# screen info), calls ExitBootServices, and jumps. The kernel then boots
# *EFI-aware*, a heavier early path than the direct -kernel boot.
#
# The combined FAT "ESP" built here holds:
#   /EFI/BOOT/BOOTX64.EFI       go-boot   (from bin/go-boot/go-boot.efi)
#   /vmlinuz                    the kernel (a bzImage)
#   /initrd.gz                  the initramfs
#   /loader/entries/arch.conf   tells go-boot what to boot
#
# At the go-boot `>` prompt, press ENTER (or type `linux`) to boot the
# default entry; go-boot.efi must be built with
# DEFAULT_LINUX_ENTRY=\loader\entries\arch.conf (the Makefile default) AND
# CONSOLE=COM1 (see run_go-boot.sh for why). Exit temu with Ctrl-A x.
#
# Env knobs:
#   MEM=4096                    guest RAM in MiB (default 2048; go-boot
#                               needs ~960 MiB before the kernel + initrd)
#   KERNEL=path INITRD=path     override the kernel (bzImage) / initramfs
#                               (default: TinyCore from bin/tinycore64/)
#   CMDLINE="..."               override the kernel command line
#   OVMF=bin/ovmf/OVMF.fd       use the release firmware (faster, quiet)
#   TINYEMU_BIOS_DEBUG=stderr   watch the OVMF boot (off by default = fast)

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=$(uname -s | tr A-Z a-z)
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
TEMU="$ROOT/bin/temu.${OS}-${ARCH}.bin"

MEM=${MEM:-2048}
OVMF="${OVMF:-$ROOT/bin/ovmf/OVMF_DEBUG.fd}"
EFI="$ROOT/bin/go-boot/go-boot.efi"
ESP="$ROOT/bin/go-boot/esp-linux.img"

# Default payload: TinyCore (self-contained initramfs, no second disk).
if [ -z "$KERNEL" ] || [ -z "$INITRD" ]; then
	"$ROOT/scripts/extract_tinycore64.sh" >/dev/null 2>&1 || true
fi
KERNEL=${KERNEL:-$ROOT/bin/tinycore64/vmlinuz64}
INITRD=${INITRD:-$ROOT/bin/tinycore64/corepure64.gz}
CMDLINE=${CMDLINE:-"console=ttyS0,115200 earlyprintk=ttyS0,115200 loglevel=8 noapic nolapic acpi=off pci=noacpi nosmp nokaslr tsc=reliable rdinit=/bin/sh"}

[ -x "$TEMU" ]   || { echo "missing emulator binary $TEMU" >&2; exit 1; }
[ -r "$OVMF" ]   || { echo "missing OVMF firmware $OVMF (see bin/ovmf/get_omvf.sh)" >&2; exit 1; }
[ -r "$EFI" ]    || { echo "missing UEFI loader $EFI (build go-boot CONSOLE=COM1)" >&2; exit 1; }
[ -r "$KERNEL" ] || { echo "missing kernel $KERNEL" >&2; exit 1; }
[ -r "$INITRD" ] || { echo "missing initrd $INITRD" >&2; exit 1; }

# Rebuild the ESP when missing or older than any input.
need_build=0
[ -f "$ESP" ] || need_build=1
for f in "$EFI" "$KERNEL" "$INITRD"; do
	[ "$f" -nt "$ESP" ] && need_build=1
done

write_entry() {
	# UAPI Type #1 entry. linux/initrd are io/fs paths (root-relative, no
	# leading slash); options is the kernel command line.
	printf 'title go-boot Linux\nlinux vmlinuz\ninitrd initrd.gz\noptions %s\n' "$CMDLINE"
}

build_esp_darwin() {
	tmp="$ROOT/bin/go-boot/.esp-linux-build"
	rm -f "$tmp.dmg" "$ESP"
	hdiutil create -megabytes 96 -fs MS-DOS -volname GOBOOT -layout NONE -o "$tmp" >/dev/null
	mnt=$(mktemp -d)
	hdiutil attach "$tmp.dmg" -mountpoint "$mnt" >/dev/null
	mkdir -p "$mnt/EFI/BOOT" "$mnt/loader/entries"
	COPYFILE_DISABLE=1 cp "$EFI" "$mnt/EFI/BOOT/BOOTX64.EFI"
	COPYFILE_DISABLE=1 cp "$KERNEL" "$mnt/vmlinuz"
	COPYFILE_DISABLE=1 cp "$INITRD" "$mnt/initrd.gz"
	write_entry > "$mnt/loader/entries/arch.conf"
	hdiutil detach "$mnt" >/dev/null
	rmdir "$mnt" 2>/dev/null || true
	mv "$tmp.dmg" "$ESP"
}

build_esp_mtools() {
	command -v mformat >/dev/null 2>&1 || {
		echo "need 'mtools' (mformat/mcopy) to build the ESP on $OS" >&2; exit 1; }
	rm -f "$ESP"
	dd if=/dev/zero of="$ESP" bs=1048576 count=96 status=none
	mformat -i "$ESP" -F -v GOBOOT ::
	mmd -i "$ESP" ::/EFI ::/EFI/BOOT ::/loader ::/loader/entries
	mcopy -i "$ESP" "$EFI" ::/EFI/BOOT/BOOTX64.EFI
	mcopy -i "$ESP" "$KERNEL" ::/vmlinuz
	mcopy -i "$ESP" "$INITRD" ::/initrd.gz
	write_entry | mcopy -i "$ESP" - ::/loader/entries/arch.conf
}

if [ "$need_build" = 1 ]; then
	echo "[run_go-boot-linux] building ESP $ESP"
	echo "    kernel: $KERNEL"
	echo "    initrd: $INITRD"
	echo "    cmdline: $CMDLINE"
	case $OS in
		darwin) build_esp_darwin ;;
		*)      build_esp_mtools ;;
	esac
fi

echo "Starting Linux via go-boot under OVMF (x86_64, ${MEM} MiB) at: $(date)"
echo "  At the go-boot '>' prompt press ENTER to boot Linux (or type 'linux')."
echo "  Boot to shell takes a few minutes (LZMA + initramfs decompress are"
echo "  CPU-bound under the interpreter). Exit temu with Ctrl-A x."

exec "$TEMU" -machine x86_64 -m "$MEM" -apic -bios "$OVMF" -drive "$ESP"
