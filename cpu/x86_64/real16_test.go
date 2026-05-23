package x86_64

import (
	"testing"

	"github.com/jtolio/tinyemu-go/mem"
)

// Tests for real-mode 16-bit code execution in cpu/x86_64. These pin
// behaviour that's necessary for legacy-BIOS (SeaBIOS, MenuetOS) boot
// paths but isn't exercised by the long-mode direct-kernel-load path
// our Linux guests use.

// newRealModeCPU returns a CPU in real mode with 1 MB of low RAM, code
// placed at 0xF000:rip (so the segment base is 0xF0000) so callers can
// write bytes at [0xF0000+rip:] and Step().
func newRealModeCPU(t *testing.T) (*CPU, *mem.PhysMemoryMap) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	// Default reset state is already real mode at CS=F000, RIP=FFF0;
	// keep that.
	return c, mm
}

// writeBytes is a small convenience for laying down a code stream at a
// physical address.
func writeBytes(t *testing.T, mm *mem.PhysMemoryMap, addr uint64, b []byte) {
	t.Helper()
	for i, x := range b {
		if err := mm.Write8(addr+uint64(i), x); err != nil {
			t.Fatalf("write byte %d at %#x: %v", i, addr+uint64(i), err)
		}
	}
}

// TestRealMode_DefaultOperandAndAddressSize pins that Step() picks
// (operandSize=2, addressSize=2) by default in real mode. Reproduces
// the SeaBIOS regression — when the decoder defaulted to (4, 8), the
// reset-vector JMP FAR was misdecoded as a 32-bit-offset jump and the
// CPU lost itself in unmapped memory.
func TestRealMode_DefaultOperandAndAddressSize(t *testing.T) {
	c, _ := newRealModeCPU(t)
	// We can't observe operandSize directly; instead verify behaviour
	// via an instruction whose decoding depends on it. MOV AX, imm16
	// (0xB8) is one byte opcode + imm-of-default-operand-size.
	mm := c.memMap
	writeBytes(t, mm, 0xFFFF0, []byte{0xB8, 0x34, 0x12, 0xF4}) // MOV AX,0x1234 ; HLT
	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if got := c.GetReg16(AX); got != 0x1234 {
		t.Errorf("AX = %#x, want 0x1234 (decoded as imm32 instead of imm16?)", got)
	}
	// RIP should be 0xFFF0 + 4 = 0xFFF4 (1 byte opcode + 2 byte imm16 + 1 byte HLT).
	if got := c.GetRIP(); got != 0xFFF4 {
		t.Errorf("RIP = %#x, want 0xFFF4 (decoded extra bytes as imm32?)", got)
	}
}

// TestRealMode_JMPFar pins that 0xEA in real mode loads CS.base via
// sel<<4 without touching a GDT (none exists). Reproduces the SeaBIOS
// reset-vector decode.
func TestRealMode_JMPFar(t *testing.T) {
	c, mm := newRealModeCPU(t)
	// JMP FAR 0x1234:0xABCD ; HLT-at-target
	writeBytes(t, mm, 0xFFFF0, []byte{0xEA, 0xCD, 0xAB, 0x34, 0x12})
	// Place a HLT at the target so the test can verify we landed.
	target := uint64(0x1234)<<4 + 0xABCD
	writeBytes(t, mm, target, []byte{0xF4})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if got := c.GetSeg(CS); got != 0x1234 {
		t.Errorf("CS selector = %#x, want 0x1234", got)
	}
	if got := c.GetSegBase64(CS); got != 0x12340 {
		t.Errorf("CS base = %#x, want 0x12340 (sel<<4)", got)
	}
	if got := c.GetRIP(); got != 0xABCD+1 {
		t.Errorf("RIP = %#x, want 0xABCE (after HLT)", got)
	}
}

