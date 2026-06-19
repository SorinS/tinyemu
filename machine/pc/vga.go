package pc

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/sorins/tinyemu-go/mem"
)

// VGA models a minimal IBM VGA adapter sufficient to:
//
//   - Accept memory writes to the 128 KB aperture at 0xA0000-0xBFFFF
//     without faulting (Linux's vga_con writes characters into the text
//     buffer at 0xB8000).
//   - Service the legacy register ports (CRTC at 0x3D4/0x3D5, sequencer
//     at 0x3C4/0x3C5, attribute controller at 0x3C0/0x3C1, graphics
//     controller at 0x3CE/0x3CF, plus miscellaneous output / input
//     status registers) without faulting.
//   - Optionally render the 80×25 text-mode framebuffer to a writer
//     (stderr by default when TINYEMU_X86_VGA_RENDER=1) as ANSI escape
//     sequences, so a developer can watch what the VGA console would
//     have shown if it were connected to a real monitor.
//
// Scope deliberately excludes:
//
//   - VGA graphics modes (the 4-plane and chained 256-color layouts).
//   - Mode programming via the CRTC/sequencer (the actual display
//     timing is irrelevant when we render in software).
//   - Palette programming via the DAC (we use ANSI fallback colours).
//   - VBE / VESA BIOS Extensions.
//
// These would matter if we wanted to run X11 or DOS games inside the
// emulator; for "boot a serial-and-VGA Linux kernel to a shell," the
// minimum above is enough.
//
// Reference:
//   - IBM Personal System/2 Hardware Interface Technical Reference,
//     "Video Subsystem" (the original VGA spec).
//   - osdev.org "VGA Hardware" article.
//   - Linux drivers/video/console/vgacon.c
type VGA struct {
	mu       sync.Mutex
	aperture [VGAApertureSize]byte // 128 KB framebuffer aperture

	// CRTC controller. Index at 0x3D4; data at 0x3D5. The cursor
	// position is stored at index 0x0E (high) / 0x0F (low) — the only
	// CRTC registers the kernel touches frequently.
	crtcIndex uint8
	crtcRegs  [256]uint8

	// Sequencer at 0x3C4 (index) / 0x3C5 (data). Mostly programmed
	// once at mode-set time and forgotten.
	seqIndex uint8
	seqRegs  [256]uint8

	// Graphics controller at 0x3CE / 0x3CF.
	gcIndex uint8
	gcRegs  [256]uint8

	// Attribute controller at 0x3C0. Internal flip-flop selects between
	// "next write is an index" and "next write is the data for that
	// index". Reading port 0x3DA resets the flip-flop to index mode.
	acFlipFlop bool
	acIndex    uint8
	acRegs     [256]uint8

	// Miscellaneous output register (0x3C2 write / 0x3CC read). Mode
	// selection bits, plus "I/O address select" between the mono and
	// colour CRTC port aliases.
	miscOut uint8

	// Sink for rendered framebuffer output. If nil, rendering is off.
	renderSink io.Writer

	// Sink for raw character writes to the text-mode framebuffer (every
	// printable byte written to an even offset inside 0xB8000..0xBFFFF).
	// Useful when a BIOS / kernel prints via direct framebuffer writes
	// and never bothers to update the CRTC cursor (so renderSink, which
	// fires on cursor update, never triggers). Off by default; set via
	// TINYEMU_VGA_CHAR_LOG=<path> or =stderr.
	charSink io.Writer

	// Toggle for the input-status-1 "display-enable" bit (0x3DA bit 0).
	// We flip it on each read so polling-for-retrace loops don't spin.
	inputStatus1 uint8
}

// VGA hardware constants.
const (
	// VGAApertureBase / Size are already declared in pc.go.
	vgaTextOffset = 0xB8000 - VGAApertureAddr // offset within the aperture
	vgaTextCols   = 80
	vgaTextRows   = 25
)

// vgaRenderDefault picks an initial render sink based on environment.
// TINYEMU_X86_VGA_RENDER=1 sends rendered text-mode output to stderr.
func vgaRenderDefault() io.Writer {
	if os.Getenv("TINYEMU_X86_VGA_RENDER") == "1" {
		return os.Stderr
	}
	return nil
}

