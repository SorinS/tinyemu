package x86_64

import (
	"errors"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// Tests for the 0xEA JMP FAR PTR16:16/32 mode gate.
//
// Regression context: a 0xEA in *compatibility* mode (EFER.LMA=1 but
// CS.L=0) is the exact instruction Linux's startup_32 uses to enter
// 64-bit mode — `ljmp $__KERNEL_CS, $startup_64`, where __KERNEL_CS is a
// descriptor with L=1. For a week the handler gated on EFER.LMA and #UD'd
// that ljmp, so every x86_64 Linux boot died immediately with
// "0xEA JMP FAR is invalid in long mode" (commit f3d9399, fixed here).
// 0xEA is invalid ONLY when actually executing 64-bit code (CS.L=1).

// codeDesc64 is a 64-bit code-segment descriptor (P=1, DPL=0, code,
// L=1, G=1) — the kind __KERNEL_CS points at.
const codeDesc64 = uint64(0x00AF9A000000FFFF)

// codeDesc32 is a 32-bit code-segment descriptor (P=1, DPL=0, code,
// D/B=1, G=1, L=0).
const codeDesc32 = uint64(0x00CF9A000000FFFF)

// newJmpFarCPU returns a CPU with 1 MiB of RAM and a GDT at 0x2000 whose
// selector 0x08 (index 1) is `desc`. Selector 0 is the null descriptor.
func newJmpFarCPU(t *testing.T, desc uint64) (*CPU, *mem.PhysMemoryMap) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	const gdtBase = 0x2000
	c.SetSegBase64(GDTR, gdtBase)
	c.segLimit[GDTR] = 0xFFFF
	// Write the descriptor at selector 0x08 (index 1).
	if err := mm.Write64(gdtBase+8, desc); err != nil {
		t.Fatalf("write GDT desc: %v", err)
	}
	return c, mm
}

// TestJmpFar_CompatToLong64 is the regression test: a 0xEA ljmp executed
// in compatibility mode (LMA=1, CS.L=0) to a 64-bit code selector must
// perform the jump and land the CPU in 64-bit mode — NOT #UD.
func TestJmpFar_CompatToLong64(t *testing.T) {
	c, mm := newJmpFarCPU(t, codeDesc64)

	// Compat mode: long mode active (LMA), but current CS is a 32-bit
	// code segment (D=1, L=0). Flat CS base so the code sits at linear
	// 0x1000.
	// LMA set, but leave CR0.PG clear so instruction fetches are
	// identity-mapped (no page tables needed for the test). Mode
	// detection keys off EFER.LMA + CS.L, not PG.
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetCR64(0, CR0_PE)
	c.SetSegBase64(CS, 0)
	c.SetSegAccess(CS, 0xC9A) // D=1, L=0 → ModeCompat32
	if c.mode != ModeCompat32 {
		t.Fatalf("setup: mode=%v want ModeCompat32", c.mode)
	}
	c.SetRIP(0x1000)

	// ljmp $0x08, $0x00500000  →  EA <off32> <sel16>  (operand size 4 in
	// compat mode).
	writeBytes(t, mm, 0x1000, []byte{0xEA, 0x00, 0x00, 0x50, 0x00, 0x08, 0x00})

	if err := c.Step(); err != nil {
		t.Fatalf("0xEA in compat mode returned error (the f3d9399 regression): %v", err)
	}
	if c.mode != ModeLong64 {
		t.Errorf("after ljmp to 64-bit CS: mode=%v want ModeLong64", c.mode)
	}
	if c.GetSeg(CS) != 0x08 {
		t.Errorf("CS selector=%#x want 0x08", c.GetSeg(CS))
	}
	if c.GetRIP() != 0x00500000 {
		t.Errorf("RIP=%#x want 0x500000", c.GetRIP())
	}
	if c.GetSegBase64(CS) != 0 {
		t.Errorf("CS base=%#x want 0 (flat 64-bit)", c.GetSegBase64(CS))
	}
}

// TestJmpFar_Protected32 — a plain 32-bit protected-mode far jump loads
// CS from the GDT (no long mode involved).
func TestJmpFar_Protected32(t *testing.T) {
	c, mm := newJmpFarCPU(t, codeDesc32)

	c.SetCR64(0, CR0_PE)
	c.SetSegBase64(CS, 0)
	c.SetSegAccess(CS, 0xC9A) // PE=1, D=1 → ModeProtected32
	if c.mode != ModeProtected32 {
		t.Fatalf("setup: mode=%v want ModeProtected32", c.mode)
	}
	c.SetRIP(0x1000)
	writeBytes(t, mm, 0x1000, []byte{0xEA, 0x00, 0x20, 0x00, 0x00, 0x08, 0x00})

	if err := c.Step(); err != nil {
		t.Fatalf("0xEA in pm32 returned error: %v", err)
	}
	if c.mode != ModeProtected32 {
		t.Errorf("after pm32 ljmp: mode=%v want ModeProtected32", c.mode)
	}
	if c.GetSeg(CS) != 0x08 || c.GetRIP() != 0x2000 {
		t.Errorf("CS=%#x RIP=%#x want 0x08:0x2000", c.GetSeg(CS), c.GetRIP())
	}
}

// TestJmpFar_RealMode — real-mode 0xEA loads CS.base = sel<<4 directly.
func TestJmpFar_RealMode(t *testing.T) {
	c, mm := newJmpFarCPU(t, 0)
	// Reset state is real mode at CS=F000:FFF0; redirect to a known spot.
	c.SetSegBase64(CS, 0)
	c.seg[CS] = 0
	c.recomputeMode()
	if c.mode != ModeReal16 {
		t.Fatalf("setup: mode=%v want ModeReal16", c.mode)
	}
	c.SetRIP(0x1000)
	// Real mode: operand size 2 → EA <off16> <sel16>. JMP 0x0200:0x0034.
	writeBytes(t, mm, 0x1000, []byte{0xEA, 0x34, 0x00, 0x00, 0x02})

	if err := c.Step(); err != nil {
		t.Fatalf("0xEA in real mode returned error: %v", err)
	}
	if c.GetSeg(CS) != 0x0200 || c.GetSegBase64(CS) != 0x2000 || c.GetRIP() != 0x34 {
		t.Errorf("real-mode ljmp: CS=%#x base=%#x RIP=%#x want 0x200/0x2000/0x34",
			c.GetSeg(CS), c.GetSegBase64(CS), c.GetRIP())
	}
}

// TestJmpFar_Invalid64Bit — 0xEA is genuinely #UD when already running
// in 64-bit mode (CS.L=1). We surface that as ErrNotImplemented.
func TestJmpFar_Invalid64Bit(t *testing.T) {
	c, mm := newJmpFarCPU(t, codeDesc64)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetCR64(0, CR0_PE)
	c.SetSegBase64(CS, 0)
	c.SetSegAccess(CS, 0x2A9A) // L=1 → ModeLong64
	if c.mode != ModeLong64 {
		t.Fatalf("setup: mode=%v want ModeLong64", c.mode)
	}
	c.SetRIP(0x1000)
	writeBytes(t, mm, 0x1000, []byte{0xEA, 0x00, 0x00, 0x50, 0x00, 0x08, 0x00})

	err := c.Step()
	if err == nil {
		t.Fatal("0xEA in 64-bit mode should #UD, got no error")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("err=%v, want ErrNotImplemented", err)
	}
}
