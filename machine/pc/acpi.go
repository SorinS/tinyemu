package pc

import "encoding/binary"

// Minimal ACPI table set, packaged for SeaBIOS's BiosLinker / fw_cfg
// path. We hand SeaBIOS three files via fw_cfg:
//
//   - etc/acpi/rsdp     — the 36-byte RSDP, with the RsdtAddress
//                         field left as an offset (zero); SeaBIOS
//                         patches it via ADD_POINTER once it knows
//                         where it has allocated the tables blob.
//   - etc/acpi/tables   — RSDT + FADT + MADT + HPET concatenated.
//                         The RSDT entries are stored as offsets
//                         into this same blob; SeaBIOS patches them
//                         to absolute addresses with ADD_POINTER.
//   - etc/table-loader  — the BiosLinker script telling SeaBIOS the
//                         allocation zone, the pointer fixups, and
//                         the checksum fixups it owes us.
//
// We do NOT pre-finalize the table checksums or pointers in the
// blob; that's the BiosLinker's job. Pre-finalized checksums would
// be wrong after SeaBIOS rewrites the pointers, and pre-baked
// absolute pointers would assume an allocation address we don't
// control.

const (
	// Where in the MADT we record the LAPIC base, and where in the
	// HPET descriptor we record the HPET MMIO base. The values come
	// from the LAPIC and IOAPIC stubs registered in PC.New (see
	// pc.go); changing them here without also changing the stubs
	// gives the guest an address it can't talk to.
	lapicBase  = 0xFEE00000
	ioapicBase = 0xFEC00000
	hpetBase   = 0xFED00000
)

// ACPI sub-table sizes. Kept as package-level constants so the
// blob-offset arithmetic in tablesBlob() and tableLoaderScript()
// agrees with both the builder and the tests.
const (
	rsdpLen   = 36
	rsdtLen   = 36 + 4*3    // header + 3 entries (FADT, MADT, HPET)
	fadtLen   = 276
	madtLen   = 44 + 8 + 12 // header + Local-APIC entry + I/O-APIC entry
	hpetLen   = 56

	// Offsets of each sub-table inside the tables blob.
	rsdtBlobOff = 0
	fadtBlobOff = rsdtBlobOff + rsdtLen
	madtBlobOff = fadtBlobOff + fadtLen
	hpetBlobOff = madtBlobOff + madtLen

	tablesBlobLen = hpetBlobOff + hpetLen

	// Where SeaBIOS will need to patch pointers into the RSDT.
	rsdtFadtPtrOff = 36 // points to FADT
	rsdtMadtPtrOff = 40 // points to MADT
	rsdtHpetPtrOff = 44 // points to HPET

	// The RSDP's RsdtAddress field — SeaBIOS patches this to the
	// absolute address where it allocates the tables blob.
	rsdpRsdtAddrOff = 16
	rsdpChecksumOff = 8
)

// writeACPITableHeader writes the standard 36-byte ACPI table header
// (sig, length, revision, checksum-placeholder, OEM, etc.). The
// caller fills in the body; the BiosLinker computes the final
// checksum after pointer patching.
func writeACPITableHeader(buf []byte, sig string, length uint32, revision byte) {
	copy(buf[0:4], sig)
	binary.LittleEndian.PutUint32(buf[4:8], length)
	buf[8] = revision
	// buf[9] = checksum — left at zero; BiosLinker rewrites it.
	copy(buf[10:16], "TEMU64")                   // OEMID
	copy(buf[16:24], "TEMUACPI")                 // OEM Table ID
	binary.LittleEndian.PutUint32(buf[24:28], 1) // OEM revision
	copy(buf[28:32], "TEMU")                     // Creator ID
	binary.LittleEndian.PutUint32(buf[32:36], 1) // Creator revision
}

// finalizeChecksum makes the byte-sum over the first `length` bytes
// of `buf` equal to zero by writing the negated partial sum into
// buf[checksumOffset]. Used by tests; the production blob lets the
// BiosLinker compute the same checksum at the right time.
func finalizeChecksum(buf []byte, length int, checksumOffset int) {
	buf[checksumOffset] = 0
	var sum byte
	for i := 0; i < length; i++ {
		sum += buf[i]
	}
	buf[checksumOffset] = byte(-int8(sum))
}

