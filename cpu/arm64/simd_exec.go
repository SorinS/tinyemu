package arm64

import (
	"fmt"
	"math"
	"math/bits"
)

// Advanced SIMD (vector) execution. First slices: the "three same" integer group
// (add/sub/mul + logicals) and the copy group (dup/umov/smov/ins), matching the
// asm/arm64 encoder. Validated against native Apple Silicon (the oracle dumps
// V0–V31).

// execSIMD dispatches the Advanced SIMD data group (bits[28:24]=0b01110):
// three-same (bit21=1, bit10=1), the copy group (bits[23:21]=000) and the
// across-lanes reductions (bits[21:17]=11000, bits[11:10]=10).
func (c *CPU) execSIMD(w uint32) error {
	switch {
	case (w>>17)&0x1F == 0x18 && (w>>10)&3 == 0b10:
		return c.execSIMDAcross(w)
	case (w>>17)&0x1F == 0x10 && (w>>10)&3 == 0b10:
		return c.execSIMD2RegMisc(w)
	case (w>>21)&1 == 1 && (w>>10)&1 == 1:
		return c.execSIMD3(w)
	case (w>>21)&7 == 0 && (w>>15)&1 == 0 && (w>>10)&1 == 1:
		return c.execSIMDCopy(w)
	}
	return fmt.Errorf("arm64: unsupported Adv-SIMD encoding %08x at %#x", w, c.PC)
}

// execSIMD2RegMisc executes abs/neg (opcode 0x0B) and cnt/not (opcode 0x05).
func (c *CPU) execSIMD2RegMisc(w uint32) error {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	opcode := (w >> 12) & 0x1F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	vn := c.Vreg[rn]

	switch {
	case opcode == 0x05 && u == 1: // not — bitwise, whole register
		res := [2]uint64{^vn[0], ^vn[1]}
		if q == 0 {
			res[1] = 0
		}
		c.Vreg[rd] = res
		return nil
	case opcode == 0x05 && u == 0: // cnt — per-byte popcount
		c.Vreg[rd] = c.simd2RegLanes(vn, 0, q, func(a uint64, _ uint) uint64 {
			return uint64(bits.OnesCount8(uint8(a)))
		})
		return nil
	case opcode == 0x0B && u == 0: // abs
		c.Vreg[rd] = c.simd2RegLanes(vn, size, q, func(a uint64, e uint) uint64 {
			v := sextLane(a, e)
			if v < 0 {
				v = -v
			}
			return uint64(v)
		})
		return nil
	case opcode == 0x0B && u == 1: // neg
		c.Vreg[rd] = c.simd2RegLanes(vn, size, q, func(a uint64, _ uint) uint64 {
			return uint64(0) - a
		})
		return nil
	}
	return fmt.Errorf("arm64: unsupported two-reg-misc opcode %08x at %#x", w, c.PC)
}

// simd2RegLanes applies a per-lane unary op (results masked to the element
// width), zeroing the upper 64 bits for a half-vector.
func (c *CPU) simd2RegLanes(vn [2]uint64, size, q uint32, op func(a uint64, ebits uint) uint64) [2]uint64 {
	ebits := uint(8) << size
	var mask uint64
	if ebits >= 64 {
		mask = ^uint64(0)
	} else {
		mask = (uint64(1) << ebits) - 1
	}
	words := 1
	if q == 1 {
		words = 2
	}
	var res [2]uint64
	for wi := 0; wi < words; wi++ {
		var out uint64
		for off := uint(0); off < 64; off += ebits {
			out |= (op((vn[wi]>>off)&mask, ebits) & mask) << off
		}
		res[wi] = out
	}
	return res
}

