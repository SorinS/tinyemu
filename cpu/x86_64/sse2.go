package x86_64

import (
	"fmt"
	ssemath "math"
)

// SSE2 / MMX dispatch for the long-mode backend. Ported from
// cpu/x86/exec.go (the SSE2/MMX `case 0xNN:` blocks inside `case
// 0x0F:`). Each opcode follows the same dual-purpose pattern as i386:
//   - no prefix       → MMX (operates on 64-bit MM regs)
//   - 66 prefix       → SSE2 (operates on 128-bit XMM regs)
//   - F2 / F3 prefix  → SSE2 variants with different element semantics
//
// opSSE2 returns (handled, err). If handled=false the caller falls
// through to its own "unimplemented" path. This lets opTwoByte add
// the SSE2 surface in a single switch statement without disturbing
// existing handlers.
//
// REX notes:
//   - mr.reg / mr.rm are already extended by REX.R / REX.B, so
//     c.xmm[mr.reg] and c.xmm[mr.rm] cover the full 0..15 range.
//   - MMX registers MM0..MM7 are NOT extended by REX (Intel SDM Vol
//     2A §2.1.7). We mask `& 7` whenever indexing c.mm[...].
//   - The triggering opcode for this port is 66 REX.W 0F 6E /r =
//     MOVQ xmm, r/m64. The REX.W bit makes the 0x6E source operand
//     64-bit instead of 32-bit; that's handled in case 0x6E below.

