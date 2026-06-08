package pc

import (
	"bytes"
	"encoding/binary"
)

// e820 entry types, per the ACPI E820 spec. SeaBIOS recognises 1 (RAM)
// and 2 (RESERVED); other types (3 = ACPI reclaim, 4 = NVS, 5 =
// unusable) are passed through to the OS.
const (
	e820TypeRAM      uint32 = 1
	e820TypeReserved uint32 = 2
)

// Memory-layout boundaries shared by the fw_cfg E820 map and the CMOS
// memory-size registers so firmware sees one consistent picture.
const (
	// pciHoleStart is the base of the 32-bit PCI MMIO hole: guest RAM
	// fills [0, pciHoleStart) and anything beyond is relocated above
	// 4 GiB. 3 GiB matches the QEMU i440fx/q35 default split.
	pciHoleStart = 0xC0000000
	fourGiB      = 0x100000000
)

// e820Entry is the on-the-wire format firmware reads from the fw_cfg
// "etc/e820" file. 20 bytes per entry, little-endian, no padding.
type e820Entry struct {
	Start uint64
	Size  uint64
	Type  uint32
}

// ramSplit returns how much of the configured guest RAM is mapped below
// the 32-bit PCI hole and how much is relocated above 4 GiB. Both the
// E820 map and the CMOS size registers derive from this so they never
// disagree.
func ramSplit(ramSize uint64) (below4G, above4G uint64) {
	if ramSize <= pciHoleStart {
		return ramSize, 0
	}
	return pciHoleStart, ramSize - pciHoleStart
}

// buildE820 emits the fw_cfg "etc/e820" blob describing the guest's
// memory layout. Firmware (SeaBIOS, OVMF) reads it from fw_cfg; a guest
// OS gets its own map elsewhere (the boot_params zero page for a direct
// Linux load), so this file is firmware-facing only.
//
// The below-4 GiB system RAM is described as a SINGLE entry based at 0
// whose *length* is the full below-hole size — exactly QEMU's fw_cfg
// convention. This matters because OVMF's PlatformGetLowMemoryCB reads
// the length of the base-0 RAM entry as the total amount of low memory:
//
//	if (E820Entry->BaseAddr != 0) return;
//	PlatformInfoHob->LowMemory = (UINT32)E820Entry->Length;
//
// The previous layout split low RAM around the EBDA/BIOS holes, so the
// base-0 entry was only 640 KiB. OVMF then sized ALL of low memory at
// 640 KiB, could not fit its ~64 MiB of PEI permanent memory below that,
// and placed PEI memory (and the EmuVariable NV-store reservation) up
// near 4 GiB — above our mapped RAM — faulting the instant it copied the
// variable store in from flash. The legacy 0xA0000-0x100000 hole is
// reserved by the firmware itself, exactly as on QEMU, so it does not
// need its own E820 entry.
//
// RAM beyond the PCI hole is emitted as a second entry based at 4 GiB,
// mirroring the CMOS 0x5B/0x5C/0x5D above-4 GiB fields.
func buildE820(ramSize uint64) []byte {
	below4G, above4G := ramSplit(ramSize)

	entries := []e820Entry{
		{Start: 0, Size: below4G, Type: e820TypeRAM},
	}
	if above4G > 0 {
		entries = append(entries, e820Entry{Start: fourGiB, Size: above4G, Type: e820TypeRAM})
	}

	var buf bytes.Buffer
	for _, e := range entries {
		_ = binary.Write(&buf, binary.LittleEndian, e)
	}
	return buf.Bytes()
}
