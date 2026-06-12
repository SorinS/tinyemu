package x86_64

import (
	"strings"
	"testing"
)

// Regression tests for privileged/system instructions: LMSW, INVLPG, LTR.

// TestLMSWCannotClearPE: LMSW may set CR0.PE but must never clear it — there
// is no escape from protected mode via LMSW. It must still update MP/EM/TS.
func TestLMSWCannotClearPE(t *testing.T) {
	c, mm := longModeCPU(t)
	c.cr[0] |= CR0_PE  // already in protected mode
	c.cr[0] &^= CR0_TS // TS starts clear
	// MSW source word 0x0008 (TS=1, PE=0) in memory at [RAX].
	c.SetReg64(RAX, 0x4000)
	_ = mm.Write8(0x4000, 0x08)
	_ = mm.Write8(0x4001, 0x00)
	// 0F 01 30 = LMSW [RAX] (ModRM mod=00 reg=6 rm=0)
	if err := runInsn(t, c, mm, []byte{0x0F, 0x01, 0x30}); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if c.cr[0]&CR0_PE == 0 {
		t.Errorf("LMSW cleared CR0.PE (illegal — cannot leave protected mode)")
	}
	if c.cr[0]&CR0_TS == 0 {
		t.Errorf("LMSW failed to set CR0.TS (the legal bits must still update)")
	}
}

// TestInvlpgUsesLinearAddress: INVLPG must invalidate the operand's linear
// address (segment base + effective address), not the bare effective
// address. With a non-zero FS base the two differ; only the linear entry
// should be dropped.
func TestInvlpgUsesLinearAddress(t *testing.T) {
	c, mm := longModeCPU(t)
	const fsBase = uint64(0x40000)
	const ea = uint64(0x2000)
	lin := fsBase + ea // 0x42000

	c.segBase[FS] = fsBase
	c.SetReg64(RAX, ea)

	// Seed two distinct TLB entries: one at the linear address, one at the
	// bare EA. The fix must drop the linear one and leave the EA one.
	c.tlb.insert(lin, 0xAA000, true, false, false, false)
	c.tlb.insert(ea, 0xBB000, true, false, false, false)

	// 64 0F 01 38 = INVLPG FS:[RAX]  (0x64 = FS override, ModRM 0x38 = [RAX])
	if err := runInsn(t, c, mm, []byte{0x64, 0x0F, 0x01, 0x38}); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, ok := c.tlb.lookup(lin, false, false, false); ok {
		t.Errorf("INVLPG did not invalidate the linear address %#x", lin)
	}
	if _, ok := c.tlb.lookup(ea, false, false, false); !ok {
		t.Errorf("INVLPG wrongly invalidated the bare EA %#x instead of the linear address", ea)
	}
}

// TestLTRRejectsNotPresentDescriptor: LTR must not silently install a zero
// TR base/limit when the descriptor read fails. An unmapped GDT reads back
// as a zero descriptor (Present bit clear), which must surface as an error
// rather than corrupting TR.
func TestLTRRejectsNotPresentDescriptor(t *testing.T) {
	c, mm := longModeCPU(t)
	c.segBase[GDTR] = 0x200000 // above the 1 MiB RAM region → descriptor reads as 0
	c.SetReg64(RAX, 0x8)       // selector 0x8 (index 1, non-null)
	// 0F 00 D8 = LTR AX (Group 6 /3, ModRM mod=11 reg=3 rm=0)
	err := runInsn(t, c, mm, []byte{0x0F, 0x00, 0xD8})
	if err == nil {
		t.Fatalf("LTR silently accepted a not-present TSS descriptor (TR base/limit would be corrupted to 0)")
	}
	if !strings.Contains(err.Error(), "LTR") {
		t.Errorf("error = %q, want it to mention LTR", err)
	}
}
