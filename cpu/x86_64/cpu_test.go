package x86_64

import (
	"testing"

	"github.com/jtolio/tinyemu-go/cpu"
	"github.com/jtolio/tinyemu-go/mem"
)

func newTestCPU(t *testing.T) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	return NewCPU(mm)
}

func TestReset_DefaultState(t *testing.T) {
	c := newTestCPU(t)

	if c.mode != ModeReal16 {
		t.Errorf("mode = %v, want ModeReal16", c.mode)
	}
	if c.GetRIP() != 0xFFF0 {
		t.Errorf("RIP = %#x, want 0xFFF0", c.GetRIP())
	}
	if c.GetSeg(CS) != 0xF000 {
		t.Errorf("CS selector = %#x, want 0xF000", c.GetSeg(CS))
	}
	if c.GetSegBase64(CS) != 0xF0000 {
		t.Errorf("CS base = %#x, want 0xF0000", c.GetSegBase64(CS))
	}
	if c.GetRFLAGS() != 2 {
		t.Errorf("RFLAGS = %#x, want 2 (reserved bit 1)", c.GetRFLAGS())
	}
	// CR0 power-on value is ET | CD | NW (caches disabled), per Intel SDM.
	if c.GetCR64(0) != CR0_ET|CR0_CD|CR0_NW {
		t.Errorf("CR0 = %#x, want %#x (ET|CD|NW)", c.GetCR64(0), CR0_ET|CR0_CD|CR0_NW)
	}
	if c.GetEFER() != 0 {
		t.Errorf("EFER = %#x, want 0", c.GetEFER())
	}
	// EDX resets to the processor signature (family 6); all other GPRs zero.
	for i := 0; i < 16; i++ {
		want := uint64(0)
		if i == RDX {
			want = 0x600
		}
		if v := c.GetReg64(i); v != want {
			t.Errorf("reg64[%d] = %#x, want %#x", i, v, want)
		}
	}
	if c.GetCycles() != 0 {
		t.Errorf("cycles = %d, want 0", c.GetCycles())
	}
	if c.IsPowerDown() {
		t.Errorf("powerDown set after Reset")
	}
	if c.GetINTR() != 0 {
		t.Errorf("INTR line = %d, want 0", c.GetINTR())
	}
}

// TestReset_AfterMutation: writing then Reset must produce exactly the
// same state as a fresh CPU. Guards against half-baked Reset entries.
func TestReset_AfterMutation(t *testing.T) {
	c := newTestCPU(t)
	for i := 0; i < 16; i++ {
		c.SetReg64(i, uint64(0xDEADBEEF00000000)|uint64(i))
	}
	c.SetRIP(0x1234_5678_9ABC_DEF0)
	c.SetRFLAGS(0xFFFFFFFF)
	c.SetCR64(0, CR0_PE|CR0_PG)
	c.SetEFER(EFER_LMA | EFER_LME | EFER_SCE)
	c.SetINTR(1)
	c.cycles = 12345
	c.powerDown = true

	c.Reset()

	fresh := newTestCPU(t)
	if c.GetRIP() != fresh.GetRIP() {
		t.Errorf("RIP not reset: %#x vs %#x", c.GetRIP(), fresh.GetRIP())
	}
	if c.GetRFLAGS() != fresh.GetRFLAGS() {
		t.Errorf("RFLAGS not reset: %#x vs %#x", c.GetRFLAGS(), fresh.GetRFLAGS())
	}
	if c.GetCR64(0) != fresh.GetCR64(0) {
		t.Errorf("CR0 not reset: %#x vs %#x", c.GetCR64(0), fresh.GetCR64(0))
	}
	if c.GetEFER() != fresh.GetEFER() {
		t.Errorf("EFER not reset: %#x vs %#x", c.GetEFER(), fresh.GetEFER())
	}
	if c.GetINTR() != fresh.GetINTR() {
		t.Errorf("INTR not reset: %d vs %d", c.GetINTR(), fresh.GetINTR())
	}
	if c.IsPowerDown() != fresh.IsPowerDown() {
		t.Errorf("powerDown not reset")
	}
	for i := 0; i < 16; i++ {
		want := uint64(0)
		if i == RDX {
			want = 0x600 // EDX resets to the processor signature
		}
		if c.GetReg64(i) != want {
			t.Errorf("reg64[%d] not reset after Reset: %#x, want %#x", i, c.GetReg64(i), want)
		}
	}
}

