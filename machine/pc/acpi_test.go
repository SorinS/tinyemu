package pc

import (
	"encoding/binary"
	"testing"
)

// TestRSDPBlob_SignatureAndLayout pins the load-bearing invariants of
// the RSDP blob handed to SeaBIOS via fw_cfg:
//   - "RSD PTR " at offset 0 (firmware scan key)
//   - 36-byte length
//   - revision = 0 (drives Pure64/legacy down the RSDT path)
//   - RsdtAddress field is *zero* (BiosLinker patches it; pre-baked
//     values would lock us to a specific allocation address)
func TestRSDPBlob_SignatureAndLayout(t *testing.T) {
	r := rsdpBlob()
	if len(r) != rsdpLen {
		t.Fatalf("rsdpBlob length = %d, want %d", len(r), rsdpLen)
	}
	if string(r[0:8]) != "RSD PTR " {
		t.Errorf("signature = %q, want %q", r[0:8], "RSD PTR ")
	}
	if r[15] != 0 {
		t.Errorf("revision = %d, want 0 (ACPI v1.0 / RSDT path)", r[15])
	}
	rsdtAddr := binary.LittleEndian.Uint32(r[rsdpRsdtAddrOff : rsdpRsdtAddrOff+4])
	if rsdtAddr != 0 {
		t.Errorf("RsdtAddress = %#x, want 0 (BiosLinker patches it; pre-baked values lie)",
			rsdtAddr)
	}
}

// TestTablesBlob_SignaturesAtExpectedOffsets verifies the
// concatenated blob is laid out the way the BiosLinker script
// expects: RSDT at 0, FADT after, then MADT, then HPET. If a sub-
// table grows and the offset constants drift, this catches it.
func TestTablesBlob_SignaturesAtExpectedOffsets(t *testing.T) {
	b := tablesBlob()
	if len(b) != tablesBlobLen {
		t.Fatalf("tablesBlob length = %d, want %d", len(b), tablesBlobLen)
	}
	checks := []struct {
		name string
		off  int
		sig  string
	}{
		{"RSDT", rsdtBlobOff, "RSDT"},
		{"FADT", fadtBlobOff, "FACP"},
		{"MADT", madtBlobOff, "APIC"},
		{"HPET", hpetBlobOff, "HPET"},
	}
	for _, c := range checks {
		got := string(b[c.off : c.off+4])
		if got != c.sig {
			t.Errorf("%s signature at offset %#x: got %q, want %q",
				c.name, c.off, got, c.sig)
		}
	}
}

// TestTablesBlob_RSDTPointersAreRelative makes sure the RSDT entry
// pointers carry *file-relative offsets*, not absolute addresses.
// The BiosLinker reads these, adds the allocated base, and writes
// back the absolute. Pre-baked absolutes would point at random
// memory after SeaBIOS allocates the blob.
func TestTablesBlob_RSDTPointersAreRelative(t *testing.T) {
	b := tablesBlob()
	want := []struct {
		field string
		off   int
		val   uint32
	}{
		{"FADT", rsdtFadtPtrOff, uint32(fadtBlobOff)},
		{"MADT", rsdtMadtPtrOff, uint32(madtBlobOff)},
		{"HPET", rsdtHpetPtrOff, uint32(hpetBlobOff)},
	}
	for _, w := range want {
		got := binary.LittleEndian.Uint32(b[w.off : w.off+4])
		if got != w.val {
			t.Errorf("RSDT->%s pointer at offset %#x = %#x, want %#x (file-relative)",
				w.field, w.off, got, w.val)
		}
	}
}

// TestTablesBlob_MADTLapicBase verifies the LAPIC base in MADT
// matches the LAPIC stub we register in PC.New.
func TestTablesBlob_MADTLapicBase(t *testing.T) {
	b := tablesBlob()
	got := binary.LittleEndian.Uint32(b[madtBlobOff+36 : madtBlobOff+40])
	if got != uint32(lapicBase) {
		t.Errorf("MADT.LocalApicAddress = %#x, want %#x", got, lapicBase)
	}
}

// TestTableLoaderScript_HasRequiredOps decodes the BiosLinker script
// and confirms it contains the commands we expect SeaBIOS to need:
//   - ALLOCATE for both file names
//   - ADD_POINTER for the RSDP's RsdtAddress (size 4)
//   - ADD_CHECKSUM for the RSDP at offset 8 covering 20 bytes
// Tests that fail here mean SeaBIOS will either skip our tables or
// finalize them wrong.
func TestTableLoaderScript_HasRequiredOps(t *testing.T) {
	script := tableLoaderScript()
	if len(script)%biosLinkerEntrySize != 0 {
		t.Fatalf("script length %d not a multiple of entry size %d",
			len(script), biosLinkerEntrySize)
	}
	var (
		sawAllocateRSDP, sawAllocateTables bool
		sawRSDPRsdtPointer, sawRSDPChecksum bool
	)
	for i := 0; i < len(script); i += biosLinkerEntrySize {
		e := script[i : i+biosLinkerEntrySize]
		cmd := binary.LittleEndian.Uint32(e[0:4])
		// First file-name field always lives at offset 4.
		name := nullTerm(e[4 : 4+biosLinkerFileSize])
		switch cmd {
		case biosLinkerCmdAllocate:
			if name == "etc/acpi/rsdp" {
				sawAllocateRSDP = true
			}
			if name == "etc/acpi/tables" {
				sawAllocateTables = true
			}
		case biosLinkerCmdAddPointer:
			if name == "etc/acpi/rsdp" {
				off := binary.LittleEndian.Uint32(e[4+2*biosLinkerFileSize : 4+2*biosLinkerFileSize+4])
				size := e[4+2*biosLinkerFileSize+4]
				if off == rsdpRsdtAddrOff && size == 4 {
					sawRSDPRsdtPointer = true
				}
			}
		case biosLinkerCmdAddChecksum:
			if name == "etc/acpi/rsdp" {
				off := binary.LittleEndian.Uint32(e[4+biosLinkerFileSize : 4+biosLinkerFileSize+4])
				start := binary.LittleEndian.Uint32(e[4+biosLinkerFileSize+4 : 4+biosLinkerFileSize+8])
				length := binary.LittleEndian.Uint32(e[4+biosLinkerFileSize+8 : 4+biosLinkerFileSize+12])
				if off == rsdpChecksumOff && start == 0 && length == 20 {
					sawRSDPChecksum = true
				}
			}
		}
	}
	if !sawAllocateRSDP {
		t.Error("missing ALLOCATE etc/acpi/rsdp")
	}
	if !sawAllocateTables {
		t.Error("missing ALLOCATE etc/acpi/tables")
	}
	if !sawRSDPRsdtPointer {
		t.Error("missing ADD_POINTER for RSDP.RsdtAddress (size=4, off=16)")
	}
	if !sawRSDPChecksum {
		t.Error("missing ADD_CHECKSUM for RSDP (off=8, length=20)")
	}
}

func nullTerm(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
