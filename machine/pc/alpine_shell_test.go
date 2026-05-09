package pc

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jtolio/tinyemu-go/cpu/x86"
)

// TestAlpineKernelShell tries to boot the real Alpine vmlinuz and capture
// UART output to see if we get a shell prompt.
func TestAlpineKernelShell(t *testing.T) {
	kernelPath := "../../bin/vmlinuz-alpine-x86"
	initrdPath := "../../bin/initrd-alpine-x86"

	kernelData, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("Alpine kernel not found at %s: %v", kernelPath, err)
	}

	var initrdData []byte
	if d, err := os.ReadFile(initrdPath); err == nil {
		initrdData = d
	}

	p, err := New(Config{
		RAMSize: 128 * 1024 * 1024, // 128MB
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	// Replace UART output with a buffer so we can inspect kernel messages
	var uartBuf bytes.Buffer
	p.uart.SetOutput(&uartBuf)

	if err := p.LoadBIOS(nil, kernelData, initrdData, "console=ttyS0"); err != nil {
		t.Fatalf("LoadBIOS failed: %v", err)
	}

	cpu := p.GetCPU().(*x86.CPU)

	// Run up to 10 million steps or until halt
	maxSteps := 10_000_000
	start := time.Now()
	for i := 0; i < maxSteps && !cpu.IsPowerDown(); i++ {
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d: EIP=0x%08X error: %v", i, cpu.GetEIP(), err)
		}
		// Print output periodically
		if i%1_000_000 == 0 && i > 0 {
			output := uartBuf.String()
			if len(output) > 0 {
				t.Logf("After %d steps (%v): UART output:\n%s", i, time.Since(start), output)
			}
		}
	}

	output := uartBuf.String()
	t.Logf("Final UART output (%d chars):\n%s", len(output), output)

	// Check for signs of life
	if strings.Contains(output, "Linux") {
		t.Logf("Kernel boot messages detected!")
	}
	if strings.Contains(output, "login:") || strings.Contains(output, "~ #") || strings.Contains(output, "localhost") {
		t.Logf("Shell prompt detected!")
	}
}
