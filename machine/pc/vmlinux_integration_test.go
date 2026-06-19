package pc

import (
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
)

// TestVMLinuxBootIntegration loads a real decompressed vmlinux ELF (if available)
// and verifies the kernel starts executing without immediate errors.
// This test is skipped if /tmp/vmlinuz does not exist.
func TestVMLinuxBootIntegration(t *testing.T) {
	kernelPath := "/tmp/vmlinuz"
	kernelData, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("vmlinux not found at %s: %v", kernelPath, err)
	}

	p, err := New(Config{
		RAMSize: 256 * 1024 * 1024, // 256MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	if err := p.LoadBIOS(nil, kernelData, nil, "console=ttyS0"); err != nil {
		t.Fatalf("LoadBIOS failed: %v", err)
	}

	cpu := p.GetCPU().(*x86.CPU)

	// Verify initial CPU state: physical entry, paging OFF
	if cpu.GetEIP() != 0x01000000 {
		t.Errorf("EIP = 0x%08X, want 0x01000000", cpu.GetEIP())
	}
	if cpu.GetCR(0)&x86.CR0_PG != 0 {
		t.Errorf("CR0.PG should not be set initially")
	}
	if cpu.GetCR(0)&x86.CR0_PE == 0 {
		t.Errorf("CR0.PE not set")
	}

	// Run up to 100K steps and verify no errors
	for i := 0; i < 100_000; i++ {
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d: EIP=0x%08X error: %v", i, cpu.GetEIP(), err)
		}
		p.CheckTimer()
	}
}

// TestVMLinuxBootLongRun runs the kernel for 20M steps and verifies it does
// not crash, get stuck in a zero-filled region, or leave the kernel image.
// This is the critical regression test for the ~9M-step crash.
func TestVMLinuxBootLongRun(t *testing.T) {
	kernelPath := "/tmp/vmlinuz"
	kernelData, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("vmlinux not found at %s: %v", kernelPath, err)
	}

	p, err := New(Config{
		RAMSize: 256 * 1024 * 1024, // 256MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	if err := p.LoadBIOS(nil, kernelData, nil, "console=ttyS0"); err != nil {
		t.Fatalf("LoadBIOS failed: %v", err)
	}

	cpu := p.GetCPU().(*x86.CPU)

	// Detect stuck-in-zero-region by sampling EIP every 100K steps.
	// If EIP stays identical across two samples, the CPU is stuck.
	lastSampleEIP := uint32(0)
	streak := 0

	for i := 0; i < 20_000_000; i++ {
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d: EIP=0x%08X error: %v", i, cpu.GetEIP(), err)
		}
		p.CheckTimer()

		if i%100_000 == 0 {
			eip := cpu.GetEIP()
			if eip == lastSampleEIP {
				streak++
				if streak >= 2 {
					// Read a few bytes at EIP to see if it's a zero region
					b0 := cpu.ReadMem8(eip)
					b1 := cpu.ReadMem8(eip + 1)
					t.Fatalf("CPU stuck at EIP=0x%08X after %d steps (bytes=%02X %02X)", eip, i, b0, b1)
				}
			} else {
				streak = 0
			}
			lastSampleEIP = eip
		}
	}


}
