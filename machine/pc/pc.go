// Package pc implements an x86 PC machine/board emulator.
package pc

import (
	"errors"
	"fmt"
	"os"

	"github.com/jtolio/tinyemu-go/cpu"
	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/mem"
	"github.com/jtolio/tinyemu-go/virtio"
)

// Memory map constants for a standard PC.
const (
	LowRAMAddr      = 0x00000000
	LowRAMSize      = 0x000A0000 // 640KB conventional memory
	VGAApertureAddr = 0x000A0000
	VGAApertureSize = 0x00020000 // 128KB VGA memory
	BIOSShadowAddr  = 0x000C0000
	BIOSShadowSize  = 0x00040000 // 256KB
	BIOSROMAddr     = 0x000F0000
	BIOSROMSize     = 0x00010000 // 64KB
	HighBIOSAddr    = 0xFFF00000

	VirtIOBaseAddr = 0x00010000 // Fixed MMIO address for VirtIO devices
	VirtIOSize     = 0x00001000 // 4KB per device
	VirtIOIRQ      = 8          // First VirtIO IRQ (after PIC internal uses)
)

// Config holds configuration for creating a new PC.
type Config struct {
	RAMSize uint64 // RAM size in bytes
	Console *virtio.CharacterDevice
}

// PC represents a complete x86 PC machine.
type PC struct {
	memMap     *mem.PhysMemoryMap
	cpu        *x86.CPU
	io         *IOPortDispatcher
	pic        *PIC8259
	pit        *PIT8254
	rtc        *CMOSRTC
	uart       *UART16550
	kbd        *Keyboard8042
	ata        *ATAController
	pciHost    *PCIHost
	vga        *VGA
	ramSize    uint64
	lowRAM     *mem.PhysMemoryRange
	biosROM    *mem.PhysMemoryRange
	biosHigh   *mem.PhysMemoryRange
	console    *virtio.CharacterDevice
	consoleDev *virtio.Console

	virtioDevices []*virtio.Device
	virtioCount   int
	nextVirtIOIRQ int

	lastTickCycles uint64

	shutdownRequested bool
	shutdownExitCode  int
}

