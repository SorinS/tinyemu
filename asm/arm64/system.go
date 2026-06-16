package arm64

import (
	"fmt"
	"strconv"
	"strings"
)

// System instructions: hints (nop/yield/wfe/…), barriers (dmb/dsb/isb),
// system-register move (mrs/msr), and exception generation (svc/brk/…).

// hintNums maps the hint mnemonics to their hint number.
var hintNums = map[string]uint32{
	"nop": 0, "yield": 1, "wfe": 2, "wfi": 3, "sev": 4, "sevl": 5,
}

// barrierOptions maps a barrier scope name to its CRm value.
var barrierOptions = map[string]uint32{
	"sy": 15, "st": 14, "ld": 13, "ish": 11, "ishst": 10, "ishld": 9,
	"nsh": 7, "nshst": 6, "nshld": 5, "osh": 3, "oshst": 2, "oshld": 1,
}

func encodeSystem(mnem string, ops []string) (uint32, error) {
	if n, ok := hintNums[mnem]; ok { // nop/yield/wfe/wfi/sev/sevl
		if len(ops) != 0 {
			return 0, fmt.Errorf("%s takes no operands", mnem)
		}
		return hintWord(n), nil
	}
	switch mnem {
	case "hint":
		if len(ops) != 1 {
			return 0, fmt.Errorf("hint expects #imm")
		}
		n, ok := parseImm(ops[0])
		if !ok || n < 0 || n > 127 {
			return 0, fmt.Errorf("hint number must be 0..127")
		}
		return hintWord(uint32(n)), nil
	case "dmb", "dsb", "isb":
		return encodeBarrier(mnem, ops)
	case "mrs":
		return encodeMRS(ops)
	case "msr":
		return encodeMSR(ops)
	}
	return 0, fmt.Errorf("unknown system op %q", mnem)
}

// hintWord builds a HINT instruction for hint number n (CRm:op2).
func hintWord(n uint32) uint32 {
	return 0xD503201F | (n>>3)<<8 | (n&7)<<5
}

func encodeBarrier(mnem string, ops []string) (uint32, error) {
	op2 := map[string]uint32{"dsb": 4, "dmb": 5, "isb": 6}[mnem]
	crm := uint32(15) // default option is "sy"
	if len(ops) == 1 {
		opt := strings.ToLower(strings.TrimSpace(ops[0]))
		if v, ok := barrierOptions[opt]; ok {
			crm = v
		} else if n, ok := parseImm(opt); ok && n >= 0 && n <= 15 {
			crm = uint32(n)
		} else {
			return 0, fmt.Errorf("bad barrier option %q", ops[0])
		}
	} else if len(ops) != 0 {
		return 0, fmt.Errorf("%s expects at most one option", mnem)
	}
	return 0xD503301F | crm<<8 | op2<<5, nil
}

func encodeMRS(ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("mrs expects Xt, sysreg")
	}
	rt, ok := parseReg(ops[0])
	if !ok || !rt.is64 {
		return 0, fmt.Errorf("mrs destination must be a 64-bit register")
	}
	field, ok := parseSysreg(ops[1])
	if !ok {
		return 0, fmt.Errorf("unknown system register %q", ops[1])
	}
	return 0xD5300000 | field | rt.num, nil
}

func encodeMSR(ops []string) (uint32, error) {
	if len(ops) != 2 {
		return 0, fmt.Errorf("msr expects sysreg, Xt")
	}
	field, ok := parseSysreg(ops[0])
	if !ok {
		return 0, fmt.Errorf("unknown system register %q", ops[0])
	}
	rt, ok := parseReg(ops[1])
	if !ok || !rt.is64 {
		return 0, fmt.Errorf("msr source must be a 64-bit register")
	}
	return 0xD5100000 | field | rt.num, nil
}

// namedSysregs maps common system-register names to their encoded field bits
// (o0<<19 | op1<<16 | CRn<<12 | CRm<<8 | op2<<5).
var namedSysregs = map[string][5]uint32{
	// name: {o0, op1, CRn, CRm, op2}
	"nzcv":        {1, 3, 4, 2, 0},
	"fpcr":        {1, 3, 4, 4, 0},
	"fpsr":        {1, 3, 4, 4, 1},
	"tpidr_el0":   {1, 3, 13, 0, 2},
	"tpidrro_el0": {1, 3, 13, 0, 3},
	"midr_el1":    {1, 0, 0, 0, 0},
	"mpidr_el1":   {1, 0, 0, 0, 5},
	"ctr_el0":     {1, 3, 0, 0, 1},
	"dczid_el0":   {1, 3, 0, 0, 7},
	"cntfrq_el0":  {1, 3, 14, 0, 0},
	"cntvct_el0":  {1, 3, 14, 0, 2},
}

// parseSysreg resolves a system-register operand — a known name or the generic
// S<op0>_<op1>_C<n>_C<m>_<op2> form — to its encoded field bits.
func parseSysreg(s string) (uint32, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if f, ok := namedSysregs[s]; ok {
		return f[0]<<19 | f[1]<<16 | f[2]<<12 | f[3]<<8 | f[4]<<5, true
	}
	// Generic: s3_3_c4_c2_0
	if !strings.HasPrefix(s, "s") {
		return 0, false
	}
	parts := strings.Split(s[1:], "_")
	if len(parts) != 5 {
		return 0, false
	}
	op0, e0 := strconv.Atoi(parts[0])
	op1, e1 := strconv.Atoi(parts[1])
	crn, e2 := atoiC(parts[2])
	crm, e3 := atoiC(parts[3])
	op2, e4 := strconv.Atoi(parts[4])
	if e0 != nil || e1 != nil || e2 != nil || e3 != nil || e4 != nil ||
		op0 < 2 || op0 > 3 || op1 > 7 || crn > 15 || crm > 15 || op2 > 7 {
		return 0, false
	}
	o0 := uint32(op0 - 2) // op0 ∈ {2,3} → o0 bit ∈ {0,1}
	return o0<<19 | uint32(op1)<<16 | uint32(crn)<<12 | uint32(crm)<<8 | uint32(op2)<<5, true
}

// atoiC parses a "c<n>" component of a generic system-register name.
func atoiC(s string) (int, error) {
	return strconv.Atoi(strings.TrimPrefix(s, "c"))
}

// exceptionForm gives the opc-derived high bits and the LL field per mnemonic.
var exceptionBase = map[string]uint32{
	"svc": 0xD4000001, "hvc": 0xD4000002, "smc": 0xD4000003,
	"brk": 0xD4200000, "hlt": 0xD4400000,
}

func encodeException(mnem string, ops []string) (uint32, error) {
	base, ok := exceptionBase[mnem]
	if !ok {
		return 0, fmt.Errorf("unknown exception op %q", mnem)
	}
	if len(ops) != 1 {
		return 0, fmt.Errorf("%s expects #imm16", mnem)
	}
	imm, ok := parseImm(ops[0])
	if !ok || imm < 0 || imm > 0xFFFF {
		return 0, fmt.Errorf("%s immediate must be 0..0xFFFF", mnem)
	}
	return base | uint32(imm)<<5, nil
}
