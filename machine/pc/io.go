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
type IOPortDispatcher struct {
	readHandlers  [65536]IOReadFunc
	writeHandlers [65536]IOWriteFunc
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
	if fn := io.readHandlers[port]; fn != nil {
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
	if fn := io.writeHandlers[port]; fn != nil {
		fn(port, uint32(val))
	}
}
