package x86_64

import (
	"fmt"
	"math"
)

// x87 FPU implementation for the long-mode backend.
//
// Ported from cpu/x86/x87.go. The arithmetic surface is identical;
// what differs is calling convention:
//   - addresses are uint64 not uint32
//   - the instruction pointer is c.rip and c.GetReg* take int
//   - flags live in c.rflags / RFLAGS_*
//
// We model the register file with 8 × float64 (not the architectural
// 80-bit extended precision). We lose 11 bits of mantissa vs. real
// hardware; almost no software relies on those bits.
//
// The register file is a stack. ST(i) means register `i` slots from
// the current top. The physical index is (fpuTop + i) mod 8. Push
// (FLD) decrements fpuTop then writes; pop reads then increments.
// Status word bits 11-13 mirror fpuTop. Tag word: 2 bits per
// physical register, 00=valid 01=zero 10=special 11=empty.

// fpuTag* are the per-register tag-word values per Intel SDM Vol 1.
const (
	fpuTagValid   = 0
	fpuTagZero    = 1
	fpuTagSpecial = 2
	fpuTagEmpty   = 3
)

// fpuReset clears the FPU state to architectural reset (post-FNINIT).
func (c *CPU) fpuReset() {
	for i := range c.fpu {
		c.fpu[i] = 0
	}
	c.fpuTop = 0
	c.fpuTag = 0xFFFF
	c.fpuStatusWord = 0
	c.fpuControlWord = 0x037F
	c.fpuInitialized = true
}

func (c *CPU) fpuPhysIdx(stI int) int {
	return int((uint(c.fpuTop) + uint(stI)) & 7)
}

func (c *CPU) fpuST(i int) float64 {
	return c.fpu[c.fpuPhysIdx(i)]
}

func (c *CPU) fpuSetST(i int, v float64) {
	idx := c.fpuPhysIdx(i)
	c.fpu[idx] = v
	c.fpuTagWrite(uint8(idx), classifyForTag(v))
}

func (c *CPU) fpuTagWrite(phys, tag uint8) {
	shift := phys * 2
	c.fpuTag = (c.fpuTag &^ (3 << shift)) | (uint16(tag&3) << shift)
}

func (c *CPU) fpuTagRead(phys uint8) uint8 {
	return uint8((c.fpuTag >> (phys * 2)) & 3)
}

func classifyForTag(v float64) uint8 {
	switch {
	case v == 0:
		return fpuTagZero
	case math.IsNaN(v) || math.IsInf(v, 0):
		return fpuTagSpecial
	default:
		return fpuTagValid
	}
}

func (c *CPU) fpuPush(v float64) {
	c.fpuTop = (c.fpuTop - 1) & 7
	c.fpu[c.fpuTop] = v
	c.fpuTagWrite(c.fpuTop, classifyForTag(v))
	c.fpuStatusWriteTop()
}

func (c *CPU) fpuPop() float64 {
	v := c.fpu[c.fpuTop]
	c.fpuTagWrite(c.fpuTop, fpuTagEmpty)
	c.fpuTop = (c.fpuTop + 1) & 7
	c.fpuStatusWriteTop()
	return v
}

func (c *CPU) fpuStatusWriteTop() {
	c.fpuStatusWord = (c.fpuStatusWord &^ 0x3800) | (uint16(c.fpuTop) << 11)
}

func (c *CPU) fpuLoadF32(addr uint64) {
	b := c.readMem32(addr)
	c.fpuPush(float64(math.Float32frombits(b)))
}

func (c *CPU) fpuLoadF64(addr uint64) {
	b := c.readMem64(addr)
	c.fpuPush(math.Float64frombits(b))
}

func (c *CPU) fpuStoreF32(addr uint64, v float64) {
	c.writeMem32(addr, math.Float32bits(float32(v)))
}

func (c *CPU) fpuStoreF64(addr uint64, v float64) {
	c.writeMem64(addr, math.Float64bits(v))
}

func (c *CPU) fpuLoadI16(addr uint64) {
	c.fpuPush(float64(int16(c.readMem16(addr))))
}

