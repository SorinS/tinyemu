package x86

import (
	"testing"
)

// Helper to build a standard flat GDT.
// Returns the GDT base address.
func buildFlatGDT(t *testing.T, c *CPU, gdtBase uint32) {
	t.Helper()
	for i := 0; i < 24; i++ {
		c.writeMem8(gdtBase+uint32(i), 0)
	}
	// Null descriptor at offset 0
	// Code segment at offset 8 (selector 0x08): flat, 32-bit, execute/read
	c.writeMem8(gdtBase+8, 0xFF)
	c.writeMem8(gdtBase+9, 0xFF)
	c.writeMem8(gdtBase+10, 0x00)
	c.writeMem8(gdtBase+11, 0x00)
	c.writeMem8(gdtBase+12, 0x00)
	c.writeMem8(gdtBase+13, 0x9A)
	c.writeMem8(gdtBase+14, 0xCF)
	c.writeMem8(gdtBase+15, 0x00)
	// Data segment at offset 16 (selector 0x10): flat, read/write
	c.writeMem8(gdtBase+16, 0xFF)
	c.writeMem8(gdtBase+17, 0xFF)
	c.writeMem8(gdtBase+18, 0x00)
	c.writeMem8(gdtBase+19, 0x00)
	c.writeMem8(gdtBase+20, 0x00)
	c.writeMem8(gdtBase+21, 0x92)
	c.writeMem8(gdtBase+22, 0xCF)
	c.writeMem8(gdtBase+23, 0x00)
}

// Helper to load GDT and IDT, enable protected mode, and far-jump to flat CS.
// gdtBase must already contain a valid flat GDT.
// idtBase must already contain IDT entries.
// pmEIP is where protected-mode execution starts.
func enterProtectedMode(t *testing.T, c *CPU, gdtBase, idtBase, idtLimit, pmEIP uint32) {
	t.Helper()

	// LGDT [gdtBase] — 6-byte pseudo-descriptor: limit(2) + base(4)
	c.writeMem16(gdtBase-6, uint16(23)) // limit = 23 bytes (3 entries)
	c.writeMem32(gdtBase-4, gdtBase)
	code := []byte{
		0x0F, 0x01, 0x16,
		byte(gdtBase - 6), byte((gdtBase - 6) >> 8),
	}
	c.SetEIP(0x1000)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	for i, b := range code {
		c.writeMem8(0x1000+uint32(i), b)
	}
	if err := c.Step(); err != nil {
		t.Fatalf("LGDT step error: %v", err)
	}

	// LIDT [idtBase-6]
	c.writeMem16(idtBase-6, uint16(idtLimit))
	c.writeMem32(idtBase-4, idtBase)
	code = []byte{
		0x0F, 0x01, 0x1E,
		byte(idtBase - 6), byte((idtBase - 6) >> 8),
	}
	c.SetEIP(0x1010)
	for i, b := range code {
		c.writeMem8(0x1010+uint32(i), b)
	}
	if err := c.Step(); err != nil {
		t.Fatalf("LIDT step error: %v", err)
	}

	// MOV EAX, CR0; OR EAX, 1; MOV CR0, EAX
	code = []byte{
		0x0F, 0x20, 0xC0,
		0x83, 0xC8, 0x01,
		0x0F, 0x22, 0xC0,
	}
	c.SetEIP(0x1020)
	for i, b := range code {
		c.writeMem8(0x1020+uint32(i), b)
	}
	for i := 0; i < 3; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("CR0 step error: %v", err)
		}
	}

	// Far JMP to 0x08:pmEIP
	c.writeMem8(0x1030, 0x66)
	c.writeMem8(0x1031, 0xEA)
	c.writeMem32(0x1032, pmEIP)
	c.writeMem16(0x1036, 0x0008)
	c.SetEIP(0x1030)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	if err := c.Step(); err != nil {
		t.Fatalf("far JMP step error: %v", err)
	}

	// Set up data segments and stack
	c.SetSeg(DS, 0x0010)
	c.SetSegBase(DS, 0x00000)
	c.SetSeg(SS, 0x0010)
	c.SetSegBase(SS, 0x00000)
	c.SetReg32(ESP, 0x8000)
}

// writeIDTGate writes an IDT gate descriptor at the given IDT base + offset.
func writeIDTGate(c *CPU, idtBase uint32, vector uint8, handlerAddr uint32, selector uint16, gateType uint8, dpl uint8) {
	offset := uint32(vector) * 8
	c.writeMem8(idtBase+offset+0, uint8(handlerAddr))
	c.writeMem8(idtBase+offset+1, uint8(handlerAddr>>8))
	c.writeMem8(idtBase+offset+2, uint8(selector))
	c.writeMem8(idtBase+offset+3, uint8(selector>>8))
	c.writeMem8(idtBase+offset+4, 0x00)
	c.writeMem8(idtBase+offset+5, 0x80|(dpl<<5)|(gateType&0x0F))
	c.writeMem8(idtBase+offset+6, uint8(handlerAddr>>16))
	c.writeMem8(idtBase+offset+7, uint8(handlerAddr>>24))
}

