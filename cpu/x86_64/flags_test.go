package x86_64

// PUSHF/POPF/SAHF/LAHF regression — TinyCorePure64 boot hit "opcode
// 0x9d rex=0x0" (POPFQ). The kernel uses PUSHF/POPF to save/restore
// the interrupt state around critical sections; LAHF/SAHF are
// occasionally used by older code paths to read/write the low byte
// of RFLAGS through AH.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func TestPUSHFQ_POPFQ_RoundTrip(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.reg64[RSP] = 0x8000
	// Seed RFLAGS with a set of bits the kernel cares about.
	c.SetRFLAGS(RFLAGS_IF | RFLAGS_DF | RFLAGS_PF | RFLAGS_CF)
	original := c.GetRFLAGS()

	// 9C  pushfq
	// 31 C0   xor eax, eax  (clears CF + PF, doesn't touch IF/DF directly
	//                       but we re-pop anyway to demonstrate restoration)
	// 9D  popfq
	// F4  hlt
	const base uint64 = 0x1000
	prog := []byte{0x9C, 0x31, 0xC0, 0x9D, 0xF4}
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// After PUSHFQ + XOR + POPFQ, RFLAGS must be back to the original.
	if c.GetRFLAGS() != original {
		t.Errorf("RFLAGS after round-trip = %#x, want %#x", c.GetRFLAGS(), original)
	}
}

func TestPOPFQ_FiltersReservedAndRF(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.reg64[RSP] = 0x8000
	// Stash an "all bits set" word on top of stack.
	c.reg64[RSP] = 0x7FF8
	_ = mm.Write64(0x7FF8, 0xFFFFFFFFFFFFFFFF)
	// 9D popfq; F4 hlt
	const base uint64 = 0x1000
	_ = mm.Write8(base, 0x9D)
	_ = mm.Write8(base+1, 0xF4)
	c.SetRIP(base)
	if err := c.Run(10); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Bits outside ValidFlagMask | bit-1 must be zero. RF must be
	// cleared even if the popped word had it set.
	if c.GetRFLAGS()&RFLAGS_RF != 0 {
		t.Errorf("RF set after POPF; should be cleared")
	}
	if c.GetRFLAGS() &^ (ValidFlagMask | 2) != 0 {
		t.Errorf("RFLAGS has bits outside ValidFlagMask: %#x", c.GetRFLAGS())
	}
}

func TestSAHF_LAHF(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	// SAHF then LAHF round-trip in a single program. SAHF AH=0xD5
	// sets SF/ZF/AF/PF/CF; LAHF reads them back as 0xD7 (the five
	// flags plus the always-1 bit at position 1).
	c.SetReg8(AH, 0xD5)
	// 9E SAHF; B4 00 mov ah, 0; 9F LAHF; F4 HLT
	const base uint64 = 0x1000
	prog := []byte{0x9E, 0xB4, 0x00, 0x9F, 0xF4}
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantFlags := uint64(RFLAGS_SF | RFLAGS_ZF | RFLAGS_AF | RFLAGS_PF | RFLAGS_CF)
	if c.GetRFLAGS()&wantFlags != wantFlags {
		t.Errorf("SAHF didn't set all five flags; rflags=%#x", c.GetRFLAGS())
	}
	if got := c.GetReg8(AH); got != 0xD7 {
		t.Errorf("AH after LAHF = %#x, want 0xD7", got)
	}
}
