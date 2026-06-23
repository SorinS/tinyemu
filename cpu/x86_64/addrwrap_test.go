package x86_64

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestPM32EffectiveAddressWraps: in 32-bit address mode a memory operand's
// effective address is computed modulo 2^32. GRUB's LZMA match copy uses
// `mov al,[edi+edx]` with edx = -rep0 (a small distance NEGed, e.g. 0xfffffffa),
// so edi+edx must wrap to edi-rep0 instead of landing ~4 GiB up. Missing this
// wrap made the LZMA decode read zeros, so BIOS-GRUB images (OpenWrt x86-64)
// never decompressed their core and failed to boot.
func TestPM32EffectiveAddressWraps(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE) // 32-bit protected mode
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegAccess(CS, csDBit) // D=1 -> 32-bit operand/address size

	const codeAddr = 0x1000
	// mov al, [edi+edx*1]  =  8a 04 17  (ModR/M 04 = SIB, SIB 17 = base EDI, index EDX, scale 1)
	for i, b := range []byte{0x8a, 0x04, 0x17} {
		_ = mm.Write8(codeAddr+uint64(i), byte(b))
	}
	// EDI + EDX = 0x2006 + 0xfffffffa = 0x1_0000_2000 -> must wrap to 0x2000.
	_ = mm.Write8(0x2000, 0x42)
	_ = mm.Write8(0x100002000&0xFFFFFFFF, 0x42) // same address, explicit

	c.SetRIP(codeAddr)
	c.SetReg64(RDI, 0x2006)
	c.SetReg64(RDX, 0xFFFFFFFA) // -6
	c.SetReg64(RAX, 0xAA)       // sentinel

	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if got := c.GetReg64(RAX) & 0xFF; got != 0x42 {
		t.Fatalf("AL = %#x, want 0x42 — [edi+edx] did not wrap to 32 bits "+
			"(read a >4 GiB address instead of edi-6)", got)
	}
}
