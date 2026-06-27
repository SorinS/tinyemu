package devices

import (
	"fmt"
	"os"

	"github.com/sorins/tinyemu-go/mem"
)

// NS16550 is a minimal 8250/16550-compatible UART, enough to drive a guest's
// polled serial console (e.g. NuttX's u16550 driver on the rv-virt board, which
// otherwise spins in u16550_setup forever waiting for LSR.THRE on an absent
// UART). Byte-wide registers, reg-shift 0 — matching the QEMU "virt" 16550 at
// 0x10000000. TX is always reported ready (THRE|TEMT); RX is pulled from the
// console on demand. Interrupts are not modelled yet (polled operation only).
type NS16550 struct {
	console Console

	ier uint8 // interrupt enable
	lcr uint8 // line control (bit 7 = DLAB)
	mcr uint8 // modem control
	fcr uint8 // FIFO control
	scr uint8 // scratch
	dll uint8 // divisor latch low (DLAB=1)
	dlm uint8 // divisor latch high (DLAB=1)

	rxBuf []byte // bytes pulled from the console, not yet read via RBR
	debug bool
}

// 16550 register offsets (DLAB=0 unless noted), reg-shift 0.
const (
	ns16550RBR = 0 // read  (DLAB=0): receive buffer
	ns16550THR = 0 // write (DLAB=0): transmit holding
	ns16550DLL = 0 // DLAB=1: divisor latch low
	ns16550IER = 1 // DLAB=0: interrupt enable
	ns16550DLM = 1 // DLAB=1: divisor latch high
	ns16550IIR = 2 // read: interrupt identification
	ns16550FCR = 2 // write: FIFO control
	ns16550LCR = 3 // line control
	ns16550MCR = 4 // modem control
	ns16550LSR = 5 // line status (read-only)
	ns16550MSR = 6 // modem status (read-only)
	ns16550SCR = 7 // scratch
)

// LSR bits.
const (
	ns16550LsrDR   = 0x01 // receive data ready
	ns16550LsrTHRE = 0x20 // transmit holding register empty
	ns16550LsrTEMT = 0x40 // transmitter empty
)

// NewNS16550 creates a UART that mirrors console I/O.
func NewNS16550(console Console) *NS16550 {
	return &NS16550{console: console, debug: os.Getenv("TINYEMU_UART_DEBUG") == "1"}
}

// Register maps the UART's byte registers at base (typically 0x10000000).
func (u *NS16550) Register(m *mem.PhysMemoryMap, base uint64) error {
	// NuttX's u16550 driver uses byte accesses (lbu/sb); allow 8/16/32-bit so
	// drivers that widen the access still reach the register callbacks.
	flags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	_, err := m.RegisterDevice(base, 0x1000, u, ns16550Read, ns16550Write, flags)
	return err
}

// rxAvailable pulls any pending console input into rxBuf and reports whether a
// byte is waiting.
func (u *NS16550) rxAvailable() bool {
	if len(u.rxBuf) > 0 {
		return true
	}
	if u.console == nil {
		return false
	}
	var tmp [64]byte
	if n := u.console.ReadData(tmp[:]); n > 0 {
		u.rxBuf = append(u.rxBuf, tmp[:n]...)
	}
	return len(u.rxBuf) > 0
}

func ns16550Read(opaque any, offset uint32, sizeLog2 int) uint32 {
	u := opaque.(*NS16550)
	reg := offset & 7
	dlab := u.lcr&0x80 != 0
	var v uint8
	switch reg {
	case ns16550RBR:
		if dlab {
			v = u.dll
		} else if u.rxAvailable() {
			v = u.rxBuf[0]
			u.rxBuf = u.rxBuf[1:]
		}
	case ns16550IER:
		if dlab {
			v = u.dlm
		} else {
			v = u.ier
		}
	case ns16550IIR:
		v = 0x01 // no interrupt pending
	case ns16550LCR:
		v = u.lcr
	case ns16550MCR:
		v = u.mcr
	case ns16550LSR:
		v = ns16550LsrTHRE | ns16550LsrTEMT // always ready to transmit
		if u.rxAvailable() {
			v |= ns16550LsrDR
		}
	case ns16550MSR:
		v = 0xB0 // DCD|DSR|CTS asserted
	case ns16550SCR:
		v = u.scr
	}
	if u.debug {
		fmt.Fprintf(os.Stderr, "[ns16550] R off=%#x -> %#02x\n", offset, v)
	}
	return uint32(v)
}

func ns16550Write(opaque any, offset uint32, val uint32, sizeLog2 int) {
	u := opaque.(*NS16550)
	reg := offset & 7
	dlab := u.lcr&0x80 != 0
	b := uint8(val)
	if u.debug {
		fmt.Fprintf(os.Stderr, "[ns16550] W off=%#x <- %#02x\n", offset, b)
	}
	switch reg {
	case ns16550THR:
		if dlab {
			u.dll = b
		} else if u.console != nil {
			u.console.WriteData([]byte{b})
		}
	case ns16550IER:
		if dlab {
			u.dlm = b
		} else {
			u.ier = b
		}
	case ns16550FCR:
		u.fcr = b
	case ns16550LCR:
		u.lcr = b
	case ns16550MCR:
		u.mcr = b
	case ns16550SCR:
		u.scr = b
	}
}
