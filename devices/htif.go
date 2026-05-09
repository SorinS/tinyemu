// Package devices provides hardware device emulation for the TinyEMU RISC-V emulator.
package devices

import (
	"sync"

	"github.com/jtolio/tinyemu-go/mem"
)

// HTIF memory-mapped register offsets and addresses
const (
	HTIFBaseAddr = 0x40008000
	HTIFSize     = 16 // 2 x 64-bit registers

	// Register offsets - matches OpenSBI's expected layout
	// (fromhost at base+0, tohost at base+8)
	HTIFFromHostOffset = 0x0 // fromhost register (64-bit, split as lo/hi)
	HTIFToHostOffset   = 0x8 // tohost register (64-bit, split as lo/hi)
)

// HTIF device and command identifiers
const (
	HTIFDeviceConsole = 1

	HTIFCmdConsolePutchar = 1 // Write character to console
	HTIFCmdConsoleGetchar = 0 // Request keyboard input
)

// Console provides console I/O for HTIF.
type Console interface {
	// WriteData writes data to the console output.
	WriteData(data []byte)
	// ReadData reads available data from the console input.
	// Returns the number of bytes read.
	ReadData(buf []byte) int
}

// ShutdownHandler is called when HTIF receives a shutdown command.
type ShutdownHandler func(exitCode int)

// HTIF implements the Host-Target Interface.
// It provides a simple interface for console I/O and system control
// between the emulated system and the host.
//
// The HTIF protocol uses two 64-bit registers:
//   - tohost: Guest writes commands here for the host to process
//   - fromhost: Host writes responses here for the guest to read
//
// Command format in tohost:
//   - bits 63:56 - device ID
//   - bits 55:48 - command
//   - bits 47:0  - payload
type HTIF struct {
	mu sync.Mutex

	// tohost register - commands from guest to host
	tohost uint64

	// fromhost register - responses from host to guest
	fromhost uint64

	// Console for character I/O
	console Console

	// Shutdown handler
	shutdownHandler ShutdownHandler

	// Flag indicating shutdown was requested
	shutdownRequested bool
	shutdownExitCode  int

	// Debug enables debug output
	Debug bool
}

// NewHTIF creates a new HTIF device.
func NewHTIF(console Console) *HTIF {
	return &HTIF{
		console: console,
	}
}

// SetShutdownHandler sets the callback for shutdown commands.
func (h *HTIF) SetShutdownHandler(handler ShutdownHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shutdownHandler = handler
}

// SetConsole sets the console interface.
func (h *HTIF) SetConsole(console Console) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.console = console
}

// IsShutdownRequested returns true if a shutdown was requested.
func (h *HTIF) IsShutdownRequested() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shutdownRequested
}

// GetShutdownExitCode returns the exit code from the shutdown request.
func (h *HTIF) GetShutdownExitCode() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shutdownExitCode
}

// handleCommand processes a command written to tohost.
// Reference: riscv_machine.c:129-151 (htif_handle_cmd)
func (h *HTIF) handleCommand() {
	// Decode command
	device := (h.tohost >> 56) & 0xFF
	cmd := (h.tohost >> 48) & 0xFF
	payload := h.tohost & 0xFFFFFFFFFFFF

	// Special case: tohost == 1 means shutdown
	if h.tohost == 1 {
		h.shutdownRequested = true
		h.shutdownExitCode = 0
		if h.shutdownHandler != nil {
			h.shutdownHandler(0)
		}
		return
	}

	// Handle by device
	switch device {
	case HTIFDeviceConsole:
		h.handleConsoleCommand(cmd, payload)
	default:
		// Unknown device - ignore
	}
}

// handleConsoleCommand processes console device commands.
func (h *HTIF) handleConsoleCommand(cmd uint64, payload uint64) {
	switch cmd {
	case HTIFCmdConsolePutchar:
		// Write character to console
		if h.console != nil {
			buf := []byte{byte(payload & 0xFF)}
			h.console.WriteData(buf)
		}
		// Clear tohost and acknowledge in fromhost
		h.tohost = 0
		h.fromhost = (uint64(HTIFDeviceConsole) << 56) | (uint64(HTIFCmdConsolePutchar) << 48)

	case HTIFCmdConsoleGetchar:
		// Request keyboard input - guest is polling for input
		h.tohost = 0

	default:
		// Unknown command
	}
}

// Poll is a no-op. HTIF input polling is disabled to match TinyEMU C behavior.
//
// In the C implementation (riscv_machine.c:179-192), htif_poll is wrapped in
// "#if 0" and never called. HTIF is only used for boot messages and poweroff;
// the OS uses VirtIO console for input. If HTIF were to poll, it would consume
// input characters before the VirtIO console could receive them.
//
// Reference: riscv_machine.c:179-193 (htif_poll disabled with #if 0)
func (h *HTIF) Poll() {
	// Intentionally empty - HTIF does not poll for input
}

// Read handles HTIF register reads.
// This implements mem.DeviceReadFunc.
// Reference: riscv_machine.c:102-127 (htif_read)
func (h *HTIF) Read(opaque any, offset uint32, sizeLog2 int) uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch offset {
	case HTIFToHostOffset:
		return uint32(h.tohost)
	case HTIFToHostOffset + 4:
		return uint32(h.tohost >> 32)
	case HTIFFromHostOffset:
		return uint32(h.fromhost)
	case HTIFFromHostOffset + 4:
		return uint32(h.fromhost >> 32)
	default:
		return 0
	}
}

// Write handles HTIF register writes.
// This implements mem.DeviceWriteFunc.
// Reference: riscv_machine.c:153-177 (htif_write)
func (h *HTIF) Write(opaque any, offset uint32, val uint32, sizeLog2 int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch offset {
	case HTIFToHostOffset:
		h.tohost = (h.tohost & 0xFFFFFFFF00000000) | uint64(val)
	case HTIFToHostOffset + 4:
		h.tohost = (h.tohost & 0x00000000FFFFFFFF) | (uint64(val) << 32)
		// Command is complete when high word is written
		h.handleCommand()
	case HTIFFromHostOffset:
		h.fromhost = (h.fromhost & 0xFFFFFFFF00000000) | uint64(val)
	case HTIFFromHostOffset + 4:
		h.fromhost = (h.fromhost & 0x00000000FFFFFFFF) | (uint64(val) << 32)
	}
}

// GetToHost returns the current tohost register value.
func (h *HTIF) GetToHost() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tohost
}

// GetFromHost returns the current fromhost register value.
func (h *HTIF) GetFromHost() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fromhost
}

// SetFromHost sets the fromhost register value.
// This is useful for injecting console input.
func (h *HTIF) SetFromHost(val uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fromhost = val
}

// Register registers the HTIF with a memory map at the default address.
func (h *HTIF) Register(memMap *mem.PhysMemoryMap) (*mem.PhysMemoryRange, error) {
	return h.RegisterAt(memMap, HTIFBaseAddr)
}

// RegisterAt registers the HTIF with a memory map at a custom address.
func (h *HTIF) RegisterAt(memMap *mem.PhysMemoryMap, addr uint64) (*mem.PhysMemoryRange, error) {
	return memMap.RegisterDevice(addr, HTIFSize, h, h.Read, h.Write, mem.DevIOSize32)
}
