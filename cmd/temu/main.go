// temu is the main TinyEMU RISC-V emulator command.
//
// Usage:
//
//	temu [options] [config.json]
//	temu -m 128 -bios bbl64.bin -kernel vmlinux -drive root.img
//
// Reference: tinyemu-2019-12-21/temu.c:647-835
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jtolio/tinyemu-go/devices"
	"github.com/jtolio/tinyemu-go/machine"
	"github.com/jtolio/tinyemu-go/machine/pc"
	"github.com/jtolio/tinyemu-go/p9"
	"github.com/jtolio/tinyemu-go/virtio"
	"github.com/ulikunitz/xz"
)

const version = "0.1.0"

// stringSlice is a flag.Value that collects multiple string values.
// Used for repeatable flags like -drive and -9p.
type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// Command-line flags
// Reference: tinyemu-2019-12-21/temu.c:617-645 (options array)
var (
	ramSizeMB   = flag.Int("m", 0, "RAM size in MB (overrides config)")
	allowCtrlC  = flag.Bool("ctrlc", false, "allow Ctrl-C to stop emulator")
	driveRW     = flag.Bool("rw", false, "allow disk write access")
	driveRO     = flag.Bool("ro", false, "read-only disk access")
	appendCmd   = flag.String("append", "", "append to kernel command line")
	showHelp    = flag.Bool("h", false, "show help")
	showVersion = flag.Bool("version", false, "show version")

	// New CLI-first flags
	biosPath    = flag.String("bios", "", "path to BIOS/bootloader image")
	kernelPath  = flag.String("kernel", "", "path to kernel image")
	initrdPath  = flag.String("initrd", "", "path to initrd image")
	machineType = flag.String("machine", "riscv64", "machine type (riscv64, riscv32, x86, or x86_64)")
	smpCount    = flag.Int("smp", 1, "number of CPUs (reserved for future use)")
	netUser     = flag.Bool("net-user", false, "enable user-mode networking (slirp)")
	debugMode   = flag.Bool("debug", false, "enable debug output")

	// Repeatable flags
	driveFiles  stringSlice
	cdromFiles  stringSlice
	p9Shares    stringSlice
)

func init() {
	// Register repeatable flags
	flag.Var(&driveFiles, "drive", "block device image file (can be repeated)")
	flag.Var(&cdromFiles, "cdrom", "ISO image attached as an ATAPI CD-ROM (PC only)")
	flag.Var(&p9Shares, "9p", "9P share as path:tag (e.g., /home/user:host, can be repeated)")
}

func main() {
	os.Exit(run())
}

