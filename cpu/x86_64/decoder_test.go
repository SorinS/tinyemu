package x86_64

import (
	"errors"
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// longModeFlat builds a CPU positioned in 64-bit long mode with paging
// disabled — convenient for decoder tests because the linear address
// IS the physical address and we can stuff hand-assembled bytes at
// any RAM offset and just point RIP at them. (Real hardware does not
// permit CR0.PG=0 in long mode; the emulator allows it for the same
// reason the cpu/x86 tests run with paging off — to test the decoder
// in isolation from the MMU.)
func longModeFlat(t *testing.T, ramSize uint64) (*CPU, *mem.PhysMemoryMap) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, ramSize, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	// Long-mode-active state with paging disabled — the emulator-only
	// configuration that lets us bypass the page walker for decode
	// tests. CR0.PE=1 mirrors the real bring-up state; PG=0 keeps
	// translateForData identity.
	c.SetCR64(0, CR0_PE)
	c.SetCR64(4, CR4_PAE)
	c.SetEFER(EFER_LME | EFER_LMA)
	c.SetSegAccess(CS, csLBit) // CS.L=1 ⇒ 64-bit code
	c.recomputeMode()
	if c.mode != ModeLong64 {
		t.Fatalf("setup: mode=%v want ModeLong64", c.mode)
	}
	return c, mm
}

// loadCode copies prog into the guest RAM at addr and points RIP at it.
func loadCode(t *testing.T, c *CPU, mm *mem.PhysMemoryMap, addr uint64, prog []byte) {
	t.Helper()
	for i, b := range prog {
		if err := mm.Write8(addr+uint64(i), b); err != nil {
			t.Fatalf("Write8: %v", err)
		}
	}
	c.SetRIP(addr)
}

// stepN runs Step n times, failing fast on any error.
func stepN(t *testing.T, c *CPU, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := c.Step(); err != nil {
			t.Fatalf("Step %d: %v", i, err)
		}
	}
}

func TestDecode_NOP(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	loadCode(t, c, mm, 0x100, []byte{0x90}) // NOP
	stepN(t, c, 1)
	if c.GetRIP() != 0x101 {
		t.Errorf("RIP after NOP = %#x, want 0x101", c.GetRIP())
	}
}

func TestDecode_HLT(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	loadCode(t, c, mm, 0x100, []byte{0xF4}) // HLT
	stepN(t, c, 1)
	if !c.IsPowerDown() {
		t.Errorf("HLT did not set powerDown")
	}
	if c.GetRIP() != 0x101 {
		t.Errorf("RIP after HLT = %#x", c.GetRIP())
	}
}

// MOV RAX, imm64 — the canonical REX.W=1 form with 0xB8.
func TestDecode_MOV_RAX_imm64(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	// 48 B8 EF BE AD DE 00 00 00 00   mov rax, 0xDEADBEEF
	loadCode(t, c, mm, 0x100, []byte{
		0x48, 0xB8, 0xEF, 0xBE, 0xAD, 0xDE, 0x00, 0x00, 0x00, 0x00,
	})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0xDEADBEEF {
		t.Errorf("RAX = %#x, want 0xDEADBEEF", got)
	}
	if c.GetRIP() != 0x10A {
		t.Errorf("RIP = %#x, want 0x10A", c.GetRIP())
	}
}

// MOV R8, imm64 — REX.W|REX.B=1 reaches the extended GPR file.
func TestDecode_MOV_R8_imm64(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	// 49 B8 01 02 03 04 05 06 07 08   mov r8, 0x0807060504030201
	loadCode(t, c, mm, 0x100, []byte{
		0x49, 0xB8, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	})
	stepN(t, c, 1)
	if got := c.GetReg64(R8); got != 0x0807060504030201 {
		t.Errorf("R8 = %#x", got)
	}
}

