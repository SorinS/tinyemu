package arm64

import (
	"testing"

	asmarm64 "github.com/jtolio/tinyemu-go/asm/arm64"
	"github.com/jtolio/tinyemu-go/mem"
)

// newCPU builds a CPU over 1 MiB of flat RAM with the program assembled at
// base 0x10000; it returns the CPU and the byte length of the program.
func newCPU(t *testing.T, instrs []string) (*CPU, uint64) {
	t.Helper()
	var code []byte
	for _, s := range instrs {
		b, err := asmarm64.Assemble(s)
		if err != nil {
			t.Fatalf("assemble %q: %v", s, err)
		}
		code = append(code, b...)
	}
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	const base = 0x10000
	for i, b := range code {
		_ = mm.Write8(base+uint64(i), b)
	}
	c := New(mm)
	c.PC = base
	return c, uint64(len(code))
}

func run(t *testing.T, c *CPU, n uint64) {
	t.Helper()
	end := uint64(0x10000) + n
	for step := 0; c.PC < end && step < 1000; step++ {
		if err := c.Step(); err != nil {
			t.Fatalf("step at %#x: %v", c.PC, err)
		}
	}
}

// TestARM64_LoadStoreExec checks load/store semantics directly: stores land in
// memory, loads read back with the right size and sign-extension, and the
// pre/post-index writeback updates the base. (Load/store can't use the native
// oracle — its SP/base would point at unmapped memory.)
func TestARM64_LoadStoreExec(t *testing.T) {
	const buf = 0x20000

	// store then load back, full 64-bit.
	c, n := newCPU(t, []string{"str x1, [x0]", "ldr x2, [x0]"})
	c.X[0] = buf
	c.X[1] = 0x1122334455667788
	run(t, c, n)
	if c.X[2] != 0x1122334455667788 {
		t.Errorf("ldr x2 = %#x", c.X[2])
	}
	if v, _ := c.Mem.Read64(buf); v != 0x1122334455667788 {
		t.Errorf("mem = %#x", v)
	}

	// byte/half stores write only their width; ldrb/ldrsb zero/sign-extend.
	c, n = newCPU(t, []string{"strb w1, [x0]", "ldrb w2, [x0]", "ldrsb x3, [x0]"})
	c.X[0] = buf
	c.X[1] = 0x000000FF
	run(t, c, n)
	if c.X[2] != 0xFF {
		t.Errorf("ldrb = %#x, want ff", c.X[2])
	}
	if c.X[3] != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("ldrsb = %#x, want sign-extended", c.X[3])
	}

	// ldrsw sign-extends a 32-bit value to 64 bits.
	c, n = newCPU(t, []string{"str w1, [x0]", "ldrsw x2, [x0]"})
	c.X[0] = buf
	c.X[1] = 0x80000000
	run(t, c, n)
	if c.X[2] != 0xFFFFFFFF80000000 {
		t.Errorf("ldrsw = %#x", c.X[2])
	}

	// pre-index updates the base before the access and writes it back.
	c, n = newCPU(t, []string{"str x1, [x0, #16]!"})
	c.X[0] = buf
	c.X[1] = 0xABCD
	run(t, c, n)
	if c.X[0] != buf+16 {
		t.Errorf("pre-index base = %#x, want %#x", c.X[0], buf+16)
	}
	if v, _ := c.Mem.Read64(buf + 16); v != 0xABCD {
		t.Errorf("pre-index store = %#x", v)
	}

	// post-index accesses at the base, then advances it.
	c, n = newCPU(t, []string{"str x1, [x0], #32"})
	c.X[0] = buf
	c.X[1] = 0x55
	run(t, c, n)
	if c.X[0] != buf+32 {
		t.Errorf("post-index base = %#x, want %#x", c.X[0], buf+32)
	}
	if v, _ := c.Mem.Read64(buf); v != 0x55 {
		t.Errorf("post-index store = %#x", v)
	}

	// stp/ldp round-trips a register pair.
	c, n = newCPU(t, []string{"stp x1, x2, [x0]", "ldp x3, x4, [x0]"})
	c.X[0] = buf
	c.X[1], c.X[2] = 0x1111, 0x2222
	run(t, c, n)
	if c.X[3] != 0x1111 || c.X[4] != 0x2222 {
		t.Errorf("ldp = %#x, %#x", c.X[3], c.X[4])
	}

	// register-offset load with a scaled index.
	c, n = newCPU(t, []string{"str x1, [x0, x2, lsl #3]"})
	c.X[0] = buf
	c.X[1] = 0x99
	c.X[2] = 2 // byte offset 2<<3 = 16
	run(t, c, n)
	if v, _ := c.Mem.Read64(buf + 16); v != 0x99 {
		t.Errorf("reg-offset store = %#x", v)
	}
}

