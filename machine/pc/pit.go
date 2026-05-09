package pc

// PIT8254 implements a minimal 8254 Programmable Interval Timer.
type PIT8254 struct {
	channels [3]pitChannel
	pic      *PIC8259
}

type pitChannel struct {
	count      uint16
	reload     uint16
	mode       uint8
	latched    bool
	latchValue uint16
	accessMode uint8
	halfByte   bool
	halfValue  uint8
}

// NewPIT8254 creates a new PIT.
func NewPIT8254(pic *PIC8259) *PIT8254 {
	return &PIT8254{
		pic: pic,
	}
}

// Register registers the PIT's I/O ports.
func (p *PIT8254) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x40, 0x42, func(port uint16) uint32 {
		ch := port - 0x40
		return uint32(p.readChannel(uint8(ch)))
	})
	io.RegisterRead(0x43, 0x43, func(port uint16) uint32 {
		return 0xFF // Control word port is write-only
	})
	io.RegisterWrite(0x40, 0x42, func(port uint16, val uint32) {
		ch := port - 0x40
		p.writeChannel(uint8(ch), uint8(val))
	})
	io.RegisterWrite(0x43, 0x43, func(port uint16, val uint32) {
		p.writeControl(uint8(val))
	})
}

func (p *PIT8254) readChannel(ch uint8) uint8 {
	c := &p.channels[ch]
	if c.latched {
		c.latched = false
		return uint8(c.latchValue)
	}
	return uint8(c.count)
}

func (p *PIT8254) writeChannel(ch uint8, val uint8) {
	c := &p.channels[ch]
	switch c.accessMode {
	case 1: // LSB only
		c.reload = uint16(val)
		c.count = c.reload
	case 2: // MSB only
		c.reload = uint16(val) << 8
		c.count = c.reload
	case 3: // LSB then MSB
		if !c.halfByte {
			c.halfValue = val
			c.halfByte = true
		} else {
			c.reload = uint16(c.halfValue) | (uint16(val) << 8)
			c.count = c.reload
			c.halfByte = false
		}
	}
}

func (p *PIT8254) writeControl(val uint8) {
	ch := (val >> 6) & 3
	if ch == 3 {
		// Read-back command
		return
	}
	c := &p.channels[ch]
	c.accessMode = (val >> 4) & 3
	c.mode = (val >> 1) & 7
	c.halfByte = false
}

// Tick should be called periodically to decrement the counter and generate interrupts.
func (p *PIT8254) Tick() {
	c := &p.channels[0]
	if c.count > 0 {
		c.count--
		if c.count == 0 {
			c.count = c.reload
			if c.reload > 0 {
				p.pic.RaiseIRQ(0)
			}
		}
	}
}