// execSIMDAcross executes an across-lanes reduction (addv/smaxv/umaxv/sminv/
// uminv): fold every lane of Vn into a single scalar in Vd (rest zeroed).
func (c *CPU) execSIMDAcross(w uint32) error {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	opcode := (w >> 12) & 0x1F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	ebits := uint(8) << size
	var mask uint64
	if ebits >= 64 {
		mask = ^uint64(0)
	} else {
		mask = (uint64(1) << ebits) - 1
	}
	words := 1
	if q == 1 {
		words = 2
	}
	vn := c.Vreg[rn]

	// Collect the lanes, then reduce.
	first := true
	var acc uint64    // for addv (wraps at the element width)
	var sMax, sMin int64
	var uMax, uMin uint64
	for wi := 0; wi < words; wi++ {
		for off := uint(0); off < 64; off += ebits {
			v := (vn[wi] >> off) & mask
			sv := sextLane(v, ebits)
			if first {
				acc, sMax, sMin, uMax, uMin = v, sv, sv, v, v
				first = false
				continue
			}
			acc = (acc + v) & mask
			if sv > sMax {
				sMax = sv
			}
			if sv < sMin {
				sMin = sv
			}
			if v > uMax {
				uMax = v
			}
			if v < uMin {
				uMin = v
			}
		}
	}

	var res uint64
	switch {
	case opcode == 0x1B: // addv
		res = acc
	case opcode == 0x0A && u == 0: // smaxv
		res = uint64(sMax) & mask
	case opcode == 0x0A && u == 1: // umaxv
		res = uMax
	case opcode == 0x1A && u == 0: // sminv
		res = uint64(sMin) & mask
	case opcode == 0x1A && u == 1: // uminv
		res = uMin
	default:
		return fmt.Errorf("arm64: unsupported across-lanes opcode %08x at %#x", w, c.PC)
	}
	c.Vreg[rd] = [2]uint64{res & mask, 0}
	return nil
}

// lane geometry helpers: an element of width (8<<szLog) bits never crosses a
// 64-bit word boundary (8/16/32/64 all divide 64).
func laneMask(szLog uint32) uint64 {
	wb := uint(8) << szLog
	if wb >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << wb) - 1
}

func (c *CPU) readLane(reg, szLog, index uint32) uint64 {
	wb := uint(8) << szLog
	bit := uint(index) * wb
	return (c.Vreg[reg][bit/64] >> (bit % 64)) & laneMask(szLog)
}

func (c *CPU) writeLane(reg, szLog, index uint32, val uint64) {
	wb := uint(8) << szLog
	bit := uint(index) * wb
	m := laneMask(szLog)
	word := bit / 64
	off := bit % 64
	c.Vreg[reg][word] = (c.Vreg[reg][word] &^ (m << off)) | ((val & m) << off)
}

// broadcast fills the destination register with val in every lane of the given
// element size; q selects 64- vs 128-bit (the upper half is zeroed when q==0).
func (c *CPU) broadcast(reg, szLog, q uint32, val uint64) {
	wb := uint(8) << szLog
	total := uint(64)
	if q == 1 {
		total = 128
	}
	c.Vreg[reg] = [2]uint64{}
	for bit := uint(0); bit < total; bit += wb {
		m := laneMask(szLog)
		c.Vreg[reg][bit/64] |= (val & m) << (bit % 64)
	}
}

// execSIMDCopy executes dup (general/element), umov, smov and ins
// (general/element). The element size + index live in imm5 (lowest set bit marks
// the size); op (bit29) and imm4 select the variant.
func (c *CPU) execSIMDCopy(w uint32) error {
	q := (w >> 30) & 1
	op := (w >> 29) & 1
	imm5 := (w >> 16) & 0x1F
	imm4 := (w >> 11) & 0xF
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	if imm5 == 0 {
		return fmt.Errorf("arm64: bad SIMD copy imm5 %08x", w)
	}
	szLog := uint32(bits.TrailingZeros32(imm5))
	index := imm5 >> (szLog + 1)
	wbits := uint32(8) << szLog

	if op == 1 { // INS (element): Vd[index] = Vn[imm4>>szLog]
		c.writeLane(rd, szLog, index, c.readLane(rn, szLog, imm4>>szLog))
		return nil
	}
	switch imm4 {
	case 0b0000: // DUP (element)
		c.broadcast(rd, szLog, q, c.readLane(rn, szLog, index))
	case 0b0001: // DUP (general)
		c.broadcast(rd, szLog, q, c.readX(rn, true, false))
	case 0b0011: // INS (general): Vd[index] = Rn
		c.writeLane(rd, szLog, index, c.readX(rn, true, false))
	case 0b0111: // UMOV: Rd = zero-extended lane
		c.writeX(rd, q == 1, false, c.readLane(rn, szLog, index))
	case 0b0101: // SMOV: Rd = sign-extended lane
		v := uint64(signExtend(c.readLane(rn, szLog, index), wbits))
		c.writeX(rd, q == 1, false, v)
	default:
		return fmt.Errorf("arm64: unsupported SIMD copy imm4=%d %08x", imm4, w)
	}
	return nil
}

