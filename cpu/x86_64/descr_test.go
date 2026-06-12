package x86_64

// M5b unit tests — descriptor-table loads (LGDT/LIDT), SGDT/SIDT
// round-trip, INVLPG no-op, SWAPGS.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestLIDT: load IDT base+limit from a 10-byte pseudo-descriptor.
func TestLIDT(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	// Plant a pseudo-descriptor at 0x2000:
	//   word  limit = 0x0FFF
	//   qword base  = 0xFFFFFFFF80000000
	_ = mm.Write16(0x2000, 0x0FFF)
	_ = mm.Write64(0x2002, 0xFFFFFFFF80000000)

	// Program at 0x1000:
	//   48 8D 04 25 00 20 00 00   lea rax, [0x2000]   (absolute via SIB no-base)
	//   0F 01 18                  lidt [rax]
	//   F4                        hlt
	prog := []byte{
		0x48, 0x8D, 0x04, 0x25, 0x00, 0x20, 0x00, 0x00,
		0x0F, 0x01, 0x18,
		0xF4,
	}
	const base uint64 = 0x1000
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	for i := 0; i < 3; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if c.segLimit[IDTR] != 0x0FFF {
		t.Errorf("IDTR limit = %#x, want 0xFFF", c.segLimit[IDTR])
	}
	if c.segBase[IDTR] != 0xFFFFFFFF80000000 {
		t.Errorf("IDTR base = %#x, want 0xFFFFFFFF80000000", c.segBase[IDTR])
	}
}

// TestLGDT_SGDT_RoundTrip: LGDT, then SGDT to a different location,
// then check that the read-back matches.
func TestLGDT_SGDT_RoundTrip(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	_ = mm.Write16(0x2000, 0x1FFF)
	_ = mm.Write64(0x2002, 0x0000000080000000)

	// LEA RAX,[0x2000]  ; LGDT [RAX]  ; LEA RBX,[0x3000]  ; SGDT [RBX]  ; HLT
	prog := []byte{
		0x48, 0x8D, 0x04, 0x25, 0x00, 0x20, 0x00, 0x00, // lea rax,[0x2000]
		0x0F, 0x01, 0x10, // lgdt [rax]
		0x48, 0x8D, 0x1C, 0x25, 0x00, 0x30, 0x00, 0x00, // lea rbx,[0x3000]
		0x0F, 0x01, 0x03, // sgdt [rbx]
		0xF4,
	}
	const base uint64 = 0x1000
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	for i := 0; i < 5; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	gotLimit, _ := mm.Read16(0x3000)
	gotBase, _ := mm.Read64(0x3002)
	if gotLimit != 0x1FFF {
		t.Errorf("SGDT limit = %#x, want 0x1FFF", gotLimit)
	}
	if gotBase != 0x0000000080000000 {
		t.Errorf("SGDT base = %#x", gotBase)
	}
}

// TestSWAPGS: GS.base and KernelGSBase atomically swap on 0x0F 0x01 0xF8.
func TestSWAPGS(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	c.msrGSBase = 0x1111_1111_1111_1111
	c.msrKernelGSBase = 0x2222_2222_2222_2222

	// 0F 01 F8 — SWAPGS
	prog := []byte{0x0F, 0x01, 0xF8, 0xF4}
	const base uint64 = 0x1000
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if c.msrGSBase != 0x2222_2222_2222_2222 {
		t.Errorf("GSBase = %#x, want 0x22..22", c.msrGSBase)
	}
	if c.msrKernelGSBase != 0x1111_1111_1111_1111 {
		t.Errorf("KernelGSBase = %#x, want 0x11..11", c.msrKernelGSBase)
	}
	if c.segBase[GS] != 0x2222_2222_2222_2222 {
		t.Errorf("segBase[GS] not synced after SWAPGS")
	}
}

// TestINVLPG: opcode is decoded without error (no-op for now).
func TestINVLPG(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	// 48 8D 04 25 00 20 00 00   lea rax,[0x2000]
	// 0F 01 38                  invlpg [rax]
	// F4                        hlt
	prog := []byte{
		0x48, 0x8D, 0x04, 0x25, 0x00, 0x20, 0x00, 0x00,
		0x0F, 0x01, 0x38,
		0xF4,
	}
	const base uint64 = 0x1000
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	for i := 0; i < 3; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
}

// TestLTR_LLDT: verify the selector lands in seg[TR]/seg[LDTR]. LTR walks
// the GDT, so a valid (Present) 64-bit TSS descriptor is planted first.
func TestLTR_LLDT(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()

	// Plant a valid available-64-bit-TSS descriptor at GDT[0x28] (GDTR
	// base defaults to 0). Access byte 0x89 = P=1, DPL=0, S=0, type=9;
	// base=0, limit=0x67. The high 8 bytes (base[63:32]) are zero.
	_ = mm.Write64(0x28, 0x0000890000000067)
	_ = mm.Write64(0x30, 0)

	// mov ax, 0x28 ; ltr ax  ; mov ax, 0x50 ; lldt ax ; hlt
	prog := []byte{
		0x66, 0xB8, 0x28, 0x00, // mov ax, 0x28
		0x0F, 0x00, 0xD8, // ltr ax (reg=011, rm=000)
		0x66, 0xB8, 0x50, 0x00, // mov ax, 0x50
		0x0F, 0x00, 0xD0, // lldt ax (reg=010, rm=000)
		0xF4,
	}
	const base uint64 = 0x1000
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	for i := 0; i < 4; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if c.seg[TR] != 0x28 {
		t.Errorf("TR = %#x, want 0x28", c.seg[TR])
	}
	if c.seg[LDTR] != 0x50 {
		t.Errorf("LDTR = %#x, want 0x50", c.seg[LDTR])
	}
}
