package x86

import (
	"bytes"
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// ===== CWDE/CDQ and CBW/CWD operand-size tests =====

func TestCWDEPositive(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12340078)
	code := []byte{0x98, 0xF4} // CWDE; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x00000078 {
		t.Errorf("EAX = 0x%08X, want 0x00000078", v)
	}
}

func TestCWDENegative(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x1234FF88)
	code := []byte{0x98, 0xF4} // CWDE; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xFFFFFF88 {
		t.Errorf("EAX = 0x%08X, want 0xFFFFFF88", v)
	}
}

func TestCDQPositive(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x12345678)
	code := []byte{0x99, 0xF4} // CDQ; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EDX); v != 0x00000000 {
		t.Errorf("EDX = 0x%08X, want 0x00000000", v)
	}
}

func TestCDQNegative(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x87654321)
	code := []byte{0x99, 0xF4} // CDQ; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EDX); v != 0xFFFFFFFF {
		t.Errorf("EDX = 0x%08X, want 0xFFFFFFFF", v)
	}
}

func TestCBW16Bit(t *testing.T) {
	// Real mode: operand size defaults to 16-bit
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetReg8(AL, 0x80)
	code := []byte{0x98, 0xF4} // CBW; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xFF80 {
		t.Errorf("AX = 0x%04X, want 0xFF80", v)
	}
}

func TestCWD16Bit(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetReg16(AX, 0x8000)
	code := []byte{0x99, 0xF4} // CWD; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(DX); v != 0xFFFF {
		t.Errorf("DX = 0x%04X, want 0xFFFF", v)
	}
}

func TestCWDEWithOperandSizeOverride(t *testing.T) {
	c := newTestCPU(t)
	// In 32-bit mode, 0x66 overrides to 16-bit: CBW instead of CWDE
	c.SetReg32(EAX, 0x1234FF88)
	code := []byte{0x66, 0x98, 0xF4} // CBW; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// CBW sign-extends AL to AX, leaving upper bits of EAX intact
	if v := c.GetReg32(EAX); v != 0x1234FF88 {
		t.Errorf("EAX = 0x%08X, want 0x1234FF88", v)
	}
}

// ===== String operations operand-size tests =====