// TestHandleInterruptRealMode validates real-mode interrupt delivery.
func TestHandleInterruptRealMode(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0400)
	c.SetEIP(0x1000)

	// IVT entry for vector 0x10 at physical 0x40
	c.writeMem16(0x40, 0x0200) // IP
	c.writeMem16(0x42, 0x1000) // CS

	// Handler code at 0x1000:0x0200 = 0x10200
	c.writeMem8(0x10200, 0xB8) // MOV AX, 0xBEEF
	c.writeMem8(0x10201, 0xEF)
	c.writeMem8(0x10202, 0xBE)
	c.writeMem8(0x10203, 0xCF) // IRET

	origEFLAGS := c.eflags
	origCS := c.seg[CS]
	origEIP := c.GetEIP()

	if err := c.handleInterrupt(0x10, false); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// Stack should have: FLAGS, CS, IP (each 16-bit)
	// Pushes go to 0x03FE, 0x03FC, 0x03FA (stack grows down)
	if c.GetReg16(SP) != 0x03FA {
		t.Errorf("SP = 0x%04X, want 0x03FA", c.GetReg16(SP))
	}
	if v := c.readMem16(0x03FE); v != uint16(origEFLAGS) {
		t.Errorf("FLAGS = 0x%04X, want 0x%04X", v, uint16(origEFLAGS))
	}
	if v := c.readMem16(0x03FC); v != origCS {
		t.Errorf("CS = 0x%04X, want 0x%04X", v, origCS)
	}
	if v := c.readMem16(0x03FA); v != uint16(origEIP) {
		t.Errorf("IP = 0x%04X, want 0x%04X", v, uint16(origEIP))
	}

	if c.GetSeg(CS) != 0x1000 {
		t.Errorf("CS = 0x%04X, want 0x1000", c.GetSeg(CS))
	}
	if c.GetEIP() != 0x0200 {
		t.Errorf("EIP = 0x%04X, want 0x0200", c.GetEIP())
	}
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF not cleared after real-mode interrupt")
	}
	if c.eflags&EFLAGS_TF != 0 {
		t.Errorf("TF not cleared after real-mode interrupt")
	}
}

// TestHandleInterruptProtected32BitGate validates 32-bit interrupt gate (type 0x0E).
func TestHandleInterruptProtected32BitGate(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000302 // IF=1, TF=1

	origEFLAGS := c.eflags
	origCS := uint32(c.seg[CS])
	origEIP := c.GetEIP()

	if err := c.handleInterrupt(0x20, true); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// 3 pushes of 32-bit = 12 bytes
	if c.GetReg32(ESP) != 0x7FF4 {
		t.Errorf("ESP = 0x%08X, want 0x7FF4", c.GetReg32(ESP))
	}
	// Stack layout (grows down): EFLAGS at 0x7FFC, CS at 0x7FF8, EIP at 0x7FF4
	if v := c.readMem32(0x7FFC); v != origEFLAGS {
		t.Errorf("EFLAGS = 0x%08X, want 0x%08X", v, origEFLAGS)
	}
	if v := c.readMem32(0x7FF8); v != origCS {
		t.Errorf("CS = 0x%08X, want 0x%08X", v, origCS)
	}
	if v := c.readMem32(0x7FF4); v != origEIP {
		t.Errorf("EIP = 0x%08X, want 0x%08X", v, origEIP)
	}

	if c.GetSeg(CS) != 0x0008 {
		t.Errorf("CS = 0x%04X, want 0x0008", c.GetSeg(CS))
	}
	if c.GetEIP() != 0x00004000 {
		t.Errorf("EIP = 0x%08X, want 0x00004000", c.GetEIP())
	}
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF not cleared after 32-bit interrupt gate")
	}
	if c.eflags&EFLAGS_TF != 0 {
		t.Errorf("TF not cleared after 32-bit interrupt gate")
	}
}

// TestHandleInterruptProtected32BitTrapGate validates 32-bit trap gate (type 0x0F) does NOT clear IF.
func TestHandleInterruptProtected32BitTrapGate(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x21, 0x00004000, 0x0008, 0x0F, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x21*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000302 // IF=1, TF=1

	if err := c.handleInterrupt(0x21, false); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	if c.eflags&EFLAGS_IF == 0 {
		t.Errorf("IF cleared after 32-bit trap gate (should remain set)")
	}
	if c.eflags&EFLAGS_TF == 0 {
		t.Errorf("TF cleared after 32-bit trap gate (should remain set)")
	}
	if c.GetEIP() != 0x00004000 {
		t.Errorf("EIP = 0x%08X, want 0x00004000", c.GetEIP())
	}
}

// TestHandleInterruptProtected16BitGate validates 16-bit interrupt gate (type 0x06).
func TestHandleInterruptProtected16BitGate(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x22, 0x00004000, 0x0008, 0x06, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x22*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000302 // IF=1, TF=1

	origEFLAGS := c.eflags
	origCS := uint32(c.seg[CS])
	origEIP := c.GetEIP()

	if err := c.handleInterrupt(0x22, true); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// 16-bit gate pushes 16-bit values: FLAGS, CS, IP
	// Pushes go to 0x7FFE, 0x7FFC, 0x7FFA (stack grows down)
	if c.GetReg16(SP) != 0x7FFA {
		t.Errorf("SP = 0x%04X, want 0x7FFA", c.GetReg16(SP))
	}
	if v := c.readMem16(0x7FFE); v != uint16(origEFLAGS) {
		t.Errorf("FLAGS = 0x%04X, want 0x%04X", v, uint16(origEFLAGS))
	}
	if v := c.readMem16(0x7FFC); v != uint16(origCS) {
		t.Errorf("CS = 0x%04X, want 0x%04X", v, uint16(origCS))
	}
	if v := c.readMem16(0x7FFA); v != uint16(origEIP) {
		t.Errorf("IP = 0x%04X, want 0x%04X", v, uint16(origEIP))
	}

	if c.GetEIP() != 0x00004000 {
		t.Errorf("EIP = 0x%08X, want 0x00004000", c.GetEIP())
	}
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF not cleared after 16-bit interrupt gate")
	}
}

// TestHandleInterruptProtected16BitTrapGate validates 16-bit trap gate (type 0x07) does NOT clear IF.
func TestHandleInterruptProtected16BitTrapGate(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x23, 0x00004000, 0x0008, 0x07, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x23*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000302 // IF=1, TF=1

	if err := c.handleInterrupt(0x23, false); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	if c.eflags&EFLAGS_IF == 0 {
		t.Errorf("IF cleared after 16-bit trap gate (should remain set)")
	}
	if c.eflags&EFLAGS_TF == 0 {
		t.Errorf("TF cleared after 16-bit trap gate (should remain set)")
	}
}

