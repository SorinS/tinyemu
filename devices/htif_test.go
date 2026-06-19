package devices

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// mockConsole implements Console for testing
type mockConsole struct {
	writeData []byte
	readData  []byte
	readPos   int
}

func (m *mockConsole) WriteData(data []byte) {
	m.writeData = append(m.writeData, data...)
}

func (m *mockConsole) ReadData(buf []byte) int {
	if m.readPos >= len(m.readData) {
		return 0
	}
	n := copy(buf, m.readData[m.readPos:])
	m.readPos += n
	return n
}

func TestHTIFNew(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)

	if htif == nil {
		t.Fatal("NewHTIF returned nil")
	}

	// Initial registers should be 0
	if htif.GetToHost() != 0 {
		t.Errorf("initial tohost = 0x%x, want 0", htif.GetToHost())
	}
	if htif.GetFromHost() != 0 {
		t.Errorf("initial fromhost = 0x%x, want 0", htif.GetFromHost())
	}

	// Should not be shutdown initially
	if htif.IsShutdownRequested() {
		t.Error("should not be shutdown initially")
	}
}

func TestHTIFConsolePutchar(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)

	// Write character 'A' via HTIF
	// Format: device=1, cmd=1, payload='A'
	cmd := (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsolePutchar) << 48) |
		uint64('A')

	// Write low word first
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	// Write high word - this triggers command processing
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	// Check character was written to console
	if len(console.writeData) != 1 || console.writeData[0] != 'A' {
		t.Errorf("console data = %v, want ['A']", console.writeData)
	}

	// tohost should be cleared
	if htif.GetToHost() != 0 {
		t.Errorf("tohost = 0x%x, want 0 (cleared after command)", htif.GetToHost())
	}

	// fromhost should have acknowledgment
	expectedFromhost := (uint64(HTIFDeviceConsole) << 56) | (uint64(HTIFCmdConsolePutchar) << 48)
	if htif.GetFromHost() != expectedFromhost {
		t.Errorf("fromhost = 0x%x, want 0x%x", htif.GetFromHost(), expectedFromhost)
	}
}

func TestHTIFConsolePutcharMultiple(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)

	// Write "Hello"
	message := "Hello"
	for _, ch := range message {
		cmd := (uint64(HTIFDeviceConsole) << 56) |
			(uint64(HTIFCmdConsolePutchar) << 48) |
			uint64(ch)
		htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
		htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)
	}

	if string(console.writeData) != message {
		t.Errorf("console output = %q, want %q", string(console.writeData), message)
	}
}

// TestHTIFPollIsNoOp verifies that Poll() is intentionally a no-op.
// In C TinyEMU, htif_poll is disabled with #if 0 (riscv_machine.c:179-193).
// HTIF is only used for boot messages and poweroff; the OS uses VirtIO console.
func TestHTIFPollIsNoOp(t *testing.T) {
	console := &mockConsole{
		readData: []byte{'X'},
	}
	htif := NewHTIF(console)

	// Poll should NOT read character - it's a no-op
	htif.Poll()

	// fromhost should remain 0 (Poll does nothing)
	if htif.GetFromHost() != 0 {
		t.Errorf("fromhost = 0x%x, want 0 (Poll should be no-op)", htif.GetFromHost())
	}

	// Console should not have been read
	if console.readPos != 0 {
		t.Errorf("console was read, but Poll should be no-op")
	}
}

func TestHTIFFromHostManualSet(t *testing.T) {
	htif := NewHTIF(nil)

	// SetFromHost can still be used to inject input manually if needed
	expectedFromhost := (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsoleGetchar) << 48) |
		uint64('X')
	htif.SetFromHost(expectedFromhost)

	if htif.GetFromHost() != expectedFromhost {
		t.Errorf("fromhost = 0x%x, want 0x%x", htif.GetFromHost(), expectedFromhost)
	}
}

func TestHTIFShutdown(t *testing.T) {
	htif := NewHTIF(nil)

	var shutdownCalled bool
	var shutdownCode int
	htif.SetShutdownHandler(func(exitCode int) {
		shutdownCalled = true
		shutdownCode = exitCode
	})

	// Write shutdown command (tohost = 1)
	htif.Write(nil, HTIFToHostOffset, 1, 2)
	htif.Write(nil, HTIFToHostOffset+4, 0, 2)

	if !htif.IsShutdownRequested() {
		t.Error("shutdown should be requested")
	}
	if !shutdownCalled {
		t.Error("shutdown handler should have been called")
	}
	if shutdownCode != 0 {
		t.Errorf("shutdown exit code = %d, want 0", shutdownCode)
	}
	if htif.GetShutdownExitCode() != 0 {
		t.Errorf("GetShutdownExitCode = %d, want 0", htif.GetShutdownExitCode())
	}
}

