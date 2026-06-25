package arm64

import (
	"encoding/binary"
	"testing"

	asmarm64 "github.com/sorins/tinyemu-go/asm/arm64"
	"github.com/sorins/tinyemu-go/mem"
)

// TestFPENTrap covers CPACR_EL1.FPEN gating of Advanced SIMD/FP: with FP enabled
// the instruction executes; with FP disabled it takes an "Access to SIMD/FP"
// synchronous exception (ESR.EC=0x07) to VBAR. This is what seL4's lazy-FPU
// switching (and sel4test FPU0004) relies on.
func TestFPENTrap(t *testing.T) {
	code, err := asmarm64.Assemble("fmov d0, d1")
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	w := binary.LittleEndian.Uint32(code)
	if !isFPSIMD(w) {
		t.Fatalf("isFPSIMD(%#x) = false, expected an FP/SIMD instruction", w)
	}

	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	_ = mm.Write32(0x1000, w)

	// FP enabled (default FPEN=0b11): executes, no trap, PC advances.
	c := New(mm)
	c.VBAR = 0x8000
	c.PC = 0x1000
	if err := c.Step(); err != nil {
		t.Fatalf("FP-enabled step: %v", err)
	}
	if c.PC != 0x1004 {
		t.Fatalf("FP-enabled: PC=%#x, want 0x1004 (executed without trapping)", c.PC)
	}

	// FP disabled at EL0 (FPEN=0b00): traps to VBAR + 0x400 (lower-EL AArch64
	// synchronous vector) with ESR.EC=0x07.
	c = New(mm)
	c.VBAR = 0x8000
	c.CPACR = 0 // FPEN=0b00 → trap
	c.EL = 0
	c.PC = 0x1000
	if err := c.Step(); err != nil {
		t.Fatalf("FP-disabled step: %v", err)
	}
	if c.PC != c.VBAR+0x400 {
		t.Fatalf("FP-disabled: PC=%#x, want %#x (sync trap vector)", c.PC, c.VBAR+0x400)
	}
	if ec := c.ESR >> 26; ec != 0x07 {
		t.Fatalf("ESR.EC=%#x, want 0x07 (Advanced SIMD/FP access)", ec)
	}
}
