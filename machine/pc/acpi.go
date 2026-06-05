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
	rsdtLen   = 36 + 4*4    // header + 4 entries (FADT, MADT, HPET, DSDT)
	fadtLen   = 276
	madtLen   = 44 + 8 + 12 // header + Local-APIC entry + I/O-APIC entry
	hpetLen   = 56
	// DSDT minimum: 36-byte header + a 7-byte AML "Scope(\_SB_) {}"
	// definition block so Linux's namespace loader has something
	// valid to parse. Empty body wasn't enough — the AML interpreter
	// dereferenced a zero-length result and killed kernel-init.
	dsdtLen = 36 + 7

	// Offsets of each sub-table inside the tables blob.
	rsdtBlobOff = 0
	fadtBlobOff = rsdtBlobOff + rsdtLen
	madtBlobOff = fadtBlobOff + fadtLen
	hpetBlobOff = madtBlobOff + madtLen
	dsdtBlobOff = hpetBlobOff + hpetLen

	tablesBlobLen = dsdtBlobOff + dsdtLen

	// Where SeaBIOS will need to patch pointers into the RSDT.
	rsdtFadtPtrOff = 36 // points to FADT
	rsdtMadtPtrOff = 40 // points to MADT
	rsdtHpetPtrOff = 44 // points to HPET
	rsdtDsdtPtrOff = 48 // points to DSDT (so AcpiInitializeTables sees it)

	// The RSDP's RsdtAddress field — SeaBIOS patches this to the
	// absolute address where it allocates the tables blob.
	rsdpRsdtAddrOff = 16
	rsdpChecksumOff = 8

	// FADT fields we care about beyond the header.
	fadtDsdtOff   = 40  // 32-bit DSDT pointer
	fadtFlagsOff  = 112 // 32-bit Flags bitmap; bit 20 = HW_REDUCED_ACPI
	fadtXDsdtOff  = 140 // 64-bit DSDT pointer (ACPI 2.0+)
	fadtHwReduced = 1 << 20 // HW_REDUCED_ACPI — must stay CLEAR (see FADT note in tablesBlob)
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
	binary.LittleEndian.PutUint32(rsdt[rsdtDsdtPtrOff-rsdtBlobOff:], uint32(dsdtBlobOff))

	// --- FADT ---
	// IAPC_BOOT_ARCH legacy-device flags at offset 109 (legacy
	// devices present + 8042 present).
	//
	// Flags (offset 112) is left ZERO — in particular HW_REDUCED_ACPI
	// (bit 20) must NOT be set. temu is a legacy PC: it emulates an
	// 8259 PIC, PIT, 8042, and CMOS RTC. Setting HW_REDUCED tells
	// Linux the opposite, and on x86 acpi_generic_reduced_hw_init()
	// responds by pointing legacy_pic at null_legacy_pic and noop'ing
	// pre_vector_init — so the 16 legacy IRQ descriptors are never
	// preallocated ("preallocated irqs: 0"). Every driver that then
	// calls request_irq() on a legacy line (i8042/IRQ1, rtc_cmos/IRQ8,
	// virtio_blk via PCI INTx) fails with -EINVAL and the box can't
	// mount root. The PM1a-zero-address message HW_REDUCED used to
	// suppress is only a warning; ACPICA continues without it.
	//
	// DSDT pointer (32-bit at offset 40, 64-bit X_Dsdt at offset 140)
	// references the in-blob DSDT; installACPIDirect rewrites them to
	// absolute addresses when the blob is placed.
	fadt := buf[fadtBlobOff : fadtBlobOff+fadtLen]
	writeACPITableHeader(fadt, "FACP", fadtLen, 6)
	binary.LittleEndian.PutUint32(fadt[fadtDsdtOff:fadtDsdtOff+4], uint32(dsdtBlobOff))
	binary.LittleEndian.PutUint16(fadt[109:111], 0x03)
	binary.LittleEndian.PutUint64(fadt[fadtXDsdtOff:fadtXDsdtOff+8], uint64(dsdtBlobOff))

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

	// --- DSDT ---
	// Linux's ACPI loader insists on a parseable DSDT — empty body
	// triggers AE_NULL_OBJECT during namespace load and kills
	// kernel-init. The minimum that survives is a Scope(\_SB_) {}
	// definition block:
	//   0x10        ScopeOp
	//   0x06        PkgLength (6 bytes follow inside the package)
	//   0x5C        rootchar '\' prefix
	//   '_SB_'      4-char NameSeg
	// 7 bytes total after the standard 36-byte header.
	dsdt := buf[dsdtBlobOff : dsdtBlobOff+dsdtLen]
	writeACPITableHeader(dsdt, "DSDT", dsdtLen, 2)
	copy(dsdt[36:43], []byte{0x10, 0x06, 0x5C, '_', 'S', 'B', '_'})

	return buf
}

// Fixed low-memory addresses used by installACPIDirect. Both fall
// inside the legacy "F-segment" RSDP-scan range (0xE0000..0xFFFFF),
// so a Linux kernel that ignores boot_params.acpi_rsdp_addr (or a
// PVH kernel that ignores hvm_start_info.rsdp_paddr) still finds the
// RSDP via the standard scan. The SeaBIOS path picks its own
// addresses via the BiosLinker; only the direct-kernel-boot paths
// (bzImage64, vmlinux64, PVH) use these.
const (
	directRSDPAddr   uint32 = 0xE0000
	directTablesAddr uint32 = 0xE0080
)