// TestRealMode_JMPNearRel16 pins that 0xE9 in real mode uses a 16-bit
// displacement, not 32-bit. SeaBIOS hit this in `e9 b7 ee 66 55` —
// rel16 = 0xEEB7 (negative) vs misdecoded rel32 sending RIP into
// unmapped memory.
func TestRealMode_JMPNearRel16(t *testing.T) {
	c, mm := newRealModeCPU(t)
	// JMP rel16 = +5 (skip the 5-byte sled), then HLT at the target.
	writeBytes(t, mm, 0xFFFF0, []byte{
		0xE9, 0x05, 0x00, // JMP +5 (3 bytes)
		0x90, 0x90, 0x90, 0x90, 0x90, // 5-byte sled the JMP must skip
		0xF4, // HLT
	})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	// RIP after JMP = 0xFFF0 + 3 + 5 = 0xFFF8. HLT at 0xFFF8. After
	// executing HLT, RIP = 0xFFF9.
	if got := c.GetRIP(); got != 0xFFF9 {
		t.Errorf("RIP = %#x, want 0xFFF9 — JMP rel16 went elsewhere", got)
	}
}

// TestRealMode_JccRel16 pins that 0F 8x conditional jumps in real
// mode use rel16, not rel32. SeaBIOS uses `0F 85 disp16` for `JNZ` in
// its early POST.
func TestRealMode_JccRel16(t *testing.T) {
	c, mm := newRealModeCPU(t)
	// Set ZF=1 via XOR AX,AX, then JNE +4 (should NOT take, fall
	// through), then HLT immediately. The JNE is 4 bytes; if the
	// decoder mistakenly fetches 4 disp bytes instead of 2, it'll
	// consume extra bytes and the HLT lands in the wrong place.
	writeBytes(t, mm, 0xFFFF0, []byte{
		0x31, 0xC0, // XOR AX,AX  (ZF=1)
		0x0F, 0x85, 0xFF, 0x7F, // JNE +0x7FFF (not taken)
		0xF4, // HLT
	})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	// XOR (2) + JNE (4 bytes: 0F 85 + rel16) + HLT (1) = 7 bytes.
	// Final RIP = 0xFFF0 + 7 = 0xFFF7.
	if got := c.GetRIP(); got != 0xFFF7 {
		t.Errorf("RIP = %#x, want 0xFFF7 — Jcc disp size wrong?", got)
	}
}

// TestRealMode_ModRM16Disp16 pins parseModRM16's mod=00, rm=6
// (direct disp16) special case. CMP word ptr [0x1234], 0x55.
func TestRealMode_ModRM16Disp16(t *testing.T) {
	c, mm := newRealModeCPU(t)
	// Plant 0x55 at DS:0x1234. DS.base in real mode is 0 by default
	// (CS reset only sets CS), so DS:0x1234 = linear 0x1234.
	writeBytes(t, mm, 0x1234, []byte{0x55})

	// 80 3E 34 12 55   CMP byte ptr [0x1234], 0x55
	// (opcode 0x80 = CMP r/m8, imm8 — ModRM 0x3E = mod=00, reg=7
	// (CMP), rm=6 (direct disp16). Then disp16 = 0x1234, imm8 = 0x55.)
	// Then HLT.
	writeBytes(t, mm, 0xFFFF0, []byte{
		0x80, 0x3E, 0x34, 0x12, 0x55, // CMP byte [0x1234], 0x55
		0xF4, // HLT
	})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	// CMP equal → ZF=1.
	if c.GetRFLAGS()&RFLAGS_ZF == 0 {
		t.Error("ZF expected after CMP-equal of two 0x55 bytes")
	}
	// RIP after CMP (5 bytes) + HLT (1) = 6 bytes from 0xFFF0 = 0xFFF6.
	if got := c.GetRIP(); got != 0xFFF6 {
		t.Errorf("RIP = %#x, want 0xFFF6 — ModRM disp16 mis-sized?", got)
	}
}

