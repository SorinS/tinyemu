package x86

import "testing"

// TestPOPFPreservesReservedBit1 verifies that POPF cannot clear bit 1 of
// EFLAGS, which Intel reserves-as-1. This was the root cause of the SAVE_ALL
// trampoline taking the wrong branch when booting Yocto: the kernel's
// `testl $0x2, EFLAGS` check assumes bit 1 is always set.
func TestPOPFPreservesReservedBit1(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.cpl = 0
	// Push a value WITHOUT bit 1 (an attacker / buggy popf source).
	c.push32(0xFFFFFFFD) // all bits set EXCEPT bit 1
	// POPF
	code := []byte{0x9D, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.eflags&0x2 == 0 {
		t.Errorf("EFLAGS bit 1 cleared after POPF: 0x%08X", c.eflags)
	}
}

// TestIRET32PreservesReservedBit1 verifies the same property through the
// 32-bit IRET path.
func TestIRET32PreservesReservedBit1(t *testing.T) {
	c := newTestCPU(t)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0)
	c.cpl = 0

	// Build minimal GDT with a flat code segment at 0x08.
	gdt := uint32(0x4000)
	for i := uint32(0); i < 16; i++ {
		c.writeMem8(gdt+i, 0)
	}
	c.writeMem8(gdt+8, 0xFF)
	c.writeMem8(gdt+9, 0xFF)
	c.writeMem8(gdt+10, 0)
	c.writeMem8(gdt+11, 0)
	c.writeMem8(gdt+12, 0)
	c.writeMem8(gdt+13, 0x9A)
	c.writeMem8(gdt+14, 0xCF)
	c.writeMem8(gdt+15, 0)
	c.SetSegBase(GDTR, gdt)
	c.SetSegLimit(GDTR, 15)

	c.SetReg32(ESP, 0x2000)
	// Push (in IRET order): EIP, CS, EFLAGS_without_bit1.
	c.push32(0xFFFFFFFD) // EFLAGS without bit 1
	c.push32(0x00000008) // CS
	c.push32(0x00003000) // EIP target
	// IRET; HLT at the new EIP.
	c.writeMem8(0x3000, 0xF4)

	code := []byte{0xCF}
	if err := runCode(t, c, code, 0x2500); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.eflags&0x2 == 0 {
		t.Errorf("EFLAGS bit 1 cleared after IRET: 0x%08X", c.eflags)
	}
}

// TestPOPFAtCPL3CannotChangeIOPL verifies that user-mode POPF cannot
// alter IOPL.
func TestPOPFAtCPL3CannotChangeIOPL(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.cpl = 3
	c.eflags = 0x2 | (0x3 << 12) // IOPL=3

	// User pushes EFLAGS with IOPL=0.
	c.push32(0x2)
	code := []byte{0x9D, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	iopl := (c.eflags & EFLAGS_IOPL) >> 12
	if iopl != 3 {
		t.Errorf("IOPL changed at CPL=3: got %d, want 3", iopl)
	}
}

// TestPOPFAtCPL3CannotChangeIF verifies that user-mode POPF cannot
// modify IF when CPL > IOPL.
func TestPOPFAtCPL3CannotChangeIF(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x1000)
	c.cpl = 3
	c.eflags = 0x2 | EFLAGS_IF // IF=1, IOPL=0 (so CPL > IOPL)

	// User pushes EFLAGS with IF clear.
	c.push32(0x2)
	code := []byte{0x9D, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.eflags&EFLAGS_IF == 0 {
		t.Errorf("IF cleared by user-mode POPF when CPL > IOPL: 0x%08X", c.eflags)
	}
}

// TestSAVEAllBitTestPathway is a regression test for the exact pathway that
// crashed the Yocto kernel: an exception delivery pushes EFLAGS onto the
// stack; the handler reads it back and tests bit 1; the test must succeed.
func TestSAVEAllBitTestPathway(t *testing.T) {
	c := newTestCPU(t)
	c.cpl = 0
	c.eflags = 0x202 // IF + bit 1

	// Simulate POPF with a value that has bit 1 cleared.
	c.SetReg32(ESP, 0x1000)
	c.push32(0x200) // IF set, but bit 1 clear
	code := []byte{0x9D, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// Bit 1 must still be set so kernel's testl $0x2, EFLAGS works.
	if c.eflags&0x2 == 0 {
		t.Fatalf("EFLAGS bit 1 lost: 0x%08X — SAVE_ALL trampoline would take wrong branch", c.eflags)
	}
}