// MOV R15, imm64 — verifies the high-index path.
func TestDecode_MOV_R15_imm64(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	// 49 BF FF EE DD CC BB AA 99 88   mov r15, 0x8899AABBCCDDEEFF
	loadCode(t, c, mm, 0x100, []byte{
		0x49, 0xBF, 0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88,
	})
	stepN(t, c, 1)
	if got := c.GetReg64(R15); got != 0x8899AABBCCDDEEFF {
		t.Errorf("R15 = %#x", got)
	}
}

// MOV EAX, imm32 (no REX.W) — the 32-bit write zero-extends to 64.
func TestDecode_MOV_EAX_imm32_ZeroExtends(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RAX, 0xFFFFFFFF_FFFFFFFF)
	// B8 DE C0 AD 0B   mov eax, 0x0BADC0DE
	loadCode(t, c, mm, 0x100, []byte{0xB8, 0xDE, 0xC0, 0xAD, 0x0B})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0x0BADC0DE {
		t.Errorf("RAX after MOV EAX, imm32 = %#x, want 0x0BADC0DE (zero-extended)", got)
	}
}

// MOV r/m, r — REX.W 89 D8: mov rax, rbx.
func TestDecode_MOV_RAX_from_RBX(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RBX, 0x1122_3344_5566_7788)
	// 48 89 D8   mov rax, rbx (REX.W, 0x89 Ev Gv, modrm=11 011 000)
	loadCode(t, c, mm, 0x100, []byte{0x48, 0x89, 0xD8})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0x1122_3344_5566_7788 {
		t.Errorf("RAX = %#x", got)
	}
}

// MOV r, r/m — REX.W 8B C3: mov rax, rbx.
func TestDecode_MOV_RAX_from_RBX_Form8B(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RBX, 0xCAFEBABEDEADBEEF)
	// 48 8B C3   mov rax, rbx
	loadCode(t, c, mm, 0x100, []byte{0x48, 0x8B, 0xC3})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0xCAFEBABEDEADBEEF {
		t.Errorf("RAX = %#x", got)
	}
}

// MOV mem, r — write 64 bits via [rbx]. Uses RBX=0x800 as a buffer.
func TestDecode_MOV_Mem_from_Reg(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RBX, 0x800)
	c.SetReg64(RAX, 0xAABB_CCDD_EEFF_0011)
	// 48 89 03   mov [rbx], rax (modrm=00 000 011 ⇒ [rbx])
	loadCode(t, c, mm, 0x100, []byte{0x48, 0x89, 0x03})
	stepN(t, c, 1)
	got, err := mm.Read64(0x800)
	if err != nil {
		t.Fatalf("Read64: %v", err)
	}
	if got != 0xAABB_CCDD_EEFF_0011 {
		t.Errorf("mem[0x800] = %#x", got)
	}
}

// MOV reg, mem — load 64 bits from [rbx].
func TestDecode_MOV_Reg_from_Mem(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RBX, 0x800)
	if err := mm.Write64(0x800, 0x1234_5678_9ABC_DEF0); err != nil {
		t.Fatal(err)
	}
	// 48 8B 03   mov rax, [rbx]
	loadCode(t, c, mm, 0x100, []byte{0x48, 0x8B, 0x03})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0x1234_5678_9ABC_DEF0 {
		t.Errorf("RAX = %#x", got)
	}
}

// LEA r, m — RIP-relative addressing.
func TestDecode_LEA_RIPRelative(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	// 48 8D 05 10 00 00 00   lea rax, [rip+0x10]
	// The instruction is 7 bytes. RIP after fetch is base+7. EA = RIP+0x10 = base+0x17.
	const base = uint64(0x100)
	loadCode(t, c, mm, base, []byte{
		0x48, 0x8D, 0x05, 0x10, 0x00, 0x00, 0x00,
	})
	stepN(t, c, 1)
	want := base + 7 + 0x10
	if got := c.GetReg64(RAX); got != want {
		t.Errorf("RAX = %#x, want %#x", got, want)
	}
}

