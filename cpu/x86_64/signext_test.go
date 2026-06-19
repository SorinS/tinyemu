package x86_64

// CBW/CWDE/CDQE (0x98) + CWD/CDQ/CQO (0x99) regression. Both sign-
// extend in place: 0x98 narrows RAX (AL→AX, AX→EAX, EAX→RAX) using
// REX.W to pick the destination width; 0x99 spreads the sign of
// RAX into RDX so the next IDIV has a properly sign-extended
// 128-bit dividend.

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

func runSignExtProg(t *testing.T, prep func(c *CPU), prog []byte) *CPU {
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
	if err := c.Run(20); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return c
}

// CBW: AX = sign-extend(AL).
func TestCBW(t *testing.T) {
	c := runSignExtProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0xFFFFFFFFFFFFFF80) },
		[]byte{0x66, 0x98, 0xF4}, // 66 prefix + 98 = CBW
	)
	if got := c.GetReg16(AX); got != 0xFF80 {
		t.Errorf("AX = %#x, want 0xFF80", got)
	}
}

// CDQE: RAX = sign-extend(EAX).
func TestCDQE(t *testing.T) {
	c := runSignExtProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0xFFFFFFFF) }, // EAX = -1
		[]byte{0x48, 0x98, 0xF4},                     // REX.W + 98
	)
	if got := c.GetReg64(RAX); got != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("RAX after CDQE of -1 = %#x", got)
	}
}

// CQO: RDX = sign-extension of RAX.
func TestCQO(t *testing.T) {
	c := runSignExtProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 0x8000000000000000) // negative
			c.SetReg64(RDX, 0)
		},
		[]byte{0x48, 0x99, 0xF4}, // REX.W + 99 = CQO
	)
	if got := c.GetReg64(RDX); got != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("RDX after CQO of negative = %#x", got)
	}
}

// CDQ: EDX = sign-extension of EAX. Upper 32 of RDX zero-extended.
func TestCDQ_Positive(t *testing.T) {
	c := runSignExtProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 0x12345678)
			c.SetReg64(RDX, 0xFFFFFFFFFFFFFFFF)
		},
		[]byte{0x99, 0xF4},
	)
	if got := c.GetReg64(RDX); got != 0 {
		t.Errorf("RDX after CDQ of positive = %#x, want 0 (upper zero-ext)", got)
	}
}
