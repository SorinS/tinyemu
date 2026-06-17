package arm64

import "fmt"

// Advanced SIMD (vector) execution. First slice: the "three same" integer group
// — add/sub/mul over every arrangement and the bitwise logicals — matching the
// asm/arm64 encoder. Validated against native Apple Silicon (the oracle dumps
// V0–V31).

// execSIMD3 executes a three-same vector instruction. It is reached for the
// Advanced SIMD data group (bits[28:24]=0b01110); non-three-same members return
// an unimplemented error so they surface rather than mis-execute.
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
