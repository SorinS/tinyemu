package x86_64

// M5a unit tests — CR/DR/MSR access and flag-manipulation ops driven
// by hand-built byte sequences. Asm-level NASM tests for the same
// surface live in test/x86_64/system_asm_test.go.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// stepCPU is a small helper that builds a long-mode CPU, drops bytes
// at codeBase, and runs Step until count instructions are consumed or
// HLT terminates.
func stepCPU(t *testing.T, bytes []byte, count int) *CPU {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.recomputeMode()
	const base uint64 = 0x1000
	for i, b := range bytes {
		if err := mm.Write8(base+uint64(i), b); err != nil {
			t.Fatalf("Write8: %v", err)
		}
	}
	c.SetRIP(base)
	for i := 0; i < count; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	return c
}

func TestStepFlag_STC_CLC_CMC(t *testing.T) {
	// F9 STC; F5 CMC (CF flips); F8 CLC; F4 HLT.
	c := stepCPU(t, []byte{0xF9, 0xF5, 0xF8, 0xF4}, 4)
	if c.rflags&RFLAGS_CF != 0 {
		t.Errorf("CF set after CLC final step")
	}
	// Intermediate observability check: rerun, stop after STC + CMC.
	c2 := stepCPU(t, []byte{0xF9, 0xF5, 0xF4}, 2)
	if c2.rflags&RFLAGS_CF != 0 {
		t.Errorf("CF set after STC then CMC (should toggle back to clear)")
	}
	// STC alone leaves CF set.
	c3 := stepCPU(t, []byte{0xF9, 0xF4}, 1)
	if c3.rflags&RFLAGS_CF == 0 {
		t.Errorf("CF clear after STC")
	}
}

func TestStepFlag_STI_CLI(t *testing.T) {
	// FB STI; FA CLI.
	c := stepCPU(t, []byte{0xFB, 0xFA, 0xF4}, 1)
	if c.rflags&RFLAGS_IF == 0 {
		t.Errorf("IF clear after STI")
	}
	c2 := stepCPU(t, []byte{0xFB, 0xFA, 0xF4}, 2)
	if c2.rflags&RFLAGS_IF != 0 {
		t.Errorf("IF set after CLI")
	}
}

func TestStepFlag_STD_CLD(t *testing.T) {
	c := stepCPU(t, []byte{0xFD, 0xF4}, 1) // STD
	if c.rflags&RFLAGS_DF == 0 {
		t.Errorf("DF clear after STD")
	}
	c2 := stepCPU(t, []byte{0xFD, 0xFC, 0xF4}, 2) // STD + CLD
	if c2.rflags&RFLAGS_DF != 0 {
		t.Errorf("DF set after CLD")
	}
}

// TestMovToCR0_TriggersLMA: setting CR0.PG with EFER.LME=1 should
// latch EFER.LMA on real hardware. Our writeCR helper folds that
// transition in.
func TestMovToCR0_TriggersLMA(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	// Start in 32-bit protected mode with LME set but paging off.
	c.SetCR64(0, CR0_PE)
	c.SetEFER(EFER_LME)
	c.SetCR64(4, CR4_PAE)
	c.SetSegAccess(CS, csDBit) // CS.D=1 ⇒ 32-bit code
	c.SetSegBase(CS, 0)
	c.recomputeMode()
	if c.mode != ModeProtected32 {
		t.Fatalf("setup: mode=%v want ModeProtected32", c.mode)
	}

	// 0F 22 C0 — MOV CR0, RAX with RAX = CR0_PE | CR0_PG.
	const base uint64 = 0x1000
	c.SetReg64(RAX, CR0_PE|CR0_PG)
	prog := []byte{0x0F, 0x22, 0xC0, 0xF4}
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if c.efer&EFER_LMA == 0 {
		t.Errorf("LMA not set after enabling paging with LME=1")
	}
	if c.cr[0]&CR0_PG == 0 {
		t.Errorf("CR0.PG not set")
	}
	// Mode is still ModeCompat32 because CS.L=0; a far jump would
	// switch to ModeLong64 — not yet implemented.
	if c.mode != ModeCompat32 {
		t.Errorf("mode = %v, want ModeCompat32 (CS.L=0 under LMA)", c.mode)
	}
}

