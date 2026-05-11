package x86

import "testing"

// Intel SDM (BT/BTS/BTR/BTC): when the destination is a MEMORY operand and
// the bit-index operand comes from a REGISTER, the bit index is interpreted
// as a signed integer that extends the memory address — the instruction
// modifies bit (bitIdx % 32) of dword at (base + (bitIdx / 32) * 4) (with
// signed arithmetic). This is critical for Linux's setup_clear_cpu_cap()
// which uses LOCK BTR [cap_array+0x24], EDI where EDI ranges over hundreds
// of bit indices to clear feature bits across multiple cap[] dwords.
//
// Prior to this fix our impl masked the bit index to 0x1F regardless of
// memory/register destination — so LOCK BTR [arr], 35 (intending to clear
// bit 3 of arr[1]) incorrectly cleared bit 3 of arr[0]. That silently
// destroyed Linux's boot_cpu_data.x86_capability[0] including the FPU bit,
// causing "Giving up, no FPU found" early in boot.

func TestBTRMemRegBitOver32(t *testing.T) {
	c := newTestCPU(t)
	// Set both dwords to all-ones; BTR with bit=35 should clear bit 3 of
	// dword[1], not bit 3 of dword[0].
	c.writeMem32(0x2000, 0xFFFFFFFF)
	c.writeMem32(0x2004, 0xFFFFFFFF)
	c.SetReg32(ECX, 35)
	// BTR [0x2000], ECX
	code := []byte{0x0F, 0xB3, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0xFFFFFFFF {
		t.Errorf("dword[0] = 0x%08X, want 0xFFFFFFFF (untouched)", v)
	}
	if v := c.readMem32(0x2004); v != 0xFFFFFFF7 {
		t.Errorf("dword[1] = 0x%08X, want 0xFFFFFFF7 (bit 3 cleared)", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set before BTR)")
	}
}

func TestBTSMemRegBitOver32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0)
	c.writeMem32(0x2004, 0)
	c.SetReg32(ECX, 33) // bit 1 of dword[1]
	code := []byte{0x0F, 0xAB, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0 {
		t.Errorf("dword[0] = 0x%08X, want 0 (untouched)", v)
	}
	if v := c.readMem32(0x2004); v != 0x00000002 {
		t.Errorf("dword[1] = 0x%08X, want 0x2 (bit 1 set)", v)
	}
}

func TestBTMemRegBitOver32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0)
	c.writeMem32(0x2004, 0x80000000) // bit 31 set
	c.SetReg32(ECX, 63)               // bit 31 of dword[1]
	code := []byte{0x0F, 0xA3, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit 31 of dword[1] is set)")
	}
}

func TestBTCMemRegBitOver32(t *testing.T) {
	c := newTestCPU(t)
	c.writeMem32(0x2000, 0xAAAAAAAA)
	c.writeMem32(0x2004, 0xAAAAAAAA)
	c.SetReg32(ECX, 32+1) // bit 1 of dword[1] — currently 1 → should toggle to 0
	code := []byte{0x0F, 0xBB, 0x0D, 0x00, 0x20, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2000); v != 0xAAAAAAAA {
		t.Errorf("dword[0] = 0x%08X, want 0xAAAAAAAA (untouched)", v)
	}
	if v := c.readMem32(0x2004); v != 0xAAAAAAA8 {
		t.Errorf("dword[1] = 0x%08X, want 0xAAAAAAA8 (bit 1 toggled to 0)", v)
	}
	if !c.getCF() {
		t.Error("expected CF=1 (bit was set before BTC)")
	}
}

// Linux's setup_clear_cpu_cap pattern: BTR cap_array+0x24, bit_index where
// bit_index walks 0..N*32 to clear feature bits. We verify a multi-step
// sequence matches the expected per-dword effects.
func TestBTRClearAcrossCapArray(t *testing.T) {
	c := newTestCPU(t)
	// Lay out a fake 6-dword cap array at 0x2024 (matching ESI+0x24 pattern).
	c.SetReg32(EBX, 0x2000)
	for i := uint32(0); i < 6; i++ {
		c.writeMem32(0x2024+i*4, 0xFFFFFFFF)
	}
	// Clear bit 35 (bit 3 of cap[1]) via LOCK BTR [EBX+0x24], ECX with ECX=35.
	// 0F B3 4B 24 = BTR [EBX+0x24], ECX (modrm 0x4B = mod 01 reg 001 rm 011, disp 0x24)
	c.SetReg32(ECX, 35)
	code := []byte{0x0F, 0xB3, 0x4B, 0x24, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x2024); v != 0xFFFFFFFF {
		t.Errorf("cap[0] = 0x%08X, want 0xFFFFFFFF (untouched)", v)
	}
	if v := c.readMem32(0x2028); v != 0xFFFFFFF7 {
		t.Errorf("cap[1] = 0x%08X, want 0xFFFFFFF7 (bit 3 cleared)", v)
	}
}
