package x86

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestPFDebugDefaultOff verifies that pfDebug starts false when the env var
// is unset, which is what protects normal kernel paging activity from
// flooding stderr.
func TestPFDebugDefaultOff(t *testing.T) {
	// At test init, the env var is not set unless the user opted in.
	if os.Getenv("TINYEMU_X86_PF_DEBUG") == "" && pfDebug {
		t.Fatalf("pfDebug is true with TINYEMU_X86_PF_DEBUG unset")
	}
}

// TestPFDebugCanSuppressStderr verifies that with pfDebug=false a page fault
// produces no stderr output. We toggle the package-level pfDebug to false
// regardless of the env so the test result is stable.
func TestPFDebugCanSuppressStderr(t *testing.T) {
	prev := pfDebug
	pfDebug = false
	t.Cleanup(func() { pfDebug = prev })

	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000)
	c.writePhys32(0x10000, 0) // PDE not present

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	defer func() {
		recover() // swallow the PF panic; we only care about stderr
		w.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r)
		if buf.Len() > 0 {
			t.Errorf("pfDebug=false but got stderr: %q", buf.String())
		}
	}()
	_ = c.readMem32(0x1000)
}

// TestPFDebugCanEmit verifies the opposite direction: with pfDebug=true, a
// fault produces a "[PF]" line on stderr.
func TestPFDebugCanEmit(t *testing.T) {
	prev := pfDebug
	pfDebug = true
	t.Cleanup(func() { pfDebug = prev })

	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	c.SetCR(3, 0x10000)
	c.writePhys32(0x10000, 0)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	defer func() {
		recover()
		w.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r)
		s := buf.String()
		if !strings.Contains(s, "[PF]") {
			t.Errorf("pfDebug=true but no [PF] line: %q", s)
		}
	}()
	_ = c.readMem32(0x1000)
}
