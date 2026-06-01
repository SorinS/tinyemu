package pc

// Minimal 8042 PS/2 controller stub.
//
// We model exactly the slice of the i8042 protocol a polite POST routine
// (BareMetal, plain SeaBIOS keyboard init, any "is there a keyboard?"
// probe) exercises during initialization:
//
//   - port 0x64 read: status register. Bit 0 = output-buffer-full
//     (1 when there's a byte ready at port 0x60). Bit 1 = input-buffer-
//     full (always 0 here — we're instantly ready to accept the next
//     command/data byte). Other bits zero.
//   - port 0x64 write: command. The handful of commands we honour are
//     enumerated below; everything else is a no-op.
//   - port 0x60 read: pops the next byte from the output queue, or
//     0xFF if empty. Clears the OBF bit on a non-empty read.
//   - port 0x60 write: if the previous command armed it (WR_CCB, write
//     to port 1, etc.), the byte is consumed accordingly; otherwise
//     it's discarded.
//
// We intentionally never push key-press scancodes or fire IRQ 1 — there
// is no real keyboard behind us. Guests that enable the keyboard IRQ
// then go off to do other work (the kernel's main loop, an idle HLT,
// etc.) are unblocked because the *init* completes; no key ever
// appears at the OS level but everything else carries on. If a future
// guest actually needs key input we can wire stdin scancodes into
// pushOutputByte and raise IRQ 1, but most boot-to-anything paths
// don't need that.
//
// Reference: Intel 8042 (KBC) datasheet; PS/2 Hardware Interface FAQs
// (https://wiki.osdev.org/%228042%22_PS/2_Controller). Command codes
// follow that table.

const (
	ps2PortData = 0x60 // R/W: keyboard data / command params
	ps2PortCmd  = 0x64 // R: status; W: controller command

	// Status-register bits (port 0x64 read).
	ps2StatusOBF = 1 << 0 // Output buffer full — byte ready at 0x60
	ps2StatusIBF = 1 << 1 // Input  buffer full — controller still busy

	// Commands we honour (port 0x64 write).
	ps2CmdReadCCB      = 0x20 // read controller configuration byte → next port 0x60 read
	ps2CmdWriteCCB     = 0x60 // arm: next port 0x60 write replaces CCB
	ps2CmdDisableAux   = 0xA7 // disable second PS/2 port
	ps2CmdEnableAux    = 0xA8
	ps2CmdTestAux      = 0xA9 // → 0x00 (no error) at next port 0x60 read
	ps2CmdSelfTest     = 0xAA // → 0x55 (passed) at next port 0x60 read
	ps2CmdTestKbd      = 0xAB // → 0x00 (no error) at next port 0x60 read
	ps2CmdDisableKbd   = 0xAD
	ps2CmdEnableKbd    = 0xAE
	ps2CmdReadOutput   = 0xD0 // → output port byte at next port 0x60 read
	ps2CmdWriteOutput  = 0xD1 // arm: next port 0x60 write goes to output port
	ps2CmdWriteToAux   = 0xD4 // arm: next port 0x60 write is sent to mouse
)

// PS2Controller is a non-functional 8042 stub. State is just enough to
// keep the controller-side handshake (probe, self-test, CCB read/write,
// kbd/aux enable/disable) coherent.
type PS2Controller struct {
	// outputQueue holds bytes we plan to return on the next port 0x60
	// read(s). Most commands push exactly one byte (e.g. 0x55 for the
	// self-test response); the queue lets a future enhancement push
	// multi-byte responses without changing callers.
	outputQueue []byte

	// nextDataTarget is set by commands that take a follow-up data
	// byte at port 0x60. When non-empty the next port-0x60 write is
	// routed to the named slot rather than discarded.
	nextDataTarget ps2DataTarget

	// Controller Configuration Byte. Defaults to bits {0=KBD-IRQ on,
	// 6=translation on} — what every other emulator returns by default,
	// so a guest's "read-modify-write" of the CCB doesn't accidentally
	// flip something it didn't intend to.
	ccb byte

	// Output port — written by the 0xD1 command. Bit 0 is the system-
	// reset line (real hardware: clearing it triple-faults the CPU).
	// Bit 1 is A20-gate. We don't act on either — we record them so a
	// reset-via-keyboard-controller path could be wired in later.
	outputPort byte
}

