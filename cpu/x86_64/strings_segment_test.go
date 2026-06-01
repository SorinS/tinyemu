package x86_64

// Regression test for the long-standing bug where MOVS / STOS / LODS /
// SCAS / CMPS used the raw RSI/RDI register value as a linear address
// instead of adding the source/destination segment base. The bug was
// silent in long mode (every segment base is forced to zero) but broke
// every real-mode and pm32 string op a guest issued. Symptom in the
// wild: SeaBIOS's INT 15h E820 handler does `rep movsb` from DS:0x5660
// with DS=0xF000 to copy a 20-byte entry to the caller's buffer; the
// pre-fix code read from physical 0x5660 (uninitialised conventional
// RAM, all zeros) instead of 0xF5660 where SeaBIOS had placed the
// entries, and Pure64-style BIOS-MBR loaders that walk the returned
// e820 saw no usable RAM, underflowed their MiB accumulator, and ran
// a page-table-init loop millions of iterations long that overwrote
// their own code.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestStringMOVS_RealModeAppliesDSBase reproduces SeaBIOS's INT 15h
// E820 copy: DS=0xF000, ES=0, SI=0x5660, DI=0x6000, ECX=20, real mode.
// Put a distinctive 20-byte pattern at linear 0xF5660 and a different
// pattern at linear 0x5660; after the rep movsb, [0x6000] must
// reflect the 0xF5660 source. Before the fix it reflected the 0x5660
// source instead.
func TestStringMOVS_RealModeAppliesDSBase(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)

	// Real mode (PE = 0). Segments hold selectors; bases are seg<<4
	// in real mode, which is what `mov ds, X` produces in our cpu.
	c.SetSegAccess(CS, csDBit) // 16-bit address default in real
	c.SetSeg(DS, 0xF000)
	c.SetSegBase(DS, 0xF0000)
	c.SetSeg(ES, 0x0000)
	c.SetSegBase(ES, 0x0000)
	c.SetCR64(0, 0) // PE=0 → real mode

	// Distinctive bytes at the DS:SI source (linear 0xF5660).
	want := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // start = 0
		0x00, 0xFC, 0x09, 0x00, 0x00, 0x00, 0x00, 0x00, // size = 0x9FC00
		0x01, 0x00, 0x00, 0x00, // type = 1 (RAM)
	}
	for i, b := range want {
		_ = mm.Write8(0xF5660+uint64(i), b)
	}
	// Decoy bytes at the bare-offset source (linear 0x5660). If the
	// implementation ignores DS_base it'll copy these instead.
	for i := 0; i < 20; i++ {
		_ = mm.Write8(0x5660+uint64(i), 0xEE)
	}

	// Run: rep movsb. Bytes: F3 A4 = REP MOVSB. Use a fixed code area
	// at CS:0 = 0xF0000, with CS=0xF000.
	c.SetSeg(CS, 0xF000)
	c.SetSegBase(CS, 0xF0000)
	c.SetRIP(0)
	_ = mm.Write8(0xF0000, 0xF3) // REP
	_ = mm.Write8(0xF0001, 0xA4) // MOVSB
	_ = mm.Write8(0xF0002, 0xF4) // HLT (sentinel)
	c.SetReg64(RSI, 0x5660)
	c.SetReg64(RDI, 0x6000)
	c.SetReg64(RCX, 20)
	if err := c.Step(); err != nil {
		t.Fatalf("rep movsb step: %v", err)
	}
	// Verify destination = the DS:SI source, not the bare-offset one.
	got := make([]byte, 20)
	for i := range got {
		b, err := mm.Read8(0x6000 + uint64(i))
		if err != nil {
			t.Fatalf("read dst[%d]: %v", i, err)
		}
		got[i] = b
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %#x, want %#x (rep movsb didn't apply DS_base — see SeaBIOS INT 15h E820 regression)",
				i, got[i], want[i])
		}
	}
	// Sanity: RSI/RDI/RCX advanced; RSI by 20, RDI by 20, RCX to 0.
	if rsi := c.GetReg64(RSI); rsi != 0x5660+20 {
		t.Errorf("RSI = %#x, want %#x", rsi, 0x5660+20)
	}
	if rdi := c.GetReg64(RDI); rdi != 0x6000+20 {
		t.Errorf("RDI = %#x, want %#x", rdi, 0x6000+20)
	}
	if rcx := c.GetReg64(RCX); rcx != 0 {
		t.Errorf("RCX = %#x, want 0", rcx)
	}
}

// TestStringSTOS_RealModeAppliesESBase mirrors the MOVS test for the
// STOS path: writes always go through ES, never DS, even with a DS
// segment-override prefix. We verify by setting DS to one base and ES
// to a different base, asking STOS to write AL = 0xCC, and confirming
// the byte lands at ES_base+DI, not DS_base+DI.
func TestStringSTOS_RealModeAppliesESBase(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, 0)
	c.SetSegAccess(CS, csDBit)
	c.SetSeg(CS, 0xF000)
	c.SetSegBase(CS, 0xF0000)
	c.SetSeg(DS, 0x1000) // distractor: DS_base = 0x10000
	c.SetSegBase(DS, 0x10000)
	c.SetSeg(ES, 0x2000)
	c.SetSegBase(ES, 0x20000)
	c.SetRIP(0)
	_ = mm.Write8(0xF0000, 0xAA) // STOSB
	_ = mm.Write8(0xF0001, 0xF4) // HLT
	c.SetReg64(RAX, 0xCC)
	c.SetReg64(RDI, 0x100)
	if err := c.Step(); err != nil {
		t.Fatalf("stosb step: %v", err)
	}
	if v, _ := mm.Read8(0x20100); v != 0xCC {
		t.Errorf("STOSB wrote to wrong segment: ES:DI (linear 0x20100) = %#x, want 0xCC", v)
	}
	if v, _ := mm.Read8(0x10100); v == 0xCC {
		t.Errorf("STOSB incorrectly wrote to DS:DI (linear 0x10100) — must use ES, not DS")
	}
}
