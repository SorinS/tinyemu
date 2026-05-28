package pc

import (
	"fmt"
	"os"
)

// PCI host bridge — the chip that translates the CPU's accesses to I/O
// ports 0xCF8 / 0xCFC into reads and writes of PCI device configuration
// space. This is the "Type 1" configuration access mechanism used by every
// PC chipset since the original 82430 (~1993). PCIe extended this with
// memory-mapped MMCONFIG, but Type 1 still works and is what Linux probes
// first if no MMCONFIG is reported.
//
// Bus model: we expose exactly bus 0, with 32 device slots × 8 functions
// each. Each function has 256 bytes of configuration space backing.
// Devices register themselves with the host via Bus().AddDevice.
//
// Reference: PCI Local Bus Specification rev 3.0, chapter 6.

// pciDebug controls trace logging of every config-space access. Enable
// with TINYEMU_X86_PCI_DEBUG=1.
var pciDebug = os.Getenv("TINYEMU_X86_PCI_DEBUG") == "1"

// PCIDevice represents a single function on the PCI bus. The 256-byte
// `config` field is the function's configuration space; the standard
// Type 0 header occupies the first 64 bytes (see PCI spec §6.1).
//
// barMasks implements the PCI "BAR sizing" protocol (PCI spec §6.2.5.1).
// A real PCI device hardwires the low bits of each BAR to indicate type
// and size: bit 0 = 0 for memory / 1 for I/O; for an N-byte aligned
// region, the bottom log2(N) bits read as zero. Software detects the
// region size by writing 0xFFFFFFFF to the BAR and inspecting which bits
// stuck. Each entry in `barMasks` is the write-mask for BAR i (i=0..5):
// the bits that the device permits software to change. Bits cleared in
// the mask are forced to zero on write; the type-bit (bit 0 for I/O,
// bit 0 = 0 for memory) is preserved separately via the initial BAR
// value written at construction. SetBARSize wires this up.
type PCIDevice struct {
	name     string
	config   [256]byte
	barMasks [6]uint32 // 0 means BAR is not a real resource (default RW)

	// onWrite is invoked on writes to configuration space. If nil, the
	// host's default rules apply (vendor/device/class are RO, command/
	// BARs/IRQ-line/ROM-base are RW, status and other fields are RO).
	// Devices needing custom behaviour (status clear-on-write bits) install
	// this callback. BAR sizing is handled centrally via barMasks.
	onWrite func(d *PCIDevice, off uint32, val uint32, size int)

	// onBARChange is invoked after a BAR's stored base address actually
	// changes (post-mask, ignoring the all-ones sizing probe). It lets a
	// device relocate its decoded I/O / MMIO region when firmware (e.g.
	// SeaBIOS) reassigns BARs during its own PCI enumeration — Linux
	// reuses the firmware-assigned bases, but SeaBIOS recomputes them
	// from scratch and moved our virtio-blk I/O BAR out from under the
	// statically-registered port handlers. idx is the BAR index (0..5),
	// newBase is the masked base, isIO true for I/O-space BARs.
	onBARChange func(idx int, newBase uint32, isIO bool)
}

// SetBARChangeHandler installs the BAR-relocation callback. See
// onBARChange.
func (d *PCIDevice) SetBARChangeHandler(fn func(idx int, newBase uint32, isIO bool)) {
	d.onBARChange = fn
}

// Name returns the human-readable identifier set at construction time.
func (d *PCIDevice) Name() string { return d.name }

// Config returns a slice over the device's configuration-space backing.
// Intended for tests and diagnostic inspection.
func (d *PCIDevice) Config() []byte { return d.config[:] }

func (d *PCIDevice) setU8(off uint32, v uint8) { d.config[off] = v }
func (d *PCIDevice) setU16(off uint32, v uint16) {
	d.config[off] = uint8(v)
	d.config[off+1] = uint8(v >> 8)
}
func (d *PCIDevice) setU32(off uint32, v uint32) {
	d.config[off] = uint8(v)
	d.config[off+1] = uint8(v >> 8)
	d.config[off+2] = uint8(v >> 16)
	d.config[off+3] = uint8(v >> 24)
}

