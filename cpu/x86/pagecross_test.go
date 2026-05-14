package x86

import (
	"testing"
)

// TestPageCross_Write32 — unaligned 4-byte write crossing a page boundary
// must translate each byte against its own page mapping. Prior to the fix,
// writeMem32 translated the start address ONCE and wrote four consecutive
// bytes at phys[start..start+3], so the three bytes past the page boundary
// went to the WRONG physical page — clobbering whatever lived just past
// the source page's physical extent.
//
// Reproducer: map two virtual pages A and B to non-consecutive physical
// pages P1 and P2. Issue a 4-byte write at A+0xFFF (last byte of A, first
// 3 bytes of B). Expect P1+0xFFF and P2+0..2 to be modified.
func TestPageCross_Write32(t *testing.T) {
	c := newTestCPU(t)
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegBase(DS, 0)
	c.SetSegLimit(DS, 0xFFFFFFFF)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetReg32(ESP, 0x9000)

	// Build a simple 32-bit identity-then-divergent paging setup:
	//   linear 0x10000 → phys 0x10000  (page A)
	//   linear 0x11000 → phys 0x80000  (page B; far from A)
	pdAddr := uint32(0x40000)
	ptAddr := uint32(0x41000)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(pdAddr+i*4, 0)
		c.writePhys32(ptAddr+i*4, 0)
	}
	c.writePhys32(pdAddr+0, ptAddr|0x07) // PD[0] -> PT
	// Identity-map code page (CS:0x1000) and everywhere except the cross-test pages.
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(ptAddr+i*4, (i<<12)|0x07)
	}
	// Override two specific PTEs:
	//   linear 0x10000 (PT index 0x10) → phys 0x10000 (identity, already set)
	//   linear 0x11000 (PT index 0x11) → phys 0x80000 (DIVERGE)
	c.writePhys32(ptAddr+0x11*4, 0x80000|0x07)

	// Pre-fill phys 0x10000 and 0x80000 with sentinels.
	for i := uint32(0); i < 4096; i++ {
		c.writePhys8(0x10000+i, 0xAA)
		c.writePhys8(0x80000+i, 0xBB)
	}

	c.SetCR(3, pdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	// Code at CS:0x1000:
	//   MOV EAX, 0x10FFF         B8 FF 0F 01 00
	//   MOV EDX, 0xDEADBEEF      BA EF BE AD DE
	//   MOV [EAX], EDX           89 10                ← unaligned cross-page write
	//   HLT                       F4
	code := []byte{
		0xB8, 0xFF, 0x0F, 0x01, 0x00,
		0xBA, 0xEF, 0xBE, 0xAD, 0xDE,
		0x89, 0x10,
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}

	// Expected post-write phys layout:
	//   phys 0x10FFF = 0xEF (low byte of EDX) — written via translation of
	//     linear 0x10FFF → phys 0x10FFF.
	//   phys 0x80000 = 0xBE — written via translation of linear 0x11000 → phys 0x80000.
	//   phys 0x80001 = 0xAD
	//   phys 0x80002 = 0xDE
	//   phys 0x10000..0x10FFE unchanged (= 0xAA).
	//   phys 0x80003..0x80FFF unchanged (= 0xBB).
	if got := c.readPhys8(0x10FFF); got != 0xEF {
		t.Errorf("phys 0x10FFF: got %02X, want 0xEF (low byte of EDX)", got)
	}
	if got := c.readPhys8(0x80000); got != 0xBE {
		t.Errorf("phys 0x80000: got %02X, want 0xBE (second byte of EDX, on diverged page)", got)
	}
	if got := c.readPhys8(0x80001); got != 0xAD {
		t.Errorf("phys 0x80001: got %02X, want 0xAD", got)
	}
	if got := c.readPhys8(0x80002); got != 0xDE {
		t.Errorf("phys 0x80002: got %02X, want 0xDE", got)
	}
	// Critical check: phys at 0x11000..0x11002 should NOT be modified
	// (these are the bytes that the buggy code would have written to).
	// They started as zero (we only initialized 0x10000..0x10FFF).
	if got := c.readPhys8(0x11000); got != 0x00 {
		t.Errorf("phys 0x11000 (would be wrong target): got %02X, want unchanged 0x00", got)
	}
	if got := c.readPhys8(0x11001); got != 0x00 {
		t.Errorf("phys 0x11001: got %02X, want unchanged 0x00", got)
	}
}