// run contains the main program logic. Separated from main() so that
// deferred cleanup (especially terminal restoration) runs before os.Exit().
// Reference: tinyemu-2019-12-21/temu.c:647-835
func run() int {
	flag.Usage = usage
	flag.Parse()

	if *showHelp {
		usage()
		return 0
	}

	if *showVersion {
		fmt.Printf("temu version %s\n", version)
		return 0
	}

	// Load configuration from file or CLI args
	var cfg *VMConfig
	var err error

	if flag.NArg() >= 1 {
		// Config file provided
		configPath := flag.Arg(0)
		cfg, err = LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			return 1
		}
	} else {
		// No config file - build config from CLI args
		cfg, err = buildConfigFromCLI()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "\nUse -h for help\n")
			return 1
		}
	}

	// Apply command-line overrides (these override both CLI-built and file-based configs)
	// Reference: tinyemu-2019-12-21/temu.c:719-728
	applyCLIOverrides(cfg)

	// Debug output for configuration
	if *debugMode {
		fmt.Fprintf(os.Stderr, "[debug] Configuration:\n")
		fmt.Fprintf(os.Stderr, "[debug]   Machine: %s\n", cfg.Machine)
		fmt.Fprintf(os.Stderr, "[debug]   Memory: %d MB\n", cfg.MemorySize>>20)
		fmt.Fprintf(os.Stderr, "[debug]   BIOS: %s\n", cfg.BIOS)
		fmt.Fprintf(os.Stderr, "[debug]   Kernel: %s\n", cfg.Kernel)
		fmt.Fprintf(os.Stderr, "[debug]   CmdLine: %s\n", cfg.CmdLine)
		fmt.Fprintf(os.Stderr, "[debug]   Drives: %d\n", len(cfg.Drives))
		for i, d := range cfg.Drives {
			fmt.Fprintf(os.Stderr, "[debug]     [%d] %s\n", i, d.File)
		}
	}

	// Determine drive mode from flags
	// Reference: tinyemu-2019-12-21/temu.c:659,673-677
	// Default is snapshot mode (writes to memory, not disk)
	driveMode := devices.ModeSnapshot
	if *driveRW {
		driveMode = devices.ModeReadWrite
	} else if *driveRO {
		driveMode = devices.ModeReadOnly
	}

	// Initialize terminal in raw mode
	term, err := NewTerminal(*allowCtrlC)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing terminal: %v\n", err)
		return 1
	}
	defer term.Restore()

	// Create console device
	console := NewConsoleDevice(term)

	// Create machine configuration
	machineCfg := machine.Config{
		RAMSize: cfg.MemorySize,
		MaxXLEN: 64, // Default to RV64
		Console: console.CharDevice(),
	}

	// Create machine
	m, err := machine.NewBoard(cfg.Machine, machineCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating machine: %v\n", err)
		return 1
	}
	defer m.Close()

	// Open block devices (drives). Each is attached to the machine via
	// whichever native interface the board supports. Boards that
	// implement machine.BlockDeviceAttacher (PC → ATA, ARM-virt-future →
	// virtio-blk-mmio) get the device handed to them directly; legacy
	// boards (RISC-V) fall through to the generic VirtIO-MMIO path.
	// Reference: tinyemu-2019-12-21/temu.c:731-749
	var blockDevs []devices.BlockDevice
	attacher, hasAttacher := m.(machine.BlockDeviceAttacher)
	for _, drive := range cfg.Drives {
		bd, err := openBlockDevice(drive.File, driveMode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening drive %s: %v\n", drive.File, err)
			return 1
		}
		blockDevs = append(blockDevs, bd)

		if hasAttacher {
			if err := attacher.AttachBlockDevice(bd); err != nil {
				fmt.Fprintf(os.Stderr, "Error attaching drive %s: %v\n", drive.File, err)
				return 1
			}
			continue
		}

		// Fallback: VirtIO-MMIO (RISC-V today).
		addr := m.GetVirtIOAddr()
		irq := m.GetVirtIOIRQ()
		virtBlock, err := virtio.NewBlockDevice(m.MemMap(), addr, irq, bd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating VirtIO block device: %v\n", err)
			return 1
		}
		if _, err := m.AddVirtIODevice(virtBlock.Device()); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding VirtIO block device: %v\n", err)
			return 1
		}
	}
	defer func() {
		for _, bd := range blockDevs {
			bd.Close()
		}
	}()

	// CD-ROMs (-cdrom). Routed to the board's CDROMAttacher; the PC
	// implementation creates an ATAPI controller. Each -cdrom flag adds
	// one image, but currently we only model the IDE primary master
	// slot — so a second -cdrom replaces the first.
	cdromAttacher, hasCDROMAttacher := m.(machine.CDROMAttacher)
	for _, path := range cdromFiles {
		if !hasCDROMAttacher {
			fmt.Fprintf(os.Stderr, "-cdrom is not supported on this machine type (%s)\n", *machineType)
			return 1
		}
		// CD-ROMs are read-only regardless of -rw flag.
		bd, err := openBlockDevice(path, devices.ModeReadOnly)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening CD-ROM %s: %v\n", path, err)
			return 1
		}
		blockDevs = append(blockDevs, bd)
		if err := cdromAttacher.AttachCDROM(bd); err != nil {
			fmt.Fprintf(os.Stderr, "Error attaching CD-ROM %s: %v\n", path, err)
			return 1
		}
	}

	// Open filesystems (9P)
	// Reference: tinyemu-2019-12-21/temu.c:751-781
	var p9Devs []*virtio.P9Device
	for _, fs := range cfg.Filesystems {
		hostFS, err := p9.NewHostFS(fs.File)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: must be a directory\n", fs.File)
			return 1
		}

		addr := m.GetVirtIOAddr()
		irq := m.GetVirtIOIRQ()
		p9Dev, err := virtio.NewP9Device(m.MemMap(), addr, irq, hostFS, fs.Tag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating VirtIO 9P device: %v\n", err)
			return 1
		}
		p9Devs = append(p9Devs, p9Dev)
		if _, err := m.AddVirtIODevice(p9Dev.Device()); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding VirtIO 9P device: %v\n", err)
			return 1
		}
	}
	defer func() {
		for _, p9Dev := range p9Devs {
			p9Dev.Close()
		}
	}()

	// Open network devices
	// Reference: tinyemu-2019-12-21/temu.c:783-803
	var ethDevs []*virtio.EthernetDevice
	for _, net := range cfg.Networks {
		es, err := NewNetDevice(net.Driver)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}

		addr := m.GetVirtIOAddr()
		irq := m.GetVirtIOIRQ()
		virtNet, err := virtio.NewNet(m.MemMap(), addr, irq, es)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating VirtIO network device: %v\n", err)
			return 1
		}
		ethDevs = append(ethDevs, es)
		if _, err := m.AddVirtIODevice(virtNet.Device()); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding VirtIO network device: %v\n", err)
			return 1
		}

		// Set carrier state
		// Reference: tinyemu-2019-12-21/temu.c:826-828
		if es.DeviceSetCarrier != nil {
			es.DeviceSetCarrier(true)
		}
	}

	// Enable debug on HTIF and VirtIO console if debug mode is enabled
	if *debugMode {
		if rv, ok := m.(*machine.Machine); ok {
			rv.HTIF().Debug = true
		}
		if virtConsole := m.Console(); virtConsole != nil {
			virtConsole.SetDebug(1)
		}
		virtio.DebugCharDevice = true
		virtio.DebugMMIO = true
	}

	// Load BIOS/kernel/initrd
	if err := loadImages(m, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading images: %v\n", err)
		return 1
	}

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Handle terminal resize (only if we have a terminal)
	setupResizeHandler(term, m)

	// Set initial terminal size (without raising interrupt - driver not ready yet)
	if term != nil {
		if virtConsole := m.Console(); virtConsole != nil {
			w, h := term.GetSize()
			virtConsole.SetSize(w, h)
		}
	}

	// Main emulation loop
	// Reference: tinyemu-2019-12-21/temu.c:830-834
	fmt.Fprintf(os.Stderr, "Press C-a x to exit, C-a h for help\n")

	return runEmulator(m, console, ethDevs, sigCh)
}