func (c *CPU) fpuLoadI32(addr uint64) {
	c.fpuPush(float64(int32(c.readMem32(addr))))
}

func (c *CPU) fpuLoadI64(addr uint64) {
	c.fpuPush(float64(int64(c.readMem64(addr))))
}

func (c *CPU) fpuStoreI16(addr uint64, v float64) {
	c.writeMem16(addr, uint16(int16(roundIntegerForStore(v))))
}

func (c *CPU) fpuStoreI32(addr uint64, v float64) {
	c.writeMem32(addr, uint32(int32(roundIntegerForStore(v))))
}

func (c *CPU) fpuStoreI64(addr uint64, v float64) {
	c.writeMem64(addr, uint64(int64(roundIntegerForStore(v))))
}

func roundIntegerForStore(v float64) float64 {
	return math.RoundToEven(v)
}

// fpuArith performs an FPU arithmetic operation indexed by `subop`
// (D8/DC family encoding):
//   0 = FADD,  1 = FMUL,  4 = FSUB, 5 = FSUBR, 6 = FDIV, 7 = FDIVR
//   2 = FCOM,  3 = FCOMP — compare only, no writeback
func fpuArith(subop uint8, dst, src float64) (float64, bool) {
	switch subop {
	case 0:
		return dst + src, true
	case 1:
		return dst * src, true
	case 2, 3:
		return 0, false
	case 4:
		return dst - src, true
	case 5:
		return src - dst, true
	case 6:
		return dst / src, true
	case 7:
		return src / dst, true
	}
	return 0, false
}

// fpuCompareSetFlags updates C0/C2/C3 in FSW per Intel SDM Table 4-9.
//   ST(0) > src   → C3=0 C2=0 C0=0
//   ST(0) < src   → C3=0 C2=0 C0=1
//   ST(0) = src   → C3=1 C2=0 C0=0
//   unordered     → C3=1 C2=1 C0=1
func (c *CPU) fpuCompareSetFlags(dst, src float64) {
	var c3, c2, c0 bool
	switch {
	case math.IsNaN(dst) || math.IsNaN(src):
		c3, c2, c0 = true, true, true
	case dst > src:
		c3, c2, c0 = false, false, false
	case dst < src:
		c3, c2, c0 = false, false, true
	default:
		c3, c2, c0 = true, false, false
	}
	c.fpuStatusWord &^= (1 << 8) | (1 << 10) | (1 << 14)
	if c0 {
		c.fpuStatusWord |= 1 << 8
	}
	if c2 {
		c.fpuStatusWord |= 1 << 10
	}
	if c3 {
		c.fpuStatusWord |= 1 << 14
	}
}

// fpuCompareSetFlagsRFlags is the FCOMI/FUCOMI variant: same result
// mapping but onto RFLAGS.ZF/PF/CF instead of FSW.C3/C2/C0. OF/SF/AF
// are cleared.
func (c *CPU) fpuCompareSetFlagsRFlags(dst, src float64) {
	var zf, pf, cf bool
	switch {
	case math.IsNaN(dst) || math.IsNaN(src):
		zf, pf, cf = true, true, true
	case dst > src:
		zf, pf, cf = false, false, false
	case dst < src:
		zf, pf, cf = false, false, true
	default:
		zf, pf, cf = true, false, false
	}
	c.rflags &^= RFLAGS_ZF | RFLAGS_PF | RFLAGS_CF | RFLAGS_OF | RFLAGS_SF | RFLAGS_AF
	if zf {
		c.rflags |= RFLAGS_ZF
	}
	if pf {
		c.rflags |= RFLAGS_PF
	}
	if cf {
		c.rflags |= RFLAGS_CF
	}
}

// fpuConst returns the named constant for FLDZ/FLD1/etc.
func fpuConst(subop uint8) (float64, bool) {
	switch subop {
	case 0:
		return 1.0, true
	case 1:
		return math.Log2(10), true
	case 2:
		return math.Log2(math.E), true
	case 3:
		return math.Pi, true
	case 4:
		return math.Log10(2), true
	case 5:
		return math.Ln2, true
	case 6:
		return 0.0, true
	}
	return 0, false
}