// NewVGA constructs a VGA device with default state. The render sink is
// stderr if TINYEMU_X86_VGA_RENDER=1 is set, otherwise nil (silent).
// The char sink (every framebuffer byte that's a printable ASCII char)
// is opened from TINYEMU_VGA_CHAR_LOG: `=stderr` or a file path.
func NewVGA() *VGA {
	v := &VGA{renderSink: vgaRenderDefault()}
	switch s := os.Getenv("TINYEMU_VGA_CHAR_LOG"); s {
	case "":
		// off
	case "stderr":
		v.charSink = os.Stderr
	default:
		if f, err := os.OpenFile(s, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644); err == nil {
			v.charSink = f
			fmt.Fprintf(os.Stderr, "[VGA] character writes tracing to %s\n", s)
		}
	}
	return v
}

// SetRenderSink replaces the output sink. Pass nil to disable rendering.
func (v *VGA) SetRenderSink(w io.Writer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.renderSink = w
}

// Register wires the VGA into the I/O dispatcher and the physical
// memory map. After this call, every legacy VGA port and the 128 KB
// aperture at 0xA0000-0xBFFFF are owned by this device.
func (v *VGA) Register(iod *IOPortDispatcher, mm *mem.PhysMemoryMap) error {
	// Framebuffer aperture: 0xA0000-0xBFFFF (128 KB) routed to readMem/writeMem.
	// Accept 8-, 16-, and 32-bit accesses — Linux's vga_con writes 16-bit
	// (char + attribute) words; memcpy-style code uses 32-bit; some
	// firmware does 8-bit.
	devFlags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	if _, err := mm.RegisterDevice(VGAApertureAddr, VGAApertureSize, v,
		func(opaque any, offset uint32, sizeLog2 int) uint32 {
			return opaque.(*VGA).readMem(offset, sizeLog2)
		},
		func(opaque any, offset uint32, val uint32, sizeLog2 int) {
			opaque.(*VGA).writeMem(offset, val, sizeLog2)
		}, devFlags); err != nil {
		return fmt.Errorf("register VGA aperture: %w", err)
	}

	// CRTC index / data.
	iod.RegisterRead(0x3D4, 0x3D4, func(uint16) uint32 { return uint32(v.crtcIndex) })
	iod.RegisterWrite(0x3D4, 0x3D4, func(_ uint16, val uint32) { v.crtcIndex = uint8(val) })
	iod.RegisterRead(0x3D5, 0x3D5, func(uint16) uint32 {
		v.mu.Lock()
		defer v.mu.Unlock()
		return uint32(v.crtcRegs[v.crtcIndex])
	})
	iod.RegisterWrite(0x3D5, 0x3D5, func(_ uint16, val uint32) {
		v.mu.Lock()
		v.crtcRegs[v.crtcIndex] = uint8(val)
		// Index 0x0F is the cursor's low byte — the kernel updates this
		// last after writing a chunk of text, which makes it the natural
		// place to trigger a render of the framebuffer.
		shouldRender := v.crtcIndex == 0x0F && v.renderSink != nil
		var sink io.Writer
		if shouldRender {
			sink = v.renderSink
		}
		v.mu.Unlock()
		if shouldRender {
			v.Render(sink)
		}
	})

	// Sequencer.
	iod.RegisterRead(0x3C4, 0x3C4, func(uint16) uint32 { return uint32(v.seqIndex) })
	iod.RegisterWrite(0x3C4, 0x3C4, func(_ uint16, val uint32) { v.seqIndex = uint8(val) })
	iod.RegisterRead(0x3C5, 0x3C5, func(uint16) uint32 {
		v.mu.Lock()
		defer v.mu.Unlock()
		return uint32(v.seqRegs[v.seqIndex])
	})
	iod.RegisterWrite(0x3C5, 0x3C5, func(_ uint16, val uint32) {
		v.mu.Lock()
		v.seqRegs[v.seqIndex] = uint8(val)
		v.mu.Unlock()
	})

	// Graphics controller.
	iod.RegisterRead(0x3CE, 0x3CE, func(uint16) uint32 { return uint32(v.gcIndex) })
	iod.RegisterWrite(0x3CE, 0x3CE, func(_ uint16, val uint32) { v.gcIndex = uint8(val) })
	iod.RegisterRead(0x3CF, 0x3CF, func(uint16) uint32 {
		v.mu.Lock()
		defer v.mu.Unlock()
		return uint32(v.gcRegs[v.gcIndex])
	})
	iod.RegisterWrite(0x3CF, 0x3CF, func(_ uint16, val uint32) {
		v.mu.Lock()
		v.gcRegs[v.gcIndex] = uint8(val)
		v.mu.Unlock()
	})

	// Attribute controller — single port (0x3C0) with internal flip-flop.
	iod.RegisterRead(0x3C0, 0x3C0, func(uint16) uint32 { return uint32(v.acIndex) })
	iod.RegisterRead(0x3C1, 0x3C1, func(uint16) uint32 {
		v.mu.Lock()
		defer v.mu.Unlock()
		return uint32(v.acRegs[v.acIndex])
	})
	iod.RegisterWrite(0x3C0, 0x3C0, func(_ uint16, val uint32) {
		v.mu.Lock()
		if !v.acFlipFlop {
			v.acIndex = uint8(val) & 0x1F
		} else {
			v.acRegs[v.acIndex] = uint8(val)
		}
		v.acFlipFlop = !v.acFlipFlop
		v.mu.Unlock()
	})

	// Misc output: 0x3C2 (write) / 0x3CC (read).
	iod.RegisterWrite(0x3C2, 0x3C2, func(_ uint16, val uint32) { v.miscOut = uint8(val) })
	iod.RegisterRead(0x3CC, 0x3CC, func(uint16) uint32 { return uint32(v.miscOut) })

	// Input status 1 (0x3DA). Reading resets the AC flip-flop AND
	// returns vertical-retrace / display-enable bits. We toggle bit 0
	// (display enable) so polling-for-retrace loops don't spin
	// forever.
	iod.RegisterRead(0x3DA, 0x3DA, func(uint16) uint32 {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.acFlipFlop = false
		v.inputStatus1 ^= 0x09 // toggle bits 0 (DE) and 3 (VRetrace)
		return uint32(v.inputStatus1)
	})

	// VGA at port 0x3CA / 0x3DA is also aliased at 0x3BA in
	// monochrome-mode. Some BIOS code reads 0x3BA. Mirror as a stub.
	iod.RegisterRead(0x3BA, 0x3BA, func(uint16) uint32 { return 0 })

	// 0x3C3 (subsystem enable): controls VGA enable bit. Read-as-1.
	iod.RegisterRead(0x3C3, 0x3C3, func(uint16) uint32 { return 1 })
	iod.RegisterWrite(0x3C3, 0x3C3, func(uint16, uint32) {})

	// 0x3C6-0x3C9 (DAC palette). We accept writes and read-as-zero;
	// rendering uses fixed ANSI colours regardless.
	iod.RegisterRead(0x3C6, 0x3C9, func(uint16) uint32 { return 0 })
	iod.RegisterWrite(0x3C6, 0x3C9, func(uint16, uint32) {})

	return nil
}