// rsdpBlob builds the 36-byte RSDP that SeaBIOS will place in low
// memory and announce to the firmware. RsdtAddress is left zero
// here; the BiosLinker patches it via ADD_POINTER and finalizes the
// checksum via ADD_CHECKSUM.
func rsdpBlob() []byte {
	r := make([]byte, rsdpLen)
	copy(r[0:8], "RSD PTR ")
	// r[8] = checksum (BiosLinker patches)
	copy(r[9:15], "TEMU64")
	r[15] = 0 // revision = 0 → ACPI v1.0, picks the RSDT path
	// r[16:20] = RsdtAddress — BiosLinker patches
	binary.LittleEndian.PutUint32(r[20:24], rsdpLen)
	// r[24:32] = XsdtAddress = 0 (we don't ship one; v1 consumers ignore it)
	// r[32]    = extended checksum (unused at revision=0)
	return r
}

// tablesBlob builds the concatenated RSDT + FADT + MADT + HPET. All
// inter-table pointers are stored as *offsets into this blob* — the
// BiosLinker rewrites them to absolute addresses with ADD_POINTER
// once it knows where it placed the blob in guest memory.
func tablesBlob() []byte {
	buf := make([]byte, tablesBlobLen)

	// --- RSDT ---
	rsdt := buf[rsdtBlobOff : rsdtBlobOff+rsdtLen]
	writeACPITableHeader(rsdt, "RSDT", rsdtLen, 1)
	binary.LittleEndian.PutUint32(rsdt[rsdtFadtPtrOff-rsdtBlobOff:], uint32(fadtBlobOff))
	binary.LittleEndian.PutUint32(rsdt[rsdtMadtPtrOff-rsdtBlobOff:], uint32(madtBlobOff))
	binary.LittleEndian.PutUint32(rsdt[rsdtHpetPtrOff-rsdtBlobOff:], uint32(hpetBlobOff))

	// --- FADT ---
	// Zero body except the IAPC_BOOT_ARCH legacy-device flags at
	// offset 109 (bits: legacy devices present + 8042 present). The
	// BiosLinker fills the checksum.
	fadt := buf[fadtBlobOff : fadtBlobOff+fadtLen]
	writeACPITableHeader(fadt, "FACP", fadtLen, 6)
	binary.LittleEndian.PutUint16(fadt[109:111], 0x03)

	// --- MADT ---
	madt := buf[madtBlobOff : madtBlobOff+madtLen]
	writeACPITableHeader(madt, "APIC", madtLen, 5)
	binary.LittleEndian.PutUint32(madt[36:40], uint32(lapicBase))
	binary.LittleEndian.PutUint32(madt[40:44], 0x01) // PCAT_COMPAT
	// Processor Local APIC entry (type 0, length 8)
	madt[44] = 0
	madt[45] = 8
	madt[46] = 0 // ACPI processor id
	madt[47] = 0 // APIC id
	binary.LittleEndian.PutUint32(madt[48:52], 0x01) // flags (enabled)
	// I/O APIC entry (type 1, length 12)
	madt[52] = 1
	madt[53] = 12
	madt[54] = 0 // io-apic id
	madt[55] = 0 // reserved
	binary.LittleEndian.PutUint32(madt[56:60], uint32(ioapicBase))
	binary.LittleEndian.PutUint32(madt[60:64], 0) // GSI base

	// --- HPET ---
	hpet := buf[hpetBlobOff : hpetBlobOff+hpetLen]
	writeACPITableHeader(hpet, "HPET", hpetLen, 1)
	// 40 = Address Space ID (0 = MMIO), 41 = bit width, 44..51 = base addr
	hpet[40] = 0
	hpet[41] = 64
	binary.LittleEndian.PutUint64(hpet[44:52], uint64(hpetBase))

	return buf
}

// BiosLinker (table-loader) command layout. Each entry is exactly
// 128 bytes; the leading 4 bytes are the command code, the rest is
// a union sized to the largest variant (124 bytes of payload).
const (
	biosLinkerCmdAllocate   = 1
	biosLinkerCmdAddPointer = 2
	biosLinkerCmdAddChecksum = 3

	biosLinkerEntrySize = 128
	biosLinkerFileSize  = 56 // null-padded file name field

	biosLinkerZoneHigh = 1
	biosLinkerZoneLow  = 2 // F-segment / EBDA accessible; needed for RSDP
)

