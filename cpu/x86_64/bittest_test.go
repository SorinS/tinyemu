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

// TestBTS_MemoryHighBitIndex regression-tests the SDM rule that with
// a MEMORY destination the register-supplied bit index is NOT masked
// to (operand_size - 1) — it extends the memory address. This was
// the root cause of the Linux 6.6 boot stall at the first PIT IRQ:
// `lock bts qword [system_vectors], rax` with rax = 253 was wrongly
// setting bit (253 & 63) = 61 of system_vectors[0] instead of bit
// 253 of the bitmap. The for_each_clear_bit_from loop then saw vec
// 61 as "set" (along with 44, 48-50, 54-56, 58-60, 62, 63 from the
// other APIC vectors that aliased the same way) and skipped
// installing irq_entries_start stubs at idt_table[48..63] — the
// timer IRQ delivered to an empty gate.
func TestBTS_MemoryHighBitIndex(t *testing.T) {
	c := runBytesAt(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			c.SetReg64(RAX, 0x2000) // bitmap base
			c.SetReg64(RBX, 253)    // bit index — high enough to need address adjustment
			for i := 0; i < 64; i++ {
				_ = mm.Write8(0x2000+uint64(i), 0)
			}
		},
		[]byte{0x48, 0x0F, 0xAB, 0x18, 0xF4}, // bts qword [rax], rbx
	)
	// Bit 253 = byte 31 (253/8), bit position 5 (253 mod 8). Mask 0x20.
	wantByteIdx := uint64(31)
	wantMask := byte(1 << 5)
	v, _ := c.memMap.Read8(0x2000 + wantByteIdx)
	if v != wantMask {
		t.Errorf("byte %d = %#x, want %#x (BTS misdirected — likely masked bit-index to 63)",
			wantByteIdx, v, wantMask)
	}
	// Critically: bytes 0..30 of the bitmap must NOT be touched.
	v0, _ := c.memMap.Read64(0x2000)
	if v0 != 0 {
		t.Errorf("bitmap[0..7] = %#x, want 0 — BTS wrapped the bit-index into the wrong qword", v0)
	}
}

// Same test for the negative-bit-index path. The SDM defines the
// behaviour for signed offsets too; not used by Linux but verifies
// the wordIdx/bitInWord normalisation.
func TestBTS_MemoryNegativeBitIndex(t *testing.T) {
	c := runBytesAt(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			c.SetReg64(RAX, 0x2040) // base "above" target
			var negSeven int64 = -7
			c.SetReg64(RBX, uint64(negSeven)) // bit -7 → byte -1 from base, bit 1
			for i := 0; i < 128; i++ {
				_ = mm.Write8(0x2000+uint64(i), 0)
			}
		},
		[]byte{0x48, 0x0F, 0xAB, 0x18, 0xF4}, // bts qword [rax], rbx
	)
	v, _ := c.memMap.Read8(0x2040 - 1)
	if v != (1 << 1) {
		t.Errorf("byte at base-1 = %#x, want %#x", v, byte(1<<1))
	}
}
