package pc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// newVGATestRig builds a minimal rig: a memory map with the bottom
// 640 KB of RAM, an I/O dispatcher, and a VGA wired to both. No CPU is
// needed — VGA is pure I/O / memory dispatch.
func newVGATestRig(t *testing.T) (*IOPortDispatcher, *mem.PhysMemoryMap, *VGA) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 0xA0000, 0); err != nil {
		t.Fatalf("register low RAM: %v", err)
	}
	iod := NewIOPortDispatcher()
	vga := NewVGA()
	if err := vga.Register(iod, mm); err != nil {
		t.Fatalf("register VGA: %v", err)
	}
	return iod, mm, vga
}

// TestVGAFramebufferReadWrite verifies that reads and writes to the
// aperture round-trip through the device.
func TestVGAFramebufferReadWrite(t *testing.T) {
	_, mm, vga := newVGATestRig(t)

	// Write "Hi" + colour attribute (white-on-blue, 0x1F) at row 0, col 0.
	mm.Write8(0xB8000, 'H')
	mm.Write8(0xB8001, 0x1F)
	mm.Write8(0xB8002, 'i')
	mm.Write8(0xB8003, 0x1F)

	if got, _ := mm.Read8(0xB8000); got != 'H' {
		t.Errorf("read 'H': got %02X, want %02X", got, 'H')
	}
	if got, _ := mm.Read8(0xB8003); got != 0x1F {
		t.Errorf("read attr: got %02X, want 0x1F", got)
	}

	// Snapshot also reflects the writes.
	snap := vga.Snapshot()
	if snap[0] != 'H' || snap[2] != 'i' {
		t.Errorf("snapshot chars: got %q at 0, %q at 2; want 'H', 'i'", snap[0], snap[2])
	}
}

// TestVGACRTCCursorPosition verifies that programming the cursor low/high
// registers via 0x3D4/0x3D5 ports lets us read back the position.
func TestVGACRTCCursorPosition(t *testing.T) {
	iod, _, vga := newVGATestRig(t)

	// Cursor offset = row 5, col 10 → 5*80 + 10 = 410 = 0x019A.
	iod.Write8(0x3D4, 0x0E)
	iod.Write8(0x3D5, 0x01)
	iod.Write8(0x3D4, 0x0F)
	iod.Write8(0x3D5, 0x9A)

	col, row := vga.CursorPos()
	if col != 10 || row != 5 {
		t.Errorf("cursor pos = (%d, %d), want (10, 5)", col, row)
	}
}

// TestVGAAttributeControllerFlipFlop verifies the AC's index/data
// alternation behaviour through 0x3C0, and that reading 0x3DA resets
// the flip-flop.
func TestVGAAttributeControllerFlipFlop(t *testing.T) {
	iod, _, vga := newVGATestRig(t)

	// First write to 0x3C0: this is an index write (flip-flop = false).
	iod.Write8(0x3C0, 0x05) // index = 5
	if vga.acIndex != 5 {
		t.Errorf("after first write, acIndex = %d, want 5", vga.acIndex)
	}
	// Second write: data write to index 5.
	iod.Write8(0x3C0, 0xAA)
	if vga.acRegs[5] != 0xAA {
		t.Errorf("after second write, acRegs[5] = %02X, want 0xAA", vga.acRegs[5])
	}

	// Third write: should be an index write again.
	iod.Write8(0x3C0, 0x07)
	if vga.acIndex != 7 {
		t.Errorf("after third write, acIndex = %d, want 7 (flip-flop alternation)", vga.acIndex)
	}

	// Now read 0x3DA — should reset the flip-flop, so next write is an
	// index.
	_ = iod.Read8(0x3DA)
	iod.Write8(0x3C0, 0x0A)
	if vga.acIndex != 10 {
		t.Errorf("after reset+write, acIndex = %d, want 10", vga.acIndex)
	}
}

// TestVGAInputStatus1Toggle verifies that 0x3DA toggles bits 0 and 3 on
// each read — software polling these for retrace must see them change.
func TestVGAInputStatus1Toggle(t *testing.T) {
	iod, _, _ := newVGATestRig(t)

	a := iod.Read8(0x3DA)
	b := iod.Read8(0x3DA)
	if a == b {
		t.Errorf("0x3DA reads should toggle, got %02X both times", a)
	}
	// At least bit 0 (display enable) should differ.
	if (a^b)&0x01 == 0 {
		t.Errorf("0x3DA bit 0 not toggling: a=%02X b=%02X", a, b)
	}
}

// TestVGARenderText renders a known framebuffer and inspects the output
// to confirm the right characters appear.
func TestVGARenderText(t *testing.T) {
	_, mm, vga := newVGATestRig(t)

	// Write "Hello" + attribute 0x07 at row 0, col 0.
	msg := "Hello"
	for i, ch := range msg {
		mm.Write8(uint64(0xB8000+i*2), uint8(ch))
		mm.Write8(uint64(0xB8000+i*2+1), 0x07)
	}

	var buf bytes.Buffer
	vga.Render(&buf)
	out := buf.String()
	if !strings.Contains(out, "Hello") {
		t.Errorf("rendered output missing 'Hello'\n---\n%s\n---", out)
	}
	// Must contain ANSI cursor-home.
	if !strings.Contains(out, "\x1b[H") {
		t.Error("rendered output missing cursor-home escape")
	}
}

// TestVGARenderColorTranslation verifies that the VGA palette-to-ANSI
// mapping flips red and blue (the famous quirk).
func TestVGAFgAnsiColors(t *testing.T) {
	// VGA index 1 = blue → ANSI 4.
	if got := vgaFgAnsi(1); !strings.Contains(got, "34") {
		t.Errorf("VGA blue → expected ANSI 34, got %q", got)
	}
	// VGA index 4 = red → ANSI 1 (escape code 31).
	if got := vgaFgAnsi(4); !strings.Contains(got, "31") {
		t.Errorf("VGA red → expected ANSI 31, got %q", got)
	}
	// VGA index 15 = white → bright (escape code 97).
	if got := vgaFgAnsi(15); !strings.Contains(got, "97") {
		t.Errorf("VGA white → expected ANSI 97, got %q", got)
	}
}

// TestVGAUnknownPortsDontFault verifies the various stub ports (DAC,
// alternate monochrome aliases) return without crashing.
func TestVGAUnknownPortsDontFault(t *testing.T) {
	iod, _, _ := newVGATestRig(t)

	// These should all not panic and return some value.
	_ = iod.Read8(0x3BA)
	_ = iod.Read8(0x3C3)
	for p := uint16(0x3C6); p <= 0x3C9; p++ {
		_ = iod.Read8(p)
		iod.Write8(p, 0x42)
	}
}