func TestRepMovsw(t *testing.T) {
	c := newTestCPU(t)
	// Set up source at DS:ESI and dest at ES:EDI
	c.SetReg32(ESI, 0x100)
	c.SetReg32(EDI, 0x200)
	c.SetReg32(ECX, 3)
	c.writeMem16(0x100, 0xAABB)
	c.writeMem16(0x102, 0xCCDD)
	c.writeMem16(0x104, 0xEEFF)

	// 0x66 A5 = MOVSW (16-bit operand size)
	code := []byte{0xF3, 0x66, 0xA5, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if v := c.readMem16(0x200); v != 0xAABB {
		t.Errorf("mem[0x200] = 0x%04X, want 0xAABB", v)
	}
	if v := c.readMem16(0x202); v != 0xCCDD {
		t.Errorf("mem[0x202] = 0x%04X, want 0xCCDD", v)
	}
	if v := c.readMem16(0x204); v != 0xEEFF {
		t.Errorf("mem[0x204] = 0x%04X, want 0xEEFF", v)
	}
	if v := c.GetReg32(ESI); v != 0x106 {
		t.Errorf("ESI = 0x%08X, want 0x106", v)
	}
	if v := c.GetReg32(EDI); v != 0x206 {
		t.Errorf("EDI = 0x%08X, want 0x206", v)
	}
}

func TestRepStosw(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EDI, 0x200)
	c.SetReg32(ECX, 3)
	c.SetReg16(AX, 0xBEEF)

	code := []byte{0xF3, 0x66, 0xAB, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if v := c.readMem16(0x200); v != 0xBEEF {
		t.Errorf("mem[0x200] = 0x%04X, want 0xBEEF", v)
	}
	if v := c.readMem16(0x202); v != 0xBEEF {
		t.Errorf("mem[0x202] = 0x%04X, want 0xBEEF", v)
	}
	if v := c.readMem16(0x204); v != 0xBEEF {
		t.Errorf("mem[0x204] = 0x%04X, want 0xBEEF", v)
	}
}

func TestRepLodsw(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESI, 0x100)
	c.SetReg32(ECX, 2)
	c.writeMem16(0x100, 0xAABB)
	c.writeMem16(0x102, 0xCCDD)

	code := []byte{0xF3, 0x66, 0xAD, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if v := c.GetReg16(AX); v != 0xCCDD {
		t.Errorf("AX = 0x%04X, want 0xCCDD", v)
	}
	if v := c.GetReg32(ESI); v != 0x104 {
		t.Errorf("ESI = 0x%08X, want 0x104", v)
	}
}

func TestRepScasw(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EDI, 0x200)
	c.SetReg32(ECX, 4)
	c.SetReg16(AX, 0xBEEF)
	c.writeMem16(0x200, 0x1234)
	c.writeMem16(0x202, 0x5678)
	c.writeMem16(0x204, 0xBEEF)
	c.writeMem16(0x206, 0x9ABC)

	// REPNE SCASW: scan until AX == [ES:EDI] or ECX == 0
	code := []byte{0xF2, 0x66, 0xAF, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if v := c.GetReg32(EDI); v != 0x206 {
		t.Errorf("EDI = 0x%08X, want 0x206", v)
	}
	if v := c.GetReg32(ECX); v != 1 {
		t.Errorf("ECX = 0x%08X, want 1", v)
	}
	if !c.getZF() {
		t.Errorf("ZF = false, want true")
	}
}

func TestRepneCmpsw(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESI, 0x100)
	c.SetReg32(EDI, 0x200)
	c.SetReg32(ECX, 4)
	c.writeMem16(0x100, 0xAABB)
	c.writeMem16(0x102, 0xCCDD)
	c.writeMem16(0x104, 0xBEEF)
	c.writeMem16(0x106, 0x1122)
	c.writeMem16(0x200, 0x3344)
	c.writeMem16(0x202, 0x5566)
	c.writeMem16(0x204, 0xBEEF)
	c.writeMem16(0x206, 0x7788)

	// REPNE CMPSW: repeat while NOT EQUAL, stop when EQUAL
	code := []byte{0xF2, 0x66, 0xA7, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	// Should stop at the first match (0xBEEF == 0xBEEF) at offset 4
	if v := c.GetReg32(ESI); v != 0x106 {
		t.Errorf("ESI = 0x%08X, want 0x106", v)
	}
	if v := c.GetReg32(EDI); v != 0x206 {
		t.Errorf("EDI = 0x%08X, want 0x206", v)
	}
	if v := c.GetReg32(ECX); v != 1 {
		t.Errorf("ECX = 0x%08X, want 1", v)
	}
	if !c.getZF() {
		t.Errorf("ZF = false, want true (match found)")
	}
}

// ===== POP r/m (0x8F) tests =====

func TestPopReg32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x300)
	c.writeMem32(0x300, 0xDEADBEEF)
	code := []byte{0x8F, 0xC0, 0xF4} // POP EAX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", v)
	}
	if v := c.GetReg32(ESP); v != 0x304 {
		t.Errorf("ESP = 0x%08X, want 0x304", v)
	}
}

func TestPopMem32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x300)
	c.SetReg32(EBX, 0x400)
	c.writeMem32(0x300, 0xCAFEBABE)
	code := []byte{0x8F, 0x03, 0xF4} // POP DWORD [EBX]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x400); v != 0xCAFEBABE {
		t.Errorf("mem[0x400] = 0x%08X, want 0xCAFEBABE", v)
	}
	if v := c.GetReg32(ESP); v != 0x304 {
		t.Errorf("ESP = 0x%08X, want 0x304", v)
	}
}

func TestPopReg16(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x300)
	c.writeMem32(0x300, 0x1234BEEF)
	code := []byte{0x66, 0x8F, 0xC0, 0xF4} // POP AX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xBEEF {
		t.Errorf("AX = 0x%04X, want 0xBEEF", v)
	}
	if v := c.GetReg32(ESP); v != 0x302 {
		t.Errorf("ESP = 0x%08X, want 0x302", v)
	}
}

// ===== CLC/STC/CMC tests =====

func TestCLC(t *testing.T) {
	c := newTestCPU(t)
	c.eflags |= EFLAGS_CF
	code := []byte{0xF8, 0xF4} // CLC; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Errorf("CF = true, want false")
	}
}

func TestSTC(t *testing.T) {
	c := newTestCPU(t)
	c.eflags &^= EFLAGS_CF
	code := []byte{0xF9, 0xF4} // STC; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.getCF() {
		t.Errorf("CF = false, want true")
	}
}

func TestCMC(t *testing.T) {
	c := newTestCPU(t)
	c.eflags &^= EFLAGS_CF
	code := []byte{0xF5, 0xF5, 0xF4} // CMC; CMC; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if c.getCF() {
		t.Errorf("CF = true, want false")
	}
}