func TestReg64_RoundTrip(t *testing.T) {
	c := newTestCPU(t)
	for i := 0; i < 16; i++ {
		v := uint64(0xDEAD_BEEF_CAFE_0000) | uint64(i)
		c.SetReg64(i, v)
	}
	for i := 0; i < 16; i++ {
		want := uint64(0xDEAD_BEEF_CAFE_0000) | uint64(i)
		if got := c.GetReg64(i); got != want {
			t.Errorf("reg64[%d] = %#x, want %#x", i, got, want)
		}
	}
}

// TestReg32_ZeroExtends ensures a 32-bit write clears the upper 32 bits,
// matching the long-mode semantic guarantee. This is the key invariant
// the boot path relies on so that the post-Reset zero-state plus 32-bit
// register writes leaves the high half deterministically zero.
func TestReg32_ZeroExtends(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg64(RAX, 0xFFFFFFFF_FFFFFFFF)
	c.SetReg32(EAX, 0x12345678)
	if got := c.GetReg64(RAX); got != 0x00000000_12345678 {
		t.Errorf("RAX after SetReg32 = %#x, want 0x12345678 (zero-extended)", got)
	}
	if got := c.GetReg32(EAX); got != 0x12345678 {
		t.Errorf("EAX = %#x, want 0x12345678", got)
	}
}

// TestReg16_PreservesUpper48: 16-bit writes only affect the low 16 bits.
// Real hardware does not zero-extend on 16-bit writes.
func TestReg16_PreservesUpper48(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg64(RAX, 0x1122_3344_5566_7788)
	c.SetReg16(AX, 0xAABB)
	if got := c.GetReg64(RAX); got != 0x1122_3344_5566_AABB {
		t.Errorf("RAX after SetReg16 = %#x, want 0x11223344_5566AABB", got)
	}
	if got := c.GetReg16(AX); got != 0xAABB {
		t.Errorf("AX = %#x, want 0xAABB", got)
	}
}

// TestReg8_PreservesOtherBytes: writes to AL / AH only change their
// respective bytes; all other bits in RAX are preserved.
func TestReg8_PreservesOtherBytes(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg64(RAX, 0xDEAD_BEEF_CAFE_F00D)
	c.SetReg8(AL, 0x11)
	if got := c.GetReg64(RAX); got != 0xDEAD_BEEF_CAFE_F011 {
		t.Errorf("RAX after SetReg8(AL,0x11) = %#x", got)
	}
	c.SetReg8(AH, 0x22)
	if got := c.GetReg64(RAX); got != 0xDEAD_BEEF_CAFE_2211 {
		t.Errorf("RAX after SetReg8(AH,0x22) = %#x", got)
	}
	if got := c.GetReg8(AL); got != 0x11 {
		t.Errorf("AL = %#x, want 0x11", got)
	}
	if got := c.GetReg8(AH); got != 0x22 {
		t.Errorf("AH = %#x, want 0x22", got)
	}
}

// TestExtRegs_R8toR15 covers the long-mode-only registers reachable
// via REX.B / REX.R / REX.X. They share storage with reg64[8..15].
func TestExtRegs_R8toR15(t *testing.T) {
	c := newTestCPU(t)
	values := []uint64{
		0x0808_0808_0808_0808,
		0x0909_0909_0909_0909,
		0x0A0A_0A0A_0A0A_0A0A,
		0x0B0B_0B0B_0B0B_0B0B,
		0x0C0C_0C0C_0C0C_0C0C,
		0x0D0D_0D0D_0D0D_0D0D,
		0x0E0E_0E0E_0E0E_0E0E,
		0x0F0F_0F0F_0F0F_0F0F,
	}
	for i, v := range values {
		c.SetReg64(R8+i, v)
	}
	for i, v := range values {
		if got := c.GetReg64(R8 + i); got != v {
			t.Errorf("R%d = %#x, want %#x", 8+i, got, v)
		}
	}
}

