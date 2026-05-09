// Package virtio provides VirtIO device emulation for the TinyEMU RISC-V emulator.
// This file implements the VirtIO Console device.
//
// Reference: TinyEMU virtio.c lines 1260-1360
package virtio

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/jtolio/tinyemu-go/mem"
)

// Console queue indices
// Reference: virtio.c - queue 0 is RX (host->guest), queue 1 is TX (guest->host)
const (
	ConsoleQueueRX = 0 // Receive queue (host writes to guest)
	ConsoleQueueTX = 1 // Transmit queue (guest writes to host)
)

// Console feature bits
// Reference: virtio.c line 1355
const (
	ConsoleFeatureSize = 1 << 0 // VIRTIO_CONSOLE_F_SIZE - console size configuration
)

// Console configuration space offsets
const (
	ConsoleConfigWidth  = 0 // 16-bit width
	ConsoleConfigHeight = 2 // 16-bit height
)

// Console configuration space size (4 bytes: 2 for width, 2 for height)
const ConsoleConfigSize = 4

// DebugCharDevice enables debug output for CharacterDevice.
var DebugCharDevice bool

// CharacterDevice represents a character device backend for the console.
// This allows the console to be connected to various I/O sources (stdin/stdout,
// files, network connections, etc.)
//
// The Reader should be non-blocking - it should return (0, nil) when no data
// is available, rather than blocking. When the input stream is closed,
// Reader should return io.EOF, which will be tracked by the EOF field.
//
// Reference: virtio.h lines 110-114
type CharacterDevice struct {
	// Writer receives data when the guest writes to the console (TX).
	// This typically writes to stdout or a pty.
	Writer io.Writer

	// Reader provides data from the host for the guest (RX).
	// Should be non-blocking: return (0, nil) when no data is available.
	// Return io.EOF when the input stream is closed.
	// This typically reads from stdin or a pty.
	Reader io.Reader

	// EOF is set to true when Reader returns io.EOF.
	// This can be checked to determine if the input stream has ended.
	EOF bool

	// ReadErr holds any non-EOF error from the last Read operation.
	ReadErr error

	// WriteErr holds any error from the last Write operation.
	WriteErr error
}

// ReadData reads data from the Reader into buf.
// Returns the number of bytes read. If the Reader returns io.EOF,
// the EOF field is set to true. Other errors are stored in ReadErr.
func (c *CharacterDevice) ReadData(buf []byte) int {
	if c.Reader == nil || c.EOF {
		return 0
	}
	n, err := c.Reader.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			c.EOF = true
		} else {
			c.ReadErr = err
		}
	}
	return n
}

// WriteData writes data to the Writer.
// Any error is stored in WriteErr.
func (c *CharacterDevice) WriteData(data []byte) {
	if c.Writer == nil {
		return
	}
	_, err := c.Writer.Write(data)
	if err != nil {
		c.WriteErr = err
	}
}

// Console represents a VirtIO console device.
// The console provides serial I/O between the guest and host.
//
// Queue 0 (RX): Host writes data to guest (e.g., keyboard input)
// Queue 1 (TX): Guest writes data to host (e.g., terminal output)
//
// Reference: virtio.c lines 1263-1266
type Console struct {
	dev   *Device
	cs    *CharacterDevice
	debug bool
}

// NewConsole creates a new VirtIO console device.
// Reference: virtio.c lines 1348-1360 (virtio_console_init)
func NewConsole(memMap *mem.PhysMemoryMap, addr uint64, irq *mem.IRQSignal,
	cs *CharacterDevice) (*Console, error) {

	c := &Console{
		cs: cs,
	}

	// Create the underlying VirtIO device
	var err error
	c.dev, err = NewDevice(memMap, addr, irq, DeviceIDConsole, ConsoleConfigSize, c.recvRequest)
	if err != nil {
		return nil, err
	}

	// Set device features
	c.dev.SetFeatures(ConsoleFeatureSize)

	// RX queue (queue 0) uses manual receive - we push data to it explicitly
	c.dev.Queues[ConsoleQueueRX].ManualRecv = true

	return c, nil
}

// Device returns the underlying VirtIO device.
func (c *Console) Device() *Device {
	return c.dev
}

// recvRequest handles incoming requests from the guest.
// For the console, this handles TX queue (guest -> host) data.
//
// Reference: virtio.c lines 1268-1285 (virtio_console_recv_request)
func (c *Console) recvRequest(dev *Device, queueIdx int, descIdx int, readSize int, writeSize int) int {
	if queueIdx == ConsoleQueueTX {
		// Guest is sending data to host (TX)
		if readSize > 0 && c.cs != nil {
			buf := make([]byte, readSize)
			if err := dev.MemcpyFromQueue(buf, queueIdx, descIdx, 0, readSize); err == nil {
				c.cs.WriteData(buf)
			}
		}
		// Signal that the descriptor has been consumed (with 0 bytes written back)
		dev.ConsumeDesc(queueIdx, descIdx, 0)
	}
	return 0
}

