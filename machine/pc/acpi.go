package pc

import "encoding/binary"

// Minimal ACPI table set. Generates a blob suitable for handing to
// SeaBIOS via fw_cfg (see machine/pc/fwcfg.go — TODO):
//
//   - an RSDP at a 16-byte boundary inside 0xE0000..0xFFFFF so Pure64's
//     foundACPI scan finds it
//   - an RSDT that points to a single APIC (MADT) table
//   - a MADT with one Local-APIC entry (LAPIC base = 0xFEE00000)
//   - an HPET table so Pure64's "load HPET frequency" step doesn't end
//     up with garbage in p_HPET_Address
//   - a FADT — even though we don't model power management, several
//     loaders (Pure64 included) read FADT fields like the IAPC_BOOT_ARCH
//     legacy-device flags before continuing
//
// Earlier attempts injected this blob directly into the SeaBIOS BIOS
// shadow at LoadBIOS time, but SeaBIOS rewrites every byte of
// 0xE0000..0xFFFFF during POST as it lays out its own (partially
// populated, fw_cfg-dependent) BIOS-area data structures. The
// fw_cfg path lets SeaBIOS choose where to place the tables and
// patch the pointers itself, so this builder hands SeaBIOS a blob
// rather than scribbling on its memory directly.
//
// acpiBase below records the historical "this is where we used to
// stamp the RSDP" address; the offsets it anchors are still used by
// the unit tests to verify the layout is well-formed regardless of
// final placement.

const (
	// Base of our injected ACPI table set, in the *low* BIOS shadow
	// (mirrored to the high BIOS alias by LoadBIOS).
	acpiBase = 0x000F5660

	// Layout inside the gap (all 16-byte aligned).
	acpiRSDPOff = 0x00 // 36 bytes — RSDP v2 (works for v1 too)
	acpiRSDTOff = 0x40 // RSDT header (36) + table-pointer entries
	acpiFADTOff = 0x80 // FADT (276 bytes)
	acpiMADTOff = 0x1A0 // MADT (44 header + LAPIC entry)
	acpiHPETOff = 0x200 // HPET descriptor (56 bytes)

	// LAPIC base — matches our LAPIC stub registered at PC init.
	lapicBase = 0xFEE00000

	// IOAPIC base — matches our IOAPIC stub.
	ioapicBase = 0xFEC00000

	// HPET base — fake but stable. Nothing in the emulator services
	// reads/writes here; Pure64 just stores it and BareMetal reads it
	// for an os_HPET_Address field it doesn't currently dereference.
	hpetBase = 0xFED00000
)

// guestAddr returns the *guest physical* address of an offset inside our
// ACPI gap. Loaders embed these in table-pointer fields.
func guestAddr(off int) uint32 { return uint32(acpiBase + off) }

// writeACPITableHeader writes the standard 36-byte ACPI table header
// (sig, length, revision, checksum-placeholder, OEM, etc.) and returns
// the number of bytes written. The caller fills in the body, then calls
// finalizeChecksum to fix the byte-sum-to-zero invariant.
func writeACPITableHeader(buf []byte, sig string, length uint32, revision byte) {
	copy(buf[0:4], sig)
	binary.LittleEndian.PutUint32(buf[4:8], length)
	buf[8] = revision
	// buf[9] = checksum — filled in by finalizeChecksum
	copy(buf[10:16], "TEMU64")     // OEMID
	copy(buf[16:24], "TEMUACPI")   // OEM Table ID
	binary.LittleEndian.PutUint32(buf[24:28], 1) // OEM revision
	copy(buf[28:32], "TEMU")       // Creator ID
	binary.LittleEndian.PutUint32(buf[32:36], 1) // Creator revision
}

// finalizeChecksum makes the byte-sum over the first `length` bytes of
// `buf` equal to zero by writing the negated partial sum into
// buf[checksumOffset]. Required by every ACPI table (and the RSDP).
func finalizeChecksum(buf []byte, length int, checksumOffset int) {
	buf[checksumOffset] = 0
	var sum byte
	for i := 0; i < length; i++ {
		sum += buf[i]
	}
	buf[checksumOffset] = byte(-int8(sum))
}

