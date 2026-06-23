package x86_64

import (
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// TestGrubLZMADecode exercises GRUB's i386-pc core.img self-decompression
// (lzma_decompress.S: Reed-Solomon recovery + LZMA decode of the compressed
// core to GRUB_MEMORY_MACHINE_DECOMPRESSION_ADDR = 0x100000) directly on the
// CPU — no SeaBIOS, no disk, no 3-minute boot. OpenWRT x86-64 boots via
// BIOS-GRUB and this decode mis-produces its output (only the first ~12 bytes
// are correct, the rest garbage/zero), so GRUB jumps into a broken core and
// dies at a stray HLT. Alpine x64 (isolinux) never exercises this path, which
// is why only OpenWRT exposes it.
//
// The harness starts at the stub's pm32 entry (the real-mode prologue uses BIOS
// int 13h to pull core.img off disk, which we instead preload), with flat
// segments, and skips the long Reed-Solomon pass (it doesn't alter the data
// when there are no errors) by NOPping its call, so the decode runs in seconds.
//
// Set TINYEMU_GRUB_IMG to an OpenWrt x86-64 ext4-combined.img to run it.
func TestGrubLZMADecode(t *testing.T) {
	imgPath := os.Getenv("TINYEMU_GRUB_IMG")
	if imgPath == "" {
		imgPath = "../../bin/openwrt-x64/openwrt-24.10.0-x86-64-generic-ext4-combined.img"
	}
	data, err := os.ReadFile(imgPath)
	if err != nil {
		t.Skipf("GRUB image not available (%v); set TINYEMU_GRUB_IMG", err)
	}

	// GRUB core.img lives in the post-MBR embedding gap at disk sector 1
	// (offset 0x200). boot.img loads diskboot.img to 0x8000 and diskboot loads
	// the rest (kernel.img: lzma stub + compressed core + RS redundancy) to
	// 0x8200. Mirror that load.
	const coreDiskOff = 0x200
	const coreLoadPA = 0x8000
	const coreLen = 0x42000
	if len(data) < coreDiskOff+coreLen {
		t.Fatalf("image too small: %d bytes", len(data))
	}

	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	ram, err := mm.RegisterRAM(0, 4<<20, 0) // 4 MiB: covers 0x8000 and the 0x100000 output
	if err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	copy(ram.PhysMem[coreLoadPA:], data[coreDiskOff:coreDiskOff+coreLen])

	// Skip the Reed-Solomon recovery pass for speed: at 0x825a the stub does
	// `call grub_reed_solomon_recover` (E8 rel32). RS makes no change to the
	// data when there are no errors, and the bug is downstream in the LZMA
	// decode, so NOP the call. Guard on the expected opcode so this fails loudly
	// if the layout shifts.
	const rsCallPA = 0x825a
	if ram.PhysMem[rsCallPA] != 0xE8 {
		t.Fatalf("expected E8 (call) at %#x, got %#x — stub layout changed",
			rsCallPA, ram.PhysMem[rsCallPA])
	}
	for i := 0; i < 5; i++ {
		ram.PhysMem[rsCallPA+i] = 0x90 // NOP
	}

	c := NewCPU(mm)
	// 32-bit protected mode, flat segments (the stub's real-mode prologue set
	// these up via a GDT we skip). CS gets D=1 for 32-bit operands.
	c.SetCR64(0, CR0_PE)
	for _, s := range []int{CS, DS, ES, SS, FS, GS} {
		c.SetSeg(s, 0)
		c.SetSegBase(s, 0)
		c.SetSegLimit(s, 0xFFFFFFFF)
	}
	c.SetSegAccess(CS, csDBit)
	c.SetReg64(RSP, 0x7FFF0)
	c.SetRIP(0x823b) // pm32 entry, just past the real-mode/protected-mode switch

	trace := os.Getenv("TINYEMU_LZMA_TRACE") == "1"
	rd := func(a uint64) uint32 {
		return uint32(ram.PhysMem[a]) | uint32(ram.PhysMem[a+1])<<8 |
			uint32(ram.PhysMem[a+2])<<16 | uint32(ram.PhysMem[a+3])<<24
	}
	const maxSteps = 200_000_000
	reached := false
	matchSeen := false
	for i := 0; i < maxSteps; i++ {
		rip := c.GetRIP()
		if rip >= 0x100000 && rip < 0x200000 {
			reached = true // jumped into the (decompressed) core
			break
		}
		if trace {
			if rip == 0x8ba9 { // match path entry
				matchSeen = true
			}
			if rip == 0x8aa6 { // stosb: one output byte
				pos := c.GetReg64(RDI) - 0x100000
				if pos < 48 {
					ebp := c.GetReg64(RBP)
					t.Logf("out[%2d]=%02x code=%08x range=%08x rep0=%#x match=%v",
						pos, c.GetReg64(RAX)&0xff, rd(ebp-0x10), rd(ebp-0xc), rd(ebp-0x18), matchSeen)
					matchSeen = false
				}
			}
		}
		if err := c.Step(); err != nil {
			t.Logf("Step error after %d insns at RIP=%#x: %v", i, c.GetRIP(), err)
			break
		}
	}

	out := ram.PhysMem[0x100000 : 0x100000+0x24a34]
	nz := 0
	for _, b := range out {
		if b != 0 {
			nz++
		}
	}
	t.Logf("reachedCore=%v finalRIP=%#x nonzero=%d/%d first32=% x",
		reached, c.GetRIP(), nz, len(out), out[:32])

	// A correct decode fills ~150 KiB of GRUB code. The bug leaves it almost
	// entirely zero.
	if nz < 4096 {
		t.Fatalf("GRUB LZMA decode produced only %d non-zero bytes of %d "+
			"(expected most of it) — decoder is broken", nz, len(out))
	}
}
