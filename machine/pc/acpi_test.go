package pc

import (
	"encoding/binary"
	"testing"
)

// TestBuildACPITables_RSDPSignature pins the single most load-bearing
// invariant — that the "RSD PTR " signature lands at acpiBase, on a
// 16-byte boundary, with a checksum that zeros the first-20-byte sum.
// Pure64's foundACPI scan walks 0xE0000..0xFFFFF in 16-byte strides
// looking for exactly this pattern. If the offset drifts or the
// checksum drifts, every Pure64-style payload silently halts at
// `noACPI:`.
func TestBuildACPITables_RSDPSignature(t *testing.T) {
	shadow := make([]byte, BIOSROMSize)
	buildACPITables(shadow)

	const rsdpShadowOff = acpiBase - BIOSROMAddr + acpiRSDPOff
	rsdp := shadow[rsdpShadowOff : rsdpShadowOff+20]
	if string(rsdp[0:8]) != "RSD PTR " {
		t.Fatalf("RSDP signature not at expected offset: got %q want %q",
			rsdp[0:8], "RSD PTR ")
	}
	// 16-byte alignment is implicit in acpiBase + acpiRSDPOff but
	// pin it anyway so a refactor doesn't accidentally break it.
	if (acpiBase+acpiRSDPOff)%16 != 0 {
		t.Fatalf("RSDP not 16-byte aligned (loaders scan on 16-byte boundaries): %#x", acpiBase+acpiRSDPOff)
	}
	var sum byte
	for _, b := range rsdp {
		sum += b
	}
	if sum != 0 {
		t.Fatalf("RSDP v1 checksum is %d, want 0 (first 20 bytes must sum to zero)", sum)
	}
}

// TestBuildACPITables_RSDTPointsAtSubTables verifies the RSDT entry
// pointers actually point at the tables we wrote. A loader walks RSDT,
// reads each entry's signature, and dispatches — if a pointer is wrong,
// the loader either bails or treats garbage as a table header.
func TestBuildACPITables_RSDTPointsAtSubTables(t *testing.T) {
	shadow := make([]byte, BIOSROMSize)
	buildACPITables(shadow)

	rsdpOff := acpiBase - BIOSROMAddr + acpiRSDPOff
	rsdtAddr := binary.LittleEndian.Uint32(shadow[rsdpOff+16 : rsdpOff+20])
	if rsdtAddr != guestAddr(acpiRSDTOff) {
		t.Fatalf("RSDP.RsdtAddress = %#x, want %#x", rsdtAddr, guestAddr(acpiRSDTOff))
	}
	rsdtShadow := rsdtAddr - BIOSROMAddr
	if string(shadow[rsdtShadow:rsdtShadow+4]) != "RSDT" {
		t.Fatalf("RSDT signature missing at shadow offset %#x", rsdtShadow)
	}
	// Three entries (FADT, MADT, HPET) starting at offset 36.
	wantSigs := map[uint32]string{
		guestAddr(acpiFADTOff): "FACP",
		guestAddr(acpiMADTOff): "APIC",
		guestAddr(acpiHPETOff): "HPET",
	}
	for i := 0; i < 3; i++ {
		entryAddr := binary.LittleEndian.Uint32(shadow[rsdtShadow+36+uint32(i)*4 : rsdtShadow+40+uint32(i)*4])
		sig := string(shadow[entryAddr-BIOSROMAddr : entryAddr-BIOSROMAddr+4])
		if wantSigs[entryAddr] != sig {
			t.Errorf("RSDT entry %d at %#x: sig=%q, want %q", i, entryAddr, sig, wantSigs[entryAddr])
		}
		delete(wantSigs, entryAddr)
	}
	if len(wantSigs) != 0 {
		t.Errorf("missing RSDT entries: %v", wantSigs)
	}
}

// TestBuildACPITables_MADTLapicBase verifies the MADT carries the
// expected Local APIC base. BareMetal reads this from Pure64's
// InfoMap; without it BareMetal's APIC init reads from address 0 and
// faults.
func TestBuildACPITables_MADTLapicBase(t *testing.T) {
	shadow := make([]byte, BIOSROMSize)
	buildACPITables(shadow)
	off := acpiBase - BIOSROMAddr + acpiMADTOff
	got := binary.LittleEndian.Uint32(shadow[off+36 : off+40])
	if got != uint32(lapicBase) {
		t.Errorf("MADT.LocalApicAddress = %#x, want %#x", got, lapicBase)
	}
}

// TestBuildACPITables_TableChecksums is the catch-all guard against
// stamp-and-forget header drift. Every ACPI table's first `length`
// bytes must byte-sum to zero; if a future change adds a field but
// forgets to re-finalize, this catches it.
func TestBuildACPITables_TableChecksums(t *testing.T) {
	shadow := make([]byte, BIOSROMSize)
	buildACPITables(shadow)
	tests := []struct {
		name string
		off  int
	}{
		{"RSDT", acpiRSDTOff},
		{"FADT", acpiFADTOff},
		{"MADT", acpiMADTOff},
		{"HPET", acpiHPETOff},
	}
	for _, tc := range tests {
		shadowOff := acpiBase - BIOSROMAddr + tc.off
		length := binary.LittleEndian.Uint32(shadow[shadowOff+4 : shadowOff+8])
		var sum byte
		for _, b := range shadow[shadowOff : shadowOff+int(length)] {
			sum += b
		}
		if sum != 0 {
			t.Errorf("%s checksum nonzero (sum=%d) — header probably out of sync with body", tc.name, sum)
		}
	}
}