// execSIMD3 executes a three-same vector instruction.
func (c *CPU) execSIMD3(w uint32) error {
	if (w>>21)&1 != 1 || (w>>10)&1 != 1 {
		return fmt.Errorf("arm64: unsupported Adv-SIMD encoding %08x at %#x", w, c.PC)
	}
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	size := (w >> 22) & 3
	opcode := (w >> 11) & 0x1F
	rm := (w >> 16) & 0x1F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	// FP three-same reuses this template but with opcode[15:11] >= 0x18 and a
	// different field split (bit23 is part of the op, bit22 is the sz bit).
	if opcode >= 0x18 {
		return c.execSIMD3F(w)
	}
	vn, vm := c.Vreg[rn], c.Vreg[rm]

	var res [2]uint64
	switch opcode {
	case 0x10: // add (U=0) / sub (U=1)
		if u == 0 {
			res = laneOp(vn, vm, size, q, func(a, b uint64) uint64 { return a + b })
		} else {
			res = laneOp(vn, vm, size, q, func(a, b uint64) uint64 { return a - b })
		}
	case 0x13: // mul (U=0)
		if u != 0 {
			return fmt.Errorf("arm64: unsupported Adv-SIMD opcode %08x", w)
		}
		res = laneOp(vn, vm, size, q, func(a, b uint64) uint64 { return a * b })
	case 0x03: // bitwise logicals — size field selects the op, whole-register
		res = simdLogical(vn, vm, size, u)
	case 0x06: // cmgt (signed, U=0) / cmhi (unsigned, U=1)
		if u == 0 {
			res = laneCmp(vn, vm, size, q, func(a, b uint64, e uint) bool { return sextLane(a, e) > sextLane(b, e) })
		} else {
			res = laneCmp(vn, vm, size, q, func(a, b uint64, e uint) bool { return a > b })
		}
	case 0x07: // cmge (signed, U=0) / cmhs (unsigned, U=1)
		if u == 0 {
			res = laneCmp(vn, vm, size, q, func(a, b uint64, e uint) bool { return sextLane(a, e) >= sextLane(b, e) })
		} else {
			res = laneCmp(vn, vm, size, q, func(a, b uint64, e uint) bool { return a >= b })
		}
	case 0x11: // cmtst (U=0) / cmeq (U=1)
		if u == 0 {
			res = laneCmp(vn, vm, size, q, func(a, b uint64, e uint) bool { return a&b != 0 })
		} else {
			res = laneCmp(vn, vm, size, q, func(a, b uint64, e uint) bool { return a == b })
		}
	default:
		return fmt.Errorf("arm64: unsupported Adv-SIMD opcode %08x at %#x", w, c.PC)
	}
	if q == 0 {
		res[1] = 0 // a 64-bit (half-vector) op zeros the upper 64 bits
	}
	c.Vreg[rd] = res
	return nil
}

// simd3FScalarOp maps a float three-same op (keyed by U:bit23:opcode) to the
// scalar fpArith2 opcode (fp_exec.go), so the per-lane math reuses one place.
func simd3FScalarOp(u, a, opcode uint32) (uint32, bool) {
	switch u<<6 | a<<5 | opcode {
	case 0<<6 | 0<<5 | 0x1A: // fadd
		return 2, true
	case 0<<6 | 1<<5 | 0x1A: // fsub
		return 3, true
	case 1<<6 | 0<<5 | 0x1B: // fmul
		return 0, true
	case 1<<6 | 0<<5 | 0x1F: // fdiv
		return 1, true
	case 0<<6 | 0<<5 | 0x1E: // fmax
		return 4, true
	case 0<<6 | 1<<5 | 0x1E: // fmin
		return 5, true
	case 0<<6 | 0<<5 | 0x18: // fmaxnm
		return 6, true
	case 0<<6 | 1<<5 | 0x18: // fminnm
		return 7, true
	}
	return 0, false
}

