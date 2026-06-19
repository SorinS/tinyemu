package x86_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sorins/tinyemu-go/cpu/x86"
	"github.com/sorins/tinyemu-go/mem"
)

// asmRunner assembles NASM source into a flat binary, loads it into an x86
// CPU, and runs it.
type asmRunner struct {
	cpu     *x86.CPU
	memMap  *mem.PhysMemoryMap
	startCS uint16
	startIP uint32
}

// newAsmRunner creates a runner with 1MB of RAM at physical address 0 and
// the standard PC reset state (CS=F000, base=F0000, IP=FFF0).
func newAsmRunner(t *testing.T) *asmRunner {
	mm := mem.NewPhysMemoryMap()
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("failed to register RAM: %v", err)
	}
	c := x86.NewCPU(mm)
	return &asmRunner{
		cpu:     c,
		memMap:  mm,
		startCS: 0xF000,
		startIP: 0xFFF0,
	}
}

// assemble runs NASM on the given source and returns the path to the flat
// binary. The caller should remove the file when done.
func assemble(t *testing.T, src string) string {
	t.Helper()
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "test.asm")
	binPath := filepath.Join(tmpDir, "test.bin")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write asm source: %v", err)
	}
	cmd := exec.Command("nasm", "-f", "bin", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nasm failed: %v\n%s", err, out)
	}
	return binPath
}

// load copies a flat binary into physical memory at the given address.
func (r *asmRunner) load(t *testing.T, binPath string, physAddr uint32) {
	t.Helper()
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}
	for i, b := range data {
		r.cpu.WriteMem8(physAddr+uint32(i), b)
	}
}

// run executes the CPU from the current CS:IP until HLT, error, or maxSteps.
func (r *asmRunner) run(t *testing.T, maxSteps int) error {
	t.Helper()
	for i := 0; i < maxSteps; i++ {
		if r.cpu.IsPowerDown() {
			return nil
		}
		if err := r.cpu.Step(); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}
	return fmt.Errorf("did not halt within %d steps", maxSteps)
}

// setStart sets the initial CS:IP.
func (r *asmRunner) setStart(cs uint16, ip uint32) {
	r.cpu.SetSeg(x86.CS, cs)
	r.cpu.SetSegBase(x86.CS, uint32(cs)<<4)
	r.cpu.SetEIP(ip)
	r.startCS = cs
	r.startIP = ip
}

// TestAsmMovImm runs a tiny NASM program that moves immediates into registers.
func TestAsmMovImm(t *testing.T) {
	src := `bits 16
	mov ax, 0x1234
	mov bx, 0x5678
	mov cx, 0x9ABC
	hlt
`
	bin := assemble(t, src)
	defer os.Remove(bin)

	r := newAsmRunner(t)
	r.load(t, bin, 0xF0000) // load at CS=F000 base
	r.setStart(0xF000, 0x0000)

	if err := r.run(t, 100); err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if v := r.cpu.GetReg16(x86.AX); v != 0x1234 {
		t.Errorf("AX = 0x%04X, want 0x1234", v)
	}
	if v := r.cpu.GetReg16(x86.BX); v != 0x5678 {
		t.Errorf("BX = 0x%04X, want 0x5678", v)
	}
	if v := r.cpu.GetReg16(x86.CX); v != 0x9ABC {
		t.Errorf("CX = 0x%04X, want 0x9ABC", v)
	}
}

// TestAsmAddSub runs a NASM program that exercises ADD and SUB.
func TestAsmAddSub(t *testing.T) {
	src := `bits 16
	mov ax, 10
	add ax, 5
	mov bx, ax
	sub bx, 3
	hlt
`
	bin := assemble(t, src)
	defer os.Remove(bin)

	r := newAsmRunner(t)
	r.load(t, bin, 0xF0000)
	r.setStart(0xF000, 0x0000)

	if err := r.run(t, 100); err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if v := r.cpu.GetReg16(x86.AX); v != 15 {
		t.Errorf("AX = %d, want 15", v)
	}
	if v := r.cpu.GetReg16(x86.BX); v != 12 {
		t.Errorf("BX = %d, want 12", v)
	}
}

// TestAsmPushPop runs a NASM program that exercises PUSH and POP.
func TestAsmPushPop(t *testing.T) {
	src := `bits 16
	mov sp, 0x8000
	mov ax, 0xDEAD
	push ax
	mov ax, 0xBEEF
	pop bx
	hlt
`
	bin := assemble(t, src)
	defer os.Remove(bin)

	r := newAsmRunner(t)
	r.load(t, bin, 0xF0000)
	r.setStart(0xF000, 0x0000)

	if err := r.run(t, 100); err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if v := r.cpu.GetReg16(x86.BX); v != 0xDEAD {
		t.Errorf("BX = 0x%04X, want 0xDEAD", v)
	}
	if v := r.cpu.GetReg16(x86.AX); v != 0xBEEF {
		t.Errorf("AX = 0x%04X, want 0xBEEF (should be unchanged after second mov)", v)
	}
}

// TestAsmJmpCallRet runs a NASM program with JMP, CALL and RET.
func TestAsmJmpCallRet(t *testing.T) {
	src := `bits 16
	mov ax, 0
	call inc_twice
	jmp done

inc_twice:
	inc ax
	inc ax
	ret

done:
	hlt
`
	bin := assemble(t, src)
	defer os.Remove(bin)

	r := newAsmRunner(t)
	r.load(t, bin, 0xF0000)
	r.setStart(0xF000, 0x0000)

	if err := r.run(t, 100); err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if v := r.cpu.GetReg16(x86.AX); v != 2 {
		t.Errorf("AX = %d, want 2", v)
	}
}
