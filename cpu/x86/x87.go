package x86

import (
	"encoding/binary"
	"fmt"
	"math"
)

// x87 FPU implementation.
//
// Modeled with 8 × float64 (not the architectural 80-bit extended
// precision). We lose 11 bits of mantissa vs. real hardware; almost no
// software relies on those bits. In return we get IEEE-754 arithmetic
// for free from Go.
//
// The register file is a stack. ST(i) means the register `i` slots
// from the current top. The physical index is (fpuTop + i) mod 8.
// Push (FLD) decrements fpuTop then writes; pop (FSTP/FFREE+POP)
// reads then increments. Status word bits 11-13 mirror fpuTop.
//
// Tag word: 2 bits per physical register, 00=valid 01=zero 10=special
// 11=empty. On reset all 11s; FNINIT explicit. After each push we set
// tags by examining the value's exponent/sign-bit-class.

// fpuTagValid / fpuTagZero / fpuTagSpecial / fpuTagEmpty are the
// per-register tag-word values per Intel SDM Vol 1 §8.1.7.
const (
	fpuTagValid   = 0
	fpuTagZero    = 1
	fpuTagSpecial = 2
	fpuTagEmpty   = 3
)

// CW masked-exception bits (low 6 bits of the control word). The
// kernel sets these so the FPU silently returns NaN/Inf for invalid
// ops rather than raising #MF.
const (
	cwMaskInvalid   = 1 << 0
	cwMaskDenormal  = 1 << 1
	cwMaskZeroDiv   = 1 << 2
	cwMaskOverflow  = 1 << 3
	cwMaskUnderflow = 1 << 4
	cwMaskPrecision = 1 << 5
)

// fpuReset clears the FPU state to architectural reset (post-FNINIT).
func (c *CPU) fpuReset() {
	for i := range c.fpu {
		c.fpu[i] = 0
	}
	c.fpuTop = 0
	c.fpuTag = 0xFFFF // all 11 (empty)
	c.fpuStatusWord = 0
	c.fpuControlWord = 0x037F // default: round-to-nearest, all exceptions masked
	c.fpuInitialized = true
}

// fpuPhysIdx maps architectural ST(i) to the physical register index.
func (c *CPU) fpuPhysIdx(stI int) int {
	return int((uint(c.fpuTop) + uint(stI)) & 7)
}

// fpuST returns the value of ST(i).
func (c *CPU) fpuST(i int) float64 {
	return c.fpu[c.fpuPhysIdx(i)]
}

// fpuSetST writes ST(i) and tags the register based on the value's class.
func (c *CPU) fpuSetST(i int, v float64) {
	idx := c.fpuPhysIdx(i)
	c.fpu[idx] = v
	c.fpuTagWrite(uint8(idx), classifyForTag(v))
}

// fpuTagWrite sets the 2-bit tag for physical register `phys` to `tag`.
func (c *CPU) fpuTagWrite(phys, tag uint8) {
	shift := phys * 2
	c.fpuTag = (c.fpuTag &^ (3 << shift)) | (uint16(tag&3) << shift)
}

// fpuTagRead returns the 2-bit tag for physical register `phys`.
func (c *CPU) fpuTagRead(phys uint8) uint8 {
	return uint8((c.fpuTag >> (phys * 2)) & 3)
}

// classifyForTag classifies a float64 into one of the tag-word values.
func classifyForTag(v float64) uint8 {
	switch {
	case v == 0:
		return fpuTagZero
	case math.IsNaN(v) || math.IsInf(v, 0):
		return fpuTagSpecial
	default:
		// Denormals get tagged as special too, but Go's float64 doesn't
		// expose subnormal classification cheaply; treat denormals as
		// "valid" — almost no software cares since we mask all
		// exceptions anyway.
		return fpuTagValid
	}
}

// fpuPush pushes `v` onto the stack: fpuTop = (fpuTop-1) & 7, ST(0)=v.
func (c *CPU) fpuPush(v float64) {
	c.fpuTop = (c.fpuTop - 1) & 7
	c.fpu[c.fpuTop] = v
	c.fpuTagWrite(c.fpuTop, classifyForTag(v))
	c.fpuStatusWriteTop()
}