// tableLoaderScript returns the binary BiosLinker / fw_cfg table-loader
// script. SeaBIOS reads it, allocates space for each declared file in
// the requested zone, then walks the ADD_POINTER and ADD_CHECKSUM
// entries to patch pointer fields and finalize checksums.
//
// Order matters: ALLOCATE entries must precede ADD_POINTER /
// ADD_CHECKSUM entries that name them.
func tableLoaderScript() []byte {
	var entries [][]byte

	allocate := func(file string, alignment uint32, zone byte) {
		e := make([]byte, biosLinkerEntrySize)
		binary.LittleEndian.PutUint32(e[0:4], biosLinkerCmdAllocate)
		copy(e[4:4+biosLinkerFileSize], file)
		binary.LittleEndian.PutUint32(e[4+biosLinkerFileSize:4+biosLinkerFileSize+4], alignment)
		e[4+biosLinkerFileSize+4] = zone
		entries = append(entries, e)
	}
	addPointer := func(destFile, srcFile string, offset uint32, size byte) {
		e := make([]byte, biosLinkerEntrySize)
		binary.LittleEndian.PutUint32(e[0:4], biosLinkerCmdAddPointer)
		copy(e[4:4+biosLinkerFileSize], destFile)
		copy(e[4+biosLinkerFileSize:4+2*biosLinkerFileSize], srcFile)
		binary.LittleEndian.PutUint32(e[4+2*biosLinkerFileSize:4+2*biosLinkerFileSize+4], offset)
		e[4+2*biosLinkerFileSize+4] = size
		entries = append(entries, e)
	}
	addChecksum := func(file string, checksumOff, start, length uint32) {
		e := make([]byte, biosLinkerEntrySize)
		binary.LittleEndian.PutUint32(e[0:4], biosLinkerCmdAddChecksum)
		copy(e[4:4+biosLinkerFileSize], file)
		binary.LittleEndian.PutUint32(e[4+biosLinkerFileSize:4+biosLinkerFileSize+4], checksumOff)
		binary.LittleEndian.PutUint32(e[4+biosLinkerFileSize+4:4+biosLinkerFileSize+8], start)
		binary.LittleEndian.PutUint32(e[4+biosLinkerFileSize+8:4+biosLinkerFileSize+12], length)
		entries = append(entries, e)
	}

	const tables = "etc/acpi/tables"
	const rsdp = "etc/acpi/rsdp"

	// Allocate the tables blob anywhere — SeaBIOS prefers low RAM
	// for ACPI; we let it choose. 16-byte aligned per ACPI spec.
	allocate(tables, 16, biosLinkerZoneHigh)
	// The RSDP must sit in low memory so the legacy F-segment scan
	// can reach it; ask for zone=low explicitly.
	allocate(rsdp, 16, biosLinkerZoneLow)

	// Patch RSDP.RsdtAddress to point at the start of the tables blob.
	addPointer(rsdp, tables, rsdpRsdtAddrOff, 4)
	// Patch RSDT entries to point at FADT / MADT / HPET inside the
	// tables blob. The destFile is tables itself — SeaBIOS resolves
	// "src == dest" to "patch relative to my own base".
	addPointer(tables, tables, rsdtFadtPtrOff, 4)
	addPointer(tables, tables, rsdtMadtPtrOff, 4)
	addPointer(tables, tables, rsdtHpetPtrOff, 4)

	// Checksums. RSDP uses the ACPI 1.0 first-20-bytes form; every
	// other table uses the standard "sum all length bytes to zero".
	addChecksum(rsdp, rsdpChecksumOff, 0, 20)
	addChecksum(tables, rsdtBlobOff+9, rsdtBlobOff, rsdtLen)
	addChecksum(tables, fadtBlobOff+9, fadtBlobOff, fadtLen)
	addChecksum(tables, madtBlobOff+9, madtBlobOff, madtLen)
	addChecksum(tables, hpetBlobOff+9, hpetBlobOff, hpetLen)

	out := make([]byte, 0, len(entries)*biosLinkerEntrySize)
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}
