package arm64

import (
	"fmt"
	"strconv"
	"strings"
)

// Advanced SIMD (vector) instructions. The first slice is the "three same"
// integer group over arrangement specifiers (v0.16b … v0.2d): add/sub/mul and
// the bitwise logicals. Byte-exact vs llvm-mc; execution vs native Apple Silicon
// (the oracle already dumps V0–V31).

// vecReg is a parsed vector register: number 0..31, Q (full=1/half=0) and the
// 2-bit element size (00=byte,01=half,10=single,11=double).
type vecReg struct {
	num  uint32
	q    uint32
	size uint32
}

// arrangements maps an arrangement suffix to (Q, size).
var arrangements = map[string][2]uint32{
	"8b": {0, 0b00}, "16b": {1, 0b00},
	"4h": {0, 0b01}, "8h": {1, 0b01},
	"2s": {0, 0b10}, "4s": {1, 0b10},
	"1d": {0, 0b11}, "2d": {1, 0b11},
}

// parseVecReg parses "vN.<arrangement>" (e.g. v3.4s).
func parseVecReg(s string) (vecReg, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	dot := strings.IndexByte(s, '.')
	if dot < 0 || len(s) < 2 || s[0] != 'v' {
		return vecReg{}, false
	}
	n, err := strconv.Atoi(s[1:dot])
	if err != nil || n < 0 || n > 31 {
		return vecReg{}, false
	}
	a, ok := arrangements[s[dot+1:]]
	if !ok {
		return vecReg{}, false
	}
	return vecReg{uint32(n), a[0], a[1]}, true
}

// isVecOperand reports whether an operand is a vector register (vN.arr) or an
// indexed lane (vN.t[i]) — used to route mnemonics that collide with scalar ones
// (add/sub/and/...) into the SIMD encoder.
func isVecOperand(s string) bool {
	if _, ok := parseVecReg(s); ok {
		return true
	}
	_, ok := parseVecElem(s)
	return ok
}

// simdOnlyMnemonics are vector instructions whose first operand may be a GPR
// (umov/smov) or a register list (ld1/st1), so they must route to the SIMD
// encoder by name.
var simdOnlyMnemonics = map[string]bool{
	"dup": true, "umov": true, "smov": true, "ins": true,
	"ld1": true, "st1": true,
	"addv": true, "smaxv": true, "umaxv": true, "sminv": true, "uminv": true,
}

// isSIMDLine reports whether a parsed line is an Advanced SIMD instruction.
func isSIMDLine(mnem string, ops []string) bool {
	return simdOnlyMnemonics[mnem] || (len(ops) > 0 && isVecOperand(ops[0]))
}

// simd3 describes a "three same" vector op. fixedSize >= 0 forces the size field
// (the bitwise logicals reuse it as an opcode extension and accept only the
// .8b/.16b arrangements); otherwise the arrangement's element size is used.
type simd3 struct {
	u         uint32
	opcode    uint32
	fixedSize int
	maxSize   int // largest element size allowed from the arrangement (inclusive)
}

var simd3Ops = map[string]simd3{
	"add": {u: 0, opcode: 0x10, fixedSize: -1, maxSize: 3},
	"sub": {u: 1, opcode: 0x10, fixedSize: -1, maxSize: 3},
	"mul": {u: 0, opcode: 0x13, fixedSize: -1, maxSize: 2}, // not on .2d
	// integer compares — per-lane all-ones / all-zeros result
	"cmgt":  {u: 0, opcode: 0x06, fixedSize: -1, maxSize: 3}, // signed >
	"cmge":  {u: 0, opcode: 0x07, fixedSize: -1, maxSize: 3}, // signed >=
	"cmhi":  {u: 1, opcode: 0x06, fixedSize: -1, maxSize: 3}, // unsigned >
	"cmhs":  {u: 1, opcode: 0x07, fixedSize: -1, maxSize: 3}, // unsigned >=
	"cmeq":  {u: 1, opcode: 0x11, fixedSize: -1, maxSize: 3}, // equal
	"cmtst": {u: 0, opcode: 0x11, fixedSize: -1, maxSize: 3}, // (a & b) != 0
	// bitwise logicals: size field is fixed, arrangement limited to 8b/16b.
	"and": {u: 0, opcode: 0x03, fixedSize: 0b00, maxSize: 0},
	"bic": {u: 0, opcode: 0x03, fixedSize: 0b01, maxSize: 0},
	"orr": {u: 0, opcode: 0x03, fixedSize: 0b10, maxSize: 0},
	"orn": {u: 0, opcode: 0x03, fixedSize: 0b11, maxSize: 0},
	"eor": {u: 1, opcode: 0x03, fixedSize: 0b00, maxSize: 0},
}

