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

// TestPageCross_Fetch32 — instruction-fetch path must also handle page
// crossings. An instruction with a 4-byte immediate/displacement whose
// last byte lands on the next page would otherwise decode wrong: e.g. a
// `CALL rel32` at offset 0xFFB of a page has opcode + 4-byte offset
// spanning offsets 0xFFB..0xFFE on page A and 0xFFF on page B (the byte
// at the next page boundary). Without the cross-page-aware fetch, the
// high byte of rel32 is read from the wrong physical page → wrong
// branch target. This is exactly the busybox-grep "segfault at
// 0x78XXXa29" crash class.
//
// Reproducer: place a CALL rel32 instruction at linear 0xFFB of a
// virtual page mapped to phys X, with the next virtual page mapped to
// phys Y. The 4-byte rel32 in code: bytes at X+0xFFC..0xFFE on page X,
// byte at Y+0x000 on page Y. Verify the call goes to the right target.
func TestPageCross_Fetch32(t *testing.T) {
	c := newTestCPU(t)
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegBase(DS, 0)
	c.SetSegLimit(DS, 0xFFFFFFFF)
	c.SetSegBase(SS, 0)
	c.SetSegLimit(SS, 0xFFFFFFFF)
	c.SetReg32(ESP, 0x9000)

	// Paging: identity for almost everything, except linear page 0x20000
	// goes to phys 0x90000 (DIVERGED).
	pdAddr := uint32(0x40000)
	ptAddr := uint32(0x41000)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(pdAddr+i*4, 0)
	}
	c.writePhys32(pdAddr+0, ptAddr|0x07)
	for i := uint32(0); i < 1024; i++ {
		c.writePhys32(ptAddr+i*4, (i<<12)|0x07)
	}
	// linear 0x20000 → phys 0x90000 (diverged)
	c.writePhys32(ptAddr+0x20*4, 0x90000|0x07)

	// Place a CALL rel32 at linear 0x1FFFB (last 5 bytes of page 0x1F000):
	//   opcode E8, then 4-byte rel32 = +0x100 (sign-extended).
	// EIP after this CALL = 0x1FFFB + 5 = 0x20000 (= start of next page).
	// Target = 0x20000 + 0x100 = 0x20100.
	// At target 0x20100 (in page 0x20000, which maps to phys 0x90000):
	//   MOV EAX, 0xCAFEF00D   B8 0D F0 FE CA
	//   HLT                    F4
	// We place those instructions at the correct phys (0x90000+0x100).
	//
	// Code on page 0x1F (identity-mapped to phys 0x1F000):
	//   linear 0x1FFFB:  E8 (CALL opcode)
	//   linear 0x1FFFC:  00 (rel32 byte 0)
	//   linear 0x1FFFD:  01 (rel32 byte 1)
	//   linear 0x1FFFE:  00 (rel32 byte 2)
	//   linear 0x1FFFF:  00 (rel32 byte 3, last byte of page A)
	// linear 0x20000..  unused (target is at 0x20100)
	c.writePhys8(0x1FFFB, 0xE8)
	c.writePhys8(0x1FFFC, 0x00)
	c.writePhys8(0x1FFFD, 0x01)
	c.writePhys8(0x1FFFE, 0x00)
	c.writePhys8(0x1FFFF, 0x00)
	// Target at phys 0x90100 (= linear 0x20100 via the diverged mapping):
	c.writePhys8(0x90100, 0xB8)
	c.writePhys8(0x90101, 0x0D)
	c.writePhys8(0x90102, 0xF0)
	c.writePhys8(0x90103, 0xFE)
	c.writePhys8(0x90104, 0xCA)
	c.writePhys8(0x90105, 0xF4) // HLT
	// Also populate phys 0x20100 (the WRONG target if cross-page broken)
	// with a sentinel — a HLT but moving 0xBADC0FFE into EAX. If the bug
	// re-emerges, the test will hit this and we'll see 0xBADC0FFE.
	c.writePhys8(0x20100, 0xB8)
	c.writePhys8(0x20101, 0xFE)
	c.writePhys8(0x20102, 0x0F)
	c.writePhys8(0x20103, 0xDC)
	c.writePhys8(0x20104, 0xBA)
	c.writePhys8(0x20105, 0xF4)

	c.SetCR(3, pdAddr)
	c.SetCR(0, c.GetCR(0)|CR0_PG)

	c.SetEIP(0x1FFFB)
	for i := 0; i < 50; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if !c.IsPowerDown() {
		t.Fatalf("did not halt")
	}
	if got := c.GetReg32(EAX); got != 0xCAFEF00D {
		t.Errorf("EAX = %08X, want CAFEF00D (= correct CALL target). Got BADC0FFE means cross-page fetch read wrong rel32 bytes.", got)
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
