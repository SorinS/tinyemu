package arm64

// Machine-mode driving of the core: the run loop, external interrupt injection
// (from a GIC), WFI power-down, and the cpu.Core interface a board uses.

// DAIF mask bits within c.DAIF (PSTATE.DAIF is c.DAIF<<6: D=bit3,A=bit2,I=bit1,
// F=bit0).
const (
	daifI = 1 << 1 // IRQ mask
	daifF = 1 << 0 // FIQ mask
)

// SetIRQ raises/lowers the external IRQ line (driven by the GIC CPU interface).
// Asserting it also wakes the core from WFI, regardless of the IRQ mask.
func (c *CPU) SetIRQ(level int) {
	c.irqLine = level != 0
	if c.irqLine {
		c.powerDown = false
	}
}

// SetFIQ raises/lowers the external FIQ line.
func (c *CPU) SetFIQ(level int) {
	c.fiqLine = level != 0
	if c.fiqLine {
		c.powerDown = false
	}
}

// deliverInterrupts takes a pending, unmasked IRQ/FIQ as an exception to EL1.
// Returns true if an exception was delivered.
func (c *CPU) deliverInterrupts() bool {
	if c.VBAR == 0 {
		return false
	}
	if c.irqLine && c.DAIF&daifI == 0 {
		c.takeException(excIRQ, 0, 0, c.PC, false)
		return true
	}
	if c.fiqLine && c.DAIF&daifF == 0 {
		c.takeException(excFIQ, 0, 0, c.PC, false)
		return true
	}
	return false
}

// Run executes up to maxCycles instructions, delivering pending interrupts and
// honouring WFI. It returns early (nil) when the core parks in WFI so the board
// can advance the timer / sleep before re-entering.
func (c *CPU) Run(maxCycles int) error {
	for i := 0; i < maxCycles; i++ {
		if c.deliverInterrupts() {
			c.powerDown = false
		}
		if c.powerDown || c.Halted {
			return nil
		}
		if err := c.Step(); err != nil {
			return err
		}
		c.cycles++
	}
	return nil
}

// --- cpu.Core interface ---

// GetCycles returns the retired-instruction count (also the timer counter).
func (c *CPU) GetCycles() uint64 { return c.cycles }

// SetPowerDown parks/unparks the core (WFI).
func (c *CPU) SetPowerDown(b bool) { c.powerDown = b }

// IsPowerDown reports whether the core is parked in WFI.
func (c *CPU) IsPowerDown() bool { return c.powerDown }

// HasPendingInterrupt reports whether an external interrupt line is asserted —
// i.e. whether a WFI should wake. (Masking only gates *taking* the exception.)
func (c *CPU) HasPendingInterrupt() bool { return c.irqLine || c.fiqLine }
