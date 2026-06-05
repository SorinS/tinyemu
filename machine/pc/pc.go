// Package pc implements an x86 PC machine/board emulator.
package pc

import (
	"fmt"
	"io"
	"os"

	"github.com/jtolio/tinyemu-go/cpu"
	"github.com/jtolio/tinyemu-go/cpu/x86"
	"github.com/jtolio/tinyemu-go/cpu/x86_64"
	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/mem"
	"github.com/jtolio/tinyemu-go/virtio"
)

// Memory map constants for a standard PC.
//
// BIOS layout uses the modern "256 KB at top of 4 GiB + shadowed at the
// top of low 1 MiB" arrangement so SeaBIOS (256 KB) can boot here as it
// does under QEMU. Small custom BIOSes ≤256 KB drop into the same
// region — they're placed at the END of the 256 KB window so their
// reset-vector offset (`size-16`) lands at the canonical 0xFFFFFFF0.
const (
	LowRAMAddr      = 0x00000000
	LowRAMSize      = 0x000A0000 // 640KB conventional memory
	VGAApertureAddr = 0x000A0000
	VGAApertureSize = 0x00020000 // 128KB VGA memory
	BIOSROMAddr     = 0x000C0000 // low BIOS shadow window
	BIOSROMSize     = 0x00040000 // 256 KB low shadow — fits SeaBIOS's legacy alias
	// High BIOS / firmware-flash window. Sized at 4 MiB so a full OVMF
	// (UEFI) flash image fits with its reset vector at 0xFFFFFFF0; the
	// region is RAM-backed and therefore writable, which doubles as the
	// UEFI variable store's backing (non-persistent across runs). Smaller
	// firmware (SeaBIOS = 256 KB, tiny custom BIOSes) is placed at the END
	// of the window so the reset vector still lands at 0xFFFFFFF0.
	HighBIOSSize = 0x00400000                 // 4 MiB
	HighBIOSAddr = 0x100000000 - HighBIOSSize // 0xFFC00000

	VirtIOBaseAddr = 0x00010000 // Fixed MMIO address for VirtIO devices
	VirtIOSize     = 0x00001000 // 4KB per device
	VirtIOIRQ      = 8          // First VirtIO IRQ (after PIC internal uses)
)

// Config holds configuration for creating a new PC.
type Config struct {
	RAMSize uint64 // RAM size in bytes
	Console *virtio.CharacterDevice
	// MachineType selects the CPU backend. Empty or "x86" picks the
	// i386 backend (cpu/x86); "x86_64" picks the long-mode backend
	// (cpu/x86_64). Devices and chassis are identical either way.
	MachineType string
}

// Compile-time checks: both CPU backends satisfy cpu.X86Core.
var (
	_ cpu.X86Core = (*x86.CPU)(nil)
	_ cpu.X86Core = (*x86_64.CPU)(nil)
)

