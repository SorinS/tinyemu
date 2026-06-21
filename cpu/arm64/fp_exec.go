package arm64

import (
	"fmt"
	"math"
	"math/bits"
)

// Scalar floating-point execution: the V0–V31 register file and single/double
// arithmetic, conversions, compares, selects and FP load/store. Semantics are
// validated against native Apple Silicon (oracle_test.go) — the same run-and-
// check oracle the integer core uses, extended to dump the V registers + FPCR.
//
// Register-write rule (mirrors W-zeros-X): a scalar S or D write zeros the rest
// of the 128-bit V register. writeVS/writeVD encode that.

func (c *CPU) readVD(n uint32) uint64     { return c.Vreg[n][0] }
func (c *CPU) readVS(n uint32) uint32     { return uint32(c.Vreg[n][0]) }
func (c *CPU) writeVD(n uint32, v uint64) { c.Vreg[n] = [2]uint64{v, 0} }
func (c *CPU) writeVS(n uint32, v uint32) { c.Vreg[n] = [2]uint64{uint64(v), 0} }

// execFPDataProc dispatches the scalar floating-point data-processing group
// (encodings with bits[28:24]=0b11110, bit21=1): 1-/2-source arithmetic,
// precision conversion, int↔FP conversion + fmov-GPR, compare and select.
func (c *CPU) execFPDataProc(w uint32) error {
	if (w>>21)&1 != 1 {
		return fmt.Errorf("arm64: unsupported FP encoding %08x at %#x", w, c.PC)
	}
	switch (w >> 10) & 3 { // bits[11:10]
	case 0b10: // 2-source
		return c.execFPArith2(w)
	case 0b11: // fcsel
		return c.execFPCsel(w)
	case 0b01: // fccmp — not yet implemented
		return fmt.Errorf("arm64: fccmp not implemented %08x", w)
	}
	// bits[11:10]==00: conversions / 1-source / compare / immediate.
	field6 := (w >> 10) & 0x3F // bits[15:10]
	switch {
	case field6 == 0: // int↔FP conversion + fmov GPR↔FP
		return c.execFPConvInt(w)
	case field6&0x1F == 0x10: // 1-source (bit14 set marks the group)
		return c.execFPArith1(w)
	case field6&0xF == 0x8: // compare (bits[13:10]=0b1000)
		return c.execFPCompare(w)
	case (w>>10)&7 == 0b100: // fmov immediate — not yet implemented
		return fmt.Errorf("arm64: fmov immediate not implemented %08x", w)
	}
	return fmt.Errorf("arm64: unsupported FP encoding %08x at %#x", w, c.PC)
}

// fpIsDouble reports whether the ftype field selects double precision (01).
// Single is 00. Half (11) is not yet executed.
func fpIsDouble(w uint32) (isD bool, ok bool) {
	switch (w >> 22) & 3 {
	case 0b00:
		return false, true
	case 0b01:
		return true, true
	}
	return false, false // half precision: encodable but not yet executed
}

func (c *CPU) execFPArith2(w uint32) error {
	isD, ok := fpIsDouble(w)
	if !ok {
		return fmt.Errorf("arm64: half-precision FP not implemented %08x", w)
	}
	opcode := (w >> 12) & 0xF
	rm, rn, rd := (w>>16)&0x1F, (w>>5)&0x1F, w&0x1F
	if isD {
		a := math.Float64frombits(c.readVD(rn))
		b := math.Float64frombits(c.readVD(rm))
		r, err := fpArith2Apply(opcode, a, b)
		if err != nil {
			return err
		}
		c.writeVD(rd, math.Float64bits(r))
		return nil
	}
	a := math.Float32frombits(c.readVS(rn))
	b := math.Float32frombits(c.readVS(rm))
	r, err := fpArith2Apply32(opcode, a, b)
	if err != nil {
		return err
	}
	c.writeVS(rd, math.Float32bits(r))
	return nil
}

func fpArith2Apply(op uint32, a, b float64) (float64, error) {
	switch op {
	case 0x0: // fmul
		return a * b, nil
	case 0x1: // fdiv
		return a / b, nil
	case 0x2: // fadd
		return a + b, nil
	case 0x3: // fsub
		return a - b, nil
	case 0x4: // fmax
		return math.Max(a, b), nil
	case 0x5: // fmin
		return math.Min(a, b), nil
	case 0x6: // fmaxnm
		return fmaxnm(a, b), nil
	case 0x7: // fminnm
		return fminnm(a, b), nil
	case 0x8: // fnmul
		return -(a * b), nil
	}
	return 0, fmt.Errorf("arm64: bad FP 2-source opcode %d", op)
}

