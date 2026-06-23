package arm64

import (
	"fmt"
	"strings"
)

// arrangementName reverses parseVecReg's arrangement: (Q, size) -> suffix.
func arrangementName(q, size uint32) string {
	switch size {
	case 0b00:
		if q == 1 {
			return "16b"
		}
		return "8b"
	case 0b01:
		if q == 1 {
			return "8h"
		}
		return "4h"
	case 0b10:
		if q == 1 {
			return "4s"
		}
		return "2s"
	default: // 0b11
		if q == 1 {
			return "2d"
		}
		return "1d"
	}
}

func vecName(n, q, size uint32) string {
	return fmt.Sprintf("v%d.%s", n, arrangementName(q, size))
}

// disSIMD dispatches the Advanced SIMD data group: across-lanes reductions,
// three-same and the copy group.
func disSIMD(w uint32) (string, error) {
	switch {
	case (w>>17)&0x1F == 0x18 && (w>>10)&3 == 0b10:
		return disSIMDAcross(w)
	case (w>>17)&0x1F == 0x10 && (w>>10)&3 == 0b10:
		return disSIMD2RegMisc(w)
	case (w>>29)&1 == 0 && (w>>21)&1 == 0 && (w>>15)&1 == 0 && (w>>10)&3 == 0b10:
		return disSIMDPermute(w)
	case (w>>29)&1 == 1 && (w>>21)&7 == 0 && (w>>15)&1 == 0 && (w>>10)&1 == 0:
		return disSIMDExt(w)
	case (w>>21)&1 == 1 && (w>>10)&1 == 1:
		return disSIMD3(w)
	case (w>>21)&7 == 0 && (w>>15)&1 == 0 && (w>>10)&1 == 1:
		return disSIMDCopy(w)
	}
	return "", fmt.Errorf("arm64 disasm: unsupported Adv-SIMD encoding %08x", w)
}

// byElemName names a by-element op from (U, opcode).
func byElemName(u, opcode uint32) (string, bool) {
	switch {
	case u == 0 && opcode == 0x8:
		return "mul", true
	case u == 1 && opcode == 0x0:
		return "mla", true
	case u == 1 && opcode == 0x4:
		return "mls", true
	case u == 0 && opcode == 0x9:
		return "fmul", true
	case u == 0 && opcode == 0x1:
		return "fmla", true
	case u == 0 && opcode == 0x5:
		return "fmls", true
	}
	return "", false
}

// disSIMDByElem decodes a vector-by-element op (mul/fmla/… Vd.T, Vn.T, Vm.Ts[i]).
func disSIMDByElem(w uint32) (string, error) {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	l := (w >> 21) & 1
	m := (w >> 20) & 1
	rm4 := (w >> 16) & 0xF
	opcode := (w >> 12) & 0xF
	h := (w >> 11) & 1
	rn, rd := (w>>5)&0x1F, w&0x1F
	mnem, ok := byElemName(u, opcode)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: unsupported by-element op %08x", w)
	}
	var index, rm uint32
	switch size {
	case 0b01:
		index, rm = h<<2|l<<1|m, rm4
	case 0b10:
		index, rm = h<<1|l, m<<4|rm4
	default:
		index, rm = h, m<<4|rm4
	}
	return fmt.Sprintf("%s %s, %s, %s", mnem,
		vecName(rd, q, size), vecName(rn, q, size), vecElemName(rm, size, index)), nil
}

// disSIMDExt decodes EXT.
func disSIMDExt(w uint32) (string, error) {
	q := (w >> 30) & 1
	index := (w >> 11) & 0xF
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	v := func(n uint32) string { return vecName(n, q, 0) } // .8b / .16b
	return fmt.Sprintf("ext %s, %s, %s, #%d", v(rd), v(rn), v(rm), index), nil
}

// permuteName names a permute op from its 3-bit opcode.
var permuteName = map[uint32]string{
	0b001: "uzp1", 0b010: "trn1", 0b011: "zip1",
	0b101: "uzp2", 0b110: "trn2", 0b111: "zip2",
}

