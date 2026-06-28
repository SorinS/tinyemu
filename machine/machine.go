// Package machine provides the RISC-V machine implementation that ties together
// the CPU, memory, and devices into a complete emulated system.
//
// Reference: TinyEMU riscv_machine.c
package machine

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sorins/tinyemu-go/cpu"
	"github.com/sorins/tinyemu-go/cpu/riscv"
	"github.com/sorins/tinyemu-go/devices"
	"github.com/sorins/tinyemu-go/mem"
	"github.com/sorins/tinyemu-go/virtio"
)

// Memory map constants matching TinyEMU's riscv_machine.c
// Reference: riscv_machine.c lines 65-77
const (
	LowRAMSize  = 0x00010000 // 64KB low RAM (boot code)
	LowRAMAddr  = 0x00000000
	RAMBaseAddr = 0x80000000
	CLINTAddr   = 0x02000000
	CLINTSize   = 0x000C0000
	HTIFAddr    = 0x40008000
	HTIFSize    = 0x00001000 // 4KB for HTIF registers
	UART0Addr   = 0x10000000 // 16550 UART (QEMU "virt" layout; used by bare-metal RTOSes)
	QEMUVirtPLICAddr = 0x0c000000 // PLIC alias for QEMU "virt"-layout guests (e.g. NuttX)
	UART0IRQ         = 10         // QEMU "virt" PLIC source for UART0
	VirtIOAddr  = 0x40010000
	VirtIOSize  = 0x00001000 // Size per VirtIO device
	VirtIOIRQ   = 1          // First VirtIO IRQ number
	PLICAddr    = 0x40100000
	PLICSize    = 0x00400000
	FBAddr      = 0x41000000

	// Timer frequency (10 MHz)
	RTCFreq = 10_000_000
)

// Error definitions
var (
	ErrInvalidXLEN    = errors.New("invalid XLEN value")
	ErrBIOSTooLarge   = errors.New("BIOS image too large for RAM")
	ErrKernelTooLarge = errors.New("kernel image too large for RAM")
	ErrInitrdTooLarge = errors.New("initrd image too large for RAM")
	ErrNoConsole      = errors.New("console device required")
)

// Config holds configuration for creating a new RISC-V machine.
type Config struct {
	// RAM size in bytes (must be page-aligned)
	RAMSize uint64

	// Maximum XLEN (32, 64, or 128)
	MaxXLEN int

	// Console device for serial I/O
	Console *virtio.CharacterDevice

	// RTCDeterministic enables deterministic instruction-counting mode for the
	// timer instead of wall-clock time. This is useful for reproducible tests.
	// When false (the default), real-time mode is used, which is required for
	// normal operation including Linux boot.
	// Reference: tinyemu-2019-12-21/temu.c:818 (always sets rtc_real_time = TRUE)
	RTCDeterministic bool

	// EnableAPIC wires a real local APIC on x86_64 (off by default). See
	// pc.Config.EnableAPIC.
	EnableAPIC bool
}

// Machine represents a complete RISC-V machine with CPU, memory, and devices.
// Reference: riscv_machine.c lines 43-63 (RISCVMachine struct)
type Machine struct {
	// Core components
	memMap *mem.PhysMemoryMap
	cpu    *riscv.CPU

	// RAM regions
	ramSize uint64
	lowRAM  *mem.PhysMemoryRange
	mainRAM *mem.PhysMemoryRange

	// XLEN setting
	maxXLEN int

	// Devices
	clint *devices.CLINT
	plic  *devices.PLIC
	htif  *devices.HTIF
	uart  *devices.NS16550

	// IRQ signals for PLIC
	plicIRQs [devices.PLICMaxIRQ]*mem.IRQSignal

	// VirtIO devices
	virtioDevices []*virtio.Device
	virtioCount   int
	nextVirtIOIRQ int

	// Console
	console    *virtio.CharacterDevice
	consoleDev *virtio.Console

	// Shutdown state
	shutdownRequested bool
	shutdownExitCode  int

	// Real-time clock support (matches C TinyEMU rtc_real_time)
	// Reference: riscv_machine.c lines 90-97
	rtcRealTime  bool      // If true, use wall-clock for mtime
	rtcStartTime time.Time // Wall-clock time at boot (for offset calculation)
}