// readMem services a load from the framebuffer aperture.
func (v *VGA) readMem(offset uint32, sizeLog2 int) uint32 {
	v.mu.Lock()
	defer v.mu.Unlock()
	if offset >= VGAApertureSize {
		return 0xFFFFFFFF
	}
	size := 1 << sizeLog2
	var out uint32
	for i := 0; i < size; i++ {
		if int(offset)+i < len(v.aperture) {
			out |= uint32(v.aperture[int(offset)+i]) << (i * 8)
		}
	}
	return out
}

// writeMem services a store to the framebuffer aperture. When
// charSink is set and the write lands inside the 0xB8000 text-mode
// region, every printable byte at an even offset (the "character"
// half of each char/attribute cell) is mirrored. CR (0x0D) and LF
// (0x0A) are passed through verbatim; other control bytes are
// rendered as `.` so binary writes don't corrupt the terminal.
func (v *VGA) writeMem(offset uint32, val uint32, sizeLog2 int) {
	v.mu.Lock()
	if offset >= VGAApertureSize {
		v.mu.Unlock()
		return
	}
	size := 1 << sizeLog2
	for i := 0; i < size; i++ {
		if int(offset)+i < len(v.aperture) {
			v.aperture[int(offset)+i] = uint8(val >> (i * 8))
		}
	}
	sink := v.charSink
	v.mu.Unlock()
	if sink == nil {
		return
	}
	// Capture characters dropped into the text-mode window
	// (0xB8000..0xBFFA0). Even offsets are the char; odd offsets the
	// attribute. We log every printable char (and CR/LF) per write.
	end := int(offset) + size
	for i := int(offset); i < end; i++ {
		if i < int(vgaTextOffset) || i >= int(vgaTextOffset)+vgaTextCols*vgaTextRows*2 {
			continue
		}
		if (i-int(vgaTextOffset))&1 != 0 {
			continue // attribute byte
		}
		b := v.aperture[i]
		switch {
		case b == '\r' || b == '\n':
			_, _ = sink.Write([]byte{b})
		case b >= 0x20 && b < 0x7F:
			_, _ = sink.Write([]byte{b})
		}
	}
}