// LEA r, m — RIP-relative with a negative displacement.
func TestDecode_LEA_RIPRelative_Negative(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	// 48 8D 05 F0 FF FF FF   lea rax, [rip-0x10]
	const base = uint64(0x200)
	loadCode(t, c, mm, base, []byte{
		0x48, 0x8D, 0x05, 0xF0, 0xFF, 0xFF, 0xFF,
	})
	stepN(t, c, 1)
	want := base + 7 - 0x10
	if got := c.GetReg64(RAX); got != want {
		t.Errorf("RAX = %#x, want %#x", got, want)
	}
}

// ADD r/m, r — flag bits after a 64-bit add. Tests CF, ZF, SF, OF.
func TestDecode_ADD_64_Flags(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RAX, 0xFFFFFFFF_FFFFFFFE)
	c.SetReg64(RBX, 0x0000_0000_0000_0002)
	// 48 01 D8   add rax, rbx
	loadCode(t, c, mm, 0x100, []byte{0x48, 0x01, 0xD8})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0 {
		t.Errorf("RAX = %#x, want 0", got)
	}
	if c.rflags&RFLAGS_CF == 0 {
		t.Errorf("CF not set on unsigned overflow")
	}
	if c.rflags&RFLAGS_ZF == 0 {
		t.Errorf("ZF not set when result is 0")
	}
	if c.rflags&RFLAGS_SF != 0 {
		t.Errorf("SF should be clear when result is 0")
	}
}

func TestDecode_SUB_64(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RAX, 0x100)
	c.SetReg64(RBX, 0x1)
	// 48 29 D8   sub rax, rbx
	loadCode(t, c, mm, 0x100, []byte{0x48, 0x29, 0xD8})
	stepN(t, c, 1)
	if got := c.GetReg64(RAX); got != 0xFF {
		t.Errorf("RAX = %#x, want 0xFF", got)
	}
	if c.rflags&RFLAGS_CF != 0 {
		t.Errorf("CF should be clear (no underflow)")
	}
}

// PUSH r64 / POP r64 — round-trip through the stack.
func TestDecode_PushPop_64(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RSP, 0x1000)
	c.SetReg64(RAX, 0xDEAD_BEEF_CAFE_F00D)
	c.SetReg64(RBX, 0)
	// 50         push rax
	// 5B         pop rbx
	loadCode(t, c, mm, 0x100, []byte{0x50, 0x5B})
	stepN(t, c, 2)
	if got := c.GetReg64(RBX); got != 0xDEAD_BEEF_CAFE_F00D {
		t.Errorf("RBX after push/pop = %#x", got)
	}
	if c.GetReg64(RSP) != 0x1000 {
		t.Errorf("RSP not restored after round-trip: %#x", c.GetReg64(RSP))
	}
}

// PUSH r64 with REX.B — exercises R8..R15 path through the stack.
func TestDecode_Push_R8(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RSP, 0x1000)
	c.SetReg64(R8, 0x123456789ABCDEF0)
	// 41 50    push r8
	loadCode(t, c, mm, 0x100, []byte{0x41, 0x50})
	stepN(t, c, 1)
	got, _ := mm.Read64(0x1000 - 8)
	if got != 0x123456789ABCDEF0 {
		t.Errorf("stack top = %#x", got)
	}
}

// CALL rel32 / RET — push return, jump; later RET pops it.
func TestDecode_Call_Ret(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	c.SetReg64(RSP, 0x1000)
	// At 0x100:
	//   E8 05 00 00 00     call rel32 → goes to 0x10A
	//   90                 nop (skipped)
	//   90                 nop
	//   90                 nop
	//   90                 nop
	//   90                 nop                  ; 0x10A
	//   C3                 ret                  ; 0x10A actually — wait
	// Let me recount: call is 5 bytes. After fetch RIP=0x105. disp=5 ⇒ target 0x10A.
	loadCode(t, c, mm, 0x100, []byte{
		0xE8, 0x05, 0x00, 0x00, 0x00, // call rel32=+5 (target 0x10A)
		0x90, 0x90, 0x90, 0x90, 0x90, // padding
		0xC3, // ret at 0x10A
	})
	// step 1: CALL
	stepN(t, c, 1)
	if c.GetRIP() != 0x10A {
		t.Errorf("RIP after CALL = %#x, want 0x10A", c.GetRIP())
	}
	if c.GetReg64(RSP) != 0x1000-8 {
		t.Errorf("RSP after CALL = %#x", c.GetReg64(RSP))
	}
	// step 2: RET
	stepN(t, c, 1)
	if c.GetRIP() != 0x105 {
		t.Errorf("RIP after RET = %#x, want 0x105 (instruction after CALL)", c.GetRIP())
	}
	if c.GetReg64(RSP) != 0x1000 {
		t.Errorf("RSP after RET = %#x, want 0x1000", c.GetReg64(RSP))
	}
}

