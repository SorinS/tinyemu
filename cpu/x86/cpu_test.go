package x86

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func newTestCPU(t *testing.T) *CPU {
	mm := mem.NewPhysMemoryMap()
	// Register 1MB of RAM at physical address 0
	_, err := mm.RegisterRAM(0, 1<<20, 0)
	if err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := NewCPU(mm)
	// Most unit tests exercise 32-bit instructions. Simulate a 32-bit
	// protected-mode code segment by setting CR0.PE and the CS D-bit.
	c.SetCR(0, c.GetCR(0)|CR0_PE)
	c.SetSegAccess(CS, 0x0400) // D-bit = 1
	return c
}

func TestRegisterAccessors(t *testing.T) {
	c := newTestCPU(t)

	// 32-bit
	c.SetReg32(EAX, 0x12345678)
	if v := c.GetReg32(EAX); v != 0x12345678 {
		t.Errorf("EAX = 0x%08X, want 0x12345678", v)
	}

	// 16-bit
	if v := c.GetReg16(AX); v != 0x5678 {
		t.Errorf("AX = 0x%04X, want 0x5678", v)
	}
	c.SetReg16(AX, 0xABCD)
	if v := c.GetReg32(EAX); v != 0x1234ABCD {
		t.Errorf("EAX = 0x%08X, want 0x1234ABCD", v)
	}

	// 8-bit
	if v := c.GetReg8(AL); v != 0xCD {
		t.Errorf("AL = 0x%02X, want 0xCD", v)
	}
	if v := c.GetReg8(AH); v != 0xAB {
		t.Errorf("AH = 0x%02X, want 0xAB", v)
	}
	c.SetReg8(AH, 0xEF)
	if v := c.GetReg32(EAX); v != 0x1234EFCD {
		t.Errorf("EAX = 0x%08X, want 0x1234EFCD", v)
	}

	// Zero register
	if v := c.GetReg32(EZR); v != 0 {
		t.Errorf("EZR != 0")
	}
	c.SetReg32(EZR, 0xFFFFFFFF)
	if v := c.GetReg32(EZR); v != 0 {
		t.Errorf("EZR should remain 0 after SetReg32")
	}
}

func TestResetState(t *testing.T) {
	c := newTestCPU(t)
	if c.GetSeg(CS) != 0xF000 {
		t.Errorf("CS = 0x%04X, want 0xF000", c.GetSeg(CS))
	}
	if c.GetSegBase(CS) != 0xF0000 {
		t.Errorf("CS base = 0x%05X, want 0xF0000", c.GetSegBase(CS))
	}
	if c.GetEIP() != 0xFFF0 {
		t.Errorf("EIP = 0x%04X, want 0xFFF0", c.GetEIP())
	}
}

// runCode places code at CS:IP and runs it.
func runCode(t *testing.T, c *CPU, code []byte, ip uint32) error {
	// Write code to physical memory at CS base + ip
	base := c.GetSegBase(CS)
	for i, b := range code {
		c.writeMem8(base+ip+uint32(i), b)
	}
	c.SetEIP(ip)
	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			return err
		}
	}
	return nil
}

func TestMovRegImm(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB8, 0x78, 0x56, 0x34, 0x12, // MOV EAX, 0x12345678
		0xB9, 0xEF, 0xBE, 0xAD, 0xDE, // MOV ECX, 0xDEADBEEF
		0xF4, // HLT
	}
	// Run from CS:IP = 0x0000:0x1000 so LIP = 0x1000
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x12345678 {
		t.Errorf("EAX = 0x%08X, want 0x12345678", v)
	}
	if v := c.GetReg32(ECX); v != 0xDEADBEEF {
		t.Errorf("ECX = 0x%08X, want 0xDEADBEEF", v)
	}
}

func TestPushPop(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x100)
	code := []byte{
		0x50, // PUSH EAX
		0x51, // PUSH ECX
		0x59, // POP ECX
		0x58, // POP EAX
		0xF4, // HLT
	}
	c.SetReg32(EAX, 0x11111111)
	c.SetReg32(ECX, 0x22222222)
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x11111111 {
		t.Errorf("EAX = 0x%08X, want 0x11111111", v)
	}
	if v := c.GetReg32(ECX); v != 0x22222222 {
		t.Errorf("ECX = 0x%08X, want 0x22222222", v)
	}
	if v := c.GetReg32(ESP); v != 0x100 {
		t.Errorf("ESP = 0x%08X, want 0x100", v)
	}
}