// ===== PUSHA/POPA 16-bit tests =====

func TestPushaPopa16Bit(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetReg16(SP, 0x400)
	c.SetReg16(AX, 0x1111)
	c.SetReg16(CX, 0x2222)
	c.SetReg16(DX, 0x3333)
	c.SetReg16(BX, 0x4444)
	c.SetReg16(BP, 0x5555)
	c.SetReg16(SI, 0x6666)
	c.SetReg16(DI, 0x7777)

	code := []byte{
		0x60,       // PUSHA
		0x31, 0xC0, // XOR AX, AX
		0x31, 0xC9, // XOR CX, CX
		0x61,       // POPA
		0xF4,       // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0x1111 {
		t.Errorf("AX = 0x%04X, want 0x1111", v)
	}
	if v := c.GetReg16(CX); v != 0x2222 {
		t.Errorf("CX = 0x%04X, want 0x2222", v)
	}
	if v := c.GetReg16(DX); v != 0x3333 {
		t.Errorf("DX = 0x%04X, want 0x3333", v)
	}
	if v := c.GetReg16(BX); v != 0x4444 {
		t.Errorf("BX = 0x%04X, want 0x4444", v)
	}
	if v := c.GetReg16(SP); v != 0x400 {
		t.Errorf("SP = 0x%04X, want 0x400", v)
	}
	if v := c.GetReg16(BP); v != 0x5555 {
		t.Errorf("BP = 0x%04X, want 0x5555", v)
	}
	if v := c.GetReg16(SI); v != 0x6666 {
		t.Errorf("SI = 0x%04X, want 0x6666", v)
	}
	if v := c.GetReg16(DI); v != 0x7777 {
		t.Errorf("DI = 0x%04X, want 0x7777", v)
	}
}

// ===== RCL/RCR tests =====

func TestRcl8(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x80)
	c.eflags |= EFLAGS_CF
	// C0 /2 ib = RCL r/m8, imm8
	code := []byte{0xC0, 0xD0, 0x01, 0xF4} // RCL AL, 1; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// 0x80 RCL 1 with CF=1: 10000000 -> 00000001 with CF=1 (MSB)
	if v := c.GetReg8(AL); v != 0x01 {
		t.Errorf("AL = 0x%02X, want 0x01", v)
	}
	if !c.getCF() {
		t.Errorf("CF = false, want true")
	}
}

func TestRcr8(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg8(AL, 0x01)
	c.eflags |= EFLAGS_CF
	// C0 /3 ib = RCR r/m8, imm8
	code := []byte{0xC0, 0xD8, 0x01, 0xF4} // RCR AL, 1; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// 0x01 RCR 1 with CF=1: 00000001 -> 10000000 with CF=1 (LSB)
	if v := c.GetReg8(AL); v != 0x80 {
		t.Errorf("AL = 0x%02X, want 0x80", v)
	}
	if !c.getCF() {
		t.Errorf("CF = false, want true")
	}
}

func TestRcl32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x80000000)
	c.eflags |= EFLAGS_CF
	code := []byte{0xC1, 0xD0, 0x01, 0xF4} // RCL EAX, 1; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x00000001 {
		t.Errorf("EAX = 0x%08X, want 0x00000001", v)
	}
	if !c.getCF() {
		t.Errorf("CF = false, want true")
	}
}

func TestRcr32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0x00000001)
	c.eflags |= EFLAGS_CF
	code := []byte{0xC1, 0xD8, 0x01, 0xF4} // RCR EAX, 1; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x80000000 {
		t.Errorf("EAX = 0x%08X, want 0x80000000", v)
	}
	if !c.getCF() {
		t.Errorf("CF = false, want true")
	}
}

// ===== CALLF/JMPF group 5 tests =====