func fpArith2Apply32(op uint32, a, b float32) (float32, error) {
	switch op {
	case 0x0:
		return a * b, nil
	case 0x1:
		return a / b, nil
	case 0x2:
		return a + b, nil
	case 0x3:
		return a - b, nil
	case 0x4:
		return float32(math.Max(float64(a), float64(b))), nil
	case 0x5:
		return float32(math.Min(float64(a), float64(b))), nil
	case 0x6:
		return float32(fmaxnm(float64(a), float64(b))), nil
	case 0x7:
		return float32(fminnm(float64(a), float64(b))), nil
	case 0x8:
		return -(a * b), nil
	}
	return 0, fmt.Errorf("arm64: bad FP 2-source opcode %d", op)
}

// fmaxnm/fminnm: like FMAX/FMIN but a quiet NaN operand is ignored in favour of
// the numeric operand (only NaN-vs-NaN yields NaN). math.Max/Min already give
// the right +0/-0 and NaN-propagation behaviour for the both-numeric case.
func fmaxnm(a, b float64) float64 {
	if math.IsNaN(a) {
		return b
	}
	if math.IsNaN(b) {
		return a
	}
	return math.Max(a, b)
}

func fminnm(a, b float64) float64 {
	if math.IsNaN(a) {
		return b
	}
	if math.IsNaN(b) {
		return a
	}
	return math.Min(a, b)
}

func (c *CPU) execFPArith1(w uint32) error {
	isD, ok := fpIsDouble(w)
	opcode := (w >> 15) & 0x3F // bits[20:15]
	rn, rd := (w>>5)&0x1F, w&0x1F

	// fcvt (precision conversion) is opcode 0b0001xx: the low two bits select
	// the destination precision. Handle it before the single-precision-only ops.
	if opcode>>2 == 0b0001 {
		return c.execFcvt(w)
	}
	if !ok {
		return fmt.Errorf("arm64: half-precision FP not implemented %08x", w)
	}
	if isD {
		raw := c.readVD(rn)
		switch opcode {
		case 0x0: // fmov (copy)
			c.writeVD(rd, raw)
		case 0x1: // fabs
			c.writeVD(rd, raw&^(uint64(1)<<63))
		case 0x2: // fneg
			c.writeVD(rd, raw^(uint64(1)<<63))
		case 0x3: // fsqrt
			c.writeVD(rd, math.Float64bits(math.Sqrt(math.Float64frombits(raw))))
		default:
			return fmt.Errorf("arm64: bad FP 1-source opcode %d", opcode)
		}
		return nil
	}
	raw := c.readVS(rn)
	switch opcode {
	case 0x0:
		c.writeVS(rd, raw)
	case 0x1:
		c.writeVS(rd, raw&^(uint32(1)<<31))
	case 0x2:
		c.writeVS(rd, raw^(uint32(1)<<31))
	case 0x3:
		c.writeVS(rd, math.Float32bits(float32(math.Sqrt(float64(math.Float32frombits(raw))))))
	default:
		return fmt.Errorf("arm64: bad FP 1-source opcode %d", opcode)
	}
	return nil
}

// execFcvt converts between FP precisions. ftype (bits[23:22]) is the source
// precision; opcode bits[1:0] select the destination (S=00, D=01, H=11).
func (c *CPU) execFcvt(w uint32) error {
	src := (w >> 22) & 3
	dst := (w >> 15) & 3 // opcode low two bits
	rn, rd := (w>>5)&0x1F, w&0x1F
	// Read the source value as a float64 (the common currency).
	var val float64
	switch src {
	case 0b00:
		val = float64(math.Float32frombits(c.readVS(rn)))
	case 0b01:
		val = math.Float64frombits(c.readVD(rn))
	default:
		return fmt.Errorf("arm64: fcvt from half precision not implemented %08x", w)
	}
	switch dst {
	case 0b00:
		c.writeVS(rd, math.Float32bits(float32(val)))
	case 0b01:
		c.writeVD(rd, math.Float64bits(val))
	default:
		return fmt.Errorf("arm64: fcvt to half precision not implemented %08x", w)
	}
	return nil
}

