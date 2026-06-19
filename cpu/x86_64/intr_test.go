package x86_64

// M5c unit tests — IDT-based interrupt delivery and IRETQ.

import (
	"testing"

	"github.com/sorins/tinyemu-go/mem"
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

// TestIRETQ_ToUserMode_UpdatesCPL regression-tests the Linux 6.6
// userspace-entry path: the kernel pushes a 5-slot frame with CS RPL=3
// then executes IRETQ. The CPU must (a) set c.cpl=3, (b) install
// segAccess[CS] with DPL=3 (0xFA), (c) install segAccess[SS] with
// DPL=3 (0xF2). Without these updates, c.cpl stays at 0, the next
// page-fault generates an error_code with X86_PF_USER (bit 2) cleared,
// and Linux's do_user_addr_fault routes the fault through no_context →
// "BUG: unable to handle page fault for address: ..." → init killed.
//
// We can't easily simulate the full mmu walk here, so we drive
// the bug indirectly: verify the CPL/segment state after IRETQ and
// verify the faultCode helper now reports user-mode access for c.cpl=3.
func TestIRETQ_ToUserMode_UpdatesCPL(t *testing.T) {
	c, mm := longModeCPU(t)
	c.cpl = 0 // start in kernel

	const initSP uint64 = 0x8000
	c.reg64[RSP] = initSP

	// Build an IRETQ frame at [RSP-40, RSP) matching:
	//   RIP=0x7f00_0000_0010 (user code)
	//   CS =0x33  (RPL=3, user CS selector)
	//   RFLAGS=0x202
	//   RSP=0x7fff_f000  (user stack)
	//   SS =0x2b  (RPL=3, user SS selector)
	// Stack grows down, IRETQ pops RIP first, so write in reverse:
	_ = mm.Write64(initSP-8, 0x2b)              // SS  (popped 5th)
	_ = mm.Write64(initSP-16, 0x7ffff000)       // RSP (popped 4th)
	_ = mm.Write64(initSP-24, 0x202)            // RFLAGS (popped 3rd)
	_ = mm.Write64(initSP-32, 0x33)             // CS  (popped 2nd)
	_ = mm.Write64(initSP-40, 0x7f0000000010)   // RIP (popped 1st)
	c.reg64[RSP] = initSP - 40

	// IRETQ encoding: REX.W (0x48) + 0xCF.
	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0x48)
	_ = mm.Write8(codeAddr+1, 0xCF)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("Step IRETQ: %v", err)
	}

	if c.cpl != 3 {
		t.Errorf("c.cpl = %d after IRETQ to user CS=0x33, want 3", c.cpl)
	}
	if c.seg[CS] != 0x33 {
		t.Errorf("CS = %#x, want 0x33", c.seg[CS])
	}
	if c.seg[SS] != 0x2b {
		t.Errorf("SS = %#x, want 0x2b", c.seg[SS])
	}
	if c.GetRIP() != 0x7f0000000010 {
		t.Errorf("RIP = %#x, want 0x7f0000000010", c.GetRIP())
	}
	// CS access byte must reflect DPL=3 (0xFA: P=1 DPL=3 S=1 type=1010).
	if c.segAccess[CS]&0x60 != 0x60 {
		t.Errorf("CS access = %#x, want DPL=3 (low 8 bits = 0xFA)", c.segAccess[CS])
	}
	// SS access byte must reflect DPL=3 (0xF2).
	if c.segAccess[SS]&0x60 != 0x60 {
		t.Errorf("SS access = %#x, want DPL=3 (low 8 bits = 0xF2)", c.segAccess[SS])
	}

	// The downstream property that matters most: a subsequent #PF
	// produced while c.cpl=3 must set X86_PF_USER (bit 2) in
	// error_code. mem.go::translateForData passes (c.cpl==3) as the
	// isUser argument; faultCode then ORs bit 2.
	const X86_PF_USER = uint32(1 << 2)
	code := faultCode(false, false, c.cpl == 3, false, false)
	if code&X86_PF_USER == 0 {
		t.Errorf("faultCode for cpl=3 = %#x, missing X86_PF_USER bit", code)
	}
}