// New creates a new RISC-V machine with the given configuration.
// Reference: riscv_machine.c lines 829-978 (riscv_machine_init)
func New(cfg Config) (*Machine, error) {
	// Validate XLEN
	var xlen riscv.XLEN
	switch cfg.MaxXLEN {
	case 32:
		xlen = riscv.XLEN32
	case 64:
		xlen = riscv.XLEN64
	case 128:
		xlen = riscv.XLEN128
	default:
		return nil, fmt.Errorf("%w: %d", ErrInvalidXLEN, cfg.MaxXLEN)
	}

	m := &Machine{
		memMap:        mem.NewPhysMemoryMap(),
		ramSize:       cfg.RAMSize,
		maxXLEN:       cfg.MaxXLEN,
		console:       cfg.Console,
		nextVirtIOIRQ: VirtIOIRQ,
	}

	// Create CPU
	m.cpu = riscv.NewCPU(m.memMap, xlen)

	// Optional debug trace: TINYEMU_RISCV_TRACE=1 logs exceptions, illegal
	// instructions, page faults, invalid-CSR access and privilege changes to
	// stderr (the cheap way to see where a guest wedges before any console
	// output). TINYEMU_RISCV_ITRACE=1 additionally traces every instruction
	// (very verbose).
	if os.Getenv("TINYEMU_RISCV_TRACE") != "" {
		ev := riscv.TraceException | riscv.TraceIllegalInsn | riscv.TraceMMUException |
			riscv.TraceInvalidCSR | riscv.TracePrivChange
		if os.Getenv("TINYEMU_RISCV_ITRACE") != "" {
			ev |= riscv.TraceInstruction
		}
		m.cpu.SetTracer(riscv.NewDefaultTracer(os.Stderr, ev))
	}

	// Register RAM regions
	// Low RAM (64KB at 0x0) - for boot code and FDT
	var err error
	m.lowRAM, err = m.memMap.RegisterRAM(LowRAMAddr, LowRAMSize, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to register low RAM: %w", err)
	}

	// Main RAM (at 0x80000000)
	m.mainRAM, err = m.memMap.RegisterRAM(RAMBaseAddr, cfg.RAMSize, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to register main RAM: %w", err)
	}

	// Create and register CLINT
	m.clint = devices.NewCLINT(m.cpu)
	m.clint.SetTimerFrequency(RTCFreq)
	if _, err := m.clint.Register(m.memMap); err != nil {
		return nil, fmt.Errorf("failed to register CLINT: %w", err)
	}

	// Configure real-time mode (default) or deterministic mode
	// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
	// Reference: tinyemu-2019-12-21/temu.c:818 (always sets rtc_real_time = TRUE)
	if !cfg.RTCDeterministic {
		m.rtcRealTime = true
		m.rtcStartTime = time.Now()
		m.clint.SetTimeSource(m)
	}

	// Create and register PLIC
	m.plic = devices.NewPLIC(m.cpu)
	if _, err := m.plic.Register(m.memMap); err != nil {
		return nil, fmt.Errorf("failed to register PLIC: %w", err)
	}
	m.plicIRQs = m.plic.CreateIRQs()
	// Alias the PLIC at the QEMU "virt" address so guests built for that map
	// (NuttX rv-virt) reach the same controller as the FDT-advertised one.
	if _, err := m.plic.RegisterAt(m.memMap, QEMUVirtPLICAddr); err != nil {
		return nil, fmt.Errorf("failed to alias PLIC at %#x: %w", uint64(QEMUVirtPLICAddr), err)
	}
	if os.Getenv("TINYEMU_PLIC_DEBUG") != "" {
		devices.DebugPLIC = true
	}

	// Create and register HTIF
	var htifConsole devices.Console
	if cfg.Console != nil {
		htifConsole = &htifConsoleAdapter{cs: cfg.Console}
	}
	m.htif = devices.NewHTIF(htifConsole)
	m.htif.SetShutdownHandler(func(exitCode int) {
		m.shutdownRequested = true
		m.shutdownExitCode = exitCode
	})
	if _, err := m.htif.Register(m.memMap); err != nil {
		return nil, fmt.Errorf("failed to register HTIF: %w", err)
	}

	// 16550 UART at 0x10000000 (QEMU "virt" layout). Linux/OpenSBI use HTIF
	// above, but bare-metal RTOSes (e.g. NuttX's u16550 driver) talk to this
	// 16550 directly and otherwise spin forever in UART setup polling LSR.THRE.
	if cfg.Console != nil {
		m.uart = devices.NewNS16550(&htifConsoleAdapter{cs: cfg.Console})
		if err := m.uart.Register(m.memMap, UART0Addr); err != nil {
			return nil, fmt.Errorf("failed to register 16550 UART: %w", err)
		}
		// QEMU "virt" wires UART0 to PLIC source 10 (confirmed: NuttX sets PLIC
		// enable bit 10). Connect it so RX interrupts reach the guest.
		m.uart.SetIRQLine(m.plicIRQs[UART0IRQ])
	}

	// Create VirtIO console if console is provided
	if cfg.Console != nil {
		if err := m.addVirtIOConsole(cfg.Console); err != nil {
			return nil, fmt.Errorf("failed to create VirtIO console: %w", err)
		}
	}

	return m, nil
}