// fpuPop pops the top: tag(top)=empty, fpuTop = (fpuTop+1) & 7. Returns
// the popped value.
func (c *CPU) fpuPop() float64 {
	v := c.fpu[c.fpuTop]
	c.fpuTagWrite(c.fpuTop, fpuTagEmpty)
	c.fpuTop = (c.fpuTop + 1) & 7
	c.fpuStatusWriteTop()
	return v
}

// fpuStatusWriteTop refreshes the TOP field (bits 11-13) of the status word.
func (c *CPU) fpuStatusWriteTop() {
	c.fpuStatusWord = (c.fpuStatusWord &^ 0x3800) | (uint16(c.fpuTop) << 11)
}

// fpuLoadF32 reads a 32-bit IEEE float from memory and pushes it.
func (c *CPU) fpuLoadF32(addr uint32) {
	bits := c.readMem32(addr)
	v := float64(math.Float32frombits(bits))
	c.fpuPush(v)
}

// fpuLoadF64 reads a 64-bit IEEE double from memory and pushes it.
func (c *CPU) fpuLoadF64(addr uint32) {
	lo := uint64(c.readMem32(addr))
	hi := uint64(c.readMem32(addr + 4))
	bits := lo | hi<<32
	c.fpuPush(math.Float64frombits(bits))
}

// fpuStoreF32 writes ST(0) (or top, depending on caller) as a 32-bit
// float to memory.
func (c *CPU) fpuStoreF32(addr uint32, v float64) {
	bits := math.Float32bits(float32(v))
	c.writeMem32(addr, bits)
}

// fpuStoreF64 writes a value as a 64-bit double to memory.
func (c *CPU) fpuStoreF64(addr uint32, v float64) {
	bits := math.Float64bits(v)
	c.writeMem32(addr, uint32(bits))
	c.writeMem32(addr+4, uint32(bits>>32))
}

// fpuLoadI16 reads a 16-bit signed integer from memory and pushes it
// as a float64. Used by FILD m16.
func (c *CPU) fpuLoadI16(addr uint32) {
	v := int16(c.readMem16(addr))
	c.fpuPush(float64(v))
}

// fpuLoadI32 reads a 32-bit signed integer and pushes it.
func (c *CPU) fpuLoadI32(addr uint32) {
	v := int32(c.readMem32(addr))
	c.fpuPush(float64(v))
}

// fpuLoadI64 reads a 64-bit signed integer and pushes it.
func (c *CPU) fpuLoadI64(addr uint32) {
	lo := uint64(c.readMem32(addr))
	hi := uint64(c.readMem32(addr + 4))
	v := int64(lo | hi<<32)
	c.fpuPush(float64(v))
}

// fpuStoreI16 / I32 / I64 — store with round-to-integer per the
// current rounding mode (we treat as round-to-nearest for simplicity).
func (c *CPU) fpuStoreI16(addr uint32, v float64) {
	c.writeMem16(addr, uint16(int16(roundIntegerForStore(v))))
}

func (c *CPU) fpuStoreI32(addr uint32, v float64) {
	c.writeMem32(addr, uint32(int32(roundIntegerForStore(v))))
}

func (c *CPU) fpuStoreI64(addr uint32, v float64) {
	r := int64(roundIntegerForStore(v))
	c.writeMem32(addr, uint32(r))
	c.writeMem32(addr+4, uint32(uint64(r)>>32))
}

// roundIntegerForStore picks an integer representation of `v` based on
// the FPU control word's rounding-mode bits (CW.RC, bits 10-11). We
// don't honor the rounding mode pedantically — round-to-nearest is
// the common case for `printf %d` (which truncates anyway via FISTTP)
// and for kernel use. Soft-FP correctness would require a full impl.
func roundIntegerForStore(v float64) float64 {
	return math.RoundToEven(v)
}