func TestHTIFReadWrite(t *testing.T) {
	htif := NewHTIF(nil)

	// Test tohost read/write via Read/Write methods
	htif.Write(nil, HTIFToHostOffset, 0x12345678, 2)
	val := htif.Read(nil, HTIFToHostOffset, 2)
	if val != 0x12345678 {
		t.Errorf("tohost lo = 0x%x, want 0x12345678", val)
	}

	// Note: writing high word triggers command processing, which may clear tohost
	// Let's test reading without triggering command

	// Test fromhost read/write
	htif.Write(nil, HTIFFromHostOffset, 0xDEADBEEF, 2)
	val = htif.Read(nil, HTIFFromHostOffset, 2)
	if val != 0xDEADBEEF {
		t.Errorf("fromhost lo = 0x%x, want 0xDEADBEEF", val)
	}

	htif.Write(nil, HTIFFromHostOffset+4, 0xCAFEBABE, 2)
	val = htif.Read(nil, HTIFFromHostOffset+4, 2)
	if val != 0xCAFEBABE {
		t.Errorf("fromhost hi = 0x%x, want 0xCAFEBABE", val)
	}

	// Verify combined fromhost value
	expected := uint64(0xCAFEBABEDEADBEEF)
	if htif.GetFromHost() != expected {
		t.Errorf("fromhost = 0x%x, want 0x%x", htif.GetFromHost(), expected)
	}
}

func TestHTIFUnknownDevice(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)

	// Send command to unknown device (device=5)
	cmd := (uint64(5) << 56) | (uint64(0) << 48) | uint64(0)
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	// Should not crash, console should be empty
	if len(console.writeData) != 0 {
		t.Errorf("console data = %v, want empty", console.writeData)
	}
}

func TestHTIFUnknownConsoleCommand(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)

	// Send unknown command to console (device=1, cmd=99)
	cmd := (uint64(HTIFDeviceConsole) << 56) | (uint64(99) << 48) | uint64(0)
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	// Should not crash, console should be empty
	if len(console.writeData) != 0 {
		t.Errorf("console data = %v, want empty", console.writeData)
	}
}

func TestHTIFNilConsole(t *testing.T) {
	htif := NewHTIF(nil)

	// Write character with nil console - should not crash
	cmd := (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsolePutchar) << 48) |
		uint64('A')
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	// Poll with nil console - should not crash
	htif.Poll()
}

func TestHTIFSetConsole(t *testing.T) {
	htif := NewHTIF(nil)

	// Initially nil console
	cmd := (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsolePutchar) << 48) |
		uint64('A')
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	// Set console
	console := &mockConsole{}
	htif.SetConsole(console)

	// Now write should work
	cmd = (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsolePutchar) << 48) |
		uint64('B')
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	if len(console.writeData) != 1 || console.writeData[0] != 'B' {
		t.Errorf("console data = %v, want ['B']", console.writeData)
	}
}

func TestHTIFRegister(t *testing.T) {
	htif := NewHTIF(nil)
	memMap := mem.NewPhysMemoryMap()

	pr, err := htif.Register(memMap)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if pr.Addr != HTIFBaseAddr {
		t.Errorf("addr = 0x%x, want 0x%x", pr.Addr, HTIFBaseAddr)
	}
	if pr.Size != HTIFSize {
		t.Errorf("size = 0x%x, want 0x%x", pr.Size, HTIFSize)
	}
	if pr.IsRAM {
		t.Error("HTIF should not be RAM")
	}
}

func TestHTIFRegisterAt(t *testing.T) {
	htif := NewHTIF(nil)
	memMap := mem.NewPhysMemoryMap()

	customAddr := uint64(0x50000000)
	pr, err := htif.RegisterAt(memMap, customAddr)
	if err != nil {
		t.Fatalf("RegisterAt failed: %v", err)
	}

	if pr.Addr != customAddr {
		t.Errorf("addr = 0x%x, want 0x%x", pr.Addr, customAddr)
	}
}

