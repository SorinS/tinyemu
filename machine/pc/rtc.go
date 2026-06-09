package pc

import (
	"fmt"
	"os"
	"time"
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

	// Split total RAM at the 32-bit PCI hole exactly as the fw_cfg E820
	// map does (see ramSplit), so the below-4 GiB fields below and the
	// above-4 GiB fields (0x5B-0x5D) describe the same layout firmware
	// reads from etc/e820.
	below4G, above4G := ramSplit(uint64(memSizeKB) * 1024)
	below4GKB := uint32(below4G / 1024)

	// Extended memory (1..64 MiB) in KB.
	if below4GKB > 1024 {
		extKB := below4GKB - 1024
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
	if below4GKB > 16*1024 {
		ext64K := (below4GKB - 16*1024) / 64
		if ext64K > 0xFFFF {
			ext64K = 0xFFFF
		}
		c.ram[0x34] = uint8(ext64K & 0xFF)
		c.ram[0x35] = uint8(ext64K >> 8)
	}

	// Extended memory above 4 GiB, in 64-KiB units, low/mid/high. SeaBIOS
	// and OVMF read these to size the RAM relocated past the PCI hole;
	// leaving them zero (the old behaviour) under-reports total RAM for
	// any >3 GiB config. Zero for the emulator's common small configs,
	// where everything fits below the hole.
	if above4G > 0 {
		above64K := above4G / (64 * 1024)
		c.ram[0x5B] = uint8(above64K & 0xFF)
		c.ram[0x5C] = uint8((above64K >> 8) & 0xFF)
		c.ram[0x5D] = uint8((above64K >> 16) & 0xFF)
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

// readData returns the value of the currently-indexed CMOS register. The
// time-of-day registers report the host's current UTC time in BCD (Status
// B advertises 24-hour BCD), so guests see a real wall-clock — UEFI
// variable timestamps, TLS/cert validation, and `date`/`uptime` all need a
// sane year, not the old fixed 2000-01-01. The non-clock registers (memory
// sizing, equipment, etc.) come from the ram backing via the default case.
func (c *CMOSRTC) readData() uint8 {
	now := time.Now().UTC()
	switch c.index {
	case 0x00: // Seconds
		return bcd(now.Second())
	case 0x02: // Minutes
		return bcd(now.Minute())
	case 0x04: // Hours (24-hour, per Status B bit 1)
		return bcd(now.Hour())
	case 0x06: // Day of week, CMOS convention Sunday=1
		return bcd(int(now.Weekday()) + 1)
	case 0x07: // Day of month
		return bcd(now.Day())
	case 0x08: // Month
		return bcd(int(now.Month()))
	case 0x09: // Year within century
		return bcd(now.Year() % 100)
	case 0x32: // Century (ACPI FADT century register)
		return bcd(now.Year() / 100)
	case 0x0A: // Status A — UIP clear (we compute a consistent time), rate bits
		return 0x26
	case 0x0B: // Status B — 24-hour (bit 1), BCD (bit 2 clear)
		return 0x02
	case 0x0C: // Status C
		return 0x00
	case 0x0D: // Status D — valid RAM/time (bit 7)
		return 0x80
	default:
		return c.ram[c.index]
	}
}

// bcd encodes a 0..99 value as packed binary-coded decimal.
func bcd(n int) uint8 {
	return uint8((n/10)<<4 | (n % 10))
}

func (c *CMOSRTC) writeData(val uint8) {
	c.ram[c.index] = val
}