func TestAdd(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB8, 0x10, 0x00, 0x00, 0x00, // MOV EAX, 16
		0x05, 0x20, 0x00, 0x00, 0x00, // ADD EAX, 32
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 48 {
		t.Errorf("EAX = %d, want 48", v)
	}
	if c.getZF() || c.getCF() || c.getOF() {
		t.Errorf("flags incorrect after ADD: ZF=%v CF=%v OF=%v", c.getZF(), c.getCF(), c.getOF())
	}
}

func TestIncDec(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB8, 0xFF, 0xFF, 0xFF, 0xFF, // MOV EAX, 0xFFFFFFFF
		0x40, // INC EAX
		0x48, // DEC EAX
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xFFFFFFFF {
		t.Errorf("EAX = 0x%08X, want 0xFFFFFFFF", v)
	}
}

func TestJmpShort(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xEB, 0x05, // JMP +5
		0xB8, 0xFF, 0xFF, 0xFF, 0xFF, // MOV EAX, 0xFFFFFFFF (skipped)
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0 {
		t.Errorf("EAX = 0x%08X, want 0 (JMP should skip MOV)", v)
	}
}

func TestCallRet(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ESP, 0x200)
	code := []byte{
		0xE8, 0x06, 0x00, 0x00, 0x00, // CALL rel32 (+6)
		0xB8, 0xAA, 0xAA, 0xAA, 0xAA, // MOV EAX, 0xAAAAAAAA
		0xF4,                         // HLT
		0xB8, 0xBB, 0xBB, 0xBB, 0xBB, // MOV EAX, 0xBBBBBBBB
		0xC3, // RET
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xAAAAAAAA {
		t.Errorf("EAX = 0x%08X, want 0xAAAAAAAA", v)
	}
}

func TestMemoryAccess(t *testing.T) {
	c := newTestCPU(t)
	// Write a value to memory at DS:0x100
	c.writeMem32(0x100, 0xDEADBEEF)
	code := []byte{
		0xA1, 0x00, 0x01, 0x00, 0x00, // MOV EAX, [DS:0x00000100]
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", v)
	}
}

// TestRealModeInterrupt validates handleInterrupt in real mode.
func TestRealModeInterrupt(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE) // real mode
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	// Set up IVT at 0x0000: handler for vector 0x08 at CS:IP = 0x1000:0x0200
	c.writeMem16(0x08*4+0, 0x0200) // IP
	c.writeMem16(0x08*4+2, 0x1000) // CS

	// Handler code at 0x1000:0x0200 = physical 0x10200
	// MOV AX, 0xBEEF
	// IRET
	handler := []byte{0xB8, 0xEF, 0xBE, 0xCF}
	for i, b := range handler {
		c.writeMem8(0x10200+uint32(i), b)
	}

	// Main code at 0x1000:0x0000 = physical 0x10000
	// STI
	// HLT
	code := []byte{0xFB, 0xF4}
	for i, b := range code {
		c.writeMem8(0x10000+uint32(i), b)
	}
	c.SetEIP(0x0000)
	c.SetSeg(CS, 0x1000)
	c.SetSegBase(CS, 0x10000)

	// Step 1: STI
	if err := c.Step(); err != nil {
		t.Fatalf("STI step error: %v", err)
	}
	if c.eflags&EFLAGS_IF == 0 {
		t.Fatalf("IF not set after STI")
	}

	// Step 2: HLT
	if err := c.Step(); err != nil {
		t.Fatalf("HLT step error: %v", err)
	}
	if !c.IsPowerDown() {
		t.Fatalf("CPU not halted after HLT")
	}

	// Inject hardware interrupt vector 0x08
	c.SetINTR(1)
	c.SetInterruptAckHandler(func() (uint8, bool) {
		c.SetINTR(0)
		return 0x08, true
	})

	// Step 3: CPU should service the interrupt instead of staying halted
	if err := c.Step(); err != nil {
		t.Fatalf("interrupt step error: %v", err)
	}
	if c.IsPowerDown() {
		t.Fatalf("CPU still halted after interrupt")
	}
	if c.GetSeg(CS) != 0x1000 {
		t.Errorf("CS = 0x%04X, want 0x1000", c.GetSeg(CS))
	}
	if c.GetEIP() != 0x0200 {
		t.Errorf("EIP = 0x%04X, want 0x0200", c.GetEIP())
	}

	// Step 4: execute handler MOV AX, 0xBEEF
	if err := c.Step(); err != nil {
		t.Fatalf("handler step error: %v", err)
	}
	if v := c.GetReg16(AX); v != 0xBEEF {
		t.Errorf("AX = 0x%04X, want 0xBEEF", v)
	}

	// Step 5: IRET
	if err := c.Step(); err != nil {
		t.Fatalf("IRET step error: %v", err)
	}
	if c.GetSeg(CS) != 0x1000 {
		t.Errorf("CS after IRET = 0x%04X, want 0x1000", c.GetSeg(CS))
	}
	if c.GetEIP() != 0x0002 {
		// EIP was advanced past HLT before the interrupt was taken
		t.Errorf("EIP after IRET = 0x%04X, want 0x0002", c.GetEIP())
	}
	if c.eflags&EFLAGS_IF == 0 {
		t.Errorf("IF cleared after IRET")
	}
}

