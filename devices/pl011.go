package devices

import "github.com/sorins/tinyemu-go/mem"

// PL011 is a minimal ARM PrimeCell PL011 UART — enough for a Linux guest's
// amba-pl011 driver (and earlycon) to use it as the serial console. TX is
// written straight to the console; RX is fed from the console input by the
// board's poll loop, with an optional receive interrupt into the GIC.
//
// Register map (offsets from the 4 KiB base at 0x09000000 on the virt board):
//
//	0x000 DR    data (write = TX byte, read = RX byte)
//	0x018 FR    flags (RXFE/TXFF/TXFE/BUSY)
//	0x024 IBRD / 0x028 FBRD / 0x02C LCR_H / 0x030 CR   baud + control (stored)
//	0x038 IMSC  interrupt mask    0x03C RIS  raw status
//	0x040 MIS   masked status     0x044 ICR  interrupt clear
//	0xFE0..0xFFC PeriphID/PCellID identification (PrimeCell probe)
const (
	pl011DR   = 0x000
	pl011FR   = 0x018
	pl011IBRD = 0x024
	pl011FBRD = 0x028
	pl011LCRH = 0x02C
	pl011CR   = 0x030
	pl011IFLS = 0x034
	pl011IMSC = 0x038
	pl011RIS  = 0x03C
	pl011MIS  = 0x040
	pl011ICR  = 0x044

	// FR bits.
	pl011FR_RXFE = 1 << 4 // receive FIFO empty
	pl011FR_TXFF = 1 << 5 // transmit FIFO full (never, here)
	pl011FR_TXFE = 1 << 7 // transmit FIFO empty (always, here)

	// Interrupt bits (RIS/MIS/IMSC/ICR).
	pl011INT_RX = 1 << 4 // receive interrupt
	pl011INT_TX = 1 << 5 // transmit interrupt
)

// PL011 identification registers (standard ARM PrimeCell PL011 values).
var pl011ID = map[uint32]uint32{
	0xFE0: 0x11, 0xFE4: 0x10, 0xFE8: 0x14, 0xFEC: 0x00, // PeriphID0..3
	0xFF0: 0x0D, 0xFF4: 0xF0, 0xFF8: 0x05, 0xFFC: 0xB1, // PCellID0..3
}

// PL011 holds the modelled UART state.
type PL011 struct {
	console Console
	irq     *mem.IRQSignal

	rx   []byte // received bytes awaiting the guest
	imsc uint32 // interrupt mask
	ibrd uint32
	fbrd uint32
	lcrh uint32
	cr   uint32
	ifls uint32
}

// NewPL011 creates a PL011 wired to the given console and (optional) IRQ line.
func NewPL011(console Console, irq *mem.IRQSignal) *PL011 {
	return &PL011{console: console, irq: irq, cr: 0x301} // UARTEN|TXE|RXE
}

// Register maps the UART at base in the physical memory map.
func (u *PL011) Register(memMap *mem.PhysMemoryMap, base uint64) error {
	_, err := memMap.RegisterDevice(base, 0x1000, u, u.read, u.write,
		mem.DevIOSize8|mem.DevIOSize16|mem.DevIOSize32)
	return err
}

// raw returns the raw interrupt status (RIS): RX asserted while bytes are
// buffered; TX is always ready.
func (u *PL011) raw() uint32 {
	var ris uint32 = pl011INT_TX
	if len(u.rx) > 0 {
		ris |= pl011INT_RX
	}
	return ris
}

// updateIRQ drives the IRQ line from the masked interrupt status.
func (u *PL011) updateIRQ() {
	if u.irq == nil {
		return
	}
	if u.raw()&u.imsc != 0 {
		u.irq.Raise()
	} else {
		u.irq.Lower()
	}
}

// PollInput pulls any available host input into the receive buffer and updates
// the interrupt. Called from the board's device poll loop.
func (u *PL011) PollInput() {
	if u.console == nil {
		return
	}
	var buf [64]byte
	if n := u.console.ReadData(buf[:]); n > 0 {
		u.rx = append(u.rx, buf[:n]...)
		u.updateIRQ()
	}
}

func (u *PL011) read(_ any, offset uint32, _ int) uint32 {
	if v, ok := pl011ID[offset]; ok {
		return v
	}
	switch offset {
	case pl011DR:
		var b uint32
		if len(u.rx) > 0 {
			b = uint32(u.rx[0])
			u.rx = u.rx[1:]
			u.updateIRQ()
		}
		return b
	case pl011FR:
		fr := uint32(pl011FR_TXFE) // TX always empty/ready
		if len(u.rx) == 0 {
			fr |= pl011FR_RXFE
		}
		return fr
	case pl011IBRD:
		return u.ibrd
	case pl011FBRD:
		return u.fbrd
	case pl011LCRH:
		return u.lcrh
	case pl011CR:
		return u.cr
	case pl011IFLS:
		return u.ifls
	case pl011IMSC:
		return u.imsc
	case pl011RIS:
		return u.raw()
	case pl011MIS:
		return u.raw() & u.imsc
	}
	return 0
}

func (u *PL011) write(_ any, offset uint32, val uint32, _ int) {
	switch offset {
	case pl011DR:
		if u.console != nil {
			u.console.WriteData([]byte{byte(val)})
		}
	case pl011IBRD:
		u.ibrd = val
	case pl011FBRD:
		u.fbrd = val
	case pl011LCRH:
		u.lcrh = val
	case pl011CR:
		u.cr = val
	case pl011IFLS:
		u.ifls = val
	case pl011IMSC:
		u.imsc = val
		u.updateIRQ()
	case pl011ICR:
		// Clearing acknowledges level interrupts; RX re-asserts while bytes
		// remain buffered, which updateIRQ recomputes.
		u.updateIRQ()
	}
}
