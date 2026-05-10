package x86

import "testing"

// TestDecJnzFastpath verifies the `dec eax; jnz -3` shortcut used to elide
// Linux's __delay() loop. The expected post-state is EAX=0, ZF=1, EIP just
// past the JNZ.
func TestDecJnzFastpath(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 100_000)
	// 0x1000: 48        dec eax
	// 0x1001: 75 FD     jnz -3
	// 0x1003: F4        hlt
	code := []byte{0x48, 0x75, 0xFD, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = %d, want 0", got)
	}
	if !c.getZF() {
		t.Errorf("ZF not set after fastpath")
	}
}

// TestDecJnzFastpath_SingleIter: when EAX is already 1, one normal DEC takes
// it to 0; the fastpath is not used. Verifies we don't break the non-fastpath
// case.
func TestDecJnzFastpath_SingleIter(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 1)
	code := []byte{0x48, 0x75, 0xFD, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0 {
		t.Errorf("EAX = %d, want 0", got)
	}
	if !c.getZF() {
		t.Errorf("ZF not set after final DEC")
	}
}

// TestRDTSCDelayLoopFastpath verifies that when RDTSC is followed by the
// SUB/SBB+JB pattern characteristic of Linux's delay_tsc, the fastpath reads
// bclock and target off the stack and inflates `cycles` so the loop exits.
func TestRDTSCDelayLoopFastpath(t *testing.T) {
	c := newTestCPU(t)
	const ebp = uint32(0x9000)
	c.SetReg32(EBP, ebp)
	const bclock = uint64(0x100)
	const target = uint64(1 << 20)
	c.writeMem32(ebp-0x20, uint32(bclock))
	c.writeMem32(ebp-0x1C, uint32(bclock>>32))
	c.writeMem32(ebp-0x18, uint32(target))
	c.writeMem32(ebp-0x14, uint32(target>>32))
	c.cycles = bclock

	hitsBefore := TSCFastpathHits()
	// Layout (test code starts at 0x1000):
	//   offset 0-11:  12 NOPs (target for the JB-back; we just need EIP to
	//                 land somewhere before RDTSC so the fastpath fires).
	//   offset 12-13: 0F 31           rdtsc
	//   offset 14-16: 90 90 90        nops
	//   offset 17-19: 8B 4D E8        mov ecx, [ebp-0x18]    (target_lo)
	//   offset 20-22: 8B 5D EC        mov ebx, [ebp-0x14]    (target_hi)
	//   offset 23-25: 2B 45 E0        sub eax, [ebp-0x20]    (delta_lo)
	//   offset 26-28: 1B 55 E4        sbb edx, [ebp-0x1C]    (delta_hi)
	//   offset 29-30: 39 C8           cmp eax, ecx
	//   offset 31-32: 89 D0           mov eax, edx
	//   offset 33-34: 19 D8           sbb eax, ebx
	//   offset 35-36: 72 D9           jb -39 (back to offset 0)
	//   offset 37:    F4              hlt
	code := []byte{
		0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90,
		0x0F, 0x31,
		0x90, 0x90, 0x90,
		0x8B, 0x4D, 0xE8,
		0x8B, 0x5D, 0xEC,
		0x2B, 0x45, 0xE0,
		0x1B, 0x55, 0xE4,
		0x39, 0xC8,
		0x89, 0xD0,
		0x19, 0xD8,
		0x72, 0xD9, // jb -39
		0xF4,
	}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if TSCFastpathHits() == hitsBefore {
		t.Errorf("TSC delay-loop fastpath did not fire")
	}
	if c.cycles < bclock+target {
		t.Errorf("cycles = 0x%X, want >= bclock+target = 0x%X", c.cycles, bclock+target)
	}
}