// JMP rel8 and rel32 short-range hops.
func TestDecode_JMP_rel(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	// EB 02       jmp +2
	// 90          nop  (skipped)
	// 90          nop  (skipped)
	// F4          hlt
	loadCode(t, c, mm, 0x100, []byte{0xEB, 0x02, 0x90, 0x90, 0xF4})
	stepN(t, c, 2) // jmp + hlt
	if !c.IsPowerDown() {
		t.Errorf("expected HLT to land")
	}
	if c.GetRIP() != 0x105 {
		t.Errorf("RIP = %#x, want 0x105", c.GetRIP())
	}
}

// Multi-instruction program: mov rax, imm; mov rbx, rax; hlt.
func TestDecode_Program_MovHlt(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	loadCode(t, c, mm, 0x100, []byte{
		0x48, 0xB8, 0x42, 0, 0, 0, 0, 0, 0, 0, // mov rax, 0x42
		0x48, 0x89, 0xC3, // mov rbx, rax
		0xF4, // hlt
	})
	if err := c.Run(100); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !c.IsPowerDown() {
		t.Errorf("Run should have ended in HLT")
	}
	if c.GetReg64(RAX) != 0x42 {
		t.Errorf("RAX = %#x", c.GetReg64(RAX))
	}
	if c.GetReg64(RBX) != 0x42 {
		t.Errorf("RBX = %#x", c.GetReg64(RBX))
	}
}

// Unimplemented opcode surfaces as ErrNotImplemented via errors.Is so
// the Run loop fails loudly instead of spinning silently on garbage.
func TestDecode_Unimplemented(t *testing.T) {
	c, mm := longModeFlat(t, 4096)
	loadCode(t, c, mm, 0x100, []byte{0xCC}) // INT3 — not implemented yet
	err := c.Step()
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Step(0xCC) = %v, want wrap-of ErrNotImplemented", err)
	}
}

// Fetch under paging with no PML4 mapping surfaces as a PageFaultError
// from Step (Phase 5 will route this through the IDT as #PF).
func TestDecode_FetchPagedNotPresent(t *testing.T) {
	c, _ := pagedCPU(t, 0x20_0000)
	// pagedCPU enables CR0.PG + EFER.LMA but doesn't put the CPU in
	// 64-bit long mode (it's a data-translation harness). For an
	// instruction fetch to go through Translate we need ModeLong64 so
	// lip() doesn't fall back to segBase[CS]+RIP.
	c.SetSegAccess(CS, csLBit)
	c.recomputeMode()
	if c.mode != ModeLong64 {
		t.Fatalf("setup: mode=%v", c.mode)
	}
	c.SetRIP(0x40_0000)
	err := c.Step()
	if err == nil {
		t.Fatalf("expected PF on instruction fetch")
	}
	var pfe *PageFaultError
	if !errors.As(err, &pfe) {
		t.Fatalf("err type = %T, want *PageFaultError", err)
	}
	if pfe.ErrorCode&PFErrFetch == 0 {
		t.Errorf("ErrorCode=%#x, expected PFErrFetch", pfe.ErrorCode)
	}
	// RIP must be rewound to the start of the faulting instruction so
	// the eventual #PF handler restarts cleanly.
	if c.GetRIP() != 0x40_0000 {
		t.Errorf("RIP=%#x after PF, want unchanged (0x40_0000)", c.GetRIP())
	}
}
