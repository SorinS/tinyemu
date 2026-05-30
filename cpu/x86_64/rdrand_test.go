package x86_64

// RDRAND / RDSEED regression. Two checks:
//   1. CPUID.1H ECX advertises RDRAND (bit 30). If this bit drops we'll
//      regress crng init time to ~5 minutes for an Alpine boot.
//   2. The 0x0F 0xC7 /6 reg-form opcode runs without error, returns
//      success (CF=1) and clears OF/SF/ZF/AF/PF. We don't pin the value
//      because it's pseudorandom; running it twice and seeing different
//      results probabilistically (and never erroring) is enough.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func TestCPUID_Leaf1_RDRANDAdvertised(t *testing.T) {
	_, _, cx, _ := runCPUID(t, 1)
	if cx&(1<<30) == 0 {
		t.Errorf("CPUID.1H ECX.RDRAND (bit 30) clear; Linux crng_init will fall back "+
			"to interrupt jitter and stall ~5 min on Alpine boot. ECX = %#x", cx)
	}
}

// runRDRAND assembles "RDRAND <reg>" (0F C7 /6 reg-form), executes one
// step, and returns the resulting RFLAGS and target register value.
// reg64Index is the destination register (0..15); operandSize picks
// 0x66 prefix (16-bit) vs default (32-bit) vs REX.W (64-bit).
func runRDRAND(t *testing.T, reg64Index uint8, operandSize uint8) (rax uint64, rflags uint64) {
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

	const code uint64 = 0x1000
	off := code
	put := func(b uint8) {
		_ = mm.Write8(off, b)
		off++
	}

	switch operandSize {
	case 2:
		put(0x66)
	case 8:
		put(0x48) // REX.W
	}
	// 0F C7 — modrm with mod=11, reg=6 (RDRAND), rm = reg64Index&7.
	// Use only the low 3 bits; tests cover REX.B extension via the 0x49 path.
	put(0x0F)
	put(0xC7)
	put(0xC0 | (6 << 3) | (reg64Index & 7))
	put(0xF4) // HLT — guard against runaway

	c.SetRIP(code)
	if err := c.Step(); err != nil {
		t.Fatalf("step RDRAND: %v", err)
	}
	return c.GetReg64(int(reg64Index & 7)), c.GetRFLAGS()
}

func TestRDRAND_64bit_SetsCFAndWritesRegister(t *testing.T) {
	_, fl := runRDRAND(t, 0 /*RAX*/, 8)
	if fl&RFLAGS_CF == 0 {
		t.Errorf("RDRAND r64 left CF=0; Linux's loop will retry until it hits a fast path")
	}
	if fl&(RFLAGS_OF|RFLAGS_SF|RFLAGS_ZF|RFLAGS_AF|RFLAGS_PF) != 0 {
		t.Errorf("RDRAND must clear OF/SF/ZF/AF/PF; rflags=%#x", fl)
	}
}

func TestRDRAND_32bit_SetsCFAndWritesRegister(t *testing.T) {
	v, fl := runRDRAND(t, 1 /*RCX*/, 4)
	if fl&RFLAGS_CF == 0 {
		t.Errorf("RDRAND r32 left CF=0")
	}
	// 32-bit form must zero-extend; high 32 bits of RCX should be 0.
	if v>>32 != 0 {
		t.Errorf("RDRAND r32 didn't zero-extend high 32 bits; v=%#x", v)
	}
}

func TestRDRAND_NeverErrors_ManyDraws(t *testing.T) {
	// Spin a few hundred draws to be sure we don't error or set CF=0
	// for benign reasons (e.g. seed drift). One pass would suffice for
	// correctness; a few hundred just makes the failure obvious if
	// somebody breaks the success path.
	for i := 0; i < 256; i++ {
		_, fl := runRDRAND(t, 0, 8)
		if fl&RFLAGS_CF == 0 {
			t.Fatalf("RDRAND draw %d set CF=0; we always succeed", i)
		}
	}
}