// fpuArith performs an FPU arithmetic operation indexed by `subop` in
// the range 0..7, matching the D8/DC/D9 family encoding:
//   0 = FADD,  1 = FMUL,  4 = FSUB, 5 = FSUBR, 6 = FDIV, 7 = FDIVR
//   2 = FCOM,  3 = FCOMP (compare; result not written)
// `dst` and `src` are the input values; returns (result, writeBack).
func fpuArith(subop uint8, dst, src float64) (float64, bool) {
	switch subop {
	case 0: // FADD
		return dst + src, true
	case 1: // FMUL
		return dst * src, true
	case 2, 3: // FCOM / FCOMP — compare only (caller handles flag)
		return 0, false
	case 4: // FSUB: dst - src
		return dst - src, true
	case 5: // FSUBR: src - dst
		return src - dst, true
	case 6: // FDIV: dst / src
		return dst / src, true
	case 7: // FDIVR: src / dst
		return src / dst, true
	}
	return 0, false
}

// fpuCompareSetFlags updates the C0/C2/C3 condition codes in the
// status word based on the comparison of two float64 values, per
// Intel SDM Table 4-9 (FCOM result encoding).
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
	default: // equal
		c3, c2, c0 = true, false, false
	}
	c.fpuStatusWord &^= (1 << 8) | (1 << 10) | (1 << 14) // clear C0,C2,C3
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

// fpuConst returns the named constant used by the FLDZ/FLD1/etc. group.
// `subop` is the low 3 bits of the D9 modrm byte for opcodes E8..EF.
func fpuConst(subop uint8) (float64, bool) {
	switch subop {
	case 0: // FLD1
		return 1.0, true
	case 1: // FLDL2T = log2(10)
		return math.Log2(10), true
	case 2: // FLDL2E = log2(e)
		return math.Log2(math.E), true
	case 3: // FLDPI
		return math.Pi, true
	case 4: // FLDLG2 = log10(2)
		return math.Log10(2), true
	case 5: // FLDLN2 = ln(2)
		return math.Ln2, true
	case 6: // FLDZ
		return 0.0, true
	}
	return 0, false
}

// fldenv32 / fnstenv32 placeholders. These store/load the full FPU
// environment to/from memory in the 28-byte protected-mode format.
// We don't need a faithful environment representation — Linux uses
// fnstenv mostly inside _exit cleanup and signal handling. Filling
// the right slots prevents the kernel from interpreting a partial
// dump as a valid pending exception.
func (c *CPU) fpuStoreEnv32(addr uint32) {
	// Bytes 0-1: FPU control word.
	c.writeMem32(addr, uint32(c.fpuControlWord))
	// Bytes 4-5: FPU status word.
	c.writeMem32(addr+4, uint32(c.fpuStatusWord))
	// Bytes 8-9: FPU tag word.
	c.writeMem32(addr+8, uint32(c.fpuTag))
	// Bytes 12-23: FPU instruction pointer, opcode, data pointer. We
	// don't track these so write zeros — Linux only uses them in the
	// signal frame (cleared on entry).
	c.writeMem32(addr+12, 0)
	c.writeMem32(addr+16, 0)
	c.writeMem32(addr+20, 0)
	c.writeMem32(addr+24, 0)
}

func (c *CPU) fpuLoadEnv32(addr uint32) {
	c.fpuControlWord = uint16(c.readMem32(addr))
	c.fpuStatusWord = uint16(c.readMem32(addr + 4))
	c.fpuTag = uint16(c.readMem32(addr + 8))
	c.fpuTop = uint8((c.fpuStatusWord >> 11) & 7)
}