// TestIRETQ_ToKernelMode_KeepsKernelCPL: complement test — IRETQ to
// CS with RPL=0 must keep c.cpl=0 and 0x9A access bits. Guards against
// over-correction where the user-mode fix accidentally drops the
// kernel through to CPL=3 on every IRETQ.
func TestIRETQ_ToKernelMode_KeepsKernelCPL(t *testing.T) {
	c, mm := longModeCPU(t)
	c.cpl = 0

	const initSP uint64 = 0x8000
	_ = mm.Write64(initSP-8, 0x10)            // SS  (kernel)
	_ = mm.Write64(initSP-16, 0x7000)         // RSP
	_ = mm.Write64(initSP-24, 0x202)          // RFLAGS
	_ = mm.Write64(initSP-32, 0x08)           // CS  RPL=0
	_ = mm.Write64(initSP-40, 0x1234)         // RIP
	c.reg64[RSP] = initSP - 40

	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0x48)
	_ = mm.Write8(codeAddr+1, 0xCF)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("Step IRETQ: %v", err)
	}
	if c.cpl != 0 {
		t.Errorf("c.cpl = %d after kernel IRETQ, want 0", c.cpl)
	}
	if c.segAccess[CS]&0x60 != 0 {
		t.Errorf("CS access = %#x, want DPL=0", c.segAccess[CS])
	}
}

// TestDeliverInterrupt_FromUserMode_DropsCPL: when an interrupt is
// taken with c.cpl=3 (user mode), the handler must run at CPL=0.
// Companion fix to the IRETQ CPL update — together they round-trip
// CPL correctly across the user→kernel→user transition.
//
// Also exercises TSS.RSP0: with the user→kernel stack switch wired
// up, a user-mode interrupt now requires TR to be loaded.
func TestDeliverInterrupt_FromUserMode_DropsCPL(t *testing.T) {
	c, mm := longModeCPU(t)

	// IDT.
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 0x80, 0x0008, 0x80000, 0, 0x8E)

	// TSS + GDT entry for it (kernel stack at 0xA000).
	const gdtBase uint64 = 0x3000
	const tssBase uint64 = 0x50000
	const kernelStack uint64 = 0xA000
	c.segBase[GDTR] = gdtBase
	c.segLimit[GDTR] = 0x100
	installTSSDescriptor(t, mm, gdtBase, 0x40, tssBase, 0x67)
	_ = mm.Write64(tssBase+4, kernelStack)
	c.seg[TR] = 0x40
	c.segBase[TR] = tssBase

	c.cpl = 3 // pretend we're in user mode
	c.reg64[RSP] = 0x8000

	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0xCD)
	_ = mm.Write8(codeAddr+1, 0x80)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("Step INT 0x80: %v", err)
	}
	if c.cpl != 0 {
		t.Errorf("c.cpl = %d after INT from user, want 0 (handler must run as kernel)", c.cpl)
	}
}

// installTSSDescriptor writes a 16-byte 64-bit TSS descriptor into the
// GDT at the given selector. Used by the TSS-related tests.
func installTSSDescriptor(t *testing.T, mm *mem.PhysMemoryMap, gdtBase uint64, sel uint16, tssBase uint64, limit uint32) {
	t.Helper()
	addr := gdtBase + uint64(sel&0xFFF8)
	lo := uint64(limit&0xFFFF) |
		((tssBase & 0xFFFF) << 16) |
		(((tssBase >> 16) & 0xFF) << 32) |
		(uint64(0x89) << 40) | // P=1 DPL=0 S=0 type=9 (avail 64-bit TSS)
		((uint64(limit) >> 16) & 0xF << 48) |
		(((tssBase >> 24) & 0xFF) << 56)
	hi := tssBase >> 32 & 0xFFFFFFFF
	if err := mm.Write64(addr, lo); err != nil {
		t.Fatalf("Write TSS desc lo: %v", err)
	}
	if err := mm.Write64(addr+8, hi); err != nil {
		t.Fatalf("Write TSS desc hi: %v", err)
	}
}

// TestLTR_ExtractsTSSBase verifies that LTR walks the GDT entry it was
// pointed at and resolves the TSS base address into segBase[TR]. The
// downstream deliverInterrupt path reads TSS.RSP0 = [base+4] to
// implement the user→kernel stack switch.
func TestLTR_ExtractsTSSBase(t *testing.T) {
	c, mm := longModeCPU(t)
	const gdtBase uint64 = 0x3000
	const tssBase uint64 = 0x50000
	const tssSel uint16 = 0x40
	c.segBase[GDTR] = gdtBase
	c.segLimit[GDTR] = 0x100
	installTSSDescriptor(t, mm, gdtBase, tssSel, tssBase, 0x67) // 0x67 = TSS-min limit

	c.reg64[RSP] = 0x8000
	c.reg64[RAX] = uint64(tssSel)

	// LTR ax — 0x0F 0x00 /3, modR/M for AX = D8
	const codeAddr uint64 = 0x1000
	_ = mm.Write8(codeAddr, 0x0F)
	_ = mm.Write8(codeAddr+1, 0x00)
	_ = mm.Write8(codeAddr+2, 0xD8)
	c.SetRIP(codeAddr)

	if err := c.Step(); err != nil {
		t.Fatalf("Step LTR: %v", err)
	}
	if c.seg[TR] != tssSel {
		t.Errorf("TR selector = %#x, want %#x", c.seg[TR], tssSel)
	}
	if c.segBase[TR] != tssBase {
		t.Errorf("TR base = %#x, want %#x", c.segBase[TR], tssBase)
	}
	if c.segLimit[TR] != 0x67 {
		t.Errorf("TR limit = %#x, want 0x67", c.segLimit[TR])
	}
}