// disSIMDPermute decodes zip1/zip2/uzp1/uzp2/trn1/trn2.
func disSIMDPermute(w uint32) (string, error) {
	q := (w >> 30) & 1
	size := (w >> 22) & 3
	opcode := (w >> 12) & 7
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	mnem, ok := permuteName[opcode]
	if !ok {
		return "", fmt.Errorf("arm64 disasm: unsupported permute opcode %08x", w)
	}
	return fmt.Sprintf("%s %s, %s, %s", mnem,
		vecName(rd, q, size), vecName(rn, q, size), vecName(rm, q, size)), nil
}

// disSIMD2RegMisc decodes abs/neg/cnt/not.
func disSIMD2RegMisc(w uint32) (string, error) {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	opcode := (w >> 12) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	// The int↔FP conversions use bit22 alone as the element size (single/double).
	if opcode == 0x1D || opcode == 0x1B {
		var mnem string
		switch {
		case opcode == 0x1D && u == 0:
			mnem = "scvtf"
		case opcode == 0x1D && u == 1:
			mnem = "ucvtf"
		case opcode == 0x1B && u == 0:
			mnem = "fcvtzs"
		default:
			mnem = "fcvtzu"
		}
		fsize := 0b10 | (w >> 22 & 1) // .2s/.4s -> size 10, .2d -> 11
		return fmt.Sprintf("%s %s, %s", mnem, vecName(rd, q, fsize), vecName(rn, q, fsize)), nil
	}
	var mnem string
	switch {
	case opcode == 0x0B && u == 0:
		mnem = "abs"
	case opcode == 0x0B && u == 1:
		mnem = "neg"
	case opcode == 0x05 && u == 0:
		mnem = "cnt"
	case opcode == 0x05 && u == 1:
		mnem = "not"
	default:
		return "", fmt.Errorf("arm64 disasm: unsupported two-reg-misc opcode %08x", w)
	}
	return fmt.Sprintf("%s %s, %s", mnem, vecName(rd, q, size), vecName(rn, q, size)), nil
}

// acrossName names an across-lanes reduction from (U, opcode).
func acrossName(u, opcode uint32) (string, bool) {
	switch {
	case opcode == 0x1B && u == 0:
		return "addv", true
	case opcode == 0x0A && u == 0:
		return "smaxv", true
	case opcode == 0x0A && u == 1:
		return "umaxv", true
	case opcode == 0x1A && u == 0:
		return "sminv", true
	case opcode == 0x1A && u == 1:
		return "uminv", true
	}
	return "", false
}

// disSIMDAcross decodes an across-lanes reduction "addv Bd, Vn.T".
func disSIMDAcross(w uint32) (string, error) {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	opcode := (w >> 12) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	mnem, ok := acrossName(u, opcode)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: unsupported across-lanes opcode %08x", w)
	}
	dst := fpRegName(rd, 8<<size) // scalar of the element width
	return fmt.Sprintf("%s %s, %s", mnem, dst, vecName(rn, q, size)), nil
}

// elemLetter maps an element-size log2 to its lane-suffix letter.
var elemLetter = [4]byte{'b', 'h', 's', 'd'}

// vecElemName renders an indexed lane operand "vN.<t>[index]".
func vecElemName(n, szLog, index uint32) string {
	return fmt.Sprintf("v%d.%c[%d]", n, elemLetter[szLog], index)
}

