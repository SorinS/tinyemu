package pc

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/jtolio/tinyemu-go/cpu/x86"
)

func TestLongBoot(t *testing.T) {
	kernelPath := "../../bin/vmlinuz-alpine-x86"
	kernelData, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("kernel not found: %v", err)
	}

	p, err := New(Config{
		RAMSize: 128 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}
	defer p.Close()

	var uartBuf bytes.Buffer
	p.uart.SetOutput(&uartBuf)

	if err := p.LoadBIOS(nil, kernelData, nil, "console=ttyS0"); err != nil {
		t.Fatalf("LoadBIOS failed: %v", err)
	}

	cpu := p.GetCPU().(*x86.CPU)

	start := time.Now()
	maxSteps := 500_000_000
	for i := 0; i < maxSteps && !cpu.IsPowerDown(); i++ {
		if err := cpu.Step(); err != nil {
			t.Logf("step %d: EIP=0x%08X error: %v", i, cpu.GetEIP(), err)
			break
		}
		if i%50_000_000 == 0 && i > 0 {
			output := uartBuf.String()
			t.Logf("step %d (%v): EIP=0x%08X PG=%v UART=%d chars",
				i, time.Since(start), cpu.GetEIP(),
				cpu.GetCR(0)&x86.CR0_PG != 0, len(output))
		}
	}

	output := uartBuf.String()
	t.Logf("Final output (%d chars): %s", len(output), output)
}