// execSIMD3F executes a float three-same vector op (fadd/fmul/…). bit22 is the
// element size (0=single, 1=double); per-lane arithmetic reuses the scalar FP
// kernels, so the same IEEE-754 behaviour (and the same not-yet-modelled NaN
// payload / rounding caveats) applies.
func (c *CPU) execSIMD3F(w uint32) error {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	a := (w >> 23) & 1
	sz := (w >> 22) & 1
	opcode := (w >> 11) & 0x1F
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	if opcode == 0x1C { // FP compares (fcmeq/fcmge/fcmgt) -> lane mask
		return c.execSIMD3FCmp(w)
	}
	scalarOp, ok := simd3FScalarOp(u, a, opcode)
	if !ok {
		return fmt.Errorf("arm64: unsupported FP-vector op %08x at %#x", w, c.PC)
	}
	vn, vm := c.Vreg[rn], c.Vreg[rm]
	words := 1
	if q == 1 {
		words = 2
	}
	var res [2]uint64
	if sz == 1 { // double: one 64-bit lane per word
		for wi := 0; wi < words; wi++ {
			r, _ := fpArith2Apply(scalarOp, math.Float64frombits(vn[wi]), math.Float64frombits(vm[wi]))
			res[wi] = math.Float64bits(r)
		}
	} else { // single: two 32-bit lanes per word
		for wi := 0; wi < words; wi++ {
			var out uint64
			for off := uint(0); off < 64; off += 32 {
				av := math.Float32frombits(uint32(vn[wi] >> off))
				bv := math.Float32frombits(uint32(vm[wi] >> off))
				r, _ := fpArith2Apply32(scalarOp, av, bv)
				out |= uint64(math.Float32bits(r)) << off
			}
			res[wi] = out
		}
	}
	if q == 0 {
		res[1] = 0
	}
	c.Vreg[rd] = res
	return nil
}

// execSIMD3FCmp executes the FP vector compares (fcmeq U0a0 / fcmge U1a0 /
// fcmgt U1a1), writing a per-lane all-ones (true) / all-zeros mask. Unordered
// (NaN) comparisons are false — Go's float comparisons already give that.
func (c *CPU) execSIMD3FCmp(w uint32) error {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	a := (w >> 23) & 1
	sz := (w >> 22) & 1
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	pred := func(x, y float64) bool { return x == y } // fcmeq
	switch {
	case u == 1 && a == 0:
		pred = func(x, y float64) bool { return x >= y } // fcmge
	case u == 1 && a == 1:
		pred = func(x, y float64) bool { return x > y } // fcmgt
	}
	vn, vm := c.Vreg[rn], c.Vreg[rm]
	words := 1
	if q == 1 {
		words = 2
	}
	var res [2]uint64
	for wi := 0; wi < words; wi++ {
		if sz == 1 { // double
			if pred(math.Float64frombits(vn[wi]), math.Float64frombits(vm[wi])) {
				res[wi] = ^uint64(0)
			}
		} else { // single: two lanes
			var out uint64
			for off := uint(0); off < 64; off += 32 {
				x := float64(math.Float32frombits(uint32(vn[wi] >> off)))
				y := float64(math.Float32frombits(uint32(vm[wi] >> off)))
				if pred(x, y) {
					out |= uint64(0xFFFFFFFF) << off
				}
			}
			res[wi] = out
		}
	}
	if q == 0 {
		res[1] = 0
	}
	c.Vreg[rd] = res
	return nil
}

// ld1RegCount maps the LD1/ST1 multiple-structures opcode to the register count.
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