// opSSE2 dispatches the SSE2/MMX subset of the 0x0F escape opcode
// family. has66 reports whether a 0x66 operand-size override was
// present — this is the proper way to distinguish SSE2 (66 prefix)
// from MMX (no prefix), because operandSize is forced to 8 by REX.W
// regardless of whether 66 was present, so `operandSize == 2` is
// false for `66 REX.W 0F 6E` MOVQ-from-r64 (the exact opcode that
// motivated this port).
func (c *CPU) opSSE2(opcode2, rex, repPrefix uint8, has66 bool) (bool, error) {
	switch opcode2 {

	// 0F 6E: no prefix → MOVD mm,  r/m32
	//        66        → MOVD xmm, r/m32  (32-bit, zero-extends to 128)
	//        66 REX.W  → MOVQ xmm, r/m64  (64-bit, zero-extends high lane)
	case 0x6E:
		mr := c.parseModRM64(rex)
		if has66 {
			// SSE2 form.
			if rex&rexW != 0 {
				// MOVQ xmm, r/m64
				var v uint64
				if mr.isReg {
					v = c.GetReg64(int(mr.rm))
				} else {
					v = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
				}
				c.xmm[mr.reg][0] = v
				c.xmm[mr.reg][1] = 0
				return true, nil
			}
			// MOVD xmm, r/m32
			var v uint32
			if mr.isReg {
				v = c.GetReg32(int(mr.rm))
			} else {
				v = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg][0] = uint64(v)
			c.xmm[mr.reg][1] = 0
			return true, nil
		}
		// MMX form: MOVD mm, r/m32. (REX.W on MMX is reserved on
		// most CPUs but observed-as-MOVQ on some; we don't model
		// that pre-MMX-deprecation corner.)
		var v uint32
		if mr.isReg {
			v = c.GetReg32(int(mr.rm))
		} else {
			v = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
		}
		c.mm[mr.reg&7] = uint64(v)
		return true, nil

	// 0F 6F: no prefix → MOVQ mm, mm/m64
	//        66        → MOVDQA xmm, xmm/m128
	//        F3        → MOVDQU xmm, xmm/m128
	case 0x6F:
		mr := c.parseModRM64(rex)
		if has66 || repPrefix == 1 {
			var v [2]uint64
			if mr.isReg {
				v = c.xmm[mr.rm]
			} else {
				v = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg] = v
			return true, nil
		}
		var v uint64
		if mr.isReg {
			v = c.mm[mr.rm&7]
		} else {
			v = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
		}
		c.mm[mr.reg&7] = v
		return true, nil

	// 0F 7E: no prefix → MOVD r/m32, mm
	//        66        → MOVD r/m32, xmm  (low 32 only)
	//        66 REX.W  → MOVQ r/m64, xmm  (low 64 only)
	//        F3        → MOVQ xmm, xmm/m64  (zero-extend high)
	case 0x7E:
		mr := c.parseModRM64(rex)
		if repPrefix == 1 {
			var v uint64
			if mr.isReg {
				v = c.xmm[mr.rm][0]
			} else {
				v = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg][0] = v
			c.xmm[mr.reg][1] = 0
			return true, nil
		}
		if has66 {
			if rex&rexW != 0 {
				// MOVQ r/m64, xmm
				v := c.xmm[mr.reg][0]
				if mr.isReg {
					c.SetReg64(int(mr.rm), v)
				} else {
					c.writeMem64(c.segBaseForModRM(mr)+mr.ea, v)
				}
				return true, nil
			}
			// MOVD r/m32, xmm
			lo := uint32(c.xmm[mr.reg][0])
			if mr.isReg {
				c.SetReg32(int(mr.rm), lo)
			} else {
				c.writeMem32(c.segBaseForModRM(mr)+mr.ea, lo)
			}
			return true, nil
		}
		// MMX form: MOVD r/m32, mm
		lo := uint32(c.mm[mr.reg&7])
		if mr.isReg {
			c.SetReg32(int(mr.rm), lo)
		} else {
			c.writeMem32(c.segBaseForModRM(mr)+mr.ea, lo)
		}
		return true, nil

	// 0F 7F: no prefix → MOVQ mm/m64, mm
	//        66        → MOVDQA xmm/m128, xmm
	//        F3        → MOVDQU xmm/m128, xmm
	case 0x7F:
		mr := c.parseModRM64(rex)
		if has66 || repPrefix == 1 {
			v := c.xmm[mr.reg]
			if mr.isReg {
				c.xmm[mr.rm] = v
			} else {
				c.writeMem128(c.segBaseForModRM(mr)+mr.ea, v)
			}
			return true, nil
		}
		v := c.mm[mr.reg&7]
		if mr.isReg {
			c.mm[mr.rm&7] = v
		} else {
			c.writeMem64(c.segBaseForModRM(mr)+mr.ea, v)
		}
		return true, nil

	// 0F D6: 66 → MOVQ xmm/m64, xmm
	//        F2 → MOVDQ2Q mm, xmm
	//        F3 → MOVQ2DQ xmm, mm
	case 0xD6:
		mr := c.parseModRM64(rex)
		switch {
		case has66:
			v := c.xmm[mr.reg][0]
			if mr.isReg {
				c.xmm[mr.rm][0] = v
				c.xmm[mr.rm][1] = 0
			} else {
				c.writeMem64(c.segBaseForModRM(mr)+mr.ea, v)
			}
		case repPrefix == 2:
			c.mm[mr.reg&7] = c.xmm[mr.rm][0]
		case repPrefix == 1:
			c.xmm[mr.reg][0] = c.mm[mr.rm&7]
			c.xmm[mr.reg][1] = 0
		default:
			return true, fmt.Errorf("0F D6 without 66/F2/F3 prefix at RIP=%016X", c.rip-2)
		}
		return true, nil

	// PMOVMSKB r32, mm/xmm
	case 0xD7:
		mr := c.parseModRM64(rex)
		if !mr.isReg {
			return true, fmt.Errorf("PMOVMSKB requires register source at RIP=%016X", c.rip-2)
		}
		var mask uint32
		if has66 {
			v := c.xmm[mr.rm]
			for i := 0; i < 8; i++ {
				if v[0]&(1<<(i*8+7)) != 0 {
					mask |= 1 << uint(i)
				}
			}
			for i := 0; i < 8; i++ {
				if v[1]&(1<<(i*8+7)) != 0 {
					mask |= 1 << uint(8+i)
				}
			}
		} else {
			v := c.mm[mr.rm&7]
			for i := 0; i < 8; i++ {
				if v&(1<<(i*8+7)) != 0 {
					mask |= 1 << uint(i)
				}
			}
		}
		c.SetReg32(int(mr.reg), mask)
		return true, nil

	case 0x77: // EMMS — no ModR/M, no state to update (we don't track x87 tag)
		return true, nil

	// Non-temporal stores
	case 0xE7: // MOVNTQ m64, mm
		mr := c.parseModRM64(rex)
		if mr.isReg {
			return true, fmt.Errorf("MOVNTQ requires memory dest at RIP=%016X", c.rip-2)
		}
		c.writeMem64(c.segBaseForModRM(mr)+mr.ea, c.mm[mr.reg&7])
		return true, nil

	case 0x2B: // MOVNTPS/PD m128, xmm
		mr := c.parseModRM64(rex)
		if mr.isReg {
			return true, fmt.Errorf("MOVNTPS/PD requires memory dest at RIP=%016X", c.rip-2)
		}
		c.writeMem128(c.segBaseForModRM(mr)+mr.ea, c.xmm[mr.reg])
		return true, nil

	case 0xC3: // MOVNTI m32/64, r32/64
		mr := c.parseModRM64(rex)
		if mr.isReg {
			return true, fmt.Errorf("MOVNTI requires memory dest at RIP=%016X", c.rip-2)
		}
		if rex&rexW != 0 {
			c.writeMem64(c.segBaseForModRM(mr)+mr.ea, c.GetReg64(int(mr.reg)))
		} else {
			c.writeMem32(c.segBaseForModRM(mr)+mr.ea, c.GetReg32(int(mr.reg)))
		}
		return true, nil

	// MOVMSKPS / MOVMSKPD: sign bits of packed singles/doubles → GPR
	case 0x50:
		mr := c.parseModRM64(rex)
		if !mr.isReg {
			return true, fmt.Errorf("MOVMSKPS/PD requires register source at RIP=%016X", c.rip-2)
		}
		v := c.xmm[mr.rm]
		var mask uint32
		if has66 {
			if v[0]&(1<<63) != 0 {
				mask |= 1
			}
			if v[1]&(1<<63) != 0 {
				mask |= 2
			}
		} else {
			if v[0]&(1<<31) != 0 {
				mask |= 1
			}
			if v[0]&(1<<63) != 0 {
				mask |= 2
			}
			if v[1]&(1<<31) != 0 {
				mask |= 4
			}
			if v[1]&(1<<63) != 0 {
				mask |= 8
			}
		}
		c.SetReg32(int(mr.reg), mask)
		return true, nil

	// ANDPS/ANDNPS/ORPS/XORPS (operand-size override gives the PD variants)
	case 0x54, 0x55, 0x56, 0x57:
		mr := c.parseModRM64(rex)
		var src [2]uint64
		if mr.isReg {
			src = c.xmm[mr.rm]
		} else {
			src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
		}
		dst := c.xmm[mr.reg]
		switch opcode2 {
		case 0x54:
			dst[0] &= src[0]
			dst[1] &= src[1]
		case 0x55:
			dst[0] = (^dst[0]) & src[0]
			dst[1] = (^dst[1]) & src[1]
		case 0x56:
			dst[0] |= src[0]
			dst[1] |= src[1]
		case 0x57:
			dst[0] ^= src[0]
			dst[1] ^= src[1]
		}
		c.xmm[mr.reg] = dst
		return true, nil

	// MOVAPS / MOVAPD load
	case 0x28:
		mr := c.parseModRM64(rex)
		var v [2]uint64
		if mr.isReg {
			v = c.xmm[mr.rm]
		} else {
			v = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
		}
		c.xmm[mr.reg] = v
		return true, nil

	// UCOMISS/UCOMISD/COMISS/COMISD
	case 0x2E, 0x2F:
		mr := c.parseModRM64(rex)
		var a, b float64
		if has66 {
			var ub uint64
			if mr.isReg {
				ub = c.xmm[mr.rm][0]
			} else {
				ub = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
			}
			a = ssemath.Float64frombits(c.xmm[mr.reg][0])
			b = ssemath.Float64frombits(ub)
		} else {
			var ub uint32
			if mr.isReg {
				ub = uint32(c.xmm[mr.rm][0])
			} else {
				ub = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
			}
			a = float64(ssemath.Float32frombits(uint32(c.xmm[mr.reg][0])))
			b = float64(ssemath.Float32frombits(ub))
		}
		c.fpuCompareSetFlagsRFlags(a, b)
		return true, nil

	// UNPCKLPS / UNPCKHPS (no prefix), UNPCKLPD / UNPCKHPD (66 prefix)
	case 0x14, 0x15:
		mr := c.parseModRM64(rex)
		var src [2]uint64
		if mr.isReg {
			src = c.xmm[mr.rm]
		} else {
			src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
		}
		dst := c.xmm[mr.reg]
		if has66 {
			if opcode2 == 0x14 {
				c.xmm[mr.reg] = [2]uint64{dst[0], src[0]}
			} else {
				c.xmm[mr.reg] = [2]uint64{dst[1], src[1]}
			}
		} else {
			if opcode2 == 0x14 {
				a0 := dst[0] & 0xFFFFFFFF
				a1 := (dst[0] >> 32) & 0xFFFFFFFF
				b0 := src[0] & 0xFFFFFFFF
				b1 := (src[0] >> 32) & 0xFFFFFFFF
				c.xmm[mr.reg] = [2]uint64{a0 | (b0 << 32), a1 | (b1 << 32)}
			} else {
				a2 := dst[1] & 0xFFFFFFFF
				a3 := (dst[1] >> 32) & 0xFFFFFFFF
				b2 := src[1] & 0xFFFFFFFF
				b3 := (src[1] >> 32) & 0xFFFFFFFF
				c.xmm[mr.reg] = [2]uint64{a2 | (b2 << 32), a3 | (b3 << 32)}
			}
		}
		return true, nil

	// MOVAPS / MOVAPD store
	case 0x29:
		mr := c.parseModRM64(rex)
		v := c.xmm[mr.reg]
		if mr.isReg {
			c.xmm[mr.rm] = v
		} else {
			c.writeMem128(c.segBaseForModRM(mr)+mr.ea, v)
		}
		return true, nil

	// MOVUPS / MOVUPD / MOVSS / MOVSD (load)
	case 0x10:
		mr := c.parseModRM64(rex)
		switch repPrefix {
		case 1: // F3 → MOVSS
			if mr.isReg {
				c.xmm[mr.reg][0] = (c.xmm[mr.reg][0] &^ 0xFFFFFFFF) |
					(c.xmm[mr.rm][0] & 0xFFFFFFFF)
			} else {
				v := uint64(c.readMem32(c.segBaseForModRM(mr) + mr.ea))
				c.xmm[mr.reg][0] = v
				c.xmm[mr.reg][1] = 0
			}
		case 2: // F2 → MOVSD
			if mr.isReg {
				c.xmm[mr.reg][0] = c.xmm[mr.rm][0]
			} else {
				c.xmm[mr.reg][0] = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
				c.xmm[mr.reg][1] = 0
			}
		default: // none/66 → MOVUPS/MOVUPD
			var v [2]uint64
			if mr.isReg {
				v = c.xmm[mr.rm]
			} else {
				v = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg] = v
		}
		return true, nil

	// MOVUPS / MOVUPD / MOVSS / MOVSD (store)
	case 0x11:
		mr := c.parseModRM64(rex)
		switch repPrefix {
		case 1: // F3 → MOVSS store
			if mr.isReg {
				c.xmm[mr.rm][0] = (c.xmm[mr.rm][0] &^ 0xFFFFFFFF) |
					(c.xmm[mr.reg][0] & 0xFFFFFFFF)
			} else {
				c.writeMem32(c.segBaseForModRM(mr)+mr.ea, uint32(c.xmm[mr.reg][0]))
			}
		case 2: // F2 → MOVSD store
			if mr.isReg {
				c.xmm[mr.rm][0] = c.xmm[mr.reg][0]
			} else {
				c.writeMem64(c.segBaseForModRM(mr)+mr.ea, c.xmm[mr.reg][0])
			}
		default: // none/66 → MOVUPS/MOVUPD store
			v := c.xmm[mr.reg]
			if mr.isReg {
				c.xmm[mr.rm] = v
			} else {
				c.writeMem128(c.segBaseForModRM(mr)+mr.ea, v)
			}
		}
		return true, nil

	// PAND / PANDN / POR / PXOR
	case 0xDB, 0xDF, 0xEB, 0xEF:
		mr := c.parseModRM64(rex)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			switch opcode2 {
			case 0xDB:
				dst[0] &= src[0]
				dst[1] &= src[1]
			case 0xDF:
				dst[0] = (^dst[0]) & src[0]
				dst[1] = (^dst[1]) & src[1]
			case 0xEB:
				dst[0] |= src[0]
				dst[1] |= src[1]
			case 0xEF:
				dst[0] ^= src[0]
				dst[1] ^= src[1]
			}
			c.xmm[mr.reg] = dst
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		switch opcode2 {
		case 0xDB:
			dst = dst & src
		case 0xDF:
			dst = (^dst) & src
		case 0xEB:
			dst = dst | src
		case 0xEF:
			dst = dst ^ src
		}
		c.mm[mr.reg&7] = dst
		return true, nil

	// PCMPEQ B/W/D
	case 0x74, 0x75, 0x76:
		mr := c.parseModRM64(rex)
		elem := 1 << (opcode2 - 0x74)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedCmpEq(dst[0], src[0], elem),
				packedCmpEq(dst[1], src[1], elem),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedCmpEq(dst, src, elem)
		return true, nil

	// PCMPGT B/W/D (signed)
	case 0x64, 0x65, 0x66:
		mr := c.parseModRM64(rex)
		elem := 1 << (opcode2 - 0x64)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedCmpGt(dst[0], src[0], elem),
				packedCmpGt(dst[1], src[1], elem),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedCmpGt(dst, src, elem)
		return true, nil

	// PADD B/W/D/Q
	case 0xFC, 0xFD, 0xFE, 0xD4:
		mr := c.parseModRM64(rex)
		var size int
		switch opcode2 {
		case 0xFC:
			size = 1
		case 0xFD:
			size = 2
		case 0xFE:
			size = 4
		case 0xD4:
			size = 8
		}
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedAdd(dst[0], src[0], size),
				packedAdd(dst[1], src[1], size),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedAdd(dst, src, size)
		return true, nil

	// PSUB B/W/D/Q
	case 0xF8, 0xF9, 0xFA, 0xFB:
		mr := c.parseModRM64(rex)
		var size int
		switch opcode2 {
		case 0xF8:
			size = 1
		case 0xF9:
			size = 2
		case 0xFA:
			size = 4
		case 0xFB:
			size = 8
		}
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedSub(dst[0], src[0], size),
				packedSub(dst[1], src[1], size),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedSub(dst, src, size)
		return true, nil

	// Saturating add: PADDUSB/PADDUSW/PADDSB/PADDSW
	case 0xDC, 0xDD, 0xEC, 0xED:
		mr := c.parseModRM64(rex)
		elem := 2
		if opcode2 == 0xDC || opcode2 == 0xEC {
			elem = 1
		}
		signed := opcode2 >= 0xEC
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedAddSat(dst[0], src[0], elem, signed),
				packedAddSat(dst[1], src[1], elem, signed),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedAddSat(dst, src, elem, signed)
		return true, nil

	// Saturating sub: PSUBUSB/PSUBUSW/PSUBSB/PSUBSW
	case 0xD8, 0xD9, 0xE8, 0xE9:
		mr := c.parseModRM64(rex)
		elem := 2
		if opcode2 == 0xD8 || opcode2 == 0xE8 {
			elem = 1
		}
		signed := opcode2 >= 0xE8
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedSubSat(dst[0], src[0], elem, signed),
				packedSubSat(dst[1], src[1], elem, signed),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedSubSat(dst, src, elem, signed)
		return true, nil

	// PUNPCKL{BW,WD,DQ}
	case 0x60, 0x61, 0x62:
		mr := c.parseModRM64(rex)
		elem := 1 << (opcode2 - 0x60)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedUnpackLow(dst[0], src[0], elem),
				packedUnpackHigh(dst[0], src[0], elem),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedUnpackLow(dst, src, elem)
		return true, nil

	// PUNPCKH{BW,WD,DQ}
	case 0x68, 0x69, 0x6A:
		mr := c.parseModRM64(rex)
		elem := 1 << (opcode2 - 0x68)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedUnpackLow(dst[1], src[1], elem),
				packedUnpackHigh(dst[1], src[1], elem),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedUnpackHigh(dst, src, elem)
		return true, nil

	// PUNPCKLQDQ xmm, xmm/m128 (66 0F 6C) — SSE2 only.
	case 0x6C:
		if !has66 {
			return true, fmt.Errorf("0F 6C requires 66 prefix at RIP=%016X", c.rip-2)
		}
		mr := c.parseModRM64(rex)
		var src [2]uint64
		if mr.isReg {
			src = c.xmm[mr.rm]
		} else {
			src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
		}
		c.xmm[mr.reg] = [2]uint64{c.xmm[mr.reg][0], src[0]}
		return true, nil

	// PUNPCKHQDQ xmm, xmm/m128 (66 0F 6D) — SSE2 only.
	case 0x6D:
		if !has66 {
			return true, fmt.Errorf("0F 6D requires 66 prefix at RIP=%016X", c.rip-2)
		}
		mr := c.parseModRM64(rex)
		var src [2]uint64
		if mr.isReg {
			src = c.xmm[mr.rm]
		} else {
			src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
		}
		c.xmm[mr.reg] = [2]uint64{c.xmm[mr.reg][1], src[1]}
		return true, nil

	// PACKSSWB (0F 63) / PACKSSDW (0F 6B) — signed-saturation pack.
	case 0x63, 0x6B:
		mr := c.parseModRM64(rex)
		srcSize := 2
		if opcode2 == 0x6B {
			srcSize = 4
		}
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packSignedSat(dst[0], dst[1], srcSize),
				packSignedSat(src[0], src[1], srcSize),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packSignedSat(dst, src, srcSize)
		return true, nil

	// PACKUSWB (0F 67) — unsigned-saturation pack.
	case 0x67:
		mr := c.parseModRM64(rex)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packUnsignedSat(dst[0], dst[1]),
				packUnsignedSat(src[0], src[1]),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packUnsignedSat(dst, src)
		return true, nil

	// PINSRW (0F C4)
	case 0xC4:
		mr := c.parseModRM64WithImm(rex, 1)
		var val uint16
		if mr.isReg {
			val = uint16(c.GetReg32(int(mr.rm)))
		} else {
			val = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		imm := c.fetch8()
		if has66 {
			idx := imm & 7
			v := c.xmm[mr.reg]
			if idx < 4 {
				shift := uint(idx) * 16
				v[0] = (v[0] &^ (uint64(0xFFFF) << shift)) | (uint64(val) << shift)
			} else {
				shift := uint(idx-4) * 16
				v[1] = (v[1] &^ (uint64(0xFFFF) << shift)) | (uint64(val) << shift)
			}
			c.xmm[mr.reg] = v
		} else {
			idx := imm & 3
			shift := uint(idx) * 16
			c.mm[mr.reg&7] = (c.mm[mr.reg&7] &^ (uint64(0xFFFF) << shift)) |
				(uint64(val) << shift)
		}
		return true, nil

	// PEXTRW (0F C5)
	case 0xC5:
		mr := c.parseModRM64WithImm(rex, 1)
		if !mr.isReg {
			return true, fmt.Errorf("PEXTRW requires register source at RIP=%016X", c.rip-2)
		}
		imm := c.fetch8()
		var val uint16
		if has66 {
			idx := imm & 7
			v := c.xmm[mr.rm]
			if idx < 4 {
				val = uint16(v[0] >> (uint(idx) * 16))
			} else {
				val = uint16(v[1] >> (uint(idx-4) * 16))
			}
		} else {
			idx := imm & 3
			val = uint16(c.mm[mr.rm&7] >> (uint(idx) * 16))
		}
		c.SetReg32(int(mr.reg), uint32(val))
		return true, nil

	// PMULLW (0F D5), PMULHW (0F E5)
	case 0xD5, 0xE5:
		mr := c.parseModRM64(rex)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			if opcode2 == 0xD5 {
				c.xmm[mr.reg] = [2]uint64{
					packedMulLow(dst[0], src[0]),
					packedMulLow(dst[1], src[1]),
				}
			} else {
				c.xmm[mr.reg] = [2]uint64{
					packedMulHigh(dst[0], src[0], true),
					packedMulHigh(dst[1], src[1], true),
				}
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		if opcode2 == 0xD5 {
			c.mm[mr.reg&7] = packedMulLow(dst, src)
		} else {
			c.mm[mr.reg&7] = packedMulHigh(dst, src, true)
		}
		return true, nil

	// PMULUDQ (0F F4)
	case 0xF4:
		mr := c.parseModRM64(rex)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedMulUDQ(dst[0], src[0]),
				packedMulUDQ(dst[1], src[1]),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedMulUDQ(dst, src)
		return true, nil

	// PMADDWD (0F F5)
	case 0xF5:
		mr := c.parseModRM64(rex)
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			c.xmm[mr.reg] = [2]uint64{
				packedMaddWord(dst[0], src[0]),
				packedMaddWord(dst[1], src[1]),
			}
			return true, nil
		}
		src := c.mmxSrc64(mr)
		dst := c.mm[mr.reg&7]
		c.mm[mr.reg&7] = packedMaddWord(dst, src)
		return true, nil

	// Variable-count shifts: PSRLW/PSRLD/PSRLQ / PSRAW/PSRAD / PSLLW/PSLLD/PSLLQ
	case 0xD1, 0xD2, 0xD3, 0xE1, 0xE2, 0xF1, 0xF2, 0xF3:
		mr := c.parseModRM64(rex)
		var elem int
		switch opcode2 & 0x0F {
		case 0x01:
			elem = 2
		case 0x02:
			elem = 4
		case 0x03:
			elem = 8
		}
		if has66 {
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			count := int(src[0] & 0xFF)
			dst := c.xmm[mr.reg]
			var fn func(uint64, int, int) uint64
			switch {
			case opcode2 >= 0xF1:
				fn = packedShiftLeft
			case opcode2 >= 0xE1:
				fn = packedShiftRightArith
			default:
				fn = packedShiftRightLogical
			}
			c.xmm[mr.reg] = [2]uint64{
				fn(dst[0], count, elem),
				fn(dst[1], count, elem),
			}
			return true, nil
		}
		count := int(c.mmxSrc64(mr) & 0xFF)
		dst := c.mm[mr.reg&7]
		switch {
		case opcode2 >= 0xF1:
			c.mm[mr.reg&7] = packedShiftLeft(dst, count, elem)
		case opcode2 >= 0xE1:
			c.mm[mr.reg&7] = packedShiftRightArith(dst, count, elem)
		default:
			c.mm[mr.reg&7] = packedShiftRightLogical(dst, count, elem)
		}
		return true, nil

	// PSHUFW (MMX) / PSHUFD/PSHUFLW/PSHUFHW (SSE2) — opcode 0F 70.
	case 0x70:
		mr := c.parseModRM64WithImm(rex, 1)
		switch {
		case has66: // 66 → PSHUFD
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			imm := c.fetch8()
			c.xmm[mr.reg] = pshufDword(src, imm)
		case repPrefix == 2: // F2 → PSHUFLW
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			imm := c.fetch8()
			c.xmm[mr.reg][0] = pshufWord(src[0], imm)
			c.xmm[mr.reg][1] = src[1]
		case repPrefix == 1: // F3 → PSHUFHW
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			imm := c.fetch8()
			c.xmm[mr.reg][0] = src[0]
			c.xmm[mr.reg][1] = pshufWord(src[1], imm)
		default: // no prefix → PSHUFW
			src := c.mmxSrc64(mr)
			imm := c.fetch8()
			c.mm[mr.reg&7] = pshufWord(src, imm)
		}
		return true, nil

	// Immediate-count shifts (group-encoded): 0F 71/72/73 /reg /rm.
	// reg field of ModR/M picks the sub-op (/6 PSLL, /4 PSRA, /2 PSRL,
	// /7 PSLLDQ, /3 PSRLDQ). The DEST is mr.rm (NOT mr.reg) — this
	// is the most error-prone part of the SSE2 surface.
	case 0x71, 0x72, 0x73:
		mr := c.parseModRM64WithImm(rex, 1)
		imm := int(c.fetch8())
		var elem int
		switch opcode2 {
		case 0x71:
			elem = 2
		case 0x72:
			elem = 4
		case 0x73:
			elem = 8
		}
		if has66 {
			v := c.xmm[mr.rm]
			switch mr.reg {
			case 6: // PSLLW/D/Q
				v[0] = packedShiftLeft(v[0], imm, elem)
				v[1] = packedShiftLeft(v[1], imm, elem)
			case 4: // PSRAW/D (no PSRAQ)
				if opcode2 == 0x73 {
					return true, fmt.Errorf("PSRAQ does not exist (66 0F 73 /4)")
				}
				v[0] = packedShiftRightArith(v[0], imm, elem)
				v[1] = packedShiftRightArith(v[1], imm, elem)
			case 2: // PSRLW/D/Q
				v[0] = packedShiftRightLogical(v[0], imm, elem)
				v[1] = packedShiftRightLogical(v[1], imm, elem)
			case 7: // PSLLDQ (full 128-bit byte shift left)
				if opcode2 != 0x73 {
					return true, fmt.Errorf("PSLLDQ only valid as 66 0F 73 /7, got 0F %02X /7", opcode2)
				}
				v = byteShiftLeft128(v, imm)
			case 3: // PSRLDQ (full 128-bit byte shift right)
				if opcode2 != 0x73 {
					return true, fmt.Errorf("PSRLDQ only valid as 66 0F 73 /3, got 0F %02X /3", opcode2)
				}
				v = byteShiftRight128(v, imm)
			default:
				return true, fmt.Errorf("unsupported SSE2 66 0F %02X /%d at RIP=%016X", opcode2, mr.reg, c.rip-3)
			}
			c.xmm[mr.rm] = v
			return true, nil
		}
		dst := c.mm[mr.rm&7]
		switch mr.reg {
		case 6: // PSLL
			c.mm[mr.rm&7] = packedShiftLeft(dst, imm, elem)
		case 4: // PSRA (W/D only; not Q)
			if opcode2 == 0x73 {
				return true, fmt.Errorf("PSRAQ does not exist in MMX (0F 73 /4)")
			}
			c.mm[mr.rm&7] = packedShiftRightArith(dst, imm, elem)
		case 2: // PSRL
			c.mm[mr.rm&7] = packedShiftRightLogical(dst, imm, elem)
		default:
			return true, fmt.Errorf("unsupported MMX 0F %02X /%d at RIP=%016X", opcode2, mr.reg, c.rip-3)
		}
		return true, nil
	}

	return false, nil
}
