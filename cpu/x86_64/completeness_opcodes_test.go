package x86_64

import (
	"math"
	"strings"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// These cover the "for completeness" opcode batch: POPCNT, CMPXCHG8B/16B,
// XGETBV/XSETBV, and the SSE3 horizontal/add-subtract FP instructions —
// plus the CPUID feature bits that advertise the ones guests probe for.

// run executes a single instruction's worth of bytes at codeAddr in long
// mode and returns any Step error. Paging is off in longModeCPU, so
// virtual == physical and the registered low-RAM is directly addressable.
func runInsn(t *testing.T, c *CPU, mm *mem.PhysMemoryMap, bytes []byte) error {
	t.Helper()
	const codeAddr uint64 = 0x1000
	for i, b := range bytes {
		_ = mm.Write8(codeAddr+uint64(i), b)
	}
	c.SetRIP(codeAddr)
	return c.Step()
}

func TestPOPCNT(t *testing.T) {
	// POPCNT EAX, ECX  — F3 0F B8 C1 (ModRM C1: mod=11 reg=0 rm=1).
	t.Run("32-bit", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg32(ECX, 0xF0F0_000F) // 8 + 4 = 12 set bits
		if err := runInsn(t, c, mm, []byte{0xF3, 0x0F, 0xB8, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if got := c.GetReg32(EAX); got != 12 {
			t.Errorf("POPCNT result = %d, want 12", got)
		}
		if c.rflags&RFLAGS_ZF != 0 {
			t.Errorf("ZF set for non-zero source")
		}
	})

	// POPCNT RAX, RCX — F3 48 0F B8 C1.
	t.Run("64-bit", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg64(RCX, 0xFFFF_FFFF_FFFF_FFFF) // 64 set bits
		if err := runInsn(t, c, mm, []byte{0xF3, 0x48, 0x0F, 0xB8, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if got := c.GetReg64(RAX); got != 64 {
			t.Errorf("POPCNT result = %d, want 64", got)
		}
	})

	// Source == 0 sets ZF and yields 0.
	t.Run("zero sets ZF", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg32(ECX, 0)
		c.rflags |= RFLAGS_CF | RFLAGS_OF | RFLAGS_SF // must be cleared
		if err := runInsn(t, c, mm, []byte{0xF3, 0x0F, 0xB8, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if got := c.GetReg32(EAX); got != 0 {
			t.Errorf("POPCNT(0) = %d, want 0", got)
		}
		if c.rflags&RFLAGS_ZF == 0 {
			t.Errorf("ZF not set for zero source")
		}
		if c.rflags&(RFLAGS_CF|RFLAGS_OF|RFLAGS_SF) != 0 {
			t.Errorf("CF/OF/SF not cleared by POPCNT")
		}
	})

	// Without the F3 prefix, 0F B8 is reserved → #UD. With no IDT
	// installed, Step reports the undelivered fault (vec=6).
	t.Run("no F3 is #UD", func(t *testing.T) {
		c, mm := longModeCPU(t)
		err := runInsn(t, c, mm, []byte{0x0F, 0xB8, 0xC1})
		if err == nil || !strings.Contains(err.Error(), "vec=6") {
			t.Fatalf("0F B8 without F3: err = %v, want #UD (vec=6)", err)
		}
	})
}

func TestCMPXCHG8B(t *testing.T) {
	const memAddr uint64 = 0x2000
	// CMPXCHG8B [RDI] — 0F C7 0F (ModRM 0x0F: mod=00 reg=1 rm=7).
	prog := []byte{0x0F, 0xC7, 0x0F}

	t.Run("match swaps and sets ZF", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg64(RDI, memAddr)
		_ = mm.Write64(memAddr, 0x1111_2222_3333_4444)
		c.SetReg32(EDX, 0x1111_2222) // expected high
		c.SetReg32(EAX, 0x3333_4444) // expected low — matches memory
		c.SetReg32(ECX, 0xAAAA_BBBB) // new high
		c.SetReg32(EBX, 0xCCCC_DDDD) // new low
		if err := runInsn(t, c, mm, prog); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if c.rflags&RFLAGS_ZF == 0 {
			t.Errorf("ZF not set on match")
		}
		if v, _ := mm.Read64(memAddr); v != 0xAAAA_BBBB_CCCC_DDDD {
			t.Errorf("memory = %#x, want 0xAAAABBBB_CCCCDDDD", v)
		}
	})

	t.Run("mismatch loads memory into EDX:EAX and clears ZF", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg64(RDI, memAddr)
		_ = mm.Write64(memAddr, 0x1111_2222_3333_4444)
		c.SetReg32(EDX, 0xDEAD_BEEF) // wrong
		c.SetReg32(EAX, 0xDEAD_BEEF)
		c.rflags |= RFLAGS_ZF
		if err := runInsn(t, c, mm, prog); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if c.rflags&RFLAGS_ZF != 0 {
			t.Errorf("ZF still set on mismatch")
		}
		if c.GetReg32(EDX) != 0x1111_2222 || c.GetReg32(EAX) != 0x3333_4444 {
			t.Errorf("EDX:EAX = %#x:%#x, want 0x11112222:0x33334444",
				c.GetReg32(EDX), c.GetReg32(EAX))
		}
		if v, _ := mm.Read64(memAddr); v != 0x1111_2222_3333_4444 {
			t.Errorf("memory mutated on mismatch: %#x", v)
		}
	})
}

func TestCMPXCHG16B(t *testing.T) {
	const memAddr uint64 = 0x2000
	// CMPXCHG16B [RDI] — 48 0F C7 0F.
	prog := []byte{0x48, 0x0F, 0xC7, 0x0F}

	t.Run("match swaps and sets ZF", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg64(RDI, memAddr)
		_ = mm.Write64(memAddr, 0x0123_4567_89AB_CDEF)   // low 64
		_ = mm.Write64(memAddr+8, 0xFEDC_BA98_7654_3210) // high 64
		c.SetReg64(RAX, 0x0123_4567_89AB_CDEF)           // expected low
		c.SetReg64(RDX, 0xFEDC_BA98_7654_3210)           // expected high
		c.SetReg64(RBX, 0x1111_1111_1111_1111)           // new low
		c.SetReg64(RCX, 0x2222_2222_2222_2222)           // new high
		if err := runInsn(t, c, mm, prog); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if c.rflags&RFLAGS_ZF == 0 {
			t.Errorf("ZF not set on 128-bit match")
		}
		lo, _ := mm.Read64(memAddr)
		hi, _ := mm.Read64(memAddr + 8)
		if lo != 0x1111_1111_1111_1111 || hi != 0x2222_2222_2222_2222 {
			t.Errorf("memory = %#x:%#x, want RBX:RCX", hi, lo)
		}
	})

	t.Run("mismatch loads memory into RAX:RDX and clears ZF", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg64(RDI, memAddr)
		_ = mm.Write64(memAddr, 0x0123_4567_89AB_CDEF)
		_ = mm.Write64(memAddr+8, 0xFEDC_BA98_7654_3210)
		c.SetReg64(RAX, 0xDEAD) // wrong
		c.SetReg64(RDX, 0xBEEF)
		c.rflags |= RFLAGS_ZF
		if err := runInsn(t, c, mm, prog); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if c.rflags&RFLAGS_ZF != 0 {
			t.Errorf("ZF still set on mismatch")
		}
		if c.GetReg64(RAX) != 0x0123_4567_89AB_CDEF || c.GetReg64(RDX) != 0xFEDC_BA98_7654_3210 {
			t.Errorf("RAX:RDX = %#x:%#x, want loaded memory", c.GetReg64(RDX), c.GetReg64(RAX))
		}
	})
}

