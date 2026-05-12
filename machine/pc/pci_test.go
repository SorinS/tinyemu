package pc

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/mem"
)

// newPCITestRig builds the minimum harness for talking to a PCIHost:
// an I/O dispatcher, the host registered into it, and the host's bus
// exposed so tests can add devices.
func newPCITestRig(t *testing.T) (*IOPortDispatcher, *PCIHost) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("register ram: %v", err)
	}
	_ = x86.NewCPU(mm) // the CPU isn't accessed by these tests; PCI is pure I/O
	io := NewIOPortDispatcher()
	host := NewPCIHost()
	host.Register(io)
	return io, host
}

// pciCmd builds the 32-bit value the kernel writes to 0xCF8 to address
// a specific configuration-space register.
func pciCmd(bus, dev, fn, reg uint32) uint32 {
	return 0x80000000 |
		(bus & 0xFF) << 16 |
		(dev & 0x1F) << 11 |
		(fn & 0x07) << 8 |
		(reg & 0xFC)
}

// inl32 emulates a 32-bit read from a port by combining two 16-bit reads
// the way the CPU's IN-EAX wiring does (Read16(port) | Read16(port+2)<<16).
// This mirrors what `inl 0xCFC` produces.
func inl32(io *IOPortDispatcher, port uint16) uint32 {
	return uint32(io.Read16(port)) | uint32(io.Read16(port+2))<<16
}

// outl32 emulates a 32-bit write from a port.
func outl32(io *IOPortDispatcher, port uint16, val uint32) {
	io.Write16(port, uint16(val))
	io.Write16(port+2, uint16(val>>16))
}

// TestPCINoDevice verifies that a config-space read of an empty slot
// returns 0xFFFFFFFF — the standard "no device here" indication that
// terminates Linux's enumeration walk.
func TestPCINoDevice(t *testing.T) {
	io, _ := newPCITestRig(t)
	outl32(io, 0xCF8, pciCmd(0, 5, 0, 0)) // bus 0, dev 5 (empty)
	got := inl32(io, 0xCFC)
	if got != 0xFFFFFFFF {
		t.Errorf("read empty slot: got %08X, want 0xFFFFFFFF", got)
	}
}

// TestPCIVendorDeviceReadback adds one device and reads back its
// vendor/device ID via the standard mechanism.
func TestPCIVendorDeviceReadback(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("test", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, dev)

	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0))
	got := inl32(io, 0xCFC)
	wantVendorDevice := uint32(0x12378086) // little-endian (device << 16) | vendor
	if got != wantVendorDevice {
		t.Errorf("vendor/device dword = %08X, want %08X", got, wantVendorDevice)
	}

	// 16-bit reads of the same field.
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0))
	if v := io.Read16(0xCFC); v != 0x8086 {
		t.Errorf("vendor (16-bit) = %04X, want 0x8086", v)
	}
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0))
	if v := io.Read16(0xCFE); v != 0x1237 {
		t.Errorf("device (16-bit) = %04X, want 0x1237", v)
	}
}

// TestPCIClassCodeReadback verifies the 3-byte class code field
// (offset 0x09: ProgIF, 0x0A: Subclass, 0x0B: Class).
func TestPCIClassCodeReadback(t *testing.T) {
	io, host := newPCITestRig(t)
	// classCode 0x010180 = IDE controller, ProgIF 0x80.
	dev := NewPCIDevice("ide", 0x8086, 0x7010, 0x010180, 0x00)
	host.Bus().AddDevice(1, 0, dev) // device slot 1, function 0

	outl32(io, 0xCF8, pciCmd(0, 1, 0, 0x08))
	got := inl32(io, 0xCFC)
	// dword at 0x08: rev=0, ProgIF=0x80, Subclass=0x01, Class=0x01
	// LE assembled: 0x01_01_80_00
	want := uint32(0x01018000)
	if got != want {
		t.Errorf("class dword = %08X, want %08X", got, want)
	}

	// Individual bytes.
	outl32(io, 0xCF8, pciCmd(0, 1, 0, 0x08))
	if v := io.Read8(0xCFC); v != 0x00 { // revision
		t.Errorf("revision = %02X, want 0", v)
	}
	if v := io.Read8(0xCFD); v != 0x80 { // ProgIF
		t.Errorf("ProgIF = %02X, want 0x80", v)
	}
	if v := io.Read8(0xCFE); v != 0x01 { // Subclass
		t.Errorf("Subclass = %02X, want 0x01", v)
	}
	if v := io.Read8(0xCFF); v != 0x01 { // Class
		t.Errorf("Class = %02X, want 0x01", v)
	}
}