// New creates a new x86 PC machine.
func New(cfg Config) (*PC, error) {
	p := &PC{
		memMap:        mem.NewPhysMemoryMap(),
		ramSize:       cfg.RAMSize,
		io:            NewIOPortDispatcher(),
		console:       cfg.Console,
		nextVirtIOIRQ: VirtIOIRQ,
	}

	// Create CPU
	p.cpu = x86.NewCPU(p.memMap)

	// Register low RAM (640KB)
	var err error
	p.lowRAM, err = p.memMap.RegisterRAM(LowRAMAddr, LowRAMSize, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to register low RAM: %w", err)
	}

	// Register extended RAM above 1MB
	if cfg.RAMSize > LowRAMSize {
		extendedSize := cfg.RAMSize - LowRAMSize
		_, err = p.memMap.RegisterRAM(0x00100000, extendedSize, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to register extended RAM: %w", err)
		}
	}

	// Register BIOS ROM region (low and high alias)
	p.biosROM, err = p.memMap.RegisterRAM(BIOSROMAddr, BIOSROMSize, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to register BIOS ROM: %w", err)
	}
	p.biosHigh, err = p.memMap.RegisterRAM(HighBIOSAddr, BIOSROMSize, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to register high BIOS: %w", err)
	}

	// Create and register devices. The PC has a master/slave PIC pair:
	// master at 0x20-0x21 with IRQs 0-7 (timer, kbd, serial, ...), slave
	// at 0xA0-0xA1 with IRQs 8-15 (RTC, mouse, IDE primary).
	p.pic = NewPIC8259Cascaded(p.cpu, 0x20, 0xA0)
	p.pic.Register(p.io)

	p.pit = NewPIT8254(p.pic)
	p.pit.SetCyclesFunc(p.cpu.GetCycles)
	p.pit.Register(p.io)

	memSizeKB := uint32(cfg.RAMSize / 1024)
	p.rtc = NewCMOSRTC(memSizeKB)
	p.rtc.Register(p.io)

	p.uart = NewUART16550(p.pic, 4, os.Stdout)
	p.uart.Register(p.io)

	p.kbd = NewKeyboard8042()
	p.kbd.Register(p.io)

	// PCI host bridge. Exposes the 0xCF8 / 0xCFC config-space mechanism.
	// Devices placed on bus 0 are discoverable by Linux's standard
	// PCI probe (no more "PCI: Fatal: No config space access function").
	p.pciHost = NewPCIHost()
	p.pciHost.Register(p.io)

	// (0,0,0): host bridge. Class 0x060000 = "host bridge"; Intel 440FX
	// chipset is what QEMU advertises and Linux knows by name.
	hostBridge := NewPCIDevice("440FX Host Bridge", 0x8086, 0x1237, 0x060000, 0x00)
	p.pciHost.Bus().AddDevice(0, 0, hostBridge)

	// (0,1,0): ISA bridge (PIIX3). Class 0x060100 = "ISA bridge". Marked
	// as multi-function (header type bit 7) so Linux scans function 1
	// (IDE) too.
	isaBridge := NewPCIDevice("PIIX3 ISA Bridge", 0x8086, 0x7000, 0x060100, 0x80)
	p.pciHost.Bus().AddDevice(1, 0, isaBridge)

	// (0,1,1): IDE controller. Class 0x010180 = "IDE, ProgIF 0x80
	// (legacy ports, bus-master capable)". Reports IRQ 14, INT A. The
	// kernel's IDE/ata_piix driver claims this; the actual port-I/O at
	// 0x1F0 is handled by the existing ATAController (the PCI device is
	// just an advertisement so the kernel discovers the controller via
	// the PCI bus walk).
	ideDev := NewPCIDevice("PIIX3 IDE", 0x8086, 0x7010, 0x010180, 0x00)
	ideDev.SetIRQLine(0x0E, 0x01) // IRQ 14, INT A
	// BMDMA: BAR4 advertised as 16-byte I/O region at 0xC000. On a real
	// PIIX3 this is the bus-master DMA register file (8 bytes for primary,
	// 8 for secondary). We don't model DMA; the ports read as zero, writes
	// are ignored. SetIOBAR implements PCI BAR-sizing so the kernel sees a
	// 16-byte region (without sizing the kernel logs "BMDMA: BAR4 is zero,
	// falling back to PIO" and may not dispatch the async ATAPI probe).
	ideDev.SetIOBAR(4, 0xC000, 16)
	p.io.RegisterRead(0xC000, 0xC00F, func(uint16) uint32 { return 0 })
	p.io.RegisterWrite(0xC000, 0xC00F, func(uint16, uint32) {})
	p.pciHost.Bus().AddDevice(1, 1, ideDev)

	// Stub the secondary IDE channel (0x170-0x177 + 0x376) — see
	// RegisterEmptySecondary docs.
	RegisterEmptySecondary(p.io)

	// VGA. Owns the 128 KB framebuffer aperture at 0xA0000-0xBFFFF and
	// the legacy register ports (0x3C0-0x3CF, 0x3D4-0x3DF). When the
	// environment variable TINYEMU_X86_VGA_RENDER=1 is set, the
	// 80×25 text-mode buffer is rendered to stderr as ANSI escape
	// sequences on each cursor-position update — letting the developer
	// watch what the VGA console would display on a real monitor.
	p.vga = NewVGA()
	if err := p.vga.Register(p.io, p.memMap); err != nil {
		return nil, fmt.Errorf("register VGA: %w", err)
	}

	// System Control Port B (0x61): bit 0 enables PIT channel 2 gate; bit 1
	// is the speaker data line. Linux's quick_pit_calibrate masks this port,
	// so we need it to read as 0 by default rather than 0xFF.
	var port61 uint8
	p.io.RegisterRead(0x61, 0x61, func(port uint16) uint32 {
		// Toggle the refresh bit (bit 4) on each read so software that
		// polls it for a delay doesn't spin forever.
		port61 ^= 0x10
		return uint32(port61)
	})
	p.io.RegisterWrite(0x61, 0x61, func(port uint16, val uint32) {
		port61 = uint8(val)
	})

	// LAPIC MMIO at 0xFEE00000 (4 KB) and IOAPIC at 0xFEC00000 (4 KB).
	// We don't model APICs, but modern kernels poke these regions during
	// early CPU init even with `nolapic noapic` on the cmdline. Provide
	// read-as-zero, write-ignored stubs so those accesses don't #PF.
	stubRead := func(opaque any, offset uint32, sizeLog2 int) uint32 { return 0 }
	stubWrite := func(opaque any, offset uint32, val uint32, sizeLog2 int) {}
	if _, err := p.memMap.RegisterDevice(0xFEE00000, 0x1000, nil, stubRead, stubWrite, 0); err != nil {
		return nil, fmt.Errorf("register LAPIC stub: %w", err)
	}
	if _, err := p.memMap.RegisterDevice(0xFEC00000, 0x1000, nil, stubRead, stubWrite, 0); err != nil {
		return nil, fmt.Errorf("register IOAPIC stub: %w", err)
	}

	// Wire CPU I/O to board I/O dispatcher
	p.cpu.SetIOHandlers(
		func(port uint16) uint8 { return p.io.Read8(port) },
		func(port uint16, val uint8) { p.io.Write8(port, val) },
		func(port uint16) uint16 { return p.io.Read16(port) },
		func(port uint16, val uint16) { p.io.Write16(port, val) },
		func(port uint16) uint32 { return uint32(p.io.Read16(port)) | (uint32(p.io.Read16(port+2)) << 16) },
		func(port uint16, val uint32) {
			p.io.Write16(port, uint16(val))
			p.io.Write16(port+2, uint16(val>>16))
		},
	)

	// Wire PIC interrupt delivery to CPU
	p.cpu.SetInterruptAckHandler(func() (uint8, bool) {
		vec := p.pic.DeliverInterrupt()
		if vec < 0 {
			return 0, false
		}
		return uint8(vec), true
	})

	// Create VirtIO console if console is provided
	if cfg.Console != nil {
		if err := p.addVirtIOConsole(cfg.Console); err != nil {
			return nil, fmt.Errorf("failed to create VirtIO console: %w", err)
		}
	}

	return p, nil
}

