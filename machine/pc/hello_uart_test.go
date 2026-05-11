package pc

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// buildHelloUARTELF builds a minimal 32-bit ELF that writes a fixed greeting
// to COM1 (port 0x3F8) and halts. Loaded via the vmlinux ELF path which sets
// up flat 32-bit protected mode at the entry point.
func buildHelloUARTELF(loadAddr uint32, msg string) []byte {
	// Assemble: for each byte in msg, mov al, byte; out 0x3F8, al; then hlt.
	// outb/inb to fixed port 0x3F8 needs DX since 0x3F8 > 0xFF.
	var code []byte
	// mov dx, 0x3F8 = 66 BA F8 03 (16-bit) — but we're in 32-bit mode, so use:
	//   mov edx, 0x3F8  =  BA F8 03 00 00 (5 bytes)
	code = append(code, 0xBA, 0xF8, 0x03, 0x00, 0x00)
	for i := 0; i < len(msg); i++ {
		// mov al, imm8 = B0 imm
		code = append(code, 0xB0, msg[i])
		// out dx, al = EE
		code = append(code, 0xEE)
	}
	// hlt
	code = append(code, 0xF4)

	const elfHeaderSize = 52
	const phdrSize = 32
	totalSize := elfHeaderSize + phdrSize + len(code)
	b := make([]byte, totalSize)

	// ELF header
	copy(b[:4], []byte{0x7F, 'E', 'L', 'F'})
	b[4] = 1 // 32-bit
	b[5] = 1 // little-endian
	b[6] = 1 // version
	binary.LittleEndian.PutUint16(b[16:], 2) // ET_EXEC
	binary.LittleEndian.PutUint16(b[18:], 3) // EM_386
	binary.LittleEndian.PutUint32(b[20:], 1) // version
	binary.LittleEndian.PutUint32(b[24:], loadAddr)
	binary.LittleEndian.PutUint32(b[28:], elfHeaderSize)
	binary.LittleEndian.PutUint16(b[40:], elfHeaderSize)
	binary.LittleEndian.PutUint16(b[42:], phdrSize)
	binary.LittleEndian.PutUint16(b[44:], 1) // 1 program header

	// Program header
	binary.LittleEndian.PutUint32(b[elfHeaderSize+0:], 1)                          // PT_LOAD
	binary.LittleEndian.PutUint32(b[elfHeaderSize+4:], elfHeaderSize+phdrSize)     // file offset
	binary.LittleEndian.PutUint32(b[elfHeaderSize+8:], loadAddr)                   // vaddr
	binary.LittleEndian.PutUint32(b[elfHeaderSize+12:], loadAddr)                  // paddr
	binary.LittleEndian.PutUint32(b[elfHeaderSize+16:], uint32(len(code)))         // filesz
	binary.LittleEndian.PutUint32(b[elfHeaderSize+20:], uint32(len(code)))         // memsz
	binary.LittleEndian.PutUint32(b[elfHeaderSize+24:], 5)                         // R+X
	binary.LittleEndian.PutUint32(b[elfHeaderSize+28:], 0x1000)                    // align

	// Code
	copy(b[elfHeaderSize+phdrSize:], code)
	return b
}

// TestHelloUART loads a tiny "Hello, x86 emulator!" mini-kernel via the
// vmlinux ELF path and verifies the bytes reach the COM1 writer. This
// validates the emulator's TX path end-to-end, independent of any Linux
// kernel idiosyncrasies.
func TestHelloUART(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("new PC: %v", err)
	}
	defer p.Close()

	var buf bytes.Buffer
	p.uart.SetOutput(&buf)

	const want = "Hello, x86 emulator!\n"
	elfData := buildHelloUARTELF(0x100000, want)
	if _, err := p.loadVMLinux(elfData, nil, ""); err != nil {
		t.Fatalf("loadVMLinux: %v", err)
	}

	// Run until HLT or 1s wall time.
	cpu := p.GetCPU()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !cpu.IsPowerDown() {
		if err := cpu.Run(10_000); err != nil {
			t.Fatalf("cpu.Run: %v", err)
		}
	}
	if !cpu.IsPowerDown() {
		t.Fatalf("CPU did not halt; UART so far: %q", buf.String())
	}
	got := buf.String()
	if !strings.Contains(got, want) {
		t.Errorf("UART output %q does not contain %q", got, want)
	}
}
