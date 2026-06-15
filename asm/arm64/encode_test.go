package arm64

import (
	"encoding/binary"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// llvmMC is the path to llvm-mc (brew LLVM). Tests skip if absent.
const llvmMC = "/opt/homebrew/opt/llvm/bin/llvm-mc"

func requireLLVMMC(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(llvmMC); err != nil {
		t.Skip("llvm-mc not found at " + llvmMC)
	}
}

var encRe = regexp.MustCompile(`encoding:\s*\[([^\]]*)\]`)

// mcEncode assembles one instruction with llvm-mc (AArch64) and returns its 4
// bytes. ok=false if llvm-mc errored or the encoding holds fixup placeholders
// (PC-relative operands render as 'A'/0bAAAA…, not concrete bytes).
func mcEncode(t *testing.T, src string) ([]byte, bool) {
	t.Helper()
	cmd := exec.Command(llvmMC, "--triple=aarch64", "--show-encoding")
	cmd.Stdin = strings.NewReader(src + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false
	}
	m := encRe.FindStringSubmatch(string(out))
	if m == nil {
		return nil, false
	}
	var b []byte
	for _, tok := range strings.Split(m[1], ",") {
		tok = strings.TrimSpace(tok)
		if !strings.HasPrefix(tok, "0x") || len(tok) != 4 {
			return nil, false // fixup placeholder, not a concrete byte
		}
		v, err := strconv.ParseUint(tok[2:], 16, 8)
		if err != nil {
			return nil, false
		}
		b = append(b, byte(v))
	}
	return b, true
}

// TestARM64_DifferentialLLVMMC holds the encoder byte-for-byte against llvm-mc
// across the first instruction slice. Branch cases use a fixed numeric offset
// so llvm-mc emits concrete bytes (no fixup placeholder).
func TestARM64_DifferentialLLVMMC(t *testing.T) {
	requireLLVMMC(t)
	cases := []string{
		// add/sub shifted register (x and w)
		"add x0, x1, x2", "add w0, w1, w2", "sub x3, x4, x5", "sub w3, w4, w5",
		"adds x0, x1, x2", "subs x7, x8, x9", "add x0, x1, x2, lsl #4",
		"sub x0, x1, x2, asr #3", "add sp, sp, x0", "add x0, sp, x1",
		// add/sub immediate (with lsl #12)
		"add x0, x1, #8", "add w0, w1, #1", "sub x0, x1, #0x10",
		"add x0, x1, #1, lsl #12", "adds x0, x1, #4095", "subs xzr, x0, #1",
		"add sp, sp, #16", "sub sp, sp, #32",
		// logical shifted register
		"and x0, x1, x2", "orr x0, x1, x2", "eor x0, x1, x2", "ands x0, x1, x2",
		"bic x0, x1, x2", "orn x0, x1, x2", "eon x0, x1, x2", "bics x0, x1, x2",
		"and w0, w1, w2", "orr x0, x1, x2, lsl #8", "eor x0, x1, x2, ror #16",
		// move wide
		"movz x0, #0x1234", "movz x0, #0xffff, lsl #16", "movn x0, #0",
		"movk x0, #0xabcd, lsl #32", "movz w0, #0x10", "movz x5, #0x1, lsl #48",
		// load/store unsigned offset
		"ldr x0, [x1]", "ldr x0, [x1, #8]", "str x0, [x1, #16]",
		"ldr w0, [x1, #8]", "str w2, [x3, #4]", "ldr x0, [sp, #0]",
		"ldr x30, [x29, #2040]", "str x0, [x1, #32760]",
		// branches with numeric offsets (concrete bytes)
		"b #0", "b #8", "b #-8", "bl #16", "b.eq #8", "b.ne #-4",
		"b.lt #12", "b.ge #0", "cbz x0, #8", "cbnz w1, #-8",
		// register branches
		"ret", "ret x0", "br x1", "blr x2", "ret x30",
	}
	for _, src := range cases {
		want, ok := mcEncode(t, src)
		if !ok {
			t.Logf("skip %q (llvm-mc gave no concrete bytes)", src)
			continue
		}
		got, err := Assemble(src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%-28q got %08x  llvm-mc %08x", src,
				binary.LittleEndian.Uint32(got), binary.LittleEndian.Uint32(want))
		}
	}
}
