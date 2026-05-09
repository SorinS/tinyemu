// Package pc implements an x86 PC machine/board emulator.
package pc

import (
	"errors"
	"fmt"
	"os"

	"github.com/jtolio/tinyemu-go/cpu"
	"github.com/jtolio/tinyemu-go/cpu/x86"
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
	ramSize    uint64
	lowRAM     *mem.PhysMemoryRange
	biosROM    *mem.PhysMemoryRange
	biosHigh   *mem.PhysMemoryRange
	console    *virtio.CharacterDevice
	consoleDev *virtio.Console

	virtioDevices []*virtio.Device
	virtioCount   int
	nextVirtIOIRQ int

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

	// Create and register devices
	p.pic = NewPIC8259(p.cpu, 0x20)
	p.pic.Register(p.io)

	p.pit = NewPIT8254(p.pic)
	p.pit.Register(p.io)

	memSizeKB := uint32(cfg.RAMSize / 1024)
	p.rtc = NewCMOSRTC(memSizeKB)
	p.rtc.Register(p.io)

	p.uart = NewUART16550(p.pic, 4, os.Stdout)
	p.uart.Register(p.io)

	p.kbd = NewKeyboard8042()
	p.kbd.Register(p.io)

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
		// If bzImage parsing fails, fall through to raw kernel load
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

func (p *PC) GetCPU() cpu.Core {
	return p.cpu
}

func (p *PC) MemMap() *mem.PhysMemoryMap {
	return p.memMap
}

func (p *PC) Close() {
	p.memMap.Close()
}

func (p *PC) IsShutdownRequested() bool {
	return p.shutdownRequested
}

func (p *PC) GetShutdownExitCode() int {
	return p.shutdownExitCode
}

func (p *PC) CheckTimer() {
	p.pit.Tick()
	// Update CPU interrupt line based on pending PIC interrupts
	if p.pic.PeekInterrupt() >= 0 {
		p.cpu.SetINTR(1)
	} else {
		p.cpu.SetINTR(0)
	}
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