func TestSegmentAccessors(t *testing.T) {
	c := newTestCPU(t)
	c.SetSeg(DS, 0x0023)
	c.SetSegBase(DS, 0xDEADBEEF)
	c.SetSegLimit(DS, 0xFFFFFFFF)
	c.SetSegAccess(DS, 0xC92)
	if c.GetSeg(DS) != 0x0023 {
		t.Error("DS selector mismatch")
	}
	if c.GetSegBase(DS) != 0xDEADBEEF {
		t.Error("DS base32 mismatch")
	}
	if c.GetSegBase64(DS) != 0xDEADBEEF {
		t.Error("DS base64 mismatch")
	}
	if c.GetSegLimit(DS) != 0xFFFFFFFF {
		t.Error("DS limit mismatch")
	}
	if c.GetSegAccess(DS) != 0xC92 {
		t.Error("DS access mismatch")
	}
}

// TestSegBase64 verifies the 64-bit base setter — used by long-mode WRMSR
// to FS_BASE/GS_BASE — preserves bits above the 32-bit window.
func TestSegBase64(t *testing.T) {
	c := newTestCPU(t)
	c.SetSegBase64(FS, 0x1234_5678_9ABC_DEF0)
	if got := c.GetSegBase64(FS); got != 0x1234_5678_9ABC_DEF0 {
		t.Errorf("FS base64 = %#x", got)
	}
	if got := c.GetSegBase(FS); got != 0x9ABC_DEF0 {
		t.Errorf("FS base32 (low half) = %#x", got)
	}
}

func TestCRAccessors(t *testing.T) {
	c := newTestCPU(t)
	c.SetCR(0, CR0_PE|CR0_PG)
	// CR0.ET is hardwired to 1 (P6+), so it reads back set regardless.
	if c.GetCR(0) != CR0_PE|CR0_PG|CR0_ET {
		t.Errorf("CR0 32-bit accessor = %#x", c.GetCR(0))
	}
	if c.GetCR64(0) != CR0_PE|CR0_PG|CR0_ET {
		t.Errorf("CR0 64-bit accessor = %#x", c.GetCR64(0))
	}
	c.SetCR64(3, 0x1_0000_2000)
	if c.GetCR64(3) != 0x1_0000_2000 {
		t.Errorf("CR3 64-bit accessor")
	}
	if c.GetCR(3) != 0x0000_2000 {
		t.Errorf("CR3 32-bit accessor (low half) = %#x", c.GetCR(3))
	}
}

func TestRFLAGSReservedBit(t *testing.T) {
	c := newTestCPU(t)
	c.SetRFLAGS(0)
	if c.GetRFLAGS() != 2 {
		t.Errorf("RFLAGS = %#x, expected reserved bit 1 to be re-asserted", c.GetRFLAGS())
	}
	c.SetRFLAGS(0xFFFF_FFFF_FFFF_FFFF)
	if c.GetRFLAGS()&^ValidFlagMask != 2 {
		t.Errorf("RFLAGS = %#x kept bits outside ValidFlagMask other than the reserved bit", c.GetRFLAGS())
	}
}

func TestINTRLine(t *testing.T) {
	c := newTestCPU(t)
	if c.GetINTR() != 0 {
		t.Fatal("INTR set before any input")
	}
	c.SetINTR(1)
	if c.GetINTR() != 1 {
		t.Errorf("INTR not raised")
	}
	c.SetINTR(0)
	if c.GetINTR() != 0 {
		t.Errorf("INTR not lowered")
	}
}