// htifConsoleAdapter adapts virtio.CharacterDevice to devices.Console
type htifConsoleAdapter struct {
	cs *virtio.CharacterDevice
}

func (a *htifConsoleAdapter) WriteData(data []byte) {
	if a.cs != nil {
		a.cs.WriteData(data)
	}
}

func (a *htifConsoleAdapter) ReadData(buf []byte) int {
	if a.cs != nil {
		return a.cs.ReadData(buf)
	}
	return 0
}

// addVirtIOConsole adds a VirtIO console device.
func (m *Machine) addVirtIOConsole(cs *virtio.CharacterDevice) error {
	addr := uint64(VirtIOAddr + m.virtioCount*VirtIOSize)
	irq := m.plicIRQs[m.nextVirtIOIRQ]

	console, err := virtio.NewConsole(m.memMap, addr, irq, cs)
	if err != nil {
		return err
	}

	m.consoleDev = console
	m.virtioDevices = append(m.virtioDevices, console.Device())
	m.virtioCount++
	m.nextVirtIOIRQ++

	return nil
}

// AddVirtIODevice adds a VirtIO device and returns the assigned IRQ number.
func (m *Machine) AddVirtIODevice(dev *virtio.Device) (int, error) {
	if m.nextVirtIOIRQ >= devices.PLICMaxIRQ {
		return 0, errors.New("no more PLIC IRQs available")
	}

	m.virtioDevices = append(m.virtioDevices, dev)
	irqNum := m.nextVirtIOIRQ
	m.virtioCount++
	m.nextVirtIOIRQ++

	return irqNum, nil
}

// GetVirtIOAddr returns the address for the next VirtIO device slot.
func (m *Machine) GetVirtIOAddr() uint64 {
	return uint64(VirtIOAddr + m.virtioCount*VirtIOSize)
}

// GetVirtIOIRQ returns the IRQ signal for the next VirtIO device.
func (m *Machine) GetVirtIOIRQ() *mem.IRQSignal {
	if m.nextVirtIOIRQ >= devices.PLICMaxIRQ {
		return nil
	}
	return m.plicIRQs[m.nextVirtIOIRQ]
}

// CPU returns the machine's CPU.
func (m *Machine) CPU() *riscv.CPU {
	return m.cpu
}

// GetCPU returns the machine's CPU as a generic cpu.Core.
func (m *Machine) GetCPU() cpu.Core {
	return m.cpu
}

// MemMap returns the machine's physical memory map.
func (m *Machine) MemMap() *mem.PhysMemoryMap {
	return m.memMap
}

// CLINT returns the machine's CLINT device.
func (m *Machine) CLINT() *devices.CLINT {
	return m.clint
}

// PLIC returns the machine's PLIC device.
func (m *Machine) PLIC() *devices.PLIC {
	return m.plic
}

// HTIF returns the machine's HTIF device.
func (m *Machine) HTIF() *devices.HTIF {
	return m.htif
}

// Console returns the VirtIO console device.
func (m *Machine) Console() *virtio.Console {
	return m.consoleDev
}

// VirtIOCount returns the number of VirtIO devices.
func (m *Machine) VirtIOCount() int {
	return m.virtioCount
}

// RAMSize returns the main RAM size.
func (m *Machine) RAMSize() uint64 {
	return m.ramSize
}

// MaxXLEN returns the maximum XLEN value.
func (m *Machine) MaxXLEN() int {
	return m.maxXLEN
}

