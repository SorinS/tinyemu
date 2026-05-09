// Package pc implements an x86 PC machine/board emulator.
package pc

// Keyboard8042 implements a minimal 8042 PS/2 keyboard controller stub.
// Just enough to prevent Linux from hanging during boot probe.
type Keyboard8042 struct {
	status  uint8
	command uint8
	outBuf  uint8
}

// NewKeyboard8042 creates a new keyboard controller stub.
func NewKeyboard8042() *Keyboard8042 {
	return &Keyboard8042{
		// Bit 2: system flag (self-test passed)
		// Bit 3: command/data (0 = data written to 0x60)
		// Bit 4: keyboard locked (0 = unlocked)
		// Bit 5: auxiliary output buffer full (0 = empty)
		// Bit 6: general timeout (0 = no error)
		// Bit 7: parity error (0 = no error)
		status: 0x04,
	}
}

// Register registers the keyboard controller I/O ports.
func (k *Keyboard8042) Register(io *IOPortDispatcher) {
	io.RegisterRead(0x60, 0x60, func(port uint16) uint32 {
		return uint32(k.outBuf)
	})
	io.RegisterRead(0x64, 0x64, func(port uint16) uint32 {
		return uint32(k.status)
	})
	io.RegisterWrite(0x60, 0x60, func(port uint16, val uint32) {
		// Data port write
	})
	io.RegisterWrite(0x64, 0x64, func(port uint16, val uint32) {
		k.handleCommand(uint8(val))
	})
}

func (k *Keyboard8042) handleCommand(cmd uint8) {
	switch cmd {
	case 0xAA:
		// Self-test command
		k.outBuf = 0x55 // Self-test passed
		k.status |= 0x01
	case 0xAB:
		// Interface test command
		k.outBuf = 0x00 // No error
		k.status |= 0x01
	case 0xAD:
		// Disable keyboard
	case 0xAE:
		// Enable keyboard
	case 0xD0:
		// Read output port
		k.outBuf = 0x01 // bit 0: system reset line
		k.status |= 0x01
	case 0xD1:
		// Write output port (next byte written to 0x60)
	case 0xFE:
		// Pulse output line low (system reset)
		// In a real system this would reset the CPU; we ignore it
	default:
		// Ignore unknown commands
	}
}
