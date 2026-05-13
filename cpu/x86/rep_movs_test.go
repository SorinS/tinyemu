package x86

import "testing"

// TestRepMovsB_ECXReflectsCompletedIterations exercises the
// Intel-SDM-correct restart semantics for REP MOVSB: ECX must reflect
// the number of iterations that COMPLETED, not the number we entered.
//
// This was a real bug — `executeString` decremented ECX before invoking
// the operation. When `op()` faulted (e.g. copy_to_user reaching an
// unmapped user page), the panic unwound the loop after ECX had already
// been decremented but the read/write hadn't happened. On instruction
// restart from the kernel's fault-fixup path, the CPU used the over-
// decremented ECX as the remaining count — losing one iteration per
// fault, and dropping bytes from kernel-to-user copies. Manifested as
// busybox sh seeing "echoaAlpine" instead of `echo "Alpine ...` when
// reading the Alpine init script: a byte was being silently dropped
// each time the read crossed a not-yet-faulted-in user page.
//
// We simulate the fault by having `op` panic with a pageFaultError
// after a known number of iterations, then inspecting ECX.
func TestRepMovsB_ECXReflectsCompletedIterations(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 10)

	completed := 0
	op := func() {
		if completed == 3 {
			// Simulate a #PF on iteration 4 — exactly mirroring what
			// our writeMem8 does via translateAddress when a page is
			// missing.
			panic(pageFaultError{addr: 0xDEAD0000, errorCode: 0x06})
		}
		completed++
	}

	defer func() {
		// The faulting op should have unwound the loop via panic.
		r := recover()
		if _, ok := r.(pageFaultError); !ok {
			t.Fatalf("expected pageFaultError, got %T %v", r, r)
		}
		// 3 iterations completed (op ran successfully), so ECX should
		// be 10 - 3 = 7.
		if got := c.GetReg32(ECX); got != 7 {
			t.Errorf("ECX after fault on 4th iter = %d, want 7", got)
		}
	}()
	c.executeString(1, 1, op, false, false)
}

// TestRepMovsB_NoFaultDecrementsCorrectly verifies the non-faulting case
// — all iterations complete, ECX ends at 0.
func TestRepMovsB_NoFaultDecrementsCorrectly(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(ECX, 5)
	completed := 0
	op := func() { completed++ }
	c.executeString(1, 1, op, false, false)
	if completed != 5 {
		t.Errorf("op ran %d times, want 5", completed)
	}
	if got := c.GetReg32(ECX); got != 0 {
		t.Errorf("ECX after completion = %d, want 0", got)
	}
}