// execSIMDLdSt1 executes LD1/ST1 (multiple structures, 1..4 consecutive
// registers) in the no-offset and post-index forms. Each register is a full
// 64-bit (Q=0) or 128-bit (Q=1) contiguous chunk of memory.
func (c *CPU) execSIMDLdSt1(w uint32) error {
	q := (w >> 30) & 1
	load := (w>>22)&1 == 1
	post := (w>>23)&1 == 1
	rm := (w >> 16) & 0x1F
	opcode := (w >> 12) & 0xF
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	count, ok := ld1RegCount(opcode)
	if !ok {
		return fmt.Errorf("arm64: unsupported LD1/ST1 opcode %08x at %#x", w, c.PC)
	}
	bytesPerReg := uint64(8) << q
	addr := c.readX(rn, true, true) // base is SP-capable

	for i := 0; i < count; i++ {
		reg := (rt + uint32(i)) % 32
		if load {
			lo, err := c.readMem(addr, 8)
			if err != nil {
				return err
			}
			var hi uint64
			if q == 1 {
				if hi, err = c.readMem(addr+8, 8); err != nil {
					return err
				}
			}
			c.Vreg[reg] = [2]uint64{lo, hi}
		} else {
			if err := c.writeMem(addr, c.Vreg[reg][0], 8); err != nil {
				return err
			}
			if q == 1 {
				if err := c.writeMem(addr+8, c.Vreg[reg][1], 8); err != nil {
					return err
				}
			}
		}
		addr += bytesPerReg
	}

	if post {
		var incr uint64
		if rm == 0x1F { // immediate: the whole transfer size
			incr = bytesPerReg * uint64(count)
		} else {
			incr = c.readX(rm, true, false)
		}
		c.writeX(rn, true, true, c.readX(rn, true, true)+incr)
	}
	return nil
}

// execSIMDShiftImm executes a vector shift-by-immediate (shl/sshr/ushr/ssra/
// usra). The element size and shift amount are folded into immh:immb. (immh==0
// is the modified-immediate group — movi/etc — which is not yet implemented.)
func (c *CPU) execSIMDShiftImm(w uint32) error {
	q := (w >> 30) & 1
	u := (w >> 29) & 1
	immh := (w >> 19) & 0xF
	immb := (w >> 16) & 7
	opcode := (w >> 11) & 0x1F
	rn := (w >> 5) & 0x1F
	rd := w & 0x1F
	if immh == 0 {
		return c.execSIMDModImm(w) // movi/mvni
	}
	sizeLog := uint(bits.Len32(immh) - 1) // 0..3 -> B/H/S/D
	ebits := uint(8) << sizeLog
	immhb := int(immh<<3 | immb)
	esize := int(ebits)

	var shift int
	left := opcode == 0x0A
	if left {
		shift = immhb - esize
	} else {
		shift = 2*esize - immhb
	}
	signed := u == 0
	accumulate := opcode == 0x02

	var mask uint64
	if ebits >= 64 {
		mask = ^uint64(0)
	} else {
		mask = (uint64(1) << ebits) - 1
	}
	words := 1
	if q == 1 {
		words = 2
	}
	vn := c.Vreg[rn]
	var res [2]uint64
	for wi := 0; wi < words; wi++ {
		var out uint64
		for off := uint(0); off < 64; off += ebits {
			a := (vn[wi] >> off) & mask
			var r uint64
			switch {
			case left:
				r = (a << uint(shift)) & mask
			case signed:
				r = uint64(sextLane(a, ebits)>>uint(shift)) & mask
			default:
				r = (a >> uint(shift)) & mask
			}
			if accumulate {
				r = (r + ((c.Vreg[rd][wi] >> off) & mask)) & mask
			}
			out |= r << off
		}
		res[wi] = out
	}
	if q == 0 {
		res[1] = 0
	}
	c.Vreg[rd] = res
	return nil
}

// moviExpand2D expands a .2d movi 8-bit field to a 64-bit value: bit i selects
// byte i = 0xFF (set) or 0x00 (clear).
func moviExpand2D(imm8 uint32) uint64 {
	var v uint64
	for i := 0; i < 8; i++ {
		if imm8&(1<<i) != 0 {
			v |= uint64(0xFF) << (8 * i)
		}
	}
	return v
}