// fxsave writes a 512-byte FXSAVE area at `addr`. The layout matches
// Intel SDM Vol. 1 §10.5.1 for 32-bit non-REX form. We populate the
// fields that real software actually inspects:
//
//   0   FCW       16    FPU control word
//   2   FSW       16    FPU status word (TOP bits 11-13)
//   4   FTW       8     abridged tag word (1 bit per ST: 0=empty, 1=valid)
//   5   reserved  8
//   6   FOP       16    last x87 opcode — 0
//   8   FIP       32    last x87 instruction pointer — 0
//   12  FCS       16    x87 CS — 0
//   16  FDP       32    last data pointer — 0
//   20  FDS       16    x87 DS — 0
//   24  MXCSR     32
//   28  MXCSR_MASK 32   0x0000FFBF
//   32  ST(0)..ST(7)    each 80-bit in 16-byte slots (low 10 used)
//   160 XMM0..XMM7      each 128-bit
//
// Linux's kernel context switch uses FXSAVE/FXRSTOR to save/restore
// per-thread FPU state on EVERY task switch. Stubbing these to NOP
// caused every preemption of a long-running FP user (busybox awk) to
// corrupt the FPU registers — silent random branches in awk's `if`
// statements. With this real implementation, awk's state survives.
func (c *CPU) fxsave(addr uint32) {
	c.fpuStatusWriteTop()
	c.writeMem16(addr+0, c.fpuControlWord)
	c.writeMem16(addr+2, c.fpuStatusWord)
	// Build abridged tag word: 1 bit per ST register, set when not empty.
	// Our c.fpuTag uses 2 bits per ST (00=valid, 01=zero, 10=special, 11=empty).
	var ftw uint8
	for i := 0; i < 8; i++ {
		twoBit := (c.fpuTag >> (uint(i) * 2)) & 3
		if twoBit != 3 {
			ftw |= 1 << uint(i)
		}
	}
	c.writeMem8(addr+4, ftw)
	c.writeMem8(addr+5, 0)
	c.writeMem16(addr+6, 0)   // FOP
	c.writeMem32(addr+8, 0)   // FIP
	c.writeMem16(addr+12, 0)  // FCS
	c.writeMem16(addr+14, 0)
	c.writeMem32(addr+16, 0)  // FDP
	c.writeMem16(addr+20, 0)  // FDS
	c.writeMem16(addr+22, 0)
	c.writeMem32(addr+24, c.mxcsr)
	c.writeMem32(addr+28, 0x0000FFBF)
	// ST registers in physical (not stack) order. Each at offset 32+i*16.
	for i := 0; i < 8; i++ {
		c.fpuStore80(addr+uint32(32+i*16), c.fpu[i])
		// Zero the upper 6 bytes of the 16-byte slot.
		c.writeMem16(addr+uint32(32+i*16+10), 0)
		c.writeMem32(addr+uint32(32+i*16+12), 0)
	}
	// XMM registers at offset 160 + i*16.
	for i := 0; i < 8; i++ {
		off := addr + uint32(160+i*16)
		c.writeMem32(off+0, uint32(c.xmm[i][0]))
		c.writeMem32(off+4, uint32(c.xmm[i][0]>>32))
		c.writeMem32(off+8, uint32(c.xmm[i][1]))
		c.writeMem32(off+12, uint32(c.xmm[i][1]>>32))
	}
}

// fxrstor reads a 512-byte FXSAVE area at `addr` back into the FPU and
// XMM state. Inverse of fxsave.
func (c *CPU) fxrstor(addr uint32) {
	c.fpuControlWord = c.readMem16(addr + 0)
	c.fpuStatusWord = c.readMem16(addr + 2)
	c.fpuTop = uint8((c.fpuStatusWord >> 11) & 7)
	ftw := c.readMem8(addr + 4)
	// Expand 1-bit-per-ST abridged tag to 2-bit-per-ST: 0=empty (3),
	// 1=not empty (treat as 0=valid).
	var tag uint16
	for i := 0; i < 8; i++ {
		if ftw&(1<<uint(i)) == 0 {
			tag |= 3 << (uint(i) * 2)
		}
	}
	c.fpuTag = tag
	c.mxcsr = c.readMem32(addr + 24)
	for i := 0; i < 8; i++ {
		c.fpu[i] = c.fpuLoad80(addr + uint32(32+i*16))
	}
	for i := 0; i < 8; i++ {
		off := addr + uint32(160+i*16)
		lo := uint64(c.readMem32(off+0)) | uint64(c.readMem32(off+4))<<32
		hi := uint64(c.readMem32(off+8)) | uint64(c.readMem32(off+12))<<32
		c.xmm[i] = [2]uint64{lo, hi}
	}
}