// fpuStoreEnv32 / fpuLoadEnv32 — 28-byte FPU environment image.
// Used by FNSTENV / FLDENV. Long mode adds a 32-byte form too, but
// the 28-byte form is what Linux/musl-on-Linux currently emits via
// libc setjmp / signal handlers — same as the i386 backend.
func (c *CPU) fpuStoreEnv32(addr uint64) {
	c.writeMem32(addr, uint32(c.fpuControlWord))
	c.writeMem32(addr+4, uint32(c.fpuStatusWord))
	c.writeMem32(addr+8, uint32(c.fpuTag))
	c.writeMem32(addr+12, 0)
	c.writeMem32(addr+16, 0)
	c.writeMem32(addr+20, 0)
	c.writeMem32(addr+24, 0)
}

func (c *CPU) fpuLoadEnv32(addr uint64) {
	c.fpuControlWord = uint16(c.readMem32(addr))
	c.fpuStatusWord = uint16(c.readMem32(addr + 4))
	c.fpuTag = uint16(c.readMem32(addr + 8))
	c.fpuTop = uint8((c.fpuStatusWord >> 11) & 7)
}

// fxsave writes a 512-byte FXSAVE area at `addr`. Layout follows
// Intel SDM Vol 1 §10.5.1 (32-bit non-REX form is what musl/Linux
// uses regardless of CPU mode in early boot; the 64-bit REX.W form
// has the same structure but with 64-bit FIP/FDP fields which we
// don't track anyway).
//
// We populate the fields real software inspects:
//   0   FCW       16    FPU control word
//   2   FSW       16    FPU status word
//   4   FTW       8     abridged tag word (1 bit per ST: 0=empty, 1=valid)
//   6   FOP       16    last x87 opcode — 0
//   8   FIP       32
//   12  FCS       16
//   16  FDP       32
//   20  FDS       16
//   24  MXCSR     32
//   28  MXCSR_MASK 32   0x0000FFBF
//   32  ST(0)..ST(7)    each 80-bit in 16-byte slots
//   160 XMM0..XMM15     each 128-bit (long mode covers the full 16)
func (c *CPU) fxsave(addr uint64) {
	c.fpuStatusWriteTop()
	c.writeMem16(addr+0, c.fpuControlWord)
	c.writeMem16(addr+2, c.fpuStatusWord)
	var ftw uint8
	for i := 0; i < 8; i++ {
		twoBit := (c.fpuTag >> (uint(i) * 2)) & 3
		if twoBit != 3 {
			ftw |= 1 << uint(i)
		}
	}
	c.writeMem8(addr+4, ftw)
	c.writeMem8(addr+5, 0)
	c.writeMem16(addr+6, 0)
	c.writeMem32(addr+8, 0)
	c.writeMem16(addr+12, 0)
	c.writeMem16(addr+14, 0)
	c.writeMem32(addr+16, 0)
	c.writeMem16(addr+20, 0)
	c.writeMem16(addr+22, 0)
	c.writeMem32(addr+24, c.mxcsr)
	c.writeMem32(addr+28, 0x0000FFBF)
	for i := 0; i < 8; i++ {
		c.fpuStore80(addr+uint64(32+i*16), c.fpu[i])
		c.writeMem16(addr+uint64(32+i*16+10), 0)
		c.writeMem32(addr+uint64(32+i*16+12), 0)
	}
	// XMM0..XMM15 — long mode stores the full 16 registers.
	for i := 0; i < 16; i++ {
		off := addr + uint64(160+i*16)
		c.writeMem64(off+0, c.xmm[i][0])
		c.writeMem64(off+8, c.xmm[i][1])
	}
}