// vecElem is a parsed indexed lane operand "vN.<t>[index]" (e.g. v1.s[0]).
// szLog is the element-size log2 (0=B,1=H,2=S,3=D).
type vecElem struct {
	num   uint32
	szLog int
	index uint32
}

// parseVecElem parses "vN.<t>[index]".
func parseVecElem(s string) (vecElem, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	dot := strings.IndexByte(s, '.')
	lb := strings.IndexByte(s, '[')
	if dot < 0 || lb < 0 || s[0] != 'v' || !strings.HasSuffix(s, "]") || lb < dot {
		return vecElem{}, false
	}
	n, err := strconv.Atoi(s[1:dot])
	if err != nil || n < 0 || n > 31 {
		return vecElem{}, false
	}
	var szLog int
	switch s[dot+1 : lb] {
	case "b":
		szLog = 0
	case "h":
		szLog = 1
	case "s":
		szLog = 2
	case "d":
		szLog = 3
	default:
		return vecElem{}, false
	}
	idx, err := strconv.Atoi(s[lb+1 : len(s)-1])
	if err != nil || idx < 0 || idx >= (16>>szLog) {
		return vecElem{}, false
	}
	return vecElem{uint32(n), szLog, uint32(idx)}, true
}

// elemImm5 builds the imm5 field encoding element size + index: the low set bit
// marks the size (B=…1, H=…10, S=…100, D=1000) and the index sits above it.
func elemImm5(szLog int, index uint32) uint32 {
	return (index << (szLog + 1)) | (1 << szLog)
}

// elemSizeArr maps an element-size log2 to the arrangement size field.
func arrToSzLog(size uint32) int { return int(size) }

// simd3F describes a float "three same" vector op. The FP encoding reuses the
// three-same template but bit23 ('a') is part of the op selector and bit22 is
// the element size (sz: 0=single, 1=double), so the integer (size, opcode)
// decode does not apply. opcode[15:11] >= 0x18 marks the FP sub-group.
type simd3F struct {
	u      uint32
	a      uint32 // bit23
	opcode uint32 // bits[15:11]
}

var simd3FOps = map[string]simd3F{
	"fadd":   {u: 0, a: 0, opcode: 0x1A},
	"fsub":   {u: 0, a: 1, opcode: 0x1A},
	"fmul":   {u: 1, a: 0, opcode: 0x1B},
	"fdiv":   {u: 1, a: 0, opcode: 0x1F},
	"fmax":   {u: 0, a: 0, opcode: 0x1E},
	"fmin":   {u: 0, a: 1, opcode: 0x1E},
	"fmaxnm": {u: 0, a: 0, opcode: 0x18},
	"fminnm": {u: 0, a: 1, opcode: 0x18},
}

// encodeSIMD dispatches a vector instruction whose mnemonic/operands indicate
// the Advanced SIMD encoding.
func encodeSIMD(mnem string, ops []string) (uint32, error) {
	if op, ok := simd3Ops[mnem]; ok {
		return encodeSIMD3(op, ops)
	}
	if op, ok := simd3FOps[mnem]; ok {
		return encodeSIMD3F(op, ops)
	}
	if op, ok := shiftImmOps[mnem]; ok {
		return encodeSIMDShiftImm(op, ops)
	}
	if op, ok := acrossOps[mnem]; ok {
		return encodeSIMDAcross(op, ops)
	}
	if op, ok := twoRegMiscOps[mnem]; ok {
		return encodeSIMD2RegMisc(op, ops)
	}
	switch mnem {
	case "dup":
		return encodeDup(ops)
	case "umov", "smov":
		return encodeMov2GPR(mnem, ops)
	case "ins":
		return encodeIns(ops)
	case "ld1", "st1":
		return encodeLdSt1(mnem, ops)
	}
	return 0, fmt.Errorf("unsupported SIMD op %q", mnem)
}

// encodeDup encodes DUP (general) "dup Vd.T, Rn" and DUP (element)
// "dup Vd.T, Vn.Ts[index]".
func encodeDup(ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("dup expects two operands")
	}
	rd, ok := parseVecReg(ops[0])
	if !ok {
		return 0, fmt.Errorf("dup needs a vector destination")
	}
	if elem, ok := parseVecElem(ops[1]); ok { // DUP (element)
		if int(rd.size) != elem.szLog {
			return 0, fmt.Errorf("dup element size mismatch")
		}
		imm5 := elemImm5(elem.szLog, elem.index)
		return rd.q<<30 | 0x0E000400 | imm5<<16 | elem.num<<5 | rd.num, nil
	}
	g, ok := parseReg(ops[1]) // DUP (general)
	if !ok {
		return 0, fmt.Errorf("bad dup source")
	}
	imm5 := elemImm5(arrToSzLog(rd.size), 0)
	return rd.q<<30 | 0x0E000C00 | imm5<<16 | g.num<<5 | rd.num, nil
}

