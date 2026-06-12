package x86_64

import "testing"

// Tests for the instruction prefetch buffer (cpu.go ifBuf + mem.go
// fetch8/fillFetchBuffer + the invalidation hooks). A stale instruction
// byte is the nastiest possible bug (wrong execution, no crash), so these
// pin every path: correct decode, page-boundary refill, self-modifying
// code, each TLB-flush invalidation site, and the non-RAM fallthrough.

// TestFetchBuffer_DecodesAndFills: a normal multi-byte instruction decodes
// correctly and leaves the buffer populated (so subsequent bytes are served
// without re-translating).
func TestFetchBuffer_DecodesAndFills(t *testing.T) {
	c, mm := longModeCPU(t)
	// mov eax, 0xDEADBEEF  (B8 EF BE AD DE) at 0x1000
	for i, b := range []byte{0xB8, 0xEF, 0xBE, 0xAD, 0xDE} {
		_ = mm.Write8(0x1000+uint64(i), b)
	}
	c.SetRIP(0x1000)
	if err := c.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xDEADBEEF {
		t.Errorf("EAX = %#x, want 0xDEADBEEF", got)
	}
	if c.ifBufValid == 0 {
		t.Errorf("prefetch buffer empty after a RAM fetch (should be filled)")
	}
}

// TestFetchBuffer_PageCross: an instruction whose bytes straddle a 4 KiB
// boundary must decode correctly — the fill stops at the page boundary, so
// the second half forces a refill. mov eax,imm32 placed at 0xFFD spans
// 0xFFD..0x1001.
func TestFetchBuffer_PageCross(t *testing.T) {
	c, mm := longModeCPU(t)
	const addr = uint64(0xFFD)
	for i, b := range []byte{0xB8, 0x0D, 0xF0, 0xFE, 0xCA} { // mov eax, 0xCAFEF00D
		_ = mm.Write8(addr+uint64(i), b)
	}
	c.SetRIP(addr)
	if err := c.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if got := c.GetReg32(EAX); got != 0xCAFEF00D {
		t.Errorf("page-crossing fetch decoded EAX = %#x, want 0xCAFEF00D", got)
	}
}

// TestFetchBuffer_SMC: the self-modifying-code hook. A physical write that
// overlaps the buffered bytes must invalidate it (so re-execution sees the
// new code), while a write outside the buffered range must NOT.
func TestFetchBuffer_SMC(t *testing.T) {
	c, mm := longModeCPU(t)

	t.Run("write outside range does not invalidate", func(t *testing.T) {
		c, mm := longModeCPU(t)
		for i := 0; i < 16; i++ {
			_ = mm.Write8(0x2000+uint64(i), 0x90)
		}
		if !c.fillFetchBuffer(0x2000) || c.ifBufValid == 0 {
			t.Fatal("buffer not filled")
		}
		// Well past the buffered bytes — must be left alone.
		c.writeMem8(0x2000+uint64(c.ifBufValid)+64, 0xAB)
		if c.ifBufValid == 0 {
			t.Errorf("a write outside the buffered range wrongly invalidated it")
		}
	})

	t.Run("write inside range invalidates", func(t *testing.T) {
		for i := 0; i < 16; i++ {
			_ = mm.Write8(0x2000+uint64(i), 0x90)
		}
		if !c.fillFetchBuffer(0x2000) || c.ifBufValid == 0 {
			t.Fatal("buffer not filled")
		}
		c.writeMem8(0x2004, 0xAB) // inside the buffered range
		if c.ifBufValid != 0 {
			t.Errorf("an SMC write inside the buffered range did not invalidate it")
		}
	})

	t.Run("re-execution sees rewritten code", func(t *testing.T) {
		c, mm := longModeCPU(t)
		// mov eax, 0x11111111 at 0x3000
		for i, b := range []byte{0xB8, 0x11, 0x11, 0x11, 0x11} {
			_ = mm.Write8(0x3000+uint64(i), b)
		}
		c.SetRIP(0x3000)
		if err := c.Step(); err != nil {
			t.Fatalf("Step1: %v", err)
		}
		if c.GetReg32(EAX) != 0x11111111 {
			t.Fatalf("first run EAX = %#x", c.GetReg32(EAX))
		}
		// Overwrite the immediate via the data-write path (the SMC hook).
		c.writeMem32(0x3001, 0x22222222)
		// Re-execute the same address; must fetch the NEW immediate.
		c.SetRIP(0x3000)
		if err := c.Step(); err != nil {
			t.Fatalf("Step2: %v", err)
		}
		if got := c.GetReg32(EAX); got != 0x22222222 {
			t.Errorf("re-exec EAX = %#x, want 0x22222222 (stale prefetch buffer?)", got)
		}
	})
}

// TestFetchBuffer_InvalidatedByTLBFlush: every path that flushes the TLB
// must also drop the prefetch buffer — otherwise a CR3 swap / mode change
// could surface stale instruction bytes from the old address space.
func TestFetchBuffer_InvalidatedByTLBFlush(t *testing.T) {
	triggers := []struct {
		name string
		fire func(c *CPU)
	}{
		{"CR3 reload", func(c *CPU) { c.writeCR(3, 0x5000) }},
		{"CR0 PG toggle", func(c *CPU) { c.writeCR(0, c.cr[0]^CR0_PG) }},
		{"CR4 PAE toggle", func(c *CPU) { c.writeCR(4, c.cr[4]^CR4_PAE) }},
		{"EFER LME toggle", func(c *CPU) { c.SetEFER(c.efer ^ EFER_LME) }},
	}
	for _, tr := range triggers {
		t.Run(tr.name, func(t *testing.T) {
			c, mm := longModeCPU(t)
			for i := 0; i < 8; i++ {
				_ = mm.Write8(0x1000+uint64(i), 0x90)
			}
			if !c.fillFetchBuffer(0x1000) || c.ifBufValid == 0 {
				t.Fatal("buffer not filled")
			}
			tr.fire(c)
			if c.ifBufValid != 0 {
				t.Errorf("%s did not invalidate the prefetch buffer", tr.name)
			}
		})
	}

	// INVLPG goes through the opcode handler, so drive the instruction.
	t.Run("INVLPG", func(t *testing.T) {
		c, mm := longModeCPU(t)
		if err := runInsn(t, c, mm, []byte{0x0F, 0x01, 0x38}); err != nil { // INVLPG [RAX]
			t.Fatalf("Step: %v", err)
		}
		if c.ifBufValid != 0 {
			t.Errorf("INVLPG did not invalidate the prefetch buffer")
		}
	})
}

// TestFetchBuffer_NonRAMFallthrough: a fetch target that is not RAM must
// leave the buffer empty (fillFetchBuffer returns false) so each byte falls
// through to the per-byte path — never speculatively reading ahead into a
// device region.
func TestFetchBuffer_NonRAMFallthrough(t *testing.T) {
	c, mm := longModeCPU(t)
	for i := 0; i < 8; i++ {
		_ = mm.Write8(0x1000+uint64(i), 0x90)
	}
	if !c.fillFetchBuffer(0x1000) || c.ifBufValid == 0 {
		t.Fatal("RAM fill failed")
	}
	// 0x200000 is above the 1 MiB RAM region → no RAM backing.
	if c.fillFetchBuffer(0x200000) {
		t.Errorf("fillFetchBuffer returned true for a non-RAM address")
	}
	if c.ifBufValid != 0 {
		t.Errorf("non-RAM fill left the buffer valid (would read-ahead into a device)")
	}
}