// disSIMDCopy decodes dup/umov/smov/ins.
func disSIMDCopy(w uint32) (string, error) {
	q := (w >> 30) & 1
	op := (w >> 29) & 1
	imm5 := (w >> 16) & 0x1F
	imm4 := (w >> 11) & 0xF
	rn, rd := (w>>5)&0x1F, w&0x1F
	if imm5 == 0 {
		return "", fmt.Errorf("arm64 disasm: bad SIMD copy imm5 %08x", w)
	}
	szLog := uint32(0)
	for szLog < 4 && imm5&(1<<szLog) == 0 {
		szLog++
	}
	index := imm5 >> (szLog + 1)

	if op == 1 { // ins (element)
		return fmt.Sprintf("ins %s, %s",
			vecElemName(rd, szLog, index), vecElemName(rn, szLog, imm4>>szLog)), nil
	}
	switch imm4 {
	case 0b0000: // dup (element)
		return fmt.Sprintf("dup %s, %s", vecName(rd, q, szLog), vecElemName(rn, szLog, index)), nil
	case 0b0001: // dup (general)
		return fmt.Sprintf("dup %s, %s", vecName(rd, q, szLog), rname(rn, szLog == 3, false)), nil
	case 0b0011: // ins (general)
		return fmt.Sprintf("ins %s, %s", vecElemName(rd, szLog, index), rname(rn, szLog == 3, false)), nil
	case 0b0111: // umov
		return fmt.Sprintf("umov %s, %s", rname(rd, q == 1, false), vecElemName(rn, szLog, index)), nil
	case 0b0101: // smov
		return fmt.Sprintf("smov %s, %s", rname(rd, q == 1, false), vecElemName(rn, szLog, index)), nil
	}
	return "", fmt.Errorf("arm64 disasm: unsupported SIMD copy imm4=%d %08x", imm4, w)
}

// ld1RegCount maps the LD1/ST1 multiple-structures opcode to a register count.
func ld1RegCount(opcode uint32) (int, bool) {
	switch opcode {
	case 0x7:
		return 1, true
	case 0xA:
		return 2, true
	case 0x6:
		return 3, true
	case 0x2:
		return 4, true
	}
	return 0, false
}

// disSIMDLdSt1 decodes LD1/ST1 (multiple structures).
func disSIMDLdSt1(w uint32) (string, error) {
	q := (w >> 30) & 1
	load := (w>>22)&1 == 1
	post := (w>>23)&1 == 1
	rm := (w >> 16) & 0x1F
	opcode := (w >> 12) & 0xF
	size := (w >> 10) & 3
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	count, ok := ld1RegCount(opcode)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: unsupported LD1/ST1 opcode %08x", w)
	}
	mnem := "st1"
	if load {
		mnem = "ld1"
	}
	var list []string
	for i := 0; i < count; i++ {
		list = append(list, vecName((rt+uint32(i))%32, q, size))
	}
	base := rname(rn, true, true)
	out := fmt.Sprintf("%s {%s}, [%s]", mnem, strings.Join(list, ", "), base)
	if !post {
		return out, nil
	}
	if rm == 0x1F { // immediate post-index = the transfer size
		total := (int64(8) << q) * int64(count)
		return fmt.Sprintf("%s {%s}, [%s], #%d", mnem, strings.Join(list, ", "), base, total), nil
	}
	return fmt.Sprintf("%s {%s}, [%s], %s", mnem, strings.Join(list, ", "), base, rname(rm, true, false)), nil
}

// disSIMDModImm decodes the vector MOVI/MVNI modified-immediate forms.
func disSIMDModImm(w uint32) (string, error) {
	q := (w >> 30) & 1
	op := (w >> 29) & 1
	cmode := (w >> 12) & 0xF
	imm8 := (w>>16)&7<<5 | (w>>5)&0x1F
	rd := w & 0x1F

	switch {
	case cmode == 0b1110 && op == 1: // movi .2d
		var v uint64
		for i := 0; i < 8; i++ {
			if imm8&(1<<i) != 0 {
				v |= uint64(0xFF) << (8 * i)
			}
		}
		return fmt.Sprintf("movi %s, #%#016x", vecName(rd, q, 0b11), v), nil
	case cmode == 0b1110: // movi byte
		return fmt.Sprintf("movi %s, #%#x", vecName(rd, q, 0b00), imm8), nil
	case cmode&0b1101 == 0b1000: // 16-bit (10x0)
		mnem := movMnem(op)
		shift := (cmode >> 1 & 1) * 8
		return movImmText(mnem, vecName(rd, q, 0b01), imm8, shift), nil
	case cmode&0b1001 == 0b0000: // 32-bit (0xx0)
		mnem := movMnem(op)
		shift := (cmode >> 1 & 3) * 8
		return movImmText(mnem, vecName(rd, q, 0b10), imm8, shift), nil
	}
	return "", fmt.Errorf("arm64 disasm: unsupported modified-immediate %08x", w)
}