func TestCallfMem32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x400)
	c.SetReg32(EBX, 0x500)
	// Target code at 0x2000: HLT
	c.writeMem8(0x2000, 0xF4)
	// Memory at DS:0x500 contains offset=0x00002000, selector=0x0008
	c.writeMem32(0x500, 0x00002000)
	c.writeMem16(0x504, 0x0008)
	// Set up GDT at 0x1000. Selector 0x0008 = index 1.
	c.segBase[GDTR] = 0x1000
	c.segLimit[GDTR] = 0xFFFF
	// Descriptor for index 1 at offset 8: base=0x00000000, limit=0xFFFFFFFF, code, present, 32-bit
	c.writeMem32(0x1008, 0x0000FFFF)
	c.writeMem32(0x100C, 0x00CF9B00)

	code := []byte{0xFF, 0x1B, 0xF4} // CALLF [EBX]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetEIP(); v != 0x2001 {
		t.Errorf("EIP = 0x%08X, want 0x2001 (after HLT)", v)
	}
	if v := c.seg[CS]; v != 0x0008 {
		t.Errorf("CS = 0x%04X, want 0x0008", v)
	}
	// Check return address on stack: old EIP = 0x1002 (after FF 1B)
	if v := c.readMem32(c.GetSegBase(SS) + c.GetReg32(ESP)); v != 0x1002 {
		t.Errorf("return EIP = 0x%08X, want 0x1002", v)
	}
	// Old CS = 0xF000 (Reset() default)
	if v := c.readMem32(c.GetSegBase(SS) + c.GetReg32(ESP) + 4); v != 0xF000 {
		t.Errorf("return CS = 0x%08X, want 0xF000", v)
	}
}

func TestJmpfMem32(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x500)
	// Target code at 0x2000: HLT
	c.writeMem8(0x2000, 0xF4)
	c.writeMem32(0x500, 0x00002000)
	c.writeMem16(0x504, 0x0008)
	c.segBase[GDTR] = 0x1000
	c.segLimit[GDTR] = 0xFFFF
	// Descriptor for index 1 at offset 8
	c.writeMem32(0x1008, 0x0000FFFF)
	c.writeMem32(0x100C, 0x00CF9B00)

	code := []byte{0xFF, 0x2B, 0xF4} // JMPF [EBX]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetEIP(); v != 0x2001 {
		t.Errorf("EIP = 0x%08X, want 0x2001 (after HLT)", v)
	}
	if v := c.seg[CS]; v != 0x0008 {
		t.Errorf("CS = 0x%04X, want 0x0008", v)
	}
}

// ===== INS/OUTS tests =====

func TestInsb(t *testing.T) {
	c := newTestCPU(t)
	portData := map[uint16]uint8{0x3F8: 0xAB}
	c.SetIOHandlers(
		func(p uint16) uint8 { return portData[p] },
		func(p uint16, v uint8) {},
		func(p uint16) uint16 { return 0 },
		func(p uint16, v uint16) {},
		func(p uint16) uint32 { return 0 },
		func(p uint16, v uint32) {},
	)
	c.SetReg16(DX, 0x3F8)
	c.SetReg32(EDI, 0x200)
	code := []byte{0x6C, 0xF4} // INSB; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem8(0x200); v != 0xAB {
		t.Errorf("mem[0x200] = 0x%02X, want 0xAB", v)
	}
	if v := c.GetReg32(EDI); v != 0x201 {
		t.Errorf("EDI = 0x%08X, want 0x201", v)
	}
}

func TestOutsb(t *testing.T) {
	c := newTestCPU(t)
	var outBuf bytes.Buffer
	c.SetIOHandlers(
		func(p uint16) uint8 { return 0 },
		func(p uint16, v uint8) { outBuf.WriteByte(v) },
		func(p uint16) uint16 { return 0 },
		func(p uint16, v uint16) {},
		func(p uint16) uint32 { return 0 },
		func(p uint16, v uint32) {},
	)
	c.SetReg16(DX, 0x3F8)
	c.SetReg32(ESI, 0x200)
	c.writeMem8(0x200, 0xCD)
	code := []byte{0x6E, 0xF4} // OUTSB; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if outBuf.String() != "\xcd" {
		t.Errorf("output = %q, want %q", outBuf.String(), "\xcd")
	}
	if v := c.GetReg32(ESI); v != 0x201 {
		t.Errorf("ESI = 0x%08X, want 0x201", v)
	}
}

func TestRepInsw(t *testing.T) {
	c := newTestCPU(t)
	portData := []uint8{0xAB, 0xCD, 0xEF, 0x12}
	idx := 0
	c.SetIOHandlers(
		func(p uint16) uint8 {
			v := portData[idx]
			idx++
			return v
		},
		func(p uint16, v uint8) {},
		func(p uint16) uint16 {
			v := uint16(portData[idx]) | uint16(portData[idx+1])<<8
			idx += 2
			return v
		},
		func(p uint16, v uint16) {},
		func(p uint16) uint32 { return 0 },
		func(p uint16, v uint32) {},
	)
	c.SetReg16(DX, 0x3F8)
	c.SetReg32(EDI, 0x200)
	c.SetReg32(ECX, 2)
	code := []byte{0xF3, 0x66, 0x6D, 0xF4} // REP INSW; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem16(0x200); v != 0xCDAB {
		t.Errorf("mem[0x200] = 0x%04X, want 0xCDAB", v)
	}
	if v := c.readMem16(0x202); v != 0x12EF {
		t.Errorf("mem[0x202] = 0x%04X, want 0x12EF", v)
	}
	if v := c.GetReg32(EDI); v != 0x204 {
		t.Errorf("EDI = 0x%08X, want 0x204", v)
	}
}