// fpuStore80 / fpuLoad80 — 80-bit extended-precision store/load. We
// don't model 80-bit but musl uses these for printf("%Lf") and some
// transcendental wrappers. Emit a best-effort 80-bit-format encoding
// of the float64 value, and decode by dropping the extra precision.
func (c *CPU) fpuStore80(addr uint32, v float64) {
	bits := math.Float64bits(v)
	// 64-bit IEEE: sign(1) | exp(11) | frac(52)
	sign := uint16((bits >> 63) & 1)
	exp := uint16((bits >> 52) & 0x7FF)
	frac := bits & ((1 << 52) - 1)

	// 80-bit extended: sign(1) | exp(15) | int(1) | frac(63).
	var ext80Lo uint64 // bits 0-63 (integer + fraction)
	var ext80Hi uint16 // bits 64-79 (sign + exponent)

	switch {
	case exp == 0x7FF && frac == 0:
		// Inf
		ext80Hi = (sign << 15) | 0x7FFF
		ext80Lo = 1 << 63
	case exp == 0x7FF:
		// NaN
		ext80Hi = (sign << 15) | 0x7FFF
		ext80Lo = (1 << 63) | (frac << 11)
	case exp == 0:
		// Zero / denormal — emit as 80-bit zero.
		ext80Hi = sign << 15
		ext80Lo = 0
	default:
		// Normal: rebias exponent (1023 → 16383).
		newExp := uint16(int(exp) - 1023 + 16383)
		ext80Hi = (sign << 15) | (newExp & 0x7FFF)
		ext80Lo = (1 << 63) | (frac << 11) // explicit integer bit + frac shifted
	}

	c.writeMem32(addr, uint32(ext80Lo))
	c.writeMem32(addr+4, uint32(ext80Lo>>32))
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], ext80Hi)
	c.writeMem16(addr+8, ext80Hi)
}