func movMnem(op uint32) string {
	if op == 1 {
		return "mvni"
	}
	return "movi"
}

func movImmText(mnem, reg string, imm8, shift uint32) string {
	if shift == 0 {
		return fmt.Sprintf("%s %s, #%#x", mnem, reg, imm8)
	}
	return fmt.Sprintf("%s %s, #%#x, lsl #%d", mnem, reg, imm8, shift)
}

// shiftImmName names a vector shift-by-immediate op from (U, opcode).
func shiftImmName(u, opcode uint32) (string, bool) {
	switch opcode {
	case 0x0A:
		return "shl", true
	case 0x00:
		if u == 0 {
			return "sshr", true
		}
		return "ushr", true
	case 0x02:
		if u == 0 {
			return "ssra", true
		}
		return "usra", true
	}
	return "", false
}

// disSIMDShiftImm decodes a vector shift-by-immediate (shl/sshr/ushr/ssra/usra).
func disSIMDShiftImm(w uint32) (string, error) {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	immh := (w >> 19) & 0xF
	immb := (w >> 16) & 7
	opcode := (w >> 11) & 0x1F
	rn, rd := (w>>5)&0x1F, w&0x1F
	if immh == 0 {
		return disSIMDModImm(w) // movi/mvni
	}
	mnem, ok := shiftImmName(u, opcode)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: unsupported shift-imm opcode %08x", w)
	}
	sizeLog := uint32(0)
	for b := immh; b > 1; b >>= 1 {
		sizeLog++
	}
	esize := int(8) << sizeLog
	immhb := int(immh<<3 | immb)
	var shift int
	if opcode == 0x0A {
		shift = immhb - esize
	} else {
		shift = 2*esize - immhb
	}
	v := func(n uint32) string { return vecName(n, q, sizeLog) }
	return fmt.Sprintf("%s %s, %s, #%d", mnem, v(rd), v(rn), shift), nil
}

// disSIMD3 decodes the Advanced SIMD three-same group (add/sub/mul + logicals).
func disSIMD3(w uint32) (string, error) {
	if (w>>21)&1 != 1 || (w>>10)&1 != 1 {
		return "", fmt.Errorf("arm64 disasm: unsupported Adv-SIMD encoding %08x", w)
	}
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	opcode := (w >> 11) & 0x1F
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	if opcode >= 0x18 { // FP three-same sub-group
		return disSIMD3F(w)
	}

	var mnem string
	asize := size // arrangement element size (logicals always render as byte)
	switch opcode {
	case 0x10:
		if u == 0 {
			mnem = "add"
		} else {
			mnem = "sub"
		}
	case 0x13:
		if u != 0 {
			return "", fmt.Errorf("arm64 disasm: unsupported Adv-SIMD opcode %08x", w)
		}
		mnem = "mul"
	case 0x06:
		if u == 0 {
			mnem = "cmgt"
		} else {
			mnem = "cmhi"
		}
	case 0x07:
		if u == 0 {
			mnem = "cmge"
		} else {
			mnem = "cmhs"
		}
	case 0x11:
		if u == 0 {
			mnem = "cmtst"
		} else {
			mnem = "cmeq"
		}
	case 0x17:
		mnem = "addp"
	case 0x0C:
		if u == 0 {
			mnem = "smax"
		} else {
			mnem = "umax"
		}
	case 0x0D:
		if u == 0 {
			mnem = "smin"
		} else {
			mnem = "umin"
		}
	case 0x14:
		if u == 0 {
			mnem = "smaxp"
		} else {
			mnem = "umaxp"
		}
	case 0x15:
		if u == 0 {
			mnem = "sminp"
		} else {
			mnem = "uminp"
		}
	case 0x03: // logical: size field selects the op, arrangement is .8b/.16b
		asize = 0b00
		switch {
		case u == 1:
			mnem = "eor"
		case size == 0b00:
			mnem = "and"
		case size == 0b01:
			mnem = "bic"
		case size == 0b10:
			mnem = "orr"
		default:
			mnem = "orn"
		}
	default:
		return "", fmt.Errorf("arm64 disasm: unsupported Adv-SIMD opcode %08x", w)
	}
	return fmt.Sprintf("%s %s, %s, %s", mnem,
		vecName(rd, q, asize), vecName(rn, q, asize), vecName(rm, q, asize)), nil
}