func TestRepOutsd(t *testing.T) {
	c := newTestCPU(t)
	c.SetIOHandlers(
		func(p uint16) uint8 { return 0 },
		func(p uint16, v uint8) {},
		func(p uint16) uint16 { return 0 },
		func(p uint16, v uint16) {},
		func(p uint16) uint32 {
			return 0 // we don't test 32-bit output directly; just verify no crash
		},
		func(p uint16, v uint32) {},
	)
	c.SetReg16(DX, 0x3F8)
	c.SetReg32(ESI, 0x200)
	c.SetReg32(ECX, 2)
	c.writeMem32(0x200, 0xDEADBEEF)
	c.writeMem32(0x204, 0xCAFEBABE)
	code := []byte{0xF3, 0x6F, 0xF4} // REP OUTSD; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(ESI); v != 0x208 {
		t.Errorf("ESI = 0x%08X, want 0x208", v)
	}
}

// ===== A0-A3 segment base tests =====

func TestMovMoffsWithSegmentBase(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	// Real mode, DS base = 0x10000
	c.seg[DS] = 0x1000
	c.segBase[DS] = 0x10000
	c.SetReg8(AL, 0x42)
	// A2 writes AL to DS:0x200 = physical 0x10200
	code := []byte{0xA2, 0x00, 0x02, 0xF4} // MOV [0x0200], AL; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readPhys8(0x10200); v != 0x42 {
		t.Errorf("phys[0x10200] = 0x%02X, want 0x42", v)
	}
}

func TestMovMoffsReadWithSegmentBase(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	c.seg[DS] = 0x1000
	c.segBase[DS] = 0x10000
	c.writePhys8(0x10200, 0x99)
	code := []byte{0xA0, 0x00, 0x02, 0xF4} // MOV AL, [0x0200]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg8(AL); v != 0x99 {
		t.Errorf("AL = 0x%02X, want 0x99", v)
	}
}

func TestMovMoffsWithSegOverride(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	c.seg[FS] = 0x2000
	c.segBase[FS] = 0x20000
	c.writePhys8(0x20200, 0x77)
	// 64 = FS override, A0 = MOV AL, moffs8
	code := []byte{0x64, 0xA0, 0x00, 0x02, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg8(AL); v != 0x77 {
		t.Errorf("AL = 0x%02X, want 0x77", v)
	}
}

// ===== 0F 01 group tests (SGDT, SIDT, SMSW, INVLPG) =====

func TestSGDT(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[GDTR] = 0x12345678
	c.segLimit[GDTR] = 0x01FF
	c.SetReg32(EBX, 0x500)
	// 0F 01 /0 = SGDT m
	code := []byte{0x0F, 0x01, 0x03, 0xF4} // SGDT [EBX]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem16(0x500); v != 0x01FF {
		t.Errorf("limit = 0x%04X, want 0x01FF", v)
	}
	if v := c.readMem32(0x502); v != 0x12345678 {
		t.Errorf("base = 0x%08X, want 0x12345678", v)
	}
}

func TestSIDT(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[IDTR] = 0xDEADBEEF
	c.segLimit[IDTR] = 0x03FF
	c.SetReg32(EBX, 0x500)
	code := []byte{0x0F, 0x01, 0x0B, 0xF4} // SIDT [EBX]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem16(0x500); v != 0x03FF {
		t.Errorf("limit = 0x%04X, want 0x03FF", v)
	}
	if v := c.readMem32(0x502); v != 0xDEADBEEF {
		t.Errorf("base = 0x%08X, want 0xDEADBEEF", v)
	}
}

func TestSMSW(t *testing.T) {
	c := newTestCPU(t)
	c.cr[0] = 0x00000001 // PE bit set
	code := []byte{0x0F, 0x01, 0xE0, 0xF4} // SMSW EAX; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x00000001 {
		t.Errorf("EAX = 0x%08X, want 0x00000001", v)
	}
}

func TestINVLPG(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EBX, 0x500)
	code := []byte{0x0F, 0x01, 0x3B, 0xF4} // INVLPG [EBX]; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	// INVLPG is a NOP in this emulator (no TLB).
}