func TestXGETBV_XSETBV(t *testing.T) {
	// XGETBV — 0F 01 D0. After reset XCR0 = 1 (x87 bit only).
	t.Run("XGETBV reads XCR0", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.SetReg32(ECX, 0)
		c.SetReg64(RAX, 0xDEAD)
		c.SetReg64(RDX, 0xBEEF)
		if err := runInsn(t, c, mm, []byte{0x0F, 0x01, 0xD0}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if c.GetReg64(RAX) != 1 || c.GetReg64(RDX) != 0 {
			t.Errorf("XGETBV → EDX:EAX = %#x:%#x, want 0:1", c.GetReg64(RDX), c.GetReg64(RAX))
		}
	})

	// XSETBV — 0F 01 D1. Setting XCR0 with the x87 bit kept is allowed.
	t.Run("XSETBV writes XCR0", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.cpl = 0
		c.SetReg32(ECX, 0)
		c.SetReg32(EAX, 0x1) // keep x87
		c.SetReg32(EDX, 0)
		if err := runInsn(t, c, mm, []byte{0x0F, 0x01, 0xD1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if c.xcr0 != 1 {
			t.Errorf("XCR0 = %#x, want 1", c.xcr0)
		}
	})

	// XSETBV clearing the x87 bit is #GP(0).
	t.Run("XSETBV clearing x87 bit faults #GP", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.cpl = 0
		c.SetReg32(ECX, 0)
		c.SetReg32(EAX, 0) // clear x87 — invalid
		c.SetReg32(EDX, 0)
		assertDeliversGP(t, c, mm, []byte{0x0F, 0x01, 0xD1})
		if c.xcr0 != 1 {
			t.Errorf("XCR0 mutated by faulting XSETBV: %#x", c.xcr0)
		}
	})

	// XSETBV at CPL!=0 is #GP(0). Delivering a fault from CPL 3 needs a
	// TSS (for the ring-0 stack), which this minimal harness doesn't set
	// up — so instead we run without an IDT and assert Step reports the
	// #GP (vec=13) it couldn't deliver. That proves the privilege check
	// fired; the clearing-x87 case above already exercises real delivery.
	t.Run("XSETBV at CPL 3 faults #GP", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.cpl = 3
		c.SetReg32(ECX, 0)
		c.SetReg32(EAX, 1)
		c.SetReg32(EDX, 0)
		err := runInsn(t, c, mm, []byte{0x0F, 0x01, 0xD1})
		if err == nil || !strings.Contains(err.Error(), "vec=13") {
			t.Fatalf("XSETBV at CPL 3: err = %v, want an undelivered #GP (vec=13)", err)
		}
		if c.xcr0 != 1 {
			t.Errorf("XCR0 mutated by faulting XSETBV: %#x", c.xcr0)
		}
	})
}

