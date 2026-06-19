package x86_64

// ADC/SBB regression. Linux kernel uses these for multi-word arithmetic
// (e.g. 128-bit add via ADD low / ADC high). Failure to thread CF
// would silently corrupt every BigNum / softirq accounting path.

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

func runADCSBBProg(t *testing.T, prep func(c *CPU), prog []byte) *CPU {
	t.Helper()
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
	prep(c)
	const base uint64 = 0x1000
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return c
}

// ADC reg, reg — CF=1 input.
func TestADC_64_WithCF(t *testing.T) {
	c := runADCSBBProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 5)
			c.SetReg64(RBX, 7)
			c.SetRFLAGS(c.GetRFLAGS() | RFLAGS_CF)
		},
		// 48 11 D8  adc rax, rbx
		// F4        hlt
		[]byte{0x48, 0x11, 0xD8, 0xF4},
	)
	if got := c.GetReg64(RAX); got != 13 {
		t.Errorf("ADC RAX, RBX = %d, want 5+7+1=13", got)
	}
}

// SBB reg, reg — CF=1 input.
func TestSBB_64_WithCF(t *testing.T) {
	c := runADCSBBProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 10)
			c.SetReg64(RBX, 3)
			c.SetRFLAGS(c.GetRFLAGS() | RFLAGS_CF)
		},
		// 48 19 D8  sbb rax, rbx
		// F4        hlt
		[]byte{0x48, 0x19, 0xD8, 0xF4},
	)
	if got := c.GetReg64(RAX); got != 6 {
		t.Errorf("SBB RAX, RBX = %d, want 10-3-1=6", got)
	}
}

// Multi-word ADD: 128-bit value via ADD low + ADC high.
// 0xFFFFFFFF_FFFFFFFF + 1 = 0x1_00000000_00000000
func TestMultiwordADD_via_ADC(t *testing.T) {
	c := runADCSBBProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 0xFFFFFFFFFFFFFFFF) // low of a
			c.SetReg64(RBX, 0)                  // high of a
			c.SetReg64(RCX, 1)                  // low of b
			c.SetReg64(RDX, 0)                  // high of b
		},
		// 48 01 C8  add rax, rcx   ; low half, CF set on overflow
		// 48 11 D3  adc rbx, rdx   ; high half + carry
		// F4        hlt
		[]byte{0x48, 0x01, 0xC8, 0x48, 0x11, 0xD3, 0xF4},
	)
	if got := c.GetReg64(RAX); got != 0 {
		t.Errorf("low = %#x, want 0", got)
	}
	if got := c.GetReg64(RBX); got != 1 {
		t.Errorf("high = %#x, want 1 (carry propagated via ADC)", got)
	}
}

// Group 1 /2 = ADC imm, /3 = SBB imm. NASM picks the 0x83 (imm8
// sign-extended) form for small immediates.
func TestADC_Imm8_Group1(t *testing.T) {
	c := runADCSBBProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 100)
			c.SetRFLAGS(c.GetRFLAGS() | RFLAGS_CF)
		},
		// 48 83 D0 05  adc rax, 5  (0x83 with sub-op /2 = ADC, imm8=5)
		// F4
		[]byte{0x48, 0x83, 0xD0, 0x05, 0xF4},
	)
	if got := c.GetReg64(RAX); got != 106 {
		t.Errorf("ADC RAX,5 with CF=1 = %d, want 106", got)
	}
}
