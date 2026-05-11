package x86

import "testing"

// TestTEST16IgnoresUpperBits verifies that `66 85 c0` (TEST AX, AX with the
// 0x66 operand-size override) sets ZF based on the 16-bit value, NOT the
// full 32-bit register. Prior to the fix in handleTEST_ModRM, this opcode
// silently tested EAX, so a non-zero upper half (e.g. EAX = 0xFFFF0000 from
// an earlier `mov ax, ...` that left the high half alone) made ZF=0 even
// when AX=0. busybox's wide-string scan loop relies on this exact sequence
// to find a NUL terminator; the wrong ZF made the loop walk off the end of
// the buffer and SIGBUS at the page boundary.
func TestTEST16IgnoresUpperBits(t *testing.T) {
	c := newTestCPU(t)
	// EAX = 0xFFFF0000 (upper half set, lower half zero)
	c.SetReg32(EAX, 0xFFFF0000)
	// Code: 66 85 C0  (test ax, ax); F4 (hlt) to terminate
	code := []byte{0x66, 0x85, 0xC0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF=false after TEST AX,AX with AX=0; expected ZF=true. eflags=0x%08X", c.eflags)
	}
	if c.getSF() {
		t.Errorf("SF=true after TEST AX,AX with AX=0; expected SF=false")
	}
}

// TestTEST16UpperBitsNotConsulted: upper half has bit 15 (which is sign bit
// for a 16-bit value if it were below) AND bit 31 (sign for 32-bit) — but
// the 16-bit AX is positive (0x7FFF). SF should reflect AX bit 15.
func TestTEST16SignFromLowHalfOnly(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x80007FFF) // AX=0x7FFF (positive 16-bit), EAX bit 31 set
	code := []byte{0x66, 0x85, 0xC0, 0xF4} // test ax, ax
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getSF() {
		t.Errorf("SF=true after TEST AX,AX with AX=0x7FFF; expected SF=false (sign comes from AX bit 15)")
	}
	if c.getZF() {
		t.Errorf("ZF=true after TEST AX,AX with AX=0x7FFF; expected ZF=false")
	}
}

// TestTEST16NegativeLowHalf: AX=0x8000 → SF=1.
func TestTEST16NegativeLowHalf(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x00008000) // AX=0x8000 (sign bit set in 16-bit view)
	code := []byte{0x66, 0x85, 0xC0, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getSF() {
		t.Errorf("SF=false after TEST AX,AX with AX=0x8000; expected SF=true")
	}
}

// TestTEST8ModRM verifies the 8-bit path still works (regression guard for
// the refactor).
func TestTEST8ModRM(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xFFFFFF00) // AL=0
	code := []byte{0x84, 0xC0, 0xF4} // test al, al
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF=false after TEST AL,AL with AL=0")
	}
}