// TestProtectedModeIDT validates protected-mode INT with an IDT gate.
func TestProtectedModeIDT(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	// Build GDT at 0x2000
	gdtAddr := uint32(0x2000)
	for i := 0; i < 32; i++ {
		c.writeMem8(gdtAddr+uint32(i), 0)
	}
	c.writeMem8(gdtAddr+0, 0x17)
	c.writeMem8(gdtAddr+1, 0x00)
	c.writeMem8(gdtAddr+2, 0x00)
	c.writeMem8(gdtAddr+3, 0x20)
	c.writeMem8(gdtAddr+4, 0x00)
	// Code segment at 0x08: flat, D-bit=1 (32-bit)
	c.writeMem8(gdtAddr+8, 0xFF)
	c.writeMem8(gdtAddr+9, 0xFF)
	c.writeMem8(gdtAddr+10, 0x00)
	c.writeMem8(gdtAddr+11, 0x00)
	c.writeMem8(gdtAddr+12, 0x00)
	c.writeMem8(gdtAddr+13, 0x9A)
	c.writeMem8(gdtAddr+14, 0xCF)
	c.writeMem8(gdtAddr+15, 0x00)
	// Data segment at 0x10: flat
	c.writeMem8(gdtAddr+16, 0xFF)
	c.writeMem8(gdtAddr+17, 0xFF)
	c.writeMem8(gdtAddr+18, 0x00)
	c.writeMem8(gdtAddr+19, 0x00)
	c.writeMem8(gdtAddr+20, 0x00)
	c.writeMem8(gdtAddr+21, 0x92)
	c.writeMem8(gdtAddr+22, 0xCF)
	c.writeMem8(gdtAddr+23, 0x00)

	// IDT descriptor at 0x3000 (limit + base), actual IDT at 0x3100
	idtDescAddr := uint32(0x3000)
	idtBase := uint32(0x3100)
	idtLimit := uint32(0x20)*8 + 7 // enough for vector 0x20
	c.writeMem8(idtDescAddr+0, uint8(idtLimit))
	c.writeMem8(idtDescAddr+1, uint8(idtLimit>>8))
	c.writeMem8(idtDescAddr+2, uint8(idtBase))
	c.writeMem8(idtDescAddr+3, uint8(idtBase>>8))
	c.writeMem8(idtDescAddr+4, uint8(idtBase>>16))
	c.writeMem8(idtDescAddr+5, 0x00)

	// Clear IDT area
	for i := 0; i < 256; i++ {
		c.writeMem8(idtBase+uint32(i), 0)
	}
	// Gate 0x20 at offset 0x20*8 = 0x100 within IDT
	// Handler at 0x00004000, selector 0x08
	// 32-bit interrupt gate (type 0x0E), DPL=0, P=1
	gateOffset := uint32(0x20) * 8
	c.writeMem8(idtBase+gateOffset+0, 0x00)       // offset low
	c.writeMem8(idtBase+gateOffset+1, 0x40)
	c.writeMem8(idtBase+gateOffset+2, 0x08)       // selector low
	c.writeMem8(idtBase+gateOffset+3, 0x00)       // selector high
	c.writeMem8(idtBase+gateOffset+4, 0x00)       // reserved
	c.writeMem8(idtBase+gateOffset+5, 0x8E)       // P=1, DPL=00, type=0x0E
	c.writeMem8(idtBase+gateOffset+6, 0x00)       // offset high
	c.writeMem8(idtBase+gateOffset+7, 0x00)

	// LGDT [0x2000]
	c.writeMem8(0x1000, 0x0F)
	c.writeMem8(0x1001, 0x01)
	c.writeMem8(0x1002, 0x16)
	c.writeMem8(0x1003, 0x00)
	c.writeMem8(0x1004, 0x20)
	// LIDT [0x3000]
	c.writeMem8(0x1005, 0x0F)
	c.writeMem8(0x1006, 0x01)
	c.writeMem8(0x1007, 0x1E)
	c.writeMem8(0x1008, 0x00)
	c.writeMem8(0x1009, 0x30)
	// MOV EAX, CR0
	c.writeMem8(0x100A, 0x0F)
	c.writeMem8(0x100B, 0x20)
	c.writeMem8(0x100C, 0xC0)
	// OR EAX, 1
	c.writeMem8(0x100D, 0x83)
	c.writeMem8(0x100E, 0xC8)
	c.writeMem8(0x100F, 0x01)
	// MOV CR0, EAX
	c.writeMem8(0x1010, 0x0F)
	c.writeMem8(0x1011, 0x22)
	c.writeMem8(0x1012, 0xC0)
	// JMPF 0x08:0x1020
	c.writeMem8(0x1013, 0x66)
	c.writeMem8(0x1014, 0xEA)
	c.writeMem8(0x1015, 0x20)
	c.writeMem8(0x1016, 0x10)
	c.writeMem8(0x1017, 0x00)
	c.writeMem8(0x1018, 0x00)
	c.writeMem8(0x1019, 0x08)
	c.writeMem8(0x101A, 0x00)
	// Protected mode code at 0x1020:
	// MOV AX, 0x10
	c.writeMem8(0x1020, 0x66)
	c.writeMem8(0x1021, 0xB8)
	c.writeMem8(0x1022, 0x10)
	c.writeMem8(0x1023, 0x00)
	// MOV DS, AX
	c.writeMem8(0x1024, 0x8E)
	c.writeMem8(0x1025, 0xD8)
	// MOV SS, AX
	c.writeMem8(0x1026, 0x8E)
	c.writeMem8(0x1027, 0xD0)
	// MOV ESP, 0x00008000
	c.writeMem8(0x1028, 0x66)
	c.writeMem8(0x1029, 0xBC)
	c.writeMem8(0x102A, 0x00)
	c.writeMem8(0x102B, 0x80)
	c.writeMem8(0x102C, 0x00)
	c.writeMem8(0x102D, 0x00)
	// STI
	c.writeMem8(0x102E, 0xFB)
	// INT 0x20
	c.writeMem8(0x102F, 0xCD)
	c.writeMem8(0x1030, 0x20)
	// HLT
	c.writeMem8(0x1031, 0xF4)

	// Handler at 0x00004000:
	// MOV EAX, 0xDEADBEEF
	c.writeMem8(0x4000, 0xB8)
	c.writeMem8(0x4001, 0xEF)
	c.writeMem8(0x4002, 0xBE)
	c.writeMem8(0x4003, 0xAD)
	c.writeMem8(0x4004, 0xDE)
	// IRET
	c.writeMem8(0x4005, 0xCF)

	c.SetEIP(0x1000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	// Run through protected mode transition + INT + handler + IRET + HLT
	for i := 0; i < 20; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}
	}
	if !c.IsPowerDown() {
		t.Fatalf("CPU did not halt")
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", v)
	}
	if c.GetEIP() != 0x1032 {
		// INT is 2 bytes at 0x102F; return address is 0x1031 (HLT).
		// HLT advances EIP to 0x1032 before halting.
		t.Errorf("EIP = 0x%08X, want 0x00001032", c.GetEIP())
	}
	if c.eflags&EFLAGS_IF == 0 {
		t.Errorf("IF cleared after IRET from interrupt gate")
	}
}

