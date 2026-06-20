package machine

import (
	"encoding/binary"
	"testing"
)

// Compile-time interface checks.
var (
	_ Board              = (*ARM64Machine)(nil)
	_ VirtioMMIOAttacher = (*ARM64Machine)(nil)
)

func TestARM64BoardBoot(t *testing.T) {
	m, err := NewARM64(Config{RAMSize: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// A tiny fake arm64 Linux "kernel": one WFI word, with the Image magic
	// ("ARMd") at offset 56 so it's classified as a Linux Image (not firmware).
	image := make([]byte, 64)
	binary.LittleEndian.PutUint32(image, 0xD503207F)          // WFI at entry
	binary.LittleEndian.PutUint32(image[56:], linuxImageMagic) // "ARMd"
	if err := m.LoadBIOS(image, nil, nil, "console=ttyAMA0"); err != nil {
		t.Fatal(err)
	}
	if m.cpu.PC != a64RAMBase+a64KernelOff {
		t.Errorf("PC=%#x, want %#x", m.cpu.PC, a64RAMBase+a64KernelOff)
	}
	if m.cpu.X[0] != a64RAMBase+a64DTBOff {
		t.Errorf("x0 (DTB)=%#x, want %#x", m.cpu.X[0], a64RAMBase+a64DTBOff)
	}

	// The DTB at RAM base must have a valid FDT header.
	dtb := m.ram.PhysMem[a64DTBOff:]
	if magic := binary.BigEndian.Uint32(dtb); magic != 0xd00dfeed {
		t.Fatalf("FDT magic=%#x, want 0xd00dfeed", magic)
	}
	totalSize := binary.BigEndian.Uint32(dtb[4:])
	if totalSize < 0x100 || totalSize > 0x10000 {
		t.Errorf("FDT totalsize=%d looks wrong", totalSize)
	}

	// Run the fake kernel: it should execute WFI and park.
	if err := m.Run(10); err != nil {
		t.Fatal(err)
	}
	if !m.cpu.IsPowerDown() {
		t.Error("kernel WFI did not power down the core")
	}
}

func TestARM64PSCISystemOff(t *testing.T) {
	m, err := NewARM64(Config{RAMSize: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.cpu.X[0] = 0x84000008 // SYSTEM_OFF
	if !m.psci(m.cpu) {
		t.Fatal("psci did not handle SYSTEM_OFF")
	}
	if !m.IsShutdownRequested() {
		t.Error("SYSTEM_OFF did not request shutdown")
	}
}
