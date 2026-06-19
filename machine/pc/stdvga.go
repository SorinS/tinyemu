package pc

import (
	"os"

	"github.com/sorins/tinyemu-go/mem"
)

// stdVGAEnabled gates the QEMU "std-VGA" graphics device (Bochs VBE).
// Default off; set TINYEMU_STDVGA=1 to expose it. Unlike ramfb (a fw_cfg
// paravirt framebuffer), this is a generic PCI VGA card driven through the
// Bochs VBE dispi interface — the de-facto "linear framebuffer" device
// that VESA, Linux bochs-drm/vesafb, and UEFI's QemuVideoDxe all speak,
// with no firmware-specific channel. temu is headless, so the framebuffer
// is write-only (pixels held in RAM); this provides the device + GOP,
// not a picture.
var stdVGAEnabled = os.Getenv("TINYEMU_STDVGA") == "1"

const (
	stdVGAVendor   = 0x1234 // QEMU "QEMU Standard VGA"
	stdVGADevice   = 0x1111
	stdVGAFBSize   = 16 * 1024 * 1024 // BAR0: 16 MiB linear framebuffer
	stdVGAMMIOSize = 0x1000           // BAR2: MMIO register window
	// Default BAR bases. Firmware reassigns these during PCI enumeration;
	// the framebuffer sits at the start of the 32-bit PCI hole, which our
	// memory model keeps clear of RAM (see pciHoleStart).
	stdVGAFBBase   = 0xC0000000
	stdVGAMMIOBase = 0xC1000000

	// Bochs VBE "dispi" register indices.
	vbeDispiIndexID         = 0x0
	vbeDispiIndexXRes       = 0x1
	vbeDispiIndexYRes       = 0x2
	vbeDispiIndexBPP        = 0x3
	vbeDispiIndexEnable     = 0x4
	vbeDispiIndexBank       = 0x5
	vbeDispiIndexVirtWidth  = 0x6
	vbeDispiIndexVirtHeight = 0x7
	vbeDispiIndexXOffset    = 0x8
	vbeDispiIndexYOffset    = 0x9
	vbeDispiIndexVideoMem   = 0xa
	vbeDispiID5             = 0xB0C5 // ID the guest reads to detect the iface

	vbeDispiIOIndex = 0x1CE // legacy dispi index port
	vbeDispiIOData  = 0x1CF // legacy dispi data port

	// QemuVideoDxe accesses the dispi registers as 16-bit MMIO at this
	// offset within the BAR2 window (QEMU stdvga layout).
	stdVGAMMIODispi = 0x500
)

// StdVGA is a QEMU std-VGA (Bochs VBE) device: a PCI VGA function plus the
// dispi register block (reachable via the legacy 0x1CE/0x1CF I/O ports and
// via MMIO in BAR2) plus a linear framebuffer in BAR0.
type StdVGA struct {
	index uint16 // currently-selected dispi register (via 0x1CE)

	xres, yres, bpp, enable, bank     uint16
	virtWidth, virtHeight, xoff, yoff uint16

	fb   *mem.PhysMemoryRange // BAR0 framebuffer RAM
	mmio *mem.PhysMemoryRange // BAR2 MMIO window
	pci  *PCIDevice
}

func NewStdVGA() *StdVGA { return &StdVGA{} }

func (s *StdVGA) dispiRead(reg uint16) uint16 {
	switch reg {
	case vbeDispiIndexID:
		return vbeDispiID5
	case vbeDispiIndexXRes:
		return s.xres
	case vbeDispiIndexYRes:
		return s.yres
	case vbeDispiIndexBPP:
		return s.bpp
	case vbeDispiIndexEnable:
		return s.enable
	case vbeDispiIndexBank:
		return s.bank
	case vbeDispiIndexVirtWidth:
		return s.virtWidth
	case vbeDispiIndexVirtHeight:
		return s.virtHeight
	case vbeDispiIndexXOffset:
		return s.xoff
	case vbeDispiIndexYOffset:
		return s.yoff
	case vbeDispiIndexVideoMem:
		return uint16(stdVGAFBSize >> 16) // framebuffer size in 64 KiB units
	}
	return 0
}

