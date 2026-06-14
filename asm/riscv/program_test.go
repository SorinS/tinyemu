package riscv

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mcObjectText assembles a full program with llvm-mc to an ELF object and
// returns its .text section bytes — labels resolved. ok=false if llvm-mc fails.
func mcObjectText(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "p.s")
	out := filepath.Join(dir, "p.o")
	if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(llvmMC, "--triple=riscv64", "--mattr=+m,+a", "--filetype=obj", in, "-o", out)
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	f, err := elf.Open(out)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	sec := f.Section(".text")
	if sec == nil {
		return nil, false
	}
	data, err := sec.Data()
	if err != nil {
		return nil, false
	}
	return data, true
}

// TestRISCV_ProgramLabels checks that label-resolved programs match llvm-mc
// byte-for-byte (forward and backward branches, jal, pseudos).
func TestRISCV_ProgramLabels(t *testing.T) {
	requireLLVMMC(t)
	programs := []string{
		// backward branch (loop)
		"start:\n  addi a0, zero, 0\nloop:\n  addi a0, a0, 1\n  blt a0, a1, loop\n  ret\n",
		// forward conditional + fallthrough
		"  beq a0, a1, equal\n  addi a2, zero, 1\nequal:\n  ret\n",
		// jal to a label, and j pseudo
		"  jal ra, func\n  j done\nfunc:\n  addi a0, a0, 1\n  ret\ndone:\n  ret\n",
		// beqz/bnez pseudos to labels
		"  beqz a0, skip\n  addi a1, a1, 1\nskip:\n  bnez a1, skip\n  ret\n",
	}
	for i, src := range programs {
		want, ok := mcObjectText(t, src)
		if !ok {
			t.Logf("SKIP prog %d (llvm-mc failed)", i)
			continue
		}
		got, err := AssembleProgram(src)
		switch {
		case err != nil:
			t.Errorf("prog %d MISS: %v", i, err)
		case !eq(got, want):
			t.Errorf("prog %d DIFF:\n mine % x\n llvm % x", i, got, want)
		}
	}
}

func TestRISCV_CollectLabelsAndLine(t *testing.T) {
	src := "start:\n  addi a0, zero, 0\nloop:\n  blt a0, a1, loop\n"
	labels := CollectLabels(src)
	if labels["start"] != 0 || labels["loop"] != 4 {
		t.Fatalf("labels = %v, want start=0 loop=4", labels)
	}
	// A single line that branches to a known label assembles.
	if _, err := AssembleLine("blt a0, a1, loop", labels); err != nil {
		t.Errorf("AssembleLine(branch to label): %v", err)
	}
	// Label-only line yields nil.
	if b, err := AssembleLine("loop:", labels); err != nil || b != nil {
		t.Errorf("AssembleLine(label-only) = %x, %v; want nil, nil", b, err)
	}
}
