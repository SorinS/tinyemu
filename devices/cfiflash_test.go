package devices

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// newTestFlash registers a small CFI flash and returns it plus the memory map.
func newTestFlash(t *testing.T, size int) (*CFIFlash, *mem.PhysMemoryMap) {
	t.Helper()
	f := NewCFIFlash(size, 0xFF)
	m := mem.NewPhysMemoryMap()
	if err := f.Register(m, 0); err != nil {
		t.Fatalf("register: %v", err)
	}
	return f, m
}

func TestCFIReadArrayDefault(t *testing.T) {
	f, m := newTestFlash(t, 0x1000)
	f.Init([]byte{0x11, 0x22, 0x33, 0x44})
	// Power-on mode is read-array: reads return flash contents.
	if v, _ := m.Read32(0); v != 0x44332211 {
		t.Fatalf("read-array = %#x, want 0x44332211", v)
	}
	if v, _ := m.Read8(1); v != 0x22 {
		t.Fatalf("read8 = %#x, want 0x22", v)
	}
}

func TestCFIReadStatusReady(t *testing.T) {
	_, m := newTestFlash(t, 0x1000)
	// Issuing read-status (byte-replicated across the two x16 lanes) makes reads
	// return the status word with the ready bit set in both lanes.
	m.Write32(0, 0x00700070)
	if v, _ := m.Read32(0); v != 0x00800080 {
		t.Fatalf("status32 = %#x, want 0x00800080", v)
	}
	if v, _ := m.Read16(0); v != 0x0080 {
		t.Fatalf("status16 = %#x, want 0x0080", v)
	}
	// Read-array returns to normal reads.
	m.Write32(0, 0x00FF00FF)
	if v, _ := m.Read32(0); v != 0xFFFFFFFF {
		t.Fatalf("after read-array = %#x, want 0xFFFFFFFF", v)
	}
}

func TestCFISingleWordProgram(t *testing.T) {
	_, m := newTestFlash(t, 0x1000)
	// Program setup, then the data word; NOR programming only clears bits.
	m.Write32(0x10, 0x00400040) // program setup
	m.Write32(0x10, 0xA5A5A5A5) // data
	// After a program the device is in status mode and reports ready.
	if v, _ := m.Read32(0x10); v != 0x00800080 {
		t.Fatalf("post-program status = %#x, want ready", v)
	}
	m.Write32(0x10, 0x00FF00FF) // back to read-array
	if v, _ := m.Read32(0x10); v != 0xA5A5A5A5 {
		t.Fatalf("programmed word = %#x, want 0xA5A5A5A5", v)
	}
	// Programming again can only clear more bits (AND semantics).
	m.Write32(0x10, 0x00400040)
	m.Write32(0x10, 0x0F0F0F0F)
	m.Write32(0x10, 0x00FF00FF)
	if v, _ := m.Read32(0x10); v != (0xA5A5A5A5 & 0x0F0F0F0F) {
		t.Fatalf("re-program = %#x, want %#x", v, 0xA5A5A5A5&0x0F0F0F0F)
	}
}

func TestCFIBlockErase(t *testing.T) {
	f, m := newTestFlash(t, cfiSectorSize*2)
	// Dirty a word in block 0 and a word in block 1.
	f.mem[0x20] = 0x00
	f.mem[cfiSectorSize+0x20] = 0x00
	// Erase block 0 (setup 0x20 + confirm 0xD0 to an address within the block).
	m.Write32(0x100, 0x00200020)
	m.Write32(0x100, 0x00D000D0)
	if f.mem[0x20] != 0xFF {
		t.Fatalf("block 0 not erased: %#x", f.mem[0x20])
	}
	if f.mem[cfiSectorSize+0x20] != 0x00 {
		t.Fatalf("block 1 wrongly erased: %#x", f.mem[cfiSectorSize+0x20])
	}
}

func TestCFIBufferedProgram(t *testing.T) {
	_, m := newTestFlash(t, 0x1000)
	// Buffered program: setup (0xE8), count-1, data words, confirm (0xD0).
	m.Write32(0, 0x00E800E8) // buffer setup
	m.Write32(0, 2)          // 3 words (count - 1 = 2)
	m.Write32(0x00, 0x11111111)
	m.Write32(0x04, 0x22222222)
	m.Write32(0x08, 0x33333333)
	m.Write32(0, 0x00D000D0) // confirm
	m.Write32(0, 0x00FF00FF) // read-array
	for i, want := range []uint32{0x11111111, 0x22222222, 0x33333333} {
		if v, _ := m.Read32(uint64(i * 4)); v != want {
			t.Fatalf("buffered word %d = %#x, want %#x", i, v, want)
		}
	}
}
