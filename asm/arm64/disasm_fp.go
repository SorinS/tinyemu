package arm64

import "fmt"

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
