package pc

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func parseE820(t *testing.T, blob []byte) []e820Entry {
	t.Helper()
	if len(blob)%20 != 0 {
		t.Fatalf("e820 blob length %d is not a multiple of 20", len(blob))
	}
	var out []e820Entry
	r := bytes.NewReader(blob)
	for r.Len() > 0 {
		var e e820Entry
		if err := binary.Read(r, binary.LittleEndian, &e); err != nil {
			t.Fatalf("decode e820 entry: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// TestE820Below4GIsSingleBase0Entry is the OVMF memory-detection
// regression guard. OVMF's PlatformGetLowMemoryCB reads the *length* of
// the e820 RAM entry based at 0 as the total amount of below-4 GiB memory:
//
//	if (E820Entry->BaseAddr != 0) return;
//	PlatformInfoHob->LowMemory = (UINT32)E820Entry->Length;
//
// The old layout split low RAM around the EBDA/BIOS holes, so the base-0
// entry was only 640 KiB; OVMF then sized all of low memory at 640 KiB,
// could not fit its PEI permanent memory, and stranded it (and the
// variable store) near 4 GiB — a #PF. The below-4 GiB RAM must therefore
// be a single base-0 entry carrying the full length.
func TestE820Below4GIsSingleBase0Entry(t *testing.T) {
	const ram = 512 * 1024 * 1024
	entries := parseE820(t, buildE820(ram))

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 for a <4GiB config: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Start != 0 {
		t.Errorf("entry start = %#x, want 0", e.Start)
	}
	if e.Type != e820TypeRAM {
		t.Errorf("entry type = %d, want RAM(%d)", e.Type, e820TypeRAM)
	}
	if e.Size != ram {
		t.Errorf("base-0 RAM length = %#x, want %#x (full below-4GiB size; a short length is the 4GiB-stranding bug)", e.Size, uint64(ram))
	}
}

// TestE820Above4GSplit: RAM beyond the 32-bit PCI hole is relocated to a
// second entry based at 4 GiB, mirroring CMOS 0x5B/0x5C/0x5D.
func TestE820Above4GSplit(t *testing.T) {
	const ram = 5 * 1024 * 1024 * 1024 // 5 GiB
	entries := parseE820(t, buildE820(ram))

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 for a >hole config: %+v", len(entries), entries)
	}
	below, above := entries[0], entries[1]
	if below.Start != 0 || below.Size != pciHoleStart || below.Type != e820TypeRAM {
		t.Errorf("below entry = %+v, want {0, %#x, RAM}", below, uint64(pciHoleStart))
	}
	if above.Start != fourGiB || above.Type != e820TypeRAM {
		t.Errorf("above entry start = %#x type %d, want {%#x, RAM}", above.Start, above.Type, uint64(fourGiB))
	}
	if want := uint64(ram) - pciHoleStart; above.Size != want {
		t.Errorf("above entry size = %#x, want %#x", above.Size, want)
	}
	// Total RAM described must equal the configured size.
	if got := below.Size + above.Size; got != ram {
		t.Errorf("total e820 RAM = %#x, want %#x", got, uint64(ram))
	}
}

func TestRamSplit(t *testing.T) {
	cases := []struct {
		ram, below, above uint64
	}{
		{512 * 1024 * 1024, 512 * 1024 * 1024, 0},
		{pciHoleStart, pciHoleStart, 0},
		{pciHoleStart + 0x40000000, pciHoleStart, 0x40000000},
	}
	for _, c := range cases {
		below, above := ramSplit(c.ram)
		if below != c.below || above != c.above {
			t.Errorf("ramSplit(%#x) = (%#x, %#x), want (%#x, %#x)", c.ram, below, above, c.below, c.above)
		}
	}
}
