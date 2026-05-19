package x86_64

// M6 unit tests — SYSCALL/SYSRET round-trip.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func TestSYSCALL_DispatchesToLSTAR(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA | EFER_SCE)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	// STAR: kernel CS=0x08 (bits 47:32), user CS=0x10 (bits 63:48).
	c.msrStar = (uint64(0x10) << 48) | (uint64(0x08) << 32)
	c.msrLstar = 0x80100000
	c.msrSFMask = uint64(RFLAGS_IF | RFLAGS_DF)
	c.rflags |= RFLAGS_IF | RFLAGS_DF

	// Place SYSCALL (0F 05) at 0x1000 followed by a marker we won't reach.
	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0x0F)
	_ = mm.Write8(codeAddr+1, 0x05)
	c.SetRIP(codeAddr)
	preFlags := c.rflags

	if err := c.Step(); err != nil {
		t.Fatalf("step SYSCALL: %v", err)
	}
	if c.GetReg64(RCX) != codeAddr+2 {
		t.Errorf("RCX = %#x, want %#x (return address)", c.GetReg64(RCX), codeAddr+2)
	}
	if c.GetReg64(R11) != preFlags {
		t.Errorf("R11 = %#x, want %#x (saved RFLAGS)", c.GetReg64(R11), preFlags)
	}
	if c.GetRIP() != 0x80100000 {
		t.Errorf("RIP = %#x, want LSTAR=0x80100000", c.GetRIP())
	}
	if c.seg[CS] != 0x08 {
		t.Errorf("CS = %#x, want 0x08", c.seg[CS])
	}
	if c.seg[SS] != 0x10 {
		t.Errorf("SS = %#x, want 0x10", c.seg[SS])
	}
	if c.rflags&(RFLAGS_IF|RFLAGS_DF) != 0 {
		t.Errorf("SFMASK didn't clear IF+DF; rflags = %#x", c.rflags)
	}
}

// TestSYSCALL_SYSRET_RoundTrip: user code SYSCALLs, kernel SYSRETQs,
// user resumes after the SYSCALL.
func TestSYSCALL_SYSRET_RoundTrip(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA | EFER_SCE)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	// STAR layout: STAR[47:32] = kernel CS = 0x08; STAR[63:48] = 0x10
	// → user CS becomes 0x10+16=0x20 | 3, user SS = 0x10+8=0x18 | 3.
	c.msrStar = (uint64(0x10) << 48) | (uint64(0x08) << 32)
	c.msrLstar = 0x90000

	// User code at 0x1000:
	//   48 C7 C0 11 00 00 00   mov rax, 0x11
	//   0F 05                  syscall
	//   48 C7 C3 22 00 00 00   mov rbx, 0x22     ; runs after SYSRET
	//   F4                     hlt
	const userAddr uint64 = 0x1000
	userCode := []byte{
		0x48, 0xC7, 0xC0, 0x11, 0x00, 0x00, 0x00,
		0x0F, 0x05,
		0x48, 0xC7, 0xC3, 0x22, 0x00, 0x00, 0x00,
		0xF4,
	}
	for i, b := range userCode {
		_ = mm.Write8(userAddr+uint64(i), b)
	}

	// Kernel handler at 0x90000:
	//   48 C7 C2 33 00 00 00   mov rdx, 0x33    ; observe in kernel
	//   48 0F 07               sysretq
	kernel := []byte{
		0x48, 0xC7, 0xC2, 0x33, 0x00, 0x00, 0x00,
		0x48, 0x0F, 0x07,
	}
	for i, b := range kernel {
		_ = mm.Write8(0x90000+uint64(i), b)
	}

	c.SetRIP(userAddr)
	c.cpl = 3
	for i := 0; i < 6; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d (RIP=%#x): %v", i, c.GetRIP(), err)
		}
	}

	if c.GetReg64(RAX) != 0x11 {
		t.Errorf("RAX = %#x, want 0x11 (set before SYSCALL)", c.GetReg64(RAX))
	}
	if c.GetReg64(RDX) != 0x33 {
		t.Errorf("RDX = %#x, want 0x33 (set in kernel)", c.GetReg64(RDX))
	}
	if c.GetReg64(RBX) != 0x22 {
		t.Errorf("RBX = %#x, want 0x22 (set after SYSRET)", c.GetReg64(RBX))
	}
	if c.cpl != 3 {
		t.Errorf("CPL = %d, want 3 (returned to user)", c.cpl)
	}
	if !c.IsPowerDown() {
		t.Errorf("expected user code to reach HLT after SYSRET")
	}
}