// handleX87 dispatches a single x87 opcode (D8-DF). Returns an error
// only for opcodes we explicitly reject; unimplemented ops fall back
// to NOP-with-ModRM-consume to keep the decoder in sync.
func (c *CPU) handleX87(opcode uint8) error {
	mr := c.parseModRM()

	switch opcode {
	case 0xD8:
		// D8 family: ST(0) := ST(0) op src, src is m32 or ST(i).
		var src float64
		if mr.isReg {
			src = c.fpuST(int(mr.rm))
		} else {
			bits := c.readMem32(c.segBaseForModRM(mr) + mr.ea)
			src = float64(math.Float32frombits(bits))
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
		// D9 /n m32: load/store float32 + control.
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0: // FLD m32
			c.fpuLoadF32(addr)
		case 2: // FST m32
			c.fpuStoreF32(addr, c.fpuST(0))
		case 3: // FSTP m32
			c.fpuStoreF32(addr, c.fpuST(0))
			c.fpuPop()
		case 4: // FLDENV m28
			c.fpuLoadEnv32(addr)
		case 5: // FLDCW m16
			c.fpuControlWord = c.readMem16(addr)
		case 6: // FNSTENV m28
			c.fpuStoreEnv32(addr)
		case 7: // FNSTCW m16
			c.writeMem16(addr, c.fpuControlWord)
		}

	case 0xDA:
		// DA /n m32 (signed int32): ST(0) := ST(0) op (int32) src.
		// DA reg form (mod=11) is FCMOVcc / FUCOMPP. busybox awk's
		// `istrue(v)` for numeric vars compiles to `fldz; fldl v; fucompp;
		// fnstsw ax; sahf; setne al` — stubbing FUCOMPP made every
		// `if (numeric_expr)` take the THEN branch.
		if mr.isReg {
			switch {
			case mr.reg == 5 && mr.rm == 1:
				// FUCOMPP: compare ST(0) with ST(1), pop both.
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(1))
				c.fpuPop()
				c.fpuPop()
			case mr.reg == 0: // FCMOVB ST(0), ST(i)  (CF=1)
				if c.eflags&EFLAGS_CF != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm)))
				}
			case mr.reg == 1: // FCMOVE ST(0), ST(i)  (ZF=1)
				if c.eflags&EFLAGS_ZF != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm)))
				}
			case mr.reg == 2: // FCMOVBE ST(0), ST(i) (CF=1 OR ZF=1)
				if c.eflags&(EFLAGS_CF|EFLAGS_ZF) != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm)))
				}
			case mr.reg == 3: // FCMOVU ST(0), ST(i)  (PF=1)
				if c.eflags&EFLAGS_PF != 0 {
					c.fpuSetST(0, c.fpuST(int(mr.rm)))
				}
			default:
				return fmt.Errorf("unsupported DA reg form: reg=%d rm=%d at EIP=%08X", mr.reg, mr.rm, c.eip-2)
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
		case 0: // FILD m32
			c.fpuLoadI32(addr)
		case 2: // FIST m32
			c.fpuStoreI32(addr, c.fpuST(0))
		case 3: // FISTP m32
			c.fpuStoreI32(addr, c.fpuST(0))
			c.fpuPop()
		case 5: // FLD m80
			c.fpuPush(c.fpuLoad80(addr))
		case 7: // FSTP m80
			c.fpuStore80(addr, c.fpuST(0))
			c.fpuPop()
		}

	case 0xDC:
		// DC family: same as D8 but with 64-bit memory operand or
		// ST(i) destination form.
		if mr.isReg {
			// DC reg form: ST(i) := ST(i) op ST(0). FSUBR/FDIVR
			// swapped relative to D8 conventions (because the
			// "destination" is now ST(i)).
			src := c.fpuST(0)
			dstIdx := int(mr.rm)
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
		// DC /n m64
		lo := uint64(c.readMem32(c.segBaseForModRM(mr) + mr.ea))
		hi := uint64(c.readMem32(c.segBaseForModRM(mr) + mr.ea + 4))
		src := math.Float64frombits(lo | hi<<32)
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
			case 0: // FFREE ST(i): tag = empty
				c.fpuTagWrite(uint8(c.fpuPhysIdx(int(mr.rm))), fpuTagEmpty)
			case 2: // FST ST(i)
				c.fpuSetST(int(mr.rm), c.fpuST(0))
			case 3: // FSTP ST(i)
				c.fpuSetST(int(mr.rm), c.fpuST(0))
				c.fpuPop()
			case 4: // FUCOM
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(int(mr.rm)))
			case 5: // FUCOMP
				c.fpuCompareSetFlags(c.fpuST(0), c.fpuST(int(mr.rm)))
				c.fpuPop()
			}
			return nil
		}
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0: // FLD m64
			c.fpuLoadF64(addr)
		case 2: // FST m64
			c.fpuStoreF64(addr, c.fpuST(0))
		case 3: // FSTP m64
			c.fpuStoreF64(addr, c.fpuST(0))
			c.fpuPop()
		case 4: // FRSTOR — fall back to NOP (we don't model save area)
			// Skip the 108-byte env+register dump.
		case 6: // FSAVE
			// Skip.
		case 7: // FNSTSW m16
			c.writeMem16(addr, c.fpuStatusWord)
		}

	case 0xDE:
		// DE family: same as D8 but operand is m16 signed int OR
		// ST(i)-destined "and pop" variant.
		if mr.isReg {
			// DE reg form: ST(i) := ST(i) op ST(0), then POP.
			src := c.fpuST(0)
			dstIdx := int(mr.rm)
			dst := c.fpuST(dstIdx)
			if mr.reg == 3 && mr.rm == 1 {
				// FCOMPP — modrm D9 (mod=11 reg=011 rm=001)
				c.fpuCompareSetFlags(dst, src)
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
		// DE /n m16 (signed int16)
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
			// DF E0 = FNSTSW AX (mod=11 reg=100 rm=000).
			if mr.reg == 4 && mr.rm == 0 {
				c.SetReg16(AX, c.fpuStatusWord)
			}
			return nil
		}
		addr := c.segBaseForModRM(mr) + mr.ea
		switch mr.reg {
		case 0: // FILD m16
			c.fpuLoadI16(addr)
		case 2: // FIST m16
			c.fpuStoreI16(addr, c.fpuST(0))
		case 3: // FISTP m16
			c.fpuStoreI16(addr, c.fpuST(0))
			c.fpuPop()
		case 5: // FILD m64
			c.fpuLoadI64(addr)
		case 7: // FISTP m64
			c.fpuStoreI64(addr, c.fpuST(0))
			c.fpuPop()
		}
	}
	return nil
}

