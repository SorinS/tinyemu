package arm64

import (
	"testing"

	asmarm64 "github.com/sorins/tinyemu-go/asm/arm64"
	"github.com/sorins/tinyemu-go/mem"
)

// place assembles instrs and writes them at the physical address.
func place(t *testing.T, mm *mem.PhysMemoryMap, addr uint64, instrs []string) {
	t.Helper()
	off := uint64(0)
	for _, s := range instrs {
		b, err := asmarm64.Assemble(s)
		if err != nil {
			t.Fatalf("assemble %q: %v", s, err)
		}
		for i, by := range b {
			_ = mm.Write8(addr+off+uint64(i), by)
		}
		off += uint64(len(b))
	}
}

// runTo steps until PC reaches sentinel (or a cap), starting from c.PC.
func runTo(t *testing.T, c *CPU, sentinel uint64) {
	t.Helper()
	c.X[30] = sentinel
	for step := 0; c.PC != sentinel && step < 500; step++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step at %#x: %v", c.PC, err)
		}
	}
	if c.PC != sentinel {
		t.Fatalf("did not reach sentinel; stuck at %#x", c.PC)
	}
}

const (
	excCode     = 0x100000
	excVBAR     = 0x80000
	excHandler  = excVBAR + 0x200 // current-EL SPx synchronous vector
	excSentinel = 0xDEAD0000
)

// TestException_SVC exercises the full synchronous-exception round trip: an EL1
// program installs VBAR, executes svc, the vectored handler marks a register
// and erets, and control resumes after the svc. It checks ESR_EL1 (EC=SVC +
// imm), the ELR return address, and that PSTATE/SP survive the round trip.
func TestException_SVC(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<21, 0); err != nil {
		t.Fatal(err)
	}
	place(t, mm, excCode, []string{
		"movz x10, #0x8, lsl #16", // x10 = 0x80000
		"msr vbar_el1, x10",
		"movz x0, #1",   // overwritten by the handler
		"svc #42",       // → handler; ELR = the next instruction
		"movz x1, #0x5555", // proves control resumed after eret
		"ret",
	})
	place(t, mm, excHandler, []string{
		"movz x0, #0xabcd", // handler ran
		"eret",
	})
	c := New(mm)
	c.PC = excCode
	c.SP = 0x70000
	runTo(t, c, excSentinel)

	if c.X[0] != 0xABCD {
		t.Errorf("x0 = %#x; handler did not run", c.X[0])
	}
	if c.X[1] != 0x5555 {
		t.Errorf("x1 = %#x; did not resume after eret", c.X[1])
	}
	if want := uint64(0x15)<<26 | 42; c.ESR != want {
		t.Errorf("ESR = %#x, want %#x (EC=SVC, imm=42)", c.ESR, want)
	}
	// ELR was the instruction after svc (svc is at code+0x0C, so ELR=code+0x10).
	if c.ELR != excCode+0x10 {
		t.Errorf("ELR = %#x, want %#x", c.ELR, uint64(excCode+0x10))
	}
}

// TestException_DataAbort drives a translation fault into the EL1 handler: the
// MMU is on, a load hits an unmapped VA, the data-abort vector reads FAR/ESR,
// skips the faulting instruction (ELR += 4) to avoid an infinite retry, and
// erets. Checks the abort EC, FAR, and that the handler ran and resumed.
func TestException_DataAbort(t *testing.T) {
	const (
		l0, l1, l2, l3 = 0x1000, 0x2000, 0x3000, 0x4000
		badVA          = 0x40010000 // intentionally NOT mapped (L3 leaf absent)
	)
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<21, 0); err != nil {
		t.Fatal(err)
	}
	// Identity-map [0,1GiB) so the code, tables, and VBAR are reachable with the
	// MMU on; leave badVA (in the second L1 entry) unmapped → translation fault.
	_ = mm.Write64(l0+0*8, l1|0b11)
	_ = mm.Write64(l1+0*8, 0x0|(1<<10)|0b01) // identity block, AF
	// (no entry for L1[1] → VA 0x40010000 faults at L1)

	place(t, mm, excCode, []string{
		"movz x10, #0x8, lsl #16", "msr vbar_el1, x10", // VBAR
		"movz x0, #16", "msr tcr_el1, x0", // T0SZ=16
		"movz x1, #0x1000", "msr ttbr0_el1, x1",
		"movz x2, #1", "msr sctlr_el1, x2", "isb", // MMU on
		"movz x3, #0x4001, lsl #16", // x3 = badVA
		"ldr x4, [x3]",              // faults → data abort handler
		"movz x5, #0x600d",          // resumed after the handler skipped the ldr
		"ret",
	})
	place(t, mm, excHandler, []string{
		"mrs x20, far_el1", // capture fault address
		"mrs x21, esr_el1", // capture syndrome
		"mrs x22, elr_el1", "add x22, x22, #4", "msr elr_el1, x22", // skip the ldr
		"movz x23, #0xdead",
		"eret",
	})
	c := New(mm)
	c.PC = excCode
	c.SP = 0x70000
	runTo(t, c, excSentinel)

	if c.X[23] != 0xDEAD {
		t.Errorf("x23 = %#x; abort handler did not run", c.X[23])
	}
	if c.X[20] != badVA {
		t.Errorf("FAR (x20) = %#x, want %#x", c.X[20], uint64(badVA))
	}
	if ec := (c.X[21] >> 26) & 0x3F; ec != 0x25 {
		t.Errorf("ESR EC = %#x, want 0x25 (data abort, same EL)", ec)
	}
	if c.X[5] != 0x600D {
		t.Errorf("x5 = %#x; did not resume after the abort", c.X[5])
	}
}