func TestHasPendingInterrupt(t *testing.T) {
	c := newTestCPU(t)
	// Line low + IF clear → no pending.
	if c.HasPendingInterrupt() {
		t.Errorf("pending with line=0, IF=0")
	}
	// Line high but IF still clear → not deliverable, no pending.
	c.SetINTR(1)
	if c.HasPendingInterrupt() {
		t.Errorf("pending with line=1, IF=0 (interrupts disabled)")
	}
	// IF set + line high → pending.
	c.SetRFLAGS(RFLAGS_IF)
	if !c.HasPendingInterrupt() {
		t.Errorf("not pending with line=1, IF=1")
	}
	// IF set + line low → not pending.
	c.SetINTR(0)
	if c.HasPendingInterrupt() {
		t.Errorf("pending with line=0, IF=1")
	}
}

func TestCyclesAccessors(t *testing.T) {
	c := newTestCPU(t)
	if c.GetCycles() != 0 {
		t.Fatalf("cycles = %d at construction", c.GetCycles())
	}
	c.AddCycles(7)
	c.AddCycles(13)
	if c.GetCycles() != 20 {
		t.Errorf("cycles = %d, want 20", c.GetCycles())
	}
}

func TestPowerDownAccessors(t *testing.T) {
	c := newTestCPU(t)
	c.SetPowerDown(true)
	if !c.IsPowerDown() {
		t.Error("SetPowerDown(true) not visible")
	}
	c.SetPowerDown(false)
	if c.IsPowerDown() {
		t.Error("SetPowerDown(false) not visible")
	}
}

// TestRecomputeMode verifies the mode derivation cases that M1 + later
// milestones will switch on.
func TestRecomputeMode(t *testing.T) {
	c := newTestCPU(t)

	// Default after Reset: real mode.
	c.recomputeMode()
	if c.mode != ModeReal16 {
		t.Errorf("after Reset, mode=%v want ModeReal16", c.mode)
	}

	// Enter 16-bit protected mode (PE=1, CS.D=0).
	c.SetCR(0, CR0_PE)
	c.SetSegAccess(CS, 0x9A) // present, code, no D bit
	c.recomputeMode()
	if c.mode != ModeProtected16 {
		t.Errorf("PE=1, CS.D=0: mode=%v want ModeProtected16", c.mode)
	}

	// Enter 32-bit protected mode (PE=1, CS.D=1).
	c.SetSegAccess(CS, 0xC9A) // bit 10 (D) set
	c.recomputeMode()
	if c.mode != ModeProtected32 {
		t.Errorf("PE=1, CS.D=1: mode=%v want ModeProtected32", c.mode)
	}

	// Enter long mode compat (LMA=1, CS.L=0).
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, 0xC9A) // L bit (bit 9) cleared
	c.recomputeMode()
	if c.mode != ModeCompat32 {
		t.Errorf("LMA=1, CS.L=0: mode=%v want ModeCompat32", c.mode)
	}

	// Enter 64-bit long mode (LMA=1, CS.L=1).
	c.SetSegAccess(CS, 0x29A) // L bit (bit 9) set, D bit clear (AMD spec)
	c.recomputeMode()
	if c.mode != ModeLong64 {
		t.Errorf("LMA=1, CS.L=1: mode=%v want ModeLong64", c.mode)
	}

	if !c.IsLongMode() {
		t.Errorf("IsLongMode() false in long mode")
	}
}

func TestModeString(t *testing.T) {
	cases := []struct {
		m    Mode
		want string
	}{
		{ModeReal16, "real16"},
		{ModeProtected16, "pm16"},
		{ModeProtected32, "pm32"},
		{ModeCompat32, "compat32"},
		{ModeLong64, "long64"},
	}
	for _, tc := range cases {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", tc.m, got, tc.want)
		}
	}
}