// encodeMov2GPR encodes UMOV (zero-extend) and SMOV (sign-extend) of a lane to
// a GPR: "umov Wd, Vn.Ts[index]".
func encodeMov2GPR(mnem string, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("%s expects two operands", mnem)
	}
	g, ok1 := parseReg(ops[0])
	elem, ok2 := parseVecElem(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad %s operands", mnem)
	}
	imm5 := elemImm5(elem.szLog, elem.index)
	var q, imm4 uint32
	if g.is64 {
		q = 1
	}
	// Match the valid width pairings (the forms llvm-mc accepts): UMOV moves a
	// B/H/S lane to Wd or only a D lane to Xd; SMOV sign-extends a B/H lane to Wd
	// or a B/H/S lane to Xd. Reject the rest rather than emit bytes llvm won't.
	if mnem == "umov" {
		imm4 = 0b0111
		ok := (!g.is64 && elem.szLog <= 2) || (g.is64 && elem.szLog == 3)
		if !ok {
			return 0, fmt.Errorf("invalid umov width: %s from .%c", ops[0], "bhsd"[elem.szLog])
		}
	} else { // smov
		imm4 = 0b0101
		maxSz := 1 // Wd: B/H
		if g.is64 {
			maxSz = 2 // Xd: B/H/S
		}
		if elem.szLog > maxSz {
			return 0, fmt.Errorf("invalid smov width: %s from .%c", ops[0], "bhsd"[elem.szLog])
		}
	}
	return q<<30 | 0x0E000000 | imm5<<16 | imm4<<11 | 1<<10 | elem.num<<5 | g.num, nil
}

// encodeIns encodes INS (general) "ins Vd.Ts[index], Rn" and INS (element)
// "ins Vd.Ts[idx1], Vn.Ts[idx2]".
func encodeIns(ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("ins expects two operands")
	}
	dst, ok := parseVecElem(ops[0])
	if !ok {
		return 0, fmt.Errorf("ins needs Vd.Ts[index]")
	}
	imm5 := elemImm5(dst.szLog, dst.index)
	if src, ok := parseVecElem(ops[1]); ok { // INS (element): op=1
		if src.szLog != dst.szLog {
			return 0, fmt.Errorf("ins element size mismatch")
		}
		imm4 := src.index << dst.szLog
		return 1<<30 | 1<<29 | 0x0E000000 | imm5<<16 | imm4<<11 | 1<<10 | src.num<<5 | dst.num, nil
	}
	g, ok := parseReg(ops[1]) // INS (general): op=0, imm4=0011, Q=1
	if !ok {
		return 0, fmt.Errorf("bad ins source")
	}
	return 1<<30 | 0x0E000000 | imm5<<16 | 0b0011<<11 | 1<<10 | g.num<<5 | dst.num, nil
}

// twoRegMisc describes an Advanced SIMD two-register-miscellaneous op. byteOnly
// marks the ops that take only the .8b/.16b arrangements (not/cnt).
type twoRegMisc struct {
	u        uint32
	opcode   uint32
	byteOnly bool
}

var twoRegMiscOps = map[string]twoRegMisc{
	"abs": {u: 0, opcode: 0x0B},
	"neg": {u: 1, opcode: 0x0B},
	"cnt": {u: 0, opcode: 0x05, byteOnly: true},
	"not": {u: 1, opcode: 0x05, byteOnly: true},
}

// encodeSIMD2RegMisc encodes a two-register-misc op "abs Vd.T, Vn.T".
func encodeSIMD2RegMisc(op twoRegMisc, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("expected Vd.T, Vn.T")
	}
	rd, ok1 := parseVecReg(ops[0])
	rn, ok2 := parseVecReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad SIMD register")
	}
	if rd.q != rn.q || rd.size != rn.size {
		return 0, fmt.Errorf("arrangement mismatch")
	}
	if op.byteOnly && rd.size != 0 {
		return 0, fmt.Errorf("%s takes only .8b/.16b", ops[0])
	}
	if rd.size == 0b11 && rd.q == 0 {
		return 0, fmt.Errorf("invalid .1d arrangement")
	}
	return rd.q<<30 | op.u<<29 | 0x0E200000 | rd.size<<22 |
		op.opcode<<12 | 0b10<<10 | rn.num<<5 | rd.num, nil
}