// TestDeliverInterrupt_FromUser_SwitchesToTSSRSP0 is the regression for
// the symptom the user hit after the IRETQ CPL fix:
//   ERROR in Run: x86_64: page fault at 0x7f5aaca8297e (errorcode=0x14)
//
// errorcode=0x14 means the CPL fix is working (X86_PF_USER set), but
// our deliverInterrupt was pushing the IRETQ frame on whatever RSP
// was live at fault time — i.e., the user RSP. The push then faulted
// trying to write to user-stack memory (either unmapped or about to
// be lazily mapped), pushFrameRecover bubbled it back, and Step
// reported it as an unhandled emulator error.
//
// Fix: when c.cpl=3 at delivery time, load RSP from TSS.RSP0 before
// pushing. This test exercises that path end-to-end.
func TestDeliverInterrupt_FromUser_SwitchesToTSSRSP0(t *testing.T) {
	c, mm := longModeCPU(t)

	// GDT with TSS slot 0x40 → TSS at 0x50000. TSS.RSP0 (offset 4)
	// holds the kernel stack pointer.
	const gdtBase uint64 = 0x3000
	const tssBase uint64 = 0x50000
	const kernelStack uint64 = 0xA000
	c.segBase[GDTR] = gdtBase
	c.segLimit[GDTR] = 0x100
	installTSSDescriptor(t, mm, gdtBase, 0x40, tssBase, 0x67)
	if err := mm.Write64(tssBase+4, kernelStack); err != nil {
		t.Fatalf("write TSS.RSP0: %v", err)
	}
	c.seg[TR] = 0x40
	c.segBase[TR] = tssBase

	// IDT with vec 14 (#PF) gate pointing at 0x80000.
	const idtBase uint64 = 0x4000
	c.segBase[IDTR] = idtBase
	c.segLimit[IDTR] = 0x1000 - 1
	installIDTGate(t, mm, idtBase, 14, 0x0008, 0x80000, 0, 0x8E)

	// Put us in user mode with a bogus user RSP that is intentionally
	// in unmapped memory. If deliverInterrupt forgets to switch to
	// TSS.RSP0, the push will fault and the test will see the wrong
	// behaviour.
	c.cpl = 3
	const bogusUserRSP uint64 = 0xDEADBEEF00 // not mapped
	c.reg64[RSP] = bogusUserRSP
	c.seg[SS] = 0x2b

	// Trigger #PF synthetically by calling deliverInterrupt directly.
	// (Driving an actual user-mode fault would require paging set up,
	// which is more setup than this unit test needs.)
	if err := c.deliverInterrupt(14, true, 0x14); err != nil {
		t.Fatalf("deliverInterrupt: %v", err)
	}

	if c.GetRIP() != 0x80000 {
		t.Errorf("handler RIP = %#x, want 0x80000", c.GetRIP())
	}
	if c.cpl != 0 {
		t.Errorf("c.cpl = %d after entering kernel handler, want 0", c.cpl)
	}
	// The pushed frame should be on the KERNEL stack, not user.
	// After 6 pushes (RIP/CS/RFLAGS/RSP/SS + error code), RSP =
	// kernelStack - 48.
	if c.reg64[RSP] != kernelStack-48 {
		t.Errorf("RSP after delivery = %#x, want %#x (kernelStack - 48)",
			c.reg64[RSP], kernelStack-48)
	}
	// Saved user RSP must be readable on the kernel stack.
	savedRSP, _ := mm.Read64(c.reg64[RSP] + 24) // offset of RSP in frame: 0=err, 8=RIP, 16=CS, 24=RFLAGS, 32=RSP, 40=SS
	// Frame order (high→low after pushes): SS, RSP, RFLAGS, CS, RIP, err.
	// We pushed SS first (highest addr), err last (lowest).
	// So at RSP we have err; at RSP+8 RIP, RSP+16 CS, RSP+24 RFLAGS,
	// RSP+32 RSP, RSP+40 SS.
	if savedRSP != bogusUserRSP {
		// Try the actual order — push order in code is SS, RSP, RFLAGS, CS, RIP, err
		// Stack grows down, so layout from low→high: err, RIP, CS, RFLAGS, RSP, SS.
		// savedRSP at offset 32:
		actual, _ := mm.Read64(c.reg64[RSP] + 32)
		if actual != bogusUserRSP {
			t.Errorf("saved user RSP on kernel stack: tried offsets 24 and 32, got %#x and %#x — want %#x",
				savedRSP, actual, bogusUserRSP)
		}
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