// fpVecMnem names a float three-same op from (U, bit23 'a', opcode[15:11]).
func fpVecMnem(u, a, opcode uint32) (string, bool) {
	switch u<<6 | a<<5 | opcode {
	case 0<<6 | 0<<5 | 0x1A:
		return "fadd", true
	case 0<<6 | 1<<5 | 0x1A:
		return "fsub", true
	case 1<<6 | 0<<5 | 0x1B:
		return "fmul", true
	case 1<<6 | 0<<5 | 0x1F:
		return "fdiv", true
	case 0<<6 | 0<<5 | 0x1E:
		return "fmax", true
	case 0<<6 | 1<<5 | 0x1E:
		return "fmin", true
	case 0<<6 | 0<<5 | 0x18:
		return "fmaxnm", true
	case 0<<6 | 1<<5 | 0x18:
		return "fminnm", true
	case 0<<6 | 0<<5 | 0x1C:
		return "fcmeq", true
	case 1<<6 | 0<<5 | 0x1C:
		return "fcmge", true
	case 1<<6 | 1<<5 | 0x1C:
		return "fcmgt", true
	case 1<<6 | 0<<5 | 0x1A:
		return "faddp", true
	case 1<<6 | 0<<5 | 0x1E:
		return "fmaxp", true
	case 1<<6 | 1<<5 | 0x1E:
		return "fminp", true
	}
	return "", false
}

// disSIMD3F decodes the float three-same group. The arrangement is .2s/.4s (sz=0)
// or .2d (sz=1); render via a synthetic size field (10 for single, 11 for double).
func disSIMD3F(w uint32) (string, error) {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	a := (w >> 23) & 1
	sz := (w >> 22) & 1
	opcode := (w >> 11) & 0x1F
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	mnem, ok := fpVecMnem(u, a, opcode)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: unsupported FP-vector op %08x", w)
	}
	size := 0b10 | sz // .2s/.4s -> size 10, .2d -> size 11
	return fmt.Sprintf("%s %s, %s, %s", mnem,
		vecName(rd, q, uint32(size)), vecName(rn, q, uint32(size)), vecName(rm, q, uint32(size))), nil
}

// Scalar floating-point disassembly: the decode counterpart of fp.go, covering
// exactly the forms the assembler encodes so bytes round-trip through Assemble.

// fpRegName names a scalar FP/SIMD register from its number and ftype-derived
// width. The size is in bits (8/16/32/64/128).
func fpRegName(n uint32, sizeBits int) string {
	var p byte
	switch sizeBits {
	case 8:
		p = 'b'
	case 16:
		p = 'h'
	case 32:
		p = 's'
	case 64:
		p = 'd'
	case 128:
		p = 'q'
	default:
		return fmt.Sprintf("?%d", n)
	}
	return fmt.Sprintf("%c%d", p, n)
}

// ftypeSize converts the 2-bit ftype field to a width in bits (S=00, D=01,
// H=11). ok=false for the reserved 10.
func ftypeSize(ftype uint32) (int, bool) {
	switch ftype {
	case 0b00:
		return 32, true
	case 0b01:
		return 64, true
	case 0b11:
		return 16, true
	}
	return 0, false
}

var fpArith2Names = [9]string{
	"fmul", "fdiv", "fadd", "fsub", "fmax", "fmin", "fmaxnm", "fminnm", "fnmul",
}

var fpArith1Names = [4]string{"fmov", "fabs", "fneg", "fsqrt"}