// TestSetters_LatchLMA is a regression test against a bug where the
// CR/EFER setter methods bypassed writeCR and the LMA latching logic.
// The chassis bring-up sequence in machine/pc/bzimage64.go calls
// SetEFER(LME) followed by SetCR(0, ...|PG) — exactly the order that
// must trigger LMA on real hardware. Pinning each setter individually
// so future refactors can't silently drop the funnel.
func TestSetters_LatchLMA(t *testing.T) {
	t.Run("SetCR_PG_With_LME_LatchesLMA", func(t *testing.T) {
		c := newTestCPU(t)
		c.SetEFER(EFER_LME)
		if c.GetEFER()&EFER_LMA != 0 {
			t.Fatalf("LMA latched too early (paging not yet on)")
		}
		c.SetCR(0, CR0_PE|CR0_PG)
		if c.GetEFER()&EFER_LMA == 0 {
			t.Errorf("LMA failed to latch on SetCR with PG=1 and EFER.LME set")
		}
	})

	t.Run("SetCR64_PG_With_LME_LatchesLMA", func(t *testing.T) {
		c := newTestCPU(t)
		c.SetEFER(EFER_LME)
		c.SetCR64(0, CR0_PE|CR0_PG)
		if c.GetEFER()&EFER_LMA == 0 {
			t.Errorf("LMA failed to latch via SetCR64")
		}
	})

	t.Run("SetEFER_After_PG_LatchesLMA", func(t *testing.T) {
		c := newTestCPU(t)
		c.SetCR(0, CR0_PE|CR0_PG) // PG up first, LME still off
		if c.GetEFER()&EFER_LMA != 0 {
			t.Fatalf("LMA latched without LME")
		}
		c.SetEFER(EFER_LME) // now flip LME
		if c.GetEFER()&EFER_LMA == 0 {
			t.Errorf("SetEFER didn't latch LMA even though PG was already on")
		}
	})

	t.Run("SetCR_ClearPG_DropsLMA", func(t *testing.T) {
		c := newTestCPU(t)
		c.SetEFER(EFER_LME)
		c.SetCR(0, CR0_PE|CR0_PG)
		// Sanity: LMA up.
		if c.GetEFER()&EFER_LMA == 0 {
			t.Fatal("setup: LMA should have latched")
		}
		c.SetCR(0, CR0_PE) // clear PG
		if c.GetEFER()&EFER_LMA != 0 {
			t.Errorf("LMA still set after PG cleared")
		}
	})

	t.Run("SetSegAccess_CS_RecomputesMode", func(t *testing.T) {
		c := newTestCPU(t)
		c.SetEFER(EFER_LME)
		c.SetCR(0, CR0_PE|CR0_PG)
		// We're in compat32 at this point (CS.L=0).
		if c.mode != ModeCompat32 {
			t.Fatalf("setup: mode=%v want ModeCompat32", c.mode)
		}
		// Toggle CS.L=1 and confirm the cached mode flips without an
		// explicit recompute call.
		c.SetSegAccess(CS, csLBit)
		if c.mode != ModeLong64 {
			t.Errorf("SetSegAccess(CS, csLBit) didn't flip mode; got %v", c.mode)
		}
	})
}

// TestSatisfiesX86Core uses the *CPU through the cpu.X86Core interface
// to confirm the contract is exercisable end-to-end (the compile-time
// var _ assertion in exec.go covers signature presence; this drives the
// methods via the interface).
func TestSatisfiesX86Core(t *testing.T) {
	var core cpu.X86Core = newTestCPU(t)
	core.SetINTR(1)
	if core.GetINTR() != 1 {
		t.Errorf("interface-level GetINTR")
	}
	core.AddCycles(42)
	if core.GetCycles() != 42 {
		t.Errorf("interface-level cycles")
	}
	core.SetSeg(CS, 0x0008)
	core.SetSegBase(CS, 0)
	core.SetSegLimit(CS, 0xFFFFFFFF)
	core.SetSegAccess(CS, 0xC9A)
	core.SetEIP(0x1000)
	core.SetReg32(EAX, 0xDEADBEEF)
	core.SetCR(0, CR0_PE)
	if core.GetCR(0) != CR0_PE|CR0_ET { // ET hardwired to 1
		t.Errorf("interface-level CR = %#x", core.GetCR(0))
	}
}
