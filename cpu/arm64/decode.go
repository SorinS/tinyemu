package arm64

import "math/bits"

// width-aware bit helpers shared by the executor.

func widthOf(is64 bool) uint32 {
	if is64 {
		return 64
	}
	return 32
}

// onesMask returns a value with the low k bits set (k in 0..64).
func onesMask(k uint32) uint64 {
	if k >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << k) - 1
}

// rorWidth rotates the low `width` bits of v right by r.
func rorWidth(v uint64, r, width uint32) uint64 {
	mask := onesMask(width)
	v &= mask
	r %= width
	if r == 0 {
		return v
	}
	return ((v >> r) | (v << (width - r))) & mask
}

// decodeBitmask reconstructs a logical immediate from N/immr/imms (the inverse
// of the assembler's encodeBitmask). A self-contained copy so the CPU does not
// depend on the assembler package.
func decodeBitmask(n, imms, immr uint32, regSize int) (uint64, bool) {
	combined := (n << 6) | ((^imms) & 0x3F)
	if combined == 0 {
		return 0, false
	}
	length := 31 - bits.LeadingZeros32(combined)
	if length < 1 || length > 6 {
		return 0, false
	}
	esize := 1 << uint(length)
	if esize > regSize {
		return 0, false
	}
	levels := uint32((1 << uint(length)) - 1)
	s := imms & levels
	r := immr & levels
	if s == levels {
		return 0, false
	}
	elem := rorWidth(onesMask(s+1), r, uint32(esize))
	value := uint64(0)
	for i := 0; i < regSize; i += esize {
		value |= elem << uint(i)
	}
	return value, true
}

// bfmMasks computes the wmask/tmask of the ARM bitfield-move operation for a
// given imms/immr and register width (the DecodeBitMasks used by SBFM/BFM/UBFM,
// where N == sf so the element size equals the register width).
func bfmMasks(width, imms, immr uint32) (wmask, tmask uint64) {
	levels := width - 1
	s := imms & levels
	r := immr & levels
	diff := (s - r) & levels
	welem := onesMask(s + 1)
	telem := onesMask(diff + 1)
	wmask = rorWidth(welem, r, width)
	tmask = telem
	if width == 32 {
		wmask &= 0xFFFFFFFF
		tmask &= 0xFFFFFFFF
	}
	return
}

// extendReg applies an AArch64 extend (option) and left shift to a register
// value, used by the add/sub extended-register form.
func extendReg(val uint64, option, shift uint32) uint64 {
	var v uint64
	switch option {
	case 0: // uxtb
		v = val & 0xFF
	case 1: // uxth
		v = val & 0xFFFF
	case 2: // uxtw
		v = val & 0xFFFFFFFF
	case 3: // uxtx
		v = val
	case 4: // sxtb
		v = uint64(int64(int8(val)))
	case 5: // sxth
		v = uint64(int64(int16(val)))
	case 6: // sxtw
		v = uint64(int64(int32(val)))
	case 7: // sxtx
		v = val
	}
	return v << shift
}

// shiftReg applies a logical/arith/rotate shift to a register value for the
// shifted-register data-processing forms.
func shiftReg(val uint64, shiftType, amount, width uint32) uint64 {
	val &= onesMask(width)
	amount &= width - 1 // amounts are < width for these forms
	switch shiftType {
	case 0: // lsl
		return (val << amount) & onesMask(width)
	case 1: // lsr
		return val >> amount
	case 2: // asr
		if width == 32 {
			return uint64(uint32(int32(val) >> amount))
		}
		return uint64(int64(val) >> amount)
	default: // ror
		return rorWidth(val, amount, width)
	}
}

// signExtend sign-extends the low `bits` of v (bits in 1..64).
func signExtend(v uint64, b uint32) int64 {
	shift := 64 - b
	return int64(v<<shift) >> shift
}
