package x86

import (
	"testing"
)

// TestFSSegmentOverrideOnModRMLoad verifies that a `64` (FS) prefix on a
// ModRM-encoded load is honored.
//
// Regression: handleModRM32 previously hard-coded `c.segBase[DS] + mr.ea` and
// silently dropped any segment-override prefix. The Linux kernel uses
// `mov reg, fs:[per_cpu_var]` everywhere for per-CPU accesses, so dropping
// FS meant per-CPU reads landed at the static link-time address instead of
// the dynamic per-CPU instance — the kernel ended up dereferencing zero-
// initialized data and faulting (boot died at "Attempted to kill the idle
// task!").
func TestFSSegmentOverrideOnModRMLoad(t *testing.T) {
	c := newTestCPU(t)
	// FS.base = 0x10000. The target memory is at FS:0x40 = linear 0x10040.
	c.segBase[FS] = 0x10000
	c.writeMem32(0x10040, 0xDEADBEEF)
	// Also stash a different value at DS:0x40 = linear 0x40 so we can tell
	// which segment was actually used.
	c.writeMem32(0x00040, 0xBADBADBA)
	// `64 8B 05 40 00 00 00` = MOV EAX, fs:[0x40]
	code := []byte{0x64, 0x8B, 0x05, 0x40, 0x00, 0x00, 0x00, 0xF4} // ...; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xDEADBEEF {
		t.Errorf("EAX = 0x%08X, want 0xDEADBEEF (FS-prefixed load went to DS)", v)
	}
}

// TestFSSegmentOverrideOnModRMStore verifies the same for stores.
func TestFSSegmentOverrideOnModRMStore(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[FS] = 0x10000
	c.SetReg32(EAX, 0xCAFEBABE)
	// `64 89 05 80 00 00 00` = MOV fs:[0x80], EAX
	code := []byte{0x64, 0x89, 0x05, 0x80, 0x00, 0x00, 0x00, 0xF4} // ...; HLT
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.readMem32(0x10080); v != 0xCAFEBABE {
		t.Errorf("memory at FS:0x80 (linear 0x10080) = 0x%08X, want 0xCAFEBABE", v)
	}
	if v := c.readMem32(0x00080); v != 0 {
		t.Errorf("memory at DS:0x80 (linear 0x80) = 0x%08X — store leaked to DS segment", v)
	}
}

// TestGSSegmentOverrideOnModRMLoad verifies GS prefix (the rarely-used twin
// of FS — Linux uses it for cpu_number on i386 in some configurations).
func TestGSSegmentOverrideOnModRMLoad(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[GS] = 0x20000
	c.writeMem32(0x20100, 0x11223344)
	// `65 8B 05 00 01 00 00` = MOV EAX, gs:[0x100]
	code := []byte{0x65, 0x8B, 0x05, 0x00, 0x01, 0x00, 0x00, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x11223344 {
		t.Errorf("EAX = 0x%08X, want 0x11223344", v)
	}
}

// TestEBPDefaultsToSSWithoutOverride verifies that addressing modes built on
// EBP default to SS even without an explicit prefix. Linux's stack accesses
// depend on this: every `mov reg, [ebp+disp]` is an SS access.
func TestEBPDefaultsToSSWithoutOverride(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[SS] = 0x30000
	c.SetReg32(EBP, 0x100)
	// SS:[EBP+4] = linear 0x30104
	c.writeMem32(0x30104, 0xABCDEF01)
	// `8B 45 04` = MOV EAX, [EBP+4]
	code := []byte{0x8B, 0x45, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0xABCDEF01 {
		t.Errorf("EAX = 0x%08X, want 0xABCDEF01 ([EBP+4] didn't use SS)", v)
	}
}

// TestDSPrefixOverridesSSDefault verifies that an explicit DS prefix on an
// EBP-based access overrides the default SS.
func TestDSPrefixOverridesSSDefault(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[SS] = 0x30000
	c.SetReg32(EBP, 0x100)
	c.writeMem32(0x30104, 0x11111111) // SS:[EBP+4]
	c.writeMem32(0x00104, 0x22222222) // DS:[EBP+4]
	// `3E 8B 45 04` = MOV EAX, ds:[EBP+4]
	code := []byte{0x3E, 0x8B, 0x45, 0x04, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x22222222 {
		t.Errorf("EAX = 0x%08X, want 0x22222222 (DS prefix didn't override SS default)", v)
	}
}

// TestSIBWithEBPBaseDefaultsToSS exercises the SIB encoding path: when SIB
// base is EBP (5), the default segment is SS.
func TestSIBWithEBPBaseDefaultsToSS(t *testing.T) {
	c := newTestCPU(t)
	c.segBase[SS] = 0x40000
	c.SetReg32(EBP, 0x200)
	c.SetReg32(ECX, 0)
	// SS:[EBP+ECX*1+8] = linear 0x40208
	c.writeMem32(0x40208, 0x99887766)
	// `8B 44 0D 08` = MOV EAX, [EBP + ECX + 8]
	// ModRM = 44 (mod=01, reg=000, rm=100=SIB)
	// SIB = 0D (scale=00, index=001=ECX, base=101=EBP)
	// disp8 = 08
	code := []byte{0x8B, 0x44, 0x0D, 0x08, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if v := c.GetReg32(EAX); v != 0x99887766 {
		t.Errorf("EAX = 0x%08X, want 0x99887766 (SIB w/ EBP base didn't use SS)", v)
	}
}
