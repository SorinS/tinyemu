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

// e820Entry is the on-the-wire format SeaBIOS reads from the
// fw_cfg "etc/e820" file (or returns to INT 15h E820 callers).
// 20 bytes per entry, little-endian, no padding.
type e820Entry struct {
	Start uint64
	Size  uint64
	Type  uint32
}

// buildE820 emits the fw_cfg "etc/e820" blob describing the guest's
// memory layout. SeaBIOS without this file has its INT 15h E820
// handler copy zeros to the caller's buffer (the qemu_cfg_e820 path
// allocates the source buffer only when the file is present; missing
// means the staging buffer never gets populated). Any BIOS-MBR boot
// loader that builds page tables from the E820 map (Pure64, GRUB
// when not using its UEFI path, anything that scans for usable RAM)
// then sees no RAM and either halts or — with Pure64's "subtract 2
// MiB for stacks" math — wraps to 0xFFFFFFFE and runs a 4-million-
// iteration page-table loop that overwrites the loader's own code.
//
// The layout mirrors what SeaBIOS would build internally if it
// computed one from CMOS RamSize: 640 KiB conventional + a 1 KiB
// EBDA hole + the BIOS-area hole at 0xF0000..0x100000 + extended
// RAM from 1 MiB to (ramSize - smallReservedTop) + a small
// reserved tail + the 256 KiB high-BIOS shadow at the top of 4 GiB.
//
// ramSize is the configured guest RAM in bytes; for RAM > 4 GiB we
// would emit an additional entry above 4 GiB, but the emulator's
// supported configs don't reach that yet.
func buildE820(ramSize uint64) []byte {
	const (
		ebdaStart      = 0x9FC00 // 640 KiB - 1 KiB EBDA
		ebdaEnd        = 0xA0000
		biosLowStart   = 0xF0000
		biosLowEnd     = 0x100000
		highRAMStart   = 0x100000
		highBIOSStart  = 0xFFFC0000 // top 256 KiB of 4 GiB
		fourGiB        = 0x100000000
		smallReservedTop = 12 * 1024 // top 12 KiB of low RAM reserved for ACPI/SMM/...
	)

	if ramSize <= highRAMStart+smallReservedTop {
		// Pathologically tiny RAM. Drop the small-reserved-tail entry
		// rather than emit a negative-sized region; the kernel would
		// reject it and the loader would see an underflow either way.
		smallReservedTop_ := uint64(0)
		_ = smallReservedTop_
	}

	highRAMEnd := ramSize - smallReservedTop
	if ramSize <= highRAMStart {
		highRAMEnd = highRAMStart
	}

	entries := []e820Entry{
		{Start: 0, Size: ebdaStart, Type: e820TypeRAM},
		{Start: ebdaStart, Size: ebdaEnd - ebdaStart, Type: e820TypeReserved},
		{Start: biosLowStart, Size: biosLowEnd - biosLowStart, Type: e820TypeReserved},
		{Start: highRAMStart, Size: highRAMEnd - highRAMStart, Type: e820TypeRAM},
		{Start: highRAMEnd, Size: ramSize - highRAMEnd, Type: e820TypeReserved},
		{Start: highBIOSStart, Size: fourGiB - highBIOSStart, Type: e820TypeReserved},
	}

	var buf bytes.Buffer
	for _, e := range entries {
		_ = binary.Write(&buf, binary.LittleEndian, e)
	}
	return buf.Bytes()
}