// TestMovFromCR0 / TestMovToCR3: round-trip control-register access.
func TestMovToFromCRn(t *testing.T) {
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
	c.recomputeMode()

	// 0F 20 C0 — MOV RAX, CR0 (reg=000, rm=000)
	// 0F 22 D8 — MOV CR3, RAX (reg=011, rm=000)
	const base uint64 = 0x1000
	c.SetCR64(0, CR0_PE|CR0_ET|0x10000)
	c.SetCR64(3, 0xDEADBEEFCAFE1000)
	prog := []byte{
		0x0F, 0x20, 0xC0, // MOV RAX, CR0
		0x0F, 0x22, 0xD8, // MOV CR3, RAX
		0xF4,
	}
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	for i := 0; i < 2; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if c.GetReg64(RAX) != CR0_PE|CR0_ET|0x10000 {
		t.Errorf("RAX = %#x", c.GetReg64(RAX))
	}
	if c.cr[3] != CR0_PE|CR0_ET|0x10000 {
		t.Errorf("CR3 not loaded from RAX")
	}
}

// TestWRMSR_EFER: writing the EFER MSR latches LMA when paging is on.
// Called via direct writeMSR (instead of through a stepped WRMSR
// instruction) so we don't need a valid PML4 just to test the latch.
func TestWRMSR_EFER(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.cr[0] = CR0_PE | CR0_PG // PG on without going through writeCR
	c.efer = 0
	if err := c.writeMSR(msrEFER, uint64(EFER_LME)); err != nil {
		t.Fatalf("writeMSR: %v", err)
	}
	if c.efer&EFER_LME == 0 {
		t.Errorf("LME not set after writeMSR(EFER, LME)")
	}
	if c.efer&EFER_LMA == 0 {
		t.Errorf("LMA not latched (paging on when LME set)")
	}

	// And the inverse: PG off ⇒ LME without LMA latch.
	c2 := NewCPU(mm)
	c2.cr[0] = CR0_PE
	if err := c2.writeMSR(msrEFER, uint64(EFER_LME)); err != nil {
		t.Fatalf("writeMSR: %v", err)
	}
	if c2.efer&EFER_LMA != 0 {
		t.Errorf("LMA latched without paging on")
	}
}

// TestWRMSR_FSBase: writing FS_BASE updates the FS segment base.
func TestWRMSR_FSBase(t *testing.T) {
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
	c.recomputeMode()

	c.SetReg32(ECX, msrFSBaseMSR)
	c.SetReg32(EAX, 0xCAFEBABE)
	c.SetReg32(EDX, 0x12345678)
	const base uint64 = 0x1000
	prog := []byte{0x0F, 0x30, 0xF4}
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	want := uint64(0x12345678_CAFEBABE)
	if c.msrFSBase != want {
		t.Errorf("msrFSBase = %#x, want %#x", c.msrFSBase, want)
	}
	if c.segBase[FS] != want {
		t.Errorf("segBase[FS] not synced; got %#x", c.segBase[FS])
	}
}

// TestRDMSR_RoundTrip: WRMSR then RDMSR returns the same value.
func TestRDMSR_RoundTrip(t *testing.T) {
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
	c.recomputeMode()

	// Write LSTAR = 0xFFFFFFFF80100000.
	c.SetReg32(ECX, msrLSTAR)
	c.SetReg32(EAX, 0x80100000)
	c.SetReg32(EDX, 0xFFFFFFFF)
	// Then read it back into different registers via RDMSR (ECX kept).
	const base uint64 = 0x1000
	prog := []byte{
		0x0F, 0x30, // WRMSR
		0x0F, 0x32, // RDMSR
		0xF4,
	}
	for i, b := range prog {
		_ = mm.Write8(base+uint64(i), b)
	}
	c.SetRIP(base)
	for i := 0; i < 2; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if c.GetReg32(EAX) != 0x80100000 {
		t.Errorf("EAX = %#x after RDMSR, want 0x80100000", c.GetReg32(EAX))
	}
	if c.GetReg32(EDX) != 0xFFFFFFFF {
		t.Errorf("EDX = %#x after RDMSR, want 0xFFFFFFFF", c.GetReg32(EDX))
	}
}