// buildACPITables fills the supplied BIOS-shadow slice with a minimal
// RSDP + RSDT + FADT + MADT + HPET that satisfies Pure64's walker.
// `biosShadow` must be the entire low BIOS shadow buffer (256 KiB), and
// the caller is responsible for making sure the gap at acpiBase is
// otherwise empty (it is, in the bundled SeaBIOS image — see comment
// at the top of this file).
func buildACPITables(biosShadow []byte) {
	// Slice into the gap. The constants above are guest-physical, so
	// translate to BIOS-shadow offsets by subtracting BIOSROMAddr.
	const shadowOff = acpiBase - BIOSROMAddr

	// === RSDP (36 bytes — ACPI 2.0 layout) ===
	// Pure64 only reads the first 20 (v1.0 layout) but writing the
	// v2.0 length is harmless and lets later loaders treat it as v2.
	rsdp := biosShadow[shadowOff+acpiRSDPOff : shadowOff+acpiRSDPOff+36]
	copy(rsdp[0:8], "RSD PTR ")
	// rsdp[8] = checksum — patched after fill
	copy(rsdp[9:15], "TEMU64")             // OEMID
	rsdp[15] = 0                           // Revision = 0 (ACPI 1.0)
	binary.LittleEndian.PutUint32(rsdp[16:20], guestAddr(acpiRSDTOff))
	// v2.0 fields — present in the buffer but Pure64 v1 path ignores them.
	binary.LittleEndian.PutUint32(rsdp[20:24], 36) // length
	binary.LittleEndian.PutUint64(rsdp[24:32], 0)  // XsdtAddress = 0 (Pure64 routes via v1 path)
	// rsdp[32] = extended checksum (only meaningful in v2)
	rsdp[33], rsdp[34], rsdp[35] = 0, 0, 0
	finalizeChecksum(rsdp, 20, 8) // v1 checksum: first 20 bytes

	// === RSDT (header + 3 32-bit entries) ===
	// Entries: FADT, MADT, HPET. Pure64 walks all entries and
	// dispatches on signature.
	const rsdtEntries = 3
	rsdtLen := uint32(36 + 4*rsdtEntries)
	rsdt := biosShadow[shadowOff+acpiRSDTOff : shadowOff+acpiRSDTOff+int(rsdtLen)]
	writeACPITableHeader(rsdt, "RSDT", rsdtLen, 1)
	binary.LittleEndian.PutUint32(rsdt[36:40], guestAddr(acpiFADTOff))
	binary.LittleEndian.PutUint32(rsdt[40:44], guestAddr(acpiMADTOff))
	binary.LittleEndian.PutUint32(rsdt[44:48], guestAddr(acpiHPETOff))
	finalizeChecksum(rsdt, int(rsdtLen), 9)

	// === FADT (276 bytes — ACPI 6.x layout) ===
	// We zero-fill the body; the only fields Pure64 reads are the
	// header (checksum + length) and IAPC_BOOT_ARCH. Other loaders
	// might read more, in which case revisit and stamp the
	// power-management / SCI fields they need.
	const fadtLen = 276
	fadt := biosShadow[shadowOff+acpiFADTOff : shadowOff+acpiFADTOff+fadtLen]
	writeACPITableHeader(fadt, "FACP", fadtLen, 6)
	// IAPC_BOOT_ARCH at offset 109 — set bits for "legacy devices
	// present" and "8042 present" so the kernel knows the PS/2 path
	// is available. Pure64 falls back to these when it can't find
	// the field in CMOS.
	binary.LittleEndian.PutUint16(fadt[109:111], 0x03)
	finalizeChecksum(fadt, fadtLen, 9)

	// === MADT (header + Local APIC entry + IO APIC entry) ===
	// Layout per ACPI Vol 5.2.12:
	//   0x00..0x23  : standard table header
	//   0x24..0x27  : 32-bit Local APIC physical address
	//   0x28..0x2B  : 32-bit flags (bit 0 = 8259 PIC present)
	//   0x2C..      : variable-length interrupt-controller structures
	// We supply one "Processor Local APIC" (type 0, length 8) +
	// one "I/O APIC" (type 1, length 12). That covers everything
	// Pure64 will store in its InfoMap.
	const madtLen = 44 + 8 + 12
	madt := biosShadow[shadowOff+acpiMADTOff : shadowOff+acpiMADTOff+madtLen]
	writeACPITableHeader(madt, "APIC", madtLen, 5)
	binary.LittleEndian.PutUint32(madt[36:40], uint32(lapicBase))
	binary.LittleEndian.PutUint32(madt[40:44], 0x01) // PCAT_COMPAT — 8259 present
	// Local APIC entry — type=0, length=8, ACPI processor id=0,
	// APIC id=0, flags=1 (processor enabled).
	madt[44] = 0  // type
	madt[45] = 8  // length
	madt[46] = 0  // ACPI processor id
	madt[47] = 0  // APIC id
	binary.LittleEndian.PutUint32(madt[48:52], 0x01) // flags
	// I/O APIC entry — type=1, length=12, id=0, reserved=0, addr,
	// gsi-base=0.
	madt[52] = 1  // type
	madt[53] = 12 // length
	madt[54] = 0  // io-apic id
	madt[55] = 0  // reserved
	binary.LittleEndian.PutUint32(madt[56:60], uint32(ioapicBase))
	binary.LittleEndian.PutUint32(madt[60:64], 0) // GSI base
	finalizeChecksum(madt, madtLen, 9)

	// === HPET (56 bytes — ACPI Vol 6 HPET-spec layout) ===
	// We do not model the HPET MMIO. Pure64 reads the base address
	// from this table, the BareMetal kernel stores it, but nothing
	// actually pokes the HPET in our boot path. Leaving the rest of
	// the table zeroed is intentional — we'd rather Pure64 record
	// "no minimum tick" (which is treated as "I don't know") than
	// invent a plausible-but-wrong number.
	const hpetLen = 56
	hpet := biosShadow[shadowOff+acpiHPETOff : shadowOff+acpiHPETOff+hpetLen]
	writeACPITableHeader(hpet, "HPET", hpetLen, 1)
	// 36..39 : Event Timer Block ID (vendor, etc.) — leave zero
	// 40     : Address Space ID (0 = MMIO)
	hpet[40] = 0
	// 41 : Register Bit Width
	hpet[41] = 64
	// 42 : Register Bit Offset
	hpet[42] = 0
	// 43 : Reserved
	hpet[43] = 0
	// 44..51 : Base Address (64-bit)
	binary.LittleEndian.PutUint64(hpet[44:52], uint64(hpetBase))
	// 52     : HPET number (0 since we describe only one)
	hpet[52] = 0
	// 53..54 : minimum tick
	binary.LittleEndian.PutUint16(hpet[53:55], 0)
	// 55     : page protection
	hpet[55] = 0
	finalizeChecksum(hpet, hpetLen, 9)
}
