package arm64

import "math/bits"

// AArch64 logical-immediate (the "bitmask immediate") encoding.
//
// and/orr/eor/ands with an immediate don't carry the value literally. The
// operand must be a pattern that, across the register, is a replicated element
// of size e ∈ {2,4,8,16,32,64}, where each element is a rotation of a
// contiguous run of 1s (neither all-0 nor all-1). It is encoded as three
// fields: N (1 bit), immr (6, the rotation), imms (6, size + run length).
//
// This pair of functions is the encode/decode of that scheme; they are exact
// inverses (proven exhaustively in bitmask_test.go) and the encode side is
// anchored to llvm-mc on a sample.

// rorElem rotates the low `esize` bits of v right by r (within esize).
func rorElem(v uint64, r, esize int) uint64 {
	mask := ^uint64(0) >> uint(64-esize)
	v &= mask
	r %= esize
	if r == 0 {
		return v
	}
	return ((v >> uint(r)) | (v << uint(esize-r))) & mask
}

// encodeBitmask encodes imm as a logical immediate for a regSize-bit register
// (32 or 64). ok is false when imm is not a legal logical immediate (0,
// all-ones, or a non-replicable / non-contiguous pattern).
func encodeBitmask(imm uint64, regSize int) (n, immr, imms uint32, ok bool) {
	if regSize == 32 {
		if imm>>32 != 0 {
			return 0, 0, 0, false
		}
		imm |= imm << 32 // analyse as a 64-bit value with a 32-bit period
	}
	if imm == 0 || imm == ^uint64(0) {
		return 0, 0, 0, false
	}

	// Element size: the smallest power-of-two period the pattern repeats at.
	size := 64
	for {
		size >>= 1
		mask := (uint64(1) << uint(size)) - 1
		if (imm & mask) != ((imm >> uint(size)) & mask) {
			size <<= 1
			break
		}
		if size <= 2 {
			break
		}
	}
	esize := size

	eltMask := ^uint64(0) >> uint(64-esize)
	elt := imm & eltMask
	ones := bits.OnesCount64(elt)
	if ones == 0 || ones == esize {
		return 0, 0, 0, false // an all-0 or all-1 element is not encodable
	}

	// The element must be a rotation of the contiguous run 0^(esize-ones) 1^ones.
	// esize ≤ 64, so a direct search for the rotation is both trivial and
	// obviously correct (and yields the unique immr because esize is minimal).
	base := (uint64(1) << uint(ones)) - 1
	rot := -1
	for r := 0; r < esize; r++ {
		if rorElem(base, r, esize) == elt {
			rot = r
			break
		}
	}
	if rot < 0 {
		return 0, 0, 0, false
	}
	immr = uint32(rot)

	// imms top bits encode esize, low bits encode (ones-1); N=1 only for e=64.
	switch esize {
	case 64:
		n, imms = 1, uint32(ones-1)
	case 32:
		imms = uint32(ones - 1)
	case 16:
		imms = 0x20 | uint32(ones-1)
	case 8:
		imms = 0x30 | uint32(ones-1)
	case 4:
		imms = 0x38 | uint32(ones-1)
	case 2:
		imms = 0x3C | uint32(ones-1)
	}
	return n, immr, imms, true
}

// decodeBitmask is the inverse: it reconstructs the immediate value from the
// N/immr/imms fields for a regSize-bit register. ok is false for the field
// combinations that don't denote a valid logical immediate.
func decodeBitmask(n, imms, immr uint32, regSize int) (uint64, bool) {
	// len = highest set bit of the 7-bit (N : NOT(imms)).
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
		return 0, false // S+1 == esize → all-ones element, not allowed
	}
	elem := rorElem((uint64(1)<<uint(s+1))-1, int(r), esize)

	value := uint64(0)
	for i := 0; i < regSize; i += esize {
		value |= elem << uint(i)
	}
	return value, true
}