// TestPagingIdentityMap validates basic paging with identity-mapped page tables.
func TestPagingIdentityMap(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x7000)

	pdAddr := uint32(0x10000)
	ptAddr := uint32(0x11000)

	c.writePhys32(pdAddr+0, ptAddr|0x07)
	for i := uint32(1); i < 1024; i++ {
		c.writePhys32(pdAddr+i*4, 0)
	}
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(ptAddr+i*4, (i<<12)|0x07)
	}

	c.writePhys16(0x0E*4+0, 0x0200)
	c.writePhys16(0x0E*4+2, 0x0000)
	c.writePhys8(0x0200, 0xB8)
	c.writePhys8(0x0201, 0xEF)
	c.writePhys8(0x0202, 0xBE)
	c.writePhys8(0x0203, 0xCF)

	gdtAddr := uint32(0x4000)
	// Null descriptor at offset 0
	for i := uint32(0); i < 8; i++ {
		c.writePhys8(gdtAddr+i, 0)
	}
	// Code segment descriptor at offset 8 (selector 0x08)
	c.writePhys8(gdtAddr+8, 0xFF)
	c.writePhys8(gdtAddr+9, 0xFF)
	c.writePhys8(gdtAddr+10, 0x00)
	c.writePhys8(gdtAddr+11, 0x00)
	c.writePhys8(gdtAddr+12, 0x00)
	c.writePhys8(gdtAddr+13, 0x9A)
	c.writePhys8(gdtAddr+14, 0xCF)
	c.writePhys8(gdtAddr+15, 0x00)
	c.writePhys16(0x4010, 0x000F)
	c.writePhys32(0x4012, gdtAddr)

	c.writePhys8(0x1000, 0x0F)
	c.writePhys8(0x1001, 0x01)
	c.writePhys8(0x1002, 0x16)
	c.writePhys8(0x1003, 0x10)
	c.writePhys8(0x1004, 0x40)
	c.SetEIP(0x1000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	if err := c.Step(); err != nil {
		t.Fatalf("LGDT step error: %v", err)
	}

	// Write instructions with correct 0x66 prefixes for 32-bit ops in real mode.
	// 0x2000: MOV EAX, pdAddr
	c.writePhys8(0x2000, 0x66)
	c.writePhys8(0x2001, 0xB8)
	c.writePhys32(0x2002, pdAddr)
	// 0x2006: MOV CR3, EAX
	c.writePhys8(0x2006, 0x0F)
	c.writePhys8(0x2007, 0x22)
	c.writePhys8(0x2008, 0xD8)
	// 0x2009: MOV EAX, CR0
	c.writePhys8(0x2009, 0x0F)
	c.writePhys8(0x200A, 0x20)
	c.writePhys8(0x200B, 0xC0)
	// 0x200C: OR EAX, 0x80000001
	c.writePhys8(0x200C, 0x66)
	c.writePhys8(0x200D, 0x0D)
	c.writePhys32(0x200E, 0x80000001)
	// 0x2012: MOV CR0, EAX
	c.writePhys8(0x2012, 0x0F)
	c.writePhys8(0x2013, 0x22)
	c.writePhys8(0x2014, 0xC0)
	// 0x2015: 0x66 JMPF 0x08:0x3020
	c.writePhys8(0x2015, 0x66)
	c.writePhys8(0x2016, 0xEA)
	c.writePhys32(0x2017, 0x00003020)
	c.writePhys16(0x201B, 0x0008)

	// Protected mode code at 0x3020
	c.writePhys8(0x3020, 0xA1)
	c.writePhys32(0x3021, 0x00003000)
	c.writePhys8(0x3025, 0xF4)

	c.writePhys32(0x3000, 0xDEADBEEF)

	c.SetEIP(0x2000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	for i := 0; i < 20; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}
	}
	if !c.IsPowerDown() {
		t.Fatalf("CPU did not halt")
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", v)
	}
}

