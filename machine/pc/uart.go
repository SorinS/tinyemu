// Package pc implements an x86 PC machine/board emulator.
package pc

import (
	"io"
	"sync"
)

// UART16550 implements a minimal 16550A-compatible serial port (COM1).
// Implements TX-to-writer, RX FIFO with optional IRQ, basic LSR/IIR/IER
// semantics — enough for Linux console=ttyS0 input and output.
type UART16550 struct {
	mu sync.Mutex

	thr uint8 // Transmit Holding Register (write) / RBR (read)
	rbr uint8 // last byte popped from rxFIFO, returned by 0x3F8 read

	ier uint8 // Interrupt Enable Register
	lcr uint8 // Line Control Register
	mcr uint8 // Modem Control Register
	lsr uint8 // Line Status Register
	msr uint8 // Modem Status Register
	scr uint8 // Scratch Register
	dll uint8 // Divisor Latch Low (when DLAB=1)
	dlh uint8 // Divisor Latch High (when DLAB=1)

	rxFIFO []byte
	thrEmpty bool

	pic *PIC8259
	irq uint8
	out io.Writer
}

// LSR bit definitions.
const (
	lsrDR  = 0x01 // Data Ready (RBR has a byte)
	lsrTHRE = 0x20 // Transmitter Holding Register Empty
	lsrTEMT = 0x40 // Transmitter Empty (THR + shift)
)

// IER bit definitions.
const (
	ierRxData = 0x01 // Received Data Available Interrupt enable
	ierTHRE   = 0x02 // Transmitter Holding Register Empty Interrupt enable
)

// NewUART16550 creates a new UART at the given IRQ with output directed to w.
func NewUART16550(pic *PIC8259, irq uint8, w io.Writer) *UART16550 {
	return &UART16550{
		lsr:      lsrTHRE | lsrTEMT, // THR empty + transmitter idle
		msr:      0x30,              // CTS and DSR asserted
		thrEmpty: true,
		pic:      pic,
		irq:      irq,
		out:      w,
	}
}

// SetOutput changes the UART output writer.
func (u *UART16550) SetOutput(w io.Writer) {
	u.mu.Lock()
	u.out = w
	u.mu.Unlock()
}

// Push appends bytes to the receive FIFO and updates LSR/IRQ accordingly.
// Safe to call from any goroutine (e.g. a stdin reader).
func (u *UART16550) Push(b []byte) {
	if len(b) == 0 {
		return
	}
	u.mu.Lock()
	u.rxFIFO = append(u.rxFIFO, b...)
	u.lsr |= lsrDR
	u.updateIRQLocked()
	u.mu.Unlock()
}

// updateIRQLocked asserts or deasserts the UART's IRQ line based on current
// status and the IER mask. Caller must hold u.mu.
func (u *UART16550) updateIRQLocked() {
	if u.pic == nil {
		return
	}
	pending := false
	if u.ier&ierRxData != 0 && len(u.rxFIFO) > 0 {
		pending = true
	}
	if u.ier&ierTHRE != 0 && u.thrEmpty {
		pending = true
	}
	if pending {
		u.pic.RaiseIRQ(u.irq)
	} else {
		u.pic.LowerIRQ(u.irq)
	}
}

// iirLocked computes the IIR value from current state.
func (u *UART16550) iirLocked() uint8 {
	// Priorities: 011 Receiver Line Status, 010 Received Data Available,
	// 001 Transmitter Holding Register Empty, 000 Modem Status.
	// Bit 0 = 0 means "interrupt pending"; 1 means "no interrupt pending".
	// Bits 7:6 = 11 to indicate 16550A FIFO mode.
	const fifoBits = 0xC0
	if u.ier&ierRxData != 0 && len(u.rxFIFO) > 0 {
		return fifoBits | 0x04 // received data available, bit 0 = 0
	}
	if u.ier&ierTHRE != 0 && u.thrEmpty {
		return fifoBits | 0x02
	}
	return fifoBits | 0x01 // no interrupt
}

// popRBRLocked pops one byte from the RX FIFO into u.rbr and updates LSR/IRQ.
func (u *UART16550) popRBRLocked() {
	if len(u.rxFIFO) == 0 {
		return
	}
	u.rbr = u.rxFIFO[0]
	u.rxFIFO = u.rxFIFO[1:]
	if len(u.rxFIFO) == 0 {
		u.lsr &^= lsrDR
	}
	u.updateIRQLocked()
}

// Register registers the UART's I/O ports (COM1: 0x3F8-0x3FF).
func (u *UART16550) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x3F8, 0x3F8, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.dlabLocked() {
			return uint32(u.dll)
		}
		u.popRBRLocked()
		return uint32(u.rbr)
	})
	io.RegisterRead(0x3F9, 0x3F9, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.dlabLocked() {
			return uint32(u.dlh)
		}
		return uint32(u.ier)
	})
	io.RegisterRead(0x3FA, 0x3FA, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		return uint32(u.iirLocked())
	})
	io.RegisterRead(0x3FB, 0x3FB, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		return uint32(u.lcr)
	})
	io.RegisterRead(0x3FC, 0x3FC, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		return uint32(u.mcr)
	})
	io.RegisterRead(0x3FD, 0x3FD, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		return uint32(u.lsr)
	})
	io.RegisterRead(0x3FE, 0x3FE, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		return uint32(u.msr)
	})
	io.RegisterRead(0x3FF, 0x3FF, func(port uint16) uint32 {
		u.mu.Lock()
		defer u.mu.Unlock()
		return uint32(u.scr)
	})

	io.RegisterWrite(0x3F8, 0x3F8, func(port uint16, val uint32) {
		u.mu.Lock()
		if u.dlabLocked() {
			u.dll = uint8(val)
			u.mu.Unlock()
			return
		}
		u.thr = uint8(val)
		out := u.out
		u.thrEmpty = true
		u.lsr |= lsrTHRE | lsrTEMT
		u.updateIRQLocked()
		u.mu.Unlock()
		if out != nil {
			out.Write([]byte{uint8(val)})
		}
	})
	io.RegisterWrite(0x3F9, 0x3F9, func(port uint16, val uint32) {
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.dlabLocked() {
			u.dlh = uint8(val)
			return
		}
		u.ier = uint8(val)
		u.updateIRQLocked()
	})
	io.RegisterWrite(0x3FA, 0x3FA, func(port uint16, val uint32) {
		// FCR (FIFO Control). We don't model the FIFO, but clearing bits
		// shouldn't break anything.
	})
	io.RegisterWrite(0x3FB, 0x3FB, func(port uint16, val uint32) {
		u.mu.Lock()
		u.lcr = uint8(val)
		u.mu.Unlock()
	})
	io.RegisterWrite(0x3FC, 0x3FC, func(port uint16, val uint32) {
		u.mu.Lock()
		u.mcr = uint8(val)
		u.mu.Unlock()
	})
	io.RegisterWrite(0x3FF, 0x3FF, func(port uint16, val uint32) {
		u.mu.Lock()
		u.scr = uint8(val)
		u.mu.Unlock()
	})
}

func (u *UART16550) dlab() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.dlabLocked()
}

func (u *UART16550) dlabLocked() bool {
	return u.lcr&0x80 != 0
}
