package main

import (
	"fmt"

	"github.com/jtolio/tinyemu-go/virtio"
)

// ConsoleDevice provides console I/O with escape sequence handling.
// Reference: tinyemu-2019-12-21/temu.c:58-66 (STDIODevice)
type ConsoleDevice struct {
	term     *Terminal
	escState int
	exitFlag bool
	// inputBuf holds input read from terminal when guest isn't ready to receive it.
	// This prevents early input loss during boot before VirtIO console is initialized.
	// Reference: tinyemu-2019-12-21/temu.c:554-566 - C version only reads from stdin
	// when virtio_console_can_write_data() is true, so OS buffers input. Go version
	// must buffer explicitly since we always read to process escape sequences.
	inputBuf []byte
}

// Escape sequence states
const (
	escNone    = 0
	escWaiting = 1 // Got C-a, waiting for next character
)

// NewConsoleDevice creates a new console device.
// Reference: tinyemu-2019-12-21/temu.c:177-205 (console_init)
func NewConsoleDevice(term *Terminal) *ConsoleDevice {
	return &ConsoleDevice{
		term: term,
	}
}

// CharDevice returns a VirtIO CharacterDevice for this console.
func (c *ConsoleDevice) CharDevice() *virtio.CharacterDevice {
	return &virtio.CharacterDevice{
		Writer: c,
		Reader: c,
	}
}

// Write implements io.Writer. It writes data to the console output.
func (c *ConsoleDevice) Write(buf []byte) (int, error) {
	return c.term.Write(buf)
}

// Read implements io.Reader. It reads data from the console input, processing escape sequences.
// Reference: tinyemu-2019-12-21/temu.c:105-153 (console_read)
func (c *ConsoleDevice) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	// Read from terminal
	tempBuf := make([]byte, len(buf))
	n := c.term.Read(tempBuf)
	if n == 0 {
		return 0, nil
	}

	// Process escape sequences
	j := 0
	for i := 0; i < n; i++ {
		ch := tempBuf[i]
		if c.escState == escWaiting {
			c.escState = escNone
			switch ch {
			case 'x':
				// C-a x: exit emulator
				c.exitFlag = true
				return 0, nil
			case 'h':
				// C-a h: print help
				c.printHelp()
			case 1:
				// C-a C-a: send literal C-a
				buf[j] = ch
				j++
			default:
				// Unknown escape, ignore
			}
		} else {
			if ch == 1 {
				// C-a (Ctrl-A)
				c.escState = escWaiting
			} else {
				buf[j] = ch
				j++
			}
		}
	}

	return j, nil
}

// ShouldExit returns true if the user requested exit via escape sequence.
func (c *ConsoleDevice) ShouldExit() bool {
	return c.exitFlag
}

// printHelp prints the console help message.
// Reference: tinyemu-2019-12-21/temu.c:131-136
func (c *ConsoleDevice) printHelp() {
	fmt.Printf("\n" +
		"C-a h   print this help\n" +
		"C-a x   exit emulator\n" +
		"C-a C-a send C-a\n")
}

// BufferInput stores input that couldn't be delivered to the guest yet.
// This is used when the VirtIO console's receive queue isn't ready.
func (c *ConsoleDevice) BufferInput(data []byte) {
	c.inputBuf = append(c.inputBuf, data...)
}

// GetBufferedInput returns buffered input up to maxLen bytes and removes
// those bytes from the buffer. Returns nil if no buffered input.
func (c *ConsoleDevice) GetBufferedInput(maxLen int) []byte {
	if len(c.inputBuf) == 0 {
		return nil
	}
	n := len(c.inputBuf)
	if n > maxLen {
		n = maxLen
	}
	result := c.inputBuf[:n]
	c.inputBuf = c.inputBuf[n:]
	return result
}

// BufferedLen returns the number of bytes buffered waiting to be delivered to the guest.
func (c *ConsoleDevice) BufferedLen() int {
	return len(c.inputBuf)
}