// assertDeliversGP installs a #GP (vector 13) IDT gate, runs the bytes,
// and asserts the fault was delivered (RIP lands at the handler). This is
// what Step does with the raiseGP panic: recover, rewind RIP, vector
// through the IDT — so a bare panic never escapes Step.
func assertDeliversGP(t *testing.T, c *CPU, mm *mem.PhysMemoryMap, bytes []byte) {
	t.Helper()
	const idtBase uint64 = 0x4000
	const gpHandler uint64 = 0x90000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 13, 0x0008, gpHandler, 0, 0x8E)
	c.reg64[RSP] = 0x8000
	if err := runInsn(t, c, mm, bytes); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if c.GetRIP() != gpHandler {
		t.Errorf("RIP = %#x after #GP, want handler %#x", c.GetRIP(), gpHandler)
	}
}

// packF32 lays four float32 lanes into the [2]uint64 XMM representation
// (lane0 low 32 of word0, lane1 high 32 of word0, lanes 2/3 in word1).
func packF32(a, b, cc, d float32) [2]uint64 {
	return [2]uint64{
		uint64(math.Float32bits(a)) | uint64(math.Float32bits(b))<<32,
		uint64(math.Float32bits(cc)) | uint64(math.Float32bits(d))<<32,
	}
}

func unpackF32(v [2]uint64) [4]float32 {
	return [4]float32{
		math.Float32frombits(uint32(v[0])),
		math.Float32frombits(uint32(v[0] >> 32)),
		math.Float32frombits(uint32(v[1])),
		math.Float32frombits(uint32(v[1] >> 32)),
	}
}

func packF64(a, b float64) [2]uint64 {
	return [2]uint64{math.Float64bits(a), math.Float64bits(b)}
}