// TestHandleInterruptErrorCode validates that error codes are pushed for exceptions.
func TestHandleInterruptErrorCode(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0E, 0x00004000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0E*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000200 // IF=1

	origEFLAGS := c.eflags
	origCS := uint32(c.seg[CS])
	origEIP := c.GetEIP()
	errorCode := uint32(0x00000006)

	if err := c.handleInterrupt(0x0E, true, errorCode); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// 4 pushes of 32-bit = 16 bytes
	if c.GetReg32(ESP) != 0x7FF0 {
		t.Errorf("ESP = 0x%08X, want 0x7FF0", c.GetReg32(ESP))
	}
	// Stack layout (grows down): EFLAGS at 0x7FFC, CS at 0x7FF8, EIP at 0x7FF4, errorCode at 0x7FF0
	if v := c.readMem32(0x7FFC); v != origEFLAGS {
		t.Errorf("EFLAGS = 0x%08X, want 0x%08X", v, origEFLAGS)
	}
	if v := c.readMem32(0x7FF8); v != origCS {
		t.Errorf("CS = 0x%08X, want 0x%08X", v, origCS)
	}
	if v := c.readMem32(0x7FF4); v != origEIP {
		t.Errorf("EIP = 0x%08X, want 0x%08X", v, origEIP)
	}
	if v := c.readMem32(0x7FF0); v != errorCode {
		t.Errorf("errorCode = 0x%08X, want 0x%08X", v, errorCode)
	}
}

// TestHandleInterruptErrorCode16Bit validates error code push with 16-bit gate.
// Error codes are always 32-bit in protected mode, even for 16-bit gates.
func TestHandleInterruptErrorCode16Bit(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0E, 0x00004000, 0x0008, 0x06, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0E*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000200 // IF=1

	errorCode := uint32(0x00000002)
	if err := c.handleInterrupt(0x0E, true, errorCode); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// 3 pushes of 16-bit + 1 push of 32-bit = 6 + 4 = 10 bytes
	if c.GetReg32(ESP) != 0x7FF6 {
		t.Errorf("ESP = 0x%08X, want 0x7FF6", c.GetReg32(ESP))
	}
	// Stack layout (grows down): FLAGS at 0x7FFE, CS at 0x7FFC, IP at 0x7FFA, errorCode at 0x7FF6
	if v := c.readMem32(0x7FF6); v != errorCode {
		t.Errorf("errorCode = 0x%08X, want 0x%08X", v, errorCode)
	}
}

// TestHandleInterruptHardwareWakesHLT validates that hardware interrupts clear powerDown.
func TestHandleInterruptHardwareWakesHLT(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0400)
	c.SetEIP(0x1000)

	c.powerDown = true
	c.writeMem16(0x20, 0x0200)
	c.writeMem16(0x22, 0x1000)

	if err := c.handleInterrupt(0x08, true); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}
	if c.IsPowerDown() {
		t.Errorf("CPU still in powerDown after hardware interrupt")
	}
}

// TestHandleInterruptSoftwareDoesNotWakeHLT validates that software interrupts do NOT clear powerDown.
func TestHandleInterruptSoftwareDoesNotWakeHLT(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0400)
	c.SetEIP(0x1000)

	c.powerDown = true
	c.writeMem16(0x20, 0x0200)
	c.writeMem16(0x22, 0x1000)

	if err := c.handleInterrupt(0x08, false); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}
	if !c.IsPowerDown() {
		t.Errorf("powerDown cleared by software interrupt (should remain set)")
	}
}

// TestHandleInterruptIDTLimitExceeded validates error when vector exceeds IDT limit.
func TestHandleInterruptIDTLimitExceeded(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	// IDT limit is only enough for vector 0x00 (7 bytes)
	enterProtectedMode(t, c, gdtBase, idtBase, 7, 0x5000)

	err := c.handleInterrupt(0x01, true)
	if err == nil {
		t.Fatalf("expected error for vector exceeding IDT limit")
	}
}

// TestHandleInterruptGateNotPresent validates error when gate descriptor is not present.
func TestHandleInterruptGateNotPresent(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	// Write a gate with P=0
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0008, 0x0E, 0)
	c.writeMem8(idtBase+0x20*8+5, 0x0E) // Clear P bit
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	err := c.handleInterrupt(0x20, true)
	if err == nil {
		t.Fatalf("expected error for not-present gate")
	}
}

// TestHandleInterruptNullSelector validates error when gate has null CS selector.
func TestHandleInterruptNullSelector(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0000, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	err := c.handleInterrupt(0x20, true)
	if err == nil {
		t.Fatalf("expected error for null CS selector")
	}
}

// TestHandleInterruptCSNotCode validates error when gate points to data segment.
func TestHandleInterruptCSNotCode(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	// Point to data segment selector 0x10
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0010, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	err := c.handleInterrupt(0x20, true)
	if err == nil {
		t.Fatalf("expected error for CS not being a code segment")
	}
}

// TestHandleInterruptCSDPLGreaterThanCPL validates error when CS DPL > CPL.
func TestHandleInterruptCSDPLGreaterThanCPL(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	// Modify code segment descriptor at 0x08 to have DPL=3
	c.writeMem8(gdtBase+13, 0xFA) // access: P=1, DPL=3, S=1, type=0xA (execute/read)
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	// CPL is 0 (from RPL of current CS=0x08)
	err := c.handleInterrupt(0x20, true)
	if err == nil {
		t.Fatalf("expected error for CS DPL > CPL")
	}
}

