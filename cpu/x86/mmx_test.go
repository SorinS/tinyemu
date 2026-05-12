package x86

import "testing"

// TestMMX_MOVD_RegToMM covers `0F 6E /r` MOVD mm, r/m32 in the
// register-source form. The 32-bit value should be zero-extended into
// the 64-bit MMX register.
func TestMMX_MOVD_RegToMM(t *testing.T) {
	c := newTestCPU(t)
	c.SetReg32(EAX, 0xDEADBEEF)
	// MOVD MM3, EAX  =  0F 6E D8   (mod=11 reg=011 rm=000)
	code := []byte{0x0F, 0x6E, 0xD8, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[3] != 0x00000000DEADBEEF {
		t.Errorf("mm3 = %016X, want 0x00000000DEADBEEF", c.mm[3])
	}
}

// TestMMX_MOVD_MMToReg covers `0F 7E /r` MOVD r/m32, mm. The low 32
// bits of the MMX register are written to the destination GPR.
func TestMMX_MOVD_MMToReg(t *testing.T) {
	c := newTestCPU(t)
	c.mm[2] = 0xAABBCCDD11223344
	// MOVD EBX, MM2  =  0F 7E D3   (mod=11 reg=010 rm=011)
	code := []byte{0x0F, 0x7E, 0xD3, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if got := c.GetReg32(EBX); got != 0x11223344 {
		t.Errorf("ebx = %08X, want 0x11223344", got)
	}
}

// TestMMX_MOVQ_RegToReg covers `0F 6F /r` MOVQ mm, mm/m64 in the
// register form, and `0F 7F /r` MOVQ mm/m64, mm symmetrically. The
// full 64 bits must round-trip.
func TestMMX_MOVQ_RegToReg(t *testing.T) {
	c := newTestCPU(t)
	c.mm[5] = 0x0123456789ABCDEF
	// MOVQ MM1, MM5  =  0F 6F CD   (mod=11 reg=001 rm=101)
	// MOVQ MM7, MM1  =  0F 7F CF   (mod=11 reg=001 rm=111)
	code := []byte{0x0F, 0x6F, 0xCD, 0x0F, 0x7F, 0xCF, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[1] != 0x0123456789ABCDEF {
		t.Errorf("mm1 = %016X, want 0x0123456789ABCDEF", c.mm[1])
	}
	if c.mm[7] != 0x0123456789ABCDEF {
		t.Errorf("mm7 = %016X, want 0x0123456789ABCDEF", c.mm[7])
	}
}

// TestMMX_MOVQ_Memory exercises the memory forms of MOVQ — the path
// musl's memcpy actually uses (load 8 bytes via MMX, store 8 bytes
// via MMX).
func TestMMX_MOVQ_Memory(t *testing.T) {
	c := newTestCPU(t)
	// Place a known qword at flat memory and have ESI point at it.
	const src = uint32(0x2000)
	const dst = uint32(0x2010)
	want := uint64(0xCAFEBABEF00DD00D)
	c.writeMem32(src, uint32(want))
	c.writeMem32(src+4, uint32(want>>32))
	c.SetReg32(ESI, src)
	c.SetReg32(EDI, dst)
	// MOVQ MM0, [ESI]  =  0F 6F 06
	// MOVQ [EDI], MM0  =  0F 7F 07
	code := []byte{0x0F, 0x6F, 0x06, 0x0F, 0x7F, 0x07, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	if c.mm[0] != want {
		t.Errorf("mm0 = %016X, want %016X", c.mm[0], want)
	}
	gotLo := uint64(c.readMem32(dst))
	gotHi := uint64(c.readMem32(dst + 4))
	if got := gotLo | gotHi<<32; got != want {
		t.Errorf("dst memory = %016X, want %016X", got, want)
	}
}

// TestMMX_EMMS verifies 0F 77 executes without faulting and consumes
// no operands. Our implementation is intentionally a no-op (we don't
// track the x87 tag word), but the opcode must decode cleanly.
func TestMMX_EMMS(t *testing.T) {
	c := newTestCPU(t)
	c.mm[0] = 0xDEADBEEFCAFEBABE
	// EMMS  =  0F 77
	code := []byte{0x0F, 0x77, 0xF4}
	if err := runCode(t, c, code, 0x1000); err != nil {
		t.Fatalf("runCode: %v", err)
	}
	// mm[0] is unchanged — we don't actually reset the register file.
	if c.mm[0] != 0xDEADBEEFCAFEBABE {
		t.Errorf("EMMS modified mm0 = %016X", c.mm[0])
	}
}