func (c *CPU) fxrstor(addr uint64) {
	c.fpuControlWord = c.readMem16(addr + 0)
	c.fpuStatusWord = c.readMem16(addr + 2)
	c.fpuTop = uint8((c.fpuStatusWord >> 11) & 7)
	ftw := c.readMem8(addr + 4)
	var tag uint16
	for i := 0; i < 8; i++ {
		if ftw&(1<<uint(i)) == 0 {
			tag |= 3 << (uint(i) * 2)
		}
	}
	c.fpuTag = tag
	c.mxcsr = c.readMem32(addr + 24)
	for i := 0; i < 8; i++ {
		c.fpu[i] = c.fpuLoad80(addr + uint64(32+i*16))
	}
	for i := 0; i < 16; i++ {
		off := addr + uint64(160+i*16)
		c.xmm[i][0] = c.readMem64(off + 0)
		c.xmm[i][1] = c.readMem64(off + 8)
	}
}

// fpuStore80 — best-effort 80-bit extended-precision store. We model
// in float64 internally so the low 11 bits of mantissa are zero.
func (c *CPU) fpuStore80(addr uint64, v float64) {
	bits := math.Float64bits(v)
	sign := uint16((bits >> 63) & 1)
	exp := uint16((bits >> 52) & 0x7FF)
	frac := bits & ((1 << 52) - 1)

	var ext80Lo uint64
	var ext80Hi uint16

	switch {
	case exp == 0x7FF && frac == 0:
		ext80Hi = (sign << 15) | 0x7FFF
		ext80Lo = 1 << 63
	case exp == 0x7FF:
		ext80Hi = (sign << 15) | 0x7FFF
		ext80Lo = (1 << 63) | (frac << 11)
	case exp == 0:
		ext80Hi = sign << 15
		ext80Lo = 0
	default:
		newExp := uint16(int(exp) - 1023 + 16383)
		ext80Hi = (sign << 15) | (newExp & 0x7FFF)
		ext80Lo = (1 << 63) | (frac << 11)
	}

	c.writeMem32(addr, uint32(ext80Lo))
	c.writeMem32(addr+4, uint32(ext80Lo>>32))
	c.writeMem16(addr+8, ext80Hi)
}

func (c *CPU) fpuLoad80(addr uint64) float64 {
	lo := uint64(c.readMem32(addr))
	hi := uint64(c.readMem32(addr + 4))
	ext80Lo := lo | hi<<32
	ext80Hi := uint64(c.readMem16(addr + 8))
	sign := (ext80Hi >> 15) & 1
	exp := ext80Hi & 0x7FFF
	frac := ext80Lo & ((1 << 63) - 1)
	var bits uint64
	switch {
	case exp == 0x7FFF && frac == 0:
		bits = sign<<63 | 0x7FF<<52
	case exp == 0x7FFF:
		bits = sign<<63 | 0x7FF<<52 | (frac >> 11)
	case exp == 0:
		bits = sign << 63
	default:
		newExp := int(exp) - 16383 + 1023
		if newExp <= 0 {
			bits = sign << 63
		} else if newExp >= 0x7FF {
			bits = sign<<63 | 0x7FF<<52
		} else {
			bits = sign<<63 | uint64(newExp)<<52 | (frac >> 11)
		}
	}
	return math.Float64frombits(bits)
}

