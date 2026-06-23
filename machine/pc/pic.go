package pc

import (
	"fmt"
	"os"

	"github.com/sorins/tinyemu-go/cpu"
)

// PIC8259 implements a single 8259 Programmable Interrupt Controller.
// The standard PC AT has two of these chained: master at 0x20-0x21 with
// IRQs 0-7, slave at 0xA0-0xA1 with IRQs 8-15, slave's INT output wired to
// master's IRQ 2. The cascade is modelled by attaching a slave PIC to a
// master via NewPIC8259Cascaded.
type PIC8259 struct {
	cpu        cpu.X86Core
	basePort   uint16
	icwState   int
	icw1       uint8
	icw2       uint8
	icw3       uint8
	icw4       uint8
	imr        uint8 // Interrupt Mask Register
	irr        uint8 // Interrupt Request Register
	isr        uint8 // In-Service Register
	readISR    bool  // OCW3: read ISR instead of IRR
	raiseCount [8]uint64

	// Cascade. The master holds a pointer to the slave; the slave holds
	// a back-pointer to the master. RaiseIRQ(n) for n>=8 routes to the
	// slave's local IRQ (n-8); when the slave has any unmasked pending
	// IRQ, the master sees IRQ 2 asserted.
	slave  *PIC8259
	master *PIC8259

	// intrFunc, when set, receives the master PIC's INTR line level
	// instead of the CPU directly — used to route the PIC through the
	// local APIC as ExtINT.
	intrFunc func(level int)

	// observer, when set on the master, is notified of every input-line
	// change (with the full 0..15 IRQ number) before the master/slave
	// dispatch — used to feed the same device lines into the I/O APIC so
	// APIC mode delivers them while the 8259 is masked.
	observer func(irq uint8, level bool)
}

// SetINTRFunc redirects the (master) PIC's INTR output. Used by the
// machine in APIC mode to feed the local APIC's ExtINT input.
func (p *PIC8259) SetINTRFunc(fn func(level int)) { p.intrFunc = fn }

// SetObserver registers a callback notified of every input-line change on the
// master PIC (full IRQ number 0..15). Used to mirror device lines into the
// I/O APIC.
func (p *PIC8259) SetObserver(fn func(irq uint8, level bool)) { p.observer = fn }

// NewPIC8259 creates a single (master) PIC. IMR defaults to 0xFF (all
// masked) so devices that fire before the kernel programs them don't
// deliver early IRQs.
func NewPIC8259(cpu cpu.X86Core, basePort uint16) *PIC8259 {
	return &PIC8259{
		cpu:      cpu,
		basePort: basePort,
		imr:      0xFF,
	}
}

// NewPIC8259Cascaded creates a master/slave pair. The master sits at
// `masterBase` (typically 0x20); the slave at `slaveBase` (0xA0). RaiseIRQ
// on the master accepts 0..15: 0-7 are local, 8-15 are routed to the
// slave. Use this in the PC board's setup if you want IDE/RTC/PS2-mouse
// (IRQs 8-15) to work.
func NewPIC8259Cascaded(cpu cpu.X86Core, masterBase, slaveBase uint16) *PIC8259 {
	master := NewPIC8259(cpu, masterBase)
	slave := NewPIC8259(nil, slaveBase) // slave doesn't drive the CPU directly
	master.slave = slave
	slave.master = master
	return master
}

// Register registers the PIC's I/O ports. If this PIC has a slave, the
// slave's ports get registered too.
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
	if p.slave != nil {
		p.slave.Register(io)
	}
}

func (p *PIC8259) writeCommand(val uint8) {
	if val&0x10 != 0 {
		// ICW1
		p.icw1 = val
		p.icwState = 1
		p.imr = 0
		p.isr = 0
		p.irr = 0
		p.updateINTR()
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
		p.updateINTR()
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
		p.updateINTR()
	}
}

// updateINTR asserts or clears the CPU INTR line based on whether any
// unmasked IRQ is pending. Called on master only; slave updates the master
// indirectly through the cascade (IRQ 2).
func (p *PIC8259) updateINTR() {
	if p.cpu == nil {
		// Slave: refresh master's view of the cascade line (IRQ 2).
		if p.master != nil {
			slavePending := p.irr &^ p.imr
			if slavePending != 0 {
				p.master.irr |= 1 << 2
			} else {
				p.master.irr &^= 1 << 2
			}
			p.master.updateINTR()
		}
		return
	}
	pending := p.irr &^ p.imr
	level := 0
	if pending != 0 {
		level = 1
	}
	// In APIC mode the machine redirects the master PIC's INTR line into
	// the local APIC (virtual-wire ExtINT) instead of straight to the CPU.
	if p.intrFunc != nil {
		p.intrFunc(level)
	} else {
		p.cpu.SetINTR(level)
	}
}