// TestPagingPageFault validates that a non-present page raises #PF.
func TestPagingPageFault(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x7000)

	pdAddr := uint32(0x10000)
	ptAddr := uint32(0x11000)

	c.writePhys32(pdAddr+0, ptAddr|0x07)
	for i := uint32(1); i < 1024; i++ {
		c.writePhys32(pdAddr+i*4, 0)
	}
	for i := uint32(0); i < 1024; i++ {
		if i == 5 {
			c.writePhys32(ptAddr+i*4, (i<<12)|0x06)
		} else {
			c.writePhys32(ptAddr+i*4, (i<<12)|0x07)
		}
	}

	// Page fault handler at 0x0200: MOV EAX, 0xADCFBEEF; HLT
	c.writePhys8(0x0200, 0xB8)
	c.writePhys8(0x0201, 0xEF)
	c.writePhys8(0x0202, 0xBE)
	c.writePhys8(0x0203, 0xCF)
	c.writePhys8(0x0204, 0xAD)
	c.writePhys8(0x0205, 0xF4) // HLT

	gdtAddr := uint32(0x4000)
	for i := uint32(0); i < 8; i++ {
		c.writePhys8(gdtAddr+i, 0)
	}
	c.writePhys8(gdtAddr+8, 0xFF)
	c.writePhys8(gdtAddr+9, 0xFF)
	c.writePhys8(gdtAddr+10, 0x00)
	c.writePhys8(gdtAddr+11, 0x00)
	c.writePhys8(gdtAddr+12, 0x00)
	c.writePhys8(gdtAddr+13, 0x9A)
	c.writePhys8(gdtAddr+14, 0xCF)
	c.writePhys8(gdtAddr+15, 0x00)
	c.writePhys16(0x4010, 0x000F)
	c.writePhys32(0x4012, gdtAddr)

	// IDT at 0x4020 with 15 entries (vectors 0x00-0x0E).
	idtBase := uint32(0x4020)
	for i := uint32(0); i < 15*8; i++ {
		c.writePhys8(idtBase+i, 0)
	}
	// Gate descriptor for vector 0x0E at offset 0x70.
	gateAddr := idtBase + 0x70
	c.writePhys8(gateAddr+0, 0x00)       // offset low
	c.writePhys8(gateAddr+1, 0x02)       // offset low >> 8
	c.writePhys8(gateAddr+2, 0x08)       // selector
	c.writePhys8(gateAddr+3, 0x00)       // selector >> 8
	c.writePhys8(gateAddr+4, 0x00)       // unused
	c.writePhys8(gateAddr+5, 0x8E)       // present=1, DPL=0, type=0x0E (32-bit interrupt gate)
	c.writePhys8(gateAddr+6, 0x00)       // offset high
	c.writePhys8(gateAddr+7, 0x00)       // offset high >> 8
	c.writePhys16(0x40A0, 0x0077)        // limit = 119 (15 entries)
	c.writePhys32(0x40A2, idtBase)

	c.writePhys8(0x1000, 0x0F)
	c.writePhys8(0x1001, 0x01)
	c.writePhys8(0x1002, 0x16)
	c.writePhys8(0x1003, 0x10)
	c.writePhys8(0x1004, 0x40)
	c.SetEIP(0x1000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	if err := c.Step(); err != nil {
		t.Fatalf("LGDT step error: %v", err)
	}

	c.writePhys8(0x1005, 0x0F)
	c.writePhys8(0x1006, 0x01)
	c.writePhys8(0x1007, 0x1E)
	c.writePhys8(0x1008, 0xA0)
	c.writePhys8(0x1009, 0x40)
	c.SetEIP(0x1005)
	if err := c.Step(); err != nil {
		t.Fatalf("LIDT step error: %v", err)
	}

	c.writePhys8(0x2000, 0x66)
	c.writePhys8(0x2001, 0xB8)
	c.writePhys32(0x2002, pdAddr)
	c.writePhys8(0x2006, 0x0F)
	c.writePhys8(0x2007, 0x22)
	c.writePhys8(0x2008, 0xD8)
	c.writePhys8(0x2009, 0x0F)
	c.writePhys8(0x200A, 0x20)
	c.writePhys8(0x200B, 0xC0)
	c.writePhys8(0x200C, 0x66)
	c.writePhys8(0x200D, 0x0D)
	c.writePhys32(0x200E, 0x80000001)
	c.writePhys8(0x2012, 0x0F)
	c.writePhys8(0x2013, 0x22)
	c.writePhys8(0x2014, 0xC0)
	c.writePhys8(0x2015, 0x66)
	c.writePhys8(0x2016, 0xEA)
	c.writePhys32(0x2017, 0x00003020)
	c.writePhys16(0x201B, 0x0008)

	c.writePhys8(0x3020, 0xA1)
	c.writePhys32(0x3021, 0x00005000)
	c.writePhys8(0x3025, 0xF4)

	c.SetEIP(0x2000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	for i := 0; i < 20; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}
	}
	if !c.IsPowerDown() {
		t.Fatalf("CPU did not halt")
	}
	if v := c.GetReg16(AX); v != 0xBEEF {
		t.Errorf("AX = 0x%04X, want 0xBEEF (handler did not run)", v)
	}
}

