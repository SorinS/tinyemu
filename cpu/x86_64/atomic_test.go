package x86_64

// XCHG / CMPXCHG / XADD regression — the trio Linux uses for
// atomic primitives. Plus RET imm16, POP r/m, and the LOOPx /
// JCXZ family. All driven by hand-built byte sequences.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func runAtomicProg(t *testing.T, prep func(c *CPU, mm *mem.PhysMemoryMap), prog []byte) *CPU {
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
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Run(100); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return c
}

// XCHG RAX, RBX. REX.W + 0x87 + ModRM 0xC3 (mod=11, reg=000=RAX,
// rm=011=RBX).
func TestXCHG_Reg64(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.SetReg64(RAX, 0xAAAA)
			c.SetReg64(RBX, 0xBBBB)
		},
		[]byte{0x48, 0x87, 0xC3, 0xF4},
	)
	if c.GetReg64(RAX) != 0xBBBB || c.GetReg64(RBX) != 0xAAAA {
		t.Errorf("after xchg: RAX=%#x RBX=%#x", c.GetReg64(RAX), c.GetReg64(RBX))
	}
}

// XCHG r/m8 — verify the REX-aware byte encoding works through the
// new path.
func TestXCHG_Byte_AH_AL(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.SetReg64(RAX, 0x1234) // AL=0x34, AH=0x12
		},
		// 86 E0  xchg ah, al   (mod=11, reg=100=AH, rm=000=AL — no REX so reg=4 means AH)
		[]byte{0x86, 0xE0, 0xF4},
	)
	if got := c.GetReg64(RAX); got != 0x3412 {
		t.Errorf("RAX = %#x, want 0x3412 (AL/AH swapped)", got)
	}
}

// CMPXCHG: match case. AL == [RBX] → write CL, ZF=1.
func TestCMPXCHG_Match(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			_ = mm.Write8(0x2000, 0x42)
			c.SetReg64(RAX, 0x42)
			c.SetReg64(RBX, 0x2000)
			c.SetReg64(RCX, 0x99)
		},
		// 0F B0 0B  cmpxchg [rbx], cl
		[]byte{0x0F, 0xB0, 0x0B, 0xF4},
	)
	v, _ := c.memMap.Read8(0x2000)
	if v != 0x99 {
		t.Errorf("mem after match = %#x, want 0x99", v)
	}
	if c.rflags&RFLAGS_ZF == 0 {
		t.Errorf("ZF clear after CMPXCHG match")
	}
}

// CMPXCHG: mismatch — AL gets the destination, ZF=0.
func TestCMPXCHG_Mismatch(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			_ = mm.Write8(0x2000, 0x42)
			c.SetReg64(RAX, 0x10) // doesn't match
			c.SetReg64(RBX, 0x2000)
			c.SetReg64(RCX, 0x99)
		},
		[]byte{0x0F, 0xB0, 0x0B, 0xF4},
	)
	if c.rflags&RFLAGS_ZF != 0 {
		t.Errorf("ZF set on mismatch")
	}
	if c.GetReg8(AL) != 0x42 {
		t.Errorf("AL = %#x, want 0x42 (loaded from dst)", c.GetReg8(AL))
	}
	// Memory unchanged.
	v, _ := c.memMap.Read8(0x2000)
	if v != 0x42 {
		t.Errorf("mem after mismatch = %#x, want unchanged 0x42", v)
	}
}

// XADD: classic "atomic counter" primitive. Destination += source;
// source = old destination.
func TestXADD(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.SetReg64(RAX, 5)
			c.SetReg64(RBX, 10)
		},
		// 48 0F C1 D8   xadd rax, rbx
		// post: rax_new = 5 (old rax was 5; rbx_new = 5 + 10 = 15)
		// wait: XADD src=rbx (reg field) dst=rax (rm field).
		// dst_new = dst + src = 5 + 10 = 15; src_new = old dst = 5.
		// rax = 15, rbx = 5.
		[]byte{0x48, 0x0F, 0xC1, 0xD8, 0xF4},
	)
	if c.GetReg64(RAX) != 15 {
		t.Errorf("RAX = %d, want 15", c.GetReg64(RAX))
	}
	if c.GetReg64(RBX) != 5 {
		t.Errorf("RBX = %d, want 5", c.GetReg64(RBX))
	}
}

// RET imm16 — pops return then advances RSP by imm16.
func TestRET_Imm16(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			c.SetReg64(RSP, 0x4000)
			_ = mm.Write64(0x4000, 0x5000)        // return addr
			_ = mm.Write8(0x5000, 0xF4)           // HLT at return addr
		},
		// C2 10 00  ret 16
		[]byte{0xC2, 0x10, 0x00},
	)
	// After RET: pop the return → RIP=0x5000, RSP=0x4008.
	// Then RSP += 16 (imm) = 0x4018.
	if c.GetReg64(RSP) != 0x4018 {
		t.Errorf("RSP = %#x, want 0x4018", c.GetReg64(RSP))
	}
	if c.GetRIP() != 0x5001 { // executed HLT, RIP advanced one byte
		t.Errorf("RIP = %#x, want past HLT", c.GetRIP())
	}
}

// POP r/m — Group 1A. ModRM.reg must be 0.
func TestPOP_RM(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			c.SetReg64(RSP, 0x4000)
			_ = mm.Write64(0x4000, 0xDEADBEEFCAFEF00D)
			c.SetReg64(RBX, 0x5000)
		},
		// 8F 03  pop qword [rbx]
		[]byte{0x8F, 0x03, 0xF4},
	)
	v, _ := c.memMap.Read64(0x5000)
	if v != 0xDEADBEEFCAFEF00D {
		t.Errorf("popped to mem = %#x", v)
	}
	if c.GetReg64(RSP) != 0x4008 {
		t.Errorf("RSP = %#x", c.GetReg64(RSP))
	}
}

// LOOP — decrements RCX, branches if RCX != 0.
func TestLOOP(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.SetReg64(RAX, 0)
			c.SetReg64(RCX, 5)
		},
		// loop: 48 FF C0   inc rax
		//       E2 FB      loop -5  (back to inc)
		//       F4         hlt
		[]byte{0x48, 0xFF, 0xC0, 0xE2, 0xFB, 0xF4},
	)
	if c.GetReg64(RAX) != 5 {
		t.Errorf("RAX = %d, want 5", c.GetReg64(RAX))
	}
	if c.GetReg64(RCX) != 0 {
		t.Errorf("RCX = %d, want 0", c.GetReg64(RCX))
	}
}

// JRCXZ — branches if RCX is zero (no decrement).
func TestJRCXZ(t *testing.T) {
	c := runAtomicProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.SetReg64(RCX, 0)
		},
		// E3 02   jrcxz +2
		// 48 ... (skipped if branch taken — use HLT padding)
		// F4 F4  hlt; hlt (target)
		[]byte{0xE3, 0x02, 0xCC, 0xCC, 0xF4},
	)
	// HLT at offset 4 → RIP = 0x1005 after HLT executes.
	if !c.IsPowerDown() {
		t.Errorf("JRCXZ didn't take branch")
	}
}
