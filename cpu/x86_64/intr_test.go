package x86_64

// M5c unit tests — IDT-based interrupt delivery and IRETQ.

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// installIDTGate writes a 16-byte interrupt gate into the IDT for the
// given vector. selector goes into the CS field; rip is the 64-bit
// handler offset; ist is the IST index (0 = no stack switch); typeP
// is the type/DPL/P byte (0x8E = present, DPL=0, 64-bit interrupt
// gate; 0x8F = trap gate).
func installIDTGate(t *testing.T, mm *mem.PhysMemoryMap, idtBase uint64, vec uint8, selector uint16, rip uint64, ist, typeP uint8) {
	t.Helper()
	addr := idtBase + uint64(vec)*16
	lo := uint64(rip&0xFFFF) |
		(uint64(selector) << 16) |
		(uint64(ist) << 32) |
		(uint64(typeP) << 40) |
		((rip & 0xFFFF0000) << 32)
	hi := (rip >> 32) & 0xFFFFFFFF
	if err := mm.Write64(addr, lo); err != nil {
		t.Fatalf("Write64 IDT low: %v", err)
	}
	if err := mm.Write64(addr+8, hi); err != nil {
		t.Fatalf("Write64 IDT high: %v", err)
	}
}

// longModeCPU is a small builder used across M5c tests.
func longModeCPU(t *testing.T) (*CPU, *mem.PhysMemoryMap) {
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
	return c, mm
}

// TestINT80_DeliveredThroughIDT: INT 0x80 jumps to the gate's handler
// with the standard 64-bit frame pushed.
func TestINT80_DeliveredThroughIDT(t *testing.T) {
	c, mm := longModeCPU(t)

	// Set up IDT at 0x4000 with 256 entries; install a present
	// interrupt gate at vector 0x80 pointing at 0x80000.
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 0x80, 0x0008, 0x80000, 0, 0x8E)

	c.reg64[RSP] = 0x8000

	// Program: just INT 0x80 then HLT (would be unreachable).
	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0xCD)
	_ = mm.Write8(codeAddr+1, 0x80)
	_ = mm.Write8(codeAddr+2, 0xF4)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("Step (INT 0x80): %v", err)
	}
	// Handler RIP should now be 0x80000.
	if c.GetRIP() != 0x80000 {
		t.Errorf("RIP after INT 0x80 = %#x, want 0x80000", c.GetRIP())
	}
	if c.seg[CS] != 0x0008 {
		t.Errorf("CS after INT 0x80 = %#x, want 0x0008", c.seg[CS])
	}
	// IF should be cleared by an interrupt gate.
	if c.rflags&RFLAGS_IF != 0 {
		t.Errorf("IF still set after interrupt-gate delivery")
	}
	// Stack: five words pushed (SS, RSP_old, RFLAGS, CS_old, RIP_old).
	// RIP_old should be the byte after the INT instruction.
	rip, _ := mm.Read64(c.GetReg64(RSP))
	if rip != codeAddr+2 {
		t.Errorf("saved RIP on stack = %#x, want %#x", rip, codeAddr+2)
	}
}

// TestINT_IRETQ_RoundTrip: INT enters a handler, IRETQ returns to the
// instruction after the INT.
func TestINT_IRETQ_RoundTrip(t *testing.T) {
	c, mm := longModeCPU(t)

	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 0x80, 0x0008, 0x80000, 0, 0x8E)

	const initSP uint64 = 0x8000
	c.reg64[RSP] = initSP

	// Caller program at 0x1000:
	//   CD 80        int 0x80
	//   48 C7 C0 11 00 00 00   mov rax, 0x11   ; runs after IRETQ
	//   F4           hlt
	const codeAddr uint64 = 0x1000
	prog := []byte{
		0xCD, 0x80,
		0x48, 0xC7, 0xC0, 0x11, 0x00, 0x00, 0x00,
		0xF4,
	}
	for i, b := range prog {
		_ = mm.Write8(codeAddr+uint64(i), b)
	}

	// Handler at 0x80000: just IRETQ (48 CF).
	_ = mm.Write8(0x80000, 0x48)
	_ = mm.Write8(0x80001, 0xCF)

	c.SetRIP(codeAddr)
	// Step INT (1) + IRETQ (1) + MOV (1) + HLT (1).
	for i := 0; i < 4; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d (RIP=%#x): %v", i, c.GetRIP(), err)
		}
	}
	// After IRETQ + MOV + HLT:
	if c.GetReg64(RAX) != 0x11 {
		t.Errorf("RAX = %#x, want 0x11 (MOV ran after IRETQ)", c.GetReg64(RAX))
	}
	if c.GetReg64(RSP) != initSP {
		t.Errorf("RSP = %#x, want %#x (stack balanced)", c.GetReg64(RSP), initSP)
	}
}

// TestINT3_VectoredAsBP: 0xCC delivers vector 3.
func TestINT3_VectoredAsBP(t *testing.T) {
	c, mm := longModeCPU(t)
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 3, 0x0008, 0x90000, 0, 0x8E)
	c.reg64[RSP] = 0x8000

	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0xCC)
	c.SetRIP(codeAddr)
	if err := c.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if c.GetRIP() != 0x90000 {
		t.Errorf("RIP after INT3 = %#x, want handler 0x90000", c.GetRIP())
	}
}

