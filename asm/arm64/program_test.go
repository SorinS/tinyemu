package arm64

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mcObjectText assembles a full program with llvm-mc to an ELF object and
// returns its .text bytes — labels resolved. ok=false if llvm-mc fails.
func mcObjectText(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "p.s")
	out := filepath.Join(dir, "p.o")
	if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(llvmMC, "--triple=aarch64", "--filetype=obj", in, "-o", out)
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

// TestARM64_ProgramLabels checks that label-resolved programs match llvm-mc
// byte-for-byte (forward/backward branches, b.cond, cbz/cbnz, bl to a label).
func TestARM64_ProgramLabels(t *testing.T) {
	requireLLVMMC(t)
	programs := []string{
		// backward branch (loop): b.<cond> to a label behind the PC
		"start:\n  movz x0, #0\nloop:\n  add x0, x0, #1\n  subs x1, x1, #1\n  b.ne loop\n  ret\n",
		// forward conditional + fallthrough
		"  subs xzr, x0, x1\n  b.eq equal\n  movz x2, #1\nequal:\n  ret\n",
		// unconditional b/bl to labels, fallthrough
		"  bl func\n  b done\nfunc:\n  add x0, x0, #1\n  ret\ndone:\n  ret\n",
		// cbz/cbnz to labels (forward and backward)
		"  cbz x0, skip\n  add x1, x1, #1\nskip:\n  cbnz x1, skip\n  ret\n",
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
		case string(got) != string(want):
			t.Errorf("prog %d DIFF:\n mine % x\n llvm % x", i, got, want)
		}
	}
}

func TestARM64_CollectLabelsAndLine(t *testing.T) {
	src := "start:\n  movz x0, #0\nloop:\n  b.ne loop\n"
	labels := CollectLabels(src)
	if labels["start"] != 0 || labels["loop"] != 4 {
		t.Fatalf("labels = %v, want start=0 loop=4", labels)
	}
	if _, err := AssembleLine("b.ne loop", labels); err != nil {
		t.Errorf("AssembleLine(branch to label): %v", err)
	}
	if b, err := AssembleLine("loop:", labels); err != nil || b != nil {
		t.Errorf("AssembleLine(label-only) = %x, %v; want nil, nil", b, err)
	}
}