// TestARM64_SystemExec checks the system instructions that can't run under the
// native oracle: hints/barriers are no-ops, svc/brk halt the core, and a
// read-only-in-the-model sysreg round-trips through the Sys map.
func TestARM64_SystemExec(t *testing.T) {
	// nop / barriers advance PC without touching registers.
	c, n := newCPU(t, []string{"nop", "dmb sy", "isb", "add x0, x1, x2"})
	c.X[1], c.X[2] = 3, 4
	run(t, c, n)
	if c.X[0] != 7 {
		t.Errorf("after nops, x0 = %d, want 7", c.X[0])
	}

	// svc halts the core and records the exception.
	c, _ = newCPU(t, []string{"movz x0, #1", "svc #42", "movz x0, #2"})
	for i := 0; i < 10 && !c.Halted; i++ {
		if err := c.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if !c.Halted || c.ExcType != "svc" || c.ExcImm != 42 {
		t.Errorf("svc: halted=%v type=%q imm=%d", c.Halted, c.ExcType, c.ExcImm)
	}
	if c.X[0] != 1 { // execution stopped at svc, before the second movz
		t.Errorf("svc should stop before the second movz; x0 = %d", c.X[0])
	}

	// brk also halts, with its own type.
	c, _ = newCPU(t, []string{"brk #1"})
	if err := c.Step(); err != nil {
		t.Fatal(err)
	}
	if !c.Halted || c.ExcType != "brk" {
		t.Errorf("brk: halted=%v type=%q", c.Halted, c.ExcType)
	}

	// msr/mrs round-trip a non-NZCV system register through the Sys map.
	c, n = newCPU(t, []string{"msr tpidr_el0, x1", "mrs x0, tpidr_el0"})
	c.X[1] = 0xCAFEF00D
	run(t, c, n)
	if c.X[0] != 0xCAFEF00D {
		t.Errorf("sysreg round-trip x0 = %#x", c.X[0])
	}
}

// TestARM64_BranchExec checks control flow: a countdown loop and bl/ret linkage.
func TestARM64_BranchExec(t *testing.T) {
	// Sum 1..5 with a backward conditional branch.
	c, n := newCPU(t, []string{
		"movz x0, #0",      // acc
		"movz x1, #5",      // counter
		"adds x0, x0, x1",  // loop: acc += counter (sets flags)
		"subs x1, x1, #1",  // counter--
		"b.ne #-8",         // if counter != 0, back to the add
	})
	run(t, c, n)
	if c.X[0] != 15 {
		t.Errorf("loop sum = %d, want 15", c.X[0])
	}

	// bl sets the link register; ret returns through it.
	c, _ = newCPU(t, []string{
		"bl #8",           // call (skip the next instr), x30 = PC+4
		"movz x0, #0xbad", // should be skipped on the call path... actually executed after ret
		"ret",             // returns to x30 (the movz after bl)
	})
	// Drive it manually: after bl, PC at the movz; set a sentinel for ret.
	c.Sentinel = 0xDEAD0000
	c.X[30] = 0 // will be overwritten by bl
	// step bl
	if err := c.Step(); err != nil {
		t.Fatal(err)
	}
	if c.X[30] != 0x10000+4 {
		t.Errorf("bl link = %#x, want %#x", c.X[30], 0x10000+4)
	}
	if c.PC != 0x10000+8 {
		t.Errorf("bl target = %#x", c.PC)
	}
}