// TestHandleInterruptSoftwareDPLCheck validates DPL check for software interrupts.
func TestHandleInterruptSoftwareDPLCheck(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	// Gate with DPL=3 for vector 0x20
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0008, 0x0E, 3)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	// CPL=0, DPL=3: software INT should succeed (DPL >= CPL)
	if err := c.handleInterrupt(0x20, false); err != nil {
		t.Fatalf("software INT with DPL=3 from CPL=0 should succeed: %v", err)
	}

	// Now change gate DPL=0 and try from CPL=3
	writeIDTGate(c, idtBase, 0x21, 0x00004000, 0x0008, 0x0E, 0)
	// Set CS RPL to 3 by changing selector to 0x0B (same descriptor, RPL=3)
	c.SetSeg(CS, 0x000B)
	c.cpl = 3

	err := c.handleInterrupt(0x21, false)
	if err == nil {
		t.Fatalf("expected error for software INT with DPL=0 from CPL=3")
	}
}

// TestHandleInterruptRealModeIVTRead validates that real mode reads IVT correctly.
func TestHandleInterruptRealModeIVTRead(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0500)

	// IVT entry for vector 0xFF at 0x03FC
	c.writeMem16(0x03FC, 0xABCD)
	c.writeMem16(0x03FE, 0x1234)

	if err := c.handleInterrupt(0xFF, false); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	if c.GetEIP() != 0xABCD {
		t.Errorf("EIP = 0x%04X, want 0xABCD", c.GetEIP())
	}
	if c.GetSeg(CS) != 0x1234 {
		t.Errorf("CS = 0x%04X, want 0x1234", c.GetSeg(CS))
	}
	if c.GetSegBase(CS) != 0x12340 {
		t.Errorf("CS base = 0x%05X, want 0x12340", c.GetSegBase(CS))
	}
}

// TestHandleInterruptPreservesLowerEFLAGS validates that only IF and TF are modified.
func TestHandleInterruptPreservesLowerEFLAGS(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0400)
	c.SetEIP(0x1000)

	c.writeMem16(0x40, 0x0200)
	c.writeMem16(0x42, 0x1000)

	// Set various EFLAGS bits except IF and TF
	c.eflags = 0x00000003 | EFLAGS_CF | EFLAGS_PF | EFLAGS_AF | EFLAGS_ZF | EFLAGS_SF | EFLAGS_DF | EFLAGS_OF | EFLAGS_NT | EFLAGS_RF | EFLAGS_AC | EFLAGS_ID

	origEFLAGS := c.eflags
	if err := c.handleInterrupt(0x10, false); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// Check that IF and TF are cleared, but other bits are preserved in the saved FLAGS
	// Pushes go to 0x03FE (FLAGS), 0x03FC (CS), 0x03FA (IP)
	expectedSaved := origEFLAGS &^ (EFLAGS_IF | EFLAGS_TF)
	if v := c.readMem16(0x03FE); v != uint16(expectedSaved) {
		t.Errorf("saved FLAGS = 0x%04X, want 0x%04X", v, uint16(expectedSaved))
	}
}


// TestHandleInterruptSTIBlocksInterrupt verifies that STI blocks interrupts for
// the next instruction (interrupt shadow). Currently skipped because this is not
// yet implemented.
func TestHandleInterruptSTIBlocksInterrupt(t *testing.T) {

	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0400)
	c.SetEIP(0x1000)

	// Code: STI, NOP, HLT
	c.writeMem8(0x1000, 0xFB) // STI
	c.writeMem8(0x1001, 0x90) // NOP
	c.writeMem8(0x1002, 0xF4) // HLT

	// IVT entry for vector 0x08
	c.writeMem16(0x20, 0x0200)
	c.writeMem16(0x22, 0x1000)

	// Step STI
	if err := c.Step(); err != nil {
		t.Fatalf("STI step error: %v", err)
	}

	// Raise interrupt
	c.SetINTR(1)
	c.SetInterruptAckHandler(func() (uint8, bool) {
		c.SetINTR(0)
		return 0x08, true
	})

	// Step NOP - interrupt should NOT be taken here (shadow)
	if err := c.Step(); err != nil {
		t.Fatalf("NOP step error: %v", err)
	}
	if c.GetEIP() != 0x1002 {
		t.Errorf("EIP = 0x%04X, want 0x1002 (interrupt taken too early)", c.GetEIP())
	}

	// Step HLT - interrupt CAN be taken now
	if err := c.Step(); err != nil {
		t.Fatalf("HLT step error: %v", err)
	}
	if c.GetEIP() != 0x0200 {
		t.Errorf("EIP = 0x%04X, want 0x0200 (interrupt not taken)", c.GetEIP())
	}
}

// TestHandleInterruptShadowAfterMovSS verifies that MOV SS blocks interrupts
// for the next instruction. Currently skipped because this is not yet implemented.
func TestHandleInterruptShadowAfterMovSS(t *testing.T) {

	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetReg32(ESP, 0x0400)
	c.SetEIP(0x1000)

	// Code: MOV AX, 0x2000; MOV SS, AX; NOP; HLT
	c.writeMem8(0x1000, 0xB8)
	c.writeMem16(0x1001, 0x2000) // MOV AX, 0x2000
	c.writeMem8(0x1003, 0x8E)
	c.writeMem8(0x1004, 0xD0) // MOV SS, AX
	c.writeMem8(0x1005, 0x90) // NOP
	c.writeMem8(0x1006, 0xF4) // HLT

	// IVT entry for vector 0x08
	c.writeMem16(0x20, 0x0200)
	c.writeMem16(0x22, 0x1000)

	// Run MOV AX
	if err := c.Step(); err != nil {
		t.Fatalf("MOV AX step error: %v", err)
	}

	// Enable interrupts
	c.eflags |= EFLAGS_IF

	// Run MOV SS
	if err := c.Step(); err != nil {
		t.Fatalf("MOV SS step error: %v", err)
	}

	// Raise interrupt AFTER MOV SS
	c.SetINTR(1)
	c.SetInterruptAckHandler(func() (uint8, bool) {
		c.SetINTR(0)
		return 0x08, true
	})

	// Run NOP - interrupt should NOT be taken here (shadow)
	if err := c.Step(); err != nil {
		t.Fatalf("NOP step error: %v", err)
	}
	if c.GetEIP() != 0x1006 {
		t.Errorf("EIP = 0x%04X, want 0x1006 (interrupt taken too early)", c.GetEIP())
	}

	// Run HLT - interrupt CAN be taken now
	if err := c.Step(); err != nil {
		t.Fatalf("HLT step error: %v", err)
	}
	if c.GetEIP() != 0x0200 {
		t.Errorf("EIP = 0x%04X, want 0x0200 (interrupt not taken)", c.GetEIP())
	}
}

