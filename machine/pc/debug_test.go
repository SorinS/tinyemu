package pc

import (
	"bytes"
	"os"
	"testing"

	"github.com/jtolio/tinyemu-go/cpu/x86"
)

func TestAlpineKernelDebug(t *testing.T) {
	kernelPath := "../../bin/vmlinuz-alpine-x86"
	initrdPath := "../../bin/initrd-alpine-x86"

	kernelData, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("Alpine kernel not found: %v", err)
	}
	var initrdData []byte
	if d, err := os.ReadFile(initrdPath); err == nil {
		initrdData = d
	}

	p, err := New(Config{RAMSize: 128 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var uartBuf bytes.Buffer
	p.uart.SetOutput(&uartBuf)

	if err := p.LoadBIOS(nil, kernelData, initrdData, "console=ttyS0"); err != nil {
		t.Fatal(err)
	}

	cpu := p.GetCPU().(*x86.CPU)

	for i := 0; i < 598_899_905; i++ {
		p.CheckTimer()
		if err := cpu.Step(); err != nil {
			t.Fatalf("step %d: EIP=0x%08X error: %v", i, cpu.GetEIP(), err)
		}
	}
	
	addrs := []uint32{0xC105E780, 0xC105E789, 0xC105E78A, 0xC1CEC5CE, 0xC1CEC5C8}
	for _, addr := range addrs {
		t.Logf("Linear 0x%08X: %02X %02X %02X %02X %02X %02X %02X %02X",
			addr,
			cpu.ReadMem8(addr), cpu.ReadMem8(addr+1),
			cpu.ReadMem8(addr+2), cpu.ReadMem8(addr+3),
			cpu.ReadMem8(addr+4), cpu.ReadMem8(addr+5),
			cpu.ReadMem8(addr+6), cpu.ReadMem8(addr+7))
	}
}