// buildConfigFromCLI creates a VMConfig from command-line arguments.
// Returns an error if required arguments are missing.
// Note: This only creates the base config structure. The actual flag values
// (drives, 9p shares, cmdline, etc.) are applied by applyCLIOverrides.
func buildConfigFromCLI() (*VMConfig, error) {
	// Validate machine type
	if *machineType != "riscv64" && *machineType != "riscv32" &&
		*machineType != "x86" && *machineType != "x86_64" {
		return nil, fmt.Errorf("unsupported machine type: %s", *machineType)
	}

	// Require at least a BIOS or kernel
	if *biosPath == "" && *kernelPath == "" {
		return nil, fmt.Errorf("no BIOS or kernel specified (use -bios or -kernel)")
	}

	// Default RAM size if not specified (applyCLIOverrides will override if -m is set)
	memSize := uint64(256) << 20 // Default 256MB

	cfg := &VMConfig{
		Version:    VMConfigVersion,
		Machine:    *machineType,
		MemorySize: memSize,
	}

	return cfg, nil
}

// applyCLIOverrides applies command-line flag overrides to an existing config.
// This handles flags that can override values from a JSON config file.
func applyCLIOverrides(cfg *VMConfig) {
	// Memory size override
	if *ramSizeMB > 0 {
		cfg.MemorySize = uint64(*ramSizeMB) << 20
	}

	// BIOS/kernel/initrd overrides (only if explicitly set)
	if *biosPath != "" {
		cfg.BIOS = *biosPath
	}
	if *kernelPath != "" {
		cfg.Kernel = *kernelPath
	}
	if *initrdPath != "" {
		cfg.Initrd = *initrdPath
	}

	// Append to kernel command line
	if *appendCmd != "" {
		if cfg.CmdLine == "" {
			cfg.CmdLine = *appendCmd
		} else {
			cfg.CmdLine = cfg.CmdLine + " " + *appendCmd
		}
	}

	// Add drives from CLI (appended to config file drives)
	for _, drive := range driveFiles {
		cfg.Drives = append(cfg.Drives, DriveConfig{
			File:   drive,
			Device: "virtio-blk",
		})
	}

	// Add 9P shares from CLI (appended to config file shares)
	existingCount := len(cfg.Filesystems)
	for i, share := range p9Shares {
		parts := strings.SplitN(share, ":", 2)
		path := parts[0]
		tag := ""
		if len(parts) > 1 {
			tag = parts[1]
		}
		if tag == "" {
			idx := existingCount + i
			if idx == 0 {
				tag = "/dev/root"
			} else {
				tag = fmt.Sprintf("/dev/root%d", idx)
			}
		}
		cfg.Filesystems = append(cfg.Filesystems, FSConfig{
			File: path,
			Tag:  tag,
		})
	}

	// Add user networking if requested and not already configured
	if *netUser && len(cfg.Networks) == 0 {
		cfg.Networks = append(cfg.Networks, NetworkConfig{
			Driver: "user",
		})
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "temu version %s - RISC-V Emulator\n", version)
	fmt.Fprintf(os.Stderr, "Copyright (c) 2016-2018 Fabrice Bellard (original TinyEMU)\n")
	fmt.Fprintf(os.Stderr, "\nUsage:\n")
	fmt.Fprintf(os.Stderr, "  temu [options] config.json     # Load config from JSON file\n")
	fmt.Fprintf(os.Stderr, "  temu [options] -bios FILE      # Run with CLI arguments only\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  temu -m 128 -bios bbl64.bin -kernel vmlinux -drive root.img\n")
	fmt.Fprintf(os.Stderr, "  temu -bios kernel.bin -append 'console=hvc0 root=/dev/vda' -drive disk.img\n")
	fmt.Fprintf(os.Stderr, "  temu -m 256 -kernel vmlinux -9p /home/user:host -net-user\n")
	fmt.Fprintf(os.Stderr, "\nOptions:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nConsole keys:\n")
	fmt.Fprintf(os.Stderr, "  C-a h   print help\n")
	fmt.Fprintf(os.Stderr, "  C-a x   exit emulator\n")
	fmt.Fprintf(os.Stderr, "  C-a C-a send C-a\n")
}