// execSIMDModImm executes the vector MOVI/MVNI modified-immediate forms (the
// immh==0 case of the shift-by-immediate group): byte/16-bit/32-bit element
// replicate, and the 64-bit movi .2d. MVNI inverts the per-element value.
func (c *CPU) execSIMDModImm(w uint32) error {
	q := (w >> 30) & 1
	op := (w >> 29) & 1
	cmode := (w >> 12) & 0xF
	imm8 := (w>>16)&7<<5 | (w>>5)&0x1F // abc:defgh
	rd := w & 0x1F

	var esize uint
	var elem uint64
	switch {
	case cmode == 0b1110 && op == 1: // movi .2d
		esize, elem = 64, moviExpand2D(imm8)
	case cmode == 0b1110: // movi byte
		esize, elem = 8, uint64(imm8)
	case cmode&0b1101 == 0b1000: // 16-bit (10x0)
		esize = 16
		elem = uint64(imm8) << (uint(cmode>>1&1) * 8)
	case cmode&0b1001 == 0b0000: // 32-bit (0xx0)
		esize = 32
		elem = uint64(imm8) << (uint(cmode>>1&3) * 8)
	default:
		return fmt.Errorf("arm64: unsupported modified-immediate cmode %08x at %#x", w, c.PC)
	}

	var emask uint64
	if esize >= 64 {
		emask = ^uint64(0)
	} else {
		emask = (uint64(1) << esize) - 1
	}
	if op == 1 && cmode != 0b1110 { // MVNI inverts the element (not the .2d form)
		elem = ^elem & emask
	}

	var word uint64
	for off := uint(0); off < 64; off += esize {
		word |= (elem & emask) << off
	}
	res := [2]uint64{word, word}
	if q == 0 {
		res[1] = 0
	}
	c.Vreg[rd] = res
	return nil
}

// sextLane sign-extends the low ebits of v to a signed 64-bit value.
func sextLane(v uint64, ebits uint) int64 {
	if ebits >= 64 {
		return int64(v)
	}
	shift := 64 - ebits
	return int64(v<<shift) >> shift
}

// laneCmp produces a per-lane all-ones (predicate true) or all-zeros mask, the
// result form of the vector compares (cmgt/cmge/cmeq/…).
func laneCmp(vn, vm [2]uint64, size, q uint32, pred func(a, b uint64, ebits uint) bool) [2]uint64 {
	ebits := uint(8) << size
	var mask uint64
	if ebits >= 64 {
		mask = ^uint64(0)
	} else {
		mask = (uint64(1) << ebits) - 1
	}
	words := 1
	if q == 1 {
		words = 2
	}
	var res [2]uint64
	for wi := 0; wi < words; wi++ {
		var out uint64
		for off := uint(0); off < 64; off += ebits {
			a := (vn[wi] >> off) & mask
			b := (vm[wi] >> off) & mask
			if pred(a, b, ebits) {
				out |= mask << off
			}
		}
		res[wi] = out
	}
	return res
}

// laneOp applies op to each element lane of vn/vm. size is the 2-bit element
// width (8<<size bits); q selects 64-bit (one word) vs 128-bit (both words).
func laneOp(vn, vm [2]uint64, size, q uint32, op func(a, b uint64) uint64) [2]uint64 {
	ebits := uint(8) << size
	var mask uint64
	if ebits >= 64 {
		mask = ^uint64(0)
	} else {
		mask = (uint64(1) << ebits) - 1
	}
	words := 1
	if q == 1 {
		words = 2
	}
	var res [2]uint64
	for wi := 0; wi < words; wi++ {
		var out uint64
		for off := uint(0); off < 64; off += ebits {
			a := (vn[wi] >> off) & mask
			b := (vm[wi] >> off) & mask
			out |= (op(a, b) & mask) << off
		}
		res[wi] = out
	}
	return res
}

// simdLogical applies the three-same bitwise logical selected by (size, U) over
// the full 128 bits: and/bic/orr/orn (U=0, size 00/01/10/11) and eor (U=1).
func simdLogical(vn, vm [2]uint64, size, u uint32) [2]uint64 {
	var op func(a, b uint64) uint64
	switch {
	case u == 1: // eor (size=00)
		op = func(a, b uint64) uint64 { return a ^ b }
	case size == 0b00: // and
		op = func(a, b uint64) uint64 { return a & b }
	case size == 0b01: // bic
		op = func(a, b uint64) uint64 { return a &^ b }
	case size == 0b10: // orr
		op = func(a, b uint64) uint64 { return a | b }
	default: // size == 0b11: orn
		op = func(a, b uint64) uint64 { return a | ^b }
	}
	return [2]uint64{op(vn[0], vm[0]), op(vn[1], vm[1])}
}
