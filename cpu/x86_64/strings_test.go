package x86_64

// String-op regression tests. The TinyCorePure64 boot run hit
// "opcode 0xa5 rex=0x48" — REX.W MOVSQ, used by the kernel's
// memcpy fast path. The tests cover MOVS/STOS/LODS/SCAS at the
// widths the boot path exercises (REP MOVSQ for memcpy, REP STOSQ
// for memset, LODS in early-printk loops, SCAS for strlen).

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func newLongMode(t *testing.T) (*CPU, *mem.PhysMemoryMap) {
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
	return c, mm
}

func TestRepMOVSQ_Memcpy(t *testing.T) {
	c, mm := newLongMode(t)
	// Source 0x2000..0x2027 = 5 qwords (0xA0,A1,...A4).
	for i := uint64(0); i < 5; i++ {
		_ = mm.Write64(0x2000+i*8, 0xA0+i)
	}
	c.SetReg64(RSI, 0x2000)
	c.SetReg64(RDI, 0x3000)
	c.SetReg64(RCX, 5)
	// F3 48 A5   rep movsq
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0xF3)
	_ = mm.Write8(code+1, 0x48)
	_ = mm.Write8(code+2, 0xA5)
	_ = mm.Write8(code+3, 0xF4)
	c.SetRIP(code)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := uint64(0); i < 5; i++ {
		got, _ := mm.Read64(0x3000 + i*8)
		if got != 0xA0+i {
			t.Errorf("dst[%d] = %#x, want %#x", i, got, 0xA0+i)
		}
	}
	if c.GetReg64(RCX) != 0 {
		t.Errorf("RCX = %d, want 0 (REP terminator)", c.GetReg64(RCX))
	}
	if c.GetReg64(RSI) != 0x2028 {
		t.Errorf("RSI = %#x, want 0x2028 (40 bytes advanced)", c.GetReg64(RSI))
	}
}

func TestRepSTOSQ_Memset(t *testing.T) {
	c, mm := newLongMode(t)
	c.SetReg64(RAX, 0xDEADBEEFCAFEF00D)
	c.SetReg64(RDI, 0x4000)
	c.SetReg64(RCX, 4)
	// F3 48 AB   rep stosq
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0xF3)
	_ = mm.Write8(code+1, 0x48)
	_ = mm.Write8(code+2, 0xAB)
	_ = mm.Write8(code+3, 0xF4)
	c.SetRIP(code)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := uint64(0); i < 4; i++ {
		got, _ := mm.Read64(0x4000 + i*8)
		if got != 0xDEADBEEFCAFEF00D {
			t.Errorf("dst[%d] = %#x", i, got)
		}
	}
}

func TestMOVS_DFSet_Decrements(t *testing.T) {
	c, mm := newLongMode(t)
	// Place a single qword at 0x5008, target empty at 0x6008.
	_ = mm.Write64(0x5008, 0x1234567890ABCDEF)
	c.SetReg64(RSI, 0x5008)
	c.SetReg64(RDI, 0x6008)
	c.SetRFLAGS(c.GetRFLAGS() | RFLAGS_DF) // DF=1 ⇒ decrement
	// 48 A5  movsq
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0x48)
	_ = mm.Write8(code+1, 0xA5)
	_ = mm.Write8(code+2, 0xF4)
	c.SetRIP(code)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := mm.Read64(0x6008)
	if got != 0x1234567890ABCDEF {
		t.Errorf("dst = %#x", got)
	}
	if c.GetReg64(RSI) != 0x5000 {
		t.Errorf("RSI = %#x, want 0x5000 (decremented)", c.GetReg64(RSI))
	}
	if c.GetReg64(RDI) != 0x6000 {
		t.Errorf("RDI = %#x, want 0x6000 (decremented)", c.GetReg64(RDI))
	}
}

func TestREPNESCASB_FindZero(t *testing.T) {
	c, mm := newLongMode(t)
	// A null-terminated string at 0x2000 of length 5.
	for i, b := range []byte("hello\x00") {
		_ = mm.Write8(0x2000+uint64(i), b)
	}
	c.SetReg64(RAX, 0)  // search target = NUL byte
	c.SetReg64(RDI, 0x2000)
	c.SetReg64(RCX, 16) // search up to 16 bytes
	// F2 AE   repne scasb
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0xF2)
	_ = mm.Write8(code+1, 0xAE)
	_ = mm.Write8(code+2, 0xF4)
	c.SetRIP(code)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// After REPNE SCASB finds NUL at offset 5: RDI = 0x2006 (one past
	// the NUL, since SCAS advances after the compare); RCX = 16-6=10.
	if c.GetReg64(RDI) != 0x2006 {
		t.Errorf("RDI = %#x, want 0x2006", c.GetReg64(RDI))
	}
	if c.GetReg64(RCX) != 10 {
		t.Errorf("RCX = %d, want 10", c.GetReg64(RCX))
	}
}

func TestLODSB_LoadsRAX(t *testing.T) {
	c, mm := newLongMode(t)
	_ = mm.Write8(0x2000, 0x77)
	c.SetReg64(RAX, 0xFFFFFFFFFFFFFF00)
	c.SetReg64(RSI, 0x2000)
	// AC   lodsb
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0xAC)
	_ = mm.Write8(code+1, 0xF4)
	c.SetRIP(code)
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if c.GetReg8(AL) != 0x77 {
		t.Errorf("AL = %#x, want 0x77", c.GetReg8(AL))
	}
	if c.GetReg64(RSI) != 0x2001 {
		t.Errorf("RSI = %#x, want 0x2001", c.GetReg64(RSI))
	}
}