// loadImages loads BIOS, kernel, and initrd images into the machine.
// Supports .xz compressed images which are decompressed automatically.
func loadImages(m machine.Board, cfg *VMConfig) error {
	var biosData, kernelData, initrdData []byte
	var err error

	// Load BIOS
	if cfg.BIOS != "" {
		biosData, err = loadImageFile(cfg.BIOS)
		if err != nil {
			return fmt.Errorf("failed to read BIOS: %w", err)
		}
	}

	// Load kernel
	if cfg.Kernel != "" {
		kernelData, err = loadImageFile(cfg.Kernel)
		if err != nil {
			return fmt.Errorf("failed to read kernel: %w", err)
		}
	}

	// Load initrd
	if cfg.Initrd != "" {
		initrdData, err = loadImageFile(cfg.Initrd)
		if err != nil {
			return fmt.Errorf("failed to read initrd: %w", err)
		}
	}

	// For x86, keep kernel separate so the PC board can attempt bzImage direct boot.
	// For RISC-V, if no BIOS but kernel is provided, kernel becomes the BIOS.
	if len(biosData) == 0 && len(kernelData) > 0 {
		if cfg.Machine == "x86" || cfg.Machine == "x86_64" {
			// Keep kernelData intact for bzImage loader
		} else {
			biosData = kernelData
			kernelData = nil
		}
	}

	if len(biosData) == 0 && len(kernelData) == 0 {
		return fmt.Errorf("no BIOS or kernel specified")
	}

	return m.LoadBIOS(biosData, kernelData, initrdData, cfg.CmdLine)
}

// loadImageFile loads a file, decompressing it if it has an .xz extension.
func loadImageFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Decompress if .xz
	if strings.HasSuffix(path, ".xz") {
		fmt.Fprintf(os.Stderr, "Decompressing %s...\n", filepath.Base(path))
		r, err := xz.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to create xz reader: %w", err)
		}
		decompressed, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("decompression failed: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Decompressed %s (%d MB)\n", filepath.Base(path), len(decompressed)/(1024*1024))
		return decompressed, nil
	}

	return data, nil
}

// openBlockDevice opens a block device with the specified mode.
// For snapshot mode, wraps a read-only file device with SnapshotBlockDevice.
// Supports .xz compressed images which are decompressed to memory.
// Reference: tinyemu-2019-12-21/temu.c:307-347 (block_device_init)
func openBlockDevice(path string, mode devices.BlockDeviceMode) (devices.BlockDevice, error) {
	// Handle .xz compressed images - decompress to memory
	if strings.HasSuffix(path, ".xz") {
		return openCompressedBlockDevice(path, mode)
	}

	if mode == devices.ModeSnapshot {
		// For snapshot mode, open read-only and wrap with copy-on-write
		// Reference: tinyemu-2019-12-21/temu.c:337-340
		fileDev, err := devices.OpenFileBlockDevice(path, devices.ModeReadOnly)
		if err != nil {
			return nil, err
		}
		return devices.NewSnapshotBlockDevice(fileDev), nil
	}
	return devices.OpenFileBlockDevice(path, mode)
}