// vgaColorMap converts VGA's 16-color palette to its ANSI equivalent.
// The two systems disagree on the colour order: VGA has blue at index 1
// where ANSI has red; cyan vs yellow, etc.
var vgaColorMap = [16]uint8{
	0,  // black
	4,  // VGA blue → ANSI 4
	2,  // VGA green → ANSI 2
	6,  // VGA cyan → ANSI 6
	1,  // VGA red → ANSI 1
	5,  // VGA magenta → ANSI 5
	3,  // VGA brown → ANSI 3 (yellow, dark)
	7,  // VGA light gray → ANSI 7 (white, dark)
	8,  // VGA dark gray → bright black
	12, // VGA light blue → bright blue
	10, // VGA light green → bright green
	14, // VGA light cyan → bright cyan
	9,  // VGA light red → bright red
	13, // VGA light magenta → bright magenta
	11, // VGA yellow → bright yellow
	15, // VGA white → bright white
}

func vgaFgAnsi(c uint8) string {
	a := vgaColorMap[c&0x0F]
	if a < 8 {
		return fmt.Sprintf("\x1b[%dm", 30+int(a))
	}
	return fmt.Sprintf("\x1b[%dm", 90+int(a-8))
}

func vgaBgAnsi(c uint8) string {
	a := vgaColorMap[c&0x07] // VGA background uses 3 bits (bit 7 is blink)
	return fmt.Sprintf("\x1b[%dm", 40+int(a))
}

// Render writes the framebuffer to w as a sequence of ANSI escape codes
// and printable characters representing the 80×25 text-mode display.
// Safe to call concurrently with port writes; takes the lock internally.
func (v *VGA) Render(w io.Writer) {
	v.mu.Lock()
	snap := make([]byte, vgaTextCols*vgaTextRows*2)
	copy(snap, v.aperture[vgaTextOffset:vgaTextOffset+len(snap)])
	v.mu.Unlock()

	var b strings.Builder
	// Move cursor to home; don't clear (so successive renders overwrite
	// in-place, giving a flicker-free view).
	b.WriteString("\x1b[H")
	var lastFg, lastBg uint8 = 0xFF, 0xFF // force initial colour write
	for row := 0; row < vgaTextRows; row++ {
		for col := 0; col < vgaTextCols; col++ {
			off := (row*vgaTextCols + col) * 2
			ch := snap[off]
			attr := snap[off+1]
			fg := attr & 0x0F
			bg := (attr >> 4) & 0x07
			if fg != lastFg {
				b.WriteString(vgaFgAnsi(fg))
				lastFg = fg
			}
			if bg != lastBg {
				b.WriteString(vgaBgAnsi(bg))
				lastBg = bg
			}
			if ch < 0x20 || ch >= 0x7F {
				b.WriteByte(' ')
			} else {
				b.WriteByte(ch)
			}
		}
		b.WriteString("\x1b[0m\n")
		lastFg, lastBg = 0xFF, 0xFF
	}
	_, _ = w.Write([]byte(b.String()))
}

// Snapshot returns a copy of the 80×25 text-mode buffer (4000 bytes:
// char/attribute pairs in row-major order). For tests and diagnostics.
func (v *VGA) Snapshot() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]byte, vgaTextCols*vgaTextRows*2)
	copy(out, v.aperture[vgaTextOffset:vgaTextOffset+len(out)])
	return out
}

// CursorPos returns the (col, row) the kernel last set via the CRTC
// cursor-position registers (0x0E / 0x0F).
func (v *VGA) CursorPos() (col, row int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	pos := int(v.crtcRegs[0x0E])<<8 | int(v.crtcRegs[0x0F])
	return pos % vgaTextCols, pos / vgaTextCols
}
