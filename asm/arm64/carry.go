package arm64

import (
	"fmt"
	"strings"
)

// Add/subtract with carry (adc/adcs/sbc/sbcs) and conditional compare
// (ccmp/ccmn).

// addSubCarryBase is the fixed encoding (sf clear) per mnemonic.
var addSubCarryBase = map[string]uint32{
	"adc":  0x1A000000,
	"adcs": 0x3A000000,
	"sbc":  0x5A000000,
	"sbcs": 0x7A000000,
}

func encodeAddSubCarry(mnem string, ops []string) (uint32, error) {
	base, ok := addSubCarryBase[mnem]
	if !ok {
		return 0, fmt.Errorf("unknown add/sub-carry %q", mnem)
	}
	if len(ops) != 3 {
		return 0, fmt.Errorf("%s expects 3 registers", mnem)
	}
	rd, ok1 := parseReg(ops[0])
	rn, ok2 := parseReg(ops[1])
	rm, ok3 := parseReg(ops[2])
	if !ok1 || !ok2 || !ok3 {
		return 0, fmt.Errorf("bad register operand")
	}
	return sfBit(rd) | base | rm.num<<16 | rn.num<<5 | rd.num, nil
}

// condCmpBase is the fixed encoding (sf clear) per mnemonic.
var condCmpBase = map[string]uint32{
	"ccmn": 0x3A400000,
	"ccmp": 0x7A400000,
}

// encodeCondCmp encodes ccmp/ccmn: Rn, Rm-or-#imm5, #nzcv, cond.
func encodeCondCmp(mnem string, ops []string) (uint32, error) {
	base, ok := condCmpBase[mnem]
	if !ok {
		return 0, fmt.Errorf("unknown conditional compare %q", mnem)
	}
	if len(ops) != 4 {
		return 0, fmt.Errorf("%s expects Rn, Rm/#imm, #nzcv, cond", mnem)
	}
	rn, ok := parseReg(ops[0])
	if !ok {
		return 0, fmt.Errorf("bad register operand")
	}
	nzcv, ok := parseImm(ops[2])
	if !ok || nzcv < 0 || nzcv > 15 {
		return 0, fmt.Errorf("nzcv must be 0..15")
	}
	cond, ok := condCodes[strings.ToLower(strings.TrimSpace(ops[3]))]
	if !ok {
		return 0, fmt.Errorf("unknown condition %q", ops[3])
	}
	word := sfBit(rn) | base | cond<<12 | rn.num<<5 | uint32(nzcv)
	if strings.HasPrefix(strings.TrimSpace(ops[1]), "#") || isImmOperand(ops[1]) {
		imm, ok := parseImm(ops[1])
		if !ok || imm < 0 || imm > 31 {
			return 0, fmt.Errorf("ccmp immediate must be 0..31")
		}
		return word | 1<<11 | uint32(imm)<<16, nil // bit11 = immediate form
	}
	rm, ok := parseReg(ops[1])
	if !ok {
		return 0, fmt.Errorf("bad comparand register")
	}
	return word | rm.num<<16, nil
}
