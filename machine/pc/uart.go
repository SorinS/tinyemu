// Package pc implements an x86 PC machine/board emulator.
package pc

import (
	"io"
)

// UART16550 implements a minimal 16550A-compatible serial port (COM1).
// Only enough functionality is provided for Linux early console output
// (console=ttyS0).  Interrupts and FIFOs are stubbed.
type UART16550 struct {
	thr   uint8 // Transmit Holding Register (write) / RBR (read)
	ier   uint8 // Interrupt Enable Register
	iir   uint8 // Interrupt Identification Register
	lcr   uint8 // Line Control Register
	mcr   uint8 // Modem Control Register
	lsr   uint8 // Line Status Register
	msr   uint8 // Modem Status Register
	scr   uint8 // Scratch Register
	dll   uint8 // Divisor Latch Low (when DLAB=1)
	dlh   uint8 // Divisor Latch High (when DLAB=1)
	pic   *PIC8259
	irq   uint8
	out   io.Writer
}

// NewUART16550 creates a new UART at the given IRQ with output directed to w.
func NewUART16550(pic *PIC8259, irq uint8, w io.Writer) *UART16550 {
	return &UART16550{
		lsr: 0x60, // THR empty, transmitter idle
		msr: 0x30, // CTS and DSR asserted
		pic: pic,
		irq: irq,
		out: w,
	}
}

// SetOutput changes the UART output writer.
func (u *UART16550) SetOutput(w io.Writer) {
	u.out = w
}

// Register registers the UART's I/O ports (COM1: 0x3F8-0x3FF).
func (u *UART16550) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x3F8, 0x3F8, func(port uint16) uint32 {
		if u.dlab() {
			return uint32(u.dll)
		}
		return uint32(u.thr)
	})
	io.RegisterRead(0x3F9, 0x3F9, func(port uint16) uint32 {
		if u.dlab() {
			return uint32(u.dlh)
		}
		return uint32(u.ier)
	})
	io.RegisterRead(0x3FA, 0x3FA, func(port uint16) uint32 {
		return uint32(u.iir)
	})
	io.RegisterRead(0x3FB, 0x3FB, func(port uint16) uint32 {
		return uint32(u.lcr)
	})
	io.RegisterRead(0x3FC, 0x3FC, func(port uint16) uint32 {
		return uint32(u.mcr)
	})
	io.RegisterRead(0x3FD, 0x3FD, func(port uint16) uint32 {
		return uint32(u.lsr)
	})
	io.RegisterRead(0x3FE, 0x3FE, func(port uint16) uint32 {
		return uint32(u.msr)
	})
	io.RegisterRead(0x3FF, 0x3FF, func(port uint16) uint32 {
		return uint32(u.scr)
	})

	io.RegisterWrite(0x3F8, 0x3F8, func(port uint16, val uint32) {
		if u.dlab() {
			u.dll = uint8(val)
			return
		}
		u.thr = uint8(val)
		if u.out != nil {
			u.out.Write([]byte{u.thr})
		}
		u.lsr |= 0x60 // THR empty, transmitter empty
		if u.ier&0x02 != 0 && u.pic != nil {
			u.pic.RaiseIRQ(u.irq)
		}
	})
	io.RegisterWrite(0x3F9, 0x3F9, func(port uint16, val uint32) {
		if u.dlab() {
			u.dlh = uint8(val)
			return
		}
		u.ier = uint8(val)
	})
	io.RegisterWrite(0x3FA, 0x3FA, func(port uint16, val uint32) {
		// FCR - not implemented
	})
	io.RegisterWrite(0x3FB, 0x3FB, func(port uint16, val uint32) {
		u.lcr = uint8(val)
	})
	io.RegisterWrite(0x3FC, 0x3FC, func(port uint16, val uint32) {
		u.mcr = uint8(val)
	})
	io.RegisterWrite(0x3FF, 0x3FF, func(port uint16, val uint32) {
		u.scr = uint8(val)
	})
}

func (u *UART16550) dlab() bool {
	return u.lcr&0x80 != 0
}
