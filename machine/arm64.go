package machine

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/sorins/tinyemu-go/cpu"
	arm64cpu "github.com/sorins/tinyemu-go/cpu/arm64"
	"github.com/sorins/tinyemu-go/devices"
	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/virtio"
)

// ARM64 "virt" board: a minimal, QEMU-virt-compatible AArch64 machine — GICv2,
// PL011 UART, the ARM generic timer, PSCI (via HVC), VirtIO-MMIO, and a generated
// device tree — enough to boot a mainline arm64 Linux Image to a shell.

// Memory map (matches the QEMU virt layout the kernel expects via the DTB).
const (
	a64GICDBase  = 0x08000000
	a64GICCBase  = 0x08010000
	a64UARTBase  = 0x09000000
	a64VirtIOBase = 0x0a000000
	a64VirtIOSize = 0x200 // per-device window
	a64RAMBase   = 0x40000000

	// Load layout within RAM.
	a64DTBOff    = 0x00000000 // DTB at RAM base
	a64KernelOff = 0x00200000 // kernel Image at base + 2 MiB (2 MiB aligned)
	a64InitrdOff = 0x08000000 // initrd at base + 128 MiB

	// UEFI firmware flash (QEMU virt layout): two 64 MiB banks below the GIC.
	// Bank 0 holds the firmware code (edk2 reset vector at 0x0); bank 1 is the
	// writable UEFI variable store (left blank for edk2 to format).
	a64FlashBase     = 0x00000000
	a64FlashCodeSize = 0x04000000
	a64FlashVarsBase = 0x04000000
	a64FlashVarsSize = 0x04000000

	// Interrupt IDs (GIC INTIDs). SPIs are 32+; the DTB encodes them as SPI n.
	a64TimerVirtPPI = 27 // EL1 virtual timer (PPI 11)
	a64TimerPhysPPI = 30 // EL1 physical timer (PPI 14)
	a64UARTIntID    = 33 // SPI 1
	a64VirtIOIntID0 = 48 // SPI 16; device i -> 48+i

	a64TimerFreq = 62500000 // CNTFRQ advertised to the guest (62.5 MHz)
)

// ARM64Machine is the virt board.
type ARM64Machine struct {
	memMap  *mem.PhysMemoryMap
	cpu     *arm64cpu.CPU
	ram     *mem.PhysMemoryRange
	ramSize uint64

	gic  *devices.GICv2
	uart *devices.PL011

	console *virtio.CharacterDevice

	virtioDevices []*virtio.Device
	virtioCount   int

	shutdownRequested bool
	shutdownExitCode  int
}

// NewARM64 creates the virt board.
func NewARM64(cfg Config) (*ARM64Machine, error) {
	ramSize := cfg.RAMSize
	if ramSize == 0 {
		ramSize = 512 << 20
	}
	m := &ARM64Machine{
		memMap:  mem.NewPhysMemoryMap(),
		ramSize: ramSize,
		console: cfg.Console,
	}

	m.cpu = arm64cpu.New(m.memMap)

	var err error
	if m.ram, err = m.memMap.RegisterRAM(a64RAMBase, ramSize, 0); err != nil {
		return nil, fmt.Errorf("register RAM: %w", err)
	}

	// GICv2 drives the CPU's external IRQ line.
	m.gic = devices.NewGICv2(func(level int) { m.cpu.SetIRQ(level) })
	if err := m.gic.Register(m.memMap, a64GICDBase, a64GICCBase); err != nil {
		return nil, fmt.Errorf("register GIC: %w", err)
	}

	// PL011 serial console on SPI 1.
	var con devices.Console
	if cfg.Console != nil {
		con = cfg.Console
	}
	m.uart = devices.NewPL011(con, m.gic.CreateIRQ(a64UARTIntID))
	if err := m.uart.Register(m.memMap, a64UARTBase); err != nil {
		return nil, fmt.Errorf("register UART: %w", err)
	}

	// Generic timer: advertise CNTFRQ, and service PSCI via HVC.
	m.cpu.SetCNTFRQ(a64TimerFreq)
	m.cpu.HVCHandler = m.psci

	return m, nil
}

