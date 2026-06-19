package devices

import "github.com/sorins/tinyemu-go/mem"

// GICv2 is a minimal ARM Generic Interrupt Controller (v2) — a distributor
// (GICD) plus a single-CPU CPU interface (GICC), enough for a Linux guest on the
// virt board. Interrupt IDs: 0–15 SGIs (software), 16–31 PPIs (per-CPU; the
// generic timer lives here), 32+ SPIs (shared: UART, virtio).
//
// Sources drive interrupts level-style: a device raises/lowers its line via the
// IRQSignal returned by CreateIRQ; the board samples the timer PPIs each tick.
// SGIs are edge-latched via GICD_SGIR. On each state change the controller
// recomputes the highest-priority deliverable interrupt and drives the CPU IRQ
// line; the CPU acknowledges by reading GICC_IAR and finishes with GICC_EOIR.
const (
	gicNumIRQ   = 256 // SGIs+PPIs (0..31) + up to 224 SPIs
	gicSpurious = 1023

	// GICD register offsets.
	gicdCTLR       = 0x000
	gicdTYPER      = 0x004
	gicdIIDR       = 0x008
	gicdIGROUPR    = 0x080 // ..0x0FC
	gicdISENABLER  = 0x100 // ..0x17C
	gicdICENABLER  = 0x180 // ..0x1FC
	gicdISPENDR    = 0x200
	gicdICPENDR    = 0x280
	gicdISACTIVER  = 0x300
	gicdICACTIVER  = 0x380
	gicdIPRIORITYR = 0x400 // ..0x7FC, byte each
	gicdITARGETSR  = 0x800 // ..0xBFC, byte each
	gicdICFGR      = 0xC00 // ..0xCFC
	gicdSGIR       = 0xF00

	// GICC register offsets.
	giccCTLR  = 0x000
	giccPMR   = 0x004
	giccBPR   = 0x008
	giccIAR   = 0x00C
	giccEOIR  = 0x010
	giccRPR   = 0x014
	giccHPPIR = 0x018
	giccIIDR  = 0x0FC
)

// GICv2 holds the controller state.
type GICv2 struct {
	setCPUIRQ func(level int) // drives the CPU's external IRQ line

	ctlrD uint32 // GICD_CTLR enable
	ctlrC uint32 // GICC_CTLR enable
	pmr   uint32 // GICC_PMR priority mask

	enabled   [gicNumIRQ]bool
	line      [gicNumIRQ]bool // raw input level (level-triggered sources)
	pendLatch [gicNumIRQ]bool // edge latch (SGIs / ISPENDR)
	active    [gicNumIRQ]bool
	prio      [gicNumIRQ]uint8 // 0 = highest
}

// NewGICv2 creates a controller that drives the CPU IRQ line through setCPUIRQ.
func NewGICv2(setCPUIRQ func(level int)) *GICv2 {
	return &GICv2{setCPUIRQ: setCPUIRQ, pmr: 0}
}

// Register maps the distributor and CPU interface (virt layout: GICD at distBase,
// GICC at cpuBase), each a 64 KiB window.
func (g *GICv2) Register(memMap *mem.PhysMemoryMap, distBase, cpuBase uint64) error {
	flags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	if _, err := memMap.RegisterDevice(distBase, 0x10000, g, g.readD, g.writeD, flags); err != nil {
		return err
	}
	_, err := memMap.RegisterDevice(cpuBase, 0x10000, g, g.readC, g.writeC, flags)
	return err
}

// CreateIRQ returns an IRQSignal that drives interrupt id's input line.
func (g *GICv2) CreateIRQ(id int) *mem.IRQSignal {
	return mem.NewIRQSignal(func(_ any, irqNum, level int) {
		g.SetLine(irqNum, level != 0)
	}, g, id)
}

// SetLine sets the raw input level for an interrupt id and re-evaluates.
func (g *GICv2) SetLine(id int, level bool) {
	if id < 0 || id >= gicNumIRQ {
		return
	}
	g.line[id] = level
	g.update()
}

// pending reports whether id has a pending condition (level held or edge latch).
func (g *GICv2) pending(id int) bool { return g.line[id] || g.pendLatch[id] }

// best returns the highest-priority deliverable interrupt id (enabled, pending,
// not active, priority passes PMR), or -1 if none.
func (g *GICv2) best() int {
	if g.ctlrD == 0 || g.ctlrC == 0 {
		return -1
	}
	bestID, bestPrio := -1, 256
	for id := 0; id < gicNumIRQ; id++ {
		if !g.enabled[id] || !g.pending(id) || g.active[id] {
			continue
		}
		if uint32(g.prio[id]) >= g.pmr { // priority must be higher (lower value) than PMR
			continue
		}
		if int(g.prio[id]) < bestPrio {
			bestID, bestPrio = id, int(g.prio[id])
		}
	}
	return bestID
}

// update drives the CPU IRQ line from the current deliverable interrupt.
func (g *GICv2) update() {
	if g.setCPUIRQ == nil {
		return
	}
	if g.best() >= 0 {
		g.setCPUIRQ(1)
	} else {
		g.setCPUIRQ(0)
	}
}

// --- distributor ---

