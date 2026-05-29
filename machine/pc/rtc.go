package pc

// CMOSRTC implements a minimal CMOS/RTC device.
type CMOSRTC struct {
	index   uint8
	ram     [128]uint8
	memSize uint32 // Total memory size in KB
}

// NewCMOSRTC creates a new CMOS/RTC device.
func NewCMOSRTC(memSizeKB uint32) *CMOSRTC {
	c := &CMOSRTC{
		memSize: memSizeKB,
	}
	// Initialize standard CMOS values
	c.ram[0x0F] = 0x00 // Shutdown status
	c.ram[0x14] = 0x02 // Equipment list (math coprocessor, 1 floppy)
	// Memory size below 1MB
	c.ram[0x15] = uint8((memSizeKB >> 10) & 0xFF) // Low byte of extended memory in KB / 64
	c.ram[0x16] = uint8((memSizeKB >> 18) & 0xFF) // High byte
	// Memory size above 1MB
	if memSizeKB > 1024 {
		extended := memSizeKB - 1024
		if extended > 0xFFFF {
			extended = 0xFFFF
		}
		c.ram[0x17] = uint8(extended & 0xFF)
		c.ram[0x18] = uint8(extended >> 8)
	}
	// Memory size above 16MB
	if memSizeKB > 16*1024 {
		extended := (memSizeKB - 16*1024) / 64
		if extended > 0xFFFF {
			extended = 0xFFFF
		}
		c.ram[0x30] = uint8(extended & 0xFF)
		c.ram[0x31] = uint8(extended >> 8)
	}
	return c
}

// SetFloppyType records that drive A holds a 1.44 MB floppy. CMOS 0x10's
// high nibble is the drive-A type (4 = 1.44 MB); the equipment byte
// 0x14 bit 0 = "floppy present" and bits 6-7 = (count-1). SeaBIOS reads
// these to decide whether to probe the FDC.
func (c *CMOSRTC) SetFloppyType144() {
	c.ram[0x10] = 0x40           // drive A = 1.44 MB, drive B = none
	c.ram[0x14] = (c.ram[0x14] & 0x3E) | 0x01 // floppy present, 1 drive
}

// Register registers the CMOS I/O ports.
func (c *CMOSRTC) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x70, 0x70, func(port uint16) uint32 {
		return 0xFF // Index port is write-only
	})
	io.RegisterRead(0x71, 0x71, func(port uint16) uint32 {
		return uint32(c.readData())
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
