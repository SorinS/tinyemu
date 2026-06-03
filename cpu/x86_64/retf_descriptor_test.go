package x86_64

import (
	"encoding/binary"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestRETF_Compat32To64Long reproduces Linux's PVH boot transition. In
// compat32 (long mode active, CS.L=0) the kernel pushes a long-mode CS
// selector + a 64-bit RIP and `retf`s into long mode. Before the fix
// the non-long-mode RETF path only updated c.seg[CS] for the selector
// and skipped reloading the descriptor cache; recomputeMode then used
// the *stale* L bit from the old selector and we stayed in compat32.
// The kernel's next instruction (assembled for long64 with RIP-
// relative addressing) decoded as 32-bit absolute and faulted on
// reading [disp32] = 0xFFEE1F90 — long before the kernel installs an
// IDT, so the fault couldn't be delivered.
//
// This test sets up the precise GDT shape Linux's PVH stub uses,
// places a `retf` + a long-mode-marker instruction, and confirms the
// CPU lands in ModeLong64 after the RETF.
func TestRETF_Compat32To64Long(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)

	// GDT at 0x80000 — two valid code descriptors:
	//   selector 0x08 → 32-bit code (D=1, L=0) → compat32
	//   selector 0x10 → 64-bit code (L=1)      → long64
	const gdtAddr uint64 = 0x80000
	// Compat32 CS: G=1 D=1 L=0, base=0, limit=0xFFFFF (×4K pages),
	// type=0xA (code, R+E), DPL=0, P=1.
	var compat32Desc uint64 = 0x00CF9A000000FFFF
	// Long64 CS: G=1 D=0 L=1, type=0xA, DPL=0, P=1. The L bit is the
	// load-bearing bit for the mode transition.
	var long64Desc uint64 = 0x00AF9A000000FFFF
	_ = mm.Write64(gdtAddr+0x00, 0)
	_ = mm.Write64(gdtAddr+0x08, compat32Desc)
	_ = mm.Write64(gdtAddr+0x10, long64Desc)

	c.SetSegBase64(GDTR, gdtAddr)
	c.SetSegLimit(GDTR, 0x17)

	// Long mode active (LMA latched) but CS.L=0 → compat32. Set the
	// EFER field directly to skip SetEFER's "need PG to latch LMA"
	// guard; this test only exercises the RETF descriptor reload.
	c.SetCR64(0, CR0_PE)
	c.efer = EFER_LME | EFER_LMA
	c.SetSeg(CS, 0x08)
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegAccess(CS, csDBit) // D=1 L=0 → ModeCompat32
	if c.mode != ModeCompat32 {
		t.Fatalf("setup wrong: c.mode=%v want compat32", c.mode)
	}
	// SS so popStack works.
	c.SetSeg(SS, 0x10)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetSegAccess(SS, csDBit|0x92) // D=1, data RW present

	// Build the RETF stack frame: push CS=0x10, then push EIP=<target>.
	// Compat32 default operand size = 4 bytes; RETF pops in that order
	// (RIP first, then CS).
	const stackTop uint64 = 0x10000
	const retfTarget uint32 = 0x2000 // arbitrary; just a marker RIP
	stackBuf := make([]byte, 8)
	binary.LittleEndian.PutUint32(stackBuf[0:4], retfTarget)
	binary.LittleEndian.PutUint32(stackBuf[4:8], 0x10) // CS selector
	for i, b := range stackBuf {
		_ = mm.Write8(stackTop-8+uint64(i), b)
	}
	c.SetReg64(RSP, stackTop-8)

	// Code: a single RETF (0xCB) at our PC.
	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0xCB)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("RETF step: %v", err)
	}

	if c.mode != ModeLong64 {
		t.Errorf("after RETF c.mode = %v, want long64 — RETF didn't reload "+
			"the CS descriptor, so the L=1 bit of the new selector was lost",
			c.mode)
	}
	if got := c.GetRIP(); got != uint64(retfTarget) {
		t.Errorf("after RETF RIP = %#x, want %#x", got, retfTarget)
	}
	if got := c.seg[CS]; got != 0x10 {
		t.Errorf("after RETF CS selector = %#x, want 0x10", got)
	}
	if c.segAccess[CS]&csLBit == 0 {
		t.Errorf("after RETF segAccess[CS] L bit clear; descriptor not reloaded")
	}
}

// TestRETF_RealModePreservesBigRealAccess pins the original real-mode
// behaviour we must NOT regress: a RETF in real mode rebuilds the base
// from selector<<4 but keeps the cached limit/access intact, so
// big-real-mode survives across far returns. SeaBIOS's transition
// thunks rely on this.
func TestRETF_RealModePreservesBigRealAccess(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)

	// Real mode: PE=0. Pre-load a "big real mode" cached access that
	// must survive the RETF (CS.D=1, limit 4 GiB — the state SeaBIOS
	// thunks set up by briefly toggling through pm32).
	c.SetCR64(0, 0)
	c.SetSeg(CS, 0xF000)
	c.SetSegBase(CS, 0xF0000)
	c.SetSegLimit(CS, 0xFFFFFFFF) // big real
	c.SetSegAccess(CS, csDBit)    // D=1 (big real)

	c.SetSeg(SS, 0)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFF)
	c.SetSegAccess(SS, 0x92)

	// Push CS=0xF000 + IP=0x1234 (16-bit operand size in real mode).
	const stackTop uint64 = 0x1000
	stackBuf := []byte{0x34, 0x12, 0x00, 0xF0}
	for i, b := range stackBuf {
		_ = mm.Write8(stackTop-4+uint64(i), b)
	}
	c.SetReg64(RSP, stackTop-4)

	// RETF at CS:0xF000 base 0xF0000 → linear 0xF0000+IP.
	const codeIP uint64 = 0x0
	c.SetRIP(codeIP)
	_ = mm.Write8(0xF0000+codeIP, 0xCB)

	if err := c.Step(); err != nil {
		t.Fatalf("RETF step: %v", err)
	}

	if c.mode != ModeReal16 {
		t.Errorf("after real-mode RETF, mode = %v, want real16", c.mode)
	}
	if c.segLimit[CS] != 0xFFFFFFFF {
		t.Errorf("CS limit reset to %#x; big-real-mode limit must be preserved", c.segLimit[CS])
	}
	if c.segAccess[CS]&csDBit == 0 {
		t.Errorf("CS.D bit cleared after real-mode RETF; big-real-mode access must survive")
	}
}