// acrossLanes describes an across-lanes reduction (addv/smaxv/…).
type acrossLanes struct {
	u      uint32
	opcode uint32
}

var acrossOps = map[string]acrossLanes{
	"addv":  {u: 0, opcode: 0x1B},
	"smaxv": {u: 0, opcode: 0x0A},
	"umaxv": {u: 1, opcode: 0x0A},
	"sminv": {u: 0, opcode: 0x1A},
	"uminv": {u: 1, opcode: 0x1A},
}

// encodeSIMDAcross encodes an across-lanes reduction "addv Bd, Vn.T". The scalar
// destination width must equal the source element size; valid arrangements are
// .8b/.16b/.4h/.8h/.4s (size 2 needs Q=1; .2d and .2s are not allowed).
func encodeSIMDAcross(op acrossLanes, ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("expected Vd, Vn.T")
	}
	dst, ok1 := parseFPReg(ops[0])
	src, ok2 := parseVecReg(ops[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad reduction operands")
	}
	if src.size == 0b11 || (src.size == 0b10 && src.q == 0) {
		return 0, fmt.Errorf("invalid arrangement for reduction")
	}
	if dst.size != 8<<src.size {
		return 0, fmt.Errorf("reduction destination width must match the element size")
	}
	return src.q<<30 | op.u<<29 | 0x0E300000 | src.size<<22 |
		op.opcode<<12 | 0b10<<10 | src.num<<5 | dst.num, nil
}

// shiftImm describes a vector shift-by-immediate op. left selects the
// left-shift immediate encoding (immh:immb = esize + shift) vs the right-shift
// one (immh:immb = 2*esize - shift).
type shiftImm struct {
	u      uint32
	opcode uint32
	left   bool
}

var shiftImmOps = map[string]shiftImm{
	"shl":  {u: 0, opcode: 0x0A, left: true},  // left
	"sshr": {u: 0, opcode: 0x00, left: false}, // signed right
	"ushr": {u: 1, opcode: 0x00, left: false}, // unsigned right
	"ssra": {u: 0, opcode: 0x02, left: false}, // signed right, accumulate
	"usra": {u: 1, opcode: 0x02, left: false}, // unsigned right, accumulate
}

// encodeSIMDShiftImm encodes a vector shift-by-immediate (shl/sshr/ushr/…). The
// element size comes from the arrangement; immh:immb folds in both the size and
// the shift amount.
func encodeSIMDShiftImm(op shiftImm, ops []string) (uint32, error) {
	if len(ops) != 3 {
		return 0, fmt.Errorf("expected Vd, Vn, #shift")
	}
	rd, ok1 := parseVecReg(ops[0])
	rn, ok2 := parseVecReg(ops[1])
	shift, ok3 := parseImm(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad shift operands")
	}
	if rd.q != rn.q || rd.size != rn.size {
		return 0, fmt.Errorf("shift arrangement mismatch")
	}
	if rd.size == 0b11 && rd.q == 0 {
		return 0, fmt.Errorf("invalid .1d arrangement")
	}
	esize := int64(8) << rd.size
	var immhb int64
	if op.left {
		if shift < 0 || shift >= esize {
			return 0, fmt.Errorf("left shift %d out of range [0,%d)", shift, esize)
		}
		immhb = esize + shift
	} else {
		if shift < 1 || shift > esize {
			return 0, fmt.Errorf("right shift %d out of range [1,%d]", shift, esize)
		}
		immhb = 2*esize - shift
	}
	return rd.q<<30 | op.u<<29 | 0x0F000000 | uint32(immhb)<<16 |
		op.opcode<<11 | 1<<10 | rn.num<<5 | rd.num, nil
}

// parseRegList parses a SIMD register list "{v0.16b, v1.16b, ...}". The members
// must share one arrangement and be consecutive (modulo 32); 1..4 registers.
func parseRegList(s string) (regs []vecReg, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, false
	}
	for _, part := range strings.Split(s[1:len(s)-1], ",") {
		r, rok := parseVecReg(part)
		if !rok {
			return nil, false
		}
		regs = append(regs, r)
	}
	if len(regs) < 1 || len(regs) > 4 {
		return nil, false
	}
	for i := 1; i < len(regs); i++ {
		if regs[i].q != regs[0].q || regs[i].size != regs[0].size {
			return nil, false
		}
		if regs[i].num != (regs[0].num+uint32(i))%32 {
			return nil, false
		}
	}
	return regs, true
}

// ld1Opcode maps a register count (1..4) to the LD1/ST1 multiple-structures
// opcode field (bits[15:12]).
var ld1Opcode = map[int]uint32{1: 0x7, 2: 0xA, 3: 0x6, 4: 0x2}