// NewPCIDevice creates a PCIDevice with the mandatory header fields filled
// in. `classCode` is the 24-bit (ProgIF | Subclass<<8 | Class<<16) value
// the PCI spec uses to describe the device class — e.g. 0x010180 is "IDE
// controller, ProgIF 0x80".
func NewPCIDevice(name string, vendor, device uint16, classCode uint32, headerType uint8) *PCIDevice {
	d := &PCIDevice{name: name}
	d.setU16(0x00, vendor)
	d.setU16(0x02, device)
	d.setU8(0x08, 0x00)                  // revision ID
	d.setU8(0x09, uint8(classCode))      // ProgIF
	d.setU8(0x0A, uint8(classCode>>8))   // Subclass
	d.setU8(0x0B, uint8(classCode>>16))  // Class
	d.setU8(0x0E, headerType)            // header type (bit 7 = multi-function)
	return d
}

// SetIRQLine fills the standard interrupt-line + interrupt-pin fields.
// Pin 0 = no interrupts; 1-4 = INTA-INTD. Linux reads these during the
// PCI probe to know which IRQ this device will fire on.
func (d *PCIDevice) SetIRQLine(line, pin uint8) {
	d.setU8(0x3C, line)
	d.setU8(0x3D, pin)
}

// SetIOBAR configures BAR `idx` (0..5) as an I/O-space region of `size`
// bytes located at `base`. `size` must be a power of two, ≥4. The device
// will respond to PCI BAR-sizing reads with the proper mask so the OS can
// compute the region's length. The base address is preserved across the
// sizing round-trip and on subsequent reads.
func (d *PCIDevice) SetIOBAR(idx int, base uint32, size uint32) {
	if idx < 0 || idx > 5 || size < 4 || size&(size-1) != 0 {
		return
	}
	off := uint32(0x10 + idx*4)
	mask := ^(size - 1) // e.g. size=16 → mask = 0xFFFFFFF0
	d.barMasks[idx] = mask
	d.setU32(off, (base&mask)|0x01) // bit 0 = 1 → I/O space
}

// PCIBus is bus 0 — the only bus we model. Slots are addressed by
// (device 0-31, function 0-7); 256 total possible functions.
type PCIBus struct {
	devices [32][8]*PCIDevice
}

// AddDevice places `d` at the given device/function slot. If a device is
// already there it is replaced.
func (b *PCIBus) AddDevice(dev, fn uint8, d *PCIDevice) {
	if dev > 31 || fn > 7 {
		return
	}
	b.devices[dev][fn] = d
}

// PCIHost is the host bridge — owns the bus and the 0xCF8 latched
// configuration address.
type PCIHost struct {
	bus     *PCIBus
	address uint32 // last value written to 0xCF8
}

// NewPCIHost creates a host bridge with an empty bus 0.
func NewPCIHost() *PCIHost {
	return &PCIHost{bus: &PCIBus{}}
}

// Bus returns the PCIBus so devices can be added.
func (h *PCIHost) Bus() *PCIBus { return h.bus }

// Register wires the host bridge into the I/O port dispatcher. After
// this call, every `inX 0xCF8` / `outX 0xCF8` and the same for 0xCFC-0xCFF
// goes through this bridge.
//
// The CPU's 32-bit OUT to port P is split into two 16-bit Write16 calls
// (P and P+2). So both 0xCF8/0xCFA AND 0xCFC/0xCFE need width-aware
// handlers that update or read the right slice of the 32-bit state.
func (h *PCIHost) Register(io *IOPortDispatcher) {
	// 0xCF8-0xCFB: the address-latch register, modelled as four
	// independently-accessible bytes of `h.address`.
	io.RegisterRead(0xCF8, 0xCFB, func(port uint16) uint32 {
		shift := uint32(port&3) * 8
		return (h.address >> shift) & 0xFF
	})
	io.RegisterWrite(0xCF8, 0xCFB, func(port uint16, v uint32) {
		shift := uint32(port&3) * 8
		h.address = (h.address &^ (0xFF << shift)) | ((v & 0xFF) << shift)
	})
	io.RegisterRead16(0xCF8, 0xCF8, func(uint16) uint32 {
		return h.address & 0xFFFF
	})
	io.RegisterRead16(0xCFA, 0xCFA, func(uint16) uint32 {
		return (h.address >> 16) & 0xFFFF
	})
	io.RegisterWrite16(0xCF8, 0xCF8, func(_ uint16, v uint32) {
		h.address = (h.address &^ 0x0000FFFF) | (v & 0xFFFF)
	})
	io.RegisterWrite16(0xCFA, 0xCFA, func(_ uint16, v uint32) {
		h.address = (h.address &^ 0xFFFF0000) | ((v & 0xFFFF) << 16)
	})

	// 0xCFC-0xCFF: the data window. 8-bit accesses can hit any of the
	// four bytes; 16-bit accesses are at 0xCFC or 0xCFE; 32-bit access
	// at 0xCFC is the CPU's two-16-bit split.
	io.RegisterRead(0xCFC, 0xCFF, func(port uint16) uint32 {
		return h.readData(port, 1)
	})
	io.RegisterWrite(0xCFC, 0xCFF, func(port uint16, v uint32) {
		h.writeData(port, v, 1)
	})
	io.RegisterRead16(0xCFC, 0xCFC, func(port uint16) uint32 {
		return h.readData(port, 2)
	})
	io.RegisterRead16(0xCFE, 0xCFE, func(port uint16) uint32 {
		return h.readData(port, 2)
	})
	io.RegisterWrite16(0xCFC, 0xCFC, func(port uint16, v uint32) {
		h.writeData(port, v, 2)
	})
	io.RegisterWrite16(0xCFE, 0xCFE, func(port uint16, v uint32) {
		h.writeData(port, v, 2)
	})
}