// execFPConvInt handles int↔FP conversions and fmov between a GPR and an FP
// register. rmode (bits[20:19]) and opcode (bits[18:16]) select the operation;
// sf (bit31) picks the GPR width; ftype the FP precision.
func (c *CPU) execFPConvInt(w uint32) error {
	rmode := (w >> 19) & 3
	opcode := (w >> 16) & 7
	sf := (w >> 31) & 1
	ftype := (w >> 22) & 3
	rn, rd := (w>>5)&0x1F, w&0x1F
	is64 := sf == 1

	switch rmode<<3 | opcode {
	case 0b00<<3 | 0b111: // fmov GPR -> FP
		v := c.readX(rn, is64, false)
		if ftype == 0b01 { // D <- X
			c.writeVD(rd, v)
		} else { // S <- W
			c.writeVS(rd, uint32(v))
		}
		return nil
	case 0b00<<3 | 0b110: // fmov FP -> GPR
		if ftype == 0b01 { // X <- D
			c.writeX(rd, true, false, c.readVD(rn))
		} else { // W <- S
			c.writeX(rd, false, false, uint64(c.readVS(rn)))
		}
		return nil
	case 0b01<<3 | 0b111: // fmov Vd.D[1] <- Xn (move GPR into the high 64 bits)
		if ftype == 0b10 {
			c.Vreg[rd][1] = c.readX(rn, true, false) // preserve the low half
			return nil
		}
	case 0b01<<3 | 0b110: // fmov Xd <- Vn.D[1] (read the high 64 bits)
		if ftype == 0b10 {
			c.writeX(rd, true, false, c.Vreg[rn][1])
			return nil
		}
	case 0b00<<3 | 0b010: // scvtf (signed int -> FP)
		return c.cvtIntToFP(rd, rn, ftype, is64, true)
	case 0b00<<3 | 0b011: // ucvtf (unsigned int -> FP)
		return c.cvtIntToFP(rd, rn, ftype, is64, false)
	case 0b11<<3 | 0b000: // fcvtzs (FP -> signed int, toward zero)
		return c.cvtFPToInt(rd, rn, ftype, is64, true)
	case 0b11<<3 | 0b001: // fcvtzu (FP -> unsigned int, toward zero)
		return c.cvtFPToInt(rd, rn, ftype, is64, false)
	}
	return fmt.Errorf("arm64: unsupported FP/int conversion %08x at %#x", w, c.PC)
}

func (c *CPU) cvtIntToFP(rd, rn, ftype uint32, is64, signed bool) error {
	raw := c.readX(rn, is64, false)
	var f float64
	if signed {
		if is64 {
			f = float64(int64(raw))
		} else {
			f = float64(int32(uint32(raw)))
		}
	} else {
		if is64 {
			f = float64(raw)
		} else {
			f = float64(uint32(raw))
		}
	}
	if ftype == 0b01 {
		c.writeVD(rd, math.Float64bits(f))
	} else {
		c.writeVS(rd, math.Float32bits(float32(f)))
	}
	return nil
}

func (c *CPU) cvtFPToInt(rd, rn, ftype uint32, is64, signed bool) error {
	var f float64
	if ftype == 0b01 {
		f = math.Float64frombits(c.readVD(rn))
	} else {
		f = float64(math.Float32frombits(c.readVS(rn)))
	}
	c.writeX(rd, is64, false, fpToIntSat(f, is64, signed))
	return nil
}

// fpToIntSat truncates toward zero and saturates, matching FCVTZS/FCVTZU: NaN
// maps to 0, and out-of-range values clamp to the signed/unsigned min/max. (Go's
// raw float→int conversion is undefined out of range, so the clamping is done
// explicitly here.)
func fpToIntSat(f float64, is64, signed bool) uint64 {
	if math.IsNaN(f) {
		return 0
	}
	f = math.Trunc(f)
	if signed {
		if is64 {
			if f >= 9223372036854775808.0 { // 2^63
				return uint64(math.MaxInt64)
			}
			if f < -9223372036854775808.0 {
				return 0x8000000000000000 // INT64_MIN
			}
			return uint64(int64(f))
		}
		if f >= 2147483648.0 { // 2^31
			return uint64(uint32(math.MaxInt32))
		}
		if f < -2147483648.0 {
			return 0x80000000 // INT32_MIN, zero-extended into Wd
		}
		return uint64(uint32(int32(f)))
	}
	if is64 {
		if f >= 18446744073709551616.0 { // 2^64
			return math.MaxUint64
		}
		if f <= 0 {
			return 0
		}
		return uint64(f)
	}
	if f >= 4294967296.0 { // 2^32
		return uint64(uint32(math.MaxUint32))
	}
	if f <= 0 {
		return 0
	}
	return uint64(uint32(f))
}