// --- Board interface ---

func (m *ARM64Machine) GetCPU() cpu.Core          { return m.cpu }
func (m *ARM64Machine) MemMap() *mem.PhysMemoryMap { return m.memMap }
func (m *ARM64Machine) Close()                    { m.memMap.Close() }
func (m *ARM64Machine) Console() *virtio.Console  { return nil } // console is the PL011
func (m *ARM64Machine) IsShutdownRequested() bool { return m.shutdownRequested }
func (m *ARM64Machine) GetShutdownExitCode() int  { return m.shutdownExitCode }
func (m *ARM64Machine) Run(maxCycles int) error   { return m.cpu.Run(maxCycles) }

// CheckTimer samples the generic-timer outputs into the GIC's timer PPIs.
func (m *ARM64Machine) CheckTimer() {
	m.gic.SetLine(a64TimerVirtPPI, m.cpu.VirtTimerPending())
	m.gic.SetLine(a64TimerPhysPPI, m.cpu.PhysTimerPending())
}

// PollDevices pumps host console input into the UART.
func (m *ARM64Machine) PollDevices() {
	m.uart.PollInput()
}

// GetSleepDuration: when parked in WFI, fast-forward the system counter to the
// next timer deadline (skipping idle time) so the timer fires immediately;
// otherwise allow a short real sleep to stay responsive to console input.
func (m *ARM64Machine) GetSleepDuration(delay int) int {
	if !m.cpu.IsPowerDown() || m.cpu.HasPendingInterrupt() {
		return 0
	}
	if next := m.cpu.NextTimerDeadline(); next != 0 {
		m.cpu.AdvanceCounter(next)
		return 0
	}
	if delay > 10 {
		delay = 10 // cap idle sleep for input responsiveness
	}
	return delay
}

// --- VirtioMMIOAttacher ---

func (m *ARM64Machine) GetVirtIOAddr() uint64 {
	return uint64(a64VirtIOBase + m.virtioCount*a64VirtIOSize)
}

func (m *ARM64Machine) GetVirtIOIRQ() *mem.IRQSignal {
	return m.gic.CreateIRQ(a64VirtIOIntID0 + m.virtioCount)
}

func (m *ARM64Machine) AddVirtIODevice(dev *virtio.Device) (int, error) {
	if m.virtioCount >= 32 {
		return 0, errors.New("no more virtio-mmio slots")
	}
	m.virtioDevices = append(m.virtioDevices, dev)
	id := a64VirtIOIntID0 + m.virtioCount
	m.virtioCount++
	return id, nil
}

// psci services a PSCI hypercall (HVC). It reads the function id from x0 and
// writes the result back to x0.
func (m *ARM64Machine) psci(c *arm64cpu.CPU) bool {
	const (
		psciVersion    = 0x84000000
		psciSystemOff  = 0x84000008
		psciSystemRst  = 0x84000009
		psciMigrInfo   = 0x84000006
		psciNotSupported = 0xFFFFFFFF
	)
	switch c.X[0] & 0xFFFFFFFF {
	case psciVersion:
		c.X[0] = 0x00000002 // PSCI v0.2
	case psciSystemOff, psciSystemRst:
		m.shutdownRequested = true
		c.X[0] = 0
	case psciMigrInfo:
		c.X[0] = 2 // migration not required
	default:
		c.X[0] = psciNotSupported
	}
	return true
}

// bootImageKind classifies what the boot blob (-bios/-kernel) is, so each type
// of payload gets the right loader.
type bootImageKind int

const (
	bootLinuxImage   bootImageKind = iota // flat arm64 Linux Image (direct boot)
	bootUEFIFirmware                      // edk2/UEFI firmware flash (FreeBSD, etc.)
)

// linuxImageMagic is "ARMd" (little-endian) at offset 56 of an arm64 Image.
const linuxImageMagic = 0x644d5241

// classifyBootImage inspects the blob's header. An arm64 Linux Image carries a
// known magic; anything else is treated as a UEFI firmware image (the chain-load
// path for FreeBSD and other UEFI OSes).
func classifyBootImage(data []byte) bootImageKind {
	if len(data) >= 64 && binary.LittleEndian.Uint32(data[56:60]) == linuxImageMagic {
		return bootLinuxImage
	}
	return bootUEFIFirmware
}

