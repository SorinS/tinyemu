package pc

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
)

// buildMinimalELF32 creates a minimal 32-bit ELF executable with one PT_LOAD segment.
// The code is placed at physical/virtual address loadAddr and consists of mov eax, imm32; hlt.
// If highMap is true, Vaddr != Paddr to simulate a high-mapped kernel.
func buildMinimalELF32(t *testing.T, loadAddr uint32, highMap bool) []byte {
	t.Helper()

	code := []byte{
		0xB8, 0xEF, 0xBE, 0xAD, 0xDE, // MOV EAX, 0xDEADBEEF
		0xF4,                         // HLT
	}

	vaddr := loadAddr
	if highMap {
		vaddr = loadAddr + 0xC0000000
	}

	elfHeaderSize := 52
	phdrSize := 32
	totalSize := elfHeaderSize + phdrSize + len(code)

	b := make([]byte, totalSize)

	// ELF header
	b[0] = 0x7F
	b[1] = 'E'
	b[2] = 'L'
	b[3] = 'F'
	b[4] = 1  // 32-bit
	b[5] = 1  // Little endian
	b[6] = 1  // ELF version
	b[7] = 0  // OS/ABI
	b[16] = 2 // Type = ET_EXEC
	b[17] = 0
	binary.LittleEndian.PutUint16(b[18:], 3) // Machine = EM_386
	binary.LittleEndian.PutUint32(b[20:], 1) // Version
	binary.LittleEndian.PutUint32(b[24:], loadAddr)                      // e_entry = physical address
	binary.LittleEndian.PutUint32(b[28:], uint32(elfHeaderSize))         // phoff = 52
	binary.LittleEndian.PutUint32(b[32:], 0)                             // shoff
	binary.LittleEndian.PutUint32(b[36:], 0)                             // flags
	binary.LittleEndian.PutUint16(b[40:], uint16(elfHeaderSize))        // ehsize
	binary.LittleEndian.PutUint16(b[42:], uint16(phdrSize))             // phentsize
	binary.LittleEndian.PutUint16(b[44:], 1)                            // phnum
	binary.LittleEndian.PutUint16(b[46:], 40)                           // shentsize
	binary.LittleEndian.PutUint16(b[48:], 0)                            // shnum
	binary.LittleEndian.PutUint16(b[50:], 0)                            // shstrndx

	// Program header (at offset 52)
	phoff := elfHeaderSize
	binary.LittleEndian.PutUint32(b[phoff+0:], 1)                    // Type = PT_LOAD
	binary.LittleEndian.PutUint32(b[phoff+4:], uint32(elfHeaderSize+phdrSize)) // Offset
	binary.LittleEndian.PutUint32(b[phoff+8:], vaddr)                // Vaddr
	binary.LittleEndian.PutUint32(b[phoff+12:], loadAddr)            // Paddr
	binary.LittleEndian.PutUint32(b[phoff+16:], uint32(len(code)))   // Filesz
	binary.LittleEndian.PutUint32(b[phoff+20:], uint32(len(code)))   // Memsz
	binary.LittleEndian.PutUint32(b[phoff+24:], 5)                   // Flags = R+X
	binary.LittleEndian.PutUint32(b[phoff+28:], 0x1000)              // Align

	// Code after headers
	copy(b[elfHeaderSize+phdrSize:], code)

	return b
}