// installACPIDirect writes a self-contained, finalised RSDP + tables
// blob to guest memory at the fixed direct-boot addresses and returns
// the physical address of the RSDP. Unlike the BiosLinker path which
// hands SeaBIOS unresolved pointers and zero checksums, this builds
// the tables with absolute addresses and computed checksums already
// in place — the direct-kernel-boot paths have no BIOS to do the
// relocation step for them.
//
// Callers should also write the returned RSDP address into the
// kernel's appropriate boot-protocol field (boot_params.acpi_rsdp_addr
// for bzImage64/vmlinux64, hvm_start_info.rsdp_paddr for PVH). The
// fixed low-memory placement is a belt-and-braces fallback for
// kernels that scan instead.
func installACPIDirect(p *PC) uint32 {
	tables := tablesBlob()

	// Patch the RSDT entries from blob-relative offsets to absolute
	// guest-physical addresses. tablesBlob() left these as offsets
	// because the SeaBIOS path needs that shape; the direct path
	// knows where the blob is going.
	binary.LittleEndian.PutUint32(tables[rsdtFadtPtrOff:], directTablesAddr+fadtBlobOff)
	binary.LittleEndian.PutUint32(tables[rsdtMadtPtrOff:], directTablesAddr+madtBlobOff)
	binary.LittleEndian.PutUint32(tables[rsdtHpetPtrOff:], directTablesAddr+hpetBlobOff)
	binary.LittleEndian.PutUint32(tables[rsdtDsdtPtrOff:], directTablesAddr+dsdtBlobOff)

	// Patch the DSDT pointers inside FADT (32-bit Dsdt at offset 40,
	// 64-bit X_Dsdt at offset 140) from blob-relative to absolute.
	dsdtAbs := directTablesAddr + dsdtBlobOff
	binary.LittleEndian.PutUint32(tables[fadtBlobOff+fadtDsdtOff:], dsdtAbs)
	binary.LittleEndian.PutUint64(tables[fadtBlobOff+fadtXDsdtOff:], uint64(dsdtAbs))

	// Finalise the checksum of each sub-table. ACPI requires that
	// the byte-sum across (length) bytes equal zero; the BiosLinker
	// would do this after relocation but here we do it ourselves.
	finalizeChecksum(tables[rsdtBlobOff:rsdtBlobOff+rsdtLen], rsdtLen, 9)
	finalizeChecksum(tables[fadtBlobOff:fadtBlobOff+fadtLen], fadtLen, 9)
	finalizeChecksum(tables[madtBlobOff:madtBlobOff+madtLen], madtLen, 9)
	finalizeChecksum(tables[hpetBlobOff:hpetBlobOff+hpetLen], hpetLen, 9)
	finalizeChecksum(tables[dsdtBlobOff:dsdtBlobOff+dsdtLen], dsdtLen, 9)

	// RSDP: ACPI 1.0 layout, checksum over the first 20 bytes.
	rsdp := rsdpBlob()
	binary.LittleEndian.PutUint32(rsdp[rsdpRsdtAddrOff:], directTablesAddr)
	finalizeChecksum(rsdp, 20, rsdpChecksumOff)

	// Write both blobs to guest physical memory.
	for i, b := range rsdp {
		p.writePhys8(directRSDPAddr+uint32(i), b)
	}
	for i, b := range tables {
		p.writePhys8(directTablesAddr+uint32(i), b)
	}
	return directRSDPAddr
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
	// Patch RSDT entries to point at FADT / MADT / HPET / DSDT inside
	// the tables blob. The destFile is tables itself — SeaBIOS
	// resolves "src == dest" to "patch relative to my own base".
	addPointer(tables, tables, rsdtFadtPtrOff, 4)
	addPointer(tables, tables, rsdtMadtPtrOff, 4)
	addPointer(tables, tables, rsdtHpetPtrOff, 4)
	addPointer(tables, tables, rsdtDsdtPtrOff, 4)
	// Patch FADT's Dsdt (32-bit) and X_Dsdt (64-bit) so the kernel's
	// namespace loader has a valid pointer to follow.
	addPointer(tables, tables, uint32(fadtBlobOff+fadtDsdtOff), 4)
	addPointer(tables, tables, uint32(fadtBlobOff+fadtXDsdtOff), 8)

	// Checksums. RSDP uses the ACPI 1.0 first-20-bytes form; every
	// other table uses the standard "sum all length bytes to zero".
	addChecksum(rsdp, rsdpChecksumOff, 0, 20)
	addChecksum(tables, rsdtBlobOff+9, rsdtBlobOff, rsdtLen)
	addChecksum(tables, fadtBlobOff+9, fadtBlobOff, fadtLen)
	addChecksum(tables, madtBlobOff+9, madtBlobOff, madtLen)
	addChecksum(tables, hpetBlobOff+9, hpetBlobOff, hpetLen)
	addChecksum(tables, dsdtBlobOff+9, dsdtBlobOff, dsdtLen)

	out := make([]byte, 0, len(entries)*biosLinkerEntrySize)
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}
