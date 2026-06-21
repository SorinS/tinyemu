package arm64

// AArch64 exception delivery to EL1 (synchronous exceptions: svc/brk and
// MMU aborts). IRQ/FIQ/SError delivery shares takeException via the type
// offset; the IRQ path is wired once an interrupt source (GIC) exists.

// Exception type offsets within a vector group (each group is 0x80 apart).
const (
	excSync   = 0x000
	excIRQ    = 0x080
	excFIQ    = 0x100
	excSError = 0x180
)

// pstate packs the current process state into the SPSR layout: NZCV (31:28),
// DAIF (9:6), and M[3:0] = EL<<2 | SPSel (M[4]=0 for AArch64).
func (c *CPU) pstate() uint64 {
	var v uint64
	if c.N {
		v |= 1 << 31
	}
	if c.Z {
		v |= 1 << 30
	}
	if c.C {
		v |= 1 << 29
	}
	if c.V {
		v |= 1 << 28
	}
	v |= uint64(c.DAIF) << 6
	v |= uint64(c.EL)<<2 | uint64(c.SPSel)
	return v
}

// setPstate restores process state from an SPSR value (used by eret).
func (c *CPU) setPstate(v uint64) {
	c.N = v>>31&1 == 1
	c.Z = v>>30&1 == 1
	c.C = v>>29&1 == 1
	c.V = v>>28&1 == 1
	c.DAIF = uint8(v >> 6 & 0xF)
	c.switchEL(uint8(v>>2&3), uint8(v&1))
}

// activeBank returns the storage for the currently selected stack pointer.
func (c *CPU) activeBank() *uint64 {
	if c.SPSel == 1 && c.EL == 1 {
		return &c.SPEL1
	}
	return &c.SPEL0
}

// switchEL changes the exception level / SP selection, banking the live SP so
// each (EL,SPSel) keeps its own stack pointer.
func (c *CPU) switchEL(newEL, newSPSel uint8) {
	*c.activeBank() = c.SP // park the live SP in the old bank
	c.EL, c.SPSel = newEL, newSPSel
	c.SP = *c.activeBank() // adopt the new bank's SP
}

// takeException delivers a synchronous/asynchronous exception to EL1: it saves
// PSTATE→SPSR_EL1, the return address→ELR_EL1, the syndrome→ESR_EL1, switches to
// EL1h with DAIF masked, and sets PC to VBAR_EL1 + group-base + typeOffset.
// far is written to FAR_EL1 when useFar (the abort cases).
func (c *CPU) takeException(typeOffset uint64, esr, far, elr uint64, useFar bool) {
	fromEL, fromSPSel := c.EL, c.SPSel
	c.SPSR = c.pstate()
	c.ELR = elr
	c.ESR = esr
	if useFar {
		c.FAR = far
	}
	// Vector group base by the source: EL0 → lower EL (AArch64); EL1 with
	// SP_ELx → current EL SPx; EL1 with SP_EL0 → current EL SP0.
	var base uint64
	switch {
	case fromEL == 0:
		base = 0x400
	case fromSPSel == 1:
		base = 0x200
	default:
		base = 0x000
	}
	c.switchEL(1, 1)      // enter EL1h
	c.DAIF = 0xF          // mask D,A,I,F
	c.exclMonitor = false // taking an exception clears the local monitor
	c.PC = c.VBAR + base + typeOffset
	if excDebug && esr>>26 != 0 && excLogCount < 30 {
		excLogCount++
		dbgf("[exc] #%d EC=%#x vec=%#x elr=%#x far=%#x fromEL=%d ec_iss=%#x\n",
			excLogCount, esr>>26, base+typeOffset, elr, far, fromEL, esr&0x1FFFFFF)
	}
	if spDebug {
		dbgf("[sp] enter EL%d/SP%d->EL1h vec=%#x esr=%#x elr=%#x SP=%#x SPEL1=%#x SPEL0=%#x\n",
			fromEL, fromSPSel, base+typeOffset, esr, elr, c.SP, c.SPEL1, c.SPEL0)
	}
}

// eret returns from an exception: restore PSTATE from SPSR_EL1 and jump to
// ELR_EL1.
func (c *CPU) eret() {
	pc := c.ELR
	c.setPstate(c.SPSR)
	c.PC = pc
	if spDebug {
		dbgf("[sp] eret  ->EL%d/SP%d pc=%#x SP=%#x SPEL1=%#x SPEL0=%#x\n",
			c.EL, c.SPSel, c.PC, c.SP, c.SPEL1, c.SPEL0)
	}
}

// --- exception syndrome (ESR_EL1) builders ---

// dfscForAbort maps an MMU abort to the 6-bit Data/Instruction Fault Status Code.
func dfscForAbort(a *abort) uint64 {
	lvl := uint64(a.level)
	switch a.kind {
	case "address-size":
		return 0x00 | lvl
	case "translation":
		return 0x04 | lvl
	case "accessflag":
		return 0x08 | lvl
	case "permission":
		return 0x0C | lvl
	}
	return lvl
}

// esrAbort builds ESR_EL1 for a data (data=true) or instruction abort taken
// from fromEL.
func esrAbort(a *abort, data bool, fromEL uint8) uint64 {
	var ec uint64
	switch {
	case data && fromEL == 1:
		ec = 0x25 // data abort, same EL
	case data:
		ec = 0x24 // data abort, lower EL
	case fromEL == 1:
		ec = 0x21 // instruction abort, same EL
	default:
		ec = 0x20 // instruction abort, lower EL
	}
	iss := dfscForAbort(a)
	if data && a.write {
		iss |= 1 << 6 // WnR: write
	}
	return ec<<26 | iss
}

// esrSVC / esrBRK build ESR_EL1 for an SVC / BRK from AArch64.
func esrSVC(imm uint16) uint64 { return 0x15<<26 | uint64(imm) }
func esrBRK(imm uint16) uint64 { return 0x3C<<26 | uint64(imm) }