// execFPCompare evaluates fcmp/fcmpe and writes NZCV. The second operand is the
// literal 0.0 when opcode2 bit3 is set, otherwise V[Rm].
func (c *CPU) execFPCompare(w uint32) error {
	isD, ok := fpIsDouble(w)
	if !ok {
		return fmt.Errorf("arm64: half-precision compare not implemented %08x", w)
	}
	rm, rn := (w>>16)&0x1F, (w>>5)&0x1F
	opcode2 := w & 0x1F
	var a, b float64
	if isD {
		a = math.Float64frombits(c.readVD(rn))
		if opcode2&0b01000 != 0 {
			b = 0
		} else {
			b = math.Float64frombits(c.readVD(rm))
		}
	} else {
		a = float64(math.Float32frombits(c.readVS(rn)))
		if opcode2&0b01000 != 0 {
			b = 0
		} else {
			b = float64(math.Float32frombits(c.readVS(rm)))
		}
	}
	switch {
	case math.IsNaN(a) || math.IsNaN(b): // unordered
		c.setFlags(false, false, true, true)
	case a == b:
		c.setFlags(false, true, true, false)
	case a < b:
		c.setFlags(true, false, false, false)
	default: // a > b
		c.setFlags(false, false, true, false)
	}
	return nil
}

func (c *CPU) execFPCsel(w uint32) error {
	isD, ok := fpIsDouble(w)
	if !ok {
		return fmt.Errorf("arm64: half-precision fcsel not implemented %08x", w)
	}
	rm, cond, rn, rd := (w>>16)&0x1F, (w>>12)&0xF, (w>>5)&0x1F, w&0x1F
	src := rm
	if c.condHolds(cond) {
		src = rn
	}
	if isD {
		c.writeVD(rd, c.readVD(src))
	} else {
		c.writeVS(rd, c.readVS(src))
	}
	return nil
}

// execFPLoadStore executes a scalar FP load/store (the V-bit form of the
// single-register load/store group). It mirrors execLoadStore's addressing but
// moves data to/from the V register file, including the 128-bit Q form.
func (c *CPU) execFPLoadStore(w uint32) error {
	size := (w >> 30) & 3
	opc := (w >> 22) & 3
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	isLoad := opc&1 == 1

	var nbytes int
	switch {
	case size == 0 && opc&2 != 0: // Q (128-bit)
		nbytes = 16
	case size == 0:
		nbytes = 1
	case size == 1:
		nbytes = 2
	case size == 2:
		nbytes = 4
	case size == 3:
		nbytes = 8
	}
	sizeShift := uint32(bits.TrailingZeros(uint(nbytes)))
	base := c.readX(rn, true, true)

	var addr uint64
	wback := false
	var wbVal uint64
	switch {
	case (w>>24)&1 == 1: // unsigned offset (scaled)
		imm12 := int64((w >> 10) & 0xFFF)
		addr = base + uint64(imm12*int64(nbytes))
	case (w>>21)&1 == 1 && (w>>10)&3 == 0b10: // register offset
		rm := (w >> 16) & 0x1F
		option := (w >> 13) & 7
		s := (w >> 12) & 1
		var sh uint32
		if s == 1 {
			sh = sizeShift
		}
		addr = base + extendReg(c.readX(rm, true, false), option, sh)
	default: // imm9: unscaled / pre / post
		imm9 := signExtend(uint64((w>>12)&0x1FF), 9)
		switch (w >> 10) & 3 {
		case 0b00: // unscaled
			addr = base + uint64(imm9)
		case 0b01: // post-index
			addr = base
			wback, wbVal = true, base+uint64(imm9)
		case 0b11: // pre-index
			addr = base + uint64(imm9)
			wback, wbVal = true, addr
		default:
			return fmt.Errorf("arm64: bad FP load/store form %08x", w)
		}
	}

	if isLoad {
		lo, err := c.readMem(addr, min(nbytes, 8))
		if err != nil {
			return err
		}
		if nbytes == 16 {
			hi, err := c.readMem(addr+8, 8)
			if err != nil {
				return err
			}
			c.Vreg[rt] = [2]uint64{lo, hi}
		} else {
			c.Vreg[rt] = [2]uint64{lo, 0} // zero-extend into the 128-bit register
		}
	} else {
		if err := c.writeMem(addr, c.Vreg[rt][0], min(nbytes, 8)); err != nil {
			return err
		}
		if nbytes == 16 {
			if err := c.writeMem(addr+8, c.Vreg[rt][1], 8); err != nil {
				return err
			}
		}
	}
	if wback {
		c.writeX(rn, true, true, wbVal)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
