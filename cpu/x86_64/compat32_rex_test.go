package x86_64

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// TestCompat32_0x48IsDecNotREX pins the SDM-mandated behaviour that in
// compat32 (long mode active, CS.L=0) the byte 0x48 is the opcode
// `DEC EAX`, NOT a REX prefix. The TinyCorePure64 PVH entry runs in
// compat32 between the PVH boot transition and the `lretq` into 64-bit
// long mode, and the kernel emits sequences like `48 8D 05 disp32`
// expecting it to decode as legacy `DEC EAX; LEA …` rather than the
// long-mode `LEA RAX, [RIP+disp32]`. Misclassifying 0x48 as REX makes
// the dispatcher consume seven bytes for a single instruction instead
// of one, skipping past the kernel's real `DEC EAX` and walking RIP
// straight into the LEA's displacement bytes.
func TestCompat32_0x48IsDecNotREX(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)

	// Compat32: long mode active (LMA latched) with CS.L=0 / CS.D=1.
	// PVH's lretq lands the guest here; the kernel runs in this mode
	// until it loads a 64-bit code segment and `lretq`s into long64.
	// We don't enable paging — the test exercises the decoder, not the
	// page walker; an identity-mapped page table would just add noise.
	c.SetCR64(0, CR0_PE)
	c.efer = EFER_LME | EFER_LMA // bypass SetEFER's "need PG for LMA" gate
	c.SetSegBase(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	c.SetSegAccess(CS, csDBit) // D=1, L=0 → ModeCompat32
	if c.mode != ModeCompat32 {
		t.Fatalf("CPU mode = %v, want compat32 (test setup wrong)", c.mode)
	}

	// Byte sequence: 0x48 0x8D 0x05 0x57 0xFD 0xFF 0xFF
	// In long64: LEA RAX, [RIP + 0xFFFFFD57]  (7 bytes, REX.W=1).
	// In compat32: DEC EAX (1 byte) then continues with the rest.
	const codeAddr uint64 = 0x1000
	code := []byte{0x48, 0x8D, 0x05, 0x57, 0xFD, 0xFF, 0xFF, 0xF4}
	for i, b := range code {
		_ = mm.Write8(codeAddr+uint64(i), b)
	}
	c.SetRIP(codeAddr)
	c.SetReg64(RAX, 0xFFFFFFFF)

	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}

	// After one Step the dispatcher must have advanced RIP by 1
	// (consuming only the 0x48 = DEC EAX) and decremented EAX by 1.
	if got := c.GetRIP(); got != codeAddr+1 {
		t.Errorf("RIP = %#x, want %#x — dispatcher consumed too many bytes "+
			"(0x48 was treated as a REX prefix instead of DEC EAX)",
			got, codeAddr+1)
	}
	if got := c.GetReg64(RAX); got != 0xFFFFFFFE {
		t.Errorf("RAX = %#x, want 0xfffffffe — DEC EAX didn't run", got)
	}
}