func (p *PC) addVirtIOConsole(cs *virtio.CharacterDevice) error {
	addr := uint64(VirtIOBaseAddr + p.virtioCount*VirtIOSize)
	irq := mem.NewIRQSignal(func(opaque any, irqNum int, level int) {
		if level != 0 {
			p.pic.RaiseIRQ(uint8(VirtIOIRQ + p.virtioCount))
		} else {
			p.pic.LowerIRQ(uint8(VirtIOIRQ + p.virtioCount))
		}
	}, nil, 0)

	console, err := virtio.NewConsole(p.memMap, addr, irq, cs)
	if err != nil {
		return err
	}

	p.consoleDev = console
	p.virtioDevices = append(p.virtioDevices, console.Device())
	p.virtioCount++
	p.nextVirtIOIRQ++

	return nil
}

// ===== machine.Board interface implementation =====

func (p *PC) Run(maxCycles int) error {
	return p.cpu.Run(maxCycles)
}

func (p *PC) LoadBIOS(biosData []byte, kernelData []byte, initrdData []byte, cmdLine string) error {
	// If kernel data is provided, try direct bzImage boot
	if len(kernelData) > 0 {
		_, err := p.loadBZImage(kernelData, initrdData, cmdLine)
		if err == nil {
			return nil
		}
		// If bzImage parsing fails, try vmlinux ELF direct boot
		_, err = p.loadVMLinux(kernelData, initrdData, cmdLine)
		if err == nil {
			return nil
		}
		// If both fail, fall through to BIOS ROM load
	}

	// Copy BIOS to both low and high ROM regions
	biosLen := uint64(len(biosData))
	if biosLen > BIOSROMSize {
		return errors.New("BIOS image too large")
	}
	copy(p.biosROM.PhysMem, biosData)
	copy(p.biosHigh.PhysMem, biosData)

	// Set CPU reset vector
	p.cpu.SetSeg(x86.CS, 0xF000)
	p.cpu.SetSegBase(x86.CS, 0xF0000)
	p.cpu.SetEIP(0xFFF0)

	return nil
}