// IsShutdownRequested returns true if a shutdown was requested via HTIF.
func (m *Machine) IsShutdownRequested() bool {
	return m.shutdownRequested || m.htif.IsShutdownRequested()
}

// GetShutdownExitCode returns the exit code from shutdown request.
func (m *Machine) GetShutdownExitCode() int {
	if m.shutdownRequested {
		return m.shutdownExitCode
	}
	return m.htif.GetShutdownExitCode()
}

// LoadBIOS loads a BIOS image into RAM at the standard location.
// Also generates the FDT and boot stub code.
// Reference: riscv_machine.c lines 754-816 (copy_bios)
// isELFImage reports whether data begins with the ELF magic.
func isELFImage(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x7f && data[1] == 'E' && data[2] == 'L' && data[3] == 'F'
}

// loadELFSegments loads each PT_LOAD segment of an ELF bios image to its physical
// address within main RAM (ram covers [ramBase, ramBase+ramSize)). The entry is
// required to be ramBase (true for NuttX and OpenSBI-position firmware) — the
// boot stub built below jumps there.
func loadELFSegments(data, ram []byte, ramBase, ramSize uint64) error {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("bios ELF parse: %w", err)
	}
	if f.Entry != ramBase {
		return fmt.Errorf("bios ELF entry %#x != RAM base %#x (unsupported)", f.Entry, ramBase)
	}
	loaded := false
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD || p.Filesz == 0 {
			continue
		}
		if p.Paddr < ramBase || p.Paddr+p.Memsz > ramBase+ramSize {
			return fmt.Errorf("bios ELF segment %#x..%#x outside RAM [%#x,%#x)", p.Paddr, p.Paddr+p.Memsz, ramBase, ramBase+ramSize)
		}
		if p.Off+p.Filesz > uint64(len(data)) {
			return fmt.Errorf("bios ELF segment file range out of bounds")
		}
		copy(ram[p.Paddr-ramBase:], data[p.Off:p.Off+p.Filesz])
		loaded = true
	}
	if !loaded {
		return fmt.Errorf("bios ELF has no loadable segments")
	}
	return nil
}

func (m *Machine) LoadBIOS(biosData []byte, kernelData []byte, initrdData []byte, cmdLine string) error {
	if uint64(len(biosData)) > m.ramSize {
		return ErrBIOSTooLarge
	}

	// Load the BIOS into main RAM. A flat binary (e.g. OpenSBI fw_jump.bin) is
	// copied verbatim and entered at RAMBaseAddr. An ELF image (e.g. NuttX) is
	// loaded by PT_LOAD segment to its physical address — copying the raw ELF
	// would leave the ELF header at the entry point and execute it as garbage.
	ramPtr := m.mainRAM.PhysMem
	if isELFImage(biosData) {
		if err := loadELFSegments(biosData, ramPtr, RAMBaseAddr, m.ramSize); err != nil {
			return err
		}
	} else {
		copy(ramPtr, biosData)
	}

	// Calculate kernel base address (page-aligned after BIOS)
	var kernelBase uint64
	if len(kernelData) > 0 {
		var align uint64
		if m.maxXLEN == 32 {
			align = 4 << 20 // 4 MB page alignment for RV32
		} else {
			align = 2 << 20 // 2 MB page alignment for RV64
		}
		kernelBase = (uint64(len(biosData)) + align - 1) &^ (align - 1)
		if kernelBase+uint64(len(kernelData)) > m.ramSize {
			return ErrKernelTooLarge
		}
		copy(ramPtr[kernelBase:], kernelData)
	}

	// Calculate initrd base address
	var initrdBase uint64
	if len(initrdData) > 0 {
		// Same allocation as QEMU
		initrdBase = m.ramSize / 2
		if initrdBase > 128<<20 {
			initrdBase = 128 << 20
		}
		if initrdBase+uint64(len(initrdData)) > m.ramSize {
			return ErrInitrdTooLarge
		}
		copy(ramPtr[initrdBase:], initrdData)
	}

	// Build FDT in low RAM
	lowRAMPtr := m.lowRAM.PhysMem
	fdtAddr := uint64(0x1000 + 8*8) // After boot code

	fdtSize := m.buildFDT(
		lowRAMPtr[fdtAddr:],
		RAMBaseAddr+kernelBase, uint64(len(kernelData)),
		RAMBaseAddr+initrdBase, uint64(len(initrdData)),
		cmdLine,
	)

	// Build boot stub at 0x1000
	// Reference: riscv_machine.c lines 810-815
	q := lowRAMPtr[0x1000:]
	// auipc t0, jump_addr (0x80000000 - 0x1000)
	binary.LittleEndian.PutUint32(q[0:], 0x297+0x80000000-0x1000)
	// auipc a1, dtb
	binary.LittleEndian.PutUint32(q[4:], 0x597)
	// addi a1, a1, dtb
	binary.LittleEndian.PutUint32(q[8:], 0x58593+uint32((fdtAddr-4)<<20))
	// csrr a0, mhartid
	binary.LittleEndian.PutUint32(q[12:], 0xf1402573)
	// jalr zero, t0, jump_addr
	binary.LittleEndian.PutUint32(q[16:], 0x00028067)

	// Set PC to boot stub
	m.cpu.PC = 0x1000

	_ = fdtSize // FDT size could be used for debugging

	return nil
}