func disFPDataProc(w uint32) (string, error) {
	if (w>>21)&1 != 1 {
		return "", fmt.Errorf("arm64 disasm: unsupported FP encoding %08x", w)
	}
	switch (w >> 10) & 3 { // bits[11:10]
	case 0b10:
		return disFPArith2(w)
	case 0b11:
		return disFPCsel(w)
	case 0b01:
		return "", fmt.Errorf("arm64 disasm: fccmp not in slice %08x", w)
	}
	field6 := (w >> 10) & 0x3F
	switch {
	case field6 == 0:
		return disFPConvInt(w)
	case field6&0x1F == 0x10:
		return disFPArith1(w)
	case field6&0xF == 0x8:
		return disFPCompare(w)
	}
	return "", fmt.Errorf("arm64 disasm: unsupported FP encoding %08x", w)
}

func disFPArith2(w uint32) (string, error) {
	sz, ok := ftypeSize((w >> 22) & 3)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: bad FP ftype %08x", w)
	}
	opcode := (w >> 12) & 0xF
	if int(opcode) >= len(fpArith2Names) {
		return "", fmt.Errorf("arm64 disasm: bad FP 2-source opcode %08x", w)
	}
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	return fmt.Sprintf("%s %s, %s, %s", fpArith2Names[opcode],
		fpRegName(rd, sz), fpRegName(rn, sz), fpRegName(rm, sz)), nil
}

func disFPArith1(w uint32) (string, error) {
	opcode := (w >> 15) & 0x3F
	rn, rd := (w>>5)&0x1F, w&0x1F
	if opcode>>2 == 0b0001 { // fcvt (precision conversion)
		srcSz, ok1 := ftypeSize((w >> 22) & 3)
		dstSz, ok2 := ftypeSize(opcode & 3)
		if !ok1 || !ok2 {
			return "", fmt.Errorf("arm64 disasm: bad fcvt %08x", w)
		}
		return fmt.Sprintf("fcvt %s, %s", fpRegName(rd, dstSz), fpRegName(rn, srcSz)), nil
	}
	if int(opcode) >= len(fpArith1Names) {
		return "", fmt.Errorf("arm64 disasm: bad FP 1-source opcode %08x", w)
	}
	sz, ok := ftypeSize((w >> 22) & 3)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: bad FP ftype %08x", w)
	}
	return fmt.Sprintf("%s %s, %s", fpArith1Names[opcode], fpRegName(rd, sz), fpRegName(rn, sz)), nil
}

func disFPConvInt(w uint32) (string, error) {
	rmode := (w >> 19) & 3
	opcode := (w >> 16) & 7
	sf := (w >> 31) & 1
	sz, ok := ftypeSize((w >> 22) & 3)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: bad FP/int ftype %08x", w)
	}
	rn, rd := (w>>5)&0x1F, w&0x1F
	gpr := rname(rd, sf == 1, false)
	gprN := rname(rn, sf == 1, false)
	fp := fpRegName(rd, sz)
	fpN := fpRegName(rn, sz)
	switch rmode<<3 | opcode {
	case 0b00<<3 | 0b111: // fmov GPR -> FP
		return fmt.Sprintf("fmov %s, %s", fp, gprN), nil
	case 0b00<<3 | 0b110: // fmov FP -> GPR
		return fmt.Sprintf("fmov %s, %s", gpr, fpN), nil
	case 0b00<<3 | 0b010: // scvtf
		return fmt.Sprintf("scvtf %s, %s", fp, gprN), nil
	case 0b00<<3 | 0b011: // ucvtf
		return fmt.Sprintf("ucvtf %s, %s", fp, gprN), nil
	case 0b11<<3 | 0b000: // fcvtzs
		return fmt.Sprintf("fcvtzs %s, %s", gpr, fpN), nil
	case 0b11<<3 | 0b001: // fcvtzu
		return fmt.Sprintf("fcvtzu %s, %s", gpr, fpN), nil
	}
	return "", fmt.Errorf("arm64 disasm: unsupported FP/int conversion %08x", w)
}

func disFPCompare(w uint32) (string, error) {
	sz, ok := ftypeSize((w >> 22) & 3)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: bad fcmp ftype %08x", w)
	}
	opcode2 := w & 0x1F
	rm, rn := (w>>16)&0x1F, (w>>5)&0x1F
	mnem := "fcmp"
	if opcode2&0b10000 != 0 {
		mnem = "fcmpe"
	}
	if opcode2&0b01000 != 0 { // compare with #0.0
		return fmt.Sprintf("%s %s, #0.0", mnem, fpRegName(rn, sz)), nil
	}
	return fmt.Sprintf("%s %s, %s", mnem, fpRegName(rn, sz), fpRegName(rm, sz)), nil
}

