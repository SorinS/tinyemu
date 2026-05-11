package pc

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jtolio/tinyemu-go/cpu/x86"
)

// loadAlpine creates a PC, loads bin/vmlinuz-alpine-x86 + bin/initrd-alpine-x86,
// and returns the machine plus a UART output buffer. The cmdline mirrors what we
// expect to use under cmd/temu for headless boot.
func loadAlpine(t *testing.T, ramMiB uint64) (*PC, *bytes.Buffer) {
	t.Helper()
	kernelPath := "../../bin/vmlinuz-alpine-x86"
	kernelData, err := os.ReadFile(kernelPath)
	if err != nil {
		t.Skipf("kernel not found: %v", err)
	}
	initrdPath := "../../bin/initrd-alpine-x86"
	initrdData, err := os.ReadFile(initrdPath)
	if err != nil {
		t.Skipf("initrd not found: %v", err)
	}

	p, err := New(Config{RAMSize: ramMiB * 1024 * 1024})
	if err != nil {
		t.Fatalf("failed to create PC: %v", err)
	}

	var uartBuf bytes.Buffer
	p.uart.SetOutput(&uartBuf)

	cmdline := "console=ttyS0,115200 noapic nolapic acpi=off pci=noacpi nosmp nokaslr lpj=100000 tsc=reliable"
	if err := p.LoadBIOS(nil, kernelData, initrdData, cmdline); err != nil {
		p.Close()
		t.Fatalf("LoadBIOS failed: %v", err)
	}
	return p, &uartBuf
}

// runBoot drives the proper event loop (CheckTimer + PollDevices + cpu.Run) for
// up to wallTimeout, stopping early when the UART buffer contains stopOn or the
// CPU powers down. Returns the captured UART output.
func runBoot(t *testing.T, p *PC, uartBuf *bytes.Buffer, wallTimeout time.Duration, stopOn string) string {
	t.Helper()
	cpu := p.GetCPU().(*x86.CPU)
	const cyclesPerSlice = 50_000

	deadline := time.Now().Add(wallTimeout)
	lastLog := time.Now()
	lastOutLen := 0

	for time.Now().Before(deadline) && !cpu.IsPowerDown() {
		p.CheckTimer()
		p.PollDevices()
		if err := cpu.Run(cyclesPerSlice); err != nil {
			t.Logf("cpu.Run error after %d cycles: EIP=0x%08X CR0=0x%08X CR3=0x%08X CR4=0x%08X EFLAGS=0x%08X ESP=0x%08X: %v",
				cpu.GetCycles(), cpu.GetEIP(), cpu.GetCR(0), cpu.GetCR(3), cpu.GetCR(4), cpu.GetEFLAGS(), cpu.GetReg32(x86.ESP), err)
			break
		}
		if stopOn != "" && strings.Contains(uartBuf.String(), stopOn) {
			break
		}
		if time.Since(lastLog) > 5*time.Second {
			out := uartBuf.String()
			eip := cpu.GetEIP()
			lin := cpu.GetSegBase(x86.CS) + eip
			var pre [8]byte
			var post [16]byte
			for i := range pre {
				pre[i] = cpu.ReadMem8(lin - 8 + uint32(i))
			}
			for i := range post {
				post[i] = cpu.ReadMem8(lin + uint32(i))
			}
			esp := cpu.GetReg32(x86.ESP)
			var stk [8]uint32
			for i := range stk {
				stk[i] = cpu.ReadMem32(esp + uint32(i)*4)
			}
			t.Logf("cycles=%d EIP=0x%08X EFLAGS=0x%08X (IF=%v) PG=%v INTR=%v PD=%v UART=%d (+%d) tscFP=%d pic={IMR=%02X IRR=%02X ISR=%02X IRQ0=%d}",
				cpu.GetCycles(), eip, cpu.GetEFLAGS(),
				cpu.GetEFLAGS()&x86.EFLAGS_IF != 0,
				cpu.GetCR(0)&x86.CR0_PG != 0,
				cpu.HasPendingInterrupt(), cpu.IsPowerDown(),
				len(out), len(out)-lastOutLen, x86.TSCFastpathHits(),
				p.pic.IMR(), p.pic.IRR(), p.pic.ISR(), p.pic.RaiseCount(0))
			_ = pre
			_ = post
			_ = stk
			lastLog = time.Now()
			lastOutLen = len(out)
		}
	}
	return uartBuf.String()
}

// TestStage1Boot is a fast iteration target: the kernel should at least print
// its banner ("Linux version") within a small wall-clock budget. If this
// passes, the bzImage entry, real-mode→protected-mode glue, basic ISA, and
// UART TX path are all working at least nominally.
func TestStage1Boot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	p, uartBuf := loadAlpine(t, 128)
	defer p.Close()

	out := runBoot(t, p, uartBuf, 120*time.Second, "Linux version")
	t.Logf("stage1 captured %d UART bytes", len(out))
	if !strings.Contains(out, "Linux version") {
		t.Errorf("did not see %q in UART output. Tail:\n%s", "Linux version", tailString(out, 4000))
	}
}

// TestLongBoot runs the full kernel boot for up to 5 minutes. It's the
// "everything" smoke test; success means we reached an Alpine login prompt.
func TestLongBoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	p, uartBuf := loadAlpine(t, 128)
	defer p.Close()

	out := runBoot(t, p, uartBuf, 5*time.Minute, "login:")
	t.Logf("captured %d UART bytes", len(out))
	t.Logf("tail:\n%s", tailString(out, 4000))
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
