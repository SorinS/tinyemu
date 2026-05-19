package x86_64

// Byte-form ALU regression — TinyCorePure64 boot hit "opcode 0x80"
// (Group 1 r/m8, imm8). Adds coverage for the 0x00/0x02/0x04/0x80/
// 0xA8 family plus the byte form of TEST (0x84).
//
// The tests verify both the result bits AND the flag profile, since
// the ALU helpers route through the same aluApply / setArithFlags
// path as the wider forms.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func runBytesAt(t *testing.T, prep func(c *CPU, mm *mem.PhysMemoryMap), bytes []byte) *CPU {
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
	if prep != nil {
		prep(c, mm)
	}
	const base uint64 = 0x1000
	for i, b := range bytes {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Run(50); err != nil {
		t.Fatalf("run: %v", err)
	}
	return c
}

// 0x80 /0 — ADD r/m8, imm8.
func TestGroup1_AddByteImm(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0xFFFFFFFFFFFFFF42)
	}, []byte{0x80, 0xC0, 0x10, 0xF4}) // add al, 0x10
	if got := c.GetReg8(AL); got != 0x52 {
		t.Errorf("AL = %#x, want 0x52", got)
	}
	// Upper bits preserved.
	if c.GetReg64(RAX) != 0xFFFFFFFFFFFFFF52 {
		t.Errorf("RAX = %#x, want upper 56 preserved", c.GetReg64(RAX))
	}
}

// 0x80 /7 — CMP r/m8, imm8.
func TestGroup1_CmpByteImm(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x42)
	}, []byte{0x80, 0xF8, 0x42, 0xF4}) // cmp al, 0x42
	if c.GetReg8(AL) != 0x42 {
		t.Errorf("CMP must not write back; AL = %#x", c.GetReg8(AL))
	}
	if c.rflags&RFLAGS_ZF == 0 {
		t.Errorf("CMP AL, AL didn't set ZF")
	}
}

// 0x00 — ADD r/m8, r8 (byte form of 0x01).
func TestAdd_Byte_Form00(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x10)
		c.SetReg64(RBX, 0x20)
	}, []byte{0x00, 0xC3, 0xF4}) // add bl, al  (rm=BL=011, reg=AL=000)
	if got := c.GetReg8(BL); got != 0x30 {
		t.Errorf("BL = %#x, want 0x30", got)
	}
}

// 0x02 — ADD r8, r/m8.
func TestAdd_Byte_Form02(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x10)
		c.SetReg64(RBX, 0x20)
	}, []byte{0x02, 0xC3, 0xF4}) // add al, bl
	if got := c.GetReg8(AL); got != 0x30 {
		t.Errorf("AL = %#x, want 0x30", got)
	}
}

// 0x04 — ADD AL, imm8.
func TestAdd_AL_Imm(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x10)
	}, []byte{0x04, 0x05, 0xF4})
	if got := c.GetReg8(AL); got != 0x15 {
		t.Errorf("AL = %#x, want 0x15", got)
	}
}

// 0x84 — TEST r/m8, r8.
func TestTEST_Byte(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0xFF)
		c.SetReg64(RBX, 0x80)
	}, []byte{0x84, 0xD8, 0xF4}) // test al, bl
	// 0xFF & 0x80 = 0x80 → ZF=0, SF=1
	if c.rflags&RFLAGS_ZF != 0 {
		t.Errorf("ZF set on non-zero TEST result")
	}
	if c.rflags&RFLAGS_SF == 0 {
		t.Errorf("SF clear despite high bit set")
	}
}

// TestGroup3_TestByteImm — regression for a wrong immediate-width
// bug in opGroup3: the byte form (0xF6 /0) used to fetch 4 bytes of
// immediate (the 32-bit path) instead of 1, consuming 3 extra bytes
// from the instruction stream and silently mis-aligning every
// subsequent fetch. Caught during TinyCorePure64 boot — manifested
// as "Group 5 /7" several instructions later because we'd landed
// inside a JE rel32's disp32.
func TestGroup3_TestByteImm(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x55)
	}, []byte{0xF6, 0xC0, 0x0F, 0xF4, 0xCC}) // f6 c0 0f = test al, 0x0f
	// 0x55 & 0x0F = 5 → not zero, ZF clear.
	if c.rflags&RFLAGS_ZF != 0 {
		t.Errorf("ZF set on test al,0x0f with 0x55 & 0x0f = 5")
	}
	// And the critical invariant: we should have HALTed (HLT at byte
	// offset 3). If imm was over-consumed (4 bytes), we'd have
	// executed the 0xCC at offset 4 as part of imm and not hit HLT.
	if !c.IsPowerDown() {
		t.Errorf("HLT didn't execute — imm width must have been wrong")
	}
}

// 0xA8 — TEST AL, imm8.
func TestTEST_AL_Imm(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x0F)
	}, []byte{0xA8, 0xF0, 0xF4}) // test al, 0xF0
	if c.rflags&RFLAGS_ZF == 0 {
		t.Errorf("ZF clear; 0x0F & 0xF0 should be 0")
	}
	if c.GetReg8(AL) != 0x0F {
		t.Errorf("TEST must not write back; AL = %#x", c.GetReg8(AL))
	}
}

// Verify the no-REX vs REX byte-register encoding works for 0x00 too
// (i.e. the same fix that AH/CH/DH/BH gets in MOV r8 applies here).
func TestAdd_Byte_AH_NoREX(t *testing.T) {
	c := runBytesAt(t, func(c *CPU, _ *mem.PhysMemoryMap) {
		c.SetReg64(RAX, 0x12_3400)
	}, []byte{0x00, 0xE0, 0xF4}) // add al, ah  (reg=AH=100, rm=AL=000)
	// AL ← AL + AH = 0x00 + 0x34 = 0x34.
	if got := c.GetReg8(AL); got != 0x34 {
		t.Errorf("AL = %#x, want 0x34 (AH=0x34 added)", got)
	}
}
