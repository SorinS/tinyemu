package x86_64

import (
	"os"
	"testing"

	"github.com/sorins/tinyemu-go/mem"
)

// BenchmarkGrubDecompress runs GRUB's full i386-pc core.img self-decompression
// — Reed-Solomon recovery AND LZMA decode (RS is NOT NOP'd here, unlike
// TestGrubLZMADecode) — directly on the CPU. This is the compute-bound phase a
// real OpenWrt x86-64 BIOS boot spends its first ~minute in, so it's the
// reference workload for interpreter throughput. Reports instructions retired
// (b.ReportMetric MIPS) so optimization work has a fast, I/O-free number.
//
//	go test ./cpu/x86_64 -run x -bench BenchmarkGrubDecompress -benchtime=1x \
//	  -cpuprofile=/tmp/rs.prof
func BenchmarkGrubDecompress(b *testing.B) {
	imgPath := os.Getenv("TINYEMU_GRUB_IMG")
	if imgPath == "" {
		imgPath = "../../bin/openwrt-x64/openwrt-24.10.0-x86-64-generic-ext4-combined.img"
	}
	data, err := os.ReadFile(imgPath)
	if err != nil {
		b.Skipf("GRUB image not available (%v); set TINYEMU_GRUB_IMG", err)
	}
	const coreDiskOff = 0x200
	const coreLoadPA = 0x8000
	const coreLen = 0x42000

	for i := 0; i < b.N; i++ {
		mm := mem.NewPhysMemoryMap()
		ram, err := mm.RegisterRAM(0, 4<<20, 0)
		if err != nil {
			b.Fatalf("RegisterRAM: %v", err)
		}
		copy(ram.PhysMem[coreLoadPA:], data[coreDiskOff:coreDiskOff+coreLen])

		c := NewCPU(mm)
		c.SetCR64(0, CR0_PE)
		for _, s := range []int{CS, DS, ES, SS, FS, GS} {
			c.SetSeg(s, 0)
			c.SetSegBase(s, 0)
			c.SetSegLimit(s, 0xFFFFFFFF)
		}
		c.SetSegAccess(CS, csDBit)
		c.SetReg64(RSP, 0x7FFF0)
		c.SetRIP(0x823b)

		const maxSteps = 2_000_000_000
		steps := 0
		for ; steps < maxSteps; steps++ {
			if rip := c.GetRIP(); rip >= 0x100000 && rip < 0x200000 {
				break
			}
			if err := c.stepCore(); err != nil {
				b.Fatalf("stepCore at RIP=%#x: %v", c.GetRIP(), err)
			}
		}
		if c.GetRIP() < 0x100000 || c.GetRIP() >= 0x200000 {
			b.Fatalf("did not reach decompressed core (RIP=%#x after %d steps)", c.GetRIP(), steps)
		}
		b.ReportMetric(float64(steps), "insns")
		mm.Close()
	}
}