// CanWriteData checks if the guest has provided a buffer for receiving data.
// Returns true if there are available descriptors in the RX queue.
//
// Reference: virtio.c lines 1287-1296 (virtio_console_can_write_data)
func (c *Console) CanWriteData() bool {
	c.dev.mu.Lock()
	defer c.dev.mu.Unlock()

	qs := &c.dev.Queues[ConsoleQueueRX]

	if qs.Ready == 0 {
		return false
	}

	// Read available index from guest memory
	availIdx := c.dev.read16(qs.AvailAddr + 2)
	return qs.LastAvailIdx != availIdx
}

// GetWriteLen returns the maximum number of bytes that can be written to the guest.
// This checks the size of the first available write buffer in the RX queue.
//
// Reference: virtio.c lines 1298-1316 (virtio_console_get_write_len)
func (c *Console) GetWriteLen() int {
	c.dev.mu.Lock()
	defer c.dev.mu.Unlock()

	return c.getWriteLenLocked()
}

// getWriteLenLocked returns the write length without locking (caller must hold lock).
func (c *Console) getWriteLenLocked() int {
	qs := &c.dev.Queues[ConsoleQueueRX]

	if qs.Ready == 0 {
		return 0
	}

	// Read available index from guest memory
	availIdx := c.dev.read16(qs.AvailAddr + 2)
	if qs.LastAvailIdx == availIdx {
		return 0
	}

	// Get the descriptor index from the available ring
	descIdx := c.dev.read16(qs.AvailAddr + 4 + uint64(qs.LastAvailIdx&uint16(qs.Num-1))*2)

	// Calculate read/write sizes for this descriptor
	readSize, writeSize, ok := c.dev.getDescRWSize(ConsoleQueueRX, int(descIdx))
	if !ok {
		return 0
	}
	// For RX queue, we need write descriptors (host writes to guest)
	_ = readSize // unused for RX
	return writeSize
}

// WriteData writes data from the host to the guest via the RX queue.
// Returns the number of bytes written.
//
// Reference: virtio.c lines 1318-1336 (virtio_console_write_data)
func (c *Console) WriteData(buf []byte) int {
	c.dev.mu.Lock()
	defer c.dev.mu.Unlock()

	qs := &c.dev.Queues[ConsoleQueueRX]

	if qs.Ready == 0 {
		return 0
	}

	// Read available index from guest memory
	availIdx := c.dev.read16(qs.AvailAddr + 2)
	if qs.LastAvailIdx == availIdx {
		return 0
	}

	// Get the descriptor index from the available ring
	descIdx := c.dev.read16(qs.AvailAddr + 4 + uint64(qs.LastAvailIdx&uint16(qs.Num-1))*2)

	// Copy data to guest memory
	bufLen := len(buf)
	if err := c.dev.MemcpyToQueue(ConsoleQueueRX, int(descIdx), 0, buf, bufLen); err != nil {
		return 0
	}

	// Signal that the descriptor has been consumed
	c.dev.consumeDescLocked(ConsoleQueueRX, int(descIdx), bufLen)

	// Advance the last available index
	qs.LastAvailIdx++

	return bufLen
}

// SetSize sets the console dimensions in the configuration space
// WITHOUT raising an interrupt. Use this for initial setup before
// the guest driver is ready.
func (c *Console) SetSize(width, height int) {
	c.dev.mu.Lock()
	defer c.dev.mu.Unlock()

	binary.LittleEndian.PutUint16(c.dev.ConfigSpace[ConsoleConfigWidth:], uint16(width))
	binary.LittleEndian.PutUint16(c.dev.ConfigSpace[ConsoleConfigHeight:], uint16(height))
}

// ResizeEvent sends a resize notification to the guest.
// This updates the console dimensions in the configuration space
// and raises a configuration change interrupt.
//
// Reference: virtio.c lines 1339-1346 (virtio_console_resize_event)
func (c *Console) ResizeEvent(width, height int) {
	c.dev.mu.Lock()
	defer c.dev.mu.Unlock()

	// Update configuration space with new dimensions
	binary.LittleEndian.PutUint16(c.dev.ConfigSpace[ConsoleConfigWidth:], uint16(width))
	binary.LittleEndian.PutUint16(c.dev.ConfigSpace[ConsoleConfigHeight:], uint16(height))

	// Raise configuration change interrupt
	c.configChangeNotify()
}

// configChangeNotify raises a configuration change interrupt.
// Reference: virtio.c lines 973-978 (virtio_config_change_notify)
func (c *Console) configChangeNotify() {
	// INT_CONFIG interrupt (bit 1)
	c.dev.IntStatus |= 2
	if c.dev.IRQ != nil {
		c.dev.IRQ.Raise()
	}
}

// SetDebug sets debug flags for the console device.
func (c *Console) SetDebug(flags int) {
	c.debug = flags != 0
	c.dev.SetDebug(flags)
}