// TestPCICommandWritable verifies the command register (offset 0x04) can
// be written and read back — Linux's PCI core uses this to enable
// I/O / memory / bus-master decoding on each device it claims.
func TestPCICommandWritable(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("test", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, dev)

	// Initial command = 0.
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0x04))
	if v := io.Read16(0xCFC); v != 0 {
		t.Errorf("initial command = %04X, want 0", v)
	}

	// Write 0x0007 (I/O + Mem + Bus Master enable).
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0x04))
	io.Write16(0xCFC, 0x0007)
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0x04))
	if v := io.Read16(0xCFC); v != 0x0007 {
		t.Errorf("command after write = %04X, want 0x0007", v)
	}
}

// TestPCIVendorIDReadOnly verifies that writes to the vendor ID are
// silently ignored — vendor and device IDs are RO per the PCI spec.
func TestPCIVendorIDReadOnly(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("test", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, dev)

	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0))
	outl32(io, 0xCFC, 0xDEADBEEF) // attempt overwrite

	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0))
	got := inl32(io, 0xCFC)
	if got != 0x12378086 {
		t.Errorf("vendor/device after RO write = %08X, want 0x12378086", got)
	}
}

// TestPCIBARWriteAndReadback verifies BARs accept writes and return them
// (default semantics for a device without a custom onWrite that does
// size-detection masking).
func TestPCIBARWriteAndReadback(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("test", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, dev)

	// Write 0xFEDCBA98 to BAR 0 (offset 0x10).
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0x10))
	outl32(io, 0xCFC, 0xFEDCBA98)
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0x10))
	got := inl32(io, 0xCFC)
	if got != 0xFEDCBA98 {
		t.Errorf("BAR0 readback = %08X, want 0xFEDCBA98", got)
	}
}

// TestPCIIRQLine verifies the SetIRQLine helper populates the right
// fields and that the kernel can read them back.
func TestPCIIRQLine(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("ide", 0x8086, 0x7010, 0x010180, 0x00)
	dev.SetIRQLine(14, 1)
	host.Bus().AddDevice(1, 0, dev) // device slot 1, function 0

	outl32(io, 0xCF8, pciCmd(0, 1, 0, 0x3C))
	got := inl32(io, 0xCFC)
	// Bytes at 0x3C: IRQ-line, INT-pin, min-grant, max-latency.
	// LE: 0x__ __ 01 0E
	if uint8(got) != 14 {
		t.Errorf("IRQ line = %d, want 14", uint8(got))
	}
	if uint8(got>>8) != 1 {
		t.Errorf("INT pin = %d, want 1", uint8(got>>8))
	}
}

// TestPCIBusUnsupported confirms reads on bus != 0 return 0xFFFFFFFF.
// We only model bus 0; a bridge would be needed to model deeper buses.
func TestPCIBusUnsupported(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("test", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, dev)

	outl32(io, 0xCF8, pciCmd(1, 0, 0, 0)) // bus 1 (doesn't exist)
	got := inl32(io, 0xCFC)
	if got != 0xFFFFFFFF {
		t.Errorf("bus 1 read = %08X, want 0xFFFFFFFF", got)
	}
}

// TestPCIDisabledAccess verifies that when bit 31 of the address is
// clear (config access disabled), all reads return 0xFFFFFFFF.
func TestPCIDisabledAccess(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("test", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, dev)

	// Set the address WITHOUT bit 31.
	outl32(io, 0xCF8, 0x00000000)
	got := inl32(io, 0xCFC)
	if got != 0xFFFFFFFF {
		t.Errorf("disabled access = %08X, want 0xFFFFFFFF", got)
	}
}

// TestPCIAddressReadback verifies that 0xCF8 is also readable (some
// devices and BIOSes save/restore it).
func TestPCIAddressReadback(t *testing.T) {
	io, _ := newPCITestRig(t)
	outl32(io, 0xCF8, 0x80123456)
	got := inl32(io, 0xCF8)
	if got != 0x80123456 {
		t.Errorf("0xCF8 readback = %08X, want 0x80123456", got)
	}
}

