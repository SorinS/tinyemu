package pc

import (
	"encoding/binary"
	"testing"
)

// TestFWCfg_SignaturePresent — SeaBIOS probes selector 0 for the
// "QEMU" magic to decide whether fw_cfg is present at all. If this
// test fails the channel won't even be probed further by firmware.
func TestFWCfg_SignaturePresent(t *testing.T) {
	f := newFWCfg()
	f.writeSelector(fwCfgSelSignature)
	got := drainFWCfg(f, 4)
	if string(got) != "QEMU" {
		t.Errorf("signature = %q, want %q", got, "QEMU")
	}
}

// TestFWCfg_IDFeatureBits — bit 0 of the ID field tells SeaBIOS
// "fw_cfg supported"; if we ever stop setting it SeaBIOS treats the
// whole channel as absent even though the signature reads OK.
func TestFWCfg_IDFeatureBits(t *testing.T) {
	f := newFWCfg()
	f.writeSelector(fwCfgSelID)
	got := drainFWCfg(f, 4)
	id := binary.LittleEndian.Uint32(got)
	if id&0x1 == 0 {
		t.Errorf("ID = %#x, expected bit 0 (fw_cfg present) to be set", id)
	}
}

// TestFWCfg_FileDirectory verifies the file-directory format is
// big-endian and self-consistent: the count matches the number of
// files we added, each entry's size matches the actual file size,
// and each entry's name reads back NUL-terminated.
func TestFWCfg_FileDirectory(t *testing.T) {
	f := newFWCfg()
	helloSel := f.addFile("etc/hello", []byte("hello world"))
	binSel := f.addFile("etc/bin", []byte{0x01, 0x02, 0x03})

	f.writeSelector(fwCfgSelFileDir)
	got := drainFWCfg(f, 4+2*fwCfgFileEntrySize)
	count := binary.BigEndian.Uint32(got[0:4])
	if count != 2 {
		t.Fatalf("directory count = %d, want 2", count)
	}
	want := []struct {
		name string
		sel  uint16
		size uint32
	}{
		{"etc/hello", helloSel, 11},
		{"etc/bin", binSel, 3},
	}
	off := 4
	for i, w := range want {
		size := binary.BigEndian.Uint32(got[off : off+4])
		sel := binary.BigEndian.Uint16(got[off+4 : off+6])
		name := nullTerm(got[off+8 : off+8+fwCfgFileNameSize])
		if size != w.size || sel != w.sel || name != w.name {
			t.Errorf("entry[%d] = (size=%d sel=%#x name=%q), want (size=%d sel=%#x name=%q)",
				i, size, sel, name, w.size, w.sel, w.name)
		}
		off += fwCfgFileEntrySize
	}
}

// TestFWCfg_FileReadbackByName ensures that selecting a per-file
// selector (the one published in the directory) returns exactly the
// bytes added — no length prefix, no padding, no off-by-one.
func TestFWCfg_FileReadbackByName(t *testing.T) {
	f := newFWCfg()
	payload := []byte("0123456789abcdef")
	sel := f.addFile("etc/test", payload)

	f.writeSelector(uint32(sel))
	got := drainFWCfg(f, len(payload))
	if string(got) != string(payload) {
		t.Errorf("file readback = %q, want %q", got, payload)
	}
}

// TestFWCfg_OffsetAutoIncrements covers the byte-by-byte read model
// SeaBIOS uses: each read from port 0x511 returns one byte from the
// current selector at the current offset, then bumps the offset.
// Off-by-one in the increment would corrupt every multi-byte field
// (ACPI table headers, BiosLinker entries, ...).
func TestFWCfg_OffsetAutoIncrements(t *testing.T) {
	f := newFWCfg()
	sel := f.addFile("etc/seq", []byte{0xDE, 0xAD, 0xBE, 0xEF})
	f.writeSelector(uint32(sel))
	for i, want := range []byte{0xDE, 0xAD, 0xBE, 0xEF} {
		got := byte(f.readData())
		if got != want {
			t.Errorf("byte[%d] = %#x, want %#x", i, got, want)
		}
	}
	// Past the end: should return zero, not panic, not loop.
	for i := 0; i < 4; i++ {
		if got := f.readData(); got != 0 {
			t.Errorf("post-EOF read[%d] = %#x, want 0", i, got)
		}
	}
}

// TestFWCfg_SelectorResetsOffset — the QEMU protocol resets the
// read offset whenever a selector is written. Without this, a guest
// switching from one file to another (or re-reading the same file
// from the start) would inherit the previous file's tail offset.
func TestFWCfg_SelectorResetsOffset(t *testing.T) {
	f := newFWCfg()
	sel := f.addFile("etc/x", []byte{1, 2, 3, 4})
	f.writeSelector(uint32(sel))
	_ = f.readData() // consume one byte
	_ = f.readData()
	f.writeSelector(uint32(sel)) // re-select same file
	if got := byte(f.readData()); got != 1 {
		t.Errorf("after re-select, first byte = %#x, want 1 (offset must reset)", got)
	}
}

// drainFWCfg reads `n` bytes from the data port and returns them.
// Test helper.
func drainFWCfg(f *fwCfg, n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(f.readData())
	}
	return out
}
