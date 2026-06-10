package x86_64

import (
	"errors"
	"testing"
)

// TestCPUFeatureProfile covers the three CPU feature profiles (advisor #2):
// how an unimplemented opcode is handled, and what CPUID.1 ECX advertises.
func TestCPUFeatureProfile(t *testing.T) {
	// Pragmatic (default): an unimplemented opcode halts with
	// ErrNotImplemented.
	t.Run("pragmatic halts on unimplemented", func(t *testing.T) {
		c := newTestCPU(t)
		if err := c.unimplementedAt("test opcode %d", 7); !errors.Is(err, ErrNotImplemented) {
			t.Errorf("got %v, want ErrNotImplemented", err)
		}
	})

	// Debug: an unimplemented opcode logs once and delivers #UD (continue),
	// surfacing as an exceptionPanic{Vec:6} that Step turns into delivery.
	t.Run("debug delivers #UD on unimplemented", func(t *testing.T) {
		c := newTestCPU(t)
		c.featureProfile = profileDebug
		c.seenUnimpl = make(map[string]struct{})
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected a #UD panic, got none")
			}
			if ex, ok := r.(exceptionPanic); !ok || ex.Vec != 6 {
				t.Errorf("panic = %v, want exceptionPanic{Vec:6}", r)
			}
		}()
		_ = c.unimplementedAt("test opcode %d", 7)
	})

	// Debug dedup must key by the formatted opcode, not the raw format
	// string: the dispatch defaults reuse one format ("0F %#02x rex=%#x")
	// for every missing 0F opcode, so two distinct opcodes sharing a format
	// must each be recorded (else only the first gap ever logs).
	t.Run("debug dedups by formatted opcode", func(t *testing.T) {
		c := newTestCPU(t)
		c.featureProfile = profileDebug
		c.seenUnimpl = make(map[string]struct{})
		call := func(op int) {
			defer func() { _ = recover() }() // unimplementedAt panics (raiseUD)
			_ = c.unimplementedAt("0F %#02x rex=%#x", op, 0)
		}
		call(0x05)
		call(0x07)
		call(0x05) // repeat must not add a new entry
		if len(c.seenUnimpl) != 2 {
			t.Errorf("seenUnimpl has %d entries, want 2 (distinct opcodes sharing a format must each record)", len(c.seenUnimpl))
		}
	})

	// CPUID.1 ECX: pragmatic advertises SSE3 (bit 0) + RDRAND (bit 30);
	// strict drops them but keeps the SSE2 baseline in EDX (bit 26).
	t.Run("CPUID ECX features by profile", func(t *testing.T) {
		const ecxBits = uint32(1<<0 | 1<<30)

		c := newTestCPU(t)
		c.SetReg64(RAX, 1)
		if err := c.opCPUID(); err != nil {
			t.Fatalf("opCPUID: %v", err)
		}
		if c.GetReg32(ECX)&ecxBits == 0 {
			t.Errorf("pragmatic: ECX=%#x missing SSE3/RDRAND", c.GetReg32(ECX))
		}

		s := newTestCPU(t)
		s.featureProfile = profileStrict
		s.SetReg64(RAX, 1)
		if err := s.opCPUID(); err != nil {
			t.Fatalf("opCPUID: %v", err)
		}
		if s.GetReg32(ECX)&ecxBits != 0 {
			t.Errorf("strict: ECX=%#x still advertises SSE3/RDRAND", s.GetReg32(ECX))
		}
		if s.GetReg32(EDX)&(1<<26) == 0 {
			t.Errorf("strict: dropped the SSE2 baseline (EDX bit 26)=%#x", s.GetReg32(EDX))
		}
	})
}

// TestUnknownMSR covers the permissive-but-logged MSR policy (advisor #4):
// an unmodeled MSR still reads as 0 / drops writes, but in the debug
// profile each is noted once. Pragmatic must stay silent (and not touch
// the nil seen-set).
func TestUnknownMSR(t *testing.T) {
	const unknown = uint32(0xDEADBEEF)

	// Debug: behaviour unchanged (0 / drop) but recorded once.
	c := newTestCPU(t)
	c.featureProfile = profileDebug
	c.seenUnimpl = make(map[string]struct{})
	if v := c.readMSR(unknown); v != 0 {
		t.Errorf("unknown RDMSR = %#x, want 0", v)
	}
	if _, ok := c.seenUnimpl["msr-RDMSR-0xdeadbeef"]; !ok {
		t.Errorf("debug: RDMSR of unknown MSR not recorded")
	}
	if err := c.writeMSR(unknown, 0x1234); err != nil {
		t.Errorf("unknown WRMSR returned error: %v", err)
	}
	if _, ok := c.seenUnimpl["msr-WRMSR-0xdeadbeef"]; !ok {
		t.Errorf("debug: WRMSR of unknown MSR not recorded")
	}

	// Pragmatic: same permissive behaviour, no recording, no nil-map panic.
	p := newTestCPU(t)
	if v := p.readMSR(unknown); v != 0 {
		t.Errorf("pragmatic unknown RDMSR = %#x, want 0", v)
	}
	if err := p.writeMSR(unknown, 0x1234); err != nil {
		t.Errorf("pragmatic unknown WRMSR: %v", err)
	}
}