// TestRealMode_ModRM16RegPair pins parseModRM16's [BX+SI] encoding
// (mod=00, rm=0). Reading via segment base of 0 lets us put the
// target byte in low RAM.
func TestRealMode_ModRM16RegPair(t *testing.T) {
	c, mm := newRealModeCPU(t)
	writeBytes(t, mm, 0x500, []byte{0x42}) // value at [BX+SI] = 0x500
	c.SetReg16(BX, 0x200)
	c.SetReg16(SI, 0x300)

	// MOV AL, [BX+SI]      8A 00       (8A = MOV r8, r/m8; ModRM 0x00
	//                                    = mod=00, reg=0 (AL), rm=0
	//                                    ([BX+SI])).
	// HLT                  F4
	writeBytes(t, mm, 0xFFFF0, []byte{0x8A, 0x00, 0xF4})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if got := c.GetReg8(AL); got != 0x42 {
		t.Errorf("AL = %#x, want 0x42 — [BX+SI] addressing broken", got)
	}
}

// pm32CPU brings the CPU into 32-bit protected mode with flat
// segments and a small stack at 0x9000. Used by stack-width tests
// that need to exercise pm32 path of pushStack/popStack.
func pm32CPU(t *testing.T) (*CPU, *mem.PhysMemoryMap) {
	t.Helper()
	mm := mem.NewPhysMemoryMap()
	t.Cleanup(mm.Close)
	if _, err := mm.RegisterRAM(0, 1<<20, 0); err != nil {
		t.Fatalf("RegisterRAM: %v", err)
	}
	c := NewCPU(mm)
	c.SetCR64(0, CR0_PE|CR0_ET|CR0_NE)
	c.SetSegAccess(CS, 0xC9A) // 32-bit code: G=1 D=1 P=1 type=A
	c.SetSegBase64(CS, 0)
	c.SetSegLimit(CS, 0xFFFFFFFF)
	for _, s := range []int{DS, ES, SS, FS, GS} {
		c.SetSegAccess(s, 0xC92) // 32-bit data
		c.SetSegBase64(s, 0)
		c.SetSegLimit(s, 0xFFFFFFFF)
	}
	c.SetReg64(RSP, 0x9000)
	c.SetRIP(0x1000)
	return c, mm
}

