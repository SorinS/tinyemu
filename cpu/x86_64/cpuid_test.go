package x86_64

// CPUID regression — TinyCorePure64 boot hit "0F 0xa2" early in the
// kernel's feature probe. Pins the values the kernel checks: vendor
// string at leaf 0, SSE2 + FPU + MSR at leaf 1, and the LM bit at
// leaf 0x80000001 that gates 64-bit operation.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

func runCPUID(t *testing.T, eax uint32) (a, b, cx, d uint32) {
	t.Helper()
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
	c.SetReg32(EAX, eax)
	c.SetReg32(ECX, 0)
	// 0F A2  cpuid
	const code uint64 = 0x1000
	_ = mm.Write8(code, 0x0F)
	_ = mm.Write8(code+1, 0xA2)
	_ = mm.Write8(code+2, 0xF4)
	c.SetRIP(code)
	if err := c.Step(); err != nil {
		t.Fatalf("step CPUID: %v", err)
	}
	return c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX)
}

func TestCPUID_Leaf0_VendorString(t *testing.T) {
	a, b, cx, d := runCPUID(t, 0)
	if a < 1 {
		t.Errorf("leaf 0 max basic = %d, want >=1", a)
	}
	// "GenuineIntel" packed as EBX=Genu, EDX=ineI, ECX=ntel
	if b != 0x756E6547 || d != 0x49656E69 || cx != 0x6C65746E {
		t.Errorf("vendor = EBX %#x EDX %#x ECX %#x — want GenuineIntel", b, d, cx)
	}
}

func TestCPUID_Leaf1_RequiredFeatures(t *testing.T) {
	_, _, _, d := runCPUID(t, 1)
	must := map[string]uint32{
		"FPU":  1 << 0,
		"TSC":  1 << 4,
		"MSR":  1 << 5,
		"PAE":  1 << 6,
		"CX8":  1 << 8,
		"CMOV": 1 << 15,
		"FXSR": 1 << 24,
		"SSE":  1 << 25,
		"SSE2": 1 << 26,
	}
	for name, mask := range must {
		if d&mask == 0 {
			t.Errorf("CPUID.1H EDX missing %s (bit %d)", name, indexOf(mask))
		}
	}
}

func TestCPUID_ExtLeaf1_LongModeBit(t *testing.T) {
	_, _, _, d := runCPUID(t, 0x80000001)
	if d&(1<<29) == 0 {
		t.Errorf("CPUID.80000001H EDX.LM (bit 29) clear; Linux would refuse 64-bit boot")
	}
	if d&(1<<11) == 0 {
		t.Errorf("SYSCALL bit clear in CPUID.80000001H EDX")
	}
	if d&(1<<20) == 0 {
		t.Errorf("NX bit clear in CPUID.80000001H EDX")
	}
}

func TestCPUID_BrandString(t *testing.T) {
	// Re-assemble bytes from the three brand-string leaves.
	var buf [48]byte
	for leaf := uint32(0); leaf < 3; leaf++ {
		a, b, cx, d := runCPUID(t, 0x80000002+leaf)
		off := leaf * 16
		put := func(o int, v uint32) {
			buf[off+uint32(o)+0] = uint8(v)
			buf[off+uint32(o)+1] = uint8(v >> 8)
			buf[off+uint32(o)+2] = uint8(v >> 16)
			buf[off+uint32(o)+3] = uint8(v >> 24)
		}
		put(0, a)
		put(4, b)
		put(8, cx)
		put(12, d)
	}
	if string(buf[:11]) != "tinyemu-go " {
		t.Errorf("brand string prefix = %q, want 'tinyemu-go '", buf[:11])
	}
}

// indexOf returns the bit position of a single-bit mask; -1 otherwise.
func indexOf(mask uint32) int {
	for i := 0; i < 32; i++ {
		if mask == 1<<i {
			return i
		}
	}
	return -1
}