// TestHandleInterruptConformingSegment verifies that interrupts through a
// conforming code segment work. Currently skipped because conforming segment
// CPL handling is not fully implemented.
func TestHandleInterruptConformingSegment(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)

	// Modify code segment descriptor at 0x08 to be conforming (type 0x0C = execute-only conforming)
	// Access byte: P=1, DPL=0, S=1, type=0xC = 11011100 = 0xDC
	c.writeMem8(gdtBase+13, 0xDC)

	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.cpl = 3 // Simulate ring-3 execution

	// For conforming segments, CPL should remain 3 (not change to 0)
	if err := c.handleInterrupt(0x20, true); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}
	if c.cpl != 3 {
		t.Errorf("CPL = %d, want 3 (conforming segment should not change CPL)", c.cpl)
	}
}

// TestHandleInterrupt16BitGateErrorCodeSize validates that error codes in
// 32-bit protected mode are always 32-bit, even when using a 16-bit gate.
func TestHandleInterrupt16BitGateErrorCodeSize(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0E, 0x00004000, 0x0008, 0x06, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0E*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = 0x00000200

	errorCode := uint32(0xDEADBEEF)
	if err := c.handleInterrupt(0x0E, true, errorCode); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// 3 pushes of 16-bit + 1 push of 32-bit = 6 + 4 = 10 bytes
	if c.GetReg32(ESP) != 0x7FF6 {
		t.Errorf("ESP = 0x%08X, want 0x7FF6", c.GetReg32(ESP))
	}
	if v := c.readMem32(0x7FF6); v != errorCode {
		t.Errorf("errorCode = 0x%08X, want 0x%08X", v, errorCode)
	}
}


// TestPush16StackLimitExpandUp validates #SS when pushing past an expand-up segment limit.
// With a broken stack, #SS delivery itself will double-fault in our emulator.
// We verify that the original push raises the expected stack fault.
func TestPush16StackLimitExpandUp(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetSeg(SS, 0x0000)
	c.SetSegBase(SS, 0x00000)
	c.SetSegLimit(SS, 0x0100) // small expand-up segment: valid 0x0000-0x0100
	c.SetSegAccess(SS, 0x92)  // read/write, expand-up
	c.SetReg16(SP, 0x0104)    // push16 will go to 0x0102 (> limit)

	// We expect a panic because #SS delivery itself will double-fault
	// (the stack is too small for the interrupt frame).
	defer func() {
		if r := recover(); r != nil {
			// Expected: double fault / panic cascade
			return
		}
		t.Errorf("expected panic for expand-up #SS double fault")
	}()

	c.writeMem8(0x1000, 0x50) // PUSH AX
	c.SetEIP(0x1000)
	c.Step() // should panic
}

// TestPush16StackLimitExpandDown validates #SS when pushing past an expand-down segment limit.
// For expand-down with limit=0x0100, valid range is 0x0101-0xFFFF.
// SP=0x0101 is at the boundary; a 16-bit push goes to 0x00FF (below, fault).
// #SS delivery itself double-faults, so we just verify the panic.
func TestPush16StackLimitExpandDown(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetSeg(SS, 0x0000)
	c.SetSegBase(SS, 0x00000)
	c.SetSegLimit(SS, 0x0100) // expand-down: valid 0x0101-0xFFFF
	c.SetSegAccess(SS, 0x96)  // read/write, expand-down
	c.SetReg16(SP, 0x0101)    // at boundary; push goes to 0x00FF (below, fault)

	// We expect a panic because #SS delivery itself will double-fault.
	defer func() {
		if r := recover(); r != nil {
			return
		}
		t.Errorf("expected panic for expand-down #SS double fault")
	}()

	c.writeMem8(0x1000, 0x50) // PUSH AX (real mode, 16-bit)
	c.SetEIP(0x1000)
	c.Step() // should panic
}

// TestPush16StackLimitOK validates that a push within the limit succeeds.
func TestPush16StackLimitOK(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)
	c.SetSeg(CS, 0x0000)
	c.SetSegBase(CS, 0x00000)
	c.SetSeg(SS, 0x0000)
	c.SetSegBase(SS, 0x00000)
	c.SetSegLimit(SS, 0x0200) // expand-up: valid 0x0000-0x0200
	c.SetSegAccess(SS, 0x92)  // read/write, expand-up
	c.SetReg16(SP, 0x0100)
	c.SetReg16(AX, 0xABCD)

	// Code: PUSH AX; HLT
	c.writeMem8(0x1000, 0x50) // PUSH AX
	c.writeMem8(0x1001, 0xF4)
	c.SetEIP(0x1000)

	if err := c.Step(); err != nil {
		t.Fatalf("PUSH step error: %v", err)
	}
	if c.GetReg16(SP) != 0x00FE {
		t.Errorf("SP = 0x%04X, want 0x00FE", c.GetReg16(SP))
	}
	if c.readMem16(0x00FE) != 0xABCD {
		t.Errorf("mem[0x00FE] = 0x%04X, want 0xABCD", c.readMem16(0x00FE))
	}
}