type ps2DataTarget uint8

const (
	ps2TargetDiscard ps2DataTarget = iota
	ps2TargetCCB
	ps2TargetOutputPort
	ps2TargetAux // mouse — also a no-op for us
)

// NewPS2Controller returns a fresh 8042 stub in its post-self-test
// default state.
func NewPS2Controller() *PS2Controller {
	return &PS2Controller{
		// 0x65 = OBF interrupt enabled + KBD clock enabled + translation enabled.
		// (bit 0 = KBD IRQ on, bit 5 = AUX clock disabled, bit 6 = translation on)
		ccb: 0x65,
	}
}

// Register wires the controller into the I/O dispatcher.
func (p *PS2Controller) Register(io *IOPortDispatcher) {
	io.RegisterRead(ps2PortData, ps2PortData, func(uint16) uint32 {
		return uint32(p.readData())
	})
	io.RegisterWrite(ps2PortData, ps2PortData, func(_ uint16, v uint32) {
		p.writeData(byte(v))
	})
	io.RegisterRead(ps2PortCmd, ps2PortCmd, func(uint16) uint32 {
		return uint32(p.readStatus())
	})
	io.RegisterWrite(ps2PortCmd, ps2PortCmd, func(_ uint16, v uint32) {
		p.writeCommand(byte(v))
	})
}

// readStatus implements the read side of port 0x64.
func (p *PS2Controller) readStatus() byte {
	var s byte
	if len(p.outputQueue) > 0 {
		s |= ps2StatusOBF
	}
	// IBF always 0 — we accept writes synchronously.
	return s
}

// readData implements the read side of port 0x60. Returns the queued
// byte or 0xFF when empty. Real silicon would also clear bit 0 of the
// status register; we do that implicitly because OBF is derived from
// the queue length.
func (p *PS2Controller) readData() byte {
	if len(p.outputQueue) == 0 {
		return 0xFF
	}
	b := p.outputQueue[0]
	p.outputQueue = p.outputQueue[1:]
	return b
}

// writeData implements the write side of port 0x60. Either consumed by
// a pending command (e.g. WriteCCB) or silently discarded.
func (p *PS2Controller) writeData(b byte) {
	switch p.nextDataTarget {
	case ps2TargetCCB:
		p.ccb = b
	case ps2TargetOutputPort:
		p.outputPort = b
	case ps2TargetAux:
		// Real hardware would forward this to the mouse and the mouse
		// would (eventually) ACK with 0xFA at the data port. We never
		// claim to have a mouse — silently drop.
	}
	p.nextDataTarget = ps2TargetDiscard
}

// writeCommand implements the write side of port 0x64 — the
// controller-command path.
func (p *PS2Controller) writeCommand(cmd byte) {
	// Any command supersedes a pending "next data writes go here"
	// arm-up; real hardware behaves the same way (the controller
	// arbitrates on the latest command).
	p.nextDataTarget = ps2TargetDiscard
	switch cmd {
	case ps2CmdReadCCB:
		p.pushOutput(p.ccb)
	case ps2CmdWriteCCB:
		p.nextDataTarget = ps2TargetCCB
	case ps2CmdSelfTest:
		p.pushOutput(0x55)
	case ps2CmdTestKbd, ps2CmdTestAux:
		p.pushOutput(0x00) // "no error detected"
	case ps2CmdReadOutput:
		p.pushOutput(p.outputPort)
	case ps2CmdWriteOutput:
		p.nextDataTarget = ps2TargetOutputPort
	case ps2CmdWriteToAux:
		p.nextDataTarget = ps2TargetAux
	case ps2CmdDisableKbd, ps2CmdEnableKbd,
		ps2CmdDisableAux, ps2CmdEnableAux:
		// State change only — no response byte.
	default:
		// Unknown command: real hardware silently ignores most of
		// the unused command space. Match that.
	}
}

// pushOutput appends a byte to the queue so the next port-0x60 read
// pops it. Multiple successive commands queue up; reads drain in order.
func (p *PS2Controller) pushOutput(b byte) {
	p.outputQueue = append(p.outputQueue, b)
}
