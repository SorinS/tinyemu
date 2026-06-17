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
// three-same (bit21=1) vs the copy group (bits[23:21]=000).
func (c *CPU) execSIMD(w uint32) error {
	switch {
	case (w>>21)&1 == 1 && (w>>10)&1 == 1:
		return c.execSIMD3(w)
	case (w>>21)&7 == 0 && (w>>15)&1 == 0 && (w>>10)&1 == 1:
		return c.execSIMDCopy(w)
	}
	return fmt.Errorf("arm64: unsupported Adv-SIMD encoding %08x at %#x", w, c.PC)
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
