package pc

import (
	"fmt"
	"os"
)

var ioapicDebug = os.Getenv("TINYEMU_IOAPIC_DEBUG") == "1"

// IOAPIC is a minimal I/O APIC (82093AA-compatible). It replaces the read-zero
// stub so that in APIC mode (-apic) device interrupt lines reach the local APIC.
//
// In legacy PIC mode the 8259 drives the LAPIC's ExtINT (virtual wire) and this
// is unused. In APIC mode the kernel masks the 8259 and programs the I/O APIC
// redirection table to route each GSI to a LAPIC vector; without a working I/O
// APIC the kernel's request_irq() on a PCI INTx line ("can't find IRQ for PCI
// INT A") never receives interrupts — e.g. virtio-blk completions are lost and
// the box can't mount root.
//
// The device input lines (GSIs) are observed off the 8259's master inputs (see
// PIC8259.SetObserver), so the same RaiseIRQ/LowerIRQ that drives PIC mode also
// drives the I/O APIC. Delivery is edge-on-assert into the LAPIC IRR, which is
// correct for our devices: each keeps its line asserted until the driver ACKs
// it (lowering the line), and virtio drains its whole used ring in one ISR, so
// a single delivery per assertion never drops a completion.
type IOAPIC struct {
	lapic    *LocalAPIC
	ioregsel uint32
	id       uint32
	redir    [ioapicNumRedir]uint64 // redirection entries (low+high dword)
	line     [ioapicNumRedir]bool   // current input level per GSI
}

const ioapicNumRedir = 24

const ioapicRedirMask = uint64(1) << 16 // redirection-entry mask bit

// NewIOAPIC creates an I/O APIC delivering to lapic. All entries start masked,
// as a real I/O APIC does at reset.
func NewIOAPIC(lapic *LocalAPIC) *IOAPIC {
	io := &IOAPIC{lapic: lapic}
	for i := range io.redir {
		io.redir[i] = ioapicRedirMask
	}
	return io
}

// MMIORead/MMIOWrite implement the IOREGSEL (offset 0x00) / IOWIN (offset 0x10)
// window at 0xFEC00000.
func (io *IOAPIC) MMIORead(_ any, offset uint32, _ int) uint32 {
	switch offset {
	case 0x00:
		return io.ioregsel
	case 0x10:
		return io.readReg(io.ioregsel)
	}
	return 0
}

func (io *IOAPIC) MMIOWrite(_ any, offset uint32, val uint32, _ int) {
	switch offset {
	case 0x00:
		io.ioregsel = val & 0xFF
	case 0x10:
		io.writeReg(io.ioregsel, val)
	}
}

func (io *IOAPIC) readReg(reg uint32) uint32 {
	switch {
	case reg == 0x00: // IOAPICID (bits 24-27)
		return io.id
	case reg == 0x01: // IOAPICVER: version 0x11, max redirection entry in bits 16-23
		return 0x11 | (uint32(ioapicNumRedir-1) << 16)
	case reg == 0x02: // IOAPICARB
		return io.id
	case reg >= 0x10 && reg < 0x10+uint32(ioapicNumRedir)*2:
		idx := (reg - 0x10) / 2
		if (reg-0x10)&1 == 0 {
			return uint32(io.redir[idx])
		}
		return uint32(io.redir[idx] >> 32)
	}
	return 0
}

func (io *IOAPIC) writeReg(reg uint32, val uint32) {
	switch {
	case reg == 0x00:
		io.id = val & 0x0F000000
	case reg >= 0x10 && reg < 0x10+uint32(ioapicNumRedir)*2:
		idx := (reg - 0x10) / 2
		if (reg-0x10)&1 == 0 {
			io.redir[idx] = (io.redir[idx] &^ 0xFFFFFFFF) | uint64(val)
		} else {
			io.redir[idx] = (io.redir[idx] & 0xFFFFFFFF) | (uint64(val) << 32)
		}
		// A redirection entry just (re)programmed while its input is still
		// asserted must deliver — the driver may unmask after the device has
		// already raised the line.
		if io.line[idx] {
			io.deliver(idx)
		}
	}
}

// SetGSI sets the input level for a GSI and delivers on a low→high edge.
func (io *IOAPIC) SetGSI(gsi int, level bool) {
	if gsi < 0 || gsi >= ioapicNumRedir {
		return
	}
	prev := io.line[gsi]
	io.line[gsi] = level
	if level && !prev {
		io.deliver(uint32(gsi))
	}
}

func (io *IOAPIC) deliver(idx uint32) {
	entry := io.redir[idx]
	if entry&ioapicRedirMask != 0 {
		return // masked
	}
	vec := uint8(entry & 0xFF)
	if vec < 16 {
		return // not yet programmed with a real vector
	}
	if ioapicDebug {
		fmt.Fprintf(os.Stderr, "[ioapic] deliver gsi=%d vec=%#x\n", idx, vec)
	}
	io.lapic.requestVector(vec)
	io.lapic.updateINTR()
}