// TestLoadVMLinuxFlat verifies loading a flat-mapped vmlinux (Vaddr == Paddr).
func TestLoadVMLinuxFlat(t *testing.T) {
	p, err := New(Config{RAMSize: 16 * 1024 * 1024})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	loadAddr := uint32(0x100000)
	elfData := buildMinimalELF32(t, loadAddr, false)

	entry, err := p.loadVMLinux(elfData, nil, "console=ttyS0")
	if err != nil {
		t.Fatalf("loadVMLinux failed: %v", err)
	}

	if entry != loadAddr {
		t.Errorf("entry point = 0x%08X, want 0x%08X", entry, loadAddr)
	}

	cpu := p.GetCPU().(*x86.CPU)

	// Verify code was loaded
	if v, _ := p.memMap.Read8(uint64(loadAddr)); v != 0xB8 {
		t.Errorf("code[0] = 0x%02X, want 0xB8", v)
	}

	// Verify CPU state: paging OFF (kernel will enable it itself)
	if cpu.GetCR(0)&x86.CR0_PG != 0 {
		t.Errorf("CR0.PG should not be set")
	}
	if cpu.GetCR(0)&x86.CR0_PE == 0 {
		t.Errorf("CR0.PE not set")
	}
	if cpu.GetEIP() != loadAddr {
		t.Errorf("EIP = 0x%08X, want 0x%08X", cpu.GetEIP(), loadAddr)
	}
	if cpu.GetReg32(x86.EBX) != 0x90000 {
		t.Errorf("EBX = 0x%08X, want 0x90000", cpu.GetReg32(x86.EBX))
	}
	if cpu.GetReg32(x86.EAX) != 0x53726448 {
		t.Errorf("EAX = 0x%08X, want 0x53726448", cpu.GetReg32(x86.EAX))
	}

	// Verify flat segments
	if cpu.GetSegBase(x86.CS) != 0 {
		t.Errorf("CS base = 0x%08X, want 0", cpu.GetSegBase(x86.CS))
	}
	if cpu.GetSegBase(x86.DS) != 0 {
		t.Errorf("DS base = 0x%08X, want 0", cpu.GetSegBase(x86.DS))
	}

	// Run until HLT
	for i := 0; i < 100 && !cpu.IsPowerDown(); i++ {
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if !cpu.IsPowerDown() {
		t.Errorf("CPU did not halt")
	}
	if cpu.GetReg32(x86.EAX) != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", cpu.GetReg32(x86.EAX))
	}
}

// TestLoadVMLinuxHighMap verifies loading a high-mapped vmlinux (Vaddr != Paddr).
func TestLoadVMLinuxHighMap(t *testing.T) {
	p, err := New(Config{RAMSize: 128 * 1024 * 1024})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	loadAddr := uint32(0x1000000)
	elfData := buildMinimalELF32(t, loadAddr, true)

	entry, err := p.loadVMLinux(elfData, nil, "")
	if err != nil {
		t.Fatalf("loadVMLinux failed: %v", err)
	}

	virtEntry := loadAddr + 0xC0000000
	if entry != virtEntry {
		t.Errorf("entry point = 0x%08X, want 0x%08X", entry, virtEntry)
	}

	cpu := p.GetCPU().(*x86.CPU)

	// EIP should be physical entry point (kernel enables paging itself)
	if cpu.GetEIP() != loadAddr {
		t.Errorf("EIP = 0x%08X, want 0x%08X", cpu.GetEIP(), loadAddr)
	}

	// Verify paging is NOT enabled (kernel sets it up itself)
	if cpu.GetCR(0)&x86.CR0_PG != 0 {
		t.Errorf("CR0.PG should not be set")
	}

	// Run until HLT to verify the kernel code executes at physical address
	for i := 0; i < 100 && !cpu.IsPowerDown(); i++ {
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if !cpu.IsPowerDown() {
		t.Errorf("CPU did not halt")
	}
	if cpu.GetReg32(x86.EAX) != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", cpu.GetReg32(x86.EAX))
	}
}

// TestLoadVMLinuxBootParams verifies boot_params contents.
func TestLoadVMLinuxBootParams(t *testing.T) {
	p, err := New(Config{RAMSize: 32 * 1024 * 1024})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	elfData := buildMinimalELF32(t, 0x100000, false)
	if _, err := p.loadVMLinux(elfData, nil, "console=ttyS0,115200"); err != nil {
		t.Fatalf("loadVMLinux failed: %v", err)
	}

	addr := uint32(0x90000)

	// boot_flag
	if v := p.readPhys16(addr + 0x1FE); v != 0xAA55 {
		t.Errorf("boot_flag = 0x%04X, want 0xAA55", v)
	}
	// header magic
	if v := p.readPhys32(addr + 0x202); v != 0x53726448 {
		t.Errorf("header = 0x%08X, want 0x53726448", v)
	}
	// version
	if v := p.readPhys16(addr + 0x206); v != 0x020F {
		t.Errorf("version = 0x%04X, want 0x020F", v)
	}
	// type_of_loader
	if v, _ := p.memMap.Read8(uint64(addr + 0x210)); v != 0xFF {
		t.Errorf("type_of_loader = 0x%02X, want 0xFF", v)
	}
	// loadflags: LOADED_HIGH | KEEP_SEGMENTS
	if v, _ := p.memMap.Read8(uint64(addr + 0x211)); v != 0x21 {
		t.Errorf("loadflags = 0x%02X, want 0x21", v)
	}
	// cmd_line_ptr
	cmdLinePtr := p.readPhys32(addr + 0x228)
	if cmdLinePtr != 0x91000 {
		t.Errorf("cmd_line_ptr = 0x%08X, want 0x91000", cmdLinePtr)
	}

	// Verify command line string
	var cmdLineBuf bytes.Buffer
	for i := uint32(0); i < 256; i++ {
		b, _ := p.memMap.Read8(uint64(cmdLinePtr + i))
		if b == 0 {
			break
		}
		cmdLineBuf.WriteByte(b)
	}
	if cmdLineBuf.String() != "console=ttyS0,115200" {
		t.Errorf("cmdline = %q, want %q", cmdLineBuf.String(), "console=ttyS0,115200")
	}
}

