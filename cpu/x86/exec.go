package x86

import (
	"fmt"
	ssemath "math"
	"os"
	"strings"
)

// detectDecJnzLoop returns true if the next two bytes after EIP form
// `JNZ -3`, indicating a one-byte DEC reg followed by a backward jump to
// itself. Used to fast-path Linux's __delay() busy loop.
func (c *CPU) detectDecJnzLoop() bool {
	lip := c.GetLIP()
	return c.readMem8(lip) == 0x75 && c.readMem8(lip+1) == 0xFD
}

// readPhys8 reads a byte from the given physical address (no paging).
func (c *CPU) readPhys8(addr uint32) uint8 {
	v, _ := c.memMap.Read8(uint64(addr & c.a20Mask))
	return v
}

// readPhys16 reads a word from the given physical address (no paging).
func (c *CPU) readPhys16(addr uint32) uint16 {
	v, _ := c.memMap.Read16(uint64(addr & c.a20Mask))
	return v
}

// readPhys32 reads a dword from the given physical address (no paging).
func (c *CPU) readPhys32(addr uint32) uint32 {
	v, _ := c.memMap.Read32(uint64(addr & c.a20Mask))
	if physWatchActive {
		c.physReadWatchHook(addr, v, 4)
	}
	return v
}

// physReadWatchHook is called by readPhys* — same range as physwatch.
func (c *CPU) physReadWatchHook(addr uint32, val uint32, size int) {
	if physWatchLo == 0 && physWatchHi == 0 {
		return
	}
	if addr < physWatchLo || addr >= physWatchHi {
		return
	}
	fmt.Fprintf(os.Stderr, "[physread] cycles=%d EIP=0x%08X read phys=0x%08X size=%d val=0x%08X\n",
		c.cycles, c.eip, addr, size, val)
}

// eipBreakpoint, if non-zero, prints register state when EIP reaches it.
// Set via TINYEMU_X86_EIPBP=hex (single address). Cheap "single breakpoint".
var eipBreakpoint = func() uint32 {
	s := os.Getenv("TINYEMU_X86_EIPBP")
	if s == "" {
		return 0
	}
	var v uint32
	if _, err := fmt.Sscanf(s, "%x", &v); err != nil {
		return 0
	}
	return v
}()

// eipBreakpoints supports multiple addresses, comma-separated hex.
// e.g. TINYEMU_X86_EIPBPS=c22a8220,c22a8248,c22a829e
var eipBreakpoints = func() map[uint32]bool {
	s := os.Getenv("TINYEMU_X86_EIPBPS")
	m := map[uint32]bool{}
	if s == "" {
		return m
	}
	for _, p := range strings.Split(s, ",") {
		var v uint32
		if _, err := fmt.Sscanf(p, "%x", &v); err == nil {
			m[v] = true
		}
	}
	return m
}()

// eipBPActive collapses the (eipBreakpoint != 0 || len(eipBreakpoints) > 0)
// check into a single bool so Step doesn't pay a map lookup on every
// instruction in the default (no-breakpoint) build.
var eipBPActive = eipBreakpoint != 0 || len(eipBreakpoints) > 0

// ReadPhys32 is the exported wrapper around readPhys32 for diagnostic tools
// that need to inspect raw physical memory (e.g. page tables).
func (c *CPU) ReadPhys32(addr uint32) uint32 { return c.readPhys32(addr) }

// readPhys64 reads a qword from the given physical address (no paging). Used
// by PAE paging-structure walks.
func (c *CPU) readPhys64(addr uint32) uint64 {
	lo := uint64(c.readPhys32(addr))
	hi := uint64(c.readPhys32(addr + 4))
	return lo | (hi << 32)
}

// writePhys64 writes a qword to the given physical address (no paging).
func (c *CPU) writePhys64(addr uint32, val uint64) {
	c.writePhys32(addr, uint32(val))
	c.writePhys32(addr+4, uint32(val>>32))
}

// writePhys8 writes a byte to the given physical address (no paging).
func (c *CPU) writePhys8(addr uint32, val uint8) {
	if physWatchActive {
		c.physWatchHook(addr, uint32(val), 1)
	}
	c.maybeInvalidateFetchBufferPhys(addr, 1)
	c.memMap.Write8(uint64(addr&c.a20Mask), val)
}

// writePhys16 writes a word to the given physical address (no paging).
func (c *CPU) writePhys16(addr uint32, val uint16) {
	if physWatchActive {
		c.physWatchHook(addr, uint32(val), 2)
	}
	c.maybeInvalidateFetchBufferPhys(addr, 2)
	c.memMap.Write16(uint64(addr&c.a20Mask), val)
}

// writePhys32 writes a dword to the given physical address (no paging).
func (c *CPU) writePhys32(addr uint32, val uint32) {
	if physWatchActive {
		c.physWatchHook(addr, val, 4)
	}
	c.maybeInvalidateFetchBufferPhys(addr, 4)
	c.memMap.Write32(uint64(addr&c.a20Mask), val)
}

// readMem8 reads a byte from the given linear address (with paging).
func (c *CPU) readMem8(addr uint32) uint8 {
	phys := c.translateAddress(addr, false, c.cpl == 3, false)
	v := c.readPhys8(phys)
	if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
		fmt.Fprintf(os.Stderr, "[RD8] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X phys=%08X val=%02X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, phys, v)
	}
	return v
}

// readMem16 reads a word from the given linear address (with paging).
// Unaligned reads that cross a page boundary translate each page separately.
func (c *CPU) readMem16(addr uint32) uint16 {
	if (addr & 0xFFF) > 0xFFE {
		// Crosses page boundary — read byte-by-byte (each byte translates).
		lo := uint16(c.readMem8(addr))
		hi := uint16(c.readMem8(addr + 1))
		v := lo | (hi << 8)
		if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
			fmt.Fprintf(os.Stderr, "[RD16x] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X (cross) val=%04X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, v)
		}
		return v
	}
	phys := c.translateAddress(addr, false, c.cpl == 3, false)
	v := c.readPhys16(phys)
	if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
		fmt.Fprintf(os.Stderr, "[RD16] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X phys=%08X val=%04X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, phys, v)
	}
	return v
}

// readMem32 reads a dword from the given linear address (with paging).
// Unaligned reads that cross a page boundary translate each page separately.
func (c *CPU) readMem32(addr uint32) uint32 {
	if (addr & 0xFFF) > 0xFFC {
		// Crosses page boundary — read byte-by-byte (each byte translates).
		b0 := uint32(c.readMem8(addr))
		b1 := uint32(c.readMem8(addr + 1))
		b2 := uint32(c.readMem8(addr + 2))
		b3 := uint32(c.readMem8(addr + 3))
		v := b0 | (b1 << 8) | (b2 << 16) | (b3 << 24)
		if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
			fmt.Fprintf(os.Stderr, "[RD32x] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X (cross) val=%08X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, v)
		}
		return v
	}
	phys := c.translateAddress(addr, false, c.cpl == 3, false)
	v := c.readPhys32(phys)
	if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
		fmt.Fprintf(os.Stderr, "[RD32] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X phys=%08X val=%08X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, phys, v)
	}
	return v
}

// writeMem8 writes a byte to the given linear address (with paging).
func (c *CPU) writeMem8(addr uint32, val uint8) {
	if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
		fmt.Fprintf(os.Stderr, "[WW8] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X val=%02X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, val)
	}
	c.writePhys8(c.translateAddress(addr, true, c.cpl == 3, false), val)
}

// writeMem16 writes a word to the given linear address (with paging).
// Unaligned writes that cross a page boundary translate each page separately.
func (c *CPU) writeMem16(addr uint32, val uint16) {
	if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
		fmt.Fprintf(os.Stderr, "[WW16] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X val=%04X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, val)
	}
	if (addr & 0xFFF) > 0xFFE {
		c.writeMem8(addr, uint8(val))
		c.writeMem8(addr+1, uint8(val>>8))
		return
	}
	c.writePhys16(c.translateAddress(addr, true, c.cpl == 3, false), val)
}

// writeMem32 writes a dword to the given linear address (with paging).
// Unaligned writes that cross a page boundary translate each page separately —
// this matters because the two pages may map to non-consecutive physical
// pages, and writing all 4 bytes to consecutive phys offsets corrupts whatever
// happens to live just past the source page's mapping.
func (c *CPU) writeMem32(addr uint32, val uint32) {
	if watchWrites && watchWriteLo <= addr && addr < watchWriteHi {
		phys := c.translateAddress(addr, true, c.cpl == 3, false)
		fmt.Fprintf(os.Stderr, "[WW32] step=%d EIP=%08X CPL=%d CR3=%08X lin=%08X phys=%08X val=%08X\n", c.cycles, c.eip, c.cpl, c.cr[3], addr, phys, val)
		c.writePhys32(phys, val)
		return
	}
	if (addr & 0xFFF) > 0xFFC {
		c.writeMem8(addr, uint8(val))
		c.writeMem8(addr+1, uint8(val>>8))
		c.writeMem8(addr+2, uint8(val>>16))
		c.writeMem8(addr+3, uint8(val>>24))
		return
	}
	c.writePhys32(c.translateAddress(addr, true, c.cpl == 3, false), val)
}

// readMem64 reads a qword from the given linear address. Used for MMX
// MOVQ. Issues two 32-bit reads — if either page-faults the panic
// unwinds normally; we don't attempt to roll back the first read.
func (c *CPU) readMem64(addr uint32) uint64 {
	lo := uint64(c.readMem32(addr))
	hi := uint64(c.readMem32(addr + 4))
	return lo | hi<<32
}

// writeMem64 writes a qword to the given linear address. Used for MMX
// MOVQ store.
func (c *CPU) writeMem64(addr uint32, val uint64) {
	c.writeMem32(addr, uint32(val))
	c.writeMem32(addr+4, uint32(val>>32))
}

// readMem128 reads a 128-bit value as [lo, hi] uint64s. Used by SSE
// move instructions.
func (c *CPU) readMem128(addr uint32) [2]uint64 {
	return [2]uint64{c.readMem64(addr), c.readMem64(addr + 8)}
}

// writeMem128 writes a 128-bit value.
func (c *CPU) writeMem128(addr uint32, val [2]uint64) {
	c.writeMem64(addr, val[0])
	c.writeMem64(addr+8, val[1])
}

// fetchMem8 reads a code byte from the given linear address. Faults during
// instruction fetch get bit 4 set in the #PF error code, and the U/S bit
// reflects whether the fetch happens at CPL=3.
func (c *CPU) fetchMem8(addr uint32) uint8 {
	return c.readPhys8(c.translateAddress(addr, false, c.cpl == 3, true))
}

// fetchMem16 reads a code word. Cross-page accesses split byte-by-byte —
// see writeMem32's comment for why this matters.
func (c *CPU) fetchMem16(addr uint32) uint16 {
	if (addr & 0xFFF) > 0xFFE {
		return uint16(c.fetchMem8(addr)) | (uint16(c.fetchMem8(addr+1)) << 8)
	}
	return c.readPhys16(c.translateAddress(addr, false, c.cpl == 3, true))
}

// fetchMem32 reads a code dword. Cross-page accesses split byte-by-byte.
// This matters for any instruction with a >1-byte immediate/displacement
// that straddles a page boundary — most critically a CALL rel32 whose
// 4-byte offset spans pages: the high byte ends up on the second page and
// must translate through the second page's PTE.
func (c *CPU) fetchMem32(addr uint32) uint32 {
	if (addr & 0xFFF) > 0xFFC {
		b0 := uint32(c.fetchMem8(addr))
		b1 := uint32(c.fetchMem8(addr + 1))
		b2 := uint32(c.fetchMem8(addr + 2))
		b3 := uint32(c.fetchMem8(addr + 3))
		return b0 | (b1 << 8) | (b2 << 16) | (b3 << 24)
	}
	return c.readPhys32(c.translateAddress(addr, false, c.cpl == 3, true))
}

// Supervisor-mode memory access helpers. The CPU itself accesses the IDT,
// GDT, LDT, and TSS as a privileged agent regardless of the current CPL —
// Intel SDM Vol. 3 §6.12.1.2 / §7.2.3. These accesses must NOT carry the U/S
// bit in their page-fault error code (they would never page-fault on a
// user-only mapping on real hardware). Routing IDT/TSS/GDT reads through
// these helpers fixes a triple-fault that occurred when delivering a #PF
// raised at CPL=3: the implicit IDT read inherited user=true and faulted on
// the kernel-owned IDT page. Cross-page accesses split byte-by-byte (an
// 8-byte IDT/GDT descriptor can straddle a page boundary).
func (c *CPU) readMemSV8(addr uint32) uint8 {
	return c.readPhys8(c.translateAddress(addr, false, false, false))
}

func (c *CPU) readMemSV16(addr uint32) uint16 {
	if (addr & 0xFFF) > 0xFFE {
		return uint16(c.readMemSV8(addr)) | (uint16(c.readMemSV8(addr+1)) << 8)
	}
	return c.readPhys16(c.translateAddress(addr, false, false, false))
}

func (c *CPU) readMemSV32(addr uint32) uint32 {
	if (addr & 0xFFF) > 0xFFC {
		b0 := uint32(c.readMemSV8(addr))
		b1 := uint32(c.readMemSV8(addr + 1))
		b2 := uint32(c.readMemSV8(addr + 2))
		b3 := uint32(c.readMemSV8(addr + 3))
		return b0 | (b1 << 8) | (b2 << 16) | (b3 << 24)
	}
	return c.readPhys32(c.translateAddress(addr, false, false, false))
}

// Exported memory accessors for tests and loaders.
func (c *CPU) ReadMem8(addr uint32) uint8   { return c.readMem8(addr) }
func (c *CPU) ReadMem16(addr uint32) uint16 { return c.readMem16(addr) }
func (c *CPU) ReadMem32(addr uint32) uint32 { return c.readMem32(addr) }
func (c *CPU) WriteMem8(addr uint32, val uint8)   { c.writeMem8(addr, val) }
func (c *CPU) WriteMem16(addr uint32, val uint16) { c.writeMem16(addr, val) }
func (c *CPU) WriteMem32(addr uint32, val uint32) { c.writeMem32(addr, val) }

func (c *CPU) maskEIP() {
	if c.DefaultOperandSize() == 2 {
		c.eip &= 0xFFFF
	}
}

// fetch8 reads the next byte from the code stream and advances EIP.
// Fast path: serve from the prefetch buffer. Slow path: refill the
// buffer from physical memory (up to a page boundary), then return.
func (c *CPU) fetch8() uint8 {
	lip := c.segBase[CS] + c.eip
	off := lip - c.ifBufLip
	if off < uint32(c.ifBufValid) {
		b := c.ifBuf[off]
		c.eip++
		c.maskEIP()
		return b
	}
	return c.fetch8Slow(lip)
}

// fetch8Slow refills the prefetch buffer at `lip` and returns the
// first byte. Separated from fetch8 so the fast path stays inlinable.
func (c *CPU) fetch8Slow(lip uint32) uint8 {
	c.fillFetchBuffer(lip)
	b := c.ifBuf[0]
	c.eip++
	c.maskEIP()
	return b
}

// fillFetchBuffer reads up to 32 instruction bytes starting at `lip`
// into c.ifBuf. The fill stops at the next page boundary so the
// translation is valid for every byte. Page faults during the fill
// propagate via panic, unwinding the in-flight Step exactly as a
// fetchMem8 fault would. When the source range lives in RAM (the
// dominant case), copy directly from the underlying byte slice — that
// saves the per-byte GetRange + readPhys8 round-trip that the slow
// path was previously paying 32 times per fill.
func (c *CPU) fillFetchBuffer(lip uint32) {
	pageOff := lip & 0xFFF
	n := uint32(len(c.ifBuf))
	if pageOff+n > 0x1000 {
		n = 0x1000 - pageOff
	}
	phys := c.translateAddress(lip, false, c.cpl == 3, true)
	paddr := uint64(phys & c.a20Mask)
	if pr := c.memMap.GetRange(paddr); pr != nil && pr.IsRAM {
		off := paddr - pr.Addr
		copy(c.ifBuf[:n], pr.PhysMem[off:off+uint64(n)])
	} else {
		for i := uint32(0); i < n; i++ {
			c.ifBuf[i] = c.readPhys8(phys + i)
		}
	}
	c.ifBufLip = lip
	c.ifBufPhys = phys
	c.ifBufValid = uint8(n)
}

// invalidateFetchBuffer drops the prefetch buffer so the next fetch
// refills from memory. Called from TLB flush paths (CR3 reload,
// INVLPG, mode switch) and from any place that may have changed the
// bytes the buffer is caching.
func (c *CPU) invalidateFetchBuffer() {
	c.ifBufValid = 0
}

// maybeInvalidateFetchBufferPhys clears the prefetch buffer if [addr,
// addr+size) overlaps the buffered physical range. Called from
// writePhys{8,16,32} so self-modifying code (or test fixtures that
// stage code in two passes) doesn't serve stale instruction bytes.
func (c *CPU) maybeInvalidateFetchBufferPhys(addr, size uint32) {
	if c.ifBufValid == 0 {
		return
	}
	bufEnd := c.ifBufPhys + uint32(c.ifBufValid)
	if addr < bufEnd && c.ifBufPhys < addr+size {
		c.ifBufValid = 0
	}
}