// GetRTCTime returns the current RTC time in ticks.
// In real-time mode, this uses wall-clock time since boot.
// In instruction-counter mode, this uses CPU cycles / RTCFreqDiv.
// Reference: riscv_machine.c lines 90-97 (rtc_get_time)
func (m *Machine) GetRTCTime() uint64 {
	if m.rtcRealTime {
		elapsed := time.Since(m.rtcStartTime)
		// Convert to RTC ticks (10 MHz = 100ns per tick)
		return uint64(elapsed.Nanoseconds() / 100)
	}
	return m.cpu.GetCycles() / devices.RTCFreqDiv
}

// CheckTimer checks and updates the timer interrupt status.
// This should be called periodically from the main emulation loop.
func (m *Machine) CheckTimer() {
	m.clint.CheckTimer()
	m.checkSstc()
}

// checkSstc raises the S-mode timer interrupt (STIP) when the CLINT time has
// reached the guest's stimecmp deadline. The Sstc extension lets S-mode (e.g.
// xv6) drive its own timer via the stimecmp CSR instead of the M-mode CLINT
// mtimecmp; stimecmp==0 means "not armed".
func (m *Machine) checkSstc() {
	if m.cpu.Stimecmp != 0 && m.clint.GetMtime() >= m.cpu.Stimecmp {
		m.cpu.SetMIP(riscv.MipSTIP)
	}
}

// PollDevices polls devices for asynchronous I/O.
// This should be called periodically from the main emulation loop.
func (m *Machine) PollDevices() {
	m.htif.Poll()
}

// Close releases all resources held by the machine.
// Reference: riscv_machine.c:980-987 (riscv_machine_end)
func (m *Machine) Close() {
	m.memMap.Close()
}

// Run executes the CPU for up to maxCycles cycles.
// Reference: riscv_machine.c:1014-1018 (riscv_machine_interp)
func (m *Machine) Run(maxCycles int) error {
	if m.uart != nil {
		m.uart.Poll()
	}
	return m.cpu.Run(maxCycles)
}

// UART returns the 16550 UART so the host stdin reader can push input into it
// (mirrors the x86 COM1 path), or nil if no console is attached.
func (m *Machine) UART() *devices.NS16550 { return m.uart }

// GetSleepDuration calculates how long to sleep before the next event.
// The delay parameter is the maximum sleep time in milliseconds.
// Returns the actual sleep time in milliseconds (0 means run immediately).
// Reference: riscv_machine.c:990-1012 (riscv_machine_get_sleep_duration)
func (m *Machine) GetSleepDuration(delay int) int {
	// Wait for an event: the only asynchronous event is the RTC timer
	if (m.cpu.GetMIP() & devices.MipMTIP) == 0 {
		mtime := m.clint.GetMtime()
		mtimecmp := m.clint.GetMtimecmp()
		if mtime >= mtimecmp {
			m.cpu.SetMIP(devices.MipMTIP)
			delay = 0
		} else {
			// Calculate time until timer fires
			delay1 := mtimecmp - mtime
			// Convert to milliseconds: delay_ms = delay_ticks / (RTCFreq / 1000)
			delay1Ms := int(delay1 / (RTCFreq / 1000))
			if delay1Ms < delay {
				delay = delay1Ms
			}
		}
	}
	// If CPU is not powered down, run immediately
	if !m.cpu.IsPowerDown() {
		delay = 0
	}
	return delay
}