// TestPageCross_Write16 — same shape for 2-byte writes at addr+0xFFF.
func TestPageCross_Write16(t *testing.T) {
	c := newTestCPU(t)
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegBase(DS, 0)
	c.SetSegLimit(DS, 0xFFFFFFFF)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetReg32(ESP, 0x9000)

	pdAddr := uint32(0x40000)
	ptAddr := uint32(0x41000)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(pdAddr+i*4, 0)
	}
	c.writePhys32(pdAddr+0, ptAddr|0x07)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(ptAddr+i*4, (i<<12)|0x07)
	}
	c.writePhys32(ptAddr+0x11*4, 0x80000|0x07)

	for i := uint32(0); i < 4096; i++ {
		c.writePhys8(0x10000+i, 0xAA)
		c.writePhys8(0x80000+i, 0xBB)
	}
	c.SetCR(3, pdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	// 16-bit unaligned cross-page write:
	//   MOV EAX, 0x10FFF
	//   MOV DX, 0xBEEF        66 BA EF BE
	//   MOV WORD PTR [EAX], DX  66 89 10
	//   HLT
	code := []byte{
		0xB8, 0xFF, 0x0F, 0x01, 0x00,
		0x66, 0xBA, 0xEF, 0xBE,
		0x66, 0x89, 0x10,
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.readPhys8(0x10FFF); got != 0xEF {
		t.Errorf("phys 0x10FFF: got %02X, want 0xEF", got)
	}
	if got := c.readPhys8(0x80000); got != 0xBE {
		t.Errorf("phys 0x80000: got %02X, want 0xBE", got)
	}
	if got := c.readPhys8(0x11000); got != 0x00 {
		t.Errorf("phys 0x11000 (would be wrong target): got %02X, want unchanged 0x00", got)
	}
}

// TestPageCross_Read32 — symmetric test for reads.
func TestPageCross_Read32(t *testing.T) {
	c := newTestCPU(t)
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegBase(DS, 0)
	c.SetSegLimit(DS, 0xFFFFFFFF)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetReg32(ESP, 0x9000)

	pdAddr := uint32(0x40000)
	ptAddr := uint32(0x41000)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(pdAddr+i*4, 0)
	}
	c.writePhys32(pdAddr+0, ptAddr|0x07)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(ptAddr+i*4, (i<<12)|0x07)
	}
	c.writePhys32(ptAddr+0x11*4, 0x80000|0x07)

	// Set up: phys 0x10FFF = 0x11, phys 0x80000 = 0x22, 0x80001 = 0x33, 0x80002 = 0x44.
	// Linear 0x10FFF.. spans pages: byte 0 = phys 0x10FFF, byte 1..3 = phys 0x80000..2.
	c.writePhys8(0x10FFF, 0x11)
	c.writePhys8(0x80000, 0x22)
	c.writePhys8(0x80001, 0x33)
	c.writePhys8(0x80002, 0x44)
	// Wrong target: phys 0x11000..2 (where the buggy code would read from).
	c.writePhys8(0x11000, 0x99)
	c.writePhys8(0x11001, 0x99)
	c.writePhys8(0x11002, 0x99)

	c.SetCR(3, pdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	code := []byte{
		0xB8, 0xFF, 0x0F, 0x01, 0x00, // MOV EAX, 0x10FFF
		0x8B, 0x10,                   // MOV EDX, [EAX]
		0xF4,                         // HLT
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	want := uint32(0x44332211)
	if got := c.GetReg32(EDX); got != want {
		t.Errorf("EDX: got %08X, want %08X (cross-page read assembled from b0=0x11, b1=0x22, b2=0x33, b3=0x44)", got, want)
	}
}