// fetch16 reads the next word from the code stream and advances EIP.
func (c *CPU) fetch16() uint16 {
	lip := c.segBase[CS] + c.eip
	off := lip - c.ifBufLip
	if off+2 <= uint32(c.ifBufValid) {
		v := uint16(c.ifBuf[off]) | uint16(c.ifBuf[off+1])<<8
		c.eip += 2
		c.maskEIP()
		return v
	}
	a := uint16(c.fetch8())
	b := uint16(c.fetch8())
	return a | b<<8
}

// fetch32 reads the next dword from the code stream and advances EIP.
func (c *CPU) fetch32() uint32 {
	lip := c.segBase[CS] + c.eip
	off := lip - c.ifBufLip
	if off+4 <= uint32(c.ifBufValid) {
		v := uint32(c.ifBuf[off]) |
			uint32(c.ifBuf[off+1])<<8 |
			uint32(c.ifBuf[off+2])<<16 |
			uint32(c.ifBuf[off+3])<<24
		c.eip += 4
		c.maskEIP()
		return v
	}
	a := uint32(c.fetch8())
	b := uint32(c.fetch8())
	cc := uint32(c.fetch8())
	d := uint32(c.fetch8())
	return a | b<<8 | cc<<16 | d<<24
}

// fetchS8 reads a signed byte from the code stream.
func (c *CPU) fetchS8() int8 {
	return int8(c.fetch8())
}

// fetchS16 reads a signed word from the code stream.
func (c *CPU) fetchS16() int16 {
	return int16(c.fetch16())
}

// fetchS32 reads a signed dword from the code stream.
func (c *CPU) fetchS32() int32 {
	return int32(c.fetch32())
}

// Step executes a single x86 instruction.
func (c *CPU) Step() (err error) {
	origEIP := c.eip
	// Sentinel: after each step, check if a watched phys addr changed.
	// Set TINYEMU_X86_PHYSSENTINEL=hex to enable.
	if physSentinelAddr != 0 {
		v, _ := c.memMap.Read32(uint64(physSentinelAddr))
		nowVal := v
		if c.physSentinelInit {
			if nowVal != c.physSentinelVal {
				fmt.Fprintf(os.Stderr,
					"[sentinel] cycles=%d prev-EIP=0x%08X next-EIP=0x%08X phys=0x%08X val %08X -> %08X EAX=%08X EBX=%08X ECX=%08X EDX=%08X ESI=%08X EDI=%08X EBP=%08X ESP=%08X\n",
					c.cycles, c.physSentinelPrevEIP, c.eip, physSentinelAddr, c.physSentinelVal, nowVal,
					c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX),
					c.GetReg32(ESI), c.GetReg32(EDI), c.GetReg32(EBP), c.GetReg32(ESP))
				c.physSentinelVal = nowVal
			}
		} else {
			c.physSentinelVal = nowVal
			c.physSentinelInit = true
		}
		c.physSentinelPrevEIP = c.eip
	}
	// EIP-breakpoint diagnostics: set TINYEMU_X86_EIPBP=hex (single addr) or
	// TINYEMU_X86_EIPBPS=hex,hex,... (multiple) to dump register state and
	// a 6-frame return-address chain when EIP lands on any of those
	// addresses. Used while reverse-engineering failures against a known
	// vmlinux.
	if eipBPActive && ((eipBreakpoint != 0 && c.eip == eipBreakpoint) || eipBreakpoints[c.eip]) {
		var frames [6]uint32
		ebp := c.GetReg32(EBP)
		for i := 0; i < 6 && ebp >= 0x1000; i++ {
			func() {
				defer func() { _ = recover() }()
				frames[i] = c.readMem32(ebp + 4)
			}()
			func() {
				defer func() { _ = recover() }()
				ebp = c.readMem32(ebp)
			}()
		}
		fmt.Fprintf(os.Stderr,
			"[bp] EIP=0x%08X cycles=%d EAX=0x%08X EBX=0x%08X ECX=0x%08X EDX=0x%08X ESI=0x%08X EDI=0x%08X ESP=0x%08X EBP=0x%08X eflags=0x%08X stack:[0x%X,0x%X,0x%X,0x%X,0x%X,0x%X]\n",
			c.eip, c.cycles,
			c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX),
			c.GetReg32(ESI), c.GetReg32(EDI), c.GetReg32(ESP), c.GetReg32(EBP),
			c.eflags, frames[0], frames[1], frames[2], frames[3], frames[4], frames[5])
	}
	defer func() {
		if r := recover(); r != nil {
			switch ex := r.(type) {
			case pageFaultError:
				c.eip = origEIP
				c.cr[2] = ex.addr
				err = c.handleInterrupt(0x0E, true, ex.errorCode)
			case stackFaultError:
				c.eip = origEIP
				err = c.handleInterrupt(0x0C, true, ex.errorCode)
			case generalProtectionFaultError:
				c.eip = origEIP
				err = c.handleInterrupt(0x0D, true, ex.errorCode)
			case invalidOpcodeError:
				_ = ex
				c.eip = origEIP
				if ud2LogAlways || ud2SkipAndLog {
					ud2Log(c, origEIP)
				}
				if ud2SkipAndLog {
					// Skip UD2 (2 bytes) and continue. This is purely
					// diagnostic: it lets the kernel run past WARN_ON
					// sites so we can capture later symptoms.
					c.eip += 2
					err = nil
				} else {
					err = c.handleInterrupt(0x06, false)
				}
			case divideError:
				_ = ex
				c.eip = origEIP
				err = c.handleInterrupt(0x00, false)
			default:
				panic(r)
			}
		}
	}()

	// Check for pending hardware interrupts before fetching the next instruction.
	if c.intrLineState != 0 && (c.eflags&EFLAGS_IF) != 0 && !c.interruptsBlocked {
		if c.ackInterruptFunc != nil {
			if vec, ok := c.ackInterruptFunc(); ok {
				return c.handleInterrupt(vec, true)
			}
		}
	}
	c.interruptsBlocked = false

	// Scan prefixes
	var repPrefix uint8 // 0 = none, 1 = REP/REPE/REPZ, 2 = REPNE/REPNZ
	var segOverride int = -1
	operandSizeToggle := false
	addressSizeToggle := false
	for {
		opcode := c.fetch8()
		switch opcode {
		case 0xF0:
			// LOCK prefix - ignored for now (we don't implement SMP)
		case 0xF3:
			repPrefix = 1
		case 0xF2:
			repPrefix = 2
		case 0x66:
			operandSizeToggle = !operandSizeToggle
		case 0x67:
			addressSizeToggle = !addressSizeToggle
		case 0x2E:
			segOverride = CS
		case 0x36:
			segOverride = SS
		case 0x3E:
			segOverride = DS
		case 0x26:
			segOverride = ES
		case 0x64:
			segOverride = FS
		case 0x65:
			segOverride = GS
		default:
			operandSize := c.DefaultOperandSize()
			if operandSizeToggle {
				if operandSize == 2 {
					operandSize = 4
				} else {
					operandSize = 2
				}
			}
			addressSize := c.DefaultAddressSize()
			if addressSizeToggle {
				if addressSize == 2 {
					addressSize = 4
				} else {
					addressSize = 2
				}
			}
			err := c.executeOpcode(opcode, repPrefix, segOverride, operandSize, addressSize)
			c.maskEIP()
			return err
		}
	}
}