func (s *StdVGA) dispiWrite(reg, val uint16) {
	switch reg {
	case vbeDispiIndexXRes:
		s.xres = val
	case vbeDispiIndexYRes:
		s.yres = val
	case vbeDispiIndexBPP:
		s.bpp = val
	case vbeDispiIndexEnable:
		s.enable = val
	case vbeDispiIndexBank:
		s.bank = val
	case vbeDispiIndexVirtWidth:
		s.virtWidth = val
	case vbeDispiIndexVirtHeight:
		s.virtHeight = val
	case vbeDispiIndexXOffset:
		s.xoff = val
	case vbeDispiIndexYOffset:
		s.yoff = val
	}
}

// mmioRead/mmioWrite service BAR2. The dispi block lives at offset 0x500,
// one 16-bit register per index (offset = 0x500 + reg*2).
func (s *StdVGA) mmioRead(_ any, offset uint32, _ int) uint32 {
	if offset >= stdVGAMMIODispi && offset < stdVGAMMIODispi+0x20 {
		return uint32(s.dispiRead(uint16((offset - stdVGAMMIODispi) / 2)))
	}
	return 0
}

func (s *StdVGA) mmioWrite(_ any, offset uint32, val uint32, _ int) {
	if offset >= stdVGAMMIODispi && offset < stdVGAMMIODispi+0x20 {
		s.dispiWrite(uint16((offset-stdVGAMMIODispi)/2), uint16(val))
	}
}

// Register wires the framebuffer + MMIO ranges into guest memory, adds the
// PCI VGA function at (dev, fn), and registers the legacy dispi I/O ports.
func (s *StdVGA) Register(io *IOPortDispatcher, mm *mem.PhysMemoryMap, bus *PCIBus, dev, fn uint8) error {
	fb, err := mm.RegisterRAM(stdVGAFBBase, stdVGAFBSize, 0)
	if err != nil {
		return err
	}
	s.fb = fb

	mmio, err := mm.RegisterDevice(stdVGAMMIOBase, stdVGAMMIOSize, nil, s.mmioRead, s.mmioWrite,
		mem.DevIOSize8|mem.DevIOSize16|mem.DevIOSize32)
	if err != nil {
		return err
	}
	s.mmio = mmio

	p := NewPCIDevice("QEMU Standard VGA", stdVGAVendor, stdVGADevice, 0x030000, 0x00)
	p.SetMemBAR(0, stdVGAFBBase, stdVGAFBSize, true) // prefetchable framebuffer
	p.SetMemBAR(2, stdVGAMMIOBase, stdVGAMMIOSize, false)
	// Follow BAR reassignment: relocate the backing ranges by updating
	// their base address (GetRange matches on the live Addr; the ranges
	// slice is preallocated so these pointers stay valid).
	p.SetBARChangeHandler(func(idx int, newBase uint32, isIO bool) {
		switch idx {
		case 0:
			s.fb.Addr = uint64(newBase)
		case 2:
			s.mmio.Addr = uint64(newBase)
		}
	})
	s.pci = p
	bus.AddDevice(dev, fn, p)

	// Legacy Bochs VBE dispi I/O ports (16-bit index/data).
	io.RegisterWrite16(vbeDispiIOIndex, vbeDispiIOIndex, func(_ uint16, v uint32) { s.index = uint16(v) })
	io.RegisterRead16(vbeDispiIOIndex, vbeDispiIOIndex, func(uint16) uint32 { return uint32(s.index) })
	io.RegisterWrite16(vbeDispiIOData, vbeDispiIOData, func(_ uint16, v uint32) { s.dispiWrite(s.index, uint16(v)) })
	io.RegisterRead16(vbeDispiIOData, vbeDispiIOData, func(uint16) uint32 { return uint32(s.dispiRead(s.index)) })
	return nil
}
