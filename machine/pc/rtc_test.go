package pc

import "testing"

// read drives the CMOS index/data port pair the way firmware does, so the
// test exercises the same path (readData) the BIOS uses.
func cmosRead(c *CMOSRTC, idx uint8) uint8 {
	c.index = idx & 0x7F
	return c.readData()
}

// TestCMOSMemoryRegisters512M checks the memory-sizing registers for the
// common 512 MiB config: everything is below the PCI hole, so the
// below-4 GiB fields describe all of RAM and the above-4 GiB fields are
// zero. 0x34/0x35 hold extended memory above 16 MiB in 64-KiB units:
// (512-16) MiB / 64 KiB = 7936 = 0x1F00.
func TestCMOSMemoryRegisters512M(t *testing.T) {
	c := NewCMOSRTC(512 * 1024) // memSize in KiB

	if lo, hi := cmosRead(c, 0x34), cmosRead(c, 0x35); lo != 0x00 || hi != 0x1F {
		t.Errorf("0x34/0x35 = %#x/%#x, want 0x00/0x1F (496 MiB / 64 KiB)", lo, hi)
	}
	// 0x17/0x18 (and the 0x30/0x31 mirror) saturate at 0xFFFF for >64 MiB.
	for _, idx := range []uint8{0x17, 0x18, 0x30, 0x31} {
		if v := cmosRead(c, idx); v != 0xFF {
			t.Errorf("0x%02x = %#x, want 0xFF (saturated)", idx, v)
		}
	}
	// No RAM above 4 GiB → 0x5B/0x5C/0x5D are zero.
	for _, idx := range []uint8{0x5B, 0x5C, 0x5D} {
		if v := cmosRead(c, idx); v != 0 {
			t.Errorf("0x%02x = %#x, want 0 (no above-4GiB RAM)", idx, v)
		}
	}
}

// TestCMOSAbove4GRegisters: a config larger than the 3 GiB PCI hole puts
// the overflow above 4 GiB, reported in 0x5B/0x5C/0x5D in 64-KiB units.
// These were previously hard-zero, under-reporting total RAM for any
// >3 GiB guest.
func TestCMOSAbove4GRegisters(t *testing.T) {
	const ramBytes = 5 * 1024 * 1024 * 1024 // 5 GiB
	c := NewCMOSRTC(ramBytes / 1024)

	above := uint64(ramBytes) - pciHoleStart // 2 GiB above 4 GiB
	want := above / (64 * 1024)
	gotLo := uint64(cmosRead(c, 0x5B))
	gotMid := uint64(cmosRead(c, 0x5C))
	gotHi := uint64(cmosRead(c, 0x5D))
	got := gotLo | gotMid<<8 | gotHi<<16
	if got != want {
		t.Errorf("0x5B/0x5C/0x5D = %#x (%02x/%02x/%02x), want %#x (above-4GiB / 64 KiB)",
			got, gotLo, gotMid, gotHi, want)
	}

	// Below-4 GiB extended fields are now capped at the hole, not total RAM.
	below4GKB := uint32(pciHoleStart / 1024)
	wantExt64K := (below4GKB - 16*1024) / 64
	gotExt64K := uint32(cmosRead(c, 0x34)) | uint32(cmosRead(c, 0x35))<<8
	if gotExt64K != wantExt64K {
		t.Errorf("0x34/0x35 = %#x, want %#x (capped at PCI hole)", gotExt64K, wantExt64K)
	}
}
