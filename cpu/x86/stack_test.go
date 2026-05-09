package x86

import (
	"testing"
)

// TestNestedCallRet verifies deep nested CALL/RET preserves return addresses.
func TestNestedCallRet(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)

	// Build a call chain: A -> B -> C -> D, each returns
	code := []byte{
		// A at 0x1000
		0xE8, 0x0A, 0x00, 0x00, 0x00, // CALL B (0x100B)
		0xB8, 0xAA, 0xAA, 0xAA, 0xAA, // MOV EAX, 0xAAAAAAAA
		0xF4, // HLT

		// B at 0x100B
		0xE8, 0x0A, 0x00, 0x00, 0x00, // CALL C (0x1016)
		0xB8, 0xBB, 0xBB, 0xBB, 0xBB, // MOV EAX, 0xBBBBBBBB
		0xC3, // RET

		// C at 0x1016
		0xE8, 0x0A, 0x00, 0x00, 0x00, // CALL D (0x1021)
		0xB8, 0xCC, 0xCC, 0xCC, 0xCC, // MOV EAX, 0xCCCCCCCC
		0xC3, // RET

		// D at 0x1021
		0xB8, 0xDD, 0xDD, 0xDD, 0xDD, // MOV EAX, 0xDDDDDDDD
		0xC3, // RET
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xAAAAAAAA {
		t.Errorf("EAX = 0x%08X, want 0xAAAAAAAA", v)
	}
	if v := c.GetReg32(ESP); v != 0x400 {
		t.Errorf("ESP = 0x%08X, want 0x400", v)
	}
}

// TestFunctionPrologueEpilogue verifies a typical function prologue/epilogue
// with SUB ESP, locals + multiple PUSH/POP + RET.
func TestFunctionPrologueEpilogue(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)
	c.SetReg32(EBP, 0x500)
	c.SetReg32(EBX, 0x11111111)
	c.SetReg32(ESI, 0x22222222)
	c.SetReg32(EDI, 0x33333333)

	code := []byte{
		// Caller at 0x1000
		0xE8, 0x01, 0x00, 0x00, 0x00, // CALL func (offset +1 -> 0x1006)
		0xF4, // HLT

		// func at 0x1006
		0x55,                         // PUSH EBP
		0x89, 0xE5,                   // MOV EBP, ESP
		0x57,                         // PUSH EDI
		0x56,                         // PUSH ESI
		0x53,                         // PUSH EBX
		0x83, 0xEC, 0x20,             // SUB ESP, 0x20
		0xB8, 0x42, 0x00, 0x00, 0x00, // MOV EAX, 0x42 (body)
		0x83, 0xC4, 0x20,             // ADD ESP, 0x20
		0x5B,                         // POP EBX
		0x5E,                         // POP ESI
		0x5F,                         // POP EDI
		0x5D,                         // POP EBP
		0xC3,                         // RET
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x42 {
		t.Errorf("EAX = 0x%08X, want 0x42", v)
	}
	if v := c.GetReg32(ESP); v != 0x400 {
		t.Errorf("ESP = 0x%08X, want 0x400", v)
	}
	if v := c.GetReg32(EBP); v != 0x500 {
		t.Errorf("EBP = 0x%08X, want 0x500", v)
	}
	if v := c.GetReg32(EBX); v != 0x11111111 {
		t.Errorf("EBX = 0x%08X, want 0x11111111", v)
	}
	if v := c.GetReg32(ESI); v != 0x22222222 {
		t.Errorf("ESI = 0x%08X, want 0x22222222", v)
	}
	if v := c.GetReg32(EDI); v != 0x33333333 {
		t.Errorf("EDI = 0x%08X, want 0x33333333", v)
	}
}

// TestPushImm8 verifies PUSH imm8 (0x6A) writes to the correct stack address.
func TestPushImm8(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x300)
	code := []byte{
		0x6A, 0xFF, // PUSH -1 (sign-extended)
		0xF4,       // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(ESP); v != 0x2FC {
		t.Errorf("ESP = 0x%08X, want 0x2FC", v)
	}
	if v := c.readMem32(0x2FC); v != 0xFFFFFFFF {
		t.Errorf("mem[0x2FC] = 0x%08X, want 0xFFFFFFFF", v)
	}
}

