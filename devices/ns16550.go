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
// 0x10000000. TX is always reported ready (THRE|TEMT); RX bytes are delivered
// via Push. Interrupts are modelled (IER/IIR) and driven onto a PLIC line, so a
// guest's RX/TX interrupts work, not just polled access.
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

	irq      *mem.IRQSignal // PLIC line (QEMU-virt UART0 = IRQ 10)
	irqLevel int            // last level driven onto irq (dedup)
	iir      uint8          // current interrupt identification
	trace    bool           // TINYEMU_UART_TRACE: log every register access (verbose)
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
	return &NS16550{console: console, iir: 0x01,
		debug: os.Getenv("TINYEMU_UART_DEBUG") == "1",
		trace: os.Getenv("TINYEMU_UART_TRACE") == "1"}
}

// SetIRQLine connects the UART to a PLIC interrupt line (QEMU-virt UART0 = 10).
func (u *NS16550) SetIRQLine(irq *mem.IRQSignal) { u.irq = irq }

// updateIRQ recomputes the interrupt-identification register and drives the IRQ
// line. RX-data-available (IER bit 0) takes priority over THR-empty (IER bit 1);
// TX is always "ready" so an enabled THR-empty interrupt stays asserted until
// the guest disables it (standard 16550 behaviour). The line is only re-driven
// on a level change — the PLIC is level-triggered and re-pends on completion.
func (u *NS16550) updateIRQ() {
	cause := uint8(0x01) // no interrupt pending
	switch {
	case u.ier&0x01 != 0 && u.rxAvailable():
		cause = 0x04 // received data available
	case u.ier&0x02 != 0:
		cause = 0x02 // transmitter holding register empty
	}
	u.iir = cause
	level := 0
	if cause != 0x01 {
		level = 1
	}
	if u.irq != nil && level != u.irqLevel {
		u.irqLevel = level
		u.irq.Set(level)
	}
}

// Register maps the UART's byte registers at base (typically 0x10000000).
func (u *NS16550) Register(m *mem.PhysMemoryMap, base uint64) error {
	// NuttX's u16550 driver uses byte accesses (lbu/sb); allow 8/16/32-bit so
	// drivers that widen the access still reach the register callbacks.
	flags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32
	_, err := m.RegisterDevice(base, 0x1000, u, ns16550Read, ns16550Write, flags)
	return err
}

// rxAvailable reports whether received data is waiting (delivered via Push).
func (u *NS16550) rxAvailable() bool {
	return len(u.rxBuf) > 0
}

// Push delivers host input bytes into the UART's receive buffer, mirroring the
// x86 COM1 path (the emulator's stdin reader calls this). Capped so a guest that
// never drains this UART — e.g. one whose console is the virtio device — can't
// grow it without bound.
func (u *NS16550) Push(data []byte) {
	if len(data) == 0 || len(u.rxBuf) > 8192 {
		return
	}
	u.rxBuf = append(u.rxBuf, data...)
	if u.debug {
		fmt.Fprintf(os.Stderr, "[ns16550] RX push %d byte(s): %q\n", len(data), string(data))
	}
	u.updateIRQ()
}

// Poll refreshes the interrupt line. Called from the machine's run loop so a
// pending RX interrupt is (re)asserted while the guest is idle (WFI).
func (u *NS16550) Poll() {
	u.updateIRQ()
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
		v = u.iir
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
	if u.trace {
		fmt.Fprintf(os.Stderr, "[ns16550] R off=%#x -> %#02x\n", offset, v)
	}
	u.updateIRQ() // reading RBR may have drained RX; refresh the line
	return uint32(v)
}

func ns16550Write(opaque any, offset uint32, val uint32, sizeLog2 int) {
	u := opaque.(*NS16550)
	reg := offset & 7
	dlab := u.lcr&0x80 != 0
	b := uint8(val)
	if u.trace {
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
	u.updateIRQ() // IER/THR changes may raise or clear the line
}