func TestHTIFMemMapAccess(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)
	memMap := mem.NewPhysMemoryMap()

	_, err := htif.Register(memMap)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Write character via memory map
	cmd := (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsolePutchar) << 48) |
		uint64('Z')

	err = memMap.Write32(HTIFBaseAddr+HTIFToHostOffset, uint32(cmd))
	if err != nil {
		t.Fatalf("Write32 failed: %v", err)
	}
	err = memMap.Write32(HTIFBaseAddr+HTIFToHostOffset+4, uint32(cmd>>32))
	if err != nil {
		t.Fatalf("Write32 failed: %v", err)
	}

	if len(console.writeData) != 1 || console.writeData[0] != 'Z' {
		t.Errorf("console data = %v, want ['Z']", console.writeData)
	}

	// Read fromhost via memory map
	valLo, err := memMap.Read32(HTIFBaseAddr + HTIFFromHostOffset)
	if err != nil {
		t.Fatalf("Read32 failed: %v", err)
	}
	valHi, err := memMap.Read32(HTIFBaseAddr + HTIFFromHostOffset + 4)
	if err != nil {
		t.Fatalf("Read32 failed: %v", err)
	}
	// Should have acknowledgment (device=1, cmd=1 in high bytes)
	fromhost := uint64(valLo) | (uint64(valHi) << 32)
	expectedFromhost := (uint64(HTIFDeviceConsole) << 56) | (uint64(HTIFCmdConsolePutchar) << 48)
	if fromhost != expectedFromhost {
		t.Errorf("fromhost = 0x%x, want 0x%x", fromhost, expectedFromhost)
	}
}

func TestHTIFConstants(t *testing.T) {
	// Verify HTIF constants match OpenSBI's expected layout
	// (fromhost at base+0, tohost at base+8)
	if HTIFBaseAddr != 0x40008000 {
		t.Errorf("HTIFBaseAddr = 0x%x, want 0x40008000", HTIFBaseAddr)
	}
	if HTIFSize != 16 {
		t.Errorf("HTIFSize = %d, want 16", HTIFSize)
	}
	if HTIFFromHostOffset != 0 {
		t.Errorf("HTIFFromHostOffset = 0x%x, want 0x0", HTIFFromHostOffset)
	}
	if HTIFToHostOffset != 8 {
		t.Errorf("HTIFToHostOffset = 0x%x, want 0x8", HTIFToHostOffset)
	}
}

func TestHTIFUnusedOffset(t *testing.T) {
	htif := NewHTIF(nil)

	// Reading from offset beyond registers should return 0
	// (but our registers are small, so this tests boundary)
	val := htif.Read(nil, 16, 2)
	if val != 0 {
		t.Errorf("unused offset read = %d, want 0", val)
	}

	// Writing to unused offset should not panic
	htif.Write(nil, 16, 0x12345678, 2)
}

func TestHTIFSetFromHost(t *testing.T) {
	htif := NewHTIF(nil)

	// Set fromhost directly
	htif.SetFromHost(0x123456789ABCDEF0)
	if htif.GetFromHost() != 0x123456789ABCDEF0 {
		t.Errorf("fromhost = 0x%x, want 0x123456789ABCDEF0", htif.GetFromHost())
	}

	// Verify read via Read method
	lo := htif.Read(nil, HTIFFromHostOffset, 2)
	hi := htif.Read(nil, HTIFFromHostOffset+4, 2)
	combined := uint64(lo) | (uint64(hi) << 32)
	if combined != 0x123456789ABCDEF0 {
		t.Errorf("combined = 0x%x, want 0x123456789ABCDEF0", combined)
	}
}

func TestHTIFConsoleGetcharRequest(t *testing.T) {
	console := &mockConsole{}
	htif := NewHTIF(console)

	// Send getchar request (device=1, cmd=0)
	cmd := (uint64(HTIFDeviceConsole) << 56) |
		(uint64(HTIFCmdConsoleGetchar) << 48)
	htif.Write(nil, HTIFToHostOffset, uint32(cmd), 2)
	htif.Write(nil, HTIFToHostOffset+4, uint32(cmd>>32), 2)

	// tohost should be cleared
	if htif.GetToHost() != 0 {
		t.Errorf("tohost = 0x%x, want 0 (cleared after getchar request)", htif.GetToHost())
	}
}