// executeOpcode executes a single opcode (after prefixes have been consumed).
// operandSize is 2 or 4 (16-bit or 32-bit).
// addressSize is 2 or 4 (16-bit or 32-bit addressing).
func (c *CPU) executeOpcode(opcode uint8, repPrefix uint8, segOverride int, operandSize, addressSize uint8) error {
	c.currentOpSize = operandSize
	c.currentAddrSize = addressSize
	c.currentSegOverride = segOverride
	switch opcode {
	// NOP
	case 0x90:
		// Do nothing

	// WAIT/FWAIT — no FPU; treat as NOP.
	case 0x9B:
		// Do nothing.

	// x87 family (D8-DF). We model the FPU as 8 × float64 (drops the
	// extra 11 bits of 80-bit extended precision) and implement the
	// common arithmetic + load/store/compare/transcendental ops that
	// musl libc and the Linux kernel use. See x87.go for the helper
	// functions and register-file mechanics.
	case 0xD8, 0xD9, 0xDA, 0xDB, 0xDC, 0xDD, 0xDE, 0xDF:
		if !c.fpuInitialized {
			c.fpuReset()
		}
		return c.handleX87(opcode)

	// PUSHA/PUSHAD
	case 0x60:
		if operandSize == 2 {
			tmpSP := c.GetReg16(SP)
			c.push16(c.GetReg16(AX))
			c.push16(c.GetReg16(CX))
			c.push16(c.GetReg16(DX))
			c.push16(c.GetReg16(BX))
			c.push16(tmpSP)
			c.push16(c.GetReg16(BP))
			c.push16(c.GetReg16(SI))
			c.push16(c.GetReg16(DI))
		} else {
			tmpESP := c.GetReg32(ESP)
			c.push32(c.GetReg32(EAX))
			c.push32(c.GetReg32(ECX))
			c.push32(c.GetReg32(EDX))
			c.push32(c.GetReg32(EBX))
			c.push32(tmpESP)
			c.push32(c.GetReg32(EBP))
			c.push32(c.GetReg32(ESI))
			c.push32(c.GetReg32(EDI))
		}

	// POPA/POPAD
	case 0x61:
		if operandSize == 2 {
			c.SetReg16(DI, c.pop16())
			c.SetReg16(SI, c.pop16())
			c.SetReg16(BP, c.pop16())
			_ = c.pop16() // discard SP
			c.SetReg16(BX, c.pop16())
			c.SetReg16(DX, c.pop16())
			c.SetReg16(CX, c.pop16())
			c.SetReg16(AX, c.pop16())
		} else {
			c.SetReg32(EDI, c.pop32())
			c.SetReg32(ESI, c.pop32())
			c.SetReg32(EBP, c.pop32())
			_ = c.pop32() // discard ESP
			c.SetReg32(EBX, c.pop32())
			c.SetReg32(EDX, c.pop32())
			c.SetReg32(ECX, c.pop32())
			c.SetReg32(EAX, c.pop32())
		}

	// HLT
	case 0xF4:
		c.powerDown = true

	// CMC - Complement Carry Flag
	case 0xF5:
		c.eflags ^= EFLAGS_CF

	// CLC - Clear Carry Flag
	case 0xF8:
		c.eflags &^= EFLAGS_CF

	// STC - Set Carry Flag
	case 0xF9:
		c.eflags |= EFLAGS_CF

	// CLI - Clear Interrupt Flag
	case 0xFA:
		c.checkIOPL()
		c.eflags &^= EFLAGS_IF

	// STI - Set Interrupt Flag
	case 0xFB:
		c.checkIOPL()
		c.eflags |= EFLAGS_IF
		c.interruptsBlocked = true

	// CLD - Clear Direction Flag
	case 0xFC:
		c.eflags &^= EFLAGS_DF

	// STD - Set Direction Flag
	case 0xFD:
		c.eflags |= EFLAGS_DF

	// PUSH r16/r32
	case 0x50:
		c.pushOp(c.GetReg32(EAX), operandSize)
	case 0x51:
		c.pushOp(c.GetReg32(ECX), operandSize)
	case 0x52:
		c.pushOp(c.GetReg32(EDX), operandSize)
	case 0x53:
		c.pushOp(c.GetReg32(EBX), operandSize)
	case 0x54:
		c.pushOp(c.GetReg32(ESP), operandSize)
	case 0x55:
		c.pushOp(c.GetReg32(EBP), operandSize)
	case 0x56:
		c.pushOp(c.GetReg32(ESI), operandSize)
	case 0x57:
		c.pushOp(c.GetReg32(EDI), operandSize)

	// PUSH imm16/imm32
	case 0x68:
		if operandSize == 2 {
			c.pushOp(uint32(c.fetch16()), operandSize)
		} else {
			c.pushOp(c.fetch32(), operandSize)
		}

	// PUSH imm8
	case 0x6A:
		c.pushOp(uint32(int32(int8(c.fetch8()))), operandSize)

	// IMUL r16/32, r/m16/32, imm16/32
	case 0x69:
		mr := c.parseModRM()
		var src int64
		if mr.isReg {
			if operandSize == 2 {
				src = int64(int16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
			} else {
				src = int64(int32(c.GetReg32(int(mr.rm))))
			}
		} else {
			if operandSize == 2 {
				src = int64(int16(c.readMem16(c.segBaseForModRM(mr) + mr.ea)))
			} else {
				src = int64(int32(c.readMem32(c.segBaseForModRM(mr) + mr.ea)))
			}
		}
		var imm int64
		if operandSize == 2 {
			imm = int64(int16(c.fetch16()))
		} else {
			imm = int64(int32(c.fetch32()))
		}
		r := src * imm
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(r))
			c.setOF(r != int64(int16(r)))
			c.setCF(r != int64(int16(r)))
		} else {
			c.SetReg32(int(mr.reg), uint32(r))
			c.setOF(r != int64(int32(r)))
			c.setCF(r != int64(int32(r)))
		}

	// INS B (IN SB)
	case 0x6C:
		c.executeString(repPrefix, 1, func() {
			port := c.GetReg16(DX)
			c.writeMem8(c.segBase[ES]+c.GetReg32(EDI), c.ioRead8(port))
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(EDI, c.GetReg32(EDI)-1)
			} else {
				c.SetReg32(EDI, c.GetReg32(EDI)+1)
			}
		}, false, false)

	// INS W/D (INSW/INSD)
	case 0x6D:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() {
			port := c.GetReg16(DX)
			addr := c.segBase[ES] + c.GetReg32(EDI)
			if size == 2 {
				c.writeMem16(addr, c.ioRead16(port))
			} else {
				c.writeMem32(addr, c.ioRead32(port))
			}
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(EDI, c.GetReg32(EDI)-size)
			} else {
				c.SetReg32(EDI, c.GetReg32(EDI)+size)
			}
		}, false, false)

	// OUTS B (OUTSB)
	case 0x6E:
		c.executeString(repPrefix, 1, func() {
			port := c.GetReg16(DX)
			seg := DS
			if segOverride != -1 {
				seg = segOverride
			}
			c.ioWrite8(port, c.readMem8(c.segBase[seg]+c.GetReg32(ESI)))
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(ESI, c.GetReg32(ESI)-1)
			} else {
				c.SetReg32(ESI, c.GetReg32(ESI)+1)
			}
		}, false, false)

	// OUTS W/D (OUTSW/OUTSD)
	case 0x6F:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() {
			port := c.GetReg16(DX)
			seg := DS
			if segOverride != -1 {
				seg = segOverride
			}
			addr := c.segBase[seg] + c.GetReg32(ESI)
			if size == 2 {
				c.ioWrite16(port, c.readMem16(addr))
			} else {
				c.ioWrite32(port, c.readMem32(addr))
			}
			if c.eflags&EFLAGS_DF != 0 {
				c.SetReg32(ESI, c.GetReg32(ESI)-size)
			} else {
				c.SetReg32(ESI, c.GetReg32(ESI)+size)
			}
		}, false, false)

	// IMUL r16/32, r/m16/32, imm8
	case 0x6B:
		mr := c.parseModRM()
		var src int64
		if mr.isReg {
			if operandSize == 2 {
				src = int64(int16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
			} else {
				src = int64(int32(c.GetReg32(int(mr.rm))))
			}
		} else {
			if operandSize == 2 {
				src = int64(int16(c.readMem16(c.segBaseForModRM(mr) + mr.ea)))
			} else {
				src = int64(int32(c.readMem32(c.segBaseForModRM(mr) + mr.ea)))
			}
		}
		imm := int64(int32(int8(c.fetch8())))
		r := src * imm
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(r))
			c.setOF(r != int64(int16(r)))
			c.setCF(r != int64(int16(r)))
		} else {
			c.SetReg32(int(mr.reg), uint32(r))
			c.setOF(r != int64(int32(r)))
			c.setCF(r != int64(int32(r)))
		}

	// POP r16/r32
	case 0x58:
		c.SetReg32(EAX, c.popOp(operandSize))
	case 0x59:
		c.SetReg32(ECX, c.popOp(operandSize))
	case 0x5A:
		c.SetReg32(EDX, c.popOp(operandSize))
	case 0x5B:
		c.SetReg32(EBX, c.popOp(operandSize))
	case 0x5C:
		c.SetReg32(ESP, c.popOp(operandSize))
	case 0x5D:
		c.SetReg32(EBP, c.popOp(operandSize))
	case 0x5E:
		c.SetReg32(ESI, c.popOp(operandSize))
	case 0x5F:
		c.SetReg32(EDI, c.popOp(operandSize))

	// MOV r8, imm8
	case 0xB0:
		c.SetReg8(AL, c.fetch8())
	case 0xB1:
		c.SetReg8(CL, c.fetch8())
	case 0xB2:
		c.SetReg8(DL, c.fetch8())
	case 0xB3:
		c.SetReg8(BL, c.fetch8())
	case 0xB4:
		c.SetReg8(AH, c.fetch8())
	case 0xB5:
		c.SetReg8(CH, c.fetch8())
	case 0xB6:
		c.SetReg8(DH, c.fetch8())
	case 0xB7:
		c.SetReg8(BH, c.fetch8())

	// MOV r16/r32, imm16/imm32
	case 0xB8:
		if operandSize == 2 {
			c.SetReg16(AX, c.fetch16())
		} else {
			c.SetReg32(EAX, c.fetch32())
		}
	case 0xB9:
		if operandSize == 2 {
			c.SetReg16(CX, c.fetch16())
		} else {
			c.SetReg32(ECX, c.fetch32())
		}
	case 0xBA:
		if operandSize == 2 {
			c.SetReg16(DX, c.fetch16())
		} else {
			c.SetReg32(EDX, c.fetch32())
		}
	case 0xBB:
		if operandSize == 2 {
			c.SetReg16(BX, c.fetch16())
		} else {
			c.SetReg32(EBX, c.fetch32())
		}
	case 0xBC:
		if operandSize == 2 {
			c.SetReg16(SP, c.fetch16())
		} else {
			c.SetReg32(ESP, c.fetch32())
		}
	case 0xBD:
		if operandSize == 2 {
			c.SetReg16(BP, c.fetch16())
		} else {
			c.SetReg32(EBP, c.fetch32())
		}
	case 0xBE:
		if operandSize == 2 {
			c.SetReg16(SI, c.fetch16())
		} else {
			c.SetReg32(ESI, c.fetch32())
		}
	case 0xBF:
		if operandSize == 2 {
			c.SetReg16(DI, c.fetch16())
		} else {
			c.SetReg32(EDI, c.fetch32())
		}

	// XCHG r/m8, r8
	case 0x86:
		mr := c.parseModRM()
		if mr.isReg {
			tmp := c.GetReg8(reg8FromModRM(int(mr.rm)))
			c.SetReg8(reg8FromModRM(int(mr.rm)), c.GetReg8(reg8FromModRM(int(mr.reg))))
			c.SetReg8(reg8FromModRM(int(mr.reg)), tmp)
		} else {
			addr := c.segBaseForModRM(mr) + mr.ea
			tmp := c.readMem8(addr)
			c.writeMem8(addr, c.GetReg8(reg8FromModRM(int(mr.reg))))
			c.SetReg8(reg8FromModRM(int(mr.reg)), tmp)
		}

	// XCHG r/m16/32, r16/32
	case 0x87:
		mr := c.parseModRM()
		if operandSize == 2 {
			if mr.isReg {
				tmp := c.GetReg16(reg16FromModRM(int(mr.rm)))
				c.SetReg16(reg16FromModRM(int(mr.rm)), c.GetReg16(reg16FromModRM(int(mr.reg))))
				c.SetReg16(reg16FromModRM(int(mr.reg)), tmp)
			} else {
				addr := c.segBaseForModRM(mr) + mr.ea
				tmp := c.readMem16(addr)
				c.writeMem16(addr, c.GetReg16(reg16FromModRM(int(mr.reg))))
				c.SetReg16(reg16FromModRM(int(mr.reg)), tmp)
			}
		} else {
			if mr.isReg {
				tmp := c.GetReg32(int(mr.rm))
				c.SetReg32(int(mr.rm), c.GetReg32(int(mr.reg)))
				c.SetReg32(int(mr.reg), tmp)
			} else {
				addr := c.segBaseForModRM(mr) + mr.ea
				tmp := c.readMem32(addr)
				c.writeMem32(addr, c.GetReg32(int(mr.reg)))
				c.SetReg32(int(mr.reg), tmp)
			}
		}

	// MOV r/m8, r8 and MOV r8, r/m8
	case 0x88, 0x8A:
		return c.handleModRM8(opcode)

	// MOV r/m16/32, r16/32 and MOV r16/32, r/m16/32
	case 0x89, 0x8B:
		if operandSize == 2 {
			return c.handleModRM16(opcode)
		}
		return c.handleModRM32(opcode)

	// MOV AL, moffs8
	case 0xA0:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		c.SetReg8(AL, c.readMem8(c.segBase[seg]+off))

	// MOV AX/EAX, moffs16/moffs32
	case 0xA1:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		if operandSize == 2 {
			c.SetReg16(AX, c.readMem16(c.segBase[seg]+off))
		} else {
			c.SetReg32(EAX, c.readMem32(c.segBase[seg]+off))
		}

	// MOV moffs8, AL
	case 0xA2:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		c.writeMem8(c.segBase[seg]+off, c.GetReg8(AL))

	// MOV moffs16/moffs32, AX/EAX
	case 0xA3:
		off := c.fetchMoffs()
		seg := DS
		if segOverride != -1 {
			seg = segOverride
		}
		if operandSize == 2 {
			c.writeMem16(c.segBase[seg]+off, c.GetReg16(AX))
		} else {
			c.writeMem32(c.segBase[seg]+off, c.GetReg32(EAX))
		}

	// MOV r/m16/32, imm16/imm32 (C7 /0)
	case 0xC7:
		return c.handleMovImm(operandSize)

	// PUSH ES
	case 0x06:
		c.push32(uint32(c.seg[ES]))

	// POP ES
	case 0x07:
		return c.popSegReg(ES, operandSize, false)

	// PUSH CS
	case 0x0E:
		c.push32(uint32(c.seg[CS]))

	// PUSH SS
	case 0x16:
		c.push32(uint32(c.seg[SS]))

	// POP SS — reload SS and (in protected mode) refresh its descriptor
	// cache. Direct seg[SS] = pop() without LoadSegmentProtected leaves the
	// old base/limit/access in place; subsequent stack accesses would use
	// the WRONG segment base.
	case 0x17:
		return c.popSegReg(SS, operandSize, true)

	// PUSH DS
	case 0x1E:
		c.push32(uint32(c.seg[DS]))

	// POP DS
	case 0x1F:
		return c.popSegReg(DS, operandSize, false)

	// ADD AL, imm8
	case 0x04:
		c.SetReg8(AL, c.add8(c.GetReg8(AL), c.fetch8()))

	// ADD EAX/AX, imm32/imm16
	case 0x05:
		if operandSize == 2 {
			c.SetReg16(AX, c.add16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.add32(c.GetReg32(EAX), c.fetch32()))
		}

	// ADD r/m8, imm8 (80 /0)
	case 0x80:
		return c.handleGroup1_8()

	// ADD r/m16/32, imm16/32 (81 /0)
	case 0x81:
		if operandSize == 2 {
			return c.handleGroup1_16()
		}
		return c.handleGroup1_32()

	// ADD r/m16/32, imm8 (83 /0, sign-extended)
	case 0x83:
		return c.handleGroup1_8x(operandSize)

	// INC r16/r32 (0x40-0x47). Operand size honored: under a 0x66 prefix or
	// 16-bit code segment, INC AX uses inc16 semantics (low 16 bits modified,
	// high 16 preserved).
	case 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47:
		reg := int(opcode - 0x40)
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(reg), c.inc16(c.GetReg16(reg16FromModRM(reg))))
		} else {
			c.SetReg32(reg, c.inc32(c.GetReg32(reg)))
		}

	// DEC r16/r32 (0x48-0x4F). Linux's __delay() is `dec eax; jnz -3` looping
	// until EAX=0; our cycle-per-instruction emulation is far too slow to
	// grind through the millions/billions of iterations the kernel passes
	// in. When we detect the pattern, short-circuit to the loop's terminal
	// state. (Fastpath only used for the 32-bit EAX/ECX paths since that's
	// what Linux's __delay emits.)
	case 0x48:
		if operandSize == 4 {
			eax := c.GetReg32(EAX)
			if eax > 1 && c.detectDecJnzLoop() {
				c.cycles += uint64(eax-1) * 2
				c.SetReg32(EAX, c.dec32(1))
				return nil
			}
			c.SetReg32(EAX, c.dec32(eax))
		} else {
			c.SetReg16(AX, c.dec16(c.GetReg16(AX)))
		}
	case 0x49:
		if operandSize == 4 {
			ecx := c.GetReg32(ECX)
			if ecx > 1 && c.detectDecJnzLoop() {
				c.cycles += uint64(ecx-1) * 2
				c.SetReg32(ECX, c.dec32(1))
				return nil
			}
			c.SetReg32(ECX, c.dec32(ecx))
		} else {
			c.SetReg16(CX, c.dec16(c.GetReg16(CX)))
		}
	case 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F:
		reg := int(opcode - 0x48)
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(reg), c.dec16(c.GetReg16(reg16FromModRM(reg))))
		} else {
			c.SetReg32(reg, c.dec32(c.GetReg32(reg)))
		}

	// JMP rel8
	case 0xEB:
		off := c.fetchS8()
		c.eip = uint32(int32(c.eip) + int32(off))

	// JMP rel16/rel32
	case 0xE9:
		if operandSize == 2 {
			off := c.fetchS16()
			c.eip = uint32(int32(c.eip) + int32(off))
		} else {
			off := c.fetchS32()
			c.eip = uint32(int32(c.eip) + int32(off))
		}

	// Jcc rel8
	case 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
		0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F:
		return c.handleJcc8(opcode)

	// CALL rel16/rel32
	case 0xE8:
		if operandSize == 2 {
			off := c.fetchS16()
			c.push16(uint16(c.eip))
			c.eip = uint32(int32(c.eip) + int32(off))
		} else {
			off := c.fetchS32()
			c.push32(c.eip)
			c.eip = uint32(int32(c.eip) + int32(off))
		}

	// RET near
	case 0xC3:
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
		} else {
			c.eip = c.pop32()
		}

	// RET imm16
	case 0xC2:
		imm := c.fetch16()
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
			c.SetReg16(SP, c.GetReg16(SP)+imm)
		} else {
			c.eip = c.pop32()
			c.SetReg32(ESP, c.GetReg32(ESP)+uint32(imm))
		}

	// INT 3
	case 0xCC:
		return c.handleInterrupt(0x03, false)

	// INT imm8
	case 0xCD:
		vec := c.fetch8()
		if syscallTrace && vec == 0x80 && c.cpl == 3 {
			fmt.Fprintf(os.Stderr, "[sys] eax=%d ebx=%08X ecx=%08X edx=%08X esi=%08X edi=%08X EIP=%08X cycles=%d\n",
				c.GetReg32(EAX), c.GetReg32(EBX), c.GetReg32(ECX), c.GetReg32(EDX),
				c.GetReg32(ESI), c.GetReg32(EDI), c.eip, c.cycles)
		}
		return c.handleInterrupt(vec, false)

	// INTO
	case 0xCE:
		if c.getOF() {
			return c.handleInterrupt(0x04, false)
		}

		// IRET. The privilege transition is conceptually atomic: ALL pops
		// from the outgoing stack happen at the old CPL, and only once the
		// frame is fully unloaded do CS/SS get installed and CPL updated.
		// If we updated CPL after popping CS but before popping EFLAGS/ESP/SS,
		// the remaining pops would inherit the new (user) CPL and #PF on the
		// supervisor-only kernel stack — that exact sequence was killing the
		// Yocto init process during return-to-userspace.
		case 0xCF:
			if operandSize == 2 {
				oldCPL := c.cpl
				newIP := c.pop16()
				newCS := c.pop16()
				newFlags := c.pop16()
				// Decide whether a privilege change occurs based on the new
				// CS's RPL — still using the old CPL for pops.
				crossPriv := c.IsProtectedMode() && int(newCS&3) > oldCPL
				var newSP uint16
				var newSS uint16
				if crossPriv {
					newSP = c.pop16()
					newSS = c.pop16()
				}
				c.eip = uint32(newIP)
				if c.IsProtectedMode() {
					if err := c.LoadSegmentProtected(CS, newCS); err != nil {
						return err
					}
				} else {
					c.seg[CS] = newCS
					c.segBase[CS] = uint32(newCS) << 4
				}
				// EFLAGS restore: use the OLD cpl (before CS load). Intel
				// SDM §6.8.1: IRET's IOPL/IF mask is gated by the CPL at
				// the time of IRET — not the new CPL the IRET transitions
				// to. Using c.cpl here would let a kernel→user IRET leave
				// IF=0 in user mode (kernel's IF=0 during interrupt entry
				// stays clobbered into user EFLAGS), masking all external
				// IRQs in user mode and stalling preemption.
				c.setEFlagsFromPop(uint32(newFlags), oldCPL)
				c.eflags |= EFLAGS_RF
				if crossPriv {
					if err := c.LoadSegmentProtected(SS, newSS); err != nil {
						return err
					}
					c.SetReg16(SP, newSP)
				}
			} else {
				oldCPL := c.cpl
				newEIP := c.pop32()
				newCS := uint16(c.pop32())
				newFlags := c.pop32()
				crossPriv := c.IsProtectedMode() && int(newCS&3) > oldCPL
				var newESP uint32
				var newSS uint16
				if crossPriv {
					newESP = c.pop32()
					newSS = uint16(c.pop32())
				}
				c.eip = newEIP
				if c.IsProtectedMode() {
					if err := c.LoadSegmentProtected(CS, newCS); err != nil {
						return err
					}
				} else {
					c.seg[CS] = newCS
					c.segBase[CS] = uint32(newCS) << 4
				}
				// EFLAGS restore: use OLD cpl (see 16-bit branch above
				// for the long explanation). Bug fixed 2026-05-14: was
				// using c.cpl which is the new (user) CPL after CS load,
				// causing IF to be masked out on kernel→user IRET and
				// userspace to resume with interrupts disabled.
				c.setEFlagsFromPop(newFlags, oldCPL)
				c.eflags |= EFLAGS_RF
				// Restore VM bit if CPL=0 and popped EFLAGS has it.
				if c.IsProtectedMode() && c.cpl == 0 && (c.eflags&EFLAGS_VM) != 0 {
					// TODO: pop ES, DS, FS, GS for full v8086 mode entry
				}
				if crossPriv {
					if err := c.LoadSegmentProtected(SS, newSS); err != nil {
						return err
					}
					c.SetReg32(ESP, newESP)
				}
			}

	// PUSHF
	case 0x9C:
		c.pushOp(c.eflags, operandSize)

	// POPF
	case 0x9D:
		c.setEFlagsFromPop(c.popOp(operandSize), c.cpl)

	// CBW/CWDE
	case 0x98:
		if operandSize == 2 {
			if int8(c.GetReg8(AL)) < 0 {
				c.SetReg8(AH, 0xFF)
			} else {
				c.SetReg8(AH, 0)
			}
		} else {
			ax := int16(c.GetReg16(AX))
			if ax < 0 {
				c.SetReg32(EAX, 0xFFFF0000|uint32(ax))
			} else {
				c.SetReg32(EAX, uint32(ax))
			}
		}

	// CWD/CDQ
	case 0x99:
		if operandSize == 2 {
			if c.GetReg16(AX)&0x8000 != 0 {
				c.SetReg16(DX, 0xFFFF)
			} else {
				c.SetReg16(DX, 0)
			}
		} else {
			if c.GetReg32(EAX)&0x80000000 != 0 {
				c.SetReg32(EDX, 0xFFFFFFFF)
			} else {
				c.SetReg32(EDX, 0)
			}
		}

	// SAHF
	case 0x9E:
		c.eflags = (c.eflags & ^uint32(EFLAGS_SF|EFLAGS_ZF|EFLAGS_AF|EFLAGS_PF|EFLAGS_CF)) | uint32(c.GetReg8(AH)&(0x80|0x40|0x10|0x04|0x01))

	// LAHF
	case 0x9F:
		c.SetReg8(AH, uint8(c.eflags)&(0x80|0x40|0x10|0x04|0x01)|0x02)

	// SUB AL, imm8
	case 0x2C:
		c.SetReg8(AL, c.sub8(c.GetReg8(AL), c.fetch8()))

	// SUB EAX/AX, imm32/imm16
	case 0x2D:
		if operandSize == 2 {
			c.SetReg16(AX, c.sub16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.sub32(c.GetReg32(EAX), c.fetch32()))
		}

	// XOR AL, imm8
	case 0x34:
		c.SetReg8(AL, c.xor8(c.GetReg8(AL), c.fetch8()))

	// XOR EAX/AX, imm32/imm16
	case 0x35:
		if operandSize == 2 {
			c.SetReg16(AX, c.xor16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.xor32(c.GetReg32(EAX), c.fetch32()))
		}

	// CMP AL, imm8
	case 0x3C:
		c.sub8(c.GetReg8(AL), c.fetch8())

	// CMP EAX/AX, imm32/imm16
	case 0x3D:
		if operandSize == 2 {
			c.sub16(c.GetReg16(AX), c.fetch16())
		} else {
			c.sub32(c.GetReg32(EAX), c.fetch32())
		}

	// TEST AL, imm8
	case 0xA8:
		c.and8(c.GetReg8(AL), c.fetch8())

	// TEST EAX/AX, imm32/imm16
	case 0xA9:
		if operandSize == 2 {
			c.and16(c.GetReg16(AX), c.fetch16())
		} else {
			c.and32(c.GetReg32(EAX), c.fetch32())
		}

	// IN AL, imm8
	case 0xE4:
		c.checkIOPL()
		port := uint16(c.fetch8())
		c.SetReg8(AL, c.ioRead8(port))

	// IN AX/EAX, imm8
	case 0xE5:
		c.checkIOPL()
		port := uint16(c.fetch8())
		if operandSize == 2 {
			c.SetReg16(AX, c.ioRead16(port))
		} else {
			c.SetReg32(EAX, c.ioRead32(port))
		}

	// OUT imm8, AL
	case 0xE6:
		c.checkIOPL()
		port := uint16(c.fetch8())
		c.ioWrite8(port, c.GetReg8(AL))

	// OUT imm8, AX/EAX
	case 0xE7:
		c.checkIOPL()
		port := uint16(c.fetch8())
		if operandSize == 2 {
			c.ioWrite16(port, c.GetReg16(AX))
		} else {
			c.ioWrite32(port, c.GetReg32(EAX))
		}

	// IN AL, DX
	case 0xEC:
		c.checkIOPL()
		c.SetReg8(AL, c.ioRead8(c.GetReg16(DX)))

	// IN AX/EAX, DX
	case 0xED:
		c.checkIOPL()
		if operandSize == 2 {
			c.SetReg16(AX, c.ioRead16(c.GetReg16(DX)))
		} else {
			c.SetReg32(EAX, c.ioRead32(c.GetReg16(DX)))
		}

	// OUT DX, AL
	case 0xEE:
		c.checkIOPL()
		c.ioWrite8(c.GetReg16(DX), c.GetReg8(AL))

	// OUT DX, AX/EAX
	case 0xEF:
		c.checkIOPL()
		if operandSize == 2 {
			c.ioWrite16(c.GetReg16(DX), c.GetReg16(AX))
		} else {
			c.ioWrite32(c.GetReg16(DX), c.GetReg32(EAX))
		}

	// ALU r/m8, r8 and r8, r/m8
	case 0x00, 0x02, 0x08, 0x0A, 0x10, 0x12, 0x18, 0x1A,
		0x20, 0x22, 0x28, 0x2A, 0x30, 0x32, 0x38, 0x3A:
		return c.handleALU_ModRM8(opcode)
	// ALU r/m16/32, r16/32 and r16/32, r/m16/32
	case 0x01, 0x03, 0x09, 0x0B, 0x11, 0x13, 0x19, 0x1B,
		0x21, 0x23, 0x29, 0x2B, 0x31, 0x33, 0x39, 0x3B:
		if operandSize == 2 {
			return c.handleALU_ModRM16(opcode)
		}
		return c.handleALU_ModRM32(opcode)

	// TEST r/m8, r8
	case 0x84:
		return c.handleTEST_ModRM(1)
	// TEST r/m16/32, r16/32
	case 0x85:
		return c.handleTEST_ModRM(int(operandSize))

	// XCHG AX, r16 / XCHG EAX, r32
	case 0x91:
		if operandSize == 2 {
			c.xchg16(EAX, ECX)
		} else {
			c.xchg32(EAX, ECX)
		}
	case 0x92:
		if operandSize == 2 {
			c.xchg16(EAX, EDX)
		} else {
			c.xchg32(EAX, EDX)
		}
	case 0x93:
		if operandSize == 2 {
			c.xchg16(EAX, EBX)
		} else {
			c.xchg32(EAX, EBX)
		}
	case 0x94:
		if operandSize == 2 {
			c.xchg16(EAX, ESP)
		} else {
			c.xchg32(EAX, ESP)
		}
	case 0x95:
		if operandSize == 2 {
			c.xchg16(EAX, EBP)
		} else {
			c.xchg32(EAX, EBP)
		}
	case 0x96:
		if operandSize == 2 {
			c.xchg16(EAX, ESI)
		} else {
			c.xchg32(EAX, ESI)
		}
	case 0x97:
		if operandSize == 2 {
			c.xchg16(EAX, EDI)
		} else {
			c.xchg32(EAX, EDI)
		}

	// POP r/m16/32 (8F /0)
	case 0x8F:
		mr := c.parseModRM()
		if mr.reg != 0 {
			return fmt.Errorf("8F /%d not implemented", mr.reg)
		}
		if operandSize == 2 {
			if mr.isReg {
				c.SetReg16(reg16FromModRM(int(mr.rm)), c.pop16())
			} else {
				c.writeMem16(c.segBaseForModRM(mr) + mr.ea, c.pop16())
			}
		} else {
			if mr.isReg {
				c.SetReg32(int(mr.rm), c.pop32())
			} else {
				c.writeMem32(c.segBaseForModRM(mr) + mr.ea, c.pop32())
			}
		}

	// MOV r/m8, imm8 (C6 /0)
	case 0xC6:
		mr := c.parseModRM()
		imm := c.fetch8()
		if mr.reg != 0 {
			return fmt.Errorf("C6 /%d not implemented", mr.reg)
		}
		if mr.isReg {
			c.SetReg8(reg8FromModRM(int(mr.rm)), imm)
		} else {
			c.writeMem8(c.segBaseForModRM(mr) + mr.ea, imm)
		}

	// LEA r16/32, m
	case 0x8D:
		mr := c.parseModRM()
		if operandSize == 2 {
			c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(mr.ea))
		} else {
			c.SetReg32(int(mr.reg), mr.ea)
		}

	// LOOP rel8
	case 0xE2:
		off := c.fetchS8()
		if addressSize == 2 {
			c.SetReg16(CX, c.GetReg16(CX)-1)
			if c.GetReg16(CX) != 0 {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		} else {
			c.SetReg32(ECX, c.GetReg32(ECX)-1)
			if c.GetReg32(ECX) != 0 {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		}

	// LOOPE/LOOPZ rel8
	case 0xE1:
		off := c.fetchS8()
		if addressSize == 2 {
			cx := c.GetReg16(CX)
			c.SetReg16(CX, cx-1)
			newCx := c.GetReg16(CX)
			zf := c.getZF()
			if eip := c.GetEIP(); eip >= 0x3F0 && eip <= 0x400 {
				// LOOPZ16 behavior documented
			}
			if newCx != 0 && zf {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		} else {
			ecx := c.GetReg32(ECX)
			c.SetReg32(ECX, ecx-1)
			newEcx := c.GetReg32(ECX)
			zf := c.getZF()
			if eip := c.GetEIP(); eip >= 0x3C0 && eip <= 0x3D0 {
				// LOOPZ32 behavior documented
			}
			if newEcx != 0 && zf {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		}

	// LOOPNE/LOOPNZ rel8
	case 0xE0:
		off := c.fetchS8()
		if addressSize == 2 {
			c.SetReg16(CX, c.GetReg16(CX)-1)
			if c.GetReg16(CX) != 0 && !c.getZF() {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		} else {
			c.SetReg32(ECX, c.GetReg32(ECX)-1)
			if c.GetReg32(ECX) != 0 && !c.getZF() {
				c.eip = uint32(int32(c.eip) + int32(off))
			}
		}

	// JCXZ/JECXZ rel8
	case 0xE3:
		off := c.fetchS8()
		if c.GetReg32(ECX) == 0 {
			c.eip = uint32(int32(c.eip) + int32(off))
		}

	// MOVS byte
	case 0xA4:
		c.executeString(repPrefix, 1, func() { c.movs(1, segOverride) }, false, false)

	// MOVS word/dword
	case 0xA5:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.movs(size, segOverride) }, false, false)

	// CMPS byte
	case 0xA6:
		c.executeString(repPrefix, 1, func() { c.cmps(1, segOverride) }, repPrefix != 0, repPrefix == 1)

	// CMPS word/dword
	case 0xA7:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.cmps(size, segOverride) }, repPrefix != 0, repPrefix == 1)

	// STOS byte
	case 0xAA:
		c.executeString(repPrefix, 1, func() { c.stos(1, segOverride) }, false, false)

	// STOS word/dword
	case 0xAB:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.stos(size, segOverride) }, false, false)

	// LODS byte
	case 0xAC:
		c.executeString(repPrefix, 1, func() { c.lods(1, segOverride) }, false, false)

	// LODS word/dword
	case 0xAD:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.lods(size, segOverride) }, false, false)

	// SCAS byte
	case 0xAE:
		c.executeString(repPrefix, 1, func() { c.scas(1, segOverride) }, repPrefix != 0, repPrefix == 1)

	// SCAS word/dword
	case 0xAF:
		size := uint32(4)
		if operandSize == 2 {
			size = 2
		}
		c.executeString(repPrefix, size, func() { c.scas(size, segOverride) }, repPrefix != 0, repPrefix == 1)

	// Group 2 shifts (C0 = imm8, C1 = imm8)
	case 0xC0:
		return c.handleGroup2_8()
	case 0xC1:
		if operandSize == 2 {
			return c.handleGroup2_16()
		}
		return c.handleGroup2_32()

	// Group 2 shifts (D0 = 1, D1 = 1, D2 = CL, D3 = CL)
	case 0xD0:
		return c.handleGroup2_8Count(1)
	case 0xD1:
		if operandSize == 2 {
			return c.handleGroup2_16Count(1)
		}
		return c.handleGroup2_32Count(1)
	case 0xD2:
		return c.handleGroup2_8Count(c.GetReg8(CL))
	case 0xD3:
		if operandSize == 2 {
			return c.handleGroup2_16Count(c.GetReg8(CL))
		}
		return c.handleGroup2_32Count(c.GetReg8(CL))

	// XLAT
	case 0xD7:
		if addressSize == 2 {
			c.SetReg8(AL, c.readMem8(c.segBase[DS]+uint32(c.GetReg16(BX)+uint16(c.GetReg8(AL)))))
		} else {
			c.SetReg8(AL, c.readMem8(c.segBase[DS]+c.GetReg32(EBX)+uint32(c.GetReg8(AL))))
		}

	// Group 3 (F6/F7)
	case 0xF6:
		return c.handleGroup3_8()
	case 0xF7:
		if operandSize == 2 {
			return c.handleGroup3_16()
		}
		return c.handleGroup3_32()

	// Group 4 (FE)
	case 0xFE:
		return c.handleGroup4_8()

	// Group 5 (FF)
	case 0xFF:
		if operandSize == 2 {
			return c.handleGroup5_16()
		}
		return c.handleGroup5_32()

	// ADC AL, imm8
	case 0x14:
		c.SetReg8(AL, c.adc8(c.GetReg8(AL), c.fetch8()))

	// ADC EAX/AX, imm32/imm16
	case 0x15:
		if operandSize == 2 {
			c.SetReg16(AX, c.adc16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.adc32(c.GetReg32(EAX), c.fetch32()))
		}

	// SBB AL, imm8
	case 0x1C:
		c.SetReg8(AL, c.sbb8(c.GetReg8(AL), c.fetch8()))

	// SBB EAX/AX, imm32/imm16
	case 0x1D:
		if operandSize == 2 {
			c.SetReg16(AX, c.sbb16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.sbb32(c.GetReg32(EAX), c.fetch32()))
		}

	// AND AL, imm8
	case 0x24:
		c.SetReg8(AL, c.and8(c.GetReg8(AL), c.fetch8()))

	// AND EAX/AX, imm32/imm16
	case 0x25:
		if operandSize == 2 {
			c.SetReg16(AX, c.and16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.and32(c.GetReg32(EAX), c.fetch32()))
		}

	// OR AL, imm8
	case 0x0C:
		c.SetReg8(AL, c.or8(c.GetReg8(AL), c.fetch8()))

	// OR EAX/AX, imm32/imm16
	case 0x0D:
		if operandSize == 2 {
			c.SetReg16(AX, c.or16(c.GetReg16(AX), c.fetch16()))
		} else {
			c.SetReg32(EAX, c.or32(c.GetReg32(EAX), c.fetch32()))
		}

	// MOV r/m16, Sreg (8C)
	case 0x8C:
		mr := c.parseModRM()
		segVal := c.seg[mr.reg]
		if mr.isReg {
			c.SetReg16(reg16FromModRM(int(mr.rm)), segVal)
		} else {
			c.writeMem16(c.segBaseForModRM(mr) + mr.ea, segVal)
		}

	// MOV Sreg, r/m16 (8E)
	case 0x8E:
		mr := c.parseModRM()
		var segVal uint16
		if mr.isReg {
			segVal = c.GetReg16(reg16FromModRM(int(mr.rm)))
		} else {
			segVal = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
		}
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(int(mr.reg), segVal); err != nil {
				return err
			}
		} else {
			c.seg[mr.reg] = segVal
			c.segBase[mr.reg] = uint32(segVal) << 4
		}
		if mr.reg == SS {
			c.interruptsBlocked = true
		}

	// LES r16/32, m16:16/32
	case 0xC4:
		mr := c.parseModRM()
		if !mr.isReg {
			addr := c.segBaseForModRM(mr) + mr.ea
			if operandSize == 2 {
				c.SetReg16(reg16FromModRM(int(mr.reg)), c.readMem16(addr))
			} else {
				c.SetReg32(int(mr.reg), c.readMem32(addr))
			}
			segVal := c.readMem16(addr + uint32(operandSize))
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(ES, segVal); err != nil {
					return err
				}
			} else {
				c.seg[ES] = segVal
				c.segBase[ES] = uint32(segVal) << 4
			}
		}

	// LDS r16/32, m16:16/32
	case 0xC5:
		mr := c.parseModRM()
		if !mr.isReg {
			addr := c.segBaseForModRM(mr) + mr.ea
			if operandSize == 2 {
				c.SetReg16(reg16FromModRM(int(mr.reg)), c.readMem16(addr))
			} else {
				c.SetReg32(int(mr.reg), c.readMem32(addr))
			}
			segVal := c.readMem16(addr + uint32(operandSize))
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(DS, segVal); err != nil {
					return err
				}
			} else {
				c.seg[DS] = segVal
				c.segBase[DS] = uint32(segVal) << 4
			}
		}

	// JMPF ptr16:16 / ptr16:32
	case 0xEA:
		var off uint32
		if operandSize == 2 {
			off = uint32(c.fetch16())
		} else {
			off = c.fetch32()
		}
		cs := c.fetch16()
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(CS, cs); err != nil {
				return err
			}
		} else {
			c.seg[CS] = cs
			c.segBase[CS] = uint32(cs) << 4
		}
		c.eip = off

	// CALLF ptr16:16 / ptr16:32
	case 0x9A:
		var off uint32
		if operandSize == 2 {
			off = uint32(c.fetch16())
		} else {
			off = c.fetch32()
		}
		cs := c.fetch16()
		c.pushOp(uint32(c.seg[CS]), operandSize)
		c.pushOp(c.eip, operandSize)
		if c.IsProtectedMode() {
			if err := c.LoadSegmentProtected(CS, cs); err != nil {
				return err
			}
		} else {
			c.seg[CS] = cs
			c.segBase[CS] = uint32(cs) << 4
		}
		c.eip = off

	// RETF
	case 0xCB:
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
			cs := c.pop16()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, cs); err != nil {
					return err
				}
			} else {
				c.seg[CS] = cs
				c.segBase[CS] = uint32(cs) << 4
			}
		} else {
			c.eip = c.pop32()
			cs := c.pop32()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, uint16(cs)); err != nil {
					return err
				}
			} else {
				c.seg[CS] = uint16(cs)
				c.segBase[CS] = uint32(cs) << 4
			}
		}

	// RETF imm16
	case 0xCA:
		imm := c.fetch16()
		if operandSize == 2 {
			c.eip = uint32(c.pop16())
			cs := c.pop16()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, cs); err != nil {
					return err
				}
			} else {
				c.seg[CS] = cs
				c.segBase[CS] = uint32(cs) << 4
			}
			c.SetReg16(SP, c.GetReg16(SP)+imm)
		} else {
			c.eip = c.pop32()
			cs := c.pop32()
			if c.IsProtectedMode() {
				if err := c.LoadSegmentProtected(CS, uint16(cs)); err != nil {
					return err
				}
			} else {
				c.seg[CS] = uint16(cs)
				c.segBase[CS] = uint32(cs) << 4
			}
			c.SetReg32(ESP, c.GetReg32(ESP)+uint32(imm))
		}

	// ENTER imm16, imm8
	case 0xC8:
		size := c.fetch16()
		nesting := c.fetch8()
		c.push32(c.GetReg32(EBP))
		frame := c.GetReg32(ESP)
		if nesting > 0 {
			for i := uint8(1); i < nesting; i++ {
				c.SetReg32(EBP, c.GetReg32(EBP)-4)
				c.push32(c.readMem32(c.segBase[SS] + c.GetReg32(EBP)))
			}
			c.push32(frame)
		}
		c.SetReg32(EBP, frame)
		c.SetReg32(ESP, c.GetReg32(ESP)-uint32(size))

	// LEAVE
	case 0xC9:
		c.SetReg32(ESP, c.GetReg32(EBP))
		c.SetReg32(EBP, c.pop32())

	// BSWAP r32
	case 0x0F:
		opcode2 := c.fetch8()
		switch opcode2 {
		case 0x00:
			return c.handleGroupO_00()
		case 0x01:
			return c.handleGroupO_01()
		case 0x02:
			return c.handleLAR(operandSize)
		case 0x03:
			return c.handleLSL(operandSize)
		case 0x20:
			// MOV r32, CR0-CR7
			return c.handleMovCR(true)
		case 0x21:
			// MOV r32, DR0-DR7
			return c.handleMovDR(true)
		case 0x22:
			// MOV CR0-CR7, r32
			return c.handleMovCR(false)
		case 0x23:
			// MOV DR0-DR7, r32
			return c.handleMovDR(false)
		case 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8A, 0x8B, 0x8C, 0x8D, 0x8E, 0x8F:
			return c.handleJccNear(opcode2, operandSize)
		case 0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97,
			0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F:
			return c.handleSETcc(opcode2)
		case 0xA0:
			// PUSH FS
			c.push32(uint32(c.seg[FS]))
		case 0xA1:
			// POP FS
			return c.popSegReg(FS, operandSize, false)
		case 0xA8:
			// PUSH GS
			c.push32(uint32(c.seg[GS]))
		case 0xA9:
			// POP GS
			return c.popSegReg(GS, operandSize, false)
		case 0xA2:
			// CPUID
			c.handleCPUID()
		case 0xA4:
			// SHLD r/m16/32, r16/32, imm8
			mr := c.parseModRM()
			count := c.fetch8() & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst << count) | (src >> (16 - count))
					cf := (dst >> (16 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst << count) | (src >> (32 - count))
					cf := (dst >> (32 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			}
		case 0xA5:
			// SHLD r/m16/32, r16/32, CL
			mr := c.parseModRM()
			count := c.GetReg8(CL) & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst << count) | (src >> (16 - count))
					cf := (dst >> (16 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst << count) | (src >> (32 - count))
					cf := (dst >> (32 - count)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			}
		case 0xAC:
			// SHRD r/m16/32, r16/32, imm8
			mr := c.parseModRM()
			count := c.fetch8() & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst >> count) | (src << (16 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst >> count) | (src << (32 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			}
		case 0xAD:
			// SHRD r/m16/32, r16/32, CL
			mr := c.parseModRM()
			count := c.GetReg8(CL) & 0x1F
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				if count != 0 {
					result := (dst >> count) | (src << (16 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 15) & 1) != ((result >> 15) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>15)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), result)
					} else {
						c.writeMem16(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				if count != 0 {
					result := (dst >> count) | (src << (32 - count))
					cf := (dst >> (count - 1)) & 1
					c.setCF(cf != 0)
					if count == 1 {
						c.setOF(((dst >> 31) & 1) != ((result >> 31) & 1))
					}
					c.setZF(result == 0)
					c.setSF((result>>31)&1 != 0)
					c.setPF(parity8(uint8(result)))
					if mr.isReg {
						c.SetReg32(int(mr.rm), result)
					} else {
						c.writeMem32(c.segBaseForModRM(mr) + mr.ea, result)
					}
				}
			}
		case 0x30:
			// WRMSR — ECX = MSR number, EDX:EAX = value
			c.handleWRMSR()
		case 0x31:
			// RDTSC
			c.handleRDTSC()
		case 0x32:
			// RDMSR — ECX = MSR number, return EDX:EAX
			c.handleRDMSR()
		case 0xAF:
			// IMUL r16/r32, r/m16/r/m32. With the 0x66 prefix this is a
			// 16-bit multiply that must only touch the low 16 bits of the
			// destination — otherwise the upper half gets clobbered with
			// the sign-extended 32-bit product, breaking any code that
			// relies on it being preserved.
			mr := c.parseModRM()
			if operandSize == 2 {
				var v int16
				if mr.isReg {
					v = int16(c.GetReg16(reg16FromModRM(int(mr.rm))))
				} else {
					v = int16(c.readMem16(c.segBaseForModRM(mr) + mr.ea))
				}
				r := int32(int16(c.GetReg16(reg16FromModRM(int(mr.reg))))) * int32(v)
				c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(r))
				c.setOF(r != int32(int16(r)))
				c.setCF(r != int32(int16(r)))
			} else {
				var v int32
				if mr.isReg {
					v = int32(c.GetReg32(int(mr.rm)))
				} else {
					v = int32(c.readMem32(c.segBaseForModRM(mr) + mr.ea))
				}
				r := int64(int32(c.GetReg32(int(mr.reg)))) * int64(v)
				c.SetReg32(int(mr.reg), uint32(r))
				c.setOF(r != int64(int32(r)))
				c.setCF(r != int64(int32(r)))
			}
		case 0xB0:
			// CMPXCHG r/m8, r8
			// SDM: CF, OF, SF, ZF, AF, PF are all set as if `CMP acc, dst`
			// had been performed (i.e. the SUB without writeback). The
			// ZF=1 case writes src→dst; ZF=0 case loads dst→AL.
			mr := c.parseModRM()
			var dst uint8
			if mr.isReg {
				dst = c.GetReg8(reg8FromModRM(int(mr.rm)))
			} else {
				dst = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
			}
			acc := c.GetReg8(AL)
			src := c.GetReg8(reg8FromModRM(int(mr.reg)))
			c.sub8(acc, dst) // sets all CMP flags
			if acc == dst {
				if mr.isReg {
					c.SetReg8(reg8FromModRM(int(mr.rm)), src)
				} else {
					c.writeMem8(c.segBaseForModRM(mr) + mr.ea, src)
				}
			} else {
				c.SetReg8(AL, dst)
			}
		case 0xB1:
			// CMPXCHG r/m16/32, r16/32
			// SDM: full CMP flag set (CF/OF/SF/ZF/AF/PF) is applied.
			mr := c.parseModRM()
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				acc := c.GetReg16(AX)
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				c.sub16(acc, dst)
				if acc == dst {
					if mr.isReg {
						c.SetReg16(reg16FromModRM(int(mr.rm)), src)
					} else {
						c.writeMem16(c.segBaseForModRM(mr) + mr.ea, src)
					}
				} else {
					c.SetReg16(AX, dst)
				}
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				acc := c.GetReg32(EAX)
				src := c.GetReg32(int(mr.reg))
				c.sub32(acc, dst)
				if acc == dst {
					if mr.isReg {
						c.SetReg32(int(mr.rm), src)
					} else {
						c.writeMem32(c.segBaseForModRM(mr) + mr.ea, src)
					}
				} else {
					c.SetReg32(EAX, dst)
				}
			}
		case 0xA3:
			// BT r/m16/32, r16/32
			// Per Intel SDM: when the destination is MEMORY and the bit
			// index comes from a REGISTER, the bit index is treated as a
			// signed value that extends the memory address — i.e. the
			// instruction accesses dword/word `base + (bit/32)*4` and
			// modifies bit `bit & 0x1F` of that dword (signed shift for
			// negative indices). For register destinations the bit index
			// IS masked to operand_size-1.
			mr := c.parseModRM()
			if operandSize == 2 {
				if mr.isReg {
					bit := uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask := uint16(1) << bit
					c.setCF((c.GetReg16(reg16FromModRM(int(mr.rm))) & mask) != 0)
				} else {
					bitIdx := int32(int16(c.GetReg16(reg16FromModRM(int(mr.reg)))))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>4)*2)
					mask := uint16(1) << uint32(bitIdx&0xF)
					c.setCF((c.readMem16(addr) & mask) != 0)
				}
			} else {
				if mr.isReg {
					bit := uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask := uint32(1) << bit
					c.setCF((c.GetReg32(int(mr.rm)) & mask) != 0)
				} else {
					bitIdx := int32(c.GetReg32(int(mr.reg)))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>5)*4)
					mask := uint32(1) << uint32(bitIdx&0x1F)
					c.setCF((c.readMem32(addr) & mask) != 0)
				}
			}
		case 0xAB:
			// BTS r/m16/32, r16/32 — memory operand + register bit index
			// uses signed bit offset that extends the memory address.
			mr := c.parseModRM()
			if operandSize == 2 {
				if mr.isReg {
					bit := uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask := uint16(1) << bit
					v := c.GetReg16(reg16FromModRM(int(mr.rm)))
					c.setCF((v & mask) != 0)
					c.SetReg16(reg16FromModRM(int(mr.rm)), v|mask)
				} else {
					bitIdx := int32(int16(c.GetReg16(reg16FromModRM(int(mr.reg)))))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>4)*2)
					mask := uint16(1) << uint32(bitIdx&0xF)
					v := c.readMem16(addr)
					c.setCF((v & mask) != 0)
					c.writeMem16(addr, v|mask)
				}
			} else {
				if mr.isReg {
					bit := uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask := uint32(1) << bit
					v := c.GetReg32(int(mr.rm))
					c.setCF((v & mask) != 0)
					c.SetReg32(int(mr.rm), v|mask)
				} else {
					bitIdx := int32(c.GetReg32(int(mr.reg)))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>5)*4)
					mask := uint32(1) << uint32(bitIdx&0x1F)
					v := c.readMem32(addr)
					c.setCF((v & mask) != 0)
					c.writeMem32(addr, v|mask)
				}
			}
		case 0xB3:
			// BTR r/m16/32, r16/32 — memory operand + register bit index
			// uses signed bit offset that extends the memory address. This
			// is what Linux's setup_clear_cpu_cap uses; prior masking-only
			// impl caused cap[0] bits to be cleared when the kernel meant
			// to clear cap[N] bits.
			mr := c.parseModRM()
			if operandSize == 2 {
				if mr.isReg {
					bit := uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask := uint16(1) << bit
					v := c.GetReg16(reg16FromModRM(int(mr.rm)))
					c.setCF((v & mask) != 0)
					c.SetReg16(reg16FromModRM(int(mr.rm)), v&^mask)
				} else {
					bitIdx := int32(int16(c.GetReg16(reg16FromModRM(int(mr.reg)))))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>4)*2)
					mask := uint16(1) << uint32(bitIdx&0xF)
					v := c.readMem16(addr)
					c.setCF((v & mask) != 0)
					c.writeMem16(addr, v&^mask)
				}
			} else {
				if mr.isReg {
					bit := uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask := uint32(1) << bit
					v := c.GetReg32(int(mr.rm))
					c.setCF((v & mask) != 0)
					c.SetReg32(int(mr.rm), v&^mask)
				} else {
					bitIdx := int32(c.GetReg32(int(mr.reg)))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>5)*4)
					mask := uint32(1) << uint32(bitIdx&0x1F)
					v := c.readMem32(addr)
					c.setCF((v & mask) != 0)
					c.writeMem32(addr, v&^mask)
				}
			}
		case 0xBA:
			// BT/BTS/BTR/BTC r/m16/32, imm8
			mr := c.parseModRM()
			imm := uint32(c.fetch8())
			switch mr.reg {
			case 4: // BT
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						c.setCF((c.GetReg16(reg16FromModRM(int(mr.rm))) & mask) != 0)
					} else {
						c.setCF((c.readMem16(c.segBaseForModRM(mr) + mr.ea) & mask) != 0)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						c.setCF((c.GetReg32(int(mr.rm)) & mask) != 0)
					} else {
						c.setCF((c.readMem32(c.segBaseForModRM(mr) + mr.ea) & mask) != 0)
					}
				}
			case 5: // BTS
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						v := c.GetReg16(reg16FromModRM(int(mr.rm)))
						c.setCF((v & mask) != 0)
						c.SetReg16(reg16FromModRM(int(mr.rm)), v|mask)
					} else {
						addr := c.segBaseForModRM(mr) + mr.ea
						v := c.readMem16(addr)
						c.setCF((v & mask) != 0)
						c.writeMem16(addr, v|mask)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						v := c.GetReg32(int(mr.rm))
						c.setCF((v & mask) != 0)
						c.SetReg32(int(mr.rm), v|mask)
					} else {
						addr := c.segBaseForModRM(mr) + mr.ea
						v := c.readMem32(addr)
						c.setCF((v & mask) != 0)
						c.writeMem32(addr, v|mask)
					}
				}
			case 6: // BTR
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						v := c.GetReg16(reg16FromModRM(int(mr.rm)))
						c.setCF((v & mask) != 0)
						c.SetReg16(reg16FromModRM(int(mr.rm)), v&^mask)
					} else {
						addr := c.segBaseForModRM(mr) + mr.ea
						v := c.readMem16(addr)
						c.setCF((v & mask) != 0)
						c.writeMem16(addr, v&^mask)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						v := c.GetReg32(int(mr.rm))
						c.setCF((v & mask) != 0)
						c.SetReg32(int(mr.rm), v&^mask)
					} else {
						addr := c.segBaseForModRM(mr) + mr.ea
						v := c.readMem32(addr)
						c.setCF((v & mask) != 0)
						c.writeMem32(addr, v&^mask)
					}
				}
			case 7: // BTC
				if operandSize == 2 {
					bit := imm & 0xF
					mask := uint16(1) << bit
					if mr.isReg {
						v := c.GetReg16(reg16FromModRM(int(mr.rm)))
						c.setCF((v & mask) != 0)
						c.SetReg16(reg16FromModRM(int(mr.rm)), v^mask)
					} else {
						addr := c.segBaseForModRM(mr) + mr.ea
						v := c.readMem16(addr)
						c.setCF((v & mask) != 0)
						c.writeMem16(addr, v^mask)
					}
				} else {
					bit := imm & 0x1F
					mask := uint32(1) << bit
					if mr.isReg {
						v := c.GetReg32(int(mr.rm))
						c.setCF((v & mask) != 0)
						c.SetReg32(int(mr.rm), v^mask)
					} else {
						addr := c.segBaseForModRM(mr) + mr.ea
						v := c.readMem32(addr)
						c.setCF((v & mask) != 0)
						c.writeMem32(addr, v^mask)
					}
				}
			default:
				return fmt.Errorf("0F BA /%d not implemented", mr.reg)
			}
		case 0xBB:
			// BTC r/m16/32, r16/32 — memory operand + register bit index
			// uses signed bit offset that extends the memory address.
			mr := c.parseModRM()
			if operandSize == 2 {
				if mr.isReg {
					bit := uint32(c.GetReg16(reg16FromModRM(int(mr.reg)))) & 0xF
					mask := uint16(1) << bit
					v := c.GetReg16(reg16FromModRM(int(mr.rm)))
					c.setCF((v & mask) != 0)
					c.SetReg16(reg16FromModRM(int(mr.rm)), v^mask)
				} else {
					bitIdx := int32(int16(c.GetReg16(reg16FromModRM(int(mr.reg)))))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>4)*2)
					mask := uint16(1) << uint32(bitIdx&0xF)
					v := c.readMem16(addr)
					c.setCF((v & mask) != 0)
					c.writeMem16(addr, v^mask)
				}
			} else {
				if mr.isReg {
					bit := uint32(c.GetReg32(int(mr.reg))) & 0x1F
					mask := uint32(1) << bit
					v := c.GetReg32(int(mr.rm))
					c.setCF((v & mask) != 0)
					c.SetReg32(int(mr.rm), v^mask)
				} else {
					bitIdx := int32(c.GetReg32(int(mr.reg)))
					base := c.segBaseForModRM(mr) + mr.ea
					addr := uint32(int32(base) + (bitIdx>>5)*4)
					mask := uint32(1) << uint32(bitIdx&0x1F)
					v := c.readMem32(addr)
					c.setCF((v & mask) != 0)
					c.writeMem32(addr, v^mask)
				}
			}
		case 0xB6:
			// MOVZX r16/r32, r/m8. With the 0x66 operand prefix the
			// destination is r16 and only the low 16 bits should be
			// modified — the upper 16 bits of the underlying r32 must be
			// preserved, NOT clobbered to zero.
			mr := c.parseModRM()
			var b uint8
			if mr.isReg {
				b = c.GetReg8(reg8FromModRM(int(mr.rm)))
			} else {
				b = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
			}
			if operandSize == 2 {
				c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(b))
			} else {
				c.SetReg32(int(mr.reg), uint32(b))
			}
		case 0xB7:
			// MOVZX r32, r/m16 — the 0x66 prefix variant
			// (MOVZX r16, r/m16) is meaningless and rejected by assemblers,
			// so we always write 32 bits here.
			mr := c.parseModRM()
			var v uint32
			if mr.isReg {
				v = uint32(c.GetReg16(reg16FromModRM(int(mr.rm))))
			} else {
				v = uint32(c.readMem16(c.segBaseForModRM(mr) + mr.ea))
			}
			c.SetReg32(int(mr.reg), v)
		case 0xBE:
			// MOVSX r16/r32, r/m8.
			mr := c.parseModRM()
			var b uint8
			if mr.isReg {
				b = c.GetReg8(reg8FromModRM(int(mr.rm)))
			} else {
				b = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
			}
			if operandSize == 2 {
				c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(int16(int8(b))))
			} else {
				c.SetReg32(int(mr.reg), uint32(int32(int8(b))))
			}
		case 0xBF:
			// MOVSX r32, r/m16.
			mr := c.parseModRM()
			var v int32
			if mr.isReg {
				v = int32(int16(c.GetReg16(reg16FromModRM(int(mr.rm)))))
			} else {
				v = int32(int16(c.readMem16(c.segBaseForModRM(mr) + mr.ea)))
			}
			c.SetReg32(int(mr.reg), uint32(v))
		case 0xC8, 0xC9, 0xCA, 0xCB, 0xCC, 0xCD, 0xCE, 0xCF:
			// BSWAP r32
			r := opcode2 & 7
			v := c.GetReg32(int(r))
			c.SetReg32(int(r), (v>>24)|((v>>8)&0xFF00)|((v<<8)&0xFF0000)|(v<<24))

		// CMOVcc r16/32, r/m16/32 (0F 40..4F).
		case 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
			0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F:
			return c.handleCMOVcc(opcode2, operandSize)

		// Multi-byte NOP and prefetch family — all decode as ModR/M
		// + ignore. Includes:
		//   0F 1F /n  multi-byte NOP
		//   0F 0D     3DNow! prefetch
		//   0F 18 /n  PREFETCHNTA / PREFETCHT0/T1/T2 (SSE prefetch hints)
		//   0F 19..1E various reserved 0F XX /n NOPs (Intel reserved
		//             these as future-NOP encodings to satisfy
		//             prefixing by future SSE instructions).
		case 0x1F, 0x0D, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E:
			c.parseModRM()

		// 0F AE — group encoding. Subopcode in reg field of ModRM:
		//   /0 FXSAVE m512       — NOPed (no XMM context to save)
		//   /1 FXRSTOR m512      — NOPed
		//   /2 LDMXCSR m32       — load MXCSR (needed: musl's fetestexcept
		//                          reads MXCSR via STMXCSR, so we must keep
		//                          a coherent register)
		//   /3 STMXCSR m32       — store MXCSR
		//   /4 XSAVE             — NOPed
		//   /5 XRSTOR / LFENCE   — NOPed
		//   /6 MFENCE / XSAVEOPT — NOPed
		//   /7 SFENCE / CLFLUSH  — NOPed
		case 0xAE:
			mr := c.parseModRM()
			switch mr.reg {
			case 0: // FXSAVE m512
				if mr.isReg {
					return fmt.Errorf("FXSAVE requires memory operand at EIP=%08X", c.eip-3)
				}
				c.fxsave(c.segBaseForModRM(mr) + mr.ea)
			case 1: // FXRSTOR m512
				if mr.isReg {
					return fmt.Errorf("FXRSTOR requires memory operand at EIP=%08X", c.eip-3)
				}
				c.fxrstor(c.segBaseForModRM(mr) + mr.ea)
			case 2: // LDMXCSR
				if mr.isReg {
					return fmt.Errorf("LDMXCSR requires memory operand at EIP=%08X", c.eip-3)
				}
				c.mxcsr = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
			case 3: // STMXCSR
				if mr.isReg {
					return fmt.Errorf("STMXCSR requires memory operand at EIP=%08X", c.eip-3)
				}
				c.writeMem32(c.segBaseForModRM(mr)+mr.ea, c.mxcsr)
			}

		// CLTS (clear CR0.TS).
		case 0x06:
			if c.IsProtectedMode() && c.cpl != 0 {
				c.raiseGeneralProtectionFault(0)
			}
			c.cr[0] &^= CR0_TS

		// INVD, WBINVD: ignore (no caches).
		case 0x08, 0x09:
			if c.IsProtectedMode() && c.cpl != 0 {
				c.raiseGeneralProtectionFault(0)
			}

		// UD2 — always raises #UD. Linux uses 0F 0B as the implementation
		// of BUG(), with the kernel #UD handler decoding the trailing bug
		// report. We must deliver #UD so that handler runs.
		case 0x0B:
			panic(invalidOpcodeError{})

		// LSS r16/32, m16:32 — load SS:reg from memory.
		case 0xB2:
			return c.handleLoadFarPointer(SS, operandSize)
		// LFS
		case 0xB4:
			return c.handleLoadFarPointer(FS, operandSize)
		// LGS
		case 0xB5:
			return c.handleLoadFarPointer(GS, operandSize)

		// XADD r/m8, r8
		case 0xC0:
			mr := c.parseModRM()
			var dst uint8
			if mr.isReg {
				dst = c.GetReg8(reg8FromModRM(int(mr.rm)))
			} else {
				dst = c.readMem8(c.segBaseForModRM(mr) + mr.ea)
			}
			src := c.GetReg8(reg8FromModRM(int(mr.reg)))
			sum := c.add8(dst, src)
			if mr.isReg {
				c.SetReg8(reg8FromModRM(int(mr.rm)), sum)
			} else {
				c.writeMem8(c.segBaseForModRM(mr) + mr.ea, sum)
			}
			c.SetReg8(reg8FromModRM(int(mr.reg)), dst)

		// XADD r/m16/32, r16/32
		case 0xC1:
			mr := c.parseModRM()
			if operandSize == 2 {
				var dst uint16
				if mr.isReg {
					dst = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					dst = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg16(reg16FromModRM(int(mr.reg)))
				sum := c.add16(dst, src)
				if mr.isReg {
					c.SetReg16(reg16FromModRM(int(mr.rm)), sum)
				} else {
					c.writeMem16(c.segBaseForModRM(mr) + mr.ea, sum)
				}
				c.SetReg16(reg16FromModRM(int(mr.reg)), dst)
			} else {
				var dst uint32
				if mr.isReg {
					dst = c.GetReg32(int(mr.rm))
				} else {
					dst = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				src := c.GetReg32(int(mr.reg))
				sum := c.add32(dst, src)
				if mr.isReg {
					c.SetReg32(int(mr.rm), sum)
				} else {
					c.writeMem32(c.segBaseForModRM(mr) + mr.ea, sum)
				}
				c.SetReg32(int(mr.reg), dst)
			}

		// CMPXCHG8B m64 (group: 0F C7 /1)
		case 0xC7:
			mr := c.parseModRM()
			if mr.reg != 1 || mr.isReg {
				return fmt.Errorf("0F C7 /%d not implemented", mr.reg)
			}
			addr := c.segBaseForModRM(mr) + mr.ea
			memLo := c.readMem32(addr)
			memHi := c.readMem32(addr + 4)
			eax := c.GetReg32(EAX)
			edx := c.GetReg32(EDX)
			if memLo == eax && memHi == edx {
				c.setZF(true)
				c.writeMem32(addr, c.GetReg32(EBX))
				c.writeMem32(addr+4, c.GetReg32(ECX))
			} else {
				c.setZF(false)
				c.SetReg32(EAX, memLo)
				c.SetReg32(EDX, memHi)
			}

		// BSF r16/32, r/m16/32
		case 0xBC:
			mr := c.parseModRM()
			if operandSize == 2 {
				var src uint16
				if mr.isReg {
					src = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					src = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				if src == 0 {
					c.setZF(true)
				} else {
					c.setZF(false)
					var i uint16
					for i = 0; i < 16; i++ {
						if src&(1<<i) != 0 {
							break
						}
					}
					c.SetReg16(reg16FromModRM(int(mr.reg)), i)
				}
			} else {
				var src uint32
				if mr.isReg {
					src = c.GetReg32(int(mr.rm))
				} else {
					src = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				if src == 0 {
					c.setZF(true)
				} else {
					c.setZF(false)
					var i uint32
					for i = 0; i < 32; i++ {
						if src&(1<<i) != 0 {
							break
						}
					}
					c.SetReg32(int(mr.reg), i)
				}
			}

		// BSR r16/32, r/m16/32
		case 0xBD:
			mr := c.parseModRM()
			if operandSize == 2 {
				var src uint16
				if mr.isReg {
					src = c.GetReg16(reg16FromModRM(int(mr.rm)))
				} else {
					src = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
				}
				if src == 0 {
					c.setZF(true)
				} else {
					c.setZF(false)
					var i int = 15
					for ; i >= 0; i-- {
						if src&(1<<uint(i)) != 0 {
							break
						}
					}
					c.SetReg16(reg16FromModRM(int(mr.reg)), uint16(i))
				}
			} else {
				var src uint32
				if mr.isReg {
					src = c.GetReg32(int(mr.rm))
				} else {
					src = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				if src == 0 {
					c.setZF(true)
				} else {
					c.setZF(false)
					var i int = 31
					for ; i >= 0; i-- {
						if src&(1<<uint(i)) != 0 {
							break
						}
					}
					c.SetReg32(int(mr.reg), uint32(i))
				}
			}

		// MMX move/EMMS spike: just enough to get musl libc past its
		// MMX-using memcpy/strlen paths. No actual SIMD math — just
		// MOVD/MOVQ between registers + memory, and EMMS to mark the
		// MMX state empty.
		//
		// 0F 6E  MOVD mm, r/m32          load 32 bits, zero-extend
		// 0F 6F  MOVQ mm, mm/m64         load 64 bits
		// 0F 7E  MOVD r/m32, mm          store low 32 bits
		// 0F 7F  MOVQ mm/m64, mm         store 64 bits
		// 0F 77  EMMS                    no-op (we have no x87 state
		//                                to tag-as-empty, but EMMS
		//                                still needs to be a valid op)
		// 0F 6E: no prefix → MOVD mm, r/m32  (MMX)
		//        66 prefix → MOVD xmm, r/m32 (SSE2, zero-extends to 128)
		case 0x6E:
			mr := c.parseModRM()
			var v uint32
			if mr.isReg {
				v = c.GetReg32(int(mr.rm))
			} else {
				v = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
			}
			if operandSize == 2 {
				c.xmm[mr.reg][0] = uint64(v)
				c.xmm[mr.reg][1] = 0
			} else {
				c.mm[mr.reg] = uint64(v)
			}

		// 0F 6F: no prefix → MOVQ mm, mm/m64       (MMX)
		//        66 prefix → MOVDQA xmm, xmm/m128 (SSE2 aligned)
		//        F3 prefix → MOVDQU xmm, xmm/m128 (SSE2 unaligned)
		case 0x6F:
			mr := c.parseModRM()
			if operandSize == 2 || repPrefix == 1 {
				var v [2]uint64
				if mr.isReg {
					v = c.xmm[mr.rm]
				} else {
					v = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
				}
				c.xmm[mr.reg] = v
			} else {
				var v uint64
				if mr.isReg {
					v = c.mm[mr.rm]
				} else {
					v = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
				}
				c.mm[mr.reg] = v
			}

		// 0F 7E: no prefix → MOVD r/m32, mm  (MMX)
		//        66 prefix → MOVD r/m32, xmm (SSE2, low 32 bits)
		//        F3 prefix → MOVQ xmm, xmm/m64 (SSE2)
		case 0x7E:
			mr := c.parseModRM()
			if repPrefix == 1 {
				var v uint64
				if mr.isReg {
					v = c.xmm[mr.rm][0]
				} else {
					v = c.readMem64(c.segBaseForModRM(mr) + mr.ea)
				}
				c.xmm[mr.reg][0] = v
				c.xmm[mr.reg][1] = 0
			} else if operandSize == 2 {
				lo := uint32(c.xmm[mr.reg][0])
				if mr.isReg {
					c.SetReg32(int(mr.rm), lo)
				} else {
					c.writeMem32(c.segBaseForModRM(mr) + mr.ea, lo)
				}
			} else {
				lo := uint32(c.mm[mr.reg])
				if mr.isReg {
					c.SetReg32(int(mr.rm), lo)
				} else {
					c.writeMem32(c.segBaseForModRM(mr) + mr.ea, lo)
				}
			}

		// 0F 7F: no prefix → MOVQ mm/m64, mm        (MMX)
		//        66 prefix → MOVDQA xmm/m128, xmm  (SSE2)
		//        F3 prefix → MOVDQU xmm/m128, xmm  (SSE2)
		case 0x7F:
			mr := c.parseModRM()
			if operandSize == 2 || repPrefix == 1 {
				v := c.xmm[mr.reg]
				if mr.isReg {
					c.xmm[mr.rm] = v
				} else {
					c.writeMem128(c.segBaseForModRM(mr) + mr.ea, v)
				}
			} else {
				v := c.mm[mr.reg]
				if mr.isReg {
					c.mm[mr.rm] = v
				} else {
					c.writeMem64(c.segBaseForModRM(mr) + mr.ea, v)
				}
			}

		// 0F D6: 66 prefix → MOVQ xmm/m64, xmm (SSE2 store low qword)
		//        F2 prefix → MOVDQ2Q mm, xmm   (SSE2 lower 64 to MMX)
		//        F3 prefix → MOVQ2DQ xmm, mm   (SSE2 MMX to lower 64)
		case 0xD6:
			mr := c.parseModRM()
			switch {
			case operandSize == 2:
				v := c.xmm[mr.reg][0]
				if mr.isReg {
					c.xmm[mr.rm][0] = v
					c.xmm[mr.rm][1] = 0
				} else {
					c.writeMem64(c.segBaseForModRM(mr) + mr.ea, v)
				}
			case repPrefix == 2:
				c.mm[mr.reg] = c.xmm[mr.rm][0]
			case repPrefix == 1:
				c.xmm[mr.reg][0] = c.mm[mr.rm]
				c.xmm[mr.reg][1] = 0
			default:
				return fmt.Errorf("0F D6 without 66/F2/F3 prefix at EIP=%08X", c.eip-2)
			}

		// 0F D7    PMOVMSKB r32, mm   — sign bits of 8 packed bytes → low 8 bits
		// 66 0F D7 PMOVMSKB r32, xmm  — sign bits of 16 packed bytes → low 16 bits
		// Used by musl strchr/strlen SSE2 implementations.
		case 0xD7:
			mr := c.parseModRM()
			if !mr.isReg {
				return fmt.Errorf("PMOVMSKB requires register source at EIP=%08X", c.eip-2)
			}
			var mask uint32
			if operandSize == 2 {
				// 128-bit XMM: 16 sign bits
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
				// 64-bit MMX: 8 sign bits
				v := c.mm[mr.rm]
				for i := 0; i < 8; i++ {
					if v&(1<<(i*8+7)) != 0 {
						mask |= 1 << uint(i)
					}
				}
			}
			c.SetReg32(int(mr.reg), mask)

		case 0x77:
			// EMMS: tag word ← all-empty. We don't track tag state.

		// Non-temporal stores (cache-bypass hint, semantically just
		// stores — we don't model caches).
		//
		// 0F E7    MOVNTQ   m64, mm
		// 0F 2B    MOVNTPS  m128, xmm    (66 prefix → MOVNTPD)
		// 0F C3    MOVNTI   m32, r32     (SSE2 integer non-temporal)
		case 0xE7:
			mr := c.parseModRM()
			if mr.isReg {
				return fmt.Errorf("MOVNTQ requires memory dest at EIP=%08X", c.eip-2)
			}
			c.writeMem64(c.segBaseForModRM(mr)+mr.ea, c.mm[mr.reg])

		case 0x2B:
			mr := c.parseModRM()
			if mr.isReg {
				return fmt.Errorf("MOVNTPS/PD requires memory dest at EIP=%08X", c.eip-2)
			}
			c.writeMem128(c.segBaseForModRM(mr)+mr.ea, c.xmm[mr.reg])

		case 0xC3:
			// MOVNTI: special-case — needs no prefix to differ from
			// MMX/SSE ops at this opcode. It's r32 → m32.
			mr := c.parseModRM()
			if mr.isReg {
				return fmt.Errorf("MOVNTI requires memory dest at EIP=%08X", c.eip-2)
			}
			c.writeMem32(c.segBaseForModRM(mr)+mr.ea, c.GetReg32(int(mr.reg)))

		// SSE/SSE2 floating-point bitwise. These operate on the full
		// 128 bits — packed-FP semantics are identical to "bitwise"
		// because the bits don't propagate carries across lanes.
		//
		// 0F 54  ANDPS  xmm, xmm/m128       (66 → ANDPD)
		// 0F 55  ANDNPS xmm, xmm/m128       (66 → ANDNPD)
		// 0F 56  ORPS   xmm, xmm/m128       (66 → ORPD)
		// 0F 50    MOVMSKPS r32, xmm   — sign bits of 4 packed singles → low 4 bits
		// 66 0F 50 MOVMSKPD r32, xmm   — sign bits of 2 packed doubles → low 2 bits
		// 0F D7    PMOVMSKB r32, mm    — sign bits of 8 packed bytes → low 8 bits
		// 66 0F D7 PMOVMSKB r32, xmm   — sign bits of 16 packed bytes → low 16 bits
		// Busybox/musl use these heavily (e.g. SSE2 strlen/memchr).
		case 0x50:
			mr := c.parseModRM()
			if !mr.isReg {
				return fmt.Errorf("MOVMSKPS/PD requires register source at EIP=%08X", c.eip-2)
			}
			v := c.xmm[mr.rm]
			var mask uint32
			if operandSize == 2 {
				// MOVMSKPD: 2 sign bits
				if v[0]&(1<<63) != 0 {
					mask |= 1
				}
				if v[1]&(1<<63) != 0 {
					mask |= 2
				}
			} else {
				// MOVMSKPS: 4 sign bits (one per 32-bit lane)
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

		// 0F 57  XORPS  xmm, xmm/m128       (66 → XORPD)
		case 0x54, 0x55, 0x56, 0x57:
			mr := c.parseModRM()
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			switch opcode2 {
			case 0x54: // AND
				dst[0] &= src[0]
				dst[1] &= src[1]
			case 0x55: // ANDN (NOT dst) AND src
				dst[0] = (^dst[0]) & src[0]
				dst[1] = (^dst[1]) & src[1]
			case 0x56: // OR
				dst[0] |= src[0]
				dst[1] |= src[1]
			case 0x57: // XOR
				dst[0] ^= src[0]
				dst[1] ^= src[1]
			}
			c.xmm[mr.reg] = dst

		// SSE/SSE2 moves between XMM, memory, and GPR. Prefix encoding
		// per Intel SDM §C.2: width and zero-extension differ
		// significantly across prefix variants and we MUST honor
		// each — a missing zero-extend leaves garbage in the high
		// lanes which gets stored as a struct-field "value" and
		// crashes the kernel a few instructions later when the
		// struct's vtable pointer is interpreted as code.
		//
		//   no prefix  0F 28/29  MOVAPS  xmm <-> m128 (aligned)
		//   66 prefix  0F 28/29  MOVAPD  xmm <-> m128 (aligned)
		//   no prefix  0F 10/11  MOVUPS  xmm <-> m128 (unaligned)
		//   66 prefix  0F 10/11  MOVUPD  xmm <-> m128 (unaligned)
		//   F3 prefix  0F 10     MOVSS  xmm <- m32: zero-extend
		//                               xmm <- xmm:  copy low 32, keep high
		//   F3 prefix  0F 11     MOVSS  m32 <- xmm: low 32 only
		//   F2 prefix  0F 10     MOVSD  xmm <- m64: zero-extend
		//                               xmm <- xmm:  copy low 64, keep high
		//   F2 prefix  0F 11     MOVSD  m64 <- xmm: low 64 only
		case 0x28:
			// MOVAPS / MOVAPD: full 128-bit load.
			mr := c.parseModRM()
			var v [2]uint64
			if mr.isReg {
				v = c.xmm[mr.rm]
			} else {
				v = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg] = v

		case 0x2E, 0x2F:
			// 0F 2E   UCOMISS  xmm1, xmm2/m32  — unordered compare scalar SP, set EFLAGS
			// 0F 2F   COMISS   xmm1, xmm2/m32  — ordered (we treat the same; no FP exceptions)
			// 66 0F 2E UCOMISD xmm1, xmm2/m64
			// 66 0F 2F COMISD  xmm1, xmm2/m64
			// Intel SDM Vol 2A: set EFLAGS.ZF/PF/CF from the comparison
			// like FCOMI/FUCOMI; OF/SF/AF cleared. Used by gcc -O2 FP
			// comparisons; busybox awk/printf compiles to these (6× UCOMISS
			// + 3× COMISS in our boot's busybox).
			mr := c.parseModRM()
			var a, b float64
			if operandSize == 2 {
				// SCALAR DOUBLE (UCOMISD/COMISD) — 66 prefix
				var ub uint64
				if mr.isReg {
					ub = c.xmm[mr.rm][0]
				} else {
					ub = uint64(c.readMem32(c.segBaseForModRM(mr)+mr.ea)) |
						uint64(c.readMem32(c.segBaseForModRM(mr)+mr.ea+4))<<32
				}
				a = ssemath.Float64frombits(c.xmm[mr.reg][0])
				b = ssemath.Float64frombits(ub)
			} else {
				// SCALAR SINGLE (UCOMISS/COMISS)
				var ub uint32
				if mr.isReg {
					ub = uint32(c.xmm[mr.rm][0])
				} else {
					ub = c.readMem32(c.segBaseForModRM(mr) + mr.ea)
				}
				a = float64(ssemath.Float32frombits(uint32(c.xmm[mr.reg][0])))
				b = float64(ssemath.Float32frombits(ub))
			}
			c.fpuCompareSetEFlags(a, b)

		case 0x14, 0x15:
			// 0F 14    UNPCKLPS xmm1, xmm2/m128 — interleave LOW packed singles
			// 0F 15    UNPCKHPS xmm1, xmm2/m128 — interleave HIGH packed singles
			// 66 0F 14 UNPCKLPD  — interleave LOW packed doubles
			// 66 0F 15 UNPCKHPD  — interleave HIGH packed doubles
			mr := c.parseModRM()
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			dst := c.xmm[mr.reg]
			if operandSize == 2 {
				// UNPCKLPD / UNPCKHPD: 64-bit elements
				if opcode2 == 0x14 {
					c.xmm[mr.reg] = [2]uint64{dst[0], src[0]}
				} else {
					c.xmm[mr.reg] = [2]uint64{dst[1], src[1]}
				}
			} else {
				// UNPCKLPS / UNPCKHPS: 32-bit elements
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

		case 0x29:
			// MOVAPS / MOVAPD store: full 128-bit.
			mr := c.parseModRM()
			v := c.xmm[mr.reg]
			if mr.isReg {
				c.xmm[mr.rm] = v
			} else {
				c.writeMem128(c.segBaseForModRM(mr) + mr.ea, v)
			}

		case 0x10:
			mr := c.parseModRM()
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

		case 0x11:
			mr := c.parseModRM()
			switch repPrefix {
			case 1: // F3 → MOVSS store
				if mr.isReg {
					c.xmm[mr.rm][0] = (c.xmm[mr.rm][0] &^ 0xFFFFFFFF) |
						(c.xmm[mr.reg][0] & 0xFFFFFFFF)
				} else {
					c.writeMem32(c.segBaseForModRM(mr) + mr.ea, uint32(c.xmm[mr.reg][0]))
				}
			case 2: // F2 → MOVSD store
				if mr.isReg {
					c.xmm[mr.rm][0] = c.xmm[mr.reg][0]
				} else {
					c.writeMem64(c.segBaseForModRM(mr) + mr.ea, c.xmm[mr.reg][0])
				}
			default: // none/66 → MOVUPS/MOVUPD store
				v := c.xmm[mr.reg]
				if mr.isReg {
					c.xmm[mr.rm] = v
				} else {
					c.writeMem128(c.segBaseForModRM(mr) + mr.ea, v)
				}
			}

		// MMX packed-integer instructions. Each operates on the full
		// 64-bit MMX register, optionally splitting it into smaller
		// elements based on the suffix encoded in the opcode:
		//   B = packed bytes        (8 × 8-bit)
		//   W = packed words        (4 × 16-bit)
		//   D = packed doublewords  (2 × 32-bit)
		//   Q = quadword            (1 × 64-bit)
		// Source operand is always r/m (another MMX register or a
		// 64-bit memory operand); destination is always an MMX
		// register encoded in the `reg` field.
		case 0xDB, 0xDF, 0xEB, 0xEF: // PAND, PANDN, POR, PXOR
			mr := c.parseModRM()
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
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
			c.mm[mr.reg] = dst

		case 0x74, 0x75, 0x76: // PCMPEQB/W/D
			mr := c.parseModRM()
			elem := 1 << (opcode2 - 0x74)
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedCmpEq(dst, src, elem)

		case 0x64, 0x65, 0x66: // PCMPGTB/W/D (signed)
			mr := c.parseModRM()
			elem := 1 << (opcode2 - 0x64)
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedCmpGt(dst, src, elem)

		case 0xFC, 0xFD, 0xFE, 0xD4: // PADDB/W/D/Q
			mr := c.parseModRM()
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
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedAdd(dst, src, size)

		case 0xF8, 0xF9, 0xFA, 0xFB: // PSUBB/W/D/Q
			mr := c.parseModRM()
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
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedSub(dst, src, size)

		// Saturating add: 0F DC PADDUSB, 0F DD PADDUSW,
		//                 0F EC PADDSB,  0F ED PADDSW
		case 0xDC, 0xDD, 0xEC, 0xED:
			mr := c.parseModRM()
			elem := 2
			if opcode2 == 0xDC || opcode2 == 0xEC {
				elem = 1
			}
			signed := opcode2 >= 0xEC
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedAddSat(dst, src, elem, signed)

		// Saturating sub: 0F D8 PSUBUSB, 0F D9 PSUBUSW,
		//                 0F E8 PSUBSB,  0F E9 PSUBSW
		case 0xD8, 0xD9, 0xE8, 0xE9:
			mr := c.parseModRM()
			elem := 2
			if opcode2 == 0xD8 || opcode2 == 0xE8 {
				elem = 1
			}
			signed := opcode2 >= 0xE8
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedSubSat(dst, src, elem, signed)

		// Unpack low halves: PUNPCKL{BW,WD,DQ} (MMX, no prefix; SSE2, 66
		// prefix → operate on 128-bit XMM operands; PUNPCKL semantics
		// span the LOW 64 bits of each operand).
		case 0x60, 0x61, 0x62:
			mr := c.parseModRM()
			elem := 1 << (opcode2 - 0x60)
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedUnpackLow(dst, src, elem)

		// Unpack high halves: PUNPCKH{BW,WD,DQ}. With 66 prefix → SSE2
		// XMM, operating on the HIGH 64 bits of each operand.
		case 0x68, 0x69, 0x6A:
			mr := c.parseModRM()
			elem := 1 << (opcode2 - 0x68)
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedUnpackHigh(dst, src, elem)

		// PUNPCKLQDQ xmm, xmm/m128 (66 0F 6C) — SSE2 only.
		// Result low 64 = dst[0]; result high 64 = src[0].
		case 0x6C:
			if operandSize != 2 {
				return fmt.Errorf("0F 6C requires 66 prefix at EIP=%08X", c.eip-2)
			}
			mr := c.parseModRM()
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg] = [2]uint64{c.xmm[mr.reg][0], src[0]}

		// PUNPCKHQDQ xmm, xmm/m128 (66 0F 6D) — SSE2 only.
		// Result low 64 = dst[1]; result high 64 = src[1].
		case 0x6D:
			if operandSize != 2 {
				return fmt.Errorf("0F 6D requires 66 prefix at EIP=%08X", c.eip-2)
			}
			mr := c.parseModRM()
			var src [2]uint64
			if mr.isReg {
				src = c.xmm[mr.rm]
			} else {
				src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
			}
			c.xmm[mr.reg] = [2]uint64{c.xmm[mr.reg][1], src[1]}

		// Pack with signed saturation: PACKSSWB (0F 63), PACKSSDW (0F 6B).
		// SSE2 (66 prefix) operates on the FULL 128 bits — half from each
		// operand. We do it by running the MMX pack twice and concatenating.
		case 0x63, 0x6B:
			mr := c.parseModRM()
			srcSize := 2
			if opcode2 == 0x6B {
				srcSize = 4
			}
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packSignedSat(dst, src, srcSize)

		// Pack with unsigned saturation: PACKUSWB (0F 67). 66 prefix →
		// SSE2 XMM full-width.
		case 0x67:
			mr := c.parseModRM()
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packUnsignedSat(dst, src)

		// 0F C4    PINSRW mm, r32/m16, imm8  — insert 16-bit value into MMX
		// 66 0F C4 PINSRW xmm, r32/m16, imm8 — into XMM (one of 8 lanes)
		case 0xC4:
			mr := c.parseModRM()
			var val uint16
			if mr.isReg {
				val = uint16(c.GetReg32(int(mr.rm)))
			} else {
				val = c.readMem16(c.segBaseForModRM(mr) + mr.ea)
			}
			imm := c.fetch8()
			if operandSize == 2 {
				// XMM 8 lanes
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
				c.mm[mr.reg] = (c.mm[mr.reg] &^ (uint64(0xFFFF) << shift)) |
					(uint64(val) << shift)
			}

		// 0F C5    PEXTRW r32, mm, imm8   — extract 16-bit lane from MMX
		// 66 0F C5 PEXTRW r32, xmm, imm8  — from XMM (one of 8 lanes)
		case 0xC5:
			mr := c.parseModRM()
			if !mr.isReg {
				return fmt.Errorf("PEXTRW requires register source at EIP=%08X", c.eip-2)
			}
			imm := c.fetch8()
			var val uint16
			if operandSize == 2 {
				idx := imm & 7
				v := c.xmm[mr.rm]
				if idx < 4 {
					val = uint16(v[0] >> (uint(idx) * 16))
				} else {
					val = uint16(v[1] >> (uint(idx-4) * 16))
				}
			} else {
				idx := imm & 3
				val = uint16(c.mm[mr.rm] >> (uint(idx) * 16))
			}
			c.SetReg32(int(mr.reg), uint32(val))

		// Multiplies: PMULLW (0F D5), PMULHW (0F E5)
		case 0xD5, 0xE5:
			mr := c.parseModRM()
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			if opcode2 == 0xD5 {
				c.mm[mr.reg] = packedMulLow(dst, src)
			} else {
				c.mm[mr.reg] = packedMulHigh(dst, src, true)
			}

		// PMULUDQ (0F F4) — unsigned 32x32 → 64 multiply, one per 64-bit lane.
		// MMX: 1 lane; SSE2 (66 prefix): 2 lanes (low 32 of each XMM qword).
		case 0xF4:
			mr := c.parseModRM()
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedMulUDQ(dst, src)

		// PMADDWD (0F F5)
		case 0xF5:
			mr := c.parseModRM()
			if operandSize == 2 {
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
				break
			}
			src := c.mmxSrc64(mr)
			dst := c.mm[mr.reg]
			c.mm[mr.reg] = packedMaddWord(dst, src)

		// Variable-count shifts (register or memory source supplies count).
		// 0F D1/D2/D3 PSRLW/PSRLD/PSRLQ
		// 0F E1/E2     PSRAW/PSRAD
		// 0F F1/F2/F3 PSLLW/PSLLD/PSLLQ
		// With 66 prefix: same opcodes but operate on XMM.
		case 0xD1, 0xD2, 0xD3, 0xE1, 0xE2, 0xF1, 0xF2, 0xF3:
			mr := c.parseModRM()
			var elem int
			switch opcode2 & 0x0F {
			case 0x01:
				elem = 2
			case 0x02:
				elem = 4
			case 0x03:
				elem = 8
			}
			if operandSize == 2 {
				var src [2]uint64
				if mr.isReg {
					src = c.xmm[mr.rm]
				} else {
					src = c.readMem128(c.segBaseForModRM(mr) + mr.ea)
				}
				// Count is the low 64-bit lane (treated as a saturated count
				// per Intel SDM); we take its byte 0.
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
				break
			}
			count := int(c.mmxSrc64(mr) & 0xFF)
			dst := c.mm[mr.reg]
			switch {
			case opcode2 >= 0xF1:
				c.mm[mr.reg] = packedShiftLeft(dst, count, elem)
			case opcode2 >= 0xE1:
				c.mm[mr.reg] = packedShiftRightArith(dst, count, elem)
			default:
				c.mm[mr.reg] = packedShiftRightLogical(dst, count, elem)
			}

		// PSHUFW (MMX), PSHUFD/PSHUFLW/PSHUFHW (SSE2) — opcode 0F 70.
		// Prefix dispatch:
		//   no prefix → PSHUFW  mm,  mm/m64,  imm8       (4 word shuffle)
		//   66 prefix → PSHUFD  xmm, xmm/m128, imm8      (4 dword shuffle)
		//   F2 prefix → PSHUFLW xmm, xmm/m128, imm8      (low 4 words shuffled,
		//                                                 high 64 bits copied)
		//   F3 prefix → PSHUFHW xmm, xmm/m128, imm8      (high 4 words shuffled,
		//                                                 low 64 bits copied)
		case 0x70:
			mr := c.parseModRM()
			switch {
			case operandSize == 2: // 66 → PSHUFD
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
				c.mm[mr.reg] = pshufWord(src, imm)
			}

		// Immediate-count shifts: 0F 71/72/73 are group-encoded; the
		// subopcode lives in the reg field of the following ModR/M byte.
		// 0F 71 /6 PSLLW, /4 PSRAW, /2 PSRLW
		// 0F 72 /6 PSLLD, /4 PSRAD, /2 PSRLD
		// 0F 73 /6 PSLLQ, /2 PSRLQ
		// With 66 prefix, the same forms operate on XMM and add two
		// 128-bit byte shifts: /7 PSLLDQ (byte shift left of full
		// 128-bit value), /3 PSRLDQ (byte shift right).
		case 0x71, 0x72, 0x73:
			mr := c.parseModRM()
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
			if operandSize == 2 {
				// SSE2 form: 66-prefixed → operates on c.xmm[mr.rm].
				v := c.xmm[mr.rm]
				switch mr.reg {
				case 6: // PSLLW/D/Q (per-lane left)
					v[0] = packedShiftLeft(v[0], imm, elem)
					v[1] = packedShiftLeft(v[1], imm, elem)
				case 4: // PSRAW/D (no PSRAQ)
					if opcode2 == 0x73 {
						return fmt.Errorf("PSRAQ does not exist (66 0F 73 /4)")
					}
					v[0] = packedShiftRightArith(v[0], imm, elem)
					v[1] = packedShiftRightArith(v[1], imm, elem)
				case 2: // PSRLW/D/Q (per-lane logical right)
					v[0] = packedShiftRightLogical(v[0], imm, elem)
					v[1] = packedShiftRightLogical(v[1], imm, elem)
				case 7: // PSLLDQ (full 128-bit byte shift left)
					if opcode2 != 0x73 {
						return fmt.Errorf("PSLLDQ only valid as 66 0F 73 /7, got 0F %02X /7", opcode2)
					}
					v = byteShiftLeft128(v, imm)
				case 3: // PSRLDQ (full 128-bit byte shift right)
					if opcode2 != 0x73 {
						return fmt.Errorf("PSRLDQ only valid as 66 0F 73 /3, got 0F %02X /3", opcode2)
					}
					v = byteShiftRight128(v, imm)
				default:
					return fmt.Errorf("unsupported SSE2 66 0F %02X /%d at EIP=%08X", opcode2, mr.reg, c.eip-3)
				}
				c.xmm[mr.rm] = v
				return nil
			}
			dst := c.mm[mr.rm]
			switch mr.reg {
			case 6: // PSLL
				c.mm[mr.rm] = packedShiftLeft(dst, imm, elem)
			case 4: // PSRA (W/D only; not Q)
				if opcode2 == 0x73 {
					return fmt.Errorf("PSRAQ does not exist in MMX (0F 73 /4)")
				}
				c.mm[mr.rm] = packedShiftRightArith(dst, imm, elem)
			case 2: // PSRL
				c.mm[mr.rm] = packedShiftRightLogical(dst, imm, elem)
			default:
				return fmt.Errorf("unsupported MMX 0F %02X /%d at EIP=%08X", opcode2, mr.reg, c.eip-3)
			}

		default:
			return fmt.Errorf("unimplemented 0F opcode: %02X at EIP=%08X", opcode2, c.eip-2)
		}

	default:
		return fmt.Errorf("unimplemented opcode: %02X at EIP=%08X", opcode, c.eip-1)
	}
	return nil
}

// executeString executes a string instruction with optional REP prefix.
func (c *CPU) executeString(repPrefix uint8, size uint32,
	op func(), checkZF bool, zfExpected bool) {
	if repPrefix == 0 {
		op()
		return
	}
	// Per Intel SDM: ECX is decremented for each iteration that COMPLETES.
	// If `op()` faults (raises #PF / #SS / #GP), the panic unwinds through
	// here; ECX must reflect successful iterations only so the kernel's
	// fault-fixup path sees an accurate remaining count and the instruction
	// is restartable. Decrementing BEFORE op() over-counts when op() faults
	// and on retry we lose one iteration (manifests as e.g. one byte dropped
	// from copy_to_user when the user page faults mid-rep-movsb).
	for c.GetReg32(ECX) != 0 {
		op()
		c.SetReg32(ECX, c.GetReg32(ECX)-1)
		if checkZF {
			if zfExpected && !c.getZF() {
				break
			}
			if !zfExpected && c.getZF() {
				break
			}
		}
	}
}

// fetchMoffs reads a 16/32-bit offset for moffs addressing.
// In real mode / 16-bit address size, this is 16 bits.
// For now, we assume 16-bit real mode.
func (c *CPU) fetchMoffs() uint32 {
	if c.currentAddrSize == 4 {
		return c.fetch32()
	}
	return uint32(c.fetch16())
}

// push16 pushes a 16-bit value onto the stack. The write is translated first
// (which may raise #PF or #SS); only on success do we commit the new SP. This
// matches real x86 instruction-restart semantics, leaving SP unchanged when a
// fault propagates so the kernel's #PF handler sees a consistent frame.
//
//go:noinline
func (c *CPU) push16(v uint16) {
	newSP := c.GetReg16(SP) - 2
	c.checkStackLimit(uint32(newSP), 2)
	c.writeMem16(c.segBase[SS]+uint32(newSP), v)
	c.SetReg16(SP, newSP)
}

// pop16 pops a 16-bit value from the stack.
func (c *CPU) pop16() uint16 {
	v := c.readMem16(c.segBase[SS] + uint32(c.GetReg16(SP)))
	c.SetReg16(SP, c.GetReg16(SP)+2)
	return v
}

// push32 pushes a 32-bit value onto the stack. PF-safe: SP is committed only
// after the memory write succeeds.
//
//go:noinline
func (c *CPU) push32(v uint32) {
	newESP := c.GetReg32(ESP) - 4
	c.checkStackLimit(newESP, 4)
	c.writeMem32(c.segBase[SS]+newESP, v)
	c.SetReg32(ESP, newESP)
}

// pop32 pops a 32-bit value from the stack.
func (c *CPU) pop32() uint32 {
	v := c.readMem32(c.segBase[SS] + c.GetReg32(ESP))
	c.SetReg32(ESP, c.GetReg32(ESP)+4)
	return v
}

// popSegReg implements POP <seg> for ES/SS/DS/FS/GS. In protected mode the
// new descriptor is fetched via LoadSegmentProtected so base/limit/access
// are kept in sync; without this the segment selector changes but accesses
// would still use the OLD base, corrupting subsequent loads/stores.
// For SS, the interrupts-blocked flag is set per Intel SDM.
func (c *CPU) popSegReg(segReg int, operandSize uint8, blockInterrupts bool) error {
	var sel uint16
	if operandSize == 2 {
		sel = c.pop16()
	} else {
		sel = uint16(c.pop32())
	}
	if c.IsProtectedMode() {
		if err := c.LoadSegmentProtected(segReg, sel); err != nil {
			return err
		}
	} else {
		c.seg[segReg] = sel
		c.segBase[segReg] = uint32(sel) << 4
		c.segLimit[segReg] = 0xFFFF
		c.segAccess[segReg] = 0
	}
	if blockInterrupts {
		c.interruptsBlocked = true
	}
	return nil
}

// eflagsReservedSetMask is the OR-mask applied to every value loaded into
// EFLAGS: bit 1 is reserved-as-1 per the Intel SDM. (Bits 3, 5, 15, 22-31
// are reserved-as-0 but we don't enforce that — software that depends on
// reading them as 0 is rare and our masking would change semantics for
// extensions like AC/VIF/VIP.)
const eflagsReservedSetMask = uint32(0x02)

// setEFlagsFromPop loads a value popped off the stack into c.eflags while
// honoring Intel's rules:
//   - bit 1 (reserved) is always written as 1.
//   - bits 17 (VM), 19 (VIF), 20 (VIP) are only writable at CPL=0.
//   - IOPL field (bits 12-13) is only writable at CPL=0; lower CPLs keep
//     the current IOPL.
//   - IF (bit 9) is only writable when CPL <= IOPL.
//
// Without this guard, real-mode/PE early-init code that does POPF or IRET
// with a value that happens to have bit 1 = 0 would silently clear it, and
// any subsequent `testl $0x2, EFLAGS` (used by Linux's SAVE_ALL trampoline
// to detect v8086 entry vs kernel entry) would take the wrong path, leading
// to stack corruption.
func (c *CPU) setEFlagsFromPop(val uint32, cpl int) {
	const fullCPL0Mask = ValidFlagMask
	preserved := uint32(0)
	allowed := uint32(0xFFFFFFFF)
	if c.IsProtectedMode() && cpl > 0 {
		// CPL > 0 cannot modify IOPL, VM, VIF, VIP.
		allowed = ^(uint32(EFLAGS_IOPL) | EFLAGS_VM | EFLAGS_VIF | EFLAGS_VIP)
		iopl := (c.eflags & EFLAGS_IOPL) >> 12
		if uint32(cpl) > iopl {
			// IF is not modifiable either.
			allowed &^= EFLAGS_IF
		}
		preserved = c.eflags &^ allowed
	}
	c.eflags = ((val & allowed) | preserved) & fullCPL0Mask
	c.eflags |= eflagsReservedSetMask
}

// pushOp pushes a value of the given operand size (2 or 4).
// checkIOPL raises #GP if the current CPL is greater than the IOPL.
func (c *CPU) checkIOPL() {
	if c.IsProtectedMode() {
		iopl := (c.eflags & EFLAGS_IOPL) >> 12
		if c.cpl > int(iopl) {
			c.raiseGeneralProtectionFault(0)
		}
	}
}

func (c *CPU) pushOp(v uint32, size uint8) {
	if size == 2 {
		c.push16(uint16(v))
	} else {
		c.push32(v)
	}
}

// popOp pops a value of the given operand size (2 or 4).
func (c *CPU) popOp(size uint8) uint32 {
	if size == 2 {
		return uint32(c.pop16())
	}
	return c.pop32()
}

// handleInterrupt services a software or hardware interrupt. The body lives
// in handleInterruptCore; this wrapper adds the diagnostic trace and the
// double-fault recovery path. On a nested fault during delivery it tries to
// deliver vector 8 (#DF); if that also faults we report a triple-fault
// error rather than letting the Go panic propagate.
func (c *CPU) handleInterrupt(vector uint8, isHardware bool, errorCode ...uint32) (err error) {
	if intDebug {
		ec := uint32(0)
		if len(errorCode) > 0 {
			ec = errorCode[0]
		}
		fmt.Fprintf(os.Stderr, "[INT] vec=0x%02X hw=%v errcode=0x%X EIP=0x%08X ESP=0x%08X CR2=0x%08X cycles=%d\n",
			vector, isHardware, ec, c.eip, c.GetReg32(ESP), c.cr[2], c.cycles)
	}
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		err = c.deliverDoubleFault(vector, r)
	}()
	return c.handleInterruptCore(vector, isHardware, errorCode...)
}

// deliverDoubleFault is called by handleInterrupt's defer when delivery
// raised a nested fault. It attempts to deliver vector 8 (#DF); on a further
// nested fault it returns a triple-fault error.
func (c *CPU) deliverDoubleFault(origVector uint8, firstPanic any) (err error) {
	var origMsg string
	switch ex := firstPanic.(type) {
	case pageFaultError:
		origMsg = fmt.Sprintf("nested #PF: linear=0x%08X errcode=0x%X", ex.addr, ex.errorCode)
	case stackFaultError:
		origMsg = fmt.Sprintf("nested #SS: errcode=0x%X", ex.errorCode)
	case generalProtectionFaultError:
		origMsg = fmt.Sprintf("nested #GP: errcode=0x%X", ex.errorCode)
	default:
		panic(firstPanic)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("triple fault delivering vector 0x%02X: %s; #DF also faulted",
				origVector, origMsg)
		}
	}()
	if dfErr := c.handleInterruptCore(8, true, 0); dfErr != nil {
		return fmt.Errorf("delivering vector 0x%02X: %s; #DF gate failed: %w",
			origVector, origMsg, dfErr)
	}
	return nil
}

// handleInterruptCore is the actual delivery body. It does NOT recover from
// panics — its callers wrap it in defer/recover (handleInterrupt for the
// initial delivery, deliverDoubleFault for the #DF attempt).
func (c *CPU) handleInterruptCore(vector uint8, isHardware bool, errorCode ...uint32) error {

	// Hardware interrupts wake the CPU from HLT.
	if isHardware {
		c.powerDown = false
	}
	if !c.IsProtectedMode() {
		// Real mode: push FLAGS, CS, IP; clear IF and TF; jump to IVT entry.
		c.push16(uint16(c.eflags))
		c.push16(c.seg[CS])
		c.push16(uint16(c.eip))
		c.eflags &^= EFLAGS_IF | EFLAGS_TF
		ivtAddr := uint32(vector) * 4
		newIP := c.readMem16(ivtAddr)
		newCS := c.readMem16(ivtAddr + 2)
		c.seg[CS] = newCS
		c.segBase[CS] = uint32(newCS) << 4
		c.eip = uint32(newIP)
		c.maskEIP()
		return nil
	}

	// Protected mode: read IDT gate descriptor.
	idtBase := c.segBase[IDTR]
	idtLimit := c.segLimit[IDTR]
	offset := uint32(vector) * 8
	if offset+7 > idtLimit {
		return fmt.Errorf("interrupt vector %02X exceeds IDT limit", vector)
	}

	gateAddr := idtBase + offset
	var gate [8]byte
	for i := 0; i < 8; i++ {
		gate[i] = c.readMemSV8(gateAddr + uint32(i))
	}

	offsetLow := uint32(gate[0]) | uint32(gate[1])<<8
	selector := uint16(gate[2]) | uint16(gate[3])<<8
	// byte 4 is unused/reserved
	gateType := gate[5] & 0x0F
	dpl := (gate[5] >> 5) & 3
	present := gate[5]&0x80 != 0
	offsetHigh := uint32(gate[6]) | uint32(gate[7])<<8

	if !present {
		return fmt.Errorf("interrupt gate not present for vector %02X", vector)
	}

	// For software interrupts (INT n), check DPL >= CPL.
	if !isHardware && dpl < uint8(c.cpl) {
		return fmt.Errorf("INT %02X: DPL < CPL", vector)
	}

	// Validate and load CS.
	if selector == 0 {
		return fmt.Errorf("interrupt gate null CS selector for vector %02X", vector)
	}
	oldCPL := c.cpl
	oldCS := c.seg[CS]
	oldEIP := c.eip
	oldEFLAGS := c.eflags
	if err := c.LoadSegmentProtected(CS, selector); err != nil {
		return fmt.Errorf("interrupt gate load CS failed for vector %02X: %w", vector, err)
	}

	csAccess := c.segAccess[CS]
	csType := (csAccess >> 0) & 0x0F
	// Code segment types are 0x8-0xF; bit 3 must be set.
	if csType&0x08 == 0 {
		return fmt.Errorf("interrupt gate CS is not code for vector %02X", vector)
	}
	csDPL := uint8((csAccess >> 5) & 3)
	if csDPL > uint8(oldCPL) {
		return fmt.Errorf("interrupt gate CS DPL > CPL for vector %02X", vector)
	}

	// For non-conforming code segments, CPL becomes the DPL of the code segment.
	// For conforming segments, CPL does not change.
	isConforming := (csType&0x04 != 0) // types 0xC-0xF with bit 2 set
	if !isConforming {
		c.cpl = int(csDPL)
	}

	// Determine operand size from gate type.
	// 0xE = 386 interrupt gate, 0xF = 386 trap gate (32-bit)
	// 0x6 = 286 interrupt gate, 0x7 = 286 trap gate (16-bit)
	gateSize := uint8(2)
	if gateType == 0x0E || gateType == 0x0F {
		gateSize = 4
	}

	// Stack switch if going to a more privileged ring (lower DPL number).
	// Conforming segments do not change CPL, so no stack switch occurs.
	if !isConforming && oldCPL > int(csDPL) {
		// Read new SS:ESP from the TSS.
		tssBase := c.segBase[TR]
		tssLimit := c.segLimit[TR]
		// TSS offsets: ESPn at 0x04 + n*8, SSn at 0x08 + n*8
		espOffset := uint32(4 + csDPL*8)
		ssOffset := uint32(8 + csDPL*8)
		if ssOffset+1 > tssLimit {
			return fmt.Errorf("TSS limit too small for stack switch to ring %d", csDPL)
		}
		newESP := c.readMemSV32(tssBase + espOffset)
		newSS := c.readMemSV16(tssBase + ssOffset)

		oldSS := c.seg[SS]
		oldESP := c.GetReg32(ESP)

		// Load new SS.
		if err := c.LoadSegmentProtected(SS, newSS); err != nil {
			return fmt.Errorf("stack switch load SS failed: %w", err)
		}
		// Set new ESP.
		c.SetReg32(ESP, newESP)

		// Push old SS and old ESP onto the new stack.
		if gateSize == 2 {
			c.push16(oldSS)
			c.push16(uint16(oldESP))
		} else {
			c.push32(uint32(oldSS))
			c.push32(oldESP)
		}
	}

	// Push FLAGS, CS, EIP (using saved old values).
	c.pushOp(oldEFLAGS, gateSize)
	c.pushOp(uint32(oldCS), gateSize)
	c.pushOp(oldEIP, gateSize)

	// Push error code if provided (for exceptions like #PF).
	// Error codes are always 32-bit in 32-bit protected mode, even for 16-bit gates.
	if len(errorCode) > 0 {
		c.push32(errorCode[0])
	}

	// Clear VM bit when entering handler from v8086 mode.
	c.eflags &^= EFLAGS_VM

	// Interrupt gate (but not trap gate) clears IF and TF.
	if gateType == 0x06 || gateType == 0x0E {
		c.eflags &^= EFLAGS_IF | EFLAGS_TF
	}

	// Set RF to suppress debug exceptions on the first instruction of the handler.
	c.eflags |= EFLAGS_RF

	// Jump to handler.
	c.eip = offsetLow | offsetHigh<<16
	c.maskEIP()
	return nil
}

// ioRead8 reads from an I/O port.
func (c *CPU) ioRead8(port uint16) uint8 {
	if c.ioRead8Func != nil {
		return c.ioRead8Func(port)
	}
	return 0xFF
}

// ioRead16 reads from an I/O port.
func (c *CPU) ioRead16(port uint16) uint16 {
	if c.ioRead16Func != nil {
		return c.ioRead16Func(port)
	}
	return 0xFFFF
}

// ioRead32 reads from an I/O port.
func (c *CPU) ioRead32(port uint16) uint32 {
	if c.ioRead32Func != nil {
		return c.ioRead32Func(port)
	}
	return 0xFFFFFFFF
}

// ioWrite8 writes to an I/O port.
func (c *CPU) ioWrite8(port uint16, val uint8) {
	if c.ioWrite8Func != nil {
		c.ioWrite8Func(port, val)
	}
}

// ioWrite16 writes to an I/O port.
func (c *CPU) ioWrite16(port uint16, val uint16) {
	if c.ioWrite16Func != nil {
		c.ioWrite16Func(port, val)
	}
}

// ioWrite32 writes to an I/O port.
func (c *CPU) ioWrite32(port uint16, val uint32) {
	if c.ioWrite32Func != nil {
		c.ioWrite32Func(port, val)
	}
}