// PC represents a complete x86 PC machine.
type PC struct {
	memMap     *mem.PhysMemoryMap
	cpu        cpu.X86Core
	is64       bool
	io         *IOPortDispatcher
	pic        *PIC8259
	pit        *PIT8254
	rtc        *CMOSRTC
	fdc        *FDC
	uart       *UART16550
	kbd        *Keyboard8042
	ata        *ATAController
	pciHost    *PCIHost
	vga        *VGA
	ramSize    uint64
	lowRAM     *mem.PhysMemoryRange
	biosROM    *mem.PhysMemoryRange
	biosHigh   *mem.PhysMemoryRange
	fwCfg      *fwCfg
	ps2        *PS2Controller
	console    *virtio.CharacterDevice
	consoleDev *virtio.Console

	virtioDevices     []*virtio.Device
	virtioCount       int
	virtioBlkPCICount int
	virtioNetPCICount int
	nextVirtIOIRQ     int

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
		is64:          cfg.MachineType == "x86_64",
	}

	// Create CPU — backend selected by MachineType.
	if p.is64 {
		p.cpu = x86_64.NewCPU(p.memMap)
	} else {
		p.cpu = x86.NewCPU(p.memMap)
	}

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
	p.biosHigh, err = p.memMap.RegisterRAM(HighBIOSAddr, HighBIOSSize, 0)
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
	//
	// Skip on x86_64 — modern Linux's libata (6.10+) hangs in async-EH
	// probing the secondary PATA channel even with our "no device"
	// stubs. Since the x86_64 boot path uses virtio-blk-pci for all
	// disk I/O, the PIIX3 IDE PCI device is dead weight that just
	// triggers a 5-minute boot stall. We keep it on x86 (32-bit) for
	// compatibility with the older 3.19 kernel paths that work today.
	if !p.is64 {
		ideDev := NewPCIDevice("PIIX3 IDE", 0x8086, 0x7010, 0x010180, 0x00)
		ideDev.SetIRQLine(0x0E, 0x01) // IRQ 14, INT A
		// BMDMA: BAR4 advertised as 16-byte I/O region at 0xC000. See
		// SetIOBAR docs for the size-sizing handshake.
		ideDev.SetIOBAR(4, 0xC000, 16)
		p.io.RegisterRead(0xC000, 0xC00F, func(uint16) uint32 { return 0 })
		p.io.RegisterWrite(0xC000, 0xC00F, func(uint16, uint32) {})
		p.pciHost.Bus().AddDevice(1, 1, ideDev)

		// Stub the secondary IDE channel (0x170-0x177 + 0x376) — see
		// RegisterEmptySecondary docs.
		RegisterEmptySecondary(p.io)
	}

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

	// System Control Port B (0x61). Bit roles:
	//   0  PIT channel 2 gate enable (we accept writes, no PIT impact)
	//   1  speaker data
	//   4  refresh-cycle toggle (DMA refresh)
	//   5  PIT channel 2 output (OUT2)
	// SeaBIOS's CPU speed calibration polls bit 5 in a tight loop; Linux's
	// quick_pit_calibrate masks the port and reads it as zero. We toggle
	// BOTH bit 4 and bit 5 on each read so any polling loop terminates,
	// regardless of which bit it watches.
	var port61 uint8
	p.io.RegisterRead(0x61, 0x61, func(port uint16) uint32 {
		port61 ^= 0x30
		return uint32(port61)
	})
	p.io.RegisterWrite(0x61, 0x61, func(port uint16, val uint32) {
		port61 = uint8(val)
	})

	// QEMU/SeaBIOS debug ports.
	//
	// 0x402: byte-write debugcon — SeaBIOS prints its boot log here when
	//        CONFIG_DEBUG_IO is on (the default in qemu's prebuilt). 0xE9
	//        is the equivalent in Bochs/older builds.
	// 0x80:  POST diagnostic byte — BIOSes also use writes to 0x80 as an
	//        ~1 µs IO-delay primitive (real hw waited on the ISA bus),
	//        so 99% of writes here are timing noise (often 0xff). The
	//        port is accepted and the last value remembered so reads
	//        return it, but logging is OFF by default — opt in via
	//        TINYEMU_BIOS_POST=1 if you really want it.
	//
	// 0x402 / 0xE9 routing:
	//   (unset)   silent — no console pollution
	//   =1        file ./tinyemu-bios.log
	//   =stderr   stderr (legacy / debug-the-debug)
	//   =<path>   that file (truncated on start)
	biosDbg := io.Writer(io.Discard)
	switch v := os.Getenv("TINYEMU_BIOS_DEBUG"); v {
	case "":
		// silent
	case "stderr":
		biosDbg = os.Stderr
	case "1":
		if f, err := os.OpenFile("tinyemu-bios.log",
			os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644); err == nil {
			biosDbg = f
			fmt.Fprintln(os.Stderr, "[BIOS] tracing to ./tinyemu-bios.log")
		}
	default:
		if f, err := os.OpenFile(v,
			os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644); err == nil {
			biosDbg = f
			fmt.Fprintf(os.Stderr, "[BIOS] tracing to %s\n", v)
		}
	}
	logPost := os.Getenv("TINYEMU_BIOS_POST") == "1"
	var postCode uint8
	p.io.RegisterRead(0x80, 0x80, func(port uint16) uint32 { return uint32(postCode) })
	p.io.RegisterWrite(0x80, 0x80, func(port uint16, val uint32) {
		postCode = uint8(val)
		if logPost {
			fmt.Fprintf(biosDbg, "[bios-post] %#02x\n", postCode)
		}
	})
	dbgWriter := func(port uint16, val uint32) {
		_, _ = biosDbg.Write([]byte{byte(val)})
	}
	// Reads return 0xE9 (QEMU_DEBUGCON_READBACK) so SeaBIOS's
	// qemu_debug_preinit probe accepts the port as present — otherwise
	// it sets DebugOutputPort=0 and every dprintf becomes a no-op.
	// 0xFF would have meant "no device", and that's what we returned
	// for the longest time, eating SeaBIOS's entire boot log.
	p.io.RegisterRead(0x402, 0x402, func(port uint16) uint32 { return 0xE9 })
	p.io.RegisterWrite(0x402, 0x402, dbgWriter)
	p.io.RegisterRead(0xE9, 0xE9, func(port uint16) uint32 { return 0xE9 })
	p.io.RegisterWrite(0xE9, 0xE9, dbgWriter)

	// LAPIC MMIO at 0xFEE00000 (4 KB) and IOAPIC at 0xFEC00000 (4 KB).
	// We don't model APICs, but modern kernels (and SeaBIOS) poke these
	// regions during early CPU init even with `nolapic noapic` on the
	// cmdline. Provide read-as-zero, write-ignored stubs accepting all
	// access widths — passing flags=0 to RegisterDevice means "no sizes
	// supported" which rejects every access and surfaces as a #PF
	// (caused the SeaBIOS post-init wall at 0xFEE00030).
	stubRead := func(opaque any, offset uint32, sizeLog2 int) uint32 { return 0 }
	stubWrite := func(opaque any, offset uint32, val uint32, sizeLog2 int) {}
	stubFlags := mem.DevIOSize8 | mem.DevIOSize16 | mem.DevIOSize32 | mem.DevIOSize64
	if _, err := p.memMap.RegisterDevice(0xFEE00000, 0x1000, nil, stubRead, stubWrite, stubFlags); err != nil {
		return nil, fmt.Errorf("register LAPIC stub: %w", err)
	}
	if _, err := p.memMap.RegisterDevice(0xFEC00000, 0x1000, nil, stubRead, stubWrite, stubFlags); err != nil {
		return nil, fmt.Errorf("register IOAPIC stub: %w", err)
	}

	// fw_cfg — QEMU paravirt channel that SeaBIOS reads to find ACPI
	// tables, kernel cmdline, SMBIOS, etc. Without this SeaBIOS publishes
	// a stub RSDP whose XsdtAddress is uninitialized, and any guest that
	// walks 0xE0000..0xFFFFF for an RSDP (Pure64, BareMetal, ACPI-aware
	// custom loaders) halts on the first checksum failure. The files
	// added here describe a minimal ACPI table set — RSDP + RSDT + FADT +
	// MADT + HPET — that SeaBIOS allocates, relocates, and publishes on
	// our behalf via its BiosLinker.
	p.fwCfg = newFWCfg()
	// etc/e820 must be added BEFORE etc/table-loader — SeaBIOS reads
	// e820 during its early POST (when responding to the MBR's INT
	// 15h E820 calls), and a missing file makes the staging buffer
	// it copies from stay all-zero. Loaders that build page tables
	// from the e820 then see no RAM, underflow their MiB counter,
	// and overrun the page-table area into their own code.
	p.fwCfg.addFile("etc/e820", buildE820(cfg.RAMSize))
	p.fwCfg.addFile("etc/acpi/rsdp", rsdpBlob())
	p.fwCfg.addFile("etc/acpi/tables", tablesBlob())
	p.fwCfg.addFile("etc/table-loader", tableLoaderScript())
	p.fwCfg.Register(p.io)

	// 8042 PS/2 controller stub — enough of the protocol to keep guests
	// that POST a real keyboard controller (BareMetal's init_hid,
	// SeaBIOS keyboard probe, Linux's i8042 driver) from spinning on
	// port 0x64 status polls. See machine/pc/ps2.go for what we do and
	// don't model.
	p.ps2 = NewPS2Controller()
	p.ps2.Register(p.io)

	// Wire CPU I/O to board I/O dispatcher
	p.cpu.SetIOHandlers(
		func(port uint16) uint8 { return p.io.Read8(port) },
		func(port uint16, val uint8) { p.io.Write8(port, val) },
		func(port uint16) uint16 { return p.io.Read16(port) },
		func(port uint16, val uint16) { p.io.Write16(port, val) },
		func(port uint16) uint32 { return p.io.Read32(port) },
		func(port uint16, val uint32) { p.io.Write32(port, val) },
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
		if p.is64 {
			// Long-mode boot. Try PVH first (OSv, FreeBSD-as-Xen-DomU,
			// any ELF with XEN_ELFNOTE_PHYS32_ENTRY); fall through to
			// vmlinux64 (Linux ELF kernels with the standard 64-bit
			// entry); fall through to bzImage (compressed Linux). Each
			// loader inspects the kernel image and returns an error if
			// the format doesn't match, so order is just a try-in-turn.
			if err := p.loadPVH64(kernelData, initrdData, cmdLine); err == nil {
				return nil
			}
			if err := p.loadVMLinux64(kernelData, initrdData, cmdLine); err == nil {
				return nil
			}
			if err := p.loadBZImage64(kernelData, initrdData, cmdLine); err != nil {
				return fmt.Errorf("64-bit kernel load failed: %w", err)
			}
			return nil
		}
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

	// Copy the firmware into the high window and the low legacy shadow.
	// Both are placed at the END of their window so the reset vector at
	// file-offset (size-16) lands at canonical 0xFFFFFFF0 (high) and the
	// top of the image aliases into 0xF0000..0xFFFFF (low shadow), exactly
	// as real hardware aliases the top of the flash down to the legacy
	// boot-block area.
	//
	// The high window is 4 MiB so a full OVMF flash fits; the low shadow
	// is only 256 KB, so for an image larger than that we mirror just its
	// top 256 KB there (all a legacy alias ever sees).
	biosLen := uint64(len(biosData))
	if biosLen > HighBIOSSize {
		return fmt.Errorf("firmware image too large: %d bytes (max %d)", biosLen, HighBIOSSize)
	}
	copy(p.biosHigh.PhysMem[HighBIOSSize-biosLen:], biosData)

	lowLen := biosLen
	if lowLen > BIOSROMSize {
		lowLen = BIOSROMSize
	}
	copy(p.biosROM.PhysMem[BIOSROMSize-lowLen:], biosData[biosLen-lowLen:])

	// Set CPU reset vector. x86 reset state has CS.base = 0xFFFF0000 with
	// EIP = 0xFFF0, so the first fetch is at 0xFFFFFFF0 — the last 16
	// bytes of the high BIOS region, which all real BIOSes use for the
	// initial far-jump-to-low-real-mode-entry.
	//
	// For tiny custom BIOSes ≤64 KB, the legacy convention used CS.base =
	// 0xF0000 (low shadow). We pick high vs low by size: anything larger
	// than 64 KB is a "modern" BIOS (SeaBIOS, OVMF in CSM, etc.) that
	// expects the high reset; smaller images keep the legacy behaviour.
	p.cpu.SetSeg(x86.CS, 0xF000)
	if biosLen > 0x10000 {
		p.cpu.SetSegBase(x86.CS, 0xFFFF0000)
	} else {
		p.cpu.SetSegBase(x86.CS, 0xF0000)
	}
	p.cpu.SetEIP(0xFFF0)

	return nil
}