// handleX87 dispatches a single x87 opcode (D8-DF). Returns an error
// only for opcodes we explicitly reject; unimplemented ops fall back
// to NOP-with-ModRM-consume to keep the decoder in sync.
//
// rex is the REX prefix byte (most x87 forms ignore REX.W since the
// memory operand size is encoded in the opcode, but REX.B can extend
// the SIB base register in the address calculation — parseModRM64
// already folds that in).
func (c *CPU) handleX87(opcode, rex uint8) error {
	mr := c.parseModRM64(rex)

	switch opcode {
	case 0xD8:
		var src float64
		if mr.isReg {
			src = c.fpuST(int(mr.rm & 7))
		} else {
			b := c.readMem32(c.segBaseForModRM(mr) + mr.ea)
			src = float64(math.Float32frombits(b))
		}
		dst := c.fpuST(0)
		if mr.reg == 2 || mr.reg == 3 {
			c.fpuCompareSetFlags(dst, src)
			if mr.reg == 3 {
				c.fpuPop()
			}
			return nil
		}
		res, write := fpuArith(mr.reg, dst, src)
		if write {
			c.fpuSetST(0, res)
		}

	case 0xD9:
		if mr.isReg {
			return c.handleX87_D9_reg(mr)
		}
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0:
			c.fpuLoadF32(addr)
		case 2:
			c.fpuStoreF32(addr, c.fpuST(0))
		case 3:
			c.fpuStoreF32(addr, c.fpuST(0))
			c.fpuPop()
		case 4:
			c.fpuLoadEnv32(addr)
		case 5:
			c.fpuControlWord = c.readMem16(addr)
		case 6:
			c.fpuStoreEnv32(addr)
		case 7:
			c.writeMem16(addr, c.fpuControlWord)
		}

	case 0xDA:
		if mr.isReg {
			switch {
			case mr.reg == 5 && (mr.rm&7) == 1:
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(1))
				c.fpuPop()
				c.fpuPop()
			case mr.reg == 0:
				if c.rflags&RFLAGS_CF != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm&7)))
				}
			case mr.reg == 1:
				if c.rflags&RFLAGS_ZF != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm&7)))
				}
			case mr.reg == 2:
				if c.rflags&(RFLAGS_CF|RFLAGS_ZF) != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm&7)))
				}
			case mr.reg == 3:
				if c.rflags&RFLAGS_PF != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm&7)))
				}
			default:
				return fmt.Errorf("unsupported DA reg form: reg=%d rm=%d at RIP=%016X", mr.reg, mr.rm, c.rip-2)
			}
			return nil
		}
		v := int32(c.readMem32(c.segBaseForModRM(mr) + mr.ea))
		src := float64(v)
		dst := c.fpuST(0)
		if mr.reg == 2 || mr.reg == 3 {
			c.fpuCompareSetFlags(dst, src)
			if mr.reg == 3 {
				c.fpuPop()
			}
			return nil
		}
		res, write := fpuArith(mr.reg, dst, src)
		if write {
			c.fpuSetST(0, res)
		}

	case 0xDB:
		if mr.isReg {
			return c.handleX87_DB_reg(mr)
		}
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0:
			c.fpuLoadI32(addr)
		case 2:
			c.fpuStoreI32(addr, c.fpuST(0))
		case 3:
			c.fpuStoreI32(addr, c.fpuST(0))
			c.fpuPop()
		case 5:
			c.fpuPush(c.fpuLoad80(addr))
		case 7:
			c.fpuStore80(addr, c.fpuST(0))
			c.fpuPop()
		}

	case 0xDC:
		if mr.isReg {
			src := c.fpuST(0)
			dstIdx := int(mr.rm & 7)
			dst := c.fpuST(dstIdx)
			if mr.reg == 2 || mr.reg == 3 {
				c.fpuCompareSetFlags(dst, src)
				if mr.reg == 3 {
					c.fpuPop()
				}
				return nil
			}
			subop := mr.reg
			switch subop {
			case 4:
				subop = 5
			case 5:
				subop = 4
			case 6:
				subop = 7
			case 7:
				subop = 6
			}
			res, write := fpuArith(subop, dst, src)
			if write {
				c.fpuSetST(dstIdx, res)
			}
			return nil
		}
		src := math.Float64frombits(c.readMem64(c.segBaseForModRM(mr) + mr.ea))
		dst := c.fpuST(0)
		if mr.reg == 2 || mr.reg == 3 {
			c.fpuCompareSetFlags(dst, src)
			if mr.reg == 3 {
				c.fpuPop()
			}
			return nil
		}
		res, write := fpuArith(mr.reg, dst, src)
		if write {
			c.fpuSetST(0, res)
		}

	case 0xDD:
		if mr.isReg {
			switch mr.reg {
			case 0:
				c.fpuTagWrite(uint8(c.fpuPhysIdx(int(mr.rm&7))), fpuTagEmpty)
			case 2:
				c.fpuSetST(int(mr.rm&7), c.fpuST(0))
			case 3:
				c.fpuSetST(int(mr.rm&7), c.fpuST(0))
				c.fpuPop()
			case 4:
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(int(mr.rm&7)))
			case 5:
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(int(mr.rm&7)))
				c.fpuPop()
			}
			return nil
		}
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0:
			c.fpuLoadF64(addr)
		case 2:
			c.fpuStoreF64(addr, c.fpuST(0))
		case 3:
			c.fpuStoreF64(addr, c.fpuST(0))
			c.fpuPop()
		case 4:
			// FRSTOR — not modelled
		case 6:
			// FSAVE — not modelled
		case 7:
			c.writeMem16(addr, c.fpuStatusWord)
		}

	case 0xDE:
		if mr.isReg {
			src := c.fpuST(0)
			dstIdx := int(mr.rm & 7)
			dst := c.fpuST(dstIdx)
			if mr.reg == 3 && (mr.rm&7) == 1 {
				// FCOMPP (DE D9) — compare ST(0) with ST(1), pop both.
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(1))
				c.fpuPop()
				c.fpuPop()
				return nil
			}
			subop := mr.reg
			switch subop {
			case 4:
				subop = 5
			case 5:
				subop = 4
			case 6:
				subop = 7
			case 7:
				subop = 6
			}
			res, write := fpuArith(subop, dst, src)
			if write {
				c.fpuSetST(dstIdx, res)
			}
			c.fpuPop()
			return nil
		}
		v := int16(c.readMem16(c.segBaseForModRM(mr) + mr.ea))
		src := float64(v)
		dst := c.fpuST(0)
		if mr.reg == 2 || mr.reg == 3 {
			c.fpuCompareSetFlags(dst, src)
			if mr.reg == 3 {
				c.fpuPop()
			}
			return nil
		}
		res, write := fpuArith(mr.reg, dst, src)
		if write {
			c.fpuSetST(0, res)
		}

	case 0xDF:
		if mr.isReg {
			switch mr.reg {
			case 4:
				if (mr.rm & 7) == 0 {
					c.SetReg16(AX, c.fpuStatusWord)
				}
			case 5:
				c.fpuCompareSetFlagsRFlags(c.fpuST(0), c.fpuST(int(mr.rm&7)))
				c.fpuPop()
			case 6:
				c.fpuCompareSetFlagsRFlags(c.fpuST(0), c.fpuST(int(mr.rm&7)))
				c.fpuPop()
			}
			return nil
		}
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0:
			c.fpuLoadI16(addr)
		case 2:
			c.fpuStoreI16(addr, c.fpuST(0))
		case 3:
			c.fpuStoreI16(addr, c.fpuST(0))
			c.fpuPop()
		case 5:
			c.fpuLoadI64(addr)
		case 7:
			c.fpuStoreI64(addr, c.fpuST(0))
			c.fpuPop()
		}
	}
	return nil
}

