package x86_64

// Tests for the smaller opcode additions that don't justify a
// dedicated file: LEAVE, RDTSC, WBINVD, the prefetch-NOP family,
// the multi-byte NOP, and the LFENCE/MFENCE/SFENCE memory fences.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func runMiscProg(t *testing.T, prep func(c *CPU, mm *mem.PhysMemoryMap), prog []byte) *CPU {
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
	if err := c.Run(50); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return c
}

// LEAVE := mov rsp, rbp ; pop rbp. Standard function epilogue.
func TestLEAVE(t *testing.T) {
	c := runMiscProg(t,
		func(c *CPU, mm *mem.PhysMemoryMap) {
			// Stack: saved RBP at [RBP]; we set RSP somewhere
			// arbitrary, then RBP at a known frame.
			c.SetReg64(RSP, 0x8000)
			c.SetReg64(RBP, 0x7FF0)
			_ = mm.Write64(0x7FF0, 0xCAFEBABEDEADBEEF) // value LEAVE pops into RBP
		},
		[]byte{0xC9, 0xF4}, // leave; hlt
	)
	if got := c.GetReg64(RBP); got != 0xCAFEBABEDEADBEEF {
		t.Errorf("RBP after LEAVE = %#x", got)
	}
	if got := c.GetReg64(RSP); got != 0x7FF8 {
		t.Errorf("RSP after LEAVE = %#x, want 0x7FF8 (RBP+8 from pop)", got)
	}
}

// RDTSC reads c.cycles into EDX:EAX.
func TestRDTSC(t *testing.T) {
	c := runMiscProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.cycles = 0x123456789ABC
		},
		[]byte{0x0F, 0x31, 0xF4}, // rdtsc; hlt
	)
	combined := uint64(c.GetReg32(EAX)) | (uint64(c.GetReg32(EDX)) << 32)
	if combined != 0x123456789ABC {
		t.Errorf("EDX:EAX = %#x, want %#x", combined, uint64(0x123456789ABC))
	}
}

// WBINVD is a no-op in our single-CPU emulation but must not error.
func TestWBINVD_NoOp(t *testing.T) {
	c := runMiscProg(t, nil, []byte{0x0F, 0x09, 0xF4}) // wbinvd; hlt
	if !c.IsPowerDown() {
		t.Errorf("HLT not reached — WBINVD must have raised an error")
	}
}

// Multi-byte NOP (0F 1F /n) is consumed including its ModR/M+disp.
func TestMultiByteNOP(t *testing.T) {
	// 0F 1F 44 00 00 — 5-byte NOP with mod=01 (disp8=0), rm=100
	// (SIB), SIB scale=0/index=0/base=0 (RAX).
	c := runMiscProg(t, nil, []byte{0x0F, 0x1F, 0x44, 0x00, 0x00, 0xF4})
	if !c.IsPowerDown() {
		t.Errorf("HLT not reached — multi-byte NOP must have consumed disp wrong")
	}
}

// LFENCE / MFENCE / SFENCE are no-ops in single-CPU mode.
func TestFences(t *testing.T) {
	cases := []struct {
		name string
		op2  byte
	}{
		{"LFENCE", 0xE8}, // ModRM 11 101 000 = mod=11, reg=5
		{"MFENCE", 0xF0}, // 11 110 000 = reg=6
		{"SFENCE", 0xF8}, // 11 111 000 = reg=7
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := runMiscProg(t, nil, []byte{0x0F, 0xAE, tc.op2, 0xF4})
			if !c.IsPowerDown() {
				t.Errorf("HLT not reached after %s", tc.name)
			}
		})
	}
}

// 0xC6 /0 — MOV r/m8, imm8. Byte sibling of 0xC7.
func TestMOVImm8_Mem(t *testing.T) {
	c := runMiscProg(t,
		func(c *CPU, _ *mem.PhysMemoryMap) {
			c.SetReg64(RBX, 0x2000)
		},
		// C6 03 42  mov byte [rbx], 0x42
		[]byte{0xC6, 0x03, 0x42, 0xF4},
	)
	v, _ := c.memMap.Read8(0x2000)
	if v != 0x42 {
		t.Errorf("mem[0x2000] = %#x, want 0x42", v)
	}
}