// TestCallRel32 verifies CALL rel32 pushes the correct return address.
func TestCallRel32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x300)
	code := []byte{
		// CALL to target at 0x1006 (offset = 0x1006 - 0x1005 = 1)
		0xE8, 0x01, 0x00, 0x00, 0x00, // CALL +1
		0xF4,                         // HLT
		// target at 0x1006
		0xB8, 0x42, 0x00, 0x00, 0x00, // MOV EAX, 0x42
		0xC3,                         // RET
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(ESP); v != 0x300 {
		t.Errorf("ESP = 0x%08X, want 0x300", v)
	}
	// Return address pushed should be 0x1005 (after the CALL instruction)
	if v := c.readMem32(0x2FC); v != 0x1005 {
		t.Errorf("return addr = 0x%08X, want 0x1005", v)
	}
}

// TestEnterLeave verifies ENTER and LEAVE instructions.
func TestEnterLeave(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)
	c.SetReg32(EBP, 0x500)

	code := []byte{
		// Caller at 0x1000
		0xE8, 0x01, 0x00, 0x00, 0x00, // CALL func (offset +1 -> 0x1006)
		0xF4, // HLT

		// func at 0x1006
		0xC8, 0x10, 0x00, 0x00, // ENTER 0x10, 0
		0xB8, 0x42, 0x00, 0x00, 0x00, // MOV EAX, 0x42
		0xC9, // LEAVE
		0xC3, // RET
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x42 {
		t.Errorf("EAX = 0x%08X, want 0x42", v)
	}
	if v := c.GetReg32(ESP); v != 0x400 {
		t.Errorf("ESP = 0x%08X, want 0x400", v)
	}
	if v := c.GetReg32(EBP); v != 0x500 {
		t.Errorf("EBP = 0x%08X, want 0x500", v)
	}
}

// TestRepMovsStack verifies REP MOVS does not corrupt stack data.
func TestRepMovsStack(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 4)
	c.SetReg32(ESI, 0x200)
	c.SetReg32(EDI, 0x300)
	c.SetReg32(ESP, 0x400)

	// Set up source data
	c.writeMem32(0x200, 0x11111111)
	c.writeMem32(0x204, 0x22222222)
	c.writeMem32(0x208, 0x33333333)
	c.writeMem32(0x20C, 0x44444444)

	code := []byte{
		0xF3, 0xA5, // REP MOVSD
		0xF4,       // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// Verify destination
	if v := c.readMem32(0x300); v != 0x11111111 {
		t.Errorf("dest[0] = 0x%08X, want 0x11111111", v)
	}
	if v := c.readMem32(0x30C); v != 0x44444444 {
		t.Errorf("dest[3] = 0x%08X, want 0x44444444", v)
	}
	// Verify stack pointer unchanged
	if v := c.GetReg32(ESP); v != 0x400 {
		t.Errorf("ESP = 0x%08X, want 0x400", v)
	}
}

// TestPushadPopad verifies PUSHAD/POPAD preserve all registers.
func TestPushadPopad(t *testing.T) {
	t.Skip("PUSHAD/POPAD not yet implemented")
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)
	c.SetReg32(EAX, 0x11111111)
	c.SetReg32(ECX, 0x22222222)
	c.SetReg32(EDX, 0x33333333)
	c.SetReg32(EBX, 0x44444444)
	c.SetReg32(ESP, 0x400)
	c.SetReg32(EBP, 0x55555555)
	c.SetReg32(ESI, 0x66666666)
	c.SetReg32(EDI, 0x77777777)

	code := []byte{
		0x60, // PUSHAD
		// Clear all regs
		0x31, 0xC0,       // XOR EAX, EAX
		0x31, 0xC9,       // XOR ECX, ECX
		0x31, 0xD2,       // XOR EDX, EDX
		0x31, 0xDB,       // XOR EBX, EBX
		0x31, 0xED,       // XOR EBP, EBP
		0x31, 0xF6,       // XOR ESI, ESI
		0x31, 0xFF,       // XOR EDI, EDI
		0x61,             // POPAD
		0xF4,             // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x11111111 {
		t.Errorf("EAX = 0x%08X, want 0x11111111", v)
	}
	if v := c.GetReg32(ECX); v != 0x22222222 {
		t.Errorf("ECX = 0x%08X, want 0x22222222", v)
	}
	if v := c.GetReg32(EDX); v != 0x33333333 {
		t.Errorf("EDX = 0x%08X, want 0x33333333", v)
	}
	if v := c.GetReg32(EBX); v != 0x44444444 {
		t.Errorf("EBX = 0x%08X, want 0x44444444", v)
	}
	if v := c.GetReg32(ESP); v != 0x400 {
		t.Errorf("ESP = 0x%08X, want 0x400", v)
	}
	if v := c.GetReg32(EBP); v != 0x55555555 {
		t.Errorf("EBP = 0x%08X, want 0x55555555", v)
	}
	if v := c.GetReg32(ESI); v != 0x66666666 {
		t.Errorf("ESI = 0x%08X, want 0x66666666", v)
	}
	if v := c.GetReg32(EDI); v != 0x77777777 {
		t.Errorf("EDI = 0x%08X, want 0x77777777", v)
	}
}