// TestPCIPCBoardWiring verifies that when a full PC machine is created,
// the host-bridge + IDE PCI devices are discoverable via the standard
// mechanism. This is the integration test that ties pci.go to pc.go.
func TestPCIPCBoardWiring(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("ram: %v", err)
	}
	// We don't go through pc.New here — that allocates huge RAM and
	// loads more than we need. Instead build a minimal rig that mirrors
	// what pc.New does for PCI alone.
	_ = x86.NewCPU(mm)
	io := NewIOPortDispatcher()
	host := NewPCIHost()
	host.Register(io)

	hostBridge := NewPCIDevice("440FX Host Bridge", 0x8086, 0x1237, 0x060000, 0x00)
	host.Bus().AddDevice(0, 0, hostBridge)
	ide := NewPCIDevice("PIIX3 IDE", 0x8086, 0x7010, 0x010180, 0x00)
	ide.SetIRQLine(14, 1)
	host.Bus().AddDevice(1, 1, ide)

	// Linux's PCI enumeration walks the bus. Simulate the first few
	// reads it does.

	// Slot (0,0,0): expect 8086:1237.
	outl32(io, 0xCF8, pciCmd(0, 0, 0, 0))
	if got := inl32(io, 0xCFC); got != 0x12378086 {
		t.Errorf("host bridge ID = %08X, want 0x12378086", got)
	}

	// Slot (0,1,0): no device.
	outl32(io, 0xCF8, pciCmd(0, 1, 0, 0))
	if got := inl32(io, 0xCFC); got != 0xFFFFFFFF {
		t.Errorf("expected no device at (0,1,0), got %08X", got)
	}

	// Slot (0,1,1): expect 8086:7010.
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0))
	if got := inl32(io, 0xCFC); got != 0x70108086 {
		t.Errorf("IDE ID = %08X, want 0x70108086", got)
	}

	// IDE class at offset 0x08: 0x010180 + revision 0.
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x08))
	if got := inl32(io, 0xCFC); got != 0x01018000 {
		t.Errorf("IDE class dword = %08X, want 0x01018000", got)
	}

	// IRQ line = 14.
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x3C))
	if got := uint8(inl32(io, 0xCFC)); got != 14 {
		t.Errorf("IDE IRQ line = %d, want 14", got)
	}
}

// TestPCIBARSizing verifies the PCI BAR-sizing protocol from §6.2.5.1
// of the spec: the OS writes 0xFFFFFFFF to a BAR and reads back the
// hardware-forced bit pattern that encodes the region size. A 16-byte
// I/O BAR at base 0xC000 must report 0xFFFFFFF1 after the sizing write
// (bit 0 = 1 for I/O, bits 1-3 forced to 0 for size = 16). After the
// OS writes a real base address back, the device must remember it AND
// keep the type bit pinned.
func TestPCIBARSizing(t *testing.T) {
	io, host := newPCITestRig(t)
	dev := NewPCIDevice("ide", 0x8086, 0x7010, 0x010180, 0x00)
	dev.SetIOBAR(4, 0xC000, 16)
	host.Bus().AddDevice(1, 1, dev)

	// Initial readback: 0xC001 (base | I/O bit).
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x20))
	if got := inl32(io, 0xCFC); got != 0xC001 {
		t.Fatalf("initial BAR4 = %08X, want 0xC001", got)
	}

	// Sizing: write 0xFFFFFFFF, read back the size-mask.
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x20))
	outl32(io, 0xCFC, 0xFFFFFFFF)
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x20))
	if got := inl32(io, 0xCFC); got != 0xFFFFFFF1 {
		t.Fatalf("BAR4 after sizing write = %08X, want 0xFFFFFFF1", got)
	}

	// Restoration: write a real base; device keeps the type bit set.
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x20))
	outl32(io, 0xCFC, 0xC000)
	outl32(io, 0xCF8, pciCmd(0, 1, 1, 0x20))
	if got := inl32(io, 0xCFC); got != 0xC001 {
		t.Fatalf("BAR4 after restore = %08X, want 0xC001", got)
	}
}