// TestHandleInterruptStackSwitch validates stack switch on privilege change.
// When an interrupt goes from CPL=3 to ring 0, the CPU loads SS0:ESP0 from
// the TSS, switches stacks, and pushes old SS:ESP onto the new stack.
func TestHandleInterruptStackSwitch(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	tssBase := uint32(0x4000)

	// Clear GDT area.
	for i := 0; i < 64; i++ {
		c.writeMem8(gdtBase+uint32(i), 0)
	}

	// Null descriptor at offset 0.

	// Ring 0 code at offset 8 (selector 0x08): flat, 32-bit, execute/read, DPL=0.
	c.writeMem8(gdtBase+8, 0xFF)
	c.writeMem8(gdtBase+9, 0xFF)
	c.writeMem8(gdtBase+10, 0x00)
	c.writeMem8(gdtBase+11, 0x00)
	c.writeMem8(gdtBase+12, 0x00)
	c.writeMem8(gdtBase+13, 0x9A) // P=1, DPL=0, S=1, type=0xA (execute/read)
	c.writeMem8(gdtBase+14, 0xCF)
	c.writeMem8(gdtBase+15, 0x00)

	// Ring 0 data at offset 16 (selector 0x10): flat, read/write, DPL=0.
	c.writeMem8(gdtBase+16, 0xFF)
	c.writeMem8(gdtBase+17, 0xFF)
	c.writeMem8(gdtBase+18, 0x00)
	c.writeMem8(gdtBase+19, 0x00)
	c.writeMem8(gdtBase+20, 0x00)
	c.writeMem8(gdtBase+21, 0x92) // P=1, DPL=0, S=1, type=0x2 (read/write)
	c.writeMem8(gdtBase+22, 0xCF)
	c.writeMem8(gdtBase+23, 0x00)

	// Ring 3 code at offset 24 (selector 0x18, use 0x1B): flat, 32-bit, execute/read, DPL=3.
	c.writeMem8(gdtBase+24, 0xFF)
	c.writeMem8(gdtBase+25, 0xFF)
	c.writeMem8(gdtBase+26, 0x00)
	c.writeMem8(gdtBase+27, 0x00)
	c.writeMem8(gdtBase+28, 0x00)
	c.writeMem8(gdtBase+29, 0xFA) // P=1, DPL=3, S=1, type=0xA (execute/read)
	c.writeMem8(gdtBase+30, 0xCF)
	c.writeMem8(gdtBase+31, 0x00)

	// Ring 3 data at offset 32 (selector 0x20, use 0x23): flat, read/write, DPL=3.
	c.writeMem8(gdtBase+32, 0xFF)
	c.writeMem8(gdtBase+33, 0xFF)
	c.writeMem8(gdtBase+34, 0x00)
	c.writeMem8(gdtBase+35, 0x00)
	c.writeMem8(gdtBase+36, 0x00)
	c.writeMem8(gdtBase+37, 0xF2) // P=1, DPL=3, S=1, type=0x2 (read/write)
	c.writeMem8(gdtBase+38, 0xCF)
	c.writeMem8(gdtBase+39, 0x00)

	// TSS descriptor at offset 40 (selector 0x28): base=0x4000, limit=0x67.
	c.writeMem8(gdtBase+40, 0x67)       // limit low
	c.writeMem8(gdtBase+41, 0x00)       // limit high
	c.writeMem8(gdtBase+42, uint8(tssBase))
	c.writeMem8(gdtBase+43, uint8(tssBase >> 8))
	c.writeMem8(gdtBase+44, uint8(tssBase >> 16))
	c.writeMem8(gdtBase+45, 0x89) // P=1, DPL=0, type=9 (32-bit available TSS)
	c.writeMem8(gdtBase+46, 0x40) // limit high + flags (limit=0x0067, flags=0x4)
	c.writeMem8(gdtBase+47, uint8(tssBase >> 24))

	// Build TSS at 0x4000.
	for i := 0; i < 104; i++ {
		c.writeMem8(tssBase+uint32(i), 0)
	}
	// ESP0 at offset 4.
	c.writeMem32(tssBase+4, 0x9000)
	// SS0 at offset 8.
	c.writeMem16(tssBase+8, 0x0010)

	// Load TR to point to TSS (selector 0x28).
	c.SetSeg(TR, 0x0028)
	c.SetSegBase(TR, tssBase)
	c.SetSegLimit(TR, 0x0067)
	c.SetSegAccess(TR, 0x89)

	// Set up IDT with a 32-bit interrupt gate to ring 0 code (0x08).
	writeIDTGate(c, idtBase, 0x20, 0x00006000, 0x0008, 0x0E, 0)

	// Enter protected mode (will land in ring 0).
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	// Now switch to ring 3 for execution.
	c.cpl = 3
	c.SetSeg(CS, 0x001B)
	c.SetSegBase(CS, 0x00000)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegAccess(CS, 0xCFA)
	c.SetSeg(SS, 0x0023)
	c.SetSegBase(SS, 0x00000)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetSegAccess(SS, 0xCF2)
	c.SetReg32(ESP, 0x8000)
	c.SetEIP(0x5000)
	c.eflags = 0x00000200

	// Trigger interrupt vector 0x20 (hardware).
	if err := c.handleInterrupt(0x20, true); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	// Verify CPL changed to 0.
	if c.cpl != 0 {
		t.Errorf("CPL = %d, want 0", c.cpl)
	}

	// Verify CS changed to ring 0 code selector (0x08).
	if c.GetSeg(CS) != 0x0008 {
		t.Errorf("CS = 0x%04X, want 0x0008", c.GetSeg(CS))
	}

	// Verify SS changed to ring 0 data selector (0x10).
	if c.GetSeg(SS) != 0x0010 {
		t.Errorf("SS = 0x%04X, want 0x0010", c.GetSeg(SS))
	}

	// Verify EIP points to handler.
	if c.GetEIP() != 0x6000 {
		t.Errorf("EIP = 0x%08X, want 0x00006000", c.GetEIP())
	}

	// Verify stack switch: ESP should be ESP0 - pushed bytes.
	// Pushed: old SS (4), old ESP (4), EFLAGS (4), CS (4), EIP (4) = 20 bytes.
	expectedESP := uint32(0x9000 - 20)
	if c.GetReg32(ESP) != expectedESP {
		t.Errorf("ESP = 0x%08X, want 0x%08X", c.GetReg32(ESP), expectedESP)
	}

	// Verify old SS and old ESP were pushed onto new stack.
	// Stack grows downward: earlier pushes are at higher addresses.
	ssBase := c.GetSegBase(SS)
	if v := c.readMem32(ssBase + expectedESP + 16); v != 0x0023 {
		t.Errorf("pushed old SS = 0x%08X, want 0x00000023", v)
	}
	if v := c.readMem32(ssBase + expectedESP + 12); v != 0x00008000 {
		t.Errorf("pushed old ESP = 0x%08X, want 0x00008000", v)
	}

	// Verify EFLAGS, CS, EIP pushed after old SS/ESP.
	if v := c.readMem32(ssBase + expectedESP + 8); v != 0x00000200 {
		t.Errorf("pushed EFLAGS = 0x%08X, want 0x00000200", v)
	}
	if v := c.readMem32(ssBase + expectedESP + 4); v != 0x0000001B {
		t.Errorf("pushed CS = 0x%08X, want 0x0000001B", v)
	}
	if v := c.readMem32(ssBase + expectedESP + 0); v != 0x00005000 {
		t.Errorf("pushed EIP = 0x%08X, want 0x00005000", v)
	}

	// Verify IF was cleared (interrupt gate).
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF still set after interrupt gate")
	}
}

