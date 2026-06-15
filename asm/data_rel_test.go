package asm

import "testing"

// TestDataAndRel checks data directives (db/dw/dd/dq) and RIP-relative symbol
// references ([rel sym]) byte-for-byte against nasm.
func TestDataAndRel(t *testing.T) {
	requireNasm(t)
	programs := []string{
		"  lea rsi, [rel arr]\n  mov rax, [rsi]\n  hlt\narr: dq 42, 43, 44\n",
		"  lea rdi, [rel msg]\n  mov al, [rdi]\n  ret\nmsg: db \"hi\", 0\n",
		"  mov eax, [rel val]\n  ret\nval: dd 0x12345678\n",
		"  lea rax, [rel tbl + 8]\n  ret\ntbl: dq 1, 2, 3\n",
		"  movzx rax, word [rel w]\n  ret\nw: dw 0xbeef\n",
	}
	for i, src := range programs {
		want, ok := nasmProgram(t, src)
		if !ok {
			t.Logf("SKIP prog %d (nasm rejected)", i)
			continue
		}
		got, err := AssembleProgram(src)
		switch {
		case err != nil:
			t.Errorf("prog %d MISS: %v", i, err)
		case !bytesEqual(got, want):
			t.Errorf("prog %d DIFF:\n mine % x\n nasm % x", i, got, want)
		}
	}
}