// handleX87_D9_reg — D9 register-form opcodes.
func (c *CPU) handleX87_D9_reg(mr modRMResult) error {
	switch mr.reg {
	case 0:
		c.fpuPush(c.fpuST(int(mr.rm & 7)))
		return nil
	case 1:
		i := int(mr.rm & 7)
		t := c.fpuST(0)
		c.fpuSetST(0, c.fpuST(i))
		c.fpuSetST(i, t)
		return nil
	case 2:
		// FNOP and undocumented forms — treat as NOP.
		return nil
	}
	// reg = 4/5/6/7 + rm pick a misc op via the merged ModR/M byte.
	modrm := uint8(mr.reg<<3|(mr.rm&7)) | 0xC0
	switch modrm {
	case 0xE0: // FCHS
		c.fpuSetST(0, -c.fpuST(0))
	case 0xE1: // FABS
		c.fpuSetST(0, math.Abs(c.fpuST(0)))
	case 0xE4: // FTST
		c.fpuCompareSetFlags(c.fpuST(0), 0)
	case 0xE5: // FXAM
		v := c.fpuST(0)
		c.fpuStatusWord &^= (1 << 8) | (1 << 10) | (1 << 14)
		switch {
		case math.IsNaN(v):
			c.fpuStatusWord |= (1 << 8)
		case math.IsInf(v, 0):
			c.fpuStatusWord |= (1 << 8) | (1 << 10)
		case v == 0:
			c.fpuStatusWord |= 1 << 14
		default:
			c.fpuStatusWord |= 1 << 10
		}
		if math.Signbit(v) {
			c.fpuStatusWord |= 1 << 9
		} else {
			c.fpuStatusWord &^= 1 << 9
		}
	case 0xE8, 0xE9, 0xEA, 0xEB, 0xEC, 0xED, 0xEE: // FLD1 / FLDL2T / ... / FLDZ
		if v, ok := fpuConst(modrm - 0xE8); ok {
			c.fpuPush(v)
		}
	case 0xF0: // F2XM1
		c.fpuSetST(0, math.Exp2(c.fpuST(0))-1)
	case 0xF1: // FYL2X
		st1 := c.fpuST(1)
		st0 := c.fpuST(0)
		c.fpuSetST(1, st1*math.Log2(st0))
		c.fpuPop()
	case 0xF2: // FPTAN
		c.fpuSetST(0, math.Tan(c.fpuST(0)))
		c.fpuPush(1.0)
	case 0xF3: // FPATAN
		c.fpuSetST(1, math.Atan2(c.fpuST(1), c.fpuST(0)))
		c.fpuPop()
	case 0xF4: // FXTRACT
		v := c.fpuST(0)
		exp, frac := math.Frexp(v)
		c.fpuSetST(0, float64(frac))
		c.fpuPush(float64(exp))
	case 0xF5: // FPREM1
		c.fpuSetST(0, math.Remainder(c.fpuST(0), c.fpuST(1)))
	case 0xF8: // FPREM
		c.fpuSetST(0, math.Mod(c.fpuST(0), c.fpuST(1)))
	case 0xF9: // FYL2XP1
		c.fpuSetST(1, c.fpuST(1)*math.Log2(c.fpuST(0)+1))
		c.fpuPop()
	case 0xFA: // FSQRT
		c.fpuSetST(0, math.Sqrt(c.fpuST(0)))
	case 0xFB: // FSINCOS
		v := c.fpuST(0)
		c.fpuSetST(0, math.Sin(v))
		c.fpuPush(math.Cos(v))
	case 0xFC: // FRNDINT
		c.fpuSetST(0, math.RoundToEven(c.fpuST(0)))
	case 0xFD: // FSCALE
		c.fpuSetST(0, c.fpuST(0)*math.Exp2(math.Trunc(c.fpuST(1))))
	case 0xFE: // FSIN
		c.fpuSetST(0, math.Sin(c.fpuST(0)))
	case 0xFF: // FCOS
		c.fpuSetST(0, math.Cos(c.fpuST(0)))
	}
	return nil
}