// TestMovImm16Loop verifies the exact pattern from the Alpine kernel crash:
// a loop using 0x66 C7 to zero memory in 16-bit chunks.
func TestMovImm16Loop(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x500)
	c.SetReg32(EAX, 0x200)
	c.SetReg32(ECX, 0x210)

	// Fill memory with non-zero values in the zeroing range and just past it
	for i := uint32(0); i < 0x400; i += 4 {
		c.writeMem32(0x200+i, 0xDEADBEEF)
	}

	// This is the loop pattern from the decompressor:
	// LEA ECX, [ESP+0x64]
	// loop_start:
	// MOV WORD [EAX], 0x0000
	// MOV WORD [EAX+0x02], 0x0000
	// ADD EAX, 0x04
	// CMP ECX, EAX
	// JNE loop_start
	code := []byte{
		0x8D, 0x4C, 0x24, 0x64, // LEA ECX, [ESP+0x64] -> ECX = 0x564
		// loop at 0x1004
		0x66, 0xC7, 0x00, 0x00, 0x00,       // MOV WORD [EAX], 0x0000
		0x66, 0xC7, 0x40, 0x02, 0x00, 0x00, // MOV WORD [EAX+0x02], 0x0000
		0x83, 0xC0, 0x04,                   // ADD EAX, 0x04
		0x39, 0xC1,                         // CMP ECX, EAX
		0x75, 0xEE,                         // JNE loop_start (0x1004)
		0xF4,                               // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Memory from 0x200 to 0x564 should be zeroed
	for i := uint32(0x200); i < 0x564; i += 4 {
		if v := c.readMem32(i); v != 0 {
			t.Errorf("mem[0x%04X] = 0x%08X, want 0x00000000", i, v)
			break
		}
	}
	// Memory at 0x564 and above should NOT be touched
	if v := c.readMem32(0x564); v != 0xDEADBEEF {
		t.Errorf("mem[0x564] = 0x%08X, want 0xDEADBEEF (should not be overwritten)", v)
	}
	// Return address on the stack should be intact (at 0x4FC)
	if v := c.readMem32(0x4FC); v != 0 {
		t.Errorf("return addr overwritten! mem[0x4FC] = 0x%08X", v)
	}
}

// TestCallRet16 verifies CALL/RET with 0x66 operand-size prefix.
func TestCallRet16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)

	code := []byte{
		// Caller at 0x1000
		0x66, 0xE8, 0x01, 0x00, // CALL rel16 (target = 0x1005)
		0xF4, // HLT

		// Target at 0x1005
		0xB8, 0xAA, 0xAA, 0xAA, 0xAA, // MOV EAX, 0xAAAAAAAA
		0x66, 0xC3, // RET (16-bit)
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xAAAAAAAA {
		t.Errorf("EAX = 0x%08X, want 0xAAAAAAAA", v)
	}
}

// TestPushPop16 verifies PUSH/POP with 0x66 operand-size prefix.
func TestPushPop16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)
	c.SetReg32(EAX, 0x1234BEEF)
	code := []byte{
		0x66, 0x50, // PUSH AX
		0x66, 0x58, // POP AX
		0xF4,       // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(ESP); v != 0x400 {
		t.Errorf("ESP = 0x%08X, want 0x400", v)
	}
	if v := c.GetReg16(AX); v != 0xBEEF {
		t.Errorf("AX = 0x%04X, want 0xBEEF", v)
	}
}