func (g *GICv2) readD(_ any, offset uint32, _ int) uint32 {
	switch {
	case offset == gicdCTLR:
		return g.ctlrD
	case offset == gicdTYPER:
		return (gicNumIRQ/32 - 1) // ITLinesNumber; CPUNumber=0 (1 CPU)
	case offset == gicdIIDR:
		return 0x0200143B // ARM GICv2
	case offset >= gicdISENABLER && offset < gicdISENABLER+0x80:
		return g.readBitfield(&g.enabled, (offset-gicdISENABLER)/4)
	case offset >= gicdICENABLER && offset < gicdICENABLER+0x80:
		return g.readBitfield(&g.enabled, (offset-gicdICENABLER)/4)
	case offset >= gicdISPENDR && offset < gicdISPENDR+0x80:
		return g.readPending((offset - gicdISPENDR) / 4)
	case offset >= gicdICPENDR && offset < gicdICPENDR+0x80:
		return g.readPending((offset - gicdICPENDR) / 4)
	case offset >= gicdISACTIVER && offset < gicdISACTIVER+0x80:
		return g.readBitfield(&g.active, (offset-gicdISACTIVER)/4)
	case offset >= gicdICACTIVER && offset < gicdICACTIVER+0x80:
		return g.readBitfield(&g.active, (offset-gicdICACTIVER)/4)
	case offset >= gicdIPRIORITYR && offset < gicdIPRIORITYR+gicNumIRQ:
		return g.readBytes(offset - gicdIPRIORITYR)
	case offset >= gicdITARGETSR && offset < gicdITARGETSR+gicNumIRQ:
		return 0x01010101 // all target CPU0
	}
	return 0
}

func (g *GICv2) writeD(_ any, offset uint32, val uint32, _ int) {
	switch {
	case offset == gicdCTLR:
		g.ctlrD = val & 1
	case offset >= gicdISENABLER && offset < gicdISENABLER+0x80:
		g.writeBits(&g.enabled, (offset-gicdISENABLER)/4, val, true)
	case offset >= gicdICENABLER && offset < gicdICENABLER+0x80:
		g.writeBits(&g.enabled, (offset-gicdICENABLER)/4, val, false)
	case offset >= gicdISPENDR && offset < gicdISPENDR+0x80:
		g.writeBits(&g.pendLatch, (offset-gicdISPENDR)/4, val, true)
	case offset >= gicdICPENDR && offset < gicdICPENDR+0x80:
		g.writeBits(&g.pendLatch, (offset-gicdICPENDR)/4, val, false)
	case offset >= gicdISACTIVER && offset < gicdISACTIVER+0x80:
		g.writeBits(&g.active, (offset-gicdISACTIVER)/4, val, true)
	case offset >= gicdICACTIVER && offset < gicdICACTIVER+0x80:
		g.writeBits(&g.active, (offset-gicdICACTIVER)/4, val, false)
	case offset >= gicdIPRIORITYR && offset < gicdIPRIORITYR+gicNumIRQ:
		g.writeBytes(offset-gicdIPRIORITYR, val)
	case offset == gicdSGIR:
		// Software-generated interrupt: latch the target SGI id (0..15) pending.
		id := int(val & 0xF)
		g.pendLatch[id] = true
	}
	g.update()
}

// --- CPU interface ---

func (g *GICv2) readC(_ any, offset uint32, _ int) uint32 {
	switch offset {
	case giccCTLR:
		return g.ctlrC
	case giccPMR:
		return g.pmr
	case giccIAR:
		return g.acknowledge()
	case giccHPPIR:
		if id := g.best(); id >= 0 {
			return uint32(id)
		}
		return gicSpurious
	case giccIIDR:
		return 0x0002043B
	}
	return 0
}

func (g *GICv2) writeC(_ any, offset uint32, val uint32, _ int) {
	switch offset {
	case giccCTLR:
		g.ctlrC = val & 1
	case giccPMR:
		g.pmr = val & 0xFF
	case giccEOIR:
		g.active[val&0x3FF] = false
	}
	g.update()
}

// acknowledge implements a GICC_IAR read: claim the best interrupt, mark it
// active, clear its edge latch, and return its id (1023 if none).
func (g *GICv2) acknowledge() uint32 {
	id := g.best()
	if id < 0 {
		return gicSpurious
	}
	g.active[id] = true
	g.pendLatch[id] = false // level sources keep g.line; edge latch is consumed
	g.update()
	return uint32(id)
}

// --- bitfield/byte register helpers (32 interrupts per 32-bit word) ---

func (g *GICv2) readBitfield(arr *[gicNumIRQ]bool, word uint32) uint32 {
	var v uint32
	for i := 0; i < 32; i++ {
		if arr[word*32+uint32(i)] {
			v |= 1 << i
		}
	}
	return v
}

func (g *GICv2) readPending(word uint32) uint32 {
	var v uint32
	for i := 0; i < 32; i++ {
		if g.pending(int(word*32) + i) {
			v |= 1 << i
		}
	}
	return v
}

func (g *GICv2) writeBits(arr *[gicNumIRQ]bool, word, val uint32, set bool) {
	for i := 0; i < 32; i++ {
		if val&(1<<i) != 0 {
			arr[word*32+uint32(i)] = set
		}
	}
}

func (g *GICv2) readBytes(base uint32) uint32 {
	var v uint32
	for i := uint32(0); i < 4; i++ {
		v |= uint32(g.prio[base+i]) << (i * 8)
	}
	return v
}

func (g *GICv2) writeBytes(base, val uint32) {
	for i := uint32(0); i < 4; i++ {
		g.prio[base+i] = uint8(val >> (i * 8))
	}
}
