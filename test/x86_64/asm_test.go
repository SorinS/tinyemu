// Package x86_64_test runs NASM-assembled 64-bit programs through the
// cpu/x86_64 long-mode emulator. The harness starts the CPU directly in
// 64-bit long mode (CR0.PE|CR4.PAE|EFER.LME|EFER.LMA, CS.L=1) with
// paging disabled — the same shape as cpu/x86_64's longModeFlat test
// helper. This isolates the decoder + ALU from the page walker.
//
// Pattern mirrors test/x86/asm_test.go for the i386 backend, so reading
// the two side by side highlights what's specifically long-mode about
// each program.
package x86_64_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86_64"
	"github.com/sorins/tinyemu-go/mem"
)

// codeBase is the physical+linear address where every assembled
// program lands. Above the conventional-memory hole but well within
// the harness's 16 MiB RAM.
const codeBase uint64 = 0x10000

type asmRunner struct {
	cpu *x86_64.CPU
	mm  *mem.PhysMemoryMap
}

func newAsmRunner(t *testing.T) *asmRunner {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 16*1024*1024, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := x86_64.NewCPU(mm)
	// Long-mode-active state, paging off (CR0.PG=0 so linear == physical
	// — the page walker is exercised separately by mmu_test.go). This is
	// not a legal real-hardware configuration but it's a clean decode-
	// only harness.
	c.SetCR64(0, x86_64.CR0_PE)
	c.SetCR64(4, x86_64.CR4_PAE)
	c.SetEFER(x86_64.EFER_LME | x86_64.EFER_LMA)
	c.SetSegAccess(x86_64.CS, 1<<9) // csLBit ⇒ 64-bit code
	// The Mode enum's recompute method is package-private. The test
	// observes long-mode behaviour anyway because Step's prefix loop
	// hardcodes operandSize=32 (the long-mode default) and lip() uses
	// segBase[CS]+RIP — so as long as segBase[CS] is zero, fetches
	// land on RIP regardless of what mode the CPU thinks it is in.
	c.SetSegBase(x86_64.CS, 0)
	return &asmRunner{cpu: c, mm: mm}
}

// assemble runs nasm on `src` (a 64-bit flat binary source) and
// returns the bytes.
func assemble(t *testing.T, src string) []byte {
	t.Helper()
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "test.asm")
	binPath := filepath.Join(tmpDir, "test.bin")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("write asm: %v", err)
	}
	cmd := exec.Command("nasm", "-f", "bin", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("nasm not available or failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	return data
}

// load writes the assembled bytes into physical memory at `addr` and
// points RIP at it.
func (r *asmRunner) load(t *testing.T, addr uint64, bin []byte) {
	t.Helper()
	for i, b := range bin {
		if err := r.mm.Write8(addr+uint64(i), b); err != nil {
			t.Fatalf("Write8 %#x: %v", addr+uint64(i), err)
		}
	}
	r.cpu.SetRIP(addr)
}

// run steps until HLT, error, or maxSteps is exceeded.
func (r *asmRunner) run(t *testing.T, maxSteps int) error {
	t.Helper()
	for i := 0; i < maxSteps; i++ {
		if r.cpu.IsPowerDown() {
			return nil
		}
		if err := r.cpu.Step(); err != nil {
			return fmt.Errorf("step %d (RIP=%#x): %w", i, r.cpu.GetRIP(), err)
		}
	}
	return fmt.Errorf("did not halt within %d steps", maxSteps)
}

// TestAsm_MovImm64 — REX.W mov r64,imm64 lands the full 64-bit value.
func TestAsm_MovImm64(t *testing.T) {
	src := `
bits 64
	mov rax, 0xDEADBEEFCAFEBABE
	mov rbx, 0x0123456789ABCDEF
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0xDEADBEEFCAFEBABE {
		t.Errorf("RAX = %#x", got)
	}
	if got := r.cpu.GetReg64(x86_64.RBX); got != 0x0123456789ABCDEF {
		t.Errorf("RBX = %#x", got)
	}
}

// TestAsm_R8R15 — REX.B reaches the extended GPR file.
func TestAsm_R8R15(t *testing.T) {
	src := `
bits 64
	mov r8,  0x0808080808080808
	mov r9,  0x0909090909090909
	mov r15, 0x1F1F1F1F1F1F1F1F
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.R8); got != 0x0808080808080808 {
		t.Errorf("R8 = %#x", got)
	}
	if got := r.cpu.GetReg64(x86_64.R9); got != 0x0909090909090909 {
		t.Errorf("R9 = %#x", got)
	}
	if got := r.cpu.GetReg64(x86_64.R15); got != 0x1F1F1F1F1F1F1F1F {
		t.Errorf("R15 = %#x", got)
	}
}

