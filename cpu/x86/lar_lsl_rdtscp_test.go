package x86

import "testing"

// installFlatCodeGDT writes a flat 32-bit code descriptor at GDT slot 1
// (selector 0x08), present, DPL=0, base=0, limit=0xFFFFF (with G=1 → 4 GB).
func installFlatCodeGDT(c *CPU) {
	gdt := uint32(0x4000)
	for i := uint32(0); i < 16; i++ {
		c.writeMem8(gdt+i, 0)
	}
	c.writeMem8(gdt+8, 0xFF)  // limit low
	c.writeMem8(gdt+9, 0xFF)  // limit mid
	c.writeMem8(gdt+10, 0x00) // base low
	c.writeMem8(gdt+11, 0x00)
	c.writeMem8(gdt+12, 0x00)
	c.writeMem8(gdt+13, 0x9A) // access: P=1 DPL=0 S=1 type=code/RX
	c.writeMem8(gdt+14, 0xCF) // flags G=1 D=1 + limit hi
	c.writeMem8(gdt+15, 0x00)
	c.SetSegBase(GDTR, gdt)
	c.SetSegLimit(GDTR, 15)
}

func TestLAR_ValidSelector(t *testing.T) {
	c := newTestCPU(t)
	installFlatCodeGDT(c)
	c.SetReg32(EBX, 0x0008) // selector 0x08
	// LAR EAX, BX = 0F 02 C3
	code := []byte{0x0F, 0x02, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF not set for valid selector")
	}
	// Access byte 0x9A → result has 0x9A in bits [15:8].
	if got := c.GetReg32(EAX); got != 0x9A00 {
		t.Errorf("LAR result = 0x%08X, want 0x00009A00", got)
	}
}

func TestLAR_NullSelector(t *testing.T) {
	c := newTestCPU(t)
	installFlatCodeGDT(c)
	c.SetReg32(EBX, 0)
	c.setZF(true) // start with ZF set so we observe the clear
	code := []byte{0x0F, 0x02, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.getZF() {
		t.Errorf("ZF still set for null selector")
	}
}

func TestLSL_ValidSelector(t *testing.T) {
	c := newTestCPU(t)
	installFlatCodeGDT(c)
	c.SetReg32(EBX, 0x0008)
	// LSL EAX, BX = 0F 03 C3
	code := []byte{0x0F, 0x03, 0xC3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if !c.getZF() {
		t.Errorf("ZF not set for valid selector")
	}
	// Flat segment with G=1, limit field 0xFFFFF → byte-granular limit 0xFFFFFFFF.
	if got := c.GetReg32(EAX); got != 0xFFFFFFFF {
		t.Errorf("LSL result = 0x%08X, want 0xFFFFFFFF", got)
	}
}

func TestRDTSCP(t *testing.T) {
	c := newTestCPU(t)
	c.cycles = 0xCAFE
	// RDTSCP = 0F 01 F9
	code := []byte{0x0F, 0x01, 0xF9, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	got := uint64(c.GetReg32(EAX)) | (uint64(c.GetReg32(EDX)) << 32)
	if got < 0xCAFE {
		t.Errorf("TSC = 0x%X, want >= 0xCAFE", got)
	}
	if c.GetReg32(ECX) != 0 {
		t.Errorf("ECX = 0x%X, want 0 (we don't support IA32_TSC_AUX)", c.GetReg32(ECX))
	}
}

func TestFXSAVE_NOPThroughDecoder(t *testing.T) {
	c := newTestCPU(t)
	// FXSAVE [EBX] = 0F AE /0 03 (mod=00 reg=000 rm=011)
	c.SetReg32(EBX, 0x2000)
	code := []byte{0x0F, 0xAE, 0x03, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// We only need the decoder to consume the instruction without erroring.
}

func TestFXRSTOR_NOPThroughDecoder(t *testing.T) {
	c := newTestCPU(t)
	// FXRSTOR [EBX] = 0F AE /1 0B (mod=00 reg=001 rm=011)
	c.SetReg32(EBX, 0x2000)
	code := []byte{0x0F, 0xAE, 0x0B, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
}