// LoadBIOS loads the boot payload and prepares the CPU. The payload may be a flat
// Linux Image (direct boot) or a UEFI firmware image (which then chain-loads an
// OS from the virtio-blk disk); the handler is chosen by classifyBootImage.
func (m *ARM64Machine) LoadBIOS(biosData, kernelData, initrdData []byte, cmdLine string) error {
	image := biosData
	if len(image) == 0 {
		image = kernelData
	}
	if len(image) == 0 {
		return errors.New("arm64: no boot image")
	}

	switch classifyBootImage(image) {
	case bootUEFIFirmware:
		return m.bootUEFIFirmware(image, cmdLine)
	default:
		return m.bootLinuxImage(image, initrdData, cmdLine)
	}
}

// bootLinuxImage loads a flat arm64 Image + optional initrd into RAM, builds the
// device tree, and sets the Linux boot protocol (x0=DTB, PC=kernel entry).
func (m *ARM64Machine) bootLinuxImage(image, initrdData []byte, cmdLine string) error {
	ramPtr := m.ram.PhysMem
	if a64KernelOff+uint64(len(image)) > m.ramSize {
		return errors.New("arm64: kernel image too large for RAM")
	}
	copy(ramPtr[a64KernelOff:], image)

	var initrdStart, initrdSize uint64
	if len(initrdData) > 0 {
		if a64InitrdOff+uint64(len(initrdData)) > m.ramSize {
			return errors.New("arm64: initrd too large for RAM")
		}
		copy(ramPtr[a64InitrdOff:], initrdData)
		initrdStart = a64RAMBase + a64InitrdOff
		initrdSize = uint64(len(initrdData))
	}

	m.buildARM64FDT(ramPtr[a64DTBOff:], initrdStart, initrdStart+initrdSize, cmdLine, false)

	m.cpu.X[0] = a64RAMBase + a64DTBOff
	m.cpu.X[1], m.cpu.X[2], m.cpu.X[3] = 0, 0, 0
	m.cpu.PC = a64RAMBase + a64KernelOff
	return nil
}

// bootUEFIFirmware maps a UEFI firmware image as the boot flash (QEMU virt
// layout: code bank at 0x0, blank writable variable bank after it), places the
// device tree at the base of RAM where edk2 looks for it, and resets the CPU to
// the firmware reset vector at 0x0. The firmware then drives the rest of the
// boot (reading the OS off the virtio-blk disk).
func (m *ARM64Machine) bootUEFIFirmware(firmware []byte, cmdLine string) error {
	if uint64(len(firmware)) > a64FlashCodeSize {
		return fmt.Errorf("arm64: UEFI firmware too large (%d > %d)", len(firmware), a64FlashCodeSize)
	}
	// Code flash bank (read-mostly) at 0x0.
	codeFlash, err := m.memMap.RegisterRAM(a64FlashBase, a64FlashCodeSize, 0)
	if err != nil {
		return fmt.Errorf("register UEFI code flash: %w", err)
	}
	copy(codeFlash.PhysMem, firmware)

	// Writable variable bank, left blank (0xFF = erased flash) for edk2 to format.
	varFlash, err := m.memMap.RegisterRAM(a64FlashVarsBase, a64FlashVarsSize, 0)
	if err != nil {
		return fmt.Errorf("register UEFI vars flash: %w", err)
	}
	for i := range varFlash.PhysMem {
		varFlash.PhysMem[i] = 0xFF
	}

	// Device tree at the base of RAM (edk2 ArmVirtQemu reads it there); include
	// the flash node so the firmware finds its variable store.
	m.buildARM64FDT(m.ram.PhysMem[a64DTBOff:], 0, 0, cmdLine, true)

	// Reset to the firmware entry at flash base.
	m.cpu.X[0], m.cpu.X[1], m.cpu.X[2], m.cpu.X[3] = 0, 0, 0, 0
	m.cpu.PC = a64FlashBase
	return nil
}
