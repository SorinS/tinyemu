package x86_64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestTSX_XBEGIN_XABORT: without hardware transactional memory, XBEGIN (0xC7 /7)
// must model an immediate abort — EAX = abort status, branch to the fallback at
// (next RIP + rel) — and XABORT (0xC6 /7) outside a transaction is a no-op. This
// is the path real RTM code falls back to (OpenWRT x86 boot hit XBEGIN).
func TestTSX_XBEGIN_XABORT(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE) // 32-bit protected mode
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegAccess(CS, csDBit) // D=1 → 32-bit operand size

	const codeAddr uint64 = 0x1000
	// XBEGIN rel32 = +0x10 : C7 F8 10 00 00 00 (6 bytes) → fallback 0x1006+0x10.
	xbegin := []byte{0xC7, 0xF8, 0x10, 0x00, 0x00, 0x00}
	for i, b := range xbegin {
		_ = mm.Write8(codeAddr+uint64(i), b)
	}
	c.SetRIP(codeAddr)
	c.SetReg64(RAX, 0xDEADBEEF)

	if err := c.Step(); err != nil {
		t.Fatalf("XBEGIN step: %v", err)
	}
	if got := c.GetRIP(); got != codeAddr+6+0x10 {
		t.Errorf("XBEGIN RIP = %#x, want %#x (fallback)", got, codeAddr+6+0x10)
	}
	if got := c.GetReg64(RAX) & 0xFFFFFFFF; got != 0 {
		t.Errorf("XBEGIN EAX = %#x, want 0 (abort status, no retry)", got)
	}

	// XABORT imm8 : C6 F8 2A (3 bytes) — no transaction, so it's a no-op.
	const aAddr uint64 = 0x2000
	for i, b := range []byte{0xC6, 0xF8, 0x2A} {
		_ = mm.Write8(aAddr+uint64(i), b)
	}
	c.SetRIP(aAddr)
	c.SetReg64(RAX, 0x12345)
	if err := c.Step(); err != nil {
		t.Fatalf("XABORT step: %v", err)
	}
	if got := c.GetRIP(); got != aAddr+3 {
		t.Errorf("XABORT RIP = %#x, want %#x", got, aAddr+3)
	}
	if got := c.GetReg64(RAX); got != 0x12345 {
		t.Errorf("XABORT clobbered RAX = %#x, want 0x12345", got)
	}
}
