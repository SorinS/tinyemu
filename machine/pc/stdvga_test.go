package pc

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

func TestStdVGA_DispiAndBARRelocation(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	io := NewIOPortDispatcher()
	bus := &PCIBus{}

	s := NewStdVGA()
	if err := s.Register(io, mm, bus, 2, 0); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// dispi ID via the legacy 0x1CE/0x1CF index/data ports.
	io.Write16(vbeDispiIOIndex, vbeDispiIndexID)
	if got := io.Read16(vbeDispiIOData); got != vbeDispiID5 {
		t.Errorf("dispi ID = %#x, want %#x", got, vbeDispiID5)
	}

	// Set XRES through the I/O ports; read it back.
	io.Write16(vbeDispiIOIndex, vbeDispiIndexXRes)
	io.Write16(vbeDispiIOData, 1024)
	io.Write16(vbeDispiIOIndex, vbeDispiIndexXRes)
	if got := io.Read16(vbeDispiIOData); got != 1024 {
		t.Errorf("XRES via I/O = %d, want 1024", got)
	}

	// Set YRES through the MMIO dispi block (offset 0x500 + reg*2), the
	// path QemuVideoDxe uses; read it back the same way.
	s.mmioWrite(nil, stdVGAMMIODispi+vbeDispiIndexYRes*2, 768, 1)
	if got := s.mmioRead(nil, stdVGAMMIODispi+vbeDispiIndexYRes*2, 1); got != 768 {
		t.Errorf("YRES via MMIO = %d, want 768", got)
	}

	// VIDEO_MEMORY reports the framebuffer size in 64 KiB units.
	if got := s.dispiRead(vbeDispiIndexVideoMem); got != uint16(stdVGAFBSize>>16) {
		t.Errorf("VideoMem = %#x, want %#x", got, stdVGAFBSize>>16)
	}

	// BAR0 reassignment relocates the framebuffer's backing range.
	writeBAR(s.pci, 0x10, 0xE0000000, 4)
	if s.fb.Addr != 0xE0000000 {
		t.Errorf("framebuffer not relocated: Addr=%#x, want 0xE0000000", s.fb.Addr)
	}
	// The all-ones sizing probe must NOT relocate (it would transiently
	// drop a 16 MiB region onto flash/APIC).
	writeBAR(s.pci, 0x10, 0xFFFFFFFF, 4)
	if s.fb.Addr != 0xE0000000 {
		t.Errorf("sizing probe relocated framebuffer to %#x", s.fb.Addr)
	}

	// BAR2 reassignment relocates the MMIO window.
	writeBAR(s.pci, 0x18, 0xE2000000, 4)
	if s.mmio.Addr != 0xE2000000 {
		t.Errorf("MMIO window not relocated: Addr=%#x, want 0xE2000000", s.mmio.Addr)
	}
}
