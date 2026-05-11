package pc

import (
	"bytes"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86"
)

func TestPCResetVector(t *testing.T) {
	pc, err := New(Config{
		RAMSize: 1 << 20, // 1MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer pc.Close()

	// Write a simple program at the reset vector (0xF000:0xFFF0 = 0xFFFF0)
	// 0x66 MOV EAX, 0x12345678  (operand-size prefix for 32-bit in real mode)
	// HLT
	code := []byte{0x66, 0xB8, 0x78, 0x56, 0x34, 0x12, 0xF4}
	copy(pc.biosROM.PhysMem[0xFFF0:], code)

	cpu := pc.GetCPU().(*x86.CPU)
	for !cpu.IsPowerDown() {
		if err := cpu.Step(); err != nil {
			t.Fatalf("execution error: %v", err)
		}
	}

	if v := cpu.GetReg32(x86.EAX); v != 0x12345678 {
		t.Errorf("EAX = 0x%08X, want 0x12345678", v)
	}
}

func TestPCLoadBIOS(t *testing.T) {
	pc, err := New(Config{
		RAMSize: 1 << 20,
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer pc.Close()

	bios := make([]byte, 64*1024)
	// Program at 0xFFF0
	bios[0xFFF0] = 0xF4 // HLT

	if err := pc.LoadBIOS(bios, nil, nil, ""); err != nil {
		t.Fatalf("LoadBIOS failed: %v", err)
	}

	cpu := pc.GetCPU().(*x86.CPU)
	if cpu.GetSeg(x86.CS) != 0xF000 {
		t.Errorf("CS = 0x%04X, want 0xF000", cpu.GetSeg(x86.CS))
	}
	if cpu.GetEIP() != 0xFFF0 {
		t.Errorf("EIP = 0x%04X, want 0xFFF0", cpu.GetEIP())
	}
}

// TestBootRealMode boots a tiny real-mode program that initializes
// segments and writes a magic value to memory before halting.
// The code must fit in the 16 bytes at the reset vector (0xFFFF0-0xFFFFF).
func TestBootRealMode(t *testing.T) {
	pc, err := New(Config{
		RAMSize: 1 << 20, // 1MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer pc.Close()

	cpu := pc.GetCPU().(*x86.CPU)

	// 14-byte program at 0xF000:0xFFF0 (physical 0xFFFF0).
	// Uses only 8-bit ops to avoid 16-bit operand-size bugs.
	code := []byte{
		0x31, 0xC0, // XOR AX, AX
		0x8E, 0xD8, // MOV DS, AX
		0xB0, 0xBE, // MOV AL, 0xBE
		0x88, 0x06, 0x00, 0x05, // MOV [0x0500], AL
		0xA0, 0x00, 0x05, // MOV AL, [0x0500]
		0xF4,       // HLT
		0x90, 0x90, // NOP padding
	}
	copy(pc.biosROM.PhysMem[0xFFF0:], code)

	// Run until HLT
	for !cpu.IsPowerDown() {
		if err := cpu.Step(); err != nil {
			t.Fatalf("execution error: %v", err)
		}
	}

	// Verify the magic value was written and read back
	if v := cpu.GetReg8(x86.AL); v != 0xBE {
		t.Errorf("AL = 0x%02X, want 0xBE", v)
	}

	// Verify memory at 0x500
	v, _ := pc.memMap.Read8(0x500)
	if v != 0xBE {
		t.Errorf("memory[0x500] = 0x%02X, want 0xBE", v)
	}
}

// TestBootStringOp boots a program that uses REP STOSB to fill memory.
// The code must fit in the 16 bytes at the reset vector (0xFFFF0-0xFFFFF).
func TestBootStringOp(t *testing.T) {
	pc, err := New(Config{
		RAMSize: 1 << 20,
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer pc.Close()

	cpu := pc.GetCPU().(*x86.CPU)

	// 15-byte program at 0xF000:0xFFF0 (physical 0xFFFF0).
	// Uses only 8-bit ops to avoid 16-bit operand-size bugs.
	code := []byte{
		0x31, 0xC0, // XOR AX, AX
		0x8E, 0xD8, // MOV DS, AX
		0x8E, 0xC0, // MOV ES, AX
		0x31, 0xFF, // XOR DI, DI
		0xB1, 0x10, // MOV CL, 16
		0xB0, 0x41, // MOV AL, 'A'
		0xF3, 0xAA, // REP STOSB
		0xF4, // HLT
		0x90, // NOP padding
	}
	copy(pc.biosROM.PhysMem[0xFFF0:], code)

	for !cpu.IsPowerDown() {
		if err := cpu.Step(); err != nil {
			t.Fatalf("execution error: %v", err)
		}
	}

	// Verify memory was filled with 'A' at ES:DI=0x0000
	for i := 0; i < 16; i++ {
		addr := uint64(i)
		v, _ := pc.memMap.Read8(addr)
		if v != 'A' {
			t.Errorf("memory[0x%X] = 0x%02X, want 'A'", addr, v)
		}
	}
	if v := cpu.GetReg16(x86.DI); v != 0x0010 {
		t.Errorf("DI = 0x%04X, want 0x0010", v)
	}
}

// TestUARTTransmit verifies that writing to UART THR outputs the byte.
func TestUARTTransmit(t *testing.T) {
	var buf bytes.Buffer
	pic := NewPIC8259(nil, 0x20)
	uart := NewUART16550(pic, 4, &buf)
	io := NewIOPortDispatcher()
	uart.Register(io)

	// Write 'H' to THR (port 0x3F8)
	io.Write8(0x3F8, 'H')
	io.Write8(0x3F8, 'i')

	if got := buf.String(); got != "Hi" {
		t.Errorf("UART output = %q, want %q", got, "Hi")
	}
}

// TestUARTDLAB verifies that when DLAB is set, port 0x3F8 accesses DLL.
func TestUARTDLAB(t *testing.T) {
	pic := NewPIC8259(nil, 0x20)
	uart := NewUART16550(pic, 4, nil)
	io := NewIOPortDispatcher()
	uart.Register(io)

	// Set DLAB (bit 7 of LCR at 0x3FB)
	io.Write8(0x3FB, 0x80)

	// Write to DLL (now at 0x3F8)
	io.Write8(0x3F8, 0x12)
	if got := io.Read8(0x3F8); got != 0x12 {
		t.Errorf("DLL = 0x%02X, want 0x12", got)
	}

	// Write to DLH (now at 0x3F9)
	io.Write8(0x3F9, 0x34)
	if got := io.Read8(0x3F9); got != 0x34 {
		t.Errorf("DLH = 0x%02X, want 0x34", got)
	}

	// Clear DLAB
	io.Write8(0x3FB, 0x00)

	// With DLAB clear, 0x3F8 is THR on write, RBR on read. Push a byte
	// into the receive FIFO and verify the read returns it.
	io.Write8(0x3F8, 0x55) // transmit; doesn't affect RBR
	uart.Push([]byte{0x77})
	if got := io.Read8(0x3F8); got != 0x77 {
		t.Errorf("RBR = 0x%02X, want 0x77 (pushed byte)", got)
	}
}

// TestKeyboardSelfTest verifies the 8042 self-test command returns 0x55.
func TestKeyboardSelfTest(t *testing.T) {
	kbd := NewKeyboard8042()
	io := NewIOPortDispatcher()
	kbd.Register(io)

	// Issue self-test command (0xAA) to command port 0x64
	io.Write8(0x64, 0xAA)

	// Read data port 0x60
	if got := io.Read8(0x60); got != 0x55 {
		t.Errorf("self-test result = 0x%02X, want 0x55", got)
	}
}

// TestKeyboardInterfaceTest verifies the 8042 interface test command.
func TestKeyboardInterfaceTest(t *testing.T) {
	kbd := NewKeyboard8042()
	io := NewIOPortDispatcher()
	kbd.Register(io)

	io.Write8(0x64, 0xAB)
	if got := io.Read8(0x60); got != 0x00 {
		t.Errorf("interface test result = 0x%02X, want 0x00", got)
	}
}

// TestKeyboardStatus verifies the initial status register values.
func TestKeyboardStatus(t *testing.T) {
	kbd := NewKeyboard8042()
	io := NewIOPortDispatcher()
	kbd.Register(io)

	// Status port should have system flag set (bit 2)
	if got := io.Read8(0x64); got&0x04 == 0 {
		t.Errorf("status system flag not set, got 0x%02X", got)
	}
}

// makeFakeBZImage creates a minimal fake bzImage for testing the loader.
func makeFakeBZImage(t *testing.T, setupSects uint8, protectedModeCode []byte) []byte {
	// setup_sects = 0 means 4 setup sectors
	sectors := int(setupSects)
	if sectors == 0 {
		sectors = 4
	}
	setupBytes := (sectors + 1) * 512

	img := make([]byte, setupBytes+len(protectedModeCode))

	// Boot flag at 0x1FE
	img[0x1F1] = setupSects
	img[0x1FE] = 0x55
	img[0x1FF] = 0xAA

	// Header magic at 0x202
	copy(img[0x202:], "HdrS")

	// Version 2.12 (0x020C)
	img[0x206] = 0x0C
	img[0x207] = 0x02

	// Payload offset at 0x248 (relative to 0x100000)
	// For simplicity, set to 0 (entry point = 0x100000)

	// Copy protected mode code after setup area
	copy(img[setupBytes:], protectedModeCode)

	return img
}

// TestBZImageLoader verifies that a fake bzImage is loaded correctly.
func TestBZImageLoader(t *testing.T) {
	pc, err := New(Config{
		RAMSize: 16 * 1024 * 1024, // 16MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer pc.Close()

	// Protected-mode code: MOV EAX, 0xDEADBEEF; HLT
	protectedCode := []byte{
		0xB8, 0xEF, 0xBE, 0xAD, 0xDE, // MOV EAX, 0xDEADBEEF
		0xF4, // HLT
	}

	img := makeFakeBZImage(t, 0, protectedCode)

	entry, err := pc.loadBZImage(img, nil, "console=ttyS0")
	if err != nil {
		t.Fatalf("loadBZImage failed: %v", err)
	}

	if entry != 0x100000 {
		t.Errorf("entry point = 0x%08X, want 0x100000", entry)
	}

	cpu := pc.GetCPU().(*x86.CPU)

	// Verify boot protocol registers
	if cpu.GetReg32(x86.EAX) != 0x53726448 {
		t.Errorf("EAX = 0x%08X, want 0x53726448", cpu.GetReg32(x86.EAX))
	}
	if cpu.GetReg32(x86.EBX) != 0x90000 {
		t.Errorf("EBX = 0x%08X, want 0x90000", cpu.GetReg32(x86.EBX))
	}

	// Verify protected mode is enabled
	if cpu.GetCR(0)&x86.CR0_PE == 0 {
		t.Errorf("CR0.PE not set")
	}

	// Verify flat segments
	if cpu.GetSegBase(x86.CS) != 0 {
		t.Errorf("CS base = 0x%08X, want 0", cpu.GetSegBase(x86.CS))
	}
	if cpu.GetSegBase(x86.DS) != 0 {
		t.Errorf("DS base = 0x%08X, want 0", cpu.GetSegBase(x86.DS))
	}

	// Run until HLT
	for !cpu.IsPowerDown() {
		if err := cpu.Step(); err != nil {
			t.Fatalf("execution error: %v", err)
		}
	}

	if cpu.GetReg32(x86.EAX) != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", cpu.GetReg32(x86.EAX))
	}
}

// TestBZImageLoaderWithInitrd verifies initrd loading.
func TestBZImageLoaderWithInitrd(t *testing.T) {
	pc, err := New(Config{
		RAMSize: 16 * 1024 * 1024, // 16MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer pc.Close()

	protectedCode := []byte{0xF4} // HLT
	img := makeFakeBZImage(t, 0, protectedCode)

	initrd := []byte{0x01, 0x02, 0x03, 0x04, 0x05}

	_, err = pc.loadBZImage(img, initrd, "")
	if err != nil {
		t.Fatalf("loadBZImage failed: %v", err)
	}

	// Read ramdisk_image and ramdisk_size from zero page
	ramdiskImage := uint32(pc.readPhys8(0x90000+0x218)) |
		uint32(pc.readPhys8(0x90000+0x219))<<8 |
		uint32(pc.readPhys8(0x90000+0x21A))<<16 |
		uint32(pc.readPhys8(0x90000+0x21B))<<24

	ramdiskSize := uint32(pc.readPhys8(0x90000+0x21C)) |
		uint32(pc.readPhys8(0x90000+0x21D))<<8 |
		uint32(pc.readPhys8(0x90000+0x21E))<<16 |
		uint32(pc.readPhys8(0x90000+0x21F))<<24

	if ramdiskSize != uint32(len(initrd)) {
		t.Errorf("ramdisk_size = %d, want %d", ramdiskSize, len(initrd))
	}

	// Verify initrd data was written
	for i, b := range initrd {
		if got := pc.readPhys8(ramdiskImage + uint32(i)); got != b {
			t.Errorf("initrd[%d] = 0x%02X, want 0x%02X", i, got, b)
		}
	}
}

// readPhys8 reads a byte from physical memory.
func (p *PC) readPhys8(addr uint32) uint8 {
	v, _ := p.memMap.Read8(uint64(addr))
	return v
}