// lookup decodes the latched 0xCF8 address and returns the target device
// plus the dword-aligned config-space offset, or (nil, 0, false) if no
// device sits at that (bus, dev, function).
func (h *PCIHost) lookup() (*PCIDevice, uint32, bool) {
	if h.address&0x80000000 == 0 {
		return nil, 0, false
	}
	bus := uint8(h.address >> 16)
	dev := uint8((h.address >> 11) & 0x1F)
	fn := uint8((h.address >> 8) & 0x07)
	regBase := h.address & 0xFC
	if bus != 0 {
		return nil, 0, false
	}
	d := h.bus.devices[dev][fn]
	if d == nil {
		return nil, 0, false
	}
	return d, regBase, true
}

// readData reads `size` bytes (1, 2, or 4) from the targeted config space.
// Returns 0xFFFFFFFF if no device responds — the standard PCI "no device"
// indication.
func (h *PCIHost) readData(port uint16, size int) uint32 {
	d, regBase, ok := h.lookup()
	if !ok {
		if pciDebug {
			fmt.Fprintf(os.Stderr, "[pci] read addr=%08x port=%04x size=%d -> 0xFFFFFFFF (no dev)\n",
				h.address, port, size)
		}
		return 0xFFFFFFFF
	}
	off := regBase | uint32(port&3)
	if int(off)+size > len(d.config) {
		return 0xFFFFFFFF
	}
	var v uint32
	switch size {
	case 1:
		v = uint32(d.config[off])
	case 2:
		v = uint32(d.config[off]) | uint32(d.config[off+1])<<8
	case 4:
		v = uint32(d.config[off]) |
			uint32(d.config[off+1])<<8 |
			uint32(d.config[off+2])<<16 |
			uint32(d.config[off+3])<<24
	}
	if pciDebug {
		fmt.Fprintf(os.Stderr, "[pci] read  %s+%02x size=%d -> %08x\n", d.name, off, size, v)
	}
	return v
}

// writeData writes `size` bytes to configuration space. The device's
// onWrite callback (if set) decides whether the write is honoured;
// otherwise the host's defaults apply.
func (h *PCIHost) writeData(port uint16, val uint32, size int) {
	d, regBase, ok := h.lookup()
	if !ok {
		return
	}
	off := regBase | uint32(port&3)
	if int(off)+size > len(d.config) {
		return
	}
	if pciDebug {
		fmt.Fprintf(os.Stderr, "[pci] write %s+%02x size=%d <- %08x\n", d.name, off, size, val)
	}
	if d.onWrite != nil {
		d.onWrite(d, off, val, size)
		return
	}
	defaultWrite(d, off, val, size)
}

