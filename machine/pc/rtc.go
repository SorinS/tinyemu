package pc

import (
	"fmt"
	"os"
)

// cmosDebug logs every CMOS register read (index → value) when
// TINYEMU_CMOS_DEBUG=1. Used to see exactly which memory-sizing
// registers (0x34/0x35 below-4GB, 0x5b/0x5c/0x5d above-4GB) firmware
// reads and what it gets back.
var cmosDebug = os.Getenv("TINYEMU_CMOS_DEBUG") == "1"

// CMOSRTC implements a minimal CMOS/RTC device.
type CMOSRTC struct {
	index   uint8
	ram     [128]uint8
	memSize uint32 // Total memory size in KB
}

// NewCMOSRTC creates a new CMOS/RTC device.
//
// The CMOS memory-size fields follow the AT-clone convention every
// PC BIOS (SeaBIOS, AMI, Phoenix, ...) consumes:
//
//	0x15/0x16  base (conventional) memory, low/high byte, in KB.
//	           Always 640 — base memory is the architecturally fixed
//	           0..0x9FFFF region, NOT a function of total RAM. The
//	           previous "memSizeKB >> 10" formula made this number
//	           track total memory and confused SeaBIOS into
//	           reporting RamSize ≈ 2.8 MiB regardless of -m.
//
//	0x17/0x18  extended memory (above 1 MiB) in KB, low/high byte,
//	           capped at 0xFFFF. With more than 64 MiB-1 KiB this
//	           saturates and the high-memory fields below take over.
//
//	0x30/0x31  duplicate of 0x17/0x18 — SeaBIOS reads either.
//
//	0x34/0x35  extended memory above 16 MiB, in 64-KiB units, low/
//	           high. The combination of 0x30/0x31 + 0x34/0x35 lets
//	           the BIOS describe up to (64 MiB - 64 KiB) + (4 GiB -
//	           64 KiB) = a bit under 4 GiB.
//
//	0x5B/0x5C/0x5D
//	           extended memory above 4 GiB, in 64-KiB units, low/
//	           mid/high. Reserved here for future >4 GiB support;
//	           the current emulator caps configured RAM well below
//	           that, so they stay zero.
func NewCMOSRTC(memSizeKB uint32) *CMOSRTC {
	c := &CMOSRTC{
		memSize: memSizeKB,
	}
	c.ram[0x0F] = 0x00 // Shutdown status
	c.ram[0x14] = 0x02 // Equipment list (math coprocessor, 1 floppy)

	// Base memory: always 640 KiB conventional.
	const baseKB uint32 = 640
	c.ram[0x15] = uint8(baseKB & 0xFF)
	c.ram[0x16] = uint8(baseKB >> 8)

	// Extended memory (1..64 MiB) in KB.
	if memSizeKB > 1024 {
		extKB := memSizeKB - 1024
		if extKB > 0xFFFF {
			extKB = 0xFFFF
		}
		c.ram[0x17] = uint8(extKB & 0xFF)
		c.ram[0x18] = uint8(extKB >> 8)
		c.ram[0x30] = c.ram[0x17] // mirror — SeaBIOS reads either
		c.ram[0x31] = c.ram[0x18]
	}

	// Extended memory above 16 MiB, in 64-KiB units. Needed for any
	// RAM size > ~65 MiB (where the 0x17/0x18 KB-units field
	// saturates).
	if memSizeKB > 16*1024 {
		ext64K := (memSizeKB - 16*1024) / 64
		if ext64K > 0xFFFF {
			ext64K = 0xFFFF
		}
		c.ram[0x34] = uint8(ext64K & 0xFF)
		c.ram[0x35] = uint8(ext64K >> 8)
	}
	return c
}

// SetFloppyType records that drive A holds a 1.44 MB floppy. CMOS 0x10's
// high nibble is the drive-A type (4 = 1.44 MB); the equipment byte
// 0x14 bit 0 = "floppy present" and bits 6-7 = (count-1). SeaBIOS reads
// these to decide whether to probe the FDC.
func (c *CMOSRTC) SetFloppyType144() {
	c.ram[0x10] = 0x40                        // drive A = 1.44 MB, drive B = none
	c.ram[0x14] = (c.ram[0x14] & 0x3E) | 0x01 // floppy present, 1 drive
}

// Register registers the CMOS I/O ports.
func (c *CMOSRTC) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x70, 0x70, func(port uint16) uint32 {
		return 0xFF // Index port is write-only
	})
	io.RegisterRead(0x71, 0x71, func(port uint16) uint32 {
		v := c.readData()
		if cmosDebug {
			fmt.Fprintf(os.Stderr, "[cmos] idx=%#02x -> %#02x\n", c.index, v)
		}
		return uint32(v)
	})
	io.RegisterWrite(0x70, 0x70, func(port uint16, val uint32) {
		c.index = uint8(val) & 0x7F
	})
	io.RegisterWrite(0x71, 0x71, func(port uint16, val uint32) {
		c.writeData(uint8(val))
	})
}

func (c *CMOSRTC) readData() uint8 {
	switch c.index {
	case 0x00: // Seconds
		return 0x00
	case 0x02: // Minutes
		return 0x00
	case 0x04: // Hours
		return 0x00
	case 0x06: // Day of week
		return 0x01
	case 0x07: // Day of month
		return 0x01
	case 0x08: // Month
		return 0x01
	case 0x09: // Year
		return 0x00
	case 0x0A: // Status A
		return 0x26
	case 0x0B: // Status B
		return 0x02
	case 0x0C: // Status C
		return 0x00
	case 0x0D: // Status D
		return 0x80
	default:
		return c.ram[c.index]
	}
}

func (c *CMOSRTC) writeData(val uint8) {
	c.ram[c.index] = val
}