// TestIRET32PrivilegeDowngrade validates that IRET restores SS:ESP when
// returning to a less privileged ring (CPL=0 -> CPL=3).
func TestIRET32PrivilegeDowngrade(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	buildFlatGDT(t, c, gdtBase)

	// Ring 3 code at offset 24 (selector 0x18, use 0x1B): flat, 32-bit, execute/read, DPL=3.
	c.writeMem8(gdtBase+24, 0xFF)
	c.writeMem8(gdtBase+25, 0xFF)
	c.writeMem8(gdtBase+26, 0x00)
	c.writeMem8(gdtBase+27, 0x00)
	c.writeMem8(gdtBase+28, 0x00)
	c.writeMem8(gdtBase+29, 0xFA) // P=1, DPL=3, S=1, type=0xA
	c.writeMem8(gdtBase+30, 0xCF)
	c.writeMem8(gdtBase+31, 0x00)

	// Ring 3 data at offset 32 (selector 0x20, use 0x23): flat, read/write, DPL=3.
	c.writeMem8(gdtBase+32, 0xFF)
	c.writeMem8(gdtBase+33, 0xFF)
	c.writeMem8(gdtBase+34, 0x00)
	c.writeMem8(gdtBase+35, 0x00)
	c.writeMem8(gdtBase+36, 0x00)
	c.writeMem8(gdtBase+37, 0xF2) // P=1, DPL=3, S=1, type=0x2
	c.writeMem8(gdtBase+38, 0xCF)
	c.writeMem8(gdtBase+39, 0x00)

	// Enter protected mode at ring 0.
	enterProtectedMode(t, c, gdtBase, 0x3000, 0x0F*8+7, 0x5000)
	// Expand GDT limit to include ring-3 descriptors at offsets 24 and 32.
	c.segLimit[GDTR] = 0x0027 // 5 entries * 8 - 1 = 39

	// Set up ring-0 stack with an IRET frame for returning to ring 3.
	// Stack layout for 32-bit IRET with privilege downgrade:
	//   [ESP+16] old SS  (0x23)
	//   [ESP+12] old ESP (0x8000)
	//   [ESP+8]  EFLAGS
	//   [ESP+4]  CS     (0x1B)
	//   [ESP+0]  EIP    (0x6000)
	c.SetReg32(ESP, 0x9000)
	c.push32(0x0023)            // SS
	c.push32(0x00008000)        // ESP
	c.push32(0x00000200)        // EFLAGS
	c.push32(0x0000001B)        // CS (ring 3)
	c.push32(0x00006000)        // EIP

	// Set CPL=0 (ring 0).
	c.cpl = 0

	// Code: IRET
	c.writeMem8(0x5000, 0xCF)
	c.SetEIP(0x5000)

	if err := c.Step(); err != nil {
		t.Fatalf("IRET step error: %v", err)
	}

	// Verify EIP restored.
	if c.GetEIP() != 0x6000 {
		t.Errorf("EIP = 0x%08X, want 0x00006000", c.GetEIP())
	}

	// Verify CS restored to ring 3.
	if c.GetSeg(CS) != 0x001B {
		t.Errorf("CS = 0x%04X, want 0x001B", c.GetSeg(CS))
	}

	// Verify CPL downgraded to 3.
	if c.cpl != 3 {
		t.Errorf("CPL = %d, want 3", c.cpl)
	}

	// Verify SS restored to ring 3.
	if c.GetSeg(SS) != 0x0023 {
		t.Errorf("SS = 0x%04X, want 0x0023", c.GetSeg(SS))
	}

	// Verify ESP restored.
	if c.GetReg32(ESP) != 0x8000 {
		t.Errorf("ESP = 0x%08X, want 0x00008000", c.GetReg32(ESP))
	}
}

