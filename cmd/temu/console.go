package main

import (
	"fmt"
	"sync/atomic"

	"github.com/jtolio/tinyemu-go/virtio"
)

// ConsoleDevice provides console I/O with escape sequence handling.
// Reference: tinyemu-2019-12-21/temu.c:58-66 (STDIODevice)
//
// Stdin is read by a background goroutine (StartReader) that blocks
// in poll(2) and feeds incoming bytes into termCh. The main emulator
// loop calls Read which drains termCh non-blockingly — no syscall on
// the hot path. Before the goroutine existed, the main loop did one
// non-blocking unix.Read every iteration; with the emulator's Run
// returning early on every HLT, that climbed to ~144K syscalls/sec
// and accounted for ~28% of total CPU time (per profile).
type ConsoleDevice struct {
	term     *Terminal
	escState int
	exitFlag atomic.Bool
	// inputBuf holds input read from terminal when guest isn't ready to receive it.
	// This prevents early input loss during boot before VirtIO console is initialized.
	// Reference: tinyemu-2019-12-21/temu.c:554-566 - C version only reads from stdin
	// when virtio_console_can_write_data() is true, so OS buffers input. Go version
	// must buffer explicitly since we always read to process escape sequences.
	inputBuf []byte

	// Async reader plumbing. termCh carries already-escape-processed
	// bytes from the reader goroutine to whoever calls Read.
	termCh chan []byte
	stopCh chan struct{}
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

// StartReader spawns a goroutine that blocks in Terminal.ReadBlocking
// and pushes already-escape-processed bytes onto termCh. Must be
// called once after the console is constructed.
func (c *ConsoleDevice) StartReader() {
	c.termCh = make(chan []byte, 16)
	c.stopCh = make(chan struct{})
	go c.readLoop()
}

// StopReader signals the reader goroutine to exit. The goroutine may
// still be parked in poll(2); it'll exit on the next data event or
// when the process terminates.
func (c *ConsoleDevice) StopReader() {
	if c.stopCh != nil {
		close(c.stopCh)
	}
}

func (c *ConsoleDevice) readLoop() {
	buf := make([]byte, 256)
	for {
		select {
		case <-c.stopCh:
			return
		default:
		}
		n := c.term.ReadBlocking(buf)
		if n <= 0 {
			// Treat EOF/error like a brief pause and retry. If the
			// terminal is genuinely gone the next Read returns 0
			// again; we just spin slowly until stopCh closes.
			select {
			case <-c.stopCh:
				return
			default:
			}
			continue
		}
		processed := c.processEscapes(buf[:n])
		if len(processed) == 0 {
			continue
		}
		out := make([]byte, len(processed))
		copy(out, processed)
		select {
		case c.termCh <- out:
		case <-c.stopCh:
			return
		}
	}
}

// processEscapes runs the C-a x / C-a h state machine over raw input.
// Returns the bytes that should reach the guest (escape bytes are
// stripped). Side effect: sets exitFlag on C-a x.
func (c *ConsoleDevice) processEscapes(raw []byte) []byte {
	out := raw[:0]
	for _, ch := range raw {
		if c.escState == escWaiting {
			c.escState = escNone
			switch ch {
			case 'x':
				c.exitFlag.Store(true)
				return out
			case 'h':
				c.printHelp()
			case 1:
				out = append(out, ch)
			default:
				// unknown escape — ignore
			}
		} else {
			if ch == 1 {
				c.escState = escWaiting
			} else {
				out = append(out, ch)
			}
		}
	}
	return out
}

// Read implements io.Reader. Drains the channel non-blockingly; no
// syscall on the fast path.
// Reference: tinyemu-2019-12-21/temu.c:105-153 (console_read)
func (c *ConsoleDevice) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	if c.termCh == nil {
		return 0, nil
	}
	var data []byte
	select {
	case data = <-c.termCh:
	default:
		return 0, nil
	}
	n := copy(buf, data)
	if n < len(data) {
		// Couldn't fit it all — buffer the leftover.
		c.inputBuf = append(c.inputBuf, data[n:]...)
	}
	return n, nil
}

// ShouldExit returns true if the user requested exit via escape sequence.
func (c *ConsoleDevice) ShouldExit() bool {
	return c.exitFlag.Load()
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
