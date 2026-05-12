package pc

import (
	"fmt"
	"os"
)

// ioDebug controls trace logging of port I/O. Enabled with TINYEMU_X86_IO_DEBUG=1.
var ioDebug = os.Getenv("TINYEMU_X86_IO_DEBUG") == "1"

// IOReadFunc is called when an I/O port is read.
type IOReadFunc func(port uint16) uint32

// IOWriteFunc is called when an I/O port is written.
type IOWriteFunc func(port uint16, val uint32)

// IOPortDispatcher manages the x86 I/O port space (64K ports).
//
// Most devices only need 8-bit register access (PIC, PIT, UART, …). A few
// devices — ATA's data port at 0x1F0 in particular — need to behave
// differently for 16-bit accesses, because a single `inw` should advance the
// device's internal byte position by 2. For those cases a device may
// register a width-specific handler via RegisterRead16 / RegisterWrite16,
// which Read16 / Write16 prefer over the 8-bit handler.
type IOPortDispatcher struct {
	readHandlers    [65536]IOReadFunc
	writeHandlers   [65536]IOWriteFunc
	readHandlers16  [65536]IOReadFunc  // optional, 16-bit-only
	writeHandlers16 [65536]IOWriteFunc // optional, 16-bit-only
	readHandlers32  [65536]IOReadFunc  // optional, 32-bit-only
	writeHandlers32 [65536]IOWriteFunc // optional, 32-bit-only
}

// NewIOPortDispatcher creates a new I/O port dispatcher.
func NewIOPortDispatcher() *IOPortDispatcher {
	return &IOPortDispatcher{}
}

// RegisterRead registers a read handler for a range of ports.
func (io *IOPortDispatcher) RegisterRead(start, end uint16, fn IOReadFunc) {
	for i := start; i <= end && i >= start; i++ {
		io.readHandlers[i] = fn
	}
}

// RegisterWrite registers a write handler for a range of ports.
func (io *IOPortDispatcher) RegisterWrite(start, end uint16, fn IOWriteFunc) {
	for i := start; i <= end && i >= start; i++ {
		io.writeHandlers[i] = fn
	}
}

// RegisterRead16 registers a 16-bit-specific read handler. Read16 prefers
// this over the 8-bit handler when set; Read8 ignores it.
func (io *IOPortDispatcher) RegisterRead16(start, end uint16, fn IOReadFunc) {
	for i := start; i <= end && i >= start; i++ {
		io.readHandlers16[i] = fn
	}
}

// RegisterWrite16 registers a 16-bit-specific write handler. Write16 prefers
// this over the 8-bit handler when set; Write8 ignores it.
func (io *IOPortDispatcher) RegisterWrite16(start, end uint16, fn IOWriteFunc) {
	for i := start; i <= end && i >= start; i++ {
		io.writeHandlers16[i] = fn
	}
}

// Read8 reads an 8-bit value from a port.
func (io *IOPortDispatcher) Read8(port uint16) uint8 {
	var val uint8 = 0xFF
	if fn := io.readHandlers[port]; fn != nil {
		val = uint8(fn(port))
	}
	if ioDebug {
		fmt.Fprintf(os.Stderr, "[io] in  %04x => %02x\n", port, val)
	}
	return val
}

// Read16 reads a 16-bit value from a port.
func (io *IOPortDispatcher) Read16(port uint16) uint16 {
	var val uint16 = 0xFFFF
	if fn := io.readHandlers16[port]; fn != nil {
		val = uint16(fn(port))
	} else if fn := io.readHandlers[port]; fn != nil {
		val = uint16(fn(port))
	}
	if ioDebug {
		fmt.Fprintf(os.Stderr, "[io] inw %04x => %04x\n", port, val)
	}
	return val
}

// Write8 writes an 8-bit value to a port.
func (io *IOPortDispatcher) Write8(port uint16, val uint8) {
	if ioDebug {
		fmt.Fprintf(os.Stderr, "[io] out %04x <= %02x\n", port, val)
	}
	if fn := io.writeHandlers[port]; fn != nil {
		fn(port, uint32(val))
	}
}

// Write16 writes a 16-bit value to a port.
func (io *IOPortDispatcher) Write16(port uint16, val uint16) {
	if ioDebug {
		fmt.Fprintf(os.Stderr, "[io] outw %04x <= %04x\n", port, val)
	}
	if fn := io.writeHandlers16[port]; fn != nil {
		fn(port, uint32(val))
	} else if fn := io.writeHandlers[port]; fn != nil {
		fn(port, uint32(val))
	}
}

// RegisterRead32 registers a 32-bit-specific read handler. Read32 prefers
// this over 16/8-bit handlers when set.
func (io *IOPortDispatcher) RegisterRead32(start, end uint16, fn IOReadFunc) {
	for i := start; i <= end && i >= start; i++ {
		io.readHandlers32[i] = fn
	}
}

// RegisterWrite32 registers a 32-bit-specific write handler. Write32 prefers
// this over 16/8-bit handlers when set.
func (io *IOPortDispatcher) RegisterWrite32(start, end uint16, fn IOWriteFunc) {
	for i := start; i <= end && i >= start; i++ {
		io.writeHandlers32[i] = fn
	}
}

// Read32 reads a 32-bit value from a port. Prefers an explicit 32-bit
// handler; falls back to two 16-bit reads if none is registered.
func (io *IOPortDispatcher) Read32(port uint16) uint32 {
	var val uint32
	if fn := io.readHandlers32[port]; fn != nil {
		val = fn(port)
	} else {
		val = uint32(io.Read16(port)) | (uint32(io.Read16(port+2)) << 16)
	}
	if ioDebug {
		fmt.Fprintf(os.Stderr, "[io] inl  %04x => %08x\n", port, val)
	}
	return val
}

// Write32 writes a 32-bit value to a port. Prefers an explicit 32-bit
// handler; falls back to two 16-bit writes if none is registered.
func (io *IOPortDispatcher) Write32(port uint16, val uint32) {
	if ioDebug {
		fmt.Fprintf(os.Stderr, "[io] outl %04x <= %08x\n", port, val)
	}
	if fn := io.writeHandlers32[port]; fn != nil {
		fn(port, val)
		return
	}
	io.Write16(port, uint16(val))
	io.Write16(port+2, uint16(val>>16))
}
