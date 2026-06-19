// vmlinux-step is a headless x86 vmlinux runner for observation and debugging.
// It loads a decompressed vmlinux ELF and steps the CPU without timer interrupts,
// avoiding the unimplemented interrupt-delivery path that crashes temu.
//
// Usage:
//
//	vmlinux-step -kernel /tmp/vmlinuz [-steps 20000000] [-sample 100000]
//
// Press Ctrl-C to stop and see final register state.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sorins/tinyemu-go/cpu/x86"
	"github.com/sorins/tinyemu-go/machine/pc"
)

func main() {
	var (
		kernelPath = flag.String("kernel", "/tmp/vmlinuz", "path to decompressed vmlinux ELF")
		initrdPath = flag.String("initrd", "", "path to initrd (optional)")
		cmdLine    = flag.String("cmdline", "console=ttyS0,115200n8", "kernel command line")
		maxSteps   = flag.Uint64("steps", 20_000_000, "maximum steps to run (0 = unlimited)")
		sampleRate = flag.Uint64("sample", 100_000, "print progress every N steps")
		ramMB      = flag.Int("m", 256, "RAM size in MB")
	)
	flag.Parse()

	kernelData, err := os.ReadFile(*kernelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading kernel: %v\n", err)
		os.Exit(1)
	}

	var initrdData []byte
	if *initrdPath != "" {
		initrdData, err = os.ReadFile(*initrdPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading initrd: %v\n", err)
			os.Exit(1)
		}
	}

	p, err := pc.New(pc.Config{
		RAMSize: uint64(*ramMB) * 1024 * 1024,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating PC: %v\n", err)
		os.Exit(1)
	}
	defer p.Close()

	if err := p.LoadBIOS(nil, kernelData, initrdData, *cmdLine); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading kernel: %v\n", err)
		os.Exit(1)
	}

	cpu := p.GetCPU().(*x86.CPU)

	fmt.Fprintf(os.Stderr, "Loaded %s (%d bytes)\n", *kernelPath, len(kernelData))
	fmt.Fprintf(os.Stderr, "RAM: %d MB | Max steps: %d | Sample every: %d\n", *ramMB, *maxSteps, *sampleRate)
	fmt.Fprintf(os.Stderr, "Press Ctrl-C to stop\n\n")

	// Handle Ctrl-C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var step uint64
	var lastEIP uint32
	var stuckCount int

	for {
		// Check for Ctrl-C
		select {
		case <-sigCh:
			fmt.Fprintf(os.Stderr, "\nStopped at step %d\n", step)
			printState(cpu)
			os.Exit(0)
		default:
		}

		if *maxSteps > 0 && step >= *maxSteps {
			fmt.Fprintf(os.Stderr, "\nReached max steps (%d)\n", *maxSteps)
			printState(cpu)
			os.Exit(0)
		}

		if err := cpu.Step(); err != nil {
			fmt.Fprintf(os.Stderr, "\nERROR at step %d: %v\n", step, err)
			printState(cpu)
			os.Exit(1)
		}
		step++

		if step%*sampleRate == 0 {
			eip := cpu.GetEIP()
			fmt.Fprintf(os.Stderr, "step %10d: EIP=%08X EAX=%08X ESP=%08X\n",
				step, eip, cpu.GetReg32(x86.EAX), cpu.GetReg32(x86.ESP))

			// Stuck detection: same EIP for multiple samples
			if eip == lastEIP {
				stuckCount++
				if stuckCount >= 5 {
					fmt.Fprintf(os.Stderr, "CPU appears stuck at EIP=%08X for %d consecutive samples\n", eip, stuckCount)
					printState(cpu)
					os.Exit(1)
				}
			} else {
				stuckCount = 0
				lastEIP = eip
			}
		}
	}
}

func printState(cpu *x86.CPU) {
	fmt.Fprintf(os.Stderr, "\nFinal CPU state:\n")
	fmt.Fprintf(os.Stderr, "  EIP=%08X  EAX=%08X  EBX=%08X  ECX=%08X  EDX=%08X\n",
		cpu.GetEIP(), cpu.GetReg32(x86.EAX), cpu.GetReg32(x86.EBX),
		cpu.GetReg32(x86.ECX), cpu.GetReg32(x86.EDX))
	fmt.Fprintf(os.Stderr, "  ESI=%08X  EDI=%08X  EBP=%08X  ESP=%08X\n",
		cpu.GetReg32(x86.ESI), cpu.GetReg32(x86.EDI),
		cpu.GetReg32(x86.EBP), cpu.GetReg32(x86.ESP))
	fmt.Fprintf(os.Stderr, "  EFLAGS=%08X  CR0=%08X  CR3=%08X  CR4=%08X\n",
		cpu.GetEFLAGS(), cpu.GetCR(0), cpu.GetCR(3), cpu.GetCR(4))
	fmt.Fprintf(os.Stderr, "  CS=%04X base=%08X  DS=%04X base=%08X\n",
		cpu.GetSeg(x86.CS), cpu.GetSegBase(x86.CS),
		cpu.GetSeg(x86.DS), cpu.GetSegBase(x86.DS))

	// Disassemble a few bytes at EIP
	eip := cpu.GetEIP()
	fmt.Fprintf(os.Stderr, "\nBytes at EIP:\n")
	for addr := eip; addr < eip+32; addr += 16 {
		var b [16]byte
		for i := 0; i < 16; i++ {
			b[i] = cpu.ReadMem8(addr + uint32(i))
		}
		fmt.Fprintf(os.Stderr, "  %08X: % x\n", addr, b[:])
	}
}
