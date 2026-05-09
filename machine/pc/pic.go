package pc

import "github.com/jtolio/tinyemu-go/cpu/x86"

// PIC8259 implements a minimal 8259 Programmable Interrupt Controller.
type PIC8259 struct {
	cpu      *x86.CPU
	basePort uint16
	icwState int
	icw1     uint8
	icw2     uint8
	icw3     uint8
	icw4     uint8
	imr      uint8 // Interrupt Mask Register
	irr      uint8 // Interrupt Request Register
	isr      uint8 // In-Service Register
	readISR  bool  // OCW3: read ISR instead of IRR
}

// NewPIC8259 creates a new PIC.
func NewPIC8259(cpu *x86.CPU, basePort uint16) *PIC8259 {
	return &PIC8259{
		cpu:      cpu,
		basePort: basePort,
	}
}

// Register registers the PIC's I/O ports.
func (p *PIC8259) Register(io *IOPortDispatcher) {
	io.RegisterRead(p.basePort, p.basePort, func(port uint16) uint32 {
		if p.readISR {
			return uint32(p.isr)
		}
		return uint32(p.irr)
	})
	io.RegisterRead(p.basePort+1, p.basePort+1, func(port uint16) uint32 {
		return uint32(p.imr)
	})
	io.RegisterWrite(p.basePort, p.basePort, func(port uint16, val uint32) {
		p.writeCommand(uint8(val))
	})
	io.RegisterWrite(p.basePort+1, p.basePort+1, func(port uint16, val uint32) {
		p.writeData(uint8(val))
	})
}

func (p *PIC8259) writeCommand(val uint8) {
	if val&0x10 != 0 {
		// ICW1
		p.icw1 = val
		p.icwState = 1
		p.imr = 0
		p.isr = 0
		p.irr = 0
		return
	}
	if val&0x08 != 0 {
		// OCW3
		if val&0x02 != 0 {
			p.readISR = val&0x01 != 0
		}
		return
	}
	// OCW2 (EOI)
	if val&0x60 == 0 {
		// Non-specific EOI
		p.isr &^= p.isr & -p.isr // clear lowest set bit
	}
}

func (p *PIC8259) writeData(val uint8) {
	switch p.icwState {
	case 1:
		p.icw2 = val
		if p.icw1&0x02 != 0 {
			p.icwState = 3 // Single mode, no ICW3
		} else {
			p.icwState = 2
		}
	case 2:
		p.icw3 = val
		if p.icw1&0x01 != 0 {
			p.icwState = 3
		} else {
			p.icwState = 0
		}
	case 3:
		p.icw4 = val
		p.icwState = 0
	default:
		// OCW1 - set IMR
		p.imr = val
	}
}

// RaiseIRQ raises an IRQ line.
func (p *PIC8259) RaiseIRQ(irq uint8) {
	if irq < 8 {
		p.irr |= 1 << irq
	}
}

// LowerIRQ lowers an IRQ line.
func (p *PIC8259) LowerIRQ(irq uint8) {
	if irq < 8 {
		p.irr &^= 1 << irq
	}
}

// PeekInterrupt returns the vector of the highest priority pending interrupt
// without modifying PIC state. Returns -1 if no interrupt is pending.
func (p *PIC8259) PeekInterrupt() int {
	pending := p.irr &^ p.imr
	if pending == 0 {
		return -1
	}
	// Find lowest IRQ number (highest priority)
	irq := uint8(0)
	for irq < 8 && (pending&(1<<irq)) == 0 {
		irq++
	}
	if irq >= 8 {
		return -1
	}
	vector := p.icw2 + irq
	return int(vector)
}

// DeliverInterrupt delivers the highest priority pending interrupt to the CPU.
// Returns the interrupt vector, or -1 if no interrupt is pending.
func (p *PIC8259) DeliverInterrupt() int {
	pending := p.irr &^ p.imr
	if pending == 0 {
		return -1
	}
	// Find lowest IRQ number (highest priority)
	irq := uint8(0)
	for irq < 8 && (pending&(1<<irq)) == 0 {
		irq++
	}
	if irq >= 8 {
		return -1
	}
	p.irr &^= 1 << irq
	p.isr |= 1 << irq
	vector := p.icw2 + irq
	return int(vector)
}