// TestPM32_RETPops4Bytes pins that RET in 32-bit protected mode pops
// exactly 4 bytes from the stack, not 8. Push64 / pop64 were used
// unconditionally; in pm32 the caller pushes EIP (4 bytes), our RET
// read 8, and we returned to (push_value | high_garbage).
func TestPM32_RETPops4Bytes(t *testing.T) {
	c, mm := pm32CPU(t)
	// Put the return address 0x1234 at [ESP]; 4 bytes above it put
	// 0xCAFE — if we mistakenly pop 8 bytes we'd see that high half.
	writeBytes(t, mm, 0x9000, []byte{
		0x34, 0x12, 0x00, 0x00, // saved EIP
		0xFE, 0xCA, 0x00, 0x00, // poison
	})
	// Program: RET ; the byte after isn't reached because RET jumps.
	writeBytes(t, mm, 0x1000, []byte{0xC3})

	if err := c.Step(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if got := c.GetRIP(); got != 0x1234 {
		t.Errorf("RIP = %#x, want 0x1234 (popped 8 bytes instead of 4?)", got)
	}
	if got := c.GetReg64(RSP); got != 0x9004 {
		t.Errorf("RSP = %#x, want 0x9004 (RET adjusted RSP by 8 instead of 4?)", got)
	}
}

// TestPM32_PUSHimmPushes4Bytes pins that PUSH imm in 32-bit
// protected mode consumes 4 stack bytes, not 8. SeaBIOS calls
// printf-like functions with a format-string pointer pushed via
// PUSH imm32; an 8-byte push would leave a 4-byte hole that later
// RETs interpret as a return address into the BIOS rodata.
func TestPM32_PUSHimmPushes4Bytes(t *testing.T) {
	c, mm := pm32CPU(t)
	// Program: PUSH 0xABCDEF12 ; HLT
	writeBytes(t, mm, 0x1000, []byte{
		0x68, 0x12, 0xEF, 0xCD, 0xAB,
		0xF4,
	})
	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if got := c.GetReg64(RSP); got != 0x9000-4 {
		t.Errorf("RSP = %#x, want %#x (PUSH imm consumed wrong slot width)", got, uint64(0x9000-4))
	}
	v, _ := c.memMap.Read32(uint64(c.GetReg64(RSP)))
	if v != 0xABCDEF12 {
		t.Errorf("stack[%#x] = %#x, want 0xABCDEF12", c.GetReg64(RSP), v)
	}
}

// TestRealMode_LGDT32BitBase pins that LGDT in real / 32-bit modes
// reads a 32-bit base from the pseudo-descriptor, not a 64-bit base.
// SeaBIOS hit this — its pseudo-descriptor is limit(2) + base(4) = 6
// bytes in real mode, but our LGDT unconditionally read 8 bytes,
// loading 4 bytes of adjacent garbage into GDTR.base. Then the next
// far jump's descriptor lookup landed at a random address and CS came
// back without its D-bit — mode stuck at pm16 even after the kernel
// thought it was in pm32, every subsequent operand-size-dependent
// decode mis-framed.
func TestRealMode_LGDT32BitBase(t *testing.T) {
	c, mm := newRealModeCPU(t)
	// Lay down a pseudo-descriptor at 0x600: limit=0xFFFF, base=0x00ABCDEF.
	writeBytes(t, mm, 0x600, []byte{
		0xFF, 0xFF, // limit
		0xEF, 0xCD, 0xAB, 0x00, // base (32-bit LE)
	})
	// Plant 8 bytes of garbage immediately after — these should NOT
	// be merged into the loaded base.
	writeBytes(t, mm, 0x606, []byte{0xFF, 0xFF, 0xFF, 0xFF})

	// LGDT [0x600]:
	//   0F 01 16 00 06  (16-bit addressing: 0F 01 + ModRM 0x16 mod=00
	//   rm=6 disp16 + disp16=0x0600). Then HLT.
	writeBytes(t, mm, 0xFFFF0, []byte{
		0x0F, 0x01, 0x16, 0x00, 0x06,
		0xF4,
	})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if got := c.GetSegBase64(GDTR); got != 0x00ABCDEF {
		t.Errorf("GDTR.base = %#x, want 0xABCDEF (loaded 64-bit instead of 32-bit?)", got)
	}
	if got := c.GetSegLimit(GDTR); got != 0xFFFF {
		t.Errorf("GDTR.limit = %#x, want 0xFFFF", got)
	}
}

// TestRealMode_ModRM16BPDefaultSS pins that [BP] addressing
// (mod=01, rm=6) uses SS as the default segment, not DS.
func TestRealMode_ModRM16BPDefaultSS(t *testing.T) {
	c, mm := newRealModeCPU(t)
	// SS.base = 0x1000 (selector value doesn't matter for this test —
	// we set the cached base directly).
	c.SetSeg(SS, 0x0100)
	c.SetSegBase64(SS, 0x1000)
	c.SetReg16(BP, 0x50)

	// Plant the data at SS:[BP+0] = 0x1000 + 0x50 = 0x1050.
	writeBytes(t, mm, 0x1050, []byte{0x77})

	// MOV AL, [BP+0]  8A 46 00
	//   ModRM 0x46 = mod=01, reg=0 (AL), rm=6 ([BP]+disp8).
	// HLT             F4
	writeBytes(t, mm, 0xFFFF0, []byte{0x8A, 0x46, 0x00, 0xF4})

	for !c.IsPowerDown() {
		if err := c.Step(); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if got := c.GetReg8(AL); got != 0x77 {
		t.Errorf("AL = %#x, want 0x77 — [BP] addressing didn't pick SS as default", got)
	}
}