// encodeLdSt1 encodes LD1/ST1 (multiple structures, 1..4 consecutive registers)
// in the no-offset, post-index-immediate and post-index-register forms.
func encodeLdSt1(mnem string, ops []string) (uint32, error) {
	if len(ops) < 2 || len(ops) > 3 {
		return 0, fmt.Errorf("%s expects {list}, [Xn]{, #imm|Xm}", mnem)
	}
	regs, ok := parseRegList(ops[0])
	if !ok {
		return 0, fmt.Errorf("bad register list %q", ops[0])
	}
	opcode := ld1Opcode[len(regs)]
	first := regs[0]
	base, ok := bareBase(ops[1])
	if !ok {
		return 0, fmt.Errorf("%s needs a [Xn] base, got %q", mnem, ops[1])
	}
	var l uint32 = 0
	if mnem == "ld1" {
		l = 1
	}
	w := first.q<<30 | 0x0C000000 | l<<22 | opcode<<12 | first.size<<10 | base.num<<5 | first.num

	if len(ops) == 2 { // no offset
		return w, nil
	}
	// Post-index: bit23 set, Rm = 0x1F for the immediate form (#imm must equal
	// the transfer size), else the index register.
	w |= 1 << 23
	bytesPerReg := int64(8) << first.q // 8 (Q=0) or 16 (Q=1)
	total := bytesPerReg * int64(len(regs))
	if imm, ok := parseImm(ops[2]); ok {
		if imm != total {
			return 0, fmt.Errorf("%s post-index immediate must be %d, got %d", mnem, total, imm)
		}
		return w | 0x1F<<16, nil
	}
	rm, ok := parseReg(ops[2])
	if !ok || !rm.is64 {
		return 0, fmt.Errorf("bad %s post-index operand %q", mnem, ops[2])
	}
	return w | rm.num<<16, nil
}

// encodeSIMD3F encodes a float three-same vector instruction (fadd/fmul/…) over
// the .2s/.4s/.2d arrangements.
func encodeSIMD3F(op simd3F, ops []string) (uint32, error) {
	if len(ops) != 3 {
		return 0, fmt.Errorf("expected Vd, Vn, Vm")
	}
	rd, ok1 := parseVecReg(ops[0])
	rn, ok2 := parseVecReg(ops[1])
	rm, ok3 := parseVecReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad SIMD register")
	}
	if rd.q != rn.q || rd.q != rm.q || rd.size != rn.size || rd.size != rm.size {
		return 0, fmt.Errorf("SIMD arrangement mismatch")
	}
	// FP vectors are .2s (Q0 size10), .4s (Q1 size10) or .2d (Q1 size11). .1d
	// (Q0 size11) is not a valid FP arrangement.
	if rd.size < 0b10 || (rd.size == 0b11 && rd.q == 0) {
		return 0, fmt.Errorf("invalid FP vector arrangement")
	}
	sz := rd.size & 1 // 0=single, 1=double
	return rd.q<<30 | op.u<<29 | 0x0E000000 | op.a<<23 | sz<<22 | 1<<21 |
		rm.num<<16 | op.opcode<<11 | 1<<10 | rn.num<<5 | rd.num, nil
}

// encodeSIMD3 encodes a three-same vector instruction: Vd, Vn, Vm with a common
// arrangement.
func encodeSIMD3(op simd3, ops []string) (uint32, error) {
	if len(ops) != 3 {
		return 0, fmt.Errorf("expected Vd, Vn, Vm")
	}
	rd, ok1 := parseVecReg(ops[0])
	rn, ok2 := parseVecReg(ops[1])
	rm, ok3 := parseVecReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad SIMD register")
	}
	if rd.q != rn.q || rd.q != rm.q || rd.size != rn.size || rd.size != rm.size {
		return 0, fmt.Errorf("SIMD arrangement mismatch")
	}
	if int(rd.size) > op.maxSize {
		return 0, fmt.Errorf("element size not allowed for this op")
	}
	// 64-bit elements exist only as the full vector .2d; .1d (Q=0) is invalid.
	if op.fixedSize < 0 && rd.size == 0b11 && rd.q == 0 {
		return 0, fmt.Errorf("invalid .1d arrangement")
	}
	size := rd.size
	if op.fixedSize >= 0 {
		size = uint32(op.fixedSize)
	}
	return op.u<<29 | 0x0E000000 | size<<22 | 1<<21 | rm.num<<16 |
		op.opcode<<11 | 1<<10 | rn.num<<5 | rd.num | rd.q<<30, nil
}
