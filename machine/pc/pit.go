package pc

// PIT8254 implements a minimal 8254 Programmable Interval Timer.
type PIT8254 struct {
	channels    [3]pitChannel
	pic         *PIC8259
	cyclesFunc  func() uint64
	lastAdvance uint64
	cycleResid  uint64 // unspent cycles below pitCyclesPerTick
}

type pitChannel struct {
	count       uint16
	reload      uint16
	mode        uint8
	latched     bool
	latchValue  uint16
	accessMode  uint8
	halfByte    bool  // write-side LSB/MSB state
	halfValue   uint8 // pending LSB on writes
	readMSBNext bool  // read-side LSB/MSB state for accessMode 3
}

// NewPIT8254 creates a new PIT.
func NewPIT8254(pic *PIC8259) *PIT8254 {
	return &PIT8254{
		pic: pic,
	}
}

// SetCyclesFunc registers a callback that returns the current CPU cycle count.
// When set, the PIT will advance lazily on every port access so software
// polling the count register sees a smoothly decreasing value.
func (p *PIT8254) SetCyclesFunc(fn func() uint64) {
	p.cyclesFunc = fn
}

// advanceLazy ticks the PIT to catch up to the current CPU cycle count. Called
// before any read or write so the kernel's polling loops see fresh state.
func (p *PIT8254) advanceLazy() {
	if p.cyclesFunc == nil {
		return
	}
	now := p.cyclesFunc()
	delta := now - p.lastAdvance
	if delta == 0 {
		return
	}
	p.lastAdvance = now
	p.tickLocked(delta)
}

// tickByCycles converts a CPU cycle delta to PIT ticks, carrying the remainder
// across calls so frequent small deltas still accumulate.
func (p *PIT8254) tickByCycles(cycleDelta uint64) uint32 {
	total := p.cycleResid + cycleDelta
	steps := total / pitCyclesPerTick
	p.cycleResid = total - steps*pitCyclesPerTick
	return uint32(steps)
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
	p.advanceLazy()
	c := &p.channels[ch]
	switch c.accessMode {
	case 1: // LSB only
		val := c.count
		if c.latched {
			val = c.latchValue
			c.latched = false
		}
		return uint8(val)
	case 2: // MSB only
		val := c.count
		if c.latched {
			val = c.latchValue
			c.latched = false
		}
		return uint8(val >> 8)
	default: // mode 3 (LSB then MSB) and mode 0 fallback
		// Real 8254 behavior: the first read latches the current count
		// internally, so LSB and MSB always come from the same snapshot.
		if !c.readMSBNext {
			if !c.latched {
				c.latchValue = c.count
				c.latched = true
			}
			c.readMSBNext = true
			return uint8(c.latchValue)
		}
		c.readMSBNext = false
		val := c.latchValue
		c.latched = false
		return uint8(val >> 8)
	}
}

func (p *PIT8254) writeChannel(ch uint8, val uint8) {
	p.advanceLazy()
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
	p.advanceLazy()
	ch := (val >> 6) & 3
	if ch == 3 {
		// Read-back command — not supported.
		return
	}
	access := (val >> 4) & 3
	if access == 0 {
		// Counter-latch command: snapshot current count.
		c := &p.channels[ch]
		c.latched = true
		c.latchValue = c.count
		c.readMSBNext = false
		return
	}
	c := &p.channels[ch]
	c.accessMode = access
	c.mode = (val >> 1) & 7
	c.halfByte = false
	c.readMSBNext = false
}

// pitCyclesPerTick is the number of CPU cycles per PIT counter decrement.
// Real PIT runs at 1.193182 MHz; we don't need wall-clock accuracy, only that
// kernel calibration loops terminate in a reasonable wall-clock budget. With a
// value of 5, our emulator's TSC-to-PIT ratio puts cpu_khz around 23 MHz —
// small enough that the kernel's TSC-based delay loops converge quickly.
const pitCyclesPerTick = 5

// Tick advances all PIT channels by the given number of CPU cycles. Channel 0
// drives IRQ0 on rollover; channels 1 and 2 are still ticked so software can
// poll their counts (used by Linux's quick_pit_calibrate).
func (p *PIT8254) Tick(cycleDelta uint64) {
	if p.cyclesFunc != nil {
		// Lazy mode is in use; CheckTimer's call is a hint we can ignore
		// (advanceLazy does the work on every access).
		p.advanceLazy()
		return
	}
	p.tickLocked(cycleDelta)
}

func (p *PIT8254) tickLocked(cycleDelta uint64) {
	steps := p.tickByCycles(cycleDelta)
	if steps == 0 {
		return
	}
	for i := range p.channels {
		c := &p.channels[i]
		if c.reload == 0 && c.count == 0 {
			continue
		}
		count := uint32(c.count)
		if count == 0 {
			if c.reload == 0 {
				continue
			}
			count = uint32(c.reload)
		}
		if steps >= count {
			if c.reload == 0 {
				// One-shot mode: counter halts at zero.
				c.count = 0
				if i == 0 {
					p.pic.RaiseIRQ(0)
				}
				continue
			}
			count = uint32(c.reload) - ((steps - count) % uint32(c.reload))
			c.count = uint16(count)
			if i == 0 {
				p.pic.RaiseIRQ(0)
			}
			continue
		}
		c.count = uint16(count - steps)
	}
}