func TestSSE3_HorizontalAndAddSub(t *testing.T) {
	// Single-precision (F2 prefix) on xmm0, xmm1: 0F D0/7C/7D C1.
	t.Run("ADDSUBPS", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.xmm[0] = packF32(1, 2, 3, 4)
		c.xmm[1] = packF32(10, 20, 30, 40)
		if err := runInsn(t, c, mm, []byte{0xF2, 0x0F, 0xD0, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		// even lanes subtract, odd lanes add.
		want := [4]float32{1 - 10, 2 + 20, 3 - 30, 4 + 40}
		if got := unpackF32(c.xmm[0]); got != want {
			t.Errorf("ADDSUBPS = %v, want %v", got, want)
		}
	})

	t.Run("HADDPS", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.xmm[0] = packF32(1, 2, 3, 4)
		c.xmm[1] = packF32(10, 20, 30, 40)
		if err := runInsn(t, c, mm, []byte{0xF2, 0x0F, 0x7C, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		want := [4]float32{1 + 2, 3 + 4, 10 + 20, 30 + 40}
		if got := unpackF32(c.xmm[0]); got != want {
			t.Errorf("HADDPS = %v, want %v", got, want)
		}
	})

	t.Run("HSUBPS", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.xmm[0] = packF32(1, 2, 3, 4)
		c.xmm[1] = packF32(10, 20, 30, 40)
		if err := runInsn(t, c, mm, []byte{0xF2, 0x0F, 0x7D, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		want := [4]float32{1 - 2, 3 - 4, 10 - 20, 30 - 40}
		if got := unpackF32(c.xmm[0]); got != want {
			t.Errorf("HSUBPS = %v, want %v", got, want)
		}
	})

	// Double-precision (66 prefix): 66 0F D0/7C/7D C1.
	t.Run("ADDSUBPD", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.xmm[0] = packF64(1, 2)
		c.xmm[1] = packF64(10, 20)
		if err := runInsn(t, c, mm, []byte{0x66, 0x0F, 0xD0, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		// low subtracts, high adds.
		if l := math.Float64frombits(c.xmm[0][0]); l != 1-10 {
			t.Errorf("ADDSUBPD low = %v, want %v", l, float64(1-10))
		}
		if h := math.Float64frombits(c.xmm[0][1]); h != 2+20 {
			t.Errorf("ADDSUBPD high = %v, want %v", h, float64(2+20))
		}
	})

	t.Run("HADDPD", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.xmm[0] = packF64(1, 2)
		c.xmm[1] = packF64(10, 20)
		if err := runInsn(t, c, mm, []byte{0x66, 0x0F, 0x7C, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if l := math.Float64frombits(c.xmm[0][0]); l != 1+2 {
			t.Errorf("HADDPD low = %v, want 3", l)
		}
		if h := math.Float64frombits(c.xmm[0][1]); h != 10+20 {
			t.Errorf("HADDPD high = %v, want 30", h)
		}
	})

	t.Run("HSUBPD", func(t *testing.T) {
		c, mm := longModeCPU(t)
		c.xmm[0] = packF64(1, 2)
		c.xmm[1] = packF64(10, 20)
		if err := runInsn(t, c, mm, []byte{0x66, 0x0F, 0x7D, 0xC1}); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if l := math.Float64frombits(c.xmm[0][0]); l != 1-2 {
			t.Errorf("HSUBPD low = %v, want -1", l)
		}
		if h := math.Float64frombits(c.xmm[0][1]); h != 10-20 {
			t.Errorf("HSUBPD high = %v, want -10", h)
		}
	})
}

// TestCPUID_AdvertisesNewFeatures: CMPXCHG16B (ECX bit 13) and POPCNT
// (ECX bit 23) are now real opcodes, so leaf 1 must advertise them — and
// keep advertising them even in the strict profile (they're implemented,
// not optional like SSE3/RDRAND).
func TestCPUID_AdvertisesNewFeatures(t *testing.T) {
	const cx16 = uint32(1 << 13)
	const popcnt = uint32(1 << 23)

	for _, p := range []cpuFeatureProfile{profilePragmatic, profileStrict} {
		c := newTestCPU(t)
		c.featureProfile = p
		c.SetReg64(RAX, 1)
		if err := c.opCPUID(); err != nil {
			t.Fatalf("opCPUID: %v", err)
		}
		ecx := c.GetReg32(ECX)
		if ecx&cx16 == 0 {
			t.Errorf("profile %d: ECX=%#x missing CX16 (bit 13)", p, ecx)
		}
		if ecx&popcnt == 0 {
			t.Errorf("profile %d: ECX=%#x missing POPCNT (bit 23)", p, ecx)
		}
		// OSXSAVE (bit 27) must stay OFF — we don't implement XSAVE.
		if ecx&(1<<27) != 0 {
			t.Errorf("profile %d: ECX=%#x wrongly advertises OSXSAVE", p, ecx)
		}
	}
}