// handleX87_D9_reg covers the D9 register-form opcodes: ST(i) ops,
// constants, transcendentals, control. The opcode-byte-level
// encoding is `D9 modrm` where modrm.mod = 11, modrm.reg + modrm.rm
// together select the operation.
func (c *CPU) handleX87_D9_reg(mr modRMResult) error {
	switch mr.reg {
	case 0: // FLD ST(i)
		c.fpuPush(c.fpuST(int(mr.rm)))
		return nil
	case 1: // FXCH ST(i)
		i := int(mr.rm)
		t := c.fpuST(0)
		c.fpuSetST(0, c.fpuST(i))
		c.fpuSetST(i, t)
		return nil
	case 2: // FNOP, etc.
		// Only D9 D0 = FNOP is defined; all other D9 /2 reg are
		// undocumented. Treat as NOP.
		return nil
	}
	// reg field of 4, 5, 6, 7 with modrm bytes E0..FF encode misc ops.
	// modrm.rm narrows further.
	modrm := uint8(mr.reg<<3 | mr.rm) | 0xC0
	switch modrm {
	case 0xE0: // FCHS
		c.fpuSetST(0, -c.fpuST(0))
	case 0xE1: // FABS
		c.fpuSetST(0, math.Abs(c.fpuST(0)))
	case 0xE4: // FTST: compare ST(0) with +0.0
		c.fpuCompareSetFlags(c.fpuST(0), 0)
	case 0xE5: // FXAM: classify ST(0). We set C0/C2/C3 minimally.
		v := c.fpuST(0)
		c.fpuStatusWord &^= (1 << 8) | (1 << 10) | (1 << 14)
		switch {
		case math.IsNaN(v):
			c.fpuStatusWord |= (1 << 8) // C0=1
		case math.IsInf(v, 0):
			c.fpuStatusWord |= (1 << 8) | (1 << 10) // C0=1 C2=1
		case v == 0:
			c.fpuStatusWord |= 1 << 14 // C3=1
		default:
			c.fpuStatusWord |= 1 << 10 // C2=1 (normal)
		}
		if math.Signbit(v) {
			c.fpuStatusWord |= 1 << 9 // C1 = sign
		} else {
			c.fpuStatusWord &^= 1 << 9
		}
	case 0xE8, 0xE9, 0xEA, 0xEB, 0xEC, 0xED, 0xEE: // FLD1 / FLDL2T / FLDL2E / FLDPI / FLDLG2 / FLDLN2 / FLDZ
		if v, ok := fpuConst(modrm - 0xE8); ok {
			c.fpuPush(v)
		}
	case 0xF0: // F2XM1: 2^ST(0) - 1
		c.fpuSetST(0, math.Exp2(c.fpuST(0))-1)
	case 0xF1: // FYL2X: ST(1) := ST(1) * log2(ST(0)); pop
		st1 := c.fpuST(1)
		st0 := c.fpuST(0)
		c.fpuSetST(1, st1*math.Log2(st0))
		c.fpuPop()
	case 0xF2: // FPTAN: tan(ST(0)); push 1.0
		c.fpuSetST(0, math.Tan(c.fpuST(0)))
		c.fpuPush(1.0)
	case 0xF3: // FPATAN: ST(1) := atan2(ST(1), ST(0)); pop
		c.fpuSetST(1, math.Atan2(c.fpuST(1), c.fpuST(0)))
		c.fpuPop()
	case 0xF4: // FXTRACT — split ST(0) into exponent and mantissa
		v := c.fpuST(0)
		exp, frac := math.Frexp(v)
		c.fpuSetST(0, float64(frac))
		c.fpuPush(float64(exp))
	case 0xF5: // FPREM1
		c.fpuSetST(0, math.Remainder(c.fpuST(0), c.fpuST(1)))
	case 0xF8: // FPREM
		c.fpuSetST(0, math.Mod(c.fpuST(0), c.fpuST(1)))
	case 0xF9: // FYL2XP1: ST(1) := ST(1) * log2(ST(0)+1); pop
		c.fpuSetST(1, c.fpuST(1)*math.Log2(c.fpuST(0)+1))
		c.fpuPop()
	case 0xFA: // FSQRT
		c.fpuSetST(0, math.Sqrt(c.fpuST(0)))
	case 0xFB: // FSINCOS: push cos(ST(0)) after sin(ST(0))
		v := c.fpuST(0)
		c.fpuSetST(0, math.Sin(v))
		c.fpuPush(math.Cos(v))
	case 0xFC: // FRNDINT
		c.fpuSetST(0, math.RoundToEven(c.fpuST(0)))
	case 0xFD: // FSCALE: ST(0) := ST(0) * 2^trunc(ST(1))
		c.fpuSetST(0, c.fpuST(0)*math.Exp2(math.Trunc(c.fpuST(1))))
	case 0xFE: // FSIN
		c.fpuSetST(0, math.Sin(c.fpuST(0)))
	case 0xFF: // FCOS
		c.fpuSetST(0, math.Cos(c.fpuST(0)))
	}
	return nil
}