// defaultWrite applies the standard PCI rules for what's writable when
// the device has not installed a custom onWrite handler.
//
//   - Vendor ID, Device ID, Revision ID, Class Code, Header Type:
//     read-only (writes silently discarded).
//   - Command (0x04) and Cache Line / Latency Timer (0x0C, 0x0D):
//     writable.
//   - Status (0x06): mostly RO; some bits are W1C (write-1-to-clear).
//     We just accept the write — Linux doesn't depend on W1C semantics
//     for our minimal devices.
//   - BARs (0x10..0x27): writable; devices without real resources
//     return 0 on read (default config-space content).
//   - Interrupt Line (0x3C): writable.
//   - Expansion ROM base (0x30..0x33): writable.
func defaultWrite(d *PCIDevice, off uint32, val uint32, size int) {
	switch {
	case off == 0x04 || off == 0x05: // command
		applyWrite(d, off, val, size)
	case off == 0x06 || off == 0x07: // status — accept writes (W1C handled simplistically)
		applyWrite(d, off, val, size)
	case off == 0x0C || off == 0x0D: // cache line / latency timer
		applyWrite(d, off, val, size)
	case off >= 0x10 && off < 0x28: // BAR0..BAR5
		writeBAR(d, off, val, size)
	case off >= 0x30 && off < 0x34: // expansion ROM base
		applyWrite(d, off, val, size)
	case off == 0x3C: // IRQ line
		applyWrite(d, off, val, size)
	}
	// Otherwise: silently discarded.
}

// writeBAR handles writes that fall within the BAR0-BAR5 range. When the
// BAR has been configured via SetIOBAR/SetMMIOBAR, the write-mask forces
// the size-detection bits to zero and preserves the type bit — implementing
// the standard PCI BAR-sizing protocol. Unconfigured BARs are stored
// verbatim (read-write storage).
func writeBAR(d *PCIDevice, off uint32, val uint32, size int) {
	idx := int((off - 0x10) / 4)
	if idx < 0 || idx > 5 {
		applyWrite(d, off, val, size)
		return
	}
	mask := d.barMasks[idx]
	if mask == 0 {
		// No sizing configured — naive RW storage.
		applyWrite(d, off, val, size)
		return
	}
	// Read the current 32-bit BAR, splice the new bytes in, then apply
	// the mask + preserve the type bit (bit 0 for I/O BAR, low 4 bits
	// for memory BAR). We always go through the 32-bit form because
	// the sizing protocol writes 0xFFFFFFFF as a dword.
	barOff := uint32(0x10 + idx*4)
	cur := uint32(d.config[barOff]) |
		uint32(d.config[barOff+1])<<8 |
		uint32(d.config[barOff+2])<<16 |
		uint32(d.config[barOff+3])<<24
	prev := cur            // base before this write, for change detection
	typeBits := cur & 0x1 // bit 0 = I/O indicator (memory leaves it 0)
	switch size {
	case 1:
		shift := (off - barOff) * 8
		cur = (cur &^ (0xFF << shift)) | ((val & 0xFF) << shift)
	case 2:
		shift := (off - barOff) * 8
		cur = (cur &^ (0xFFFF << shift)) | ((val & 0xFFFF) << shift)
	case 4:
		cur = val
	}
	cur = (cur & mask) | typeBits
	d.config[barOff] = uint8(cur)
	d.config[barOff+1] = uint8(cur >> 8)
	d.config[barOff+2] = uint8(cur >> 16)
	d.config[barOff+3] = uint8(cur >> 24)

	// Notify the device if its decoded base actually moved. We skip the
	// all-ones sizing probe (firmware writes 0xFFFFFFFF then reads back
	// the mask): after masking that leaves the high bits set, which for
	// an I/O BAR is an impossible base (I/O space is only 16 bits), so we
	// gate on isIO && base <= 0xFFFF.
	if d.onBARChange != nil {
		isIO := typeBits&0x1 != 0
		newBase := cur & mask
		changed := newBase != (prev & mask)
		validIO := isIO && newBase <= 0xFFFF
		if changed && validIO {
			d.onBARChange(idx, newBase, isIO)
		}
	}
}

func applyWrite(d *PCIDevice, off uint32, val uint32, size int) {
	switch size {
	case 1:
		d.config[off] = uint8(val)
	case 2:
		d.config[off] = uint8(val)
		d.config[off+1] = uint8(val >> 8)
	case 4:
		d.config[off] = uint8(val)
		d.config[off+1] = uint8(val >> 8)
		d.config[off+2] = uint8(val >> 16)
		d.config[off+3] = uint8(val >> 24)
	}
}