// RaiseIRQ raises an IRQ line. Accepts 0-7 for local IRQs and 8-15 for
// slave-side IRQs (master-side only — calling with 8-15 on a slave PIC is
// undefined).
func (p *PIC8259) RaiseIRQ(irq uint8) {
	if p.observer != nil {
		p.observer(irq, true)
	}
	if irq >= 8 {
		if p.slave != nil {
			p.slave.RaiseIRQ(irq - 8)
		}
		return
	}
	p.irr |= 1 << irq
	p.raiseCount[irq]++
	p.updateINTR()
}

// RaiseCount returns the cumulative number of times the given IRQ has been
// raised. Exposed for tests/diagnostics.
func (p *PIC8259) RaiseCount(irq uint8) uint64 {
	if irq < 8 {
		return p.raiseCount[irq]
	}
	return 0
}

// IMR returns the current Interrupt Mask Register. Exposed for tests.
func (p *PIC8259) IMR() uint8 { return p.imr }

// IRR returns the current Interrupt Request Register. Exposed for tests.
func (p *PIC8259) IRR() uint8 { return p.irr }

// ISR returns the In-Service Register. Exposed for tests.
func (p *PIC8259) ISR() uint8 { return p.isr }

// LowerIRQ lowers an IRQ line. Accepts 0-7 for local IRQs and 8-15 for
// slave-side IRQs.
func (p *PIC8259) LowerIRQ(irq uint8) {
	if p.observer != nil {
		p.observer(irq, false)
	}
	if irq >= 8 {
		if p.slave != nil {
			p.slave.LowerIRQ(irq - 8)
		}
		return
	}
	p.irr &^= 1 << irq
	p.updateINTR()
}

// PeekInterrupt returns the vector of the highest priority pending
// interrupt without modifying PIC state. Returns -1 if none is pending.
// If the highest priority is the cascade IRQ (2), the slave's vector is
// returned instead.
func (p *PIC8259) PeekInterrupt() int {
	pending := p.irr &^ p.imr
	if pending == 0 {
		return -1
	}
	irq := uint8(0)
	for irq < 8 && (pending&(1<<irq)) == 0 {
		irq++
	}
	if irq >= 8 {
		return -1
	}
	if irq == 2 && p.slave != nil {
		// Peek through the cascade. The slave's vector takes priority.
		return p.slave.PeekInterrupt()
	}
	return int(p.icw2 + irq)
}

// DeliverInterrupt delivers the highest priority pending interrupt to the
// CPU. If the cascade IRQ is highest, the slave is asked for its vector
// and both ISRs are updated. Returns the interrupt vector, or -1.
func (p *PIC8259) deliverDebug(irq uint8, vec int) {
	if os.Getenv("TINYEMU_X64_PIC") == "1" {
		fmt.Fprintf(os.Stderr, "[pic] deliver IRQ=%d → vec=%d (icw2=%#x)\n", irq, vec, p.icw2)
	}
}

func (p *PIC8259) DeliverInterrupt() int {
	pending := p.irr &^ p.imr
	if pending == 0 {
		return -1
	}
	irq := uint8(0)
	for irq < 8 && (pending&(1<<irq)) == 0 {
		irq++
	}
	if irq >= 8 {
		return -1
	}
	if irq == 2 && p.slave != nil {
		// Cascade: mark master's IRQ2 in-service and dispatch through slave.
		vec := p.slave.deliverLocal()
		if vec < 0 {
			// Race: slave's pending cleared between peek and deliver.
			return -1
		}
		p.isr |= 1 << 2
		p.updateINTR()
		return vec
	}
	p.irr &^= 1 << irq
	p.isr |= 1 << irq
	p.updateINTR()
	vec := int(p.icw2 + irq)
	p.deliverDebug(irq, vec)
	return vec
}

// deliverLocal services a local IRQ (slave-side variant of
// DeliverInterrupt that does not recurse into the cascade).
func (p *PIC8259) deliverLocal() int {
	pending := p.irr &^ p.imr
	if pending == 0 {
		return -1
	}
	irq := uint8(0)
	for irq < 8 && (pending&(1<<irq)) == 0 {
		irq++
	}
	if irq >= 8 {
		return -1
	}
	p.irr &^= 1 << irq
	p.isr |= 1 << irq
	p.updateINTR()
	return int(p.icw2 + irq)
}