// AttachBlockDevice attaches a BlockDevice to the PC. By default this
// goes through virtio-blk-pci (kernel sees /dev/vda), bypassing libata
// entirely — our ATA/IDE controller is functional but Linux 6.6's
// libata never completes its async PIO probe against it (see
// project_atapi_probe_stall.md). Set TINYEMU_X86_IDE=1 to force the
// legacy ATA/IDE path (where the controller is exposed at 0x1F0+ but
// the kernel may not detect it).
func (p *PC) AttachBlockDevice(bd devices.BlockDevice) error {
	if os.Getenv("TINYEMU_X86_IDE") == "1" {
		if p.ata != nil {
			_ = p.ata.Close()
		}
		p.ata = NewATAController(p.pic, bd)
		p.ata.Register(p.io)
		return nil
	}
	return p.AttachVirtioBlock(bd)
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

// virtioBlkPCIIOBase is the I/O port base for the first virtio-blk-pci
// device's legacy register window. Subsequent devices get +64 to stay
// clear of the previous device's config-space tail.
const virtioBlkPCIIOBase = 0xC100

// virtioBlkPCIIRQ is the legacy IRQ line we wire virtio-blk-pci's INTx to.
// IRQ 11 is unused by our PC board's other devices.
const virtioBlkPCIIRQ = 11

// virtioNetPCIIOBase is the I/O port base for virtio-net-pci's legacy
// register window. Disjoint from virtio-blk so multiple devices coexist.
const virtioNetPCIIOBase = 0xC200

// virtioNetPCIIRQ is the legacy IRQ line for virtio-net-pci INTx.
const virtioNetPCIIRQ = 10

// AttachFloppy presents `image` as a 1.44 MB floppy in drive A, wiring
// an FDC (ports 0x3F0-0x3F7) + slave DMA + IRQ6 and flagging the CMOS so
// SeaBIOS probes and boots it (DL=0x00). Used to boot floppy-only images
// like MenuetOS, whose boot sector reads the rest of the OS from drive 0.
func (p *PC) AttachFloppy(image []byte) {
	p.fdc = NewFDC(image, p.pic, p.memMap)
	p.fdc.Register(p.io)
	if p.rtc != nil {
		p.rtc.SetFloppyType144()
	}
}

// AttachVirtioBlock attaches a BlockDevice as a virtio-blk-pci device.
// The kernel sees it as /dev/vda. This bypasses libata entirely and is
// the recommended way to give the guest a writable disk on x86.
//
// PCI slot allocation: (0, 3+n, 0) where n = how many virtio-blk devices
// are already attached. Each device gets a 64-byte I/O BAR starting at
// 0xC100+n*64. All devices share PIC IRQ 11 (level-triggered semantics
// for shared INTx).
func (p *PC) AttachVirtioBlock(bd devices.BlockDevice) error {
	slot := uint8(3 + p.virtioBlkPCICount)
	ioBase := uint16(virtioBlkPCIIOBase + p.virtioBlkPCICount*64)

	// IRQSignal that raises/lowers our PIC IRQ.
	irq := mem.NewIRQSignal(func(_ any, _ int, level int) {
		if level != 0 {
			p.pic.RaiseIRQ(virtioBlkPCIIRQ)
		} else {
			p.pic.LowerIRQ(virtioBlkPCIIRQ)
		}
	}, nil, virtioBlkPCIIRQ)

	// Build the virtio block backend (sets up recvRequest + config).
	blk := virtio.NewBlockDeviceCore(p.memMap, irq, bd)
	transport := virtio.NewLegacyTransport(blk.Device())

	// Register the I/O BAR ports. Each access is forwarded to the
	// transport, which translates legacy register offsets to Device
	// state. registerVirtioIOPorts (re)installs the handlers for a given
	// base and is reused by the BAR-relocation callback below — closures
	// capture `base` so the offset math follows the BAR when firmware
	// moves it. SeaBIOS reassigns BARs during its own PCI enumeration
	// (Linux keeps the firmware-set bases), so without relocation our
	// statically-registered ports were left stranded at the old address
	// and every virtio access read back 0xFFFF.
	t := transport // capture for closures
	ioSize := uint16(transport.IOSize())
	registerVirtioIOPorts := func(base uint16) {
		end := base + ioSize - 1
		p.io.RegisterRead(base, end, func(port uint16) uint32 {
			return t.IORead(port-base, 1)
		})
		p.io.RegisterWrite(base, end, func(port uint16, val uint32) {
			t.IOWrite(port-base, val, 1)
		})
		p.io.RegisterRead16(base, end-1, func(port uint16) uint32 {
			return t.IORead(port-base, 2)
		})
		p.io.RegisterWrite16(base, end-1, func(port uint16, val uint32) {
			t.IOWrite(port-base, val, 2)
		})
		p.io.RegisterRead32(base, end-3, func(port uint16) uint32 {
			return t.IORead(port-base, 4)
		})
		p.io.RegisterWrite32(base, end-3, func(port uint16, val uint32) {
			t.IOWrite(port-base, val, 4)
		})
	}
	clearVirtioIOPorts := func(base uint16) {
		end := base + ioSize - 1
		p.io.RegisterRead(base, end, nil)
		p.io.RegisterWrite(base, end, nil)
		p.io.RegisterRead16(base, end-1, nil)
		p.io.RegisterWrite16(base, end-1, nil)
		p.io.RegisterRead32(base, end-3, nil)
		p.io.RegisterWrite32(base, end-3, nil)
	}
	registerVirtioIOPorts(ioBase)

	// PCI device — vendor 0x1AF4 (Red Hat), device 0x1001 (virtio-blk
	// legacy/transitional), subsystem device 0x0002 (block), class
	// 0x010000 (mass storage controller, sub: SCSI).
	pciDev := NewPCIDevice("virtio-blk", 0x1AF4, 0x1001, 0x010000, 0x00)
	pciDev.SetIRQLine(virtioBlkPCIIRQ, 0x01) // INT A
	pciDev.SetIOBAR(0, uint32(ioBase), 64)   // 64-byte I/O BAR
	// Follow BAR0 if firmware relocates the I/O region.
	curBase := ioBase
	pciDev.SetBARChangeHandler(func(idx int, newBase uint32, isIO bool) {
		if idx != 0 || !isIO {
			return
		}
		nb := uint16(newBase)
		if nb == curBase {
			return
		}
		clearVirtioIOPorts(curBase)
		registerVirtioIOPorts(nb)
		curBase = nb
	})
	// Subsystem vendor/device per virtio spec §4.1.2.1: subsys-vendor
	// is don't-care for transitional, subsys-id selects the device type.
	pciDev.setU16(0x2C, 0x1AF4)
	pciDev.setU16(0x2E, 0x0002) // 0x0002 = block
	p.pciHost.Bus().AddDevice(slot, 0, pciDev)

	p.virtioDevices = append(p.virtioDevices, blk.Device())
	p.virtioBlkPCICount++
	return nil
}

// AttachNet attaches an EthernetDevice as a virtio-net-pci device,
// satisfying the machine.NetAttacher interface. The kernel sees the NIC
// as eth0. PCI slot (0, 4+n, 0); IRQ 10; I/O BAR at 0xC200+n*64.
// Multiple devices can be attached by calling this repeatedly.
func (p *PC) AttachNet(es *virtio.EthernetDevice) error {
	slot := uint8(4 + p.virtioNetPCICount)
	ioBase := uint16(virtioNetPCIIOBase + p.virtioNetPCICount*64)

	irq := mem.NewIRQSignal(func(_ any, _ int, level int) {
		if level != 0 {
			p.pic.RaiseIRQ(virtioNetPCIIRQ)
		} else {
			p.pic.LowerIRQ(virtioNetPCIIRQ)
		}
	}, nil, virtioNetPCIIRQ)

	net := virtio.NewNetCore(p.memMap, irq, es)
	transport := virtio.NewLegacyTransport(net.Device())

	end := ioBase + uint16(transport.IOSize()) - 1
	t := transport
	p.io.RegisterRead(ioBase, end, func(port uint16) uint32 {
		return t.IORead(port-ioBase, 1)
	})
	p.io.RegisterWrite(ioBase, end, func(port uint16, val uint32) {
		t.IOWrite(port-ioBase, val, 1)
	})
	p.io.RegisterRead16(ioBase, end-1, func(port uint16) uint32 {
		return t.IORead(port-ioBase, 2)
	})
	p.io.RegisterWrite16(ioBase, end-1, func(port uint16, val uint32) {
		t.IOWrite(port-ioBase, val, 2)
	})
	p.io.RegisterRead32(ioBase, end-3, func(port uint16) uint32 {
		return t.IORead(port-ioBase, 4)
	})
	p.io.RegisterWrite32(ioBase, end-3, func(port uint16, val uint32) {
		t.IOWrite(port-ioBase, val, 4)
	})

	// PCI device: vendor 0x1AF4, transitional net device 0x1000,
	// subsystem 0x0001 (net), class 0x020000 (network controller).
	pciDev := NewPCIDevice("virtio-net", 0x1AF4, 0x1000, 0x020000, 0x00)
	pciDev.SetIRQLine(virtioNetPCIIRQ, 0x01)
	pciDev.SetIOBAR(0, uint32(ioBase), 64)
	pciDev.setU16(0x2C, 0x1AF4) // subsys vendor
	pciDev.setU16(0x2E, 0x0001) // subsys device = net
	p.pciHost.Bus().AddDevice(slot, 0, pciDev)

	p.virtioDevices = append(p.virtioDevices, net.Device())
	p.virtioNetPCICount++
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
