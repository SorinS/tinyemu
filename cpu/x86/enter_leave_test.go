package x86

import "testing"

// TestLEAVE32 verifies LEAVE in 32-bit mode:
//   ESP <- EBP
//   EBP <- pop32
// This is the path Linux uses on every C function return that uses a frame
// pointer.
func TestLEAVE32(t *testing.T) {
	c := newTestCPU(t)
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)

	c.SetReg32(EBP, 0x0FF8)
	c.writeMem32(0x0FF8, 0xCAFEBABE) // saved frame
	c.SetReg32(ESP, 0x0F00)          // arbitrary lower addr — LEAVE should reset

	code := []byte{0xC9, 0xF4} // LEAVE; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(ESP); got != 0x0FFC {
		t.Errorf("ESP = 0x%08X, want 0x0FFC (EBP+4 after pop)", got)
	}
	if got := c.GetReg32(EBP); got != 0xCAFEBABE {
		t.Errorf("EBP = 0x%08X, want 0xCAFEBABE", got)
	}
}

// TestENTER32_Level0 verifies the no-nesting form of ENTER:
//   push EBP
//   EBP <- ESP
//   ESP <- ESP - size
func TestENTER32_Level0(t *testing.T) {
	c := newTestCPU(t)
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)

	c.SetReg32(ESP, 0x1000)
	c.SetReg32(EBP, 0xDEADBEEF)

	// ENTER 0x10, 0  ; reserve 16 bytes, no nesting
	code := []byte{0xC8, 0x10, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EBP); got != 0x0FFC {
		t.Errorf("EBP = 0x%08X, want 0x0FFC (ESP after push)", got)
	}
	if got := c.GetReg32(ESP); got != 0x0FEC {
		t.Errorf("ESP = 0x%08X, want 0x0FEC (post-reservation)", got)
	}
	if got := c.readMem32(0x0FFC); got != 0xDEADBEEF {
		t.Errorf("saved EBP at top of frame = 0x%08X, want 0xDEADBEEF", got)
	}
}

// TestENTER32_Level1 — ENTER 0x10, 1 with a single display word.
// Verifies that one display word is pushed and the frame pointer is correct.
func TestENTER32_Level1(t *testing.T) {
	c := newTestCPU(t)
	c.SetSeg(CS, 0)
	c.SetSegBase(CS, 0)

	c.SetReg32(ESP, 0x1000)
	c.SetReg32(EBP, 0x0FF0)
	// We need a writable memory pre-state where EBP-4 points to a saved
	// outer frame. With nesting=1, ENTER does:
	//   push EBP            ; ESP=0xFFC, mem[0xFFC]=0xFF0
	//   frame := ESP        ; 0xFFC
	//   (loop body skipped — i starts at 1, not < 1)
	//   push frame          ; ESP=0xFF8, mem[0xFF8]=0xFFC
	//   EBP := frame        ; 0xFFC
	//   ESP -= 0x10         ; 0xFE8
	code := []byte{0xC8, 0x10, 0x00, 0x01, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EBP); got != 0x0FFC {
		t.Errorf("EBP = 0x%08X, want 0x0FFC", got)
	}
	if got := c.GetReg32(ESP); got != 0x0FE8 {
		t.Errorf("ESP = 0x%08X, want 0x0FE8", got)
	}
	if got := c.readMem32(0x0FFC); got != 0x0FF0 {
		t.Errorf("saved EBP = 0x%08X, want 0x0FF0", got)
	}
	if got := c.readMem32(0x0FF8); got != 0x0FFC {
		t.Errorf("display word = 0x%08X, want 0x0FFC (frame pointer)", got)
	}
}