func TestRepMovs(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 4)
	c.SetReg32(ESI, 0x100)
	c.SetReg32(EDI, 0x200)
	c.writeMem32(0x100, 0x11111111)
	c.writeMem32(0x104, 0x22222222)
	c.writeMem32(0x108, 0x33333333)
	c.writeMem32(0x10C, 0x44444444)
	code := []byte{
		0xF3, 0xA5, // REP MOVSD
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x200); v != 0x11111111 {
		t.Errorf("dest[0] = 0x%08X, want 0x11111111", v)
	}
	if v := c.readMem32(0x204); v != 0x22222222 {
		t.Errorf("dest[1] = 0x%08X, want 0x22222222", v)
	}
	if v := c.readMem32(0x208); v != 0x33333333 {
		t.Errorf("dest[2] = 0x%08X, want 0x33333333", v)
	}
	if v := c.readMem32(0x20C); v != 0x44444444 {
		t.Errorf("dest[3] = 0x%08X, want 0x44444444", v)
	}
	if v := c.GetReg32(ECX); v != 0 {
		t.Errorf("ECX = %d, want 0", v)
	}
}

func TestShl(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB8, 0x01, 0x00, 0x00, 0x00, // MOV EAX, 1
		0xC1, 0xE0, 0x04, // SHL EAX, 4
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 16 {
		t.Errorf("EAX = %d, want 16", v)
	}
}