// AttachBlockDevice attaches a BlockDevice as the primary IDE master at
// I/O ports 0x1F0-0x1F7 + 0x3F6, IRQ 14. Currently we support exactly one
// drive — calling this twice replaces the previously-attached device.
//
// The kernel then sees the disk as /dev/sda; pass root=/dev/sda1 on the
// cmdline to mount it.
func (p *PC) AttachBlockDevice(bd devices.BlockDevice) error {
	if p.ata != nil {
		_ = p.ata.Close()
	}
	p.ata = NewATAController(p.pic, bd)
	p.ata.Register(p.io)
	return nil
}

// AttachCDROM attaches a BlockDevice as an ATAPI CD-ROM on the primary
// IDE channel. The kernel sees it as /dev/sr0; mount with -t iso9660.
// Replaces any previously-attached disk or CD-ROM on the channel
// (we only model the master position; secondary channel + slave drives
// would require additional ATAController instances).
func (p *PC) AttachCDROM(bd devices.BlockDevice) error {
	if p.ata != nil {
		_ = p.ata.Close()
	}
	p.ata = NewCDROMController(p.pic, bd)
	p.ata.Register(p.io)
	return nil
}

func (p *PC) GetCPU() cpu.Core {
	return p.cpu
}

func (p *PC) MemMap() *mem.PhysMemoryMap {
	return p.memMap
}

func (p *PC) Close() {
	if p.ata != nil {
		_ = p.ata.Close()
	}
	p.memMap.Close()
}

func (p *PC) IsShutdownRequested() bool {
	return p.shutdownRequested
}

func (p *PC) GetShutdownExitCode() int {
	return p.shutdownExitCode
}

func (p *PC) CheckTimer() {
	cycles := p.cpu.GetCycles()
	delta := cycles - p.lastTickCycles
	p.lastTickCycles = cycles
	p.pit.Tick(delta)
}

// AdvanceIdle is called when the CPU is in HLT and no interrupt is pending.
// It "fast-forwards" virtual time by advancing the PIT enough cycles to
// fire its next scheduled tick — which then raises IRQ 0 and wakes the
// CPU from HLT. Without this, a tickless-idle kernel (NOHZ) that HLTs
// after issuing nanosleep() would block forever because the CPU's
// cycle counter doesn't advance while halted.
func (p *PC) AdvanceIdle() {
	const idleStep uint64 = 10000 // enough cycles to cover a PIT tick boundary
	p.cpu.AddCycles(idleStep)
	p.CheckTimer()
}

func (p *PC) PollDevices() {
	// Nothing to poll for now
}

func (p *PC) GetSleepDuration(delay int) int {
	if !p.cpu.IsPowerDown() {
		return 0
	}
	return delay
}

func (p *PC) GetVirtIOAddr() uint64 {
	return uint64(VirtIOBaseAddr + p.virtioCount*VirtIOSize)
}

func (p *PC) GetVirtIOIRQ() *mem.IRQSignal {
	return mem.NewIRQSignal(func(opaque any, irqNum int, level int) {
		irqNum8 := uint8(VirtIOIRQ + p.virtioCount)
		if level != 0 {
			p.pic.RaiseIRQ(irqNum8)
		} else {
			p.pic.LowerIRQ(irqNum8)
		}
	}, nil, 0)
}

func (p *PC) AddVirtIODevice(dev *virtio.Device) (int, error) {
	p.virtioDevices = append(p.virtioDevices, dev)
	irqNum := p.nextVirtIOIRQ
	p.virtioCount++
	p.nextVirtIOIRQ++
	return irqNum, nil
}

func (p *PC) Console() *virtio.Console {
	return p.consoleDev
}

// UART returns the COM1 device so external callers (e.g. cmd/temu) can push
// stdin bytes into the receive FIFO.
func (p *PC) UART() *UART16550 {
	return p.uart
}

// PIC returns the 8259 master controller (for diagnostics).
func (p *PC) PIC() *PIC8259 { return p.pic }

// PIT returns the 8254 programmable interval timer (for diagnostics).
func (p *PC) PIT() *PIT8254 { return p.pit }
