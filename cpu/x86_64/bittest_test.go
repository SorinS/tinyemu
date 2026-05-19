package x86_64

// Bit-test (BT/BTS/BTR/BTC) regression. These set CF from the
// tested bit and (for the non-BT variants) modify the bit. The
// kernel uses them heavily for bitmap structures (test_and_set_bit
// in cpumask, bitmap-based allocators, etc.).

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func runBitProg(t *testing.T, prep func(c *CPU), prog []byte) *CPU {
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

// BT r/m, imm8 — CF gets the tested bit. Group 8 sub-op 4.
func TestBitTest_Imm(t *testing.T) {
	// rax = 0x1234 (bit 12 set). bt rax, 12 → CF=1. bt rax, 13 → CF=0.
	c := runBitProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0x1234) },
		[]byte{0x48, 0x0F, 0xBA, 0xE0, 0x0C, 0xF4}, // bt rax, 0x0C
	)
	if c.GetRFLAGS()&RFLAGS_CF == 0 {
		t.Errorf("CF clear after BT of set bit")
	}
	c = runBitProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0x1234) },
		[]byte{0x48, 0x0F, 0xBA, 0xE0, 0x0D, 0xF4}, // bt rax, 0x0D
	)
	if c.GetRFLAGS()&RFLAGS_CF != 0 {
		t.Errorf("CF set after BT of clear bit")
	}
}

// BTS r/m, imm8 — Group 8 sub-op 5. Sets the bit AND copies old to CF.
func TestBTS_ImmSetsBit(t *testing.T) {
	c := runBitProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0) },
		[]byte{0x48, 0x0F, 0xBA, 0xE8, 0x07, 0xF4}, // bts rax, 7
	)
	if c.GetReg64(RAX) != 0x80 {
		t.Errorf("RAX = %#x, want 0x80", c.GetReg64(RAX))
	}
	if c.GetRFLAGS()&RFLAGS_CF != 0 {
		t.Errorf("CF set after BTS of previously-clear bit")
	}
}

// BTR r/m, imm8 — Group 8 sub-op 6. Clears the bit, copies old to CF.
func TestBTR_ImmClearsBit(t *testing.T) {
	c := runBitProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0xFF) },
		[]byte{0x48, 0x0F, 0xBA, 0xF0, 0x03, 0xF4}, // btr rax, 3
	)
	if c.GetReg64(RAX) != 0xF7 {
		t.Errorf("RAX = %#x, want 0xF7", c.GetReg64(RAX))
	}
	if c.GetRFLAGS()&RFLAGS_CF == 0 {
		t.Errorf("CF clear after BTR of set bit")
	}
}

// BTC r/m, imm8 — Group 8 sub-op 7. Toggles the bit, copies old to CF.
func TestBTC_ImmTogglesBit(t *testing.T) {
	c := runBitProg(t,
		func(c *CPU) { c.SetReg64(RAX, 0x100) },
		[]byte{0x48, 0x0F, 0xBA, 0xF8, 0x08, 0xF4}, // btc rax, 8
	)
	if c.GetReg64(RAX) != 0 {
		t.Errorf("RAX = %#x, want 0", c.GetReg64(RAX))
	}
	if c.GetRFLAGS()&RFLAGS_CF == 0 {
		t.Errorf("CF clear after BTC of set bit")
	}
}

// BT r/m, r — 0x0F 0xA3. Bit index in source register.
func TestBT_RegBitIndex(t *testing.T) {
	c := runBitProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 0x8000)
			c.SetReg64(RBX, 15) // bit index
		},
		[]byte{0x48, 0x0F, 0xA3, 0xD8, 0xF4}, // bt rax, rbx
	)
	if c.GetRFLAGS()&RFLAGS_CF == 0 {
		t.Errorf("CF clear after BT rax, rbx (bit 15 set)")
	}
}

// BTC r/m, r — 0x0F 0xBB.
func TestBTC_RegBitIndex(t *testing.T) {
	c := runBitProg(t,
		func(c *CPU) {
			c.SetReg64(RAX, 0)
			c.SetReg64(RBX, 5)
		},
		[]byte{0x48, 0x0F, 0xBB, 0xD8, 0xF4}, // btc rax, rbx
	)
	if c.GetReg64(RAX) != 0x20 {
		t.Errorf("RAX = %#x, want 0x20", c.GetReg64(RAX))
	}
	if c.GetRFLAGS()&RFLAGS_CF != 0 {
		t.Errorf("CF set after BTC of previously-clear bit")
	}
}