// openCompressedBlockDevice opens an xz-compressed block device image.
// The image is decompressed to memory. Since the decompressed data is
// ephemeral (in RAM), writes go directly to memory without COW overhead.
func openCompressedBlockDevice(path string, mode devices.BlockDeviceMode) (devices.BlockDevice, error) {
	// Read compressed data
	compressedData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compressed image: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Decompressing %s...\n", filepath.Base(path))

	// Decompress
	r, err := xz.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, fmt.Errorf("failed to create xz reader: %w", err)
	}

	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompression failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Decompressed %s (%d MB)\n", filepath.Base(path), len(decompressed)/(1024*1024))

	// Create memory block device - writes go directly to RAM
	memDev := devices.NewMemoryBlockDeviceFromData(decompressed)
	if mode == devices.ModeReadOnly {
		memDev.SetReadOnly(true)
	}
	return memDev, nil
}

// runEmulator runs the main emulation loop.
// Reference: tinyemu-2019-12-21/temu.c:830-834 (infinite virt_machine_run loop)
// Returns exit code.
func runEmulator(m machine.Board, console *ConsoleDevice, ethDevs []*virtio.EthernetDevice, sigCh <-chan os.Signal) int {
	const maxExecCycle = 500000
	const maxSleepTime = 10 * time.Millisecond

	cpu := m.GetCPU()

	for {
		// Check for signals
		select {
		case <-sigCh:
			fmt.Fprintf(os.Stderr, "\nTerminated by signal\n")
			return 0
		default:
		}

		// Check for shutdown request
		if m.IsShutdownRequested() {
			return m.GetShutdownExitCode()
		}

		// Check for console escape sequence
		if console.ShouldExit() {
			fmt.Fprintf(os.Stderr, "\nTerminated\n")
			return 0
		}

		// Check timer interrupt
		m.CheckTimer()

		// Poll devices
		m.PollDevices()

		// Poll network devices (slirp)
		// Reference: tinyemu-2019-12-21/temu.c:445-460 (virt_machine_run select loop)
		for _, es := range ethDevs {
			NetPoll(es)
		}

		// Read console input and process escape sequences (C-a x, etc.)
		// Reference: tinyemu-2019-12-21/temu.c:554-591
		// The C version only reads from stdin when virtio_console_can_write_data()
		// returns true, which keeps early input buffered in the OS. Since we must
		// always read to process escape sequences, we buffer input explicitly.
		inputBuf := make([]byte, 256)
		n, _ := console.Read(inputBuf)

		// For x86 PC boards, route stdin into the COM1 RX FIFO so the
		// kernel's serial driver delivers it to userspace.
		if pcBoard, ok := m.(*pc.PC); ok && n > 0 {
			pcBoard.UART().Push(inputBuf[:n])
		}

		// Feed console input to guest if available and guest is ready
		if virtConsole := m.Console(); virtConsole != nil && virtConsole.CanWriteData() {
			writeLen := virtConsole.GetWriteLen()

			// First, try to flush any previously buffered input
			if buffered := console.GetBufferedInput(writeLen); len(buffered) > 0 {
				written := virtConsole.WriteData(buffered)
				writeLen -= written
			}

			// Then send new input if we have room
			if n > 0 && writeLen > 0 {
				if n > writeLen {
					// Can only send part of it, buffer the rest
					virtConsole.WriteData(inputBuf[:writeLen])
					console.BufferInput(inputBuf[writeLen:n])
				} else {
					virtConsole.WriteData(inputBuf[:n])
				}
			} else if n > 0 {
				// No room, buffer all new input
				console.BufferInput(inputBuf[:n])
			}
		} else if n > 0 {
			// Guest not ready, buffer input for later
			console.BufferInput(inputBuf[:n])
		}

		// Check if CPU is waiting for interrupt
		if cpu.IsPowerDown() {
			// CPU is in WFI - check if any interrupts are pending
			if cpu.HasPendingInterrupt() {
				cpu.SetPowerDown(false)
			} else {
				// For x86 PC boards, fast-forward the PIT while
				// the CPU is halted so the next timer tick can
				// wake it. Without this, HLT deadlocks because
				// the PIT only advances on `CheckTimer` calls,
				// which are gated on CPU cycles in `pc.go`.
				if pcBoard, ok := m.(*pc.PC); ok {
					pcBoard.AdvanceIdle()
					continue
				}
				// Sleep a bit to avoid busy waiting (RISC-V WFI path)
				time.Sleep(maxSleepTime)
				continue
			}
		}

		// Execute instructions
		if err := cpu.Run(maxExecCycle); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR in Run: %v\n", err)
			return 1
		}
	}
}