func TestDiv(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB8, 0x20, 0x00, 0x00, 0x00, // MOV EAX, 32
		0x31, 0xD2, // XOR EDX, EDX
		0xB9, 0x04, 0x00, 0x00, 0x00, // MOV ECX, 4
		0xF7, 0xF1, // DIV ECX
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 8 {
		t.Errorf("EAX = %d, want 8 (quotient)", v)
	}
	if v := c.GetReg32(EDX); v != 0 {
		t.Errorf("EDX = %d, want 0 (remainder)", v)
	}
}

func TestMovSeg(t *testing.T) {
	c := newTestCPU(t)
	// This test exercises real-mode segment loading; clear protected mode.
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	code := []byte{
		0xB8, 0x00, 0x10, 0x00, 0x00, // MOV EAX, 0x1000
		0x8E, 0xD8, // MOV DS, AX
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetSeg(DS); v != 0x1000 {
		t.Errorf("DS = 0x%04X, want 0x1000", v)
	}
	if v := c.GetSegBase(DS); v != 0x10000 {
		t.Errorf("DS base = 0x%05X, want 0x10000", v)
	}
}

func TestMovsx(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB0, 0xFF, // MOV AL, 0xFF
		0x0F, 0xBE, 0xC0, // MOVSX EAX, AL
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := int32(c.GetReg32(EAX)); v != -1 {
		t.Errorf("EAX = %d, want -1", v)
	}
}

func TestBswap(t *testing.T) {
	c := newTestCPU(t)
	code := []byte{
		0xB8, 0x78, 0x56, 0x34, 0x12, // MOV EAX, 0x12345678
		0x0F, 0xC8, // BSWAP EAX
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x78563412 {
		t.Errorf("EAX = 0x%08X, want 0x78563412", v)
	}
}

func TestCpuid(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0)
	code := []byte{
		0x0F, 0xA2, // CPUID
		0xF4, // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EBX); v != 0x756E6547 {
		t.Errorf("EBX = 0x%08X, want 0x756E6547 (Genu)", v)
	}
}

// TestProtectedModeTransition validates LGDT, CR0.PE, and far JMP.
func TestProtectedModeTransition(t *testing.T) {
	c := newTestCPU(t)
	// Clear protected mode so we start in real mode
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	// Clear the CS D-bit that newTestCPU sets, so the transition
	// state uses 16-bit default operand size (real-mode CS descriptor).
	c.SetSegAccess(CS, 0)

	// Build a GDT at physical address 0x2000.
	// The GDT descriptor (used by LGDT) is at 0x2000:
	//   limit = 23 (0x17), base = 0x00002000
	// Entries start at 0x2008:
	//   Entry 0: null descriptor
	//   Entry 1 (0x08): 4GB flat code segment
	//   Entry 2 (0x10): 4GB flat data segment
	gdtAddr := uint32(0x2000)
	for i := 0; i < 32; i++ {
		c.writeMem8(gdtAddr+uint32(i), 0)
	}
	// GDT descriptor at 0x2000 (limit=0x0017, base=0x00002000)
	c.writeMem8(gdtAddr+0, 0x17) // limit low
	c.writeMem8(gdtAddr+1, 0x00) // limit high
	c.writeMem8(gdtAddr+2, 0x00) // base low
	c.writeMem8(gdtAddr+3, 0x20) // base mid
	c.writeMem8(gdtAddr+4, 0x00) // base high
	// Code segment descriptor at offset 8:
	// limit=0xFFFF, base=0x00000000, access=0x9A, flags=0xCF
	c.writeMem8(gdtAddr+8, 0xFF)
	c.writeMem8(gdtAddr+9, 0xFF)
	c.writeMem8(gdtAddr+10, 0x00)
	c.writeMem8(gdtAddr+11, 0x00)
	c.writeMem8(gdtAddr+12, 0x00)
	c.writeMem8(gdtAddr+13, 0x9A)
	c.writeMem8(gdtAddr+14, 0xCF)
	c.writeMem8(gdtAddr+15, 0x00)
	// Data segment descriptor at offset 16:
	// limit=0xFFFF, base=0x00000000, access=0x92, flags=0xCF
	c.writeMem8(gdtAddr+16, 0xFF)
	c.writeMem8(gdtAddr+17, 0xFF)
	c.writeMem8(gdtAddr+18, 0x00)
	c.writeMem8(gdtAddr+19, 0x00)
	c.writeMem8(gdtAddr+20, 0x00)
	c.writeMem8(gdtAddr+21, 0x92)
	c.writeMem8(gdtAddr+22, 0xCF)
	c.writeMem8(gdtAddr+23, 0x00)

	// Ensure CS base is 0 so runCode writes to physical 0x1000 and the
	// far JMP to a flat descriptor continues from the same linear address.
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)

	// Code at 0x1000:
	// 1. Load GDT descriptor (LGDT [0x2000])
	// 2. Enable protected mode (MOV CR0, EAX)
	// 3. Far JMP to selector 0x08, offset 0x1000 + after_jmp
	// 4. In protected mode: MOV EAX, 0xDEADBEEF
	// 5. HLT
	code := []byte{
		// LGDT [0x2000]
		0x0F, 0x01, 0x16, 0x00, 0x20,
		// MOV EAX, CR0
		0x0F, 0x20, 0xC0,
		// OR EAX, 1
		0x83, 0xC8, 0x01,
		// MOV CR0, EAX
		0x0F, 0x22, 0xC0,
		// JMPF 0x08:pm_start
		0x66, 0xEA,
	}
	// The far JMP in 16-bit mode uses a 32-bit offset with 0x66 prefix
	// After the prefix+opcode, we need: offset32 (4 bytes), selector (2 bytes)
	pmStart := uint32(0x1000 + len(code) + 6)
	code = append(code, byte(pmStart), byte(pmStart>>8), byte(pmStart>>16), byte(pmStart>>24))
	code = append(code, 0x08, 0x00)
	// Protected mode code:
	code = append(code,
		0xB8, 0xEF, 0xBE, 0xAD, 0xDE, // MOV EAX, 0xDEADBEEF
		0xF4, // HLT
	)
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if !c.IsProtectedMode() {
		t.Errorf("not in protected mode")
	}
	if v := c.GetSeg(CS); v != 0x0008 {
		t.Errorf("CS = 0x%04X, want 0x0008", v)
	}
	if v := c.GetSegBase(CS); v != 0x00000 {
		t.Errorf("CS base = 0x%05X, want 0x00000", v)
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF", v)
	}
}