// handleX87_DB_reg covers DB register-form opcodes (control + FCMOV).
func (c *CPU) handleX87_DB_reg(mr modRMResult) error {
	modrm := uint8(mr.reg<<3 | mr.rm) | 0xC0
	switch modrm {
	case 0xE0: // FNENI (8087 backwards-compat, NOP since 80387)
	case 0xE1: // FNDISI — same
	case 0xE2: // FNCLEX
		c.fpuStatusWord = 0
	case 0xE3: // FNINIT
		c.fpuReset()
	case 0xE4: // FNSETPM (NOP)
	}
	// FCMOVcc ST(0), ST(i): conditional move based on EFLAGS.
	// /0 = FCMOVB, /1 = FCMOVE, /2 = FCMOVBE, /3 = FCMOVU
	if mr.reg < 4 {
		var cond bool
		switch mr.reg {
		case 0: // CF=1
			cond = c.eflags&EFLAGS_CF != 0
		case 1: // ZF=1
			cond = c.eflags&EFLAGS_ZF != 0
		case 2: // CF=1 OR ZF=1
			cond = c.eflags&(EFLAGS_CF|EFLAGS_ZF) != 0
		case 3: // PF=1
			cond = c.eflags&EFLAGS_PF != 0
		}
		if cond {
			c.fpuSetST(0, c.fpuST(int(mr.rm)))
		}
	}
	return nil
}

func (c *CPU) fpuLoad80(addr uint32) float64 {
	lo := uint64(c.readMem32(addr))
	hi := uint64(c.readMem32(addr + 4))
	ext80Lo := lo | hi<<32
	ext80Hi := uint64(c.readMem16(addr + 8))
	sign := (ext80Hi >> 15) & 1
	exp := ext80Hi & 0x7FFF
	frac := ext80Lo & ((1 << 63) - 1)
	// Convert to float64 (rebias exponent, narrow fraction).
	var bits uint64
	switch {
	case exp == 0x7FFF && frac == 0:
		bits = sign<<63 | 0x7FF<<52
	case exp == 0x7FFF:
		bits = sign<<63 | 0x7FF<<52 | (frac >> 11)
	case exp == 0:
		bits = sign << 63 // zero / underflow
	default:
		newExp := int(exp) - 16383 + 1023
		if newExp <= 0 {
			bits = sign << 63 // underflow to ±0
		} else if newExp >= 0x7FF {
			bits = sign<<63 | 0x7FF<<52 // overflow to ±Inf
		} else {
			bits = sign<<63 | uint64(newExp)<<52 | (frac >> 11)
		}
	}
	return math.Float64frombits(bits)
}