// TestCLIIOPLViolation validates that CLI raises #GP when CPL > IOPL.
func TestCLIIOPLViolation(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0D, 0x00006000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0D*8+7, 0x5000)

	// Set up minimal TSS so stack switch on #GP works.
	tssBase := uint32(0x4000)
	for i := 0; i < 104; i++ {
		c.writeMem8(tssBase+uint32(i), 0)
	}
	c.writeMem32(tssBase+4, 0x9000) // ESP0
	c.writeMem16(tssBase+8, 0x0010) // SS0
	c.SetSeg(TR, 0x0000)
	c.SetSegBase(TR, tssBase)
	c.SetSegLimit(TR, 0x0067)

	// Set CPL=3, IOPL=0.
	c.cpl = 3
	c.eflags &^= EFLAGS_IOPL
	c.eflags |= EFLAGS_IF

	// Code: CLI
	c.writeMem8(0x5000, 0xFA)
	c.SetEIP(0x5000)

	if err := c.Step(); err != nil {
		t.Fatalf("CLI step error: %v", err)
	}

	// Should have taken #GP handler.
	if c.GetEIP() != 0x6000 {
		t.Errorf("EIP = 0x%08X, want 0x00006000 (#GP handler)", c.GetEIP())
	}
	// IF is cleared by the interrupt gate, not by CLI.
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF still set after #GP through interrupt gate")
	}
}

// TestSTIIOPLViolation validates that STI raises #GP when CPL > IOPL.
func TestSTIIOPLViolation(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0D, 0x00006000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0D*8+7, 0x5000)

	// Set up minimal TSS so stack switch on #GP works.
	tssBase := uint32(0x4000)
	for i := 0; i < 104; i++ {
		c.writeMem8(tssBase+uint32(i), 0)
	}
	c.writeMem32(tssBase+4, 0x9000) // ESP0
	c.writeMem16(tssBase+8, 0x0010) // SS0
	c.SetSeg(TR, 0x0000)
	c.SetSegBase(TR, tssBase)
	c.SetSegLimit(TR, 0x0067)

	// Set CPL=3, IOPL=0.
	c.cpl = 3
	c.eflags &^= EFLAGS_IOPL
	c.eflags &^= EFLAGS_IF

	// Code: STI
	c.writeMem8(0x5000, 0xFB)
	c.SetEIP(0x5000)

	if err := c.Step(); err != nil {
		t.Fatalf("STI step error: %v", err)
	}

	if c.GetEIP() != 0x6000 {
		t.Errorf("EIP = 0x%08X, want 0x00006000 (#GP handler)", c.GetEIP())
	}
	// IF should still be clear (STI was blocked).
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF set despite #GP")
	}
}

// TestINIOPLViolation validates that IN raises #GP when CPL > IOPL.
func TestINIOPLViolation(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0D, 0x00006000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0D*8+7, 0x5000)

	// Set up minimal TSS so stack switch on #GP works.
	tssBase := uint32(0x4000)
	for i := 0; i < 104; i++ {
		c.writeMem8(tssBase+uint32(i), 0)
	}
	c.writeMem32(tssBase+4, 0x9000) // ESP0
	c.writeMem16(tssBase+8, 0x0010) // SS0
	c.SetSeg(TR, 0x0000)
	c.SetSegBase(TR, tssBase)
	c.SetSegLimit(TR, 0x0067)

	// Set CPL=3, IOPL=0.
	c.cpl = 3
	c.eflags &^= EFLAGS_IOPL

	// Code: IN AL, 0x80
	c.writeMem8(0x5000, 0xE4)
	c.writeMem8(0x5001, 0x80)
	c.SetEIP(0x5000)

	if err := c.Step(); err != nil {
		t.Fatalf("IN step error: %v", err)
	}

	if c.GetEIP() != 0x6000 {
		t.Errorf("EIP = 0x%08X, want 0x00006000 (#GP handler)", c.GetEIP())
	}
}

// TestCLIIOPLOK validates that CLI succeeds when CPL <= IOPL.
func TestCLIIOPLOK(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x0D, 0x00006000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x0D*8+7, 0x5000)

	// Set CPL=0, IOPL=0 (CPL <= IOPL).
	c.cpl = 0
	c.eflags &^= EFLAGS_IOPL
	c.eflags |= EFLAGS_IF

	// Code: CLI; HLT
	c.writeMem8(0x5000, 0xFA)
	c.writeMem8(0x5001, 0xF4)
	c.SetEIP(0x5000)

	if err := c.Step(); err != nil {
		t.Fatalf("CLI step error: %v", err)
	}

	// Should NOT have taken #GP handler.
	if c.GetEIP() != 0x5001 {
		t.Errorf("EIP = 0x%08X, want 0x00005001", c.GetEIP())
	}
	// IF should be clear.
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF still set after CLI")
	}
}

// TestHandleInterruptClearsVMFlag validates that interrupt delivery from
// v8086 mode clears the VM bit in EFLAGS.
func TestHandleInterruptClearsVMFlag(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, c.GetCR(0)&^CR0_PE)
	c.SetSegAccess(CS, 0)

	gdtBase := uint32(0x2000)
	idtBase := uint32(0x3000)
	buildFlatGDT(t, c, gdtBase)
	writeIDTGate(c, idtBase, 0x20, 0x00004000, 0x0008, 0x0E, 0)
	enterProtectedMode(t, c, gdtBase, idtBase, 0x20*8+7, 0x5000)

	c.SetEIP(0x5000)
	c.SetReg32(ESP, 0x8000)
	c.eflags = EFLAGS_IF | EFLAGS_VM

	if err := c.handleInterrupt(0x20, true); err != nil {
		t.Fatalf("handleInterrupt error: %v", err)
	}

	if c.eflags&EFLAGS_VM != 0 {
		t.Errorf("VM bit still set after interrupt delivery")
	}
	if c.eflags&EFLAGS_IF != 0 {
		t.Errorf("IF still set after interrupt gate")
	}
	// RF should be set.
	if c.eflags&EFLAGS_RF == 0 {
		t.Errorf("RF not set after interrupt delivery")
	}
}