func disFPCsel(w uint32) (string, error) {
	sz, ok := ftypeSize((w >> 22) & 3)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: bad fcsel ftype %08x", w)
	}
	rm, cond, rn, rd := (w>>16)&0x1F, (w>>12)&0xF, (w>>5)&0x1F, w&0x1F
	return fmt.Sprintf("fcsel %s, %s, %s, %s",
		fpRegName(rd, sz), fpRegName(rn, sz), fpRegName(rm, sz), condNames[cond]), nil
}

// fpLSSize maps a FP load/store (size, opc[1]) to the access width in bits.
func fpLSSize(size, opcHi uint32) (int, bool) {
	switch {
	case size == 0 && opcHi == 1:
		return 128, true // Q
	case size == 0:
		return 8, true // B
	case size == 1:
		return 16, true // H
	case size == 2:
		return 32, true // S
	case size == 3:
		return 64, true // D
	}
	return 0, false
}

func disFPLoadStore(w uint32) (string, error) {
	size := (w >> 30) & 3
	opc := (w >> 22) & 3
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	sizeBits, ok := fpLSSize(size, (opc>>1)&1)
	if !ok {
		return "", fmt.Errorf("arm64 disasm: bad FP load/store size %08x", w)
	}
	isLoad := opc&1 == 1
	mnem := "str"
	if isLoad {
		mnem = "ldr"
	}
	rtName := fpRegName(rt, sizeBits)
	base := rname(rn, true, true)
	scale := uint32(sizeBits / 8)
	logScale := uint32(0)
	for s := scale; s > 1; s >>= 1 {
		logScale++
	}

	if (w>>24)&1 == 1 { // unsigned offset
		imm12 := (w >> 10) & 0xFFF
		mem := fmt.Sprintf("[%s]", base)
		if imm12 != 0 {
			mem = fmt.Sprintf("[%s, #%d]", base, imm12*scale)
		}
		return fmt.Sprintf("%s %s, %s", mnem, rtName, mem), nil
	}
	if (w>>21)&1 == 1 && (w>>10)&3 == 0b10 { // register offset
		rm := (w >> 16) & 0x1F
		option := (w >> 13) & 7
		s := (w >> 12) & 1
		idx := rname(rm, option&1 == 1, false)
		var ext string
		switch option {
		case 0b011:
			if s == 1 {
				ext = fmt.Sprintf(", lsl #%d", logScale)
			}
		case 0b010:
			ext = ", uxtw"
		case 0b110:
			ext = ", sxtw"
		case 0b111:
			ext = ", sxtx"
		default:
			return "", fmt.Errorf("arm64 disasm: bad FP index extend %08x", w)
		}
		if s == 1 && option != 0b011 {
			ext += fmt.Sprintf(" #%d", logScale)
		}
		return fmt.Sprintf("%s %s, [%s, %s%s]", mnem, rtName, base, idx, ext), nil
	}
	imm9 := signExtend((w>>12)&0x1FF, 9)
	switch (w >> 10) & 3 {
	case 0b00: // unscaled
		mem := fmt.Sprintf("[%s]", base)
		if imm9 != 0 {
			mem = fmt.Sprintf("[%s, #%d]", base, imm9)
		}
		// llvm renders the unscaled FP form as ldur/stur.
		m := "stur"
		if isLoad {
			m = "ldur"
		}
		return fmt.Sprintf("%s %s, %s", m, rtName, mem), nil
	case 0b01: // post-index
		return fmt.Sprintf("%s %s, [%s], #%d", mnem, rtName, base, imm9), nil
	case 0b11: // pre-index
		return fmt.Sprintf("%s %s, [%s, #%d]!", mnem, rtName, base, imm9), nil
	}
	return "", fmt.Errorf("arm64 disasm: bad FP load/store form %08x", w)
}
