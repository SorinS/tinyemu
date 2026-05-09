package mem

// SetIRQFunc is called to set an IRQ level.
// Reference: iomem.h line 133
type SetIRQFunc func(opaque any, irqNum int, level int)

// IRQSignal represents an interrupt request line.
// Reference: iomem.h lines 135-139
type IRQSignal struct {
	setIRQ SetIRQFunc
	opaque any
	irqNum int
}

// NewIRQSignal creates a new IRQ signal.
// Reference: iomem.c lines 259-264 (irq_init)
func NewIRQSignal(setIRQ SetIRQFunc, opaque any, irqNum int) *IRQSignal {
	return &IRQSignal{
		setIRQ: setIRQ,
		opaque: opaque,
		irqNum: irqNum,
	}
}

// Set sets the IRQ level (0 = low/inactive, 1 = high/active).
// Reference: iomem.h:143-146 (set_irq inline)
func (irq *IRQSignal) Set(level int) {
	if irq.setIRQ != nil {
		irq.setIRQ(irq.opaque, irq.irqNum, level)
	}
}

// Raise sets the IRQ to active (level 1).
func (irq *IRQSignal) Raise() {
	irq.Set(1)
}

// Lower sets the IRQ to inactive (level 0).
func (irq *IRQSignal) Lower() {
	irq.Set(0)
}

// IRQNum returns the IRQ number.
func (irq *IRQSignal) IRQNum() int {
	return irq.irqNum
}
