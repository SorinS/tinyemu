package x86

import "testing"

// TestPopSSReloadsDescriptor verifies that POP SS in protected mode reloads
// the SS descriptor cache (base/limit/access), not just the selector. The
// previous behavior wrote seg[SS] directly and left base/limit pointing at
// the OLD SS, so subsequent stack accesses would use the wrong segment.
func TestPopSSReloadsDescriptor(t *testing.T) {
	c := newTestCPU(t)

	gdt := uint32(0x4000)
	for i := uint32(0); i < 32; i++ {
		c.writeMem8(gdt+i, 0)
	}
	// Slot 1 (selector 0x08): writable data, base = 0xADDE0000.
	// Byte 4 is base[23:16], byte 7 is base[31:24].
	c.writeMem8(gdt+8, 0xFF)
	c.writeMem8(gdt+9, 0xFF)
	c.writeMem8(gdt+10, 0x00)
	c.writeMem8(gdt+11, 0x00)
	c.writeMem8(gdt+12, 0xDE) // base[23:16]
	c.writeMem8(gdt+13, 0x92) // data writable
	c.writeMem8(gdt+14, 0xCF)
	c.writeMem8(gdt+15, 0xAD) // base[31:24]
	// Slot 2 (selector 0x10): writable data, base = 0xFECA0000.
	c.writeMem8(gdt+16, 0xFF)
	c.writeMem8(gdt+17, 0xFF)
	c.writeMem8(gdt+18, 0x00)
	c.writeMem8(gdt+19, 0x00)
	c.writeMem8(gdt+20, 0xCA) // base[23:16]
	c.writeMem8(gdt+21, 0x92)
	c.writeMem8(gdt+22, 0xCF)
	c.writeMem8(gdt+23, 0xFE) // base[31:24]
	c.SetSegBase(GDTR, gdt)
	c.SetSegLimit(GDTR, 23)

	// Initial SS: flat base (set fields directly so the stack physical
	// writes land where POP SS will read). Pre-set a different "old base"
	// via segBase so we can prove POP SS replaces it.
	c.SetSeg(SS, 0x08)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetSegAccess(SS, 0x92)

	c.writeMem8(0x0FFC, 0x10) // selector 0x10 on stack
	c.writeMem8(0x0FFD, 0x00)
	c.writeMem8(0x0FFE, 0x00)
	c.writeMem8(0x0FFF, 0x00)
	c.SetReg32(ESP, 0x0FFC)

	// POP SS at code addr 0x2000.
	code := []byte{0x17, 0xF4}
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.seg[SS] != 0x10 {
		t.Errorf("SS = 0x%04X, want 0x10", c.seg[SS])
	}
	if c.segBase[SS] != 0xFECA0000 {
		t.Errorf("SS base = 0x%08X, want 0xFECA0000 (descriptor cache was NOT updated)", c.segBase[SS])
	}
}

// TestPopDSReloadsDescriptor: same property for DS.
func TestPopDSReloadsDescriptor(t *testing.T) {
	c := newTestCPU(t)

	gdt := uint32(0x4000)
	for i := uint32(0); i < 32; i++ {
		c.writeMem8(gdt+i, 0)
	}
	c.writeMem8(gdt+16, 0xFF)
	c.writeMem8(gdt+17, 0xFF)
	c.writeMem8(gdt+18, 0x00)
	c.writeMem8(gdt+19, 0x00)
	c.writeMem8(gdt+20, 0xEF) // base[23:16]
	c.writeMem8(gdt+21, 0x92)
	c.writeMem8(gdt+22, 0xCF)
	c.writeMem8(gdt+23, 0xBE) // base[31:24]
	c.SetSegBase(GDTR, gdt)
	c.SetSegLimit(GDTR, 23)

	// SS: flat for stack access. DS: start with non-zero base via segBase
	// hack — we want POP DS to overwrite it.
	c.SetSeg(SS, 0x08)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetSegAccess(SS, 0x92)
	c.SetSegBase(DS, 0x12340000) // bogus old base — must be replaced

	c.writeMem32(0x0FFC, 0x10)
	c.SetReg32(ESP, 0x0FFC)

	code := []byte{0x1F, 0xF4} // POP DS
	if err := runCode(t, c, code, 0x2000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.segBase[DS] != 0xBEEF0000 {
		t.Errorf("DS base = 0x%08X, want 0xBEEF0000", c.segBase[DS])
	}
}
