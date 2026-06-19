package arm64

// ARM generic timer (EL1 physical + virtual views), enough for a Linux guest to
// keep time and take its scheduler tick. The system counter is the retired-
// instruction count (c.cycles); a board advances it and drives the timer PPI
// level into the GIC via TimerPending. CNTVOFF is 0, so the virtual and physical
// counters coincide.

// Encoded sysreg fields (bits 19:5, matching the a64*Field style in cpu.go:
// (op0&1)<<19 | op1<<16 | CRn<<12 | CRm<<8 | op2<<5). All are op0=3, op1=3,
// CRn=14.
const (
	a64CNTFRQField   uint32 = 1<<19 | 3<<16 | 14<<12                   // S3_3_C14_C0_0
	a64CNTPCTField   uint32 = 1<<19 | 3<<16 | 14<<12 | 1<<5            // S3_3_C14_C0_1
	a64CNTVCTField   uint32 = 1<<19 | 3<<16 | 14<<12 | 2<<5            // S3_3_C14_C0_2
	a64CNTPTVALField uint32 = 1<<19 | 3<<16 | 14<<12 | 2<<8           // S3_3_C14_C2_0
	a64CNTPCTLField  uint32 = 1<<19 | 3<<16 | 14<<12 | 2<<8 | 1<<5    // S3_3_C14_C2_1
	a64CNTPCVALField uint32 = 1<<19 | 3<<16 | 14<<12 | 2<<8 | 2<<5    // S3_3_C14_C2_2
	a64CNTVTVALField uint32 = 1<<19 | 3<<16 | 14<<12 | 3<<8           // S3_3_C14_C3_0
	a64CNTVCTLField  uint32 = 1<<19 | 3<<16 | 14<<12 | 3<<8 | 1<<5    // S3_3_C14_C3_1
	a64CNTVCVALField uint32 = 1<<19 | 3<<16 | 14<<12 | 3<<8 | 2<<5    // S3_3_C14_C3_2
)

const (
	cntCtlEnable = 1 << 0
	cntCtlIMask  = 1 << 1
	cntCtlIStat  = 1 << 2
)

// Counter returns the current system counter value.
func (c *CPU) Counter() uint64 { return c.cycles }

// SetCNTFRQ sets the timer frequency advertised to the guest (CNTFRQ_EL0).
func (c *CPU) SetCNTFRQ(hz uint64) { c.cntfrq = hz }

// readTimerReg returns a generic-timer register value; ok=false if the field is
// not a timer register.
func (c *CPU) readTimerReg(field uint32) (uint64, bool) {
	switch field {
	case a64CNTFRQField:
		return c.cntfrq, true
	case a64CNTPCTField:
		return c.cycles, true
	case a64CNTVCTField:
		return c.cycles - c.cntvOff, true
	case a64CNTPCTLField:
		return c.timerCtlRead(c.cntpCtl, c.cntpCval, c.cycles), true
	case a64CNTPCVALField:
		return c.cntpCval, true
	case a64CNTPTVALField:
		return uint64(int64(c.cntpCval-c.cycles)) & 0xFFFFFFFF, true
	case a64CNTVCTLField:
		return c.timerCtlRead(c.cntvCtl, c.cntvCval, c.cycles-c.cntvOff), true
	case a64CNTVCVALField:
		return c.cntvCval, true
	case a64CNTVTVALField:
		return uint64(int64(c.cntvCval-(c.cycles-c.cntvOff))) & 0xFFFFFFFF, true
	}
	return 0, false
}

// writeTimerReg writes a generic-timer register; ok=false if not a timer field.
func (c *CPU) writeTimerReg(field uint32, v uint64) bool {
	switch field {
	case a64CNTFRQField:
		c.cntfrq = v
	case a64CNTPCTLField:
		c.cntpCtl = v & (cntCtlEnable | cntCtlIMask)
	case a64CNTPCVALField:
		c.cntpCval = v
	case a64CNTPTVALField:
		c.cntpCval = c.cycles + uint64(int64(int32(uint32(v)))) // TVAL is a signed 32-bit offset
	case a64CNTVCTLField:
		c.cntvCtl = v & (cntCtlEnable | cntCtlIMask)
	case a64CNTVCVALField:
		c.cntvCval = v
	case a64CNTVTVALField:
		c.cntvCval = (c.cycles - c.cntvOff) + uint64(int64(int32(uint32(v))))
	default:
		return false
	}
	return true
}

// timerCtlRead returns the control register with its ISTATUS bit reflecting
// whether the timer has fired (counter >= compare, while enabled).
func (c *CPU) timerCtlRead(ctl, cval, count uint64) uint64 {
	if ctl&cntCtlEnable != 0 && count >= cval {
		ctl |= cntCtlIStat
	}
	return ctl
}

// timerFiring reports whether the given timer (ctl/cval against count) is
// asserting its interrupt: enabled, condition met, and not masked.
func timerFiring(ctl, cval, count uint64) bool {
	return ctl&cntCtlEnable != 0 && ctl&cntCtlIMask == 0 && count >= cval
}

// PhysTimerPending / VirtTimerPending report whether each EL1 timer is currently
// asserting its PPI — the board samples these to drive the GIC timer lines.
func (c *CPU) PhysTimerPending() bool {
	return timerFiring(c.cntpCtl, c.cntpCval, c.cycles)
}

func (c *CPU) VirtTimerPending() bool {
	return timerFiring(c.cntvCtl, c.cntvCval, c.cycles-c.cntvOff)
}

// NextTimerDeadline returns the smallest counter value at which an enabled,
// unmasked timer will fire and is not yet firing, or 0 if none is armed. A board
// uses this to fast-forward the counter during WFI instead of sleeping.
func (c *CPU) NextTimerDeadline() uint64 {
	var next uint64
	consider := func(ctl, cval, count uint64) {
		if ctl&cntCtlEnable == 0 || ctl&cntCtlIMask != 0 || count >= cval {
			return
		}
		// deadline expressed in raw counter units
		deadline := c.cycles + (cval - count)
		if next == 0 || deadline < next {
			next = deadline
		}
	}
	consider(c.cntpCtl, c.cntpCval, c.cycles)
	consider(c.cntvCtl, c.cntvCval, c.cycles-c.cntvOff)
	return next
}

// AdvanceCounter jumps the system counter forward to at least `to` (used by a
// board to skip idle time during WFI). It never moves the counter backwards.
func (c *CPU) AdvanceCounter(to uint64) {
	if to > c.cycles {
		c.cycles = to
	}
}