// handleX87_DB_reg — DB register-form opcodes (control + FCMOVN* + FCOMI).
//
//   reg=0..3 rm=*  FCMOVN{B,E,BE,U} ST(0), ST(i)
//   reg=4 rm=2     FNCLEX
//   reg=4 rm=3     FNINIT
//   reg=5 rm=*     FUCOMI ST(0), ST(i)
//   reg=6 rm=*     FCOMI  ST(0), ST(i)
func (c *CPU) handleX87_DB_reg(mr modRMResult) error {
	switch mr.reg {
	case 0, 1, 2, 3:
		var cond bool
		switch mr.reg {
		case 0:
			cond = c.rflags&RFLAGS_CF == 0
		case 1:
			cond = c.rflags&RFLAGS_ZF == 0
		case 2:
			cond = c.rflags&(RFLAGS_CF|RFLAGS_ZF) == 0
		case 3:
			cond = c.rflags&RFLAGS_PF == 0
		}
		if cond {
			c.fpuSetST(0, c.fpuST(int(mr.rm&7)))
		}
	case 4:
		switch mr.rm & 7 {
		case 2: // FNCLEX
			c.fpuStatusWord = 0
		case 3: // FNINIT
			c.fpuReset()
		}
	case 5: // FUCOMI
		c.fpuCompareSetFlagsRFlags(c.fpuST(0), c.fpuST(int(mr.rm&7)))
	case 6: // FCOMI
		c.fpuCompareSetFlagsRFlags(c.fpuST(0), c.fpuST(int(mr.rm&7)))
	case 7:
		// Reserved.
	}
	return nil
}
