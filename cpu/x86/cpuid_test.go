package x86

import (
	"strings"
	"testing"
)

// runCPUID executes CPUID with the given EAX leaf and returns the four output
// registers.
func runCPUID(t *testing.T, eax uint32) (a, b, ce, d uint32) {
	t.Helper()
	c := newTestCPU(t)
	c.SetReg32(EAX, eax)
	code := []byte{0x0F, 0xA2, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	return c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX)
}

// TestCPUID_Leaf0 verifies the vendor string and max standard leaf.
func TestCPUID_Leaf0(t *testing.T) {
	a, b, ce, d := runCPUID(t, 0)
	if a < 0x00000004 {
		t.Errorf("max standard leaf = 0x%X, want >= 4", a)
	}
	if b != 0x756E6547 || d != 0x49656E69 || ce != 0x6C65746E {
		t.Errorf("vendor = %08X %08X %08X, want GenuineIntel", b, d, ce)
	}
}

// TestCPUID_Leaf1FeatureBits verifies the EDX feature flags advertised. We
// require TSC, MSR, CX8, CMOV, PSE, FPU; we require SEP and APIC NOT
// advertised so Linux takes the simple paths we support. FPU is advertised
// because Yocto's qemux86 kernel is built without CONFIG_MATH_EMULATION and
// will panic during boot otherwise — our x87 ops are NOP stubs which is
// enough for the kernel's FNINIT-based probe path.
func TestCPUID_Leaf1FeatureBits(t *testing.T) {
	_, _, _, edx := runCPUID(t, 1)
	required := uint32((1 << 0) | (1 << 3) | (1 << 4) | (1 << 5) | (1 << 8) | (1 << 15))
	if edx&required != required {
		t.Errorf("EDX = 0x%08X missing required feature bits 0x%08X", edx, required)
	}
	if edx&(1<<9) != 0 {
		t.Errorf("APIC bit set, expected cleared (we have no APIC)")
	}
	if edx&(1<<11) != 0 {
		t.Errorf("SEP bit set, expected cleared (we have no SYSENTER)")
	}
}

// TestCPUID_ExtendedLeaf verifies that the brand-string leaves report
// something sensible.
func TestCPUID_ExtendedLeaf(t *testing.T) {
	a, _, _, _ := runCPUID(t, 0x80000000)
	if a < 0x80000004 {
		t.Errorf("max extended leaf = 0x%X, want >= 0x80000004", a)
	}
	a, _, _, _ = runCPUID(t, 0x80000002)
	if a == 0 {
		t.Errorf("brand string leaf 0x80000002 EAX is zero")
	}
}

// TestCPUID_BrandStringContent decodes leaves 0x80000002..0x80000004 and
// checks the resulting 48-byte string starts with "tinyemu-go".
func TestCPUID_BrandStringContent(t *testing.T) {
	var buf [48]byte
	idx := 0
	for leaf := uint32(0x80000002); leaf <= 0x80000004; leaf++ {
		a, b, ce, d := runCPUID(t, leaf)
		for _, v := range []uint32{a, b, ce, d} {
			buf[idx+0] = byte(v)
			buf[idx+1] = byte(v >> 8)
			buf[idx+2] = byte(v >> 16)
			buf[idx+3] = byte(v >> 24)
			idx += 4
		}
	}
	got := string(buf[:])
	if !strings.HasPrefix(got, "tinyemu-go") {
		t.Errorf("brand string = %q, want prefix %q", got, "tinyemu-go")
	}
}