// TestAsm_RegToReg — MOV between 64-bit registers via 0x89 (Ev,Gv) form.
func TestAsm_RegToReg(t *testing.T) {
	src := `
bits 64
	mov rax, 0x1122334455667788
	mov rbx, rax
	mov r10, rax
	mov r11, r10
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, idx := range []int{x86_64.RAX, x86_64.RBX, x86_64.R10, x86_64.R11} {
		if got := r.cpu.GetReg64(idx); got != 0x1122334455667788 {
			t.Errorf("reg %d = %#x", idx, got)
		}
	}
}

// TestAsm_ArithFlags — ADD and SUB produce the right flag bits.
func TestAsm_ArithFlags(t *testing.T) {
	src := `
bits 64
	mov rax, 0xFFFFFFFFFFFFFFFE
	mov rbx, 2
	add rax, rbx          ; result 0, CF=1, ZF=1
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0 {
		t.Errorf("RAX = %#x, want 0", got)
	}
	fl := r.cpu.GetRFLAGS()
	if fl&x86_64.RFLAGS_CF == 0 {
		t.Errorf("CF not set on unsigned overflow")
	}
	if fl&x86_64.RFLAGS_ZF == 0 {
		t.Errorf("ZF not set on zero result")
	}
}

// TestAsm_PushPop — RSP balance and full 64-bit value transfer.
func TestAsm_PushPop(t *testing.T) {
	src := `
bits 64
	mov rsp, 0x100000
	mov rax, 0xABCDEF0123456789
	push rax
	mov rax, 0
	pop rbx
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RBX); got != 0xABCDEF0123456789 {
		t.Errorf("RBX = %#x", got)
	}
	if got := r.cpu.GetReg64(x86_64.RSP); got != 0x100000 {
		t.Errorf("RSP not restored: %#x", got)
	}
}

// TestAsm_CallRet — call+ret round-trip via the stack.
func TestAsm_CallRet(t *testing.T) {
	src := `
bits 64
	mov rsp, 0x100000
	mov rax, 0
	call add_one
	call add_one
	call add_one
	hlt

add_one:
	add rax, 1
	ret
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 200); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 3 {
		t.Errorf("RAX = %d, want 3", got)
	}
	if got := r.cpu.GetReg64(x86_64.RSP); got != 0x100000 {
		t.Errorf("RSP after 3 call/ret = %#x, want 0x100000", got)
	}
}

// TestAsm_LeaRIPRel — RIP-relative LEA computes "next instruction +
// disp32" correctly. The data label sits 10 bytes after the LEA.
func TestAsm_LeaRIPRel(t *testing.T) {
	src := `
bits 64
	lea rax, [rel data]   ; should be address of data label
	hlt
data:
	dq 0xDEADBEEF12345678
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	// data label sits at offset 8 (7-byte LEA + 1-byte HLT).
	want := codeBase + 8
	if got := r.cpu.GetReg64(x86_64.RAX); got != want {
		t.Errorf("RAX (data addr) = %#x, want %#x", got, want)
	}
}

// TestAsm_LoadStore — store a value to memory, then load it back.
func TestAsm_LoadStore(t *testing.T) {
	src := `
bits 64
	mov rdi, 0x200000          ; pointer to buffer
	mov rax, 0x1122334455667788
	mov [rdi], rax
	mov rbx, [rdi]
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RBX); got != 0x1122334455667788 {
		t.Errorf("RBX = %#x", got)
	}
	// Direct check of memory.
	if got, err := r.mm.Read64(0x200000); err != nil || got != 0x1122334455667788 {
		t.Errorf("mem[0x200000] = %#x (err=%v)", got, err)
	}
}

// TestAsm_JmpForward — short jmp skips over instructions.
func TestAsm_JmpForward(t *testing.T) {
	src := `
bits 64
	mov rax, 1
	jmp end
	mov rax, 99    ; skipped
	mov rax, 99    ; skipped
end:
	hlt
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 100); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 1 {
		t.Errorf("RAX = %d, want 1 (jmp should have skipped the overwrites)", got)
	}
}

// TestAsm_Program — composite: mov + lea + load + arith.
func TestAsm_Program(t *testing.T) {
	src := `
bits 64
	mov rsp, 0x100000
	lea rdi, [rel msg]
	mov rax, [rdi]             ; rax = *msg (load 64-bit value)
	add rax, 1
	mov [rdi + 8], rax         ; store
	hlt

msg:
	dq 0x4142434445464748       ; "HGFEDCBA" little-endian
	dq 0
`
	r := newAsmRunner(t)
	r.load(t, codeBase, assemble(t, src))
	if err := r.run(t, 200); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := r.cpu.GetReg64(x86_64.RAX); got != 0x4142434445464749 {
		t.Errorf("RAX = %#x, want 0x4142434445464749", got)
	}
}
