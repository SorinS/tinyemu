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

// isVecOperand reports whether an operand is a vector register (vN.arr) — used
// to route mnemonics that collide with scalar ones (add/sub/and/...) into the
// SIMD encoder.
func isVecOperand(s string) bool {
	_, ok := parseVecReg(s)
	return ok
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
	// bitwise logicals: size field is fixed, arrangement limited to 8b/16b.
	"and": {u: 0, opcode: 0x03, fixedSize: 0b00, maxSize: 0},
	"bic": {u: 0, opcode: 0x03, fixedSize: 0b01, maxSize: 0},
	"orr": {u: 0, opcode: 0x03, fixedSize: 0b10, maxSize: 0},
	"orn": {u: 0, opcode: 0x03, fixedSize: 0b11, maxSize: 0},
	"eor": {u: 1, opcode: 0x03, fixedSize: 0b00, maxSize: 0},
}

// encodeSIMD dispatches a vector instruction whose mnemonic/operands indicate
// the Advanced SIMD encoding.
func encodeSIMD(mnem string, ops []string) (uint32, error) {
	if op, ok := simd3Ops[mnem]; ok {
		return encodeSIMD3(op, ops)
	}
	return 0, fmt.Errorf("unsupported SIMD op %q", mnem)
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
	size := rd.size
	if op.fixedSize >= 0 {
		size = uint32(op.fixedSize)
	}
	return op.u<<29 | 0x0E000000 | size<<22 | 1<<21 | rm.num<<16 |
		op.opcode<<11 | 1<<10 | rn.num<<5 | rd.num | rd.q<<30, nil
}