// TestPF_Delivered: a page-fault during instruction execution gets
// vectored through the IDT (CR2 is set, error-code pushed).
func TestPF_Delivered(t *testing.T) {
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 16*1024*1024, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)

	// Set up identity-mapped paging for the low 2 MiB so the program
	// can execute, then leave the rest unmapped so a load triggers #PF.
	const arena uint64 = 0x100000
	c.SetCR64(0, CR0_PE|CR0_PG)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit)
	c.SetSegBase(CS, 0)
	c.SetCR64(3, arena)
	c.recomputeMode()

	// PML4[0] → PDPT (arena+0x1000); PDPT[0] → PD (arena+0x2000);
	// PD[0] = 2 MiB huge leaf mapping linear 0..2MiB → phys 0..2MiB.
	_ = mm.Write64(arena, (arena+0x1000)|pteP|pteRW)
	_ = mm.Write64(arena+0x1000, (arena+0x2000)|pteP|pteRW)
	_ = mm.Write64(arena+0x2000, 0|pteP|pteRW|ptePS)

	// Build IDT in low memory. Vector 14 (#PF) gate sends to 0x90000,
	// which we map by adding a 2 MiB huge entry — no, 0x90000 is
	// within the first 2 MiB so it's already mapped.
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 14, 0x0008, 0x90000, 0, 0x8E)

	c.reg64[RSP] = 0x8000

	// Program at 0x1000: try to read from a non-mapped address.
	//   48 B8 00 00 00 00 01 00 00 00   mov rax, 0x100000000  (above mapped 2 MiB)
	//   48 8B 18                         mov rbx, [rax]
	//   F4                               hlt
	prog := []byte{
		0x48, 0xB8, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
		0x48, 0x8B, 0x18,
		0xF4,
	}
	for i, b := range prog {
		_ = mm.Write8(0x1000+uint64(i), b)
	}
	// Handler at 0x90000: just HLT for now (we just want to verify
	// CR2 and the saved frame).
	_ = mm.Write8(0x90000, 0xF4)

	c.SetRIP(0x1000)
	// MOV imm (1) + MOV [rax] (1, faults; delivered to handler) + HLT.
	for i := 0; i < 5; i++ {
		if c.IsPowerDown() {
			break
		}
		if err := c.Step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if c.cr[2] != 0x100000000 {
		t.Errorf("CR2 = %#x after #PF, want 0x100000000", c.cr[2])
	}
	if c.GetRIP() != 0x90001 { // handler was 1 byte HLT
		t.Errorf("RIP = %#x, want past HLT in handler", c.GetRIP())
	}
	if !c.IsPowerDown() {
		t.Errorf("handler's HLT didn't execute")
	}
}

// TestRETF: 0x48 0xCB pops RIP+CS at 64-bit width. Regression for
// the TinyCorePure64 startup_64 path that does a far-return to land
// in its newly-loaded GDT's kernel CS.
func TestRETF(t *testing.T) {
	c, mm := longModeCPU(t)
	// RETF pops RIP first (at RSP) then CS (at RSP+8).
	_ = mm.Write64(0x7FF0, 0x5A5A5A5A5A5A5A5A) // RIP (popped first)
	_ = mm.Write64(0x7FF8, 0x0008)             // CS  (popped second)
	c.reg64[RSP] = 0x7FF0

	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0x48)
	_ = mm.Write8(codeAddr+1, 0xCB)
	_ = mm.Write8(codeAddr+2, 0xF4)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("step RETF: %v", err)
	}
	if c.GetRIP() != 0x5A5A5A5A5A5A5A5A {
		t.Errorf("RIP after RETF = %#x, want 0x5A5A...", c.GetRIP())
	}
	if c.seg[CS] != 0x0008 {
		t.Errorf("CS = %#x, want 0x0008", c.seg[CS])
	}
	// Stack consumed two 8-byte slots.
	if c.GetReg64(RSP) != 0x8000 {
		t.Errorf("RSP = %#x after far return, want %#x", c.GetReg64(RSP), uint64(0x8000))
	}
	// CS access cache synthesised as long-mode code.
	if c.segAccess[CS]&csLBit == 0 {
		t.Errorf("CS access L bit clear after RETF")
	}
}

// TestUD2_Vectored: 0x0F 0x0B delivers vector 6 (#UD).
func TestUD2_Vectored(t *testing.T) {
	c, mm := longModeCPU(t)
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 6, 0x0008, 0xA0000, 0, 0x8E)
	c.reg64[RSP] = 0x8000

	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0x0F)
	_ = mm.Write8(codeAddr+1, 0x0B)
	c.SetRIP(codeAddr)
	if err := c.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if c.GetRIP() != 0xA0000 {
		t.Errorf("RIP after UD2 = %#x, want #UD handler 0xA0000", c.GetRIP())
	}
}