// TestLoadVMLinuxInitrd verifies initrd loading.
func TestLoadVMLinuxInitrd(t *testing.T) {
	p, err := New(Config{RAMSize: 32 * 1024 * 1024})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	elfData := buildMinimalELF32(t, 0x100000, false)
	initrdData := []byte("INITRD_TEST_DATA")

	if _, err := p.loadVMLinux(elfData, initrdData, ""); err != nil {
		t.Fatalf("loadVMLinux failed: %v", err)
	}

	addr := uint32(0x90000)
	ramdiskImage := p.readPhys32(addr + 0x218)
	ramdiskSize := p.readPhys32(addr + 0x21C)

	if ramdiskSize != uint32(len(initrdData)) {
		t.Errorf("ramdisk_size = %d, want %d", ramdiskSize, len(initrdData))
	}

	for i, expected := range initrdData {
		actual, _ := p.memMap.Read8(uint64(ramdiskImage + uint32(i)))
		if actual != expected {
			t.Errorf("initrd[%d] = 0x%02X, want 0x%02X", i, actual, expected)
		}
	}
}

// TestLoadVMLinuxE820 verifies the e820 memory map.
func TestLoadVMLinuxE820(t *testing.T) {
	p, err := New(Config{RAMSize: 32 * 1024 * 1024})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	elfData := buildMinimalELF32(t, 0x100000, false)
	if _, err := p.loadVMLinux(elfData, nil, ""); err != nil {
		t.Fatalf("loadVMLinux failed: %v", err)
	}

	addr := uint32(0x90000)
	e820Entries, _ := p.memMap.Read8(uint64(addr + 0x1E8))
	if e820Entries != 3 {
		t.Errorf("e820_entries = %d, want 3", e820Entries)
	}

	const e820Off = 0x2D0

	// Entry 0: low RAM
	base0 := p.readPhys64(addr + e820Off)
	size0 := p.readPhys64(addr + e820Off + 8)
	typ0 := p.readPhys32(addr + e820Off + 16)
	if base0 != 0 || size0 != 0xA0000 || typ0 != 1 {
		t.Errorf("e820[0] = {0x%08X, 0x%08X, %d}, want {0, 0xA0000, 1}", base0, size0, typ0)
	}

	// Entry 1: reserved
	base1 := p.readPhys64(addr + e820Off + 20)
	size1 := p.readPhys64(addr + e820Off + 28)
	typ1 := p.readPhys32(addr + e820Off + 36)
	if base1 != 0xA0000 || size1 != 0x60000 || typ1 != 2 {
		t.Errorf("e820[1] = {0x%08X, 0x%08X, %d}, want {0xA0000, 0x60000, 2}", base1, size1, typ1)
	}

	// Entry 2: extended RAM
	base2 := p.readPhys64(addr + e820Off + 40)
	size2 := p.readPhys64(addr + e820Off + 48)
	typ2 := p.readPhys32(addr + e820Off + 56)
	if base2 != 0x100000 || size2 != 32*1024*1024-0x100000 || typ2 != 1 {
		t.Errorf("e820[2] = {0x%08X, 0x%08X, %d}, want {0x100000, 0x%08X, 1}", base2, size2, typ2, 32*1024*1024-0x100000)
	}
}

// readPhys16 reads a 16-bit little-endian value from physical memory.
func (p *PC) readPhys16(addr uint32) uint16 {
	v0, _ := p.memMap.Read8(uint64(addr))
	v1, _ := p.memMap.Read8(uint64(addr + 1))
	return uint16(v0) | uint16(v1)<<8
}

// readPhys64 reads a 64-bit little-endian value from physical memory.
func (p *PC) readPhys64(addr uint32) uint64 {
	var val uint64
	for i := uint32(0); i < 8; i++ {
		v, _ := p.memMap.Read8(uint64(addr + i))
		val |= uint64(v) << (i * 8)
	}
	return val
}
